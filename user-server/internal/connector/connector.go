// Package connector TB-06b：通用三方连接器层（L4）。
//
// 一句话职责：任意三方平台（电商/ERP/快递聚合/自定义 API）以「方言插件 + preset
// 数据」注册接入，桥接为工具层可调用能力——新平台=注册接入而非写业务代码。
//
// 方言插件接口（最小面，防过度抽象）：
//   - BuildRequest：鉴权形态→请求变形（header/query/签名）
//   - NormalizeResponse：平台响应→统一 JSON 信封
//
// 能力标签（capability）：连接器声明自己能做什么（query_express/modify_order/
// query_order/...），意图绑定层按能力路由而非按平台名 if——平台差异=数据声明。
//
// 证据级纪律（v5 二次审核）：未取证的平台能力标【预留】，v1 执行通路对未接凭证
// 的 direct_api/via_erp 显式拒绝，不承诺未验证能力。
package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/repository"
)

// ---------- 方言插件接口 ----------

// Dialect 三方平台方言插件（TB-06b）。
// BuildRequest 把连接器配置+调用参数变成出站请求（鉴权/签名在方言内完成）；
// NormalizeResponse 把平台原始响应转成统一 JSON 信封（错误时返回平台原文+错误）。
type Dialect interface {
	ID() string
	BuildRequest(ctx context.Context, cfg ConnectorConfig, endpoint EndpointConfig, args map[string]any) (*http.Request, error)
	NormalizeResponse(body []byte, statusCode int) (map[string]any, error)
}

// ConnectorConfig 连接器配置（preset 数据；凭证字段读写时经 secrets 加解密——
// v1 进程内明文持有，落库加密由上层集成面负责，沿 ToolIntegrationConfig 口径）。
type ConnectorConfig struct {
	BaseURL  string        `json:"base_url"`
	Provider string        `json:"provider"` // 方言 ID（generic/kuaidi100/...）
	Auth     ConnectorAuth `json:"auth"`
	Timeout  int           `json:"timeout_ms"` // 0=默认 8s
}

// ConnectorAuth 鉴权形态（none/bearer/api_key/header）。
type ConnectorAuth struct {
	Type   string `json:"type"`   // none/bearer/api_key
	Key    string `json:"key"`    // 凭证（加密落库面负责）
	Header string `json:"header"` // api_key 时的 header 名（缺省 Authorization）
}

// EndpointConfig 端点 preset（方法/路径/参数映射/能力标签）。
type EndpointConfig struct {
	Name         string   `json:"name"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"` // 能力标签（query_express/...）
	// ReadOnly explicitly declares a query performed with a non-safe HTTP method (e.g. POST).
	// It is honored only when every capability is query_*; the default is fail-closed.
	ReadOnly bool              `json:"read_only,omitempty"`
	ParamMap map[string]string `json:"param_map"` // 调用参数→平台参数名（缺省同名）
	// Headers preset 声明的静态出站头（§3.12 刀2 收编 CourierClient 时补：鉴权之外
	// 的固定头如 X-Api-Secret 归 preset 数据而非调用方代码）。generic 方言消费；
	// 同名时**不覆盖**鉴权头（鉴权优先），form/签名类方言按各自语义可忽略。
	Headers map[string]string `json:"headers,omitempty"`
	// Cell identifies the exact shop/action capability cell required by this endpoint.
	// High-risk endpoints without this metadata are not exposed by BridgeProvider.
	Cell *CapabilityCellRef `json:"capability_cell,omitempty"`
}

// CapabilityCellRef is endpoint metadata only; tenant/shop identity comes from the
// authenticated execution context and arguments, never from preset-global data.
type CapabilityCellRef struct {
	Product      string `json:"product"`
	Region       string `json:"region"`
	Action       string `json:"action"`
	Carrier      string `json:"carrier"`
	ShopArgument string `json:"shop_argument,omitempty"`
}

var ErrCapabilityCellDenied = errors.New("connector write denied by capability cell")

// ---------- 注册表 ----------

var (
	registryMu sync.RWMutex
	dialects   = map[string]Dialect{}
)

// Register 方言注册（重复注册 panic=配置错误即暴露，沿 tooluse RegisterExecutor 同款）。
func Register(d Dialect) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := dialects[d.ID()]; dup {
		panic(fmt.Sprintf("connector dialect already registered: %s", d.ID()))
	}
	dialects[d.ID()] = d
}

// Get 取方言（未注册返回 nil，调用方 fail-closed）。
func Get(id string) Dialect {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return dialects[id]
}

// List 方言清单（升序；vocab 端点/插件清单面消费）。
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(dialects))
	for id := range dialects {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ---------- 执行器 ----------

// Executor 连接器执行器（Dialect 无关）。
type Executor struct {
	http *http.Client
	// baseURLValidator 出站基址 SSRF 校验（默认=tooluse.ValidateOpenAPIBaseURL 严格口径；
	// 测试经 NewExecutorWithValidator 注入放行 seam——生产构造永不放宽）。
	baseURLValidator func(string) error
	writeAuthorizer  func(context.Context, *CapabilityCellRef, map[string]any) error
	execution        *capabilityExecution
}

func NewExecutor() *Executor {
	return &Executor{
		http:             &http.Client{Timeout: 10 * time.Second},
		baseURLValidator: tooluse.ValidateOpenAPIBaseURL,
	}
}

// NewExecutorWithValidator 测试/特殊部署面：注入基址校验器（仅限测试接缝，
// 生产路径用 NewExecutor 的严格口径）。
func NewExecutorWithValidator(validator func(string) error) *Executor {
	return &Executor{
		http:             &http.Client{Timeout: 10 * time.Second},
		baseURLValidator: validator,
	}
}

// NewExecutorUnrestricted 不做基址 SSRF 校验的执行器——**服务侧管理面客户端专用**。
//
// 适用面收窄到一句话：base_url 来自管理员配置（LLM 不可控）的既有服务侧客户端，
// 且其行为等价契约要求保留既有网络口径——§3.12 刀2 CourierClient 收编 generic
// 方言：旧实现无基址校验，内网快递镜像是合法部署形态（TB-03 E2E 即环回镜像）。
// LLM 可控出站面（桥接工具/openapi 导入/自定义工具）**禁用**本构造——那些面的
// 基址选择权在模型手里，必须走 NewExecutor 严格口径（缺口=把内网打通给模型）。
func NewExecutorUnrestricted() *Executor {
	return &Executor{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// ErrCapabilityNotDeclared 连接器未声明所调能力（fail-closed：不猜平台能做啥）。
var ErrCapabilityNotDeclared = errors.New("connector does not declare this capability")

// Call 经方言执行端点调用并归一化响应。
// 契约：base URL 合法性（SSRF 白名单，复用 tooluse.ValidateOpenAPIBaseURL）→
// 方言 BuildRequest → 出站 → NormalizeResponse → 统一信封。平台错误原样进
// error（调用方/审计可见），不包装成成功。
func (e *Executor) Call(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, args map[string]any) (map[string]any, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("connector base_url 未配置")
	}
	if e.baseURLValidator != nil {
		if err := e.baseURLValidator(cfg.BaseURL); err != nil {
			return nil, err
		}
	}
	d := Get(cfg.Provider)
	if d == nil {
		return nil, fmt.Errorf("方言 %q 未注册（可用: %v）", cfg.Provider, List())
	}
	boundWrite := !endpointReadOnly(ep) && e.execution != nil
	if !endpointReadOnly(ep) && !boundWrite {
		check := e.writeAuthorizer
		if check == nil {
			check = authorizePersistentConnectorWrite
		}
		if err := check(ctx, ep.Cell, args); err != nil {
			return nil, err
		}
	}
	callArgs := args
	if ep.Cell != nil {
		callArgs = make(map[string]any, len(args))
		for key, value := range args {
			callArgs[key] = value
		}
		shopArgument := ep.Cell.ShopArgument
		if shopArgument == "" {
			shopArgument = "shop_id"
		}
		delete(callArgs, shopArgument)
	}
	var req *http.Request
	var err error
	if boundWrite {
		req, err = e.boundWriteRequest(ctx, cfg, ep, args)
	} else {
		req, err = d.BuildRequest(ctx, cfg, ep, callArgs)
	}
	if err != nil {
		return nil, err
	}
	client := e.http
	if boundWrite {
		copy := *e.http
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connector %s status %d: %s", cfg.Provider, resp.StatusCode, truncateForError(body))
	}
	return d.NormalizeResponse(body, resp.StatusCode)
}

func endpointReadOnly(ep EndpointConfig) bool {
	if len(ep.Capabilities) == 0 {
		return false
	}
	for _, capability := range ep.Capabilities {
		if !strings.HasPrefix(capability, "query_") || strings.TrimSpace(strings.TrimPrefix(capability, "query_")) == "" {
			return false
		}
	}
	method := strings.ToUpper(strings.TrimSpace(ep.Method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return method == http.MethodPost && ep.ReadOnly
	}
}

func authorizePersistentConnectorWrite(ctx context.Context, ref *CapabilityCellRef, args map[string]any) error {
	if !validCapabilityCellRef(ref) || ref.Carrier != capregistry.ModeAPIAdapter {
		return fmt.Errorf("%w: capability cell metadata missing", ErrCapabilityCellDenied)
	}
	shopArgument := ref.ShopArgument
	if shopArgument == "" {
		shopArgument = "shop_id"
	}
	shop, _ := args[shopArgument].(string)
	orderID, _ := args["order_id"].(string)
	if strings.TrimSpace(shop) == "" || strings.TrimSpace(orderID) == "" {
		return fmt.Errorf("%w: execution resource binding missing", ErrCapabilityCellDenied)
	}
	source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceAfterSale, Product: ref.Product, ResourceID: orderID}
	resolver := capregistry.PersistentCapabilityResolver{Reader: repository.DefaultCapabilityEvidenceRepository()}
	if err := resolver.Authorize(ctx, source, ref.Action, ref.Carrier, shop, ref.Region, time.Now().UTC()); err != nil {
		return fmt.Errorf("%w: %w", ErrCapabilityCellDenied, err)
	}
	return fmt.Errorf("%w: execution_target_unbound", ErrCapabilityCellDenied)
}

func validCapabilityCellRef(ref *CapabilityCellRef) bool {
	return ref != nil && strings.TrimSpace(ref.Product) != "" && strings.TrimSpace(ref.Region) != "" &&
		strings.TrimSpace(ref.Action) != "" && strings.TrimSpace(ref.Carrier) != ""
}

func truncateForError(b []byte) string {
	s := string(b)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// ---------- 统一信封 ----------

// Envelope 归一化响应信封（方言输出契约）。
type Envelope struct {
	OK     bool             `json:"ok"`
	Data   map[string]any   `json:"data,omitempty"`
	Items  []map[string]any `json:"items,omitempty"`
	Notice string           `json:"notice,omitempty"`
	Raw    map[string]any   `json:"raw,omitempty"`
}

// ToMap 信封→map（工具结果回填面消费）。
func (e Envelope) ToMap() map[string]any {
	out := map[string]any{"ok": e.OK}
	if e.Notice != "" {
		out["notice"] = e.Notice
	}
	if e.Data != nil {
		out["data"] = e.Data
	}
	if e.Items != nil {
		out["items"] = e.Items
	}
	return out
}

// NormalizeJSON 通用归一化：平台响应已是 JSON 对象时原样进 Data；
// JSON 数组时进 Items；非 JSON 时进 Raw.string（不丢事实）。
func NormalizeJSON(body []byte) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err == nil {
		return map[string]any{"ok": true, "data": obj}, nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err == nil {
		return map[string]any{"ok": true, "items": arr}, nil
	}
	return map[string]any{"ok": true, "raw": map[string]any{"text": string(body)}}, nil
}

// ---------- 通用方言（generic） ----------

// GenericDialect 通用 REST/JSON 方言：Bearer/ApiKey header + JSON body（规整 API
// 的零代码接入面；签名类平台走专用方言）。
type GenericDialect struct{}

func (GenericDialect) ID() string { return "generic" }

func (GenericDialect) BuildRequest(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, args map[string]any) (*http.Request, error) {
	fullURL := strings.TrimRight(cfg.BaseURL, "/") + ep.Path
	method := strings.ToUpper(ep.Method)
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		buf, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(buf))
	} else if len(args) > 0 {
		q := url.Values{}
		for k, v := range args {
			q.Set(epParamName(ep, k), fmt.Sprintf("%v", v))
		}
		fullURL += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch cfg.Auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+cfg.Auth.Key)
	case "api_key":
		header := cfg.Auth.Header
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, cfg.Auth.Key)
	}
	// preset 声明的静态头：鉴权头之后补，同名不覆盖鉴权（鉴权优先语义）。
	for k, v := range ep.Headers {
		if v == "" || req.Header.Get(k) != "" {
			continue
		}
		req.Header.Set(k, v)
	}
	return req, nil
}

func (GenericDialect) NormalizeResponse(body []byte, statusCode int) (map[string]any, error) {
	return NormalizeJSON(body)
}

// epParamName 参数映射（preset ParamMap 缺省同名）。
func epParamName(ep EndpointConfig, arg string) string {
	if ep.ParamMap != nil {
		if mapped, ok := ep.ParamMap[arg]; ok && mapped != "" {
			return mapped
		}
	}
	return arg
}

// init 注册内置方言（generic 为零代码接入面；签名类方言随 preset 注册）。
func init() {
	Register(GenericDialect{})
}
