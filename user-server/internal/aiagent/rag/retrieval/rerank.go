package ragretrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
)

// RerankDoc 待重排文档
type RerankDoc struct {
	ID      string
	Content string
}

// RerankResult 重排结果（按相关性降序）
type RerankResult struct {
	ID    string
	Score float64
}

// RerankerInterface 重排接口
type RerankerInterface interface {
	Rerank(ctx context.Context, query string, docs []RerankDoc) ([]RerankResult, error)
}

// RerankConfig 重排配置（来自环境变量）
type RerankConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	Enabled    bool
	Timeout    int
	MaxRetries int
}

// LocalReranker 基于本地推理服务的重排实现（OpenAI 兼容 /v1/rerank）
//
// 私域部署基线（修订）：rerank 与 embedding 同走本地推理服务（llama.cpp），
// 使用 bge-reranker-v2-m3（跨编码器），数据不出域。
//
// 端点约定（与 embedding 类似，在 BaseURL 后追加 /rerank）：
//   - TEI:       BaseURL=http://host:port      → POST {BaseURL}/rerank（根路径）
//   - llama.cpp: BaseURL=http://host:port/v1   → POST {BaseURL}/rerank（即 /v1/rerank）
//
// 代码层会自动补齐 /v1（与 embedding_service.go 一致），config.yaml 中 base_url
// 无论是否带 /v1 后缀都能正确路由到 /v1/rerank。
// 两者响应字段一致：results[].index / results[].relevance_score，但量纲不同：
// TEI/云端已做 sigmoid（0~1），llama.cpp 直接回原始 logit（可为负、可 >1），
// 由 normalizeRerankScores 归一回概率域后再套用 rerankScoreFloor。
type LocalReranker struct {
	httpClient *http.Client
	cfg        *RerankConfig
}

var sharedRerankTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

// NewLocalReranker 构造本地重排器
func NewLocalReranker() *LocalReranker {
	return &LocalReranker{httpClient: &http.Client{Transport: sharedRerankTransport, Timeout: 30 * time.Second}}
}

// NewLocalRerankerWithConfig 构造带 per 知识库配置的重排器
// （Rerank 直接使用 cfg，使重排走指定云端 rerank 端点）
func NewLocalRerankerWithConfig(cfg *RerankConfig) *LocalReranker {
	return &LocalReranker{
		httpClient: &http.Client{Transport: sharedRerankTransport, Timeout: 30 * time.Second},
		cfg:        cfg,
	}
}

// 私域部署 rerank 模型支持表：
//   - bge-reranker-base       轻量基线，CPU 可跑，单文档 <50ms
//   - bge-reranker-large      高精度，GPU 推荐，单文档 <200ms
//   - bge-reranker-v2-m3      多语种，默认推荐
//   - bge-reranker-v2-gemma   多语种高精（自托管，>2B 参数）
//
// 通过 RERANK_MODEL 环境变量或 config.yaml 切换；BaseURL 不变。
const (
	RerankModelBgeBase  = "bge-reranker-base"
	RerankModelBgeLarge = "bge-reranker-large"
	RerankModelBgeV2M3  = "bge-reranker-v2-m3"
	rerankScoreFloor    = 0.3
)

// DefaultRerankConfig 读取重排配置（配置文件为准，其次环境变量，最后内置默认）
//
// 优先级（私域基线）：
//  1. config.yaml 的 rerank 段（为准）
//     - 宿主机：http://127.0.0.1:8209/v1（llama.cpp / MLX，端口 8209）
//  2. 环境变量（RERANK_*）仅在配置文件未指定时回退读取
//  3. 内置默认：http://127.0.0.1:8209/v1（仅当配置与环境都缺失时生效）
//
// 必须以配置文件为准，否则被错误的服务名带偏（宿主机无法解析容器服务名）。
func DefaultRerankConfig() *RerankConfig {
	fileCfg := config.GetAppConfig().Inference.Rerank
	baseURL := fileCfg.BaseURL
	model := fileCfg.Model
	apiKey := fileCfg.APIKey
	enabled := true
	if baseURL != "" && !fileCfg.Enabled {
		enabled = false
	}

	if baseURL == "" {
		baseURL = os.Getenv("RERANK_BASE_URL")
	}
	if model == "" {
		model = os.Getenv("RERANK_MODEL")
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("RERANK_ENABLED"))); v == "false" || v == "0" || v == "no" {
		enabled = false
	}

	if baseURL == "" {
		baseURL = config.DefaultRerankBaseURLDocker
	}
	if model == "" {
		model = config.DefaultRerankModel()
	}
	timeout := 30
	if v := os.Getenv("RERANK_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	if apiKey == "" {
		apiKey = os.Getenv("RERANK_API_KEY")
	}
	return &RerankConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Model:      model,
		Enabled:    enabled,
		Timeout:    timeout,
		MaxRetries: 2,
	}
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankResponse struct {
	Model   string `json:"model"`
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Rerank 调用本地推理服务（TEI 或 llama.cpp）的 /rerank 对候选文档重排
func (r *LocalReranker) Rerank(ctx context.Context, query string, docs []RerankDoc) ([]RerankResult, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	cfg := r.cfg
	if cfg == nil {
		cfg = DefaultRerankConfig()
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("rerank 未启用（RERANK_ENABLED=false）")
	}

	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Content
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") && !strings.HasSuffix(baseURL, "/v1/") {
		baseURL = baseURL + "/v1"
	}
	endpoint := baseURL + "/rerank"
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		res, err := r.callOnce(ctx, endpoint, timeout, cfg, query, texts)
		if err == nil {
			return mapRerankResults(docs, res), nil
		}
		lastErr = err
		logger.Errorf("[Rerank] 本地重排服务调用失败 (attempt %d/%d): %v", attempt+1, maxRetries, err)
	}
	return nil, fmt.Errorf("本地重排服务不可达（%s），已重试 %d 次: %w", endpoint, maxRetries, lastErr)
}

func (r *LocalReranker) callOnce(ctx context.Context, endpoint string, timeout time.Duration, cfg *RerankConfig, query string, texts []string) (*rerankResponse, error) {
	body, err := json.Marshal(rerankRequest{Model: cfg.Model, Query: query, Documents: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API error status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var rr rerankResponse
	if err := json.Unmarshal(respBody, &rr); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w (body=%s)", err, string(respBody))
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("rerank API error: %s", rr.Error.Message)
	}
	return &rr, nil
}

// normalizeRerankScores 把重排分归一到 0~1 概率域。
//
// llama.cpp 的 --reranking 端点返回 cross-encoder 的原始 logit（实测 -9.2 ~ +4.5），
// TEI / 云端返回的是 sigmoid 之后的概率。字段同名、量纲不同：不归一就直接套
// rerankScoreFloor=0.3，llama.cpp 下几乎整批候选都「低于阈值」，命中知识被误判为
// 知识不足并降级回 RRF 顺序，重排等于没生效。
// 仅当出现越界分（<0 或 >1）时才整体做 sigmoid，概率域响应保持原样不二次变换。
func normalizeRerankScores(scores []float64) []float64 {
	outOfRange := false
	for _, s := range scores {
		if s < 0 || s > 1 {
			outOfRange = true
			break
		}
	}
	if !outOfRange {
		return scores
	}
	out := make([]float64, len(scores))
	for i, s := range scores {
		out[i] = 1 / (1 + math.Exp(-s))
	}
	return out
}

func mapRerankResults(docs []RerankDoc, rr *rerankResponse) []RerankResult {
	type scored struct {
		id    string
		score float64
	}
	raw := make([]float64, len(rr.Results))
	for i, item := range rr.Results {
		raw[i] = item.RelevanceScore
	}
	normalized := normalizeRerankScores(raw)

	scoredMap := make(map[int]float64, len(rr.Results))
	for i, item := range rr.Results {
		scoredMap[item.Index] = normalized[i]
	}

	results := make([]scored, 0, len(docs))
	for i, d := range docs {
		score := scoredMap[i]
		if score >= rerankScoreFloor {
			results = append(results, scored{id: d.ID, score: score})
		}
	}
	sort.SliceStable(results, func(a, b int) bool {
		return results[a].score > results[b].score
	})

	out := make([]RerankResult, 0, len(results))
	for _, s := range results {
		out = append(out, RerankResult{ID: s.id, Score: s.score})
	}
	return out
}

// SetReranker 为检索服务装配重排器（可选；nil 时跳过重排）
func (r *RagRetrievalServiceImpl) SetReranker(reranker RerankerInterface) {
	r.reranker = reranker
}

func toRerankDocs(chunks []Chunk) []RerankDoc {
	docs := make([]RerankDoc, 0, len(chunks))
	for _, c := range chunks {
		docs = append(docs, RerankDoc{ID: c.ID, Content: c.Content})
	}
	return docs
}

func applyRerank(chunks []Chunk, results []RerankResult) []Chunk {
	byID := make(map[string]Chunk, len(chunks))
	for _, c := range chunks {
		byID[c.ID] = c
	}
	ordered := make([]Chunk, 0, len(chunks))
	seen := make(map[string]bool, len(results))
	for _, res := range results {
		if c, ok := byID[res.ID]; ok {
			c.Score = res.Score
			ordered = append(ordered, c)
			seen[res.ID] = true
		}
	}
	for _, c := range chunks {
		if !seen[c.ID] {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
