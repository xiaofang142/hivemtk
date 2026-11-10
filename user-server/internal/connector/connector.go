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
	Name         string            `json:"name"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Capabilities []string          `json:"capabilities"` // 能力标签（query_express/...）
	ParamMap     map[string]string `json:"param_map"`    // 调用参数→平台参数名（缺省同名）
}

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
	req, err := d.BuildRequest(ctx, cfg, ep, args)
	if err != nil {
		return nil, err
	}
	resp, err := e.http.Do(req.WithContext(ctx))
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
