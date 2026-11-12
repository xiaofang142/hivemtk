package ragretrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	llmpkg "hivemtk-user/internal/aiagent/llm"
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
	// Protocol 重排协议：空 = 按 base_url 特征推断（InferRerankProtocol）；
	// "openai" = OpenAI 兼容（TEI/llama.cpp 单数 /rerank；DashScope 兼容模式复数 /reranks）；
	// "dashscope" = DashScope 原生 text-rerank 端点（BaseURL 即完整端点路径，
	//   裸域名/缺 /rerank 时由 NormalizeRerankConfig 自动补全，不做 /v1 补齐）；
	// "tei" = TEI 原生协议（POST {base}/rerank，请求 {query,texts,truncate}，
	//   响应 [{index,score}]；用于公开 TEI Space 等非 OpenAI 兼容端点）
	Protocol string
}

// LocalReranker 基于本地推理服务的重排实现（OpenAI 兼容 /v1/rerank）
//
// 默认部署形态（修订）：rerank 与 embedding 同走本地推理服务（llama.cpp），
// 使用 bge-reranker-v2-m3（跨编码器）。
//
// 端点约定（按 base_url 特征分三类，见 resolveRerankEndpoint / NormalizeRerankConfig）：
//   - TEI / llama.cpp / 自建 OpenAI 代理（非阿里云域名）：
//     BaseURL=http://host:port    → POST {BaseURL}/v1/rerank（自动补 /v1，既有行为不变）
//     BaseURL=http://host:port/v1 → POST {BaseURL}/rerank
//   - DashScope/MaaS OpenAI 兼容（path 含 compatible-mode / compatible-api）：
//     POST {BaseURL}/reranks（复数，官方兼容端点，仅 qwen3-rerank）
//   - DashScope/MaaS 原生（裸域名或已带完整端点）：
//     POST {origin}/api/v1/services/rerank/text-rerank/text-rerank（协议 dashscope）
//   - TEI 原生（协议 tei，显式配置）：
//     BaseURL=http://host:port    → POST {BaseURL}/rerank
//     BaseURL=http://host:port/rerank → 原样（不补 /v1）
//
// 代码层会自动补齐 /v1（与 embedding_service.go 一致），config.yaml 中 base_url
// 无论是否带 /v1 后缀都能正确路由；协议误配由 NormalizeRerankConfig 启动期自愈 + WARN。
// 响应字段一致：results[].index / results[].relevance_score，但量纲不同：
// TEI/云端已做 sigmoid（0~1），llama.cpp 直接回原始 logit（可为负、可 >1），
// 由 normalizeRerankScores 归一回概率域后再套用 rerankScoreFloor。
type LocalReranker struct {
	httpClient *http.Client
	cfg        *RerankConfig
}

var sharedRerankTransport = &http.Transport{
	// Proxy: http.ProxyFromEnvironment —— 同 sharedEmbeddingTransport，云端 rerank 经系统代理出网。
	Proxy:               http.ProxyFromEnvironment,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

// NewLocalReranker 构造本地重排器
//
// 超时取 DefaultRerankConfig().Timeout（RERANK_TIMEOUT / 配置文件），
// 不能硬编码 30s——否则 Rerank() 里 cfg.Timeout 的 ctx 超时会被 client 30s 先掐断，
// RERANK_TIMEOUT 配 60s 形同虚设（P2-1）。
func NewLocalReranker() *LocalReranker {
	return &LocalReranker{httpClient: newRerankHTTPClient(timeoutSeconds(DefaultRerankConfig()))}
}

// NewLocalRerankerWithConfig 构造带 per 知识库配置的重排器
// （Rerank 直接使用 cfg，使重排走指定云端 rerank 端点）
//
// 超时关系（P2-1「RERANK_TIMEOUT 配 60s 无效」根因）：
// callOnce 每次尝试用 ctx.WithTimeout(cfg.Timeout) 控制单次请求，
// 而 http.Client.Timeout 是整个请求（含读响应体）的硬上限——硬编码 30s 就会，
// cfg.Timeout=60 时 ctx 还没到期 client 已经掐断，配置形同虚设。
// 故这里取 max(cfg.Timeout, 30s)：配置更大时放行，配置更小时仍由 ctx 生效（不放大默认值）。
func NewLocalRerankerWithConfig(cfg *RerankConfig) *LocalReranker {
	if cfg != nil {
		// 装配期静默自愈（warn=false）：per 知识库覆盖每次查询都会构造一次，避免日志刷屏
		NormalizeRerankConfig(cfg, false)
	}
	return &LocalReranker{
		httpClient: newRerankHTTPClient(timeoutSeconds(cfg)),
		cfg:        cfg,
	}
}

// minRerankHTTPClientTimeout http.Client.Timeout 下限：
// 未配置（cfg.Timeout<=0）时保持历史默认 30s；配置更大时取配置值。
const minRerankHTTPClientTimeout = 30 * time.Second

// timeoutSeconds 取 cfg.Timeout（秒），缺省/非法回落 30
func timeoutSeconds(cfg *RerankConfig) int {
	if cfg == nil || cfg.Timeout <= 0 {
		return 30
	}
	return cfg.Timeout
}

// newRerankHTTPClient 构造重排 HTTP 客户端：Timeout = max(cfg.Timeout, 30s)
func newRerankHTTPClient(timeoutSec int) *http.Client {
	t := time.Duration(timeoutSec) * time.Second
	if t < minRerankHTTPClientTimeout {
		t = minRerankHTTPClientTimeout
	}
	return &http.Client{Transport: sharedRerankTransport, Timeout: t}
}

// rerank 模型支持表：
//   - bge-reranker-base       轻量基线，CPU 可跑，单文档 <50ms
//   - bge-reranker-large      高精度，GPU 推荐，单文档 <200ms
//   - bge-reranker-v2-m3      多语种，默认推荐
//   - bge-reranker-v2-gemma   多语种高精（自托管，>2B 参数）
//
// 通过 RERANK_MODEL 环境变量或 config.yaml 切换；BaseURL 不变。
const (
	rerankScoreFloor = 0.3
	// teiProtocol 标识 TEI 原生 rerank 协议（请求 query/texts，响应 [{index,score}]）
	teiProtocol = "tei"
)

// DefaultRerankConfig 读取重排配置（配置文件为准，其次环境变量，最后内置默认）
//
// 优先级：
//  1. config.yaml 的 rerank 段（为准）
//     - 宿主机：http://127.0.0.1:8209/v1（llama.cpp / MLX，端口 8209）
//  2. 参数中心 rerank.global_override（管理端在线改，本函数每次读源；
//     enabled=true 且 base_url 非空时顶掉 config.yaml 那一层）
//  3. 内置默认：http://127.0.0.1:8209/v1（仅当配置未指定时生效）
//
// env 层已移除：SaaS 兼容要求 rerank API 连接信息禁止走环境变量，全部数据库驱动。
// 必须以配置文件为准，否则被错误的服务名带偏（宿主机无法解析容器服务名）。
func DefaultRerankConfig() *RerankConfig {
	fileCfg := config.GetAppConfig().Inference.Rerank
	baseURL := fileCfg.BaseURL
	model := fileCfg.Model
	apiKey := fileCfg.APIKey
	protocol := fileCfg.Protocol
	enabled := true
	if baseURL != "" && !fileCfg.Enabled {
		enabled = false
	}

	// 全局 rerank 手动配置（参数中心 rerank.global_override）：enabled=true
	// 且 base_url 非空才生效；生效时允许把 enabled 拉回 true（显式开启云端档）。
	if o := llmpkg.GetGlobalRerankOverride(); o != nil && o.Enabled && strings.TrimSpace(o.BaseURL) != "" {
		baseURL = strings.TrimSpace(o.BaseURL)
		model = strings.TrimSpace(o.Model)
		apiKey = o.APIKey
		if p := strings.ToLower(strings.TrimSpace(o.Protocol)); p != "" {
			protocol = p
		}
		enabled = true
		logger.Infof("[rerank] 使用全局手动配置 base_url=%s model=%s protocol=%s", baseURL, model, protocol)
	}

	if baseURL == "" {
		baseURL = config.DefaultRerankBaseURLDocker
	}
	if model == "" {
		model = config.DefaultRerankModel()
	}
	timeout := 30
	cfg := &RerankConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Model:      model,
		Enabled:    enabled,
		Timeout:    timeout,
		MaxRetries: 2,
		Protocol:   protocol,
	}
	// 启动期/装配期协议校验与自愈（warn=true 打 WARN 日志），详见 NormalizeRerankConfig
	NormalizeRerankConfig(cfg, true)
	logRerankConfigSummary(cfg)
	return cfg
}

// ---------------------------------------------------------------------------
// 端点/协议推断与自愈（P1-4：rerank 协议误配无防护 + 兼容模式端点错）
//
// 官方文档核实结论（2026-10 核对 help.aliyun.com）：
//   - DashScope 原生（所有 rerank 模型，含 qwen3.7-text-rerank / qwen3-vl-rerank / gte-rerank-v2）：
//       POST {host}/api/v1/services/rerank/text-rerank/text-rerank
//       请求体 {"model","input":{"query","documents"},"parameters":{...}}
//       响应体 {"output":{"results":[...]}, "code","message"}
//       来源 https://help.aliyun.com/zh/model-studio/rerank 「DashScope」段 curl 示例
//   - DashScope OpenAI 兼容（仅 qwen3-rerank）：
//       POST {WorkspaceId}.{region}.maas.aliyuncs.com/compatible-api/v1/reranks  ←— 复数 reranks
//       请求体 {"model","query","documents","top_n"}；响应顶层 {"object":"list","results":[...]}
//       来源 https://help.aliyun.com/zh/model-studio/rerank 「OpenAI 兼容」段 curl 示例
//   - 注意：/compatible-mode/v1 是 OpenAI 兼容 chat 的 BASE_URL 前缀，不是 rerank 端点前缀；
//     经典域名 dashscope.aliyuncs.com 上 /compatible-mode/v1/reranks 实测 404。
//     因此本文件按 base_url 特征分三类处理，而不是全局改单复数。
// ---------------------------------------------------------------------------

const (
	// dashscopeNativeRerankPath DashScope 原生 rerank 端点路径（官方）
	dashscopeNativeRerankPath = "/api/v1/services/rerank/text-rerank/text-rerank"
	// dashscopeCompatRerankSuffix DashScope OpenAI 兼容 rerank 端点后缀（官方为复数 reranks）
	dashscopeCompatRerankSuffix = "/reranks"
)

// rerankBasePath 解析 base_url 的 path（去尾部 /）；解析失败返回 ""
func rerankBasePath(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

// rerankBaseOrigin 解析 base_url 的 scheme://host；解析失败（或无 host）回落原串
func rerankBaseOrigin(base string) string {
	b := strings.TrimSpace(base)
	u, err := url.Parse(b)
	if err != nil || u.Host == "" {
		return strings.TrimRight(b, "/")
	}
	return u.Scheme + "://" + u.Host
}

// isClassicDashscopeHost 经典公网域名 dashscope.aliyuncs.com（非工作空间域名）
func isClassicDashscopeHost(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	return strings.ToLower(u.Hostname()) == "dashscope.aliyuncs.com"
}

// isDashscopeHost base 是否指向阿里云 DashScope / MaaS 域名
func isDashscopeHost(base string) bool {
	if isClassicDashscopeHost(base) {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "maas.aliyuncs.com" || strings.HasSuffix(h, ".maas.aliyuncs.com")
}

// isDashscopeCompatBase base 是否已带 DashScope OpenAI 兼容前缀（compatible-mode / compatible-api）
func isDashscopeCompatBase(base string) bool {
	p := rerankBasePath(base)
	return strings.Contains(p, "compatible-mode") || strings.Contains(p, "compatible-api")
}

// completeDashscopeNativeEndpoint 把 DashScope/MaaS base 补全为原生 rerank 完整端点（幂等）：
//   - 已含 /services/rerank/ → 原样
//   - 以 /api/v1 结尾 → 追加 /services/rerank/text-rerank/text-rerank
//   - 裸域名或其它路径（含 compatible-mode/v1）→ 整段重置为 {origin}/api/v1/services/rerank/text-rerank/text-rerank
func completeDashscopeNativeEndpoint(base string) string {
	origin := rerankBaseOrigin(base)
	p := rerankBasePath(base)
	switch {
	case strings.Contains(p, "/services/rerank/"):
		return strings.TrimRight(base, "/")
	case strings.HasSuffix(p, "/api/v1"):
		return origin + p + "/services/rerank/text-rerank/text-rerank"
	default:
		return origin + dashscopeNativeRerankPath
	}
}

// InferRerankProtocol 按 base_url 特征推断 rerank 协议（Protocol 为空时使用）：
//   - path 已含 /services/rerank/ → dashscope（原生完整端点）
//   - 经典域名 dashscope.aliyuncs.com → dashscope（该域名无可用兼容 rerank 端点）
//   - 工作空间 MaaS 域名但未带 compatible-*/ 前缀（裸域名）→ dashscope（裸域名自愈）
//   - 其余（TEI / llama.cpp / vLLM / 自建 OpenAI 代理 / 已带 compatible-* 前缀）→ openai
//
// 供 per 知识库覆盖结构（rag_products.rerank_provider_config.protocol 为空）复用。
func InferRerankProtocol(base string) string {
	if strings.Contains(rerankBasePath(base), "/services/rerank/") {
		return dashscopeProtocol
	}
	if isClassicDashscopeHost(base) {
		return dashscopeProtocol
	}
	if isDashscopeHost(base) && !isDashscopeCompatBase(base) {
		return dashscopeProtocol
	}
	return "openai"
}

// NormalizeRerankConfig 协议/端点校验 + 自愈（幂等，原地修改 cfg 并返回）。
//
// 三类 base_url 的最终行为（与 resolveRerankEndpoint 一致）：
//  1. TEI / llama.cpp / vLLM / 自建 OpenAI 代理（非阿里云域名）：protocol=openai（默认），
//     端点 = base（既有行为：自动补 /v1）+ 单数 /rerank —— 老用户不受影响。
//  2. DashScope/MaaS OpenAI 兼容（path 含 compatible-mode 或 compatible-api，且非经典域名）：
//     protocol=openai，端点 = base + 复数 /reranks（官方兼容端点）。
//  3. DashScope/MaaS 原生（裸域名、/api/v1、或已带 /api/v1/services/rerank/...）：
//     protocol=dashscope，端点 = {origin}/api/v1/services/rerank/text-rerank/text-rerank。
//  4. TEI 原生（显式 protocol=tei）：端点 = base + /rerank（已带 /rerank 则原样），不补 /v1。
//
// 自愈动作：
//   - protocol 为空 → 按 InferRerankProtocol(base) 推断
//   - protocol=dashscope 且 base 不是完整原生端点（path 为空或不含 /rerank）→ 补全默认端点 + WARN
//   - protocol=openai 且 base 指向 DashScope/MaaS（经典域名任意 path、工作空间域名裸 path）
//     → WARN「DashScope 兼容模式应设 RERANK_PROTOCOL=dashscope」并直接自愈改走 dashscope 分支
//
// warn=true 用于启动/配置读取路径；warn=false 用于 per 知识库装配路径（避免每次查询刷日志）。
func NormalizeRerankConfig(cfg *RerankConfig, warn bool) *RerankConfig {
	if cfg == nil {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.BaseURL = base
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = InferRerankProtocol(base)
		cfg.Protocol = protocol
	}

	if isTeiProtocol(protocol) {
		return cfg
	}

	if isDashscopeProtocol(protocol) {
		// P1-4①：裸域名（path 为空或不包含 /rerank）自动补全原生端点
		if p := rerankBasePath(base); !strings.Contains(p, "/rerank") {
			fixed := completeDashscopeNativeEndpoint(base)
			if warn {
				warnRerankOnce("[Rerank] protocol=dashscope 但 base_url 缺少原生 rerank 端点，已自动补全: %s -> %s", base, fixed)
			}
			cfg.BaseURL = fixed
		}
		return cfg
	}

	// protocol=openai 却指向 DashScope/MaaS：兼容模式端点极易误配（404）
	if isDashscopeHost(base) && (isClassicDashscopeHost(base) || !isDashscopeCompatBase(base)) {
		fixed := completeDashscopeNativeEndpoint(base)
		if warn {
			warnRerankOnce("[Rerank] base_url 指向 DashScope 却用 protocol=%q（%s）；DashScope 兼容模式应设 RERANK_PROTOCOL=dashscope，已自愈改走原生分支: %s -> %s",
				protocol, rerankBaseOrigin(base), base, fixed)
		}
		cfg.Protocol = dashscopeProtocol
		cfg.BaseURL = fixed
	}
	return cfg
}

// rerankWarnOnce 同一条自愈 WARN 只打一次
// （DefaultRerankConfig 在部分路径上每次查询都会调用，去重避免日志刷屏）
var rerankWarnOnce sync.Map

func warnRerankOnce(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if _, loaded := rerankWarnOnce.LoadOrStore(msg, struct{}{}); loaded {
		return
	}
	logger.Warnf("%s", msg)
}

// resolveRerankEndpoint 依 base_url 特征解析最终 POST 端点（与 NormalizeRerankConfig 四类 base 对应，幂等）
func resolveRerankEndpoint(cfg *RerankConfig) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")

	// ④ TEI 原生：base 可为裸 host 或已带 /rerank 的完整端点，不补 /v1
	if isTeiProtocol(cfg.Protocol) {
		if strings.HasSuffix(rerankBasePath(base), "/rerank") {
			return base
		}
		return base + "/rerank"
	}

	// ③ DashScope 原生：base 即完整端点（NormalizeRerankConfig 已补全，这里再兜底一次）
	if isDashscopeProtocol(cfg.Protocol) {
		if !strings.Contains(rerankBasePath(base), "/rerank") {
			base = completeDashscopeNativeEndpoint(base)
		}
		return base
	}

	// ② DashScope/MaaS OpenAI 兼容：官方端点为复数 /reranks（compatible-api/v1/reranks）；
	//    经典域名 dashscope.aliyuncs.com 的 compatible-*/reranks 实测 404，直接回退原生端点。
	if isDashscopeHost(base) {
		if isClassicDashscopeHost(base) || !isDashscopeCompatBase(base) {
			return completeDashscopeNativeEndpoint(base)
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base + dashscopeCompatRerankSuffix
	}

	// ① TEI / llama.cpp / 自建 OpenAI 代理：保持既有行为 —— 自动补 /v1 后拼单数 /rerank
	if !strings.HasSuffix(base, "/v1") && !strings.HasSuffix(base, "/v1/") {
		base += "/v1"
	}
	return base + "/rerank"
}

// rerankConfigSummaryOnce 启动期配置摘要只打一次（DefaultRerankConfig 可能被每次查询调用）
var rerankConfigSummaryOnce sync.Once

// logRerankConfigSummary 打一行 rerank 最终配置摘要（protocol + base + model，key 打码）
func logRerankConfigSummary(cfg *RerankConfig) {
	rerankConfigSummaryOnce.Do(func() {
		logger.Infof("[Rerank] 配置摘要: protocol=%s base=%s model=%s key=%s enabled=%v timeout=%ds retries=%d",
			cfg.Protocol, cfg.BaseURL, cfg.Model, maskRerankKey(cfg.APIKey), cfg.Enabled, cfg.Timeout, cfg.MaxRetries)
	})
}

// maskRerankKey 打码 API Key：只留前 6 后 4
func maskRerankKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "(empty)"
	}
	if len(k) <= 12 {
		return "***"
	}
	return k[:6] + "***" + k[len(k)-4:]
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

// dashscopeProtocol 标识 DashScope 原生 text-rerank 协议（qwen3-rerank 等）
const dashscopeProtocol = "dashscope"

// isDashscopeProtocol 判断是否走 DashScope 原生协议（空 / openai 均为 OpenAI 兼容现状）
func isDashscopeProtocol(protocol string) bool {
	return strings.ToLower(strings.TrimSpace(protocol)) == dashscopeProtocol
}

// isTeiProtocol 判断是否走 TEI 原生协议（请求 query/texts，响应 [{index,score}]）
func isTeiProtocol(protocol string) bool {
	return strings.ToLower(strings.TrimSpace(protocol)) == teiProtocol
}

// teiRerankRequest TEI 原生请求体：
// POST {base_url}/rerank
// {"query":"...","texts":["..."],"truncate":true}
//
// TEI 的 request schema 不要求 model 字段；多传未知字段可能被严格反序列化拒绝，故不发送。
type teiRerankRequest struct {
	Query    string   `json:"query"`
	Texts    []string `json:"texts"`
	Truncate bool     `json:"truncate"`
}

// teiRerankResult TEI 原生单条结果：
// [{"index":0,"score":0.98},{"index":1,"score":0.01}]
type teiRerankResult struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// toRerankResponseFromTEI 把 TEI 原生数组转换成既有 rerankResponse 形状，
// 使 mapRerankResults / normalizeRerankScores / rerankScoreFloor 等下游逻辑完全复用。
// TEI 分数为 sigmoid 后的 0~1 概率域，normalizeRerankScores 会原样通过。
func toRerankResponseFromTEI(model string, items []teiRerankResult) *rerankResponse {
	rr := &rerankResponse{Model: model}
	if len(items) == 0 {
		return rr
	}
	rr.Results = make([]struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	}, len(items))
	for i, it := range items {
		rr.Results[i].Index = it.Index
		rr.Results[i].RelevanceScore = it.Score
	}
	return rr
}

// dashscopeRequest DashScope 原生请求体：
// POST {base_url}/api/v1/services/rerank/text-rerank/text-rerank
// {"model":"...","input":{"query":"...","documents":[...]},"parameters":{"return_documents":false}}
type dashscopeRequest struct {
	Model      string              `json:"model"`
	Input      dashscopeInput      `json:"input"`
	Parameters dashscopeParameters `json:"parameters"`
}

type dashscopeInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type dashscopeParameters struct {
	ReturnDocuments bool `json:"return_documents"`
}

// dashscopeResponse DashScope 原生响应体：
// {"output":{"results":[{"index":0,"relevance_score":0.98}]},"code":"","message":""}
type dashscopeResponse struct {
	Output struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	} `json:"output"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// toRerankResponse 把 DashScope 原生响应转换成既有 rerankResponse 形状，
// 使 mapRerankResults / normalizeRerankScores / rerankScoreFloor 等下游逻辑完全复用。
// DashScope 分数为 0~1 概率域，normalizeRerankScores 会原样通过（无需特判）。
//
// 注：Results 元素是与 rerankResponse 中完全相同的匿名结构体
// （字段名/类型/顺序/json tag 一致），类型相同故可直接赋值。
func (ds *dashscopeResponse) toRerankResponse(model string) *rerankResponse {
	return &rerankResponse{
		Model:   model,
		Results: ds.Output.Results,
	}
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

	// 端点解析按 base 特征分三类（TEI/llama.cpp 单数 /rerank、DashScope 兼容复数 /reranks、
	// DashScope 原生完整端点），详见 resolveRerankEndpoint / NormalizeRerankConfig 注释
	endpoint := resolveRerankEndpoint(cfg)
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
		// P2-2：4xx（除 429）是请求本身的问题（400 参数 / 401 鉴权 / 404 端点错），
		// 退避重试只会放大延迟与日志噪音，直接返回错误。
		if !isRetryableRerankErr(err) {
			logger.Errorf("[Rerank] 4xx 客户端错误不重试: %v", err)
			return nil, fmt.Errorf("本地重排服务调用失败（%s）: %w", endpoint, err)
		}
		logger.Errorf("[Rerank] 本地重排服务调用失败 (attempt %d/%d): %v", attempt+1, maxRetries, err)
	}
	return nil, fmt.Errorf("本地重排服务不可达（%s），已重试 %d 次: %w", endpoint, maxRetries, lastErr)
}

// rerankStatusError 携带 HTTP 状态码的重排调用错误（用于 4xx 判定不重试）
type rerankStatusError struct {
	StatusCode int
	Body       string
}

func (e *rerankStatusError) Error() string {
	return fmt.Sprintf("rerank API error status=%d body=%s", e.StatusCode, e.Body)
}

// isRetryableRerankErr 重试判定：
//   - 无状态码（网络错误/超时/编解码）→ 可重试
//   - 5xx、429（限流）→ 可重试
//   - 其余 4xx（400/401/403/404 等）→ 不可重试
func isRetryableRerankErr(err error) bool {
	var se *rerankStatusError
	if errors.As(err, &se) {
		if se.StatusCode >= 400 && se.StatusCode < 500 && se.StatusCode != http.StatusTooManyRequests {
			return false
		}
	}
	return true
}

func (r *LocalReranker) callOnce(ctx context.Context, endpoint string, timeout time.Duration, cfg *RerankConfig, query string, texts []string) (*rerankResponse, error) {
	tei := isTeiProtocol(cfg.Protocol)
	dashscope := isDashscopeProtocol(cfg.Protocol)
	var body []byte
	var err error
	switch {
	case tei:
		body, err = json.Marshal(teiRerankRequest{Query: query, Texts: texts, Truncate: true})
	case dashscope:
		body, err = json.Marshal(dashscopeRequest{
			Model:      cfg.Model,
			Input:      dashscopeInput{Query: query, Documents: texts},
			Parameters: dashscopeParameters{ReturnDocuments: false},
		})
	default:
		body, err = json.Marshal(rerankRequest{Model: cfg.Model, Query: query, Documents: texts})
	}
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
	// 鉴权三种协议一致：Authorization: Bearer <key>
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
		return nil, &rerankStatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if tei {
		var items []teiRerankResult
		if err := json.Unmarshal(respBody, &items); err != nil {
			return nil, fmt.Errorf("decode TEI rerank response: %w (body=%s)", err, string(respBody))
		}
		return toRerankResponseFromTEI(cfg.Model, items), nil
	}

	if dashscope {
		var ds dashscopeResponse
		if err := json.Unmarshal(respBody, &ds); err != nil {
			return nil, fmt.Errorf("decode dashscope rerank response: %w (body=%s)", err, string(respBody))
		}
		if strings.TrimSpace(ds.Code) != "" {
			return nil, fmt.Errorf("dashscope rerank API error code=%s message=%s", ds.Code, ds.Message)
		}
		return ds.toRerankResponse(cfg.Model), nil
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
	if len(results) == 0 && len(docs) > 0 {
		logger.Warnf("[rerank] 整批 %d 候选均低于 scoreFloor=%.2f，疑似模型/阈值不匹配或查询过短", len(docs), rerankScoreFloor)
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
