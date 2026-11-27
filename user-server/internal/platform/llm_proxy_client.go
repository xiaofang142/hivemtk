package platform

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
)

// LLMProxyClient 平台 LLM 代理客户端
//
// ============================================================
// 为什么是代理而不是直连
//
// 平台垫资的真实厂商 Key **永不下发**（plan.md ADR-M7）：下发等于把垫资凭据
// 交出去，商户可以直连厂商无限量用，平台既无法计量也无法止损。
// 所以调用链是 user-server → 平台代理 → 厂商，扣费由平台在转发时实时执行。
//
// ===== 零出域铁律的例外边界 =====
// 这是本项目「数据零出域」的唯一显式例外，成立条件（缺一不可）：
//  1. 商户在平台端开通了 llm-cloud 并买了套餐（= 显式同意）；
//  2. 代理地址是商户自己配置的（PLATFORM_API_URL），平台不主动外联；
//  3. 只有 source='platform' 的 provider 会走这条路；自带 Key 的 provider
//     一次都不会调到这里。
// ============================================================

// LLMProxyResponse 代理响应（含平台回传的真实用量）
type LLMProxyResponse struct {
	StatusCode          int
	Body                []byte
	ContentType         string
	PromptTokens        int64
	CompletionTokens    int64
	RequestID           string
	Provider            string
	CachedInputTokens   int64
	CacheCreationTokens int64
	OutcomeUnknown      bool
}

// ============================================================
// 请求签名（SaaS 审计 A19）
//
// 这条通道原先只发 `Authorization: Bearer <merchant_key>`，而 merchant_key 在整套
// HMAC 体系里是**标识符**（每次调用都以 X-Merchant-Key 明文出现在对 /merchant-api
// 的请求里，平台管理端列表 json:"key" 直出），真正的凭据是每商户 api_secret。
// 也就是说：知道一个 key 就能以那个商户的身份烧掉它的 LLM 余额，且没有时效约束
// —— 抓到一个请求可以永久重放。
//
// 口径与 /merchant-api 那边**刻意不同**（那边是 method\npath\nts\nbody，query 不进签名）：
//   - 这里必须签**含 query 的 RequestURI**：provider/scenario/tenant_code 全在 query 里
//     （body 必须是 OpenAI 协议原文），不签 query 就等于允许把 tenant_code 换掉，
//     账会记到同商户另一个租户头上；
//   - 签的是 body 的 **sha256** 而不是 body 本身（body 最大 8MB，验签不该与体积挂钩）；
//   - 带 nonce：同一秒内两次同 prompt 的调用是完全正常的（签名输入里没有随机量，
//     两次会算出同一个签名），平台侧若按签名去重就会把批量任务打成 401。
//
// 两侧口径必须**逐字一致**，由 TestLLMProxySignatureMatchesPlatformContract
// （本包）与平台侧 internal/middleware/merchant_llm_auth_test.go 双向钉住。
// ============================================================

const llmProxySignDomain = "llm-proxy-v1"

// LLMProxySignature 计算 `/llm-api/v1` 请求签名。
func LLMProxySignature(secret, method, requestURI, timestamp, nonce string, body []byte) string {
	sum := sha256.Sum256(body)
	payload := llmProxySignDomain + "\n" + method + "\n" + requestURI + "\n" + timestamp + "\n" + nonce + "\n" +
		hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// newLLMProxyNonce 生成一次性随机串（16 字节 hex）。
func newLLMProxyNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// llmProxyUnsignedWarn 只在第一次"无密钥可签"时告警：
// 每个请求都打一条会把日志刷成噪音，而这条信息只需要被看见一次。
var llmProxyUnsignedWarn sync.Once

// LLMProxyClient 平台模型代理客户端
type LLMProxyClient struct {
	baseURL string
	key     string
	client  *http.Client
}

// NewLLMProxyClient 构造代理客户端
func NewLLMProxyClient() *LLMProxyClient {
	return &LLMProxyClient{
		baseURL: strings.TrimRight(platformURLForLLM(), "/"),
		key:     merchantKeyForLLM(),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// SetLLMProxyClientForTest 测试注入（避免单测去读真实 env）
var SetLLMProxyClientForTest = func(baseURL, key string, c *http.Client) *LLMProxyClient {
	return &LLMProxyClient{baseURL: strings.TrimRight(baseURL, "/"), key: key, client: c}
}

func platformURLForLLM() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_API_URL")); v != "" {
		return v
	}
	// 复用 config.PlatformURL()：它是平台地址的唯一事实源
	// （已装配配置 > env > 空串），另起一份解析迟早与它漂移。
	return config.PlatformURL()
}

func merchantKeyForLLM() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_MERCHANT_KEY")); v != "" {
		return v
	}
	if GetMerchantKey() != "" {
		return GetMerchantKey()
	}
	return "local-dev-merchant"
}

// signLLMProxyRequest 给请求补 X-Timestamp / X-Nonce / X-Signature。
//
// 取不到签名密钥时**不阻断请求**：照发未签名版本，并在**第一次**打一条告警。
// 为什么不是直接报错：兼容窗口期（平台把 PLATFORM_LLM_PROXY_REQUIRE_SIGNATURE 关掉时）
// 未签名仍被接受；而无密钥的部署本来就调不通（Available() 已要求 key），
// 在这里 raise 只会把"平台说 401"变成"本地报错"，反而更难排查是谁没配。
// 反过来，静默降级也不行 —— 那就是 A13 那条"不静默降级"的口径。
func signLLMProxyRequest(req *http.Request, body []byte) error {
	secret := GetMerchantSecret()
	if secret == "" {
		llmProxyUnsignedWarn.Do(func() {
			logger.Warn("平台 LLM 代理请求**未签名**：本机没有商户 api_secret" +
				"（.merchant_api_secret / MERCHANT_API_SECRET / PlatformCfg.Secret 全空）。" +
				"平台开启验签后将一律 401，请先完成商户注册或配置密钥")
		})
		return nil
	}
	nonce, err := newLLMProxyNonce()
	if err != nil {
		return fmt.Errorf("生成请求 nonce 失败: %w", err)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// 签的是**实际发出去的** RequestURI（path + query，含 baseURL 里的路径前缀）——
	// 平台侧算的是它收到的 URL.RequestURI()，两侧必须对着同一个字符串。
	uri := req.URL.RequestURI()
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Nonce", nonce)
	if req.Header.Get(llmProxyContractHeader) != "" {
		contextHeaders := map[string]string{}
		for _, name := range llmProxyContextSignedHeaders {
			if value := req.Header.Get(name); value != "" {
				contextHeaders[name] = value
			}
		}
		sum := sha256.Sum256(body)
		req.Header.Set("X-Signature", LLMProxySignatureV2(secret, req.Method, uri,
			[]string{ts, nonce, hex.EncodeToString(sum[:])}, contextHeaders))
		return nil
	}
	req.Header.Set("X-Signature", LLMProxySignature(secret, req.Method, uri, ts, nonce, body))
	return nil
}

// Available 平台是否可用（未配置地址/密钥时返回 false，调用方据此把
// source='platform' 的 provider 降级为不可用，而不是发一次注定失败的请求）
func (c *LLMProxyClient) Available() bool {
	return c.baseURL != "" && c.key != ""
}

// ChatCompletions 转发一次对话补全
//
// 报文的 provider/scenario/trace 等上下文放在 **query** 而不是 body：
// body 是 OpenAI 协议原文，往里塞我们自己的字段会让不支持的厂商直接 400；
// 而 query 不进协议、也不会被厂商解析。
func (c *LLMProxyClient) ChatCompletions(ctx context.Context, provider string, body []byte, meta LLMCallMeta) (*LLMProxyResponse, error) {
	if !c.Available() {
		return nil, fmt.Errorf("平台未配置（PLATFORM_API_URL / PLATFORM_MERCHANT_KEY 缺失），无法调用平台模型")
	}
	if provider == "" {
		return nil, fmt.Errorf("provider 不能为空")
	}
	q := []string{"provider=" + urlQueryEscape(provider)}
	if meta.Scenario != "" {
		q = append(q, "scenario="+urlQueryEscape(meta.Scenario))
	}
	if meta.TraceID != "" {
		q = append(q, "trace_id="+urlQueryEscape(meta.TraceID))
	}
	if meta.AgentID != "" {
		q = append(q, "agent_id="+urlQueryEscape(meta.AgentID))
	}
	if meta.UserID != "" {
		q = append(q, "user_id="+urlQueryEscape(meta.UserID))
	}
	// tenant_code 同款风格（空串不上行，与 scenario/trace_id 一致）：平台按
	// (merchant_key, tenant_code) 每租户一行记账，取不到租户归属时空串照发、
	// 平台走旧行为兜底。
	if meta.TenantCode != "" {
		q = append(q, "tenant_code="+urlQueryEscape(meta.TenantCode))
	}
	target := c.baseURL + "/llm-api/v1/chat/completions?" + strings.Join(q, "&")

	req, err := newPlatformRequest(ctx, http.MethodPost, target, body)
	if err != nil {
		return nil, fmt.Errorf("构造平台代理请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Bearer 保留：它让**新客户端**仍能对着**旧平台**（只认 Bearer 的那一版）工作，
	// 于是升级顺序是自由的 —— 这正是"A19 必须能与 user-server 同版本发布"的落地形态。
	// 身份本身不再由它决定：平台侧验签通过后用的是签名覆盖的 X-Merchant-Key。
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("X-Merchant-Key", c.key)
	if meta.TraceID != "" {
		req.Header.Set("X-Trace-Id", meta.TraceID)
	}
	for name, value := range buildLLMProxyContextHeaders(meta) {
		req.Header.Set(name, value)
	}
	if err := signLLMProxyRequest(req, body); err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用平台代理失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取平台代理响应失败: %w", err)
	}
	// 契约版本裁决（SaaS 审计 A4）：426 ⇒ 记进管理端可见的状态。
	// 这条通道尤其要紧 —— 它是最先被平台升级打到的那条（`/llm-api/v1` 同时挂验签闸
	// 与契约闸），失败表现是"模型调用全部报错"，不看这里就只会去查厂商 key。
	noteContractVerdict("/llm-api/v1/", resp.StatusCode, raw)

	out := &LLMProxyResponse{
		StatusCode:  resp.StatusCode,
		Body:        raw,
		ContentType: resp.Header.Get("Content-Type"),
		RequestID:   resp.Header.Get("X-Llm-Usage-Request-Id"),
		Provider:    resp.Header.Get("X-Llm-Provider"),
	}
	out.PromptTokens = parseIntHeader(resp.Header.Get("X-Llm-Usage-Prompt-Tokens"))
	out.CompletionTokens = parseIntHeader(resp.Header.Get("X-Llm-Usage-Completion-Tokens"))
	out.CachedInputTokens, out.CacheCreationTokens = parseLLMProxyCachedUsage(resp.Header)
	out.OutcomeUnknown = isLLMProxyOutcomeUnknown(resp)
	return out, nil
}

// LLMCallMeta 一次调用的业务上下文（用于计量归因）
type LLMCallMeta struct {
	Scenario string
	TraceID  string
	AgentID  string
	UserID   string
	// TenantCode 当前租户短码（平台端按 (merchant_key, tenant_code) 每租户
	// 一行记账）。空串 = 取不到租户归属（后台任务无绑定 / off 模式无租户行），
	// 此时 query 不上行该参数、平台按旧行为兜底 —— 取不到 code 绝不允许报错。
	TenantCode string
	// 以下是 llm-context-v2 的上下文头输入；canonical bus key 只用于本地派生 opaque header。
	ContractVersion     string
	OpaqueSessionKey    string
	ExecutionScenarioID string
	IdempotencyKey      string
}

func parseIntHeader(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}

// ParseChatCompletionUsage 从 OpenAI 兼容响应里取 usage（本地路径复用）。
//
// 与平台端 llm_proxy_service.parseUsage 同形：报文合法但 usage 全 0 时
// **不能**直接返回 0 —— 那会让「厂商省略 usage」的响应把计量悄悄变成 0，
// 商户用了算力却一直没扣费。
func ParseChatCompletionUsage(body []byte) (promptTokens, completionTokens int64) {
	var out struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &out); err == nil {
		in, comp := out.Usage.PromptTokens, out.Usage.CompletionTokens
		if in <= 0 && comp <= 0 && out.Usage.TotalTokens > 0 {
			in = out.Usage.TotalTokens / 2
			comp = out.Usage.TotalTokens - in
		}
		if in > 0 || comp > 0 {
			return in, comp
		}
	}
	return 0, 0
}
