package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
)

// EmbeddingConfig Embedding 配置
//
// 私域部署基线（， 修订）：Embedding 必须在本地推理服务内完成，
// 跑真实 BGE 模型（bge-m3，1024 维），不允许静默走公网 LLM 厂商 API，
// 也不允许静默降级到哈希伪向量。
//
//   - 正常路径：POST {BaseURL}/v1/embeddings（bge-m3，dim=1024）
//   - 强约束：BaseURL 默认 http://mtk-embedding:8208/v1（docker 网络）或 http://127.0.0.1:8208/v1（宿主机）
//   - 离线/单测：需显式设置 EMBEDDING_ALLOW_FALLBACK=true 才允许 hash 降级
type EmbeddingConfig struct {
	APIKey         string
	BaseURL        string
	APIType        string
	Model          string
	Dimension      int
	RequestTimeout int
	MaxRetries     int
	AllowFallback  bool
}

// EmbeddingServiceInterface Embedding 服务接口
type EmbeddingServiceInterface interface {
	Embed(ctx context.Context, cfg *EmbeddingConfig, texts []string) ([][]float32, error)
	EmbedOne(ctx context.Context, cfg *EmbeddingConfig, text string) ([]float32, error)
	DefaultConfig() *EmbeddingConfig
}

// EmbeddingService 真实 Embedding 服务
//
// 默认对接本地推理服务（http://mtk-embedding:8208/v1 docker / http://127.0.0.1:8208/v1 宿主机，私域部署强制）；
// 调用 /v1/embeddings（OpenAI 兼容协议，底层跑真实 bge-m3 模型）。
//
// 强约束：
//  1. 不再 fallback 到任何云端 LLM 厂商（OpenAI/Azure/通义/智谱/Moonshot）
//     私域数据禁止出域；LLM 走 API ≠ Embedding 走 API。
//  2. 当 EMBEDDING_ALLOW_FALLBACK=false（默认）时，
//     若本地 embedding 不可达直接返回错误，禁止静默使用哈希伪向量。
//  3. 当 EMBEDDING_ALLOW_FALLBACK=true 时（仅单测），
//     允许降级到 hash 实现并打 ERROR 日志。
type EmbeddingService struct {
	httpClient    *http.Client
	fallback      EmbeddingServiceInterface
	defaultConfig *EmbeddingConfig
}

var sharedEmbeddingTransport = &http.Transport{
	MaxIdleConns:        200,
	MaxIdleConnsPerHost: 50,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

type EmbeddingLane int

const (
	// EmbeddingLaneOnline 在线检索车道（默认）：API 调用方零改动即走本车道。
	EmbeddingLaneOnline EmbeddingLane = iota
	// EmbeddingLaneBatch 批量入库车道：知识库批量向量化走本车道，为在线检索让路
	// （batch 容量恒为 1，同一时刻至多一个批量任务在飞；online 至少保留 1 个槽位）。
	EmbeddingLaneBatch
)

type embeddingLanes struct {
	online chan struct{}
	batch  chan struct{}
}

func newEmbeddingLanesFrom(n int) *embeddingLanes {
	if n < 1 {
		n = 1
	}
	online := n - 1
	if online < 1 {
		online = 1
	}
	return &embeddingLanes{
		online: make(chan struct{}, online),
		batch:  make(chan struct{}, 1),
	}
}

func newEmbeddingLanes() *embeddingLanes {
	n := 1
	if v := strings.TrimSpace(os.Getenv("EMBEDDING_CONCURRENCY")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 1 && parsed <= 16 {
			n = parsed
		}
	}
	return newEmbeddingLanesFrom(n)
}

func (l *embeddingLanes) laneChannel(lane EmbeddingLane) chan struct{} {
	if l == nil {
		return nil
	}
	if lane == EmbeddingLaneBatch {
		return l.batch
	}
	return l.online
}

var embeddingLanesSem = newEmbeddingLanes()

func acquireEmbeddingSlot(ctx context.Context, ch chan struct{}) error {
	if ch == nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- struct{}{}:
		return nil
	}
}

func releaseEmbeddingSlot(ch chan struct{}) {
	if ch != nil {
		<-ch
	}
}

// NewEmbeddingService 构造真实 Embedding 服务
func NewEmbeddingService() *EmbeddingService {
	return &EmbeddingService{
		httpClient: &http.Client{Transport: sharedEmbeddingTransport, Timeout: 60 * time.Second},
		fallback:   &HashEmbeddingService{defaultDim: 1024},
	}
}

// NewEmbeddingServiceWithConfig 构造带 per 知识库配置的 Embedding 服务
// （DefaultConfig 直接返回 cfg，使检索/入库走指定云端 text-embedding 端点）
func NewEmbeddingServiceWithConfig(cfg *EmbeddingConfig) *EmbeddingService {
	return &EmbeddingService{
		httpClient:    &http.Client{Transport: sharedEmbeddingTransport, Timeout: 60 * time.Second},
		fallback:      &HashEmbeddingService{defaultDim: 1024},
		defaultConfig: cfg,
	}
}

// DefaultConfig 读取配置得到默认配置（配置文件为准，其次环境变量，最后内置默认）
//
// 优先级（私域部署基线）：
//  1. config.yaml 的 embedding 段（为准）：
//     - 宿主机：http://127.0.0.1:8208/v1（llama.cpp / MLX，端口 8208）
//  2. 环境变量（EMBEDDING_*）仅在配置文件未指定时回退读取，便于部署层/调试显式覆盖
//  3. 内置默认：http://127.0.0.1:8208/v1（仅当配置与环境都缺失时生效）
//
// 注意：必须以配置文件为准，否则被错误的服务名带偏
// （宿主机无法解析容器服务名，导致 embedding 不可达并静默降级哈希伪向量）。
func (s *EmbeddingService) DefaultConfig() *EmbeddingConfig {
	if s.defaultConfig != nil {
		return s.defaultConfig
	}
	fileCfg := config.GetAppConfig().Inference.Embedding
	baseURL := fileCfg.BaseURL
	model := fileCfg.Model
	dim := fileCfg.Dimension
	allowFallback := fileCfg.AllowFallback
	apiKey := fileCfg.APIKey

	if baseURL == "" {
		baseURL = os.Getenv("EMBEDDING_BASE_URL")
	}
	if model == "" {
		model = os.Getenv("EMBEDDING_MODEL")
	}
	if dim <= 0 {
		if v := os.Getenv("EMBEDDING_DIM"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				dim = n
			}
		}
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("EMBEDDING_ALLOW_FALLBACK"))); v != "" {
		allowFallback = v == "true" || v == "1" || v == "yes"
	}

	if baseURL == "" {
		baseURL = config.DefaultEmbeddingBaseURLDocker
	}
	if model == "" {
		model = config.DefaultEmbeddingModel()
	}
	if dim <= 0 {
		switch model {
		case "text-embedding-3-large":
			dim = 3072
		case "text-embedding-ada-002", "text-embedding-3-small":
			dim = 1536
		default:
			dim = 1024
		}
	}
	if dim != 1024 {
		logger.Warnf("[embedding] WARN: configured dimension=%d != 1024, force reset to 1024 (pgvector vector(1024) compatible)", dim)
		dim = 1024
	}

	if apiKey == "" {
		apiKey = os.Getenv("EMBEDDING_API_KEY")
	}
	return &EmbeddingConfig{
		APIKey:         apiKey,
		BaseURL:        baseURL,
		APIType:        "openai",
		Model:          model,
		Dimension:      dim,
		RequestTimeout: 60,
		MaxRetries:     5,
		AllowFallback:  allowFallback,
	}
}

var embeddingMaxBatchGetter = func() int { return 64 }

func embeddingMaxBatch() int { return embeddingMaxBatchGetter() }

// SetEmbeddingMaxBatchGetter 装配层注入 DB 驱动的 embedding_max_batch 读取器
func SetEmbeddingMaxBatchGetter(fn func() int) {
	embeddingMaxBatchGetter = fn
}

// Embed 批量向量化（在线车道，API 兼容：现有调用方默认 online，零改动）。
func (s *EmbeddingService) Embed(ctx context.Context, cfg *EmbeddingConfig, texts []string) ([][]float32, error) {
	return s.EmbedWithLane(ctx, cfg, texts, EmbeddingLaneOnline)
}

// EmbedWithSource 带来源标记的向量化（D16）：返回本批向量来源（"tei"/"hash"）。
// 批内同源：AllowFallback=true 整批走 fallback；false 整批走 provider（失败即返错，无批内静默降级）。
// 仅新调用方使用；旧 Embed 语义不变。
func (s *EmbeddingService) EmbedWithSource(ctx context.Context, cfg *EmbeddingConfig, texts []string) ([][]float32, string, error) {
	if len(texts) == 0 {
		return [][]float32{}, EmbedSourceTEI, nil
	}
	if cfg != nil && cfg.AllowFallback {
		vecs, err := s.EmbedWithLane(ctx, cfg, texts, EmbeddingLaneOnline)
		return vecs, EmbedSourceHash, err
	}
	vecs, err := s.EmbedWithLane(ctx, cfg, texts, EmbeddingLaneOnline)
	return vecs, EmbedSourceTEI, err
}

// 向量来源常量（D16）
const (
	EmbedSourceTEI  = "tei"
	EmbedSourceHash = "hash"
)

// EmbedWithLane 按指定车道向量化（N-1：批量入库传 EmbeddingLaneBatch 为在线检索让路）。
func (s *EmbeddingService) EmbedWithLane(ctx context.Context, cfg *EmbeddingConfig, texts []string, lane EmbeddingLane) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if cfg == nil {
		cfg = s.DefaultConfig()
	}

	if !cfg.AllowFallback {
		if len(texts) <= embeddingMaxBatch() {
			return s.callProviderWithRetry(ctx, cfg, texts, lane)
		}
		return s.embedInBatches(ctx, cfg, texts, lane)
	}

	logger.Errorf("[Embedding] ERROR: EMBEDDING_ALLOW_FALLBACK=true,降级为本地哈希向量(仅供离线/单测,严禁生产环境使用)")
	return s.fallback.Embed(ctx, cfg, texts)
}

func (s *EmbeddingService) embedInBatches(ctx context.Context, cfg *EmbeddingConfig, texts []string, lane EmbeddingLane) ([][]float32, error) {
	total := len(texts)
	result := make([][]float32, total)
	for i := 0; i < total; i += embeddingMaxBatch() {
		end := i + embeddingMaxBatch()
		if end > total {
			end = total
		}
		batch := texts[i:end]
		vectors, err := s.callProviderWithRetry(ctx, cfg, batch, lane)
		if err != nil {
			return nil, fmt.Errorf("embedding 分片 %d-%d 失败: %w", i, end-1, err)
		}
		copy(result[i:end], vectors)
	}
	logger.Infof("[Embedding] 大批量分片完成: %d 条文本, 分 %d 批", total, (total+embeddingMaxBatch()-1)/embeddingMaxBatch())
	return result, nil
}

func (s *EmbeddingService) callProviderWithRetry(ctx context.Context, cfg *EmbeddingConfig, texts []string, lane EmbeddingLane) ([][]float32, error) {
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	slotCh := embeddingLanesSem.laneChannel(lane)
	candidates := s.baseURLCandidates(cfg.BaseURL)
	var lastErr error
	for ci, baseURL := range candidates {
		attemptCfg := *cfg
		attemptCfg.BaseURL = baseURL
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				if isRateLimited(lastErr) {
					backoff := min(time.Duration(2<<uint(attempt))*time.Second, 30*time.Second)
					logger.Warnf("[Embedding] 限流/过载，退避 %.0fs 后重试 (baseURL=%s attempt %d/%d): %v", backoff.Seconds(), baseURL, attempt+1, maxRetries, lastErr)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(backoff):
					}
				} else {
					backoff := min(time.Duration(1<<uint(attempt))*time.Second, 30*time.Second)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(backoff):
					}
				}
			}
			if err := acquireEmbeddingSlot(ctx, slotCh); err != nil {
				return nil, err
			}
			vectors, err := s.callProvider(ctx, &attemptCfg, texts)
			releaseEmbeddingSlot(slotCh)
			if err == nil {
				return vectors, nil
			}
			lastErr = err
			if ci < len(candidates)-1 && isConnError(err) {
				logger.Warnf("[Embedding] %s 不可达，回退候选 BaseURL: %v", baseURL, err)
				break
			}
			logger.Errorf("[Embedding] 本地 embedding 服务调用失败 (baseURL=%s attempt %d/%d): %v", baseURL, attempt+1, maxRetries, err)
		}
	}
	return nil, fmt.Errorf("本地 embedding 服务不可达（%s），已重试 %d 次: %w。请检查 embedding 容器是否启动或设置正确的 EMBEDDING_BASE_URL: %v",
		cfg.BaseURL, maxRetries, lastErr, lastErr)
}

func (s *EmbeddingService) baseURLCandidates(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return []string{raw}
	}
	if strings.HasPrefix(u.Host, "mtk-embedding") || strings.HasPrefix(u.Host, "tei-embedding") {
		alt := *u
		alt.Host = "localhost" + strings.TrimPrefix(u.Host, "mtk-embedding")
		alt.Host = strings.TrimPrefix(alt.Host, "tei-embedding")
		return []string{raw, alt.String()}
	}
	return []string{raw}
}

func isConnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "i/o timeout")
}

func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no permits available") ||
		strings.Contains(msg, "Model is overloaded") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "Too Many Requests")
}

// EmbedOne 单条向量化
func (s *EmbeddingService) EmbedOne(ctx context.Context, cfg *EmbeddingConfig, text string) ([]float32, error) {
	vectors, err := s.Embed(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("embedding 返回空")
	}
	return vectors[0], nil
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (s *EmbeddingService) callProvider(ctx context.Context, cfg *EmbeddingConfig, texts []string) ([][]float32, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("EMBEDDING_BASE_URL 未配置")
	}

	if !strings.HasSuffix(baseURL, "/v1") && !strings.HasSuffix(baseURL, "/v1/") {
		baseURL = baseURL + "/v1"
	}
	endpoint := baseURL + "/embeddings"
	timeout := time.Duration(cfg.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(embeddingRequest{Model: cfg.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("embedding 鉴权失败 status=%d body=%s", resp.StatusCode, string(respBody))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("embedding 服务端错误 status=%d body=%s", resp.StatusCode, string(respBody))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API error status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var er embeddingResponse
	if err := json.Unmarshal(respBody, &er); err != nil {
		return nil, fmt.Errorf("decode response: %w (body=%s)", err, string(respBody))
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embedding API error: %s", er.Error.Message)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("embedding 返回数量不匹配: expect %d, got %d", len(texts), len(er.Data))
	}

	vectors := make([][]float32, len(texts))
	for _, d := range er.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embedding 返回 index 越界: %d", d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}
