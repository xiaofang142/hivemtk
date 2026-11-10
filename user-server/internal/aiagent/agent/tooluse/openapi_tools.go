// Package tooluse — openapi_tools.go P1-2：OpenAPI 3.x 声明式工具导入。
//
// Dify「贴 OpenAPI schema 即成工具」的低摩擦接入面（竞品对照清单 B3）：
// 解析 OpenAPI 文档 → 每个 (path, method) 操作生成一个 Tool（Name/Parameters/
// Handler=HTTP 转发），**自动套装饰器链**（与内置工具同权治理——这是我方相对
// Dify 的差异点：每个调用过权限/限流/重试/超时/审计）。
//
// 解析自实现（零新依赖）：只取工具面需要的字段（paths/methods/params/schema），
// 不引入 kin-openapi 全量校验栈。
//
// SSRF 两层防线（urlguard.go:6「缺一不可」口径，MA-01）：配置期 ValidateOpenAPIBaseURL
// 把写进配置的坏地址挡在落库前；运行期 buildOps 的 client 挂 GuardedTransport——
// 拨号期复校对重定向跳内网、DNS rebinding 依然生效（kb_crawler/mcp client 同款）。
package tooluse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/urlguard"
)

// ValidateOpenAPIBaseURL 出站基址 SSRF 防线（配置期第一层）：仅允许 http(s)，拒绝
// 环回/链路本地/私有网段/元数据地址。导入工具是「LLM 决定调用」的高危面，放宽等于把内网
// 打通给模型；确需访问内网服务的部署应走正向代理并在代理侧做访问控制。
// 注意这层只护导入时刻：基址解析结果随 DNS/重定向可变，运行期靠 buildOps 的
// GuardedTransport 拨号期复校兜底（MA-01）。
func ValidateOpenAPIBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("base_url 解析失败: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base_url 仅允许 http/https，实际 %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("base_url 缺少主机名")
	}
	// 网段判据统一委托 urlguard 单一事实源（G-10 二次审核口径：手写版漏拒
	// IPv6 ULA fc00::/7、0.0.0.0/8 段与受限广播，双源漂移即绕行面）。
	if err := urlguard.CheckHost(host); err != nil {
		return fmt.Errorf("base_url 指向非公网目标: %w", err)
	}
	return nil
}

// OpenAPIImportSpec 一次导入的输入。
type OpenAPIImportSpec struct {
	Name        string `json:"name"`        // Provider 名（唯一键）
	Description string `json:"description"` // 工具集描述
	BaseURL     string `json:"base_url"`    // 目标服务基址
	SchemaJSON  string `json:"schema_json"` // OpenAPI 3.x JSON 文档
	AuthHeader  string `json:"auth_header"` // 可选：出站鉴权头名
	AuthValue   string `json:"auth_value"`  // 可选：出站鉴权值（调用方负责经 secrets 落库）
	TimeoutMS   int    `json:"timeout_ms"`  // 单调用超时（默认 10s）
}

// openAPIOperation 一个 (path, method) 操作的解析产物。
type openAPIOperation struct {
	ToolName string
	Summary  string
	Method   string
	Path     string
	Params   []openAPIParam
	HasBody  bool
}

type openAPIParam struct {
	Name        string
	In          string // path / query / header
	Required    bool
	SchemaType  string
	Description string
}

// ParseOpenAPISchema 从 OpenAPI 3.x JSON 提取工具操作集。
// 只认 paths 下的 get/post/put/patch/delete；operationId 缺失时按
// method+path 生成（snake_case，保证 Tool 名合法）。
func ParseOpenAPISchema(schemaJSON string) ([]openAPIOperation, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &doc); err != nil {
		return nil, fmt.Errorf("OpenAPI JSON 解析失败: %w", err)
	}
	if len(doc.Paths) == 0 {
		return nil, fmt.Errorf("OpenAPI 文档无 paths")
	}
	ops := make([]openAPIOperation, 0, 8)
	seen := map[string]bool{}
	for path, methods := range doc.Paths {
		for method, raw := range methods {
			m := strings.ToLower(method)
			switch m {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}
			var op struct {
				OperationID string `json:"operationId"`
				Summary     string `json:"summary"`
				Description string `json:"description"`
				Parameters  []struct {
					Name     string `json:"name"`
					In       string `json:"in"`
					Required bool   `json:"required"`
					Schema   struct {
						Type string `json:"type"`
					} `json:"schema"`
					Description string `json:"description"`
				} `json:"parameters"`
				RequestBody *struct {
					Required bool `json:"required"`
				} `json:"requestBody"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("操作 %s %s 解析失败: %w", m, path, err)
			}
			name := op.OperationID
			if name == "" {
				name = m + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_", ".", "_").Replace(path)
			}
			name = sanitizeToolName(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			o := openAPIOperation{ToolName: name, Method: m, Path: path,
				Summary: firstNonEmpty(op.Summary, op.Description)}
			for _, p := range op.Parameters {
				o.Params = append(o.Params, openAPIParam{
					Name: p.Name, In: p.In, Required: p.Required,
					SchemaType: p.Schema.Type, Description: p.Description,
				})
			}
			o.HasBody = op.RequestBody != nil
			ops = append(ops, o)
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("OpenAPI 文档无可用操作")
	}
	return ops, nil
}

func sanitizeToolName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// openAPITool 单个 OpenAPI 操作的 Tool 实现。Handler = HTTP 转发；
// 治理由外层装饰器链负责（本结构只做纯转发，不含业务判断）。
type openAPITool struct {
	op     openAPIOperation
	spec   OpenAPIImportSpec
	client *http.Client
}

func (t *openAPITool) Name() string           { return "openapi." + t.op.ToolName }
func (t *openAPITool) Category() ToolCategory { return CategoryBusiness }
func (t *openAPITool) Description() string {
	return fmt.Sprintf("[OpenAPI:%s] %s %s — %s", t.spec.Name, strings.ToUpper(t.op.Method), t.op.Path, t.op.Summary)
}

// Parameters 由操作参数推导 JSON Schema。
func (t *openAPITool) Parameters() ToolParameters {
	props := map[string]ToolParam{}
	var required []string
	for _, p := range t.op.Params {
		typ := p.SchemaType
		if typ == "" {
			typ = "string"
		}
		props[p.Name] = ToolParam{Type: typ, Description: fmt.Sprintf("(%s) %s", p.In, p.Description)}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	if t.op.HasBody {
		props["body"] = ToolParam{Type: "object", Description: "(body) 请求体 JSON 对象"}
		required = append(required, "body")
	}
	return ToolParameters{Type: "object", Properties: props, Required: required}
}

// Execute HTTP 转发。返回体截断到 64KB（LLM 上下文保护）。
func (t *openAPITool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	target := strings.TrimRight(t.spec.BaseURL, "/") + t.op.Path
	for _, p := range t.op.Params {
		if p.In == "path" {
			v := args[p.Name]
			target = strings.ReplaceAll(target, "{"+p.Name+"}", fmt.Sprintf("%v", v))
		}
	}
	// G-2：query 参数统一 url.Values 编码（值里的 &/#/空格/= 不再污染上游请求）
	query := url.Values{}
	for _, p := range t.op.Params {
		if p.In != "query" {
			continue
		}
		v, ok := args[p.Name]
		if !ok {
			continue
		}
		query.Add(p.Name, fmt.Sprintf("%v", v))
	}
	q := target
	if len(query) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		q += sep + query.Encode()
	}

	var body io.Reader
	if t.op.HasBody {
		if b, ok := args["body"]; ok {
			raw, err := json.Marshal(b)
			if err != nil {
				return ErrorResult(t.Name(), fmt.Errorf("body 序列化失败: %w", err)), err
			}
			body = bytes.NewReader(raw)
		}
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(t.op.Method), q, body)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	req.Header.Set("Accept", "application/json")
	if t.op.HasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.spec.AuthHeader != "" {
		req.Header.Set(t.spec.AuthHeader, t.spec.AuthValue)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	out := map[string]any{
		"status": resp.StatusCode,
		"body":   string(raw),
	}
	if resp.StatusCode >= 400 {
		return ErrorResult(t.Name(), fmt.Errorf("上游 %d: %s", resp.StatusCode, truncate(string(raw), 200))), nil
	}
	return SuccessResult(t.Name(), out), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// NewOpenAPIToolProvider 把导入的 schema 组装成 ToolProvider（走既有注册中心，
// 自动进入 RegisterAll 装配与装饰链接线）。
func NewOpenAPIToolProvider(spec OpenAPIImportSpec) (ToolProvider, error) {
	ops, err := ParseOpenAPISchema(spec.SchemaJSON)
	if err != nil {
		return nil, err
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("导入 Provider 名不能为空")
	}
	timeout := time.Duration(spec.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	providerName := "openapi_" + sanitizeToolName(spec.Name)
	return &openAPIProvider{name: providerName, desc: spec.Description, tools: buildOps(ops, spec, timeout)}, nil
}

func buildOps(ops []openAPIOperation, spec OpenAPIImportSpec, timeout time.Duration) []Tool {
	// 运行期 SSRF 第二层（MA-01）：拨号期复校（GuardedTransport.DialControl），
	// 重定向跳内网/DNS rebinding 后依然生效；Timeout 语义保持不变。
	client := &http.Client{Timeout: timeout, Transport: urlguard.GuardedTransport()}
	tools := make([]Tool, 0, len(ops))
	for _, op := range ops {
		tools = append(tools, &openAPITool{op: op, spec: spec, client: client})
	}
	return tools
}

type openAPIProvider struct {
	name  string
	desc  string
	tools []Tool
}

func (p *openAPIProvider) Name() string           { return p.name }
func (p *openAPIProvider) Category() ToolCategory { return CategoryBusiness }
func (p *openAPIProvider) Description() string    { return p.desc }
func (p *openAPIProvider) Provide(ctx ProviderContext) ([]Tool, error) {
	return p.tools, nil
}
