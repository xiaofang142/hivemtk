// Package mcp — client.go P1-3：MCP 客户端侧。
//
// 我方此前只有 Server（对外暴露工具）；本文件补 client：连外部 MCP server，
// tools/list 发现 → 动态注册为 tooluse.Tool（Execute=JSON-RPC tools/call 转发）。
// server 不可达时熔断（连续失败 N 次后进入冷却，期间直接报错不发起网络请求），
// 不影响其他工具。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/urlguard"
)

// ClientConfig MCP server 连接配置。
type ClientConfig struct {
	Name      string        // server 标识（工具名前缀 mcp_<name>_）
	Endpoint  string        // JSON-RPC HTTP(S) 端点
	APIKey    string        // 可选 Bearer
	Timeout   time.Duration // 单请求超时（默认 30s）
	FailLimit int           // 连续失败 N 次熔断（默认 3）
	Cooldown  time.Duration // 熔断冷却（默认 60s）
}

// Client MCP JSON-RPC 客户端。
type Client struct {
	cfg    ClientConfig
	client *http.Client
	nextID int64

	mu        sync.Mutex
	failCnt   int
	openUntil time.Time // 熔断开启截止时刻（零值=未开）

	// initialized 本 client 是否已完成 MCP 握手（initialize + notifications/initialized）。
	// 未握手就发 tools/list，合规 server 一律回 -32002（对方要求先 initialize），
	// 这正是「连不上任何标准 MCP server」的第一个断点。
	initialized bool
	// sessionID 服务端在 initialize 响应头下发的 Mcp-Session-Id（无状态实现可为空）。
	sessionID string
}

// newClientTransport 出站 Transport 工厂。默认=守闸（拨号期 SSRF 复校，G-1）；
// 测试替身穿透用：httptest 服务器绑 127.0.0.1，守闸按设计拒绝环路地址，
// 协议层用例经 withTestTransport 注入普通 Transport（守闸本身由 urlguard 与
// service 层用例覆盖）。生产路径零注入，行为不变。
var newClientTransport = func() http.RoundTripper { return urlguard.GuardedTransport() }

// NewClient 构造。
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.FailLimit <= 0 {
		cfg.FailLimit = 3
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 60 * time.Second
	}
	// G-1：拨号期复校（重定向/rebinding 后仍生效），所有 MCP 出站请求统一过闸
	return &Client{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout, Transport: newClientTransport()}, nextID: 1}
}

// rpcRequest JSON-RPC 2.0 请求。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// call 单次 JSON-RPC 调用（带熔断判定）。
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	if !c.openUntil.IsZero() && time.Now().Before(c.openUntil) {
		c.mu.Unlock()
		return fmt.Errorf("mcp %s 熔断中（冷却至 %s）", c.cfg.Name, c.openUntil.Format("15:04:05"))
	}
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	raw, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	c.mu.Lock()
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()
	resp, err := c.client.Do(req)
	if err != nil {
		c.recordFailure()
		return err
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.recordFailure()
		return err
	}
	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return fmt.Errorf("mcp %s HTTP %d: %s", c.cfg.Name, resp.StatusCode, truncateStr(string(body), 200))
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		c.recordFailure()
		return fmt.Errorf("mcp %s 响应非 JSON-RPC: %w", c.cfg.Name, err)
	}
	if rpcResp.Error != nil {
		// 协议层业务错误不计入熔断（server 活着，是这次调用的事）
		return fmt.Errorf("mcp %s JSON-RPC error %d: %s", c.cfg.Name, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	c.mu.Lock()
	c.failCnt = 0
	c.mu.Unlock()
	if out != nil && len(rpcResp.Result) > 0 {
		return json.Unmarshal(rpcResp.Result, out)
	}
	return nil
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failCnt++
	if c.failCnt >= c.cfg.FailLimit {
		c.openUntil = time.Now().Add(c.cfg.Cooldown)
		c.failCnt = 0
	}
}

// ListTools 发现外部工具集。
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	if err := c.EnsureInitialized(ctx); err != nil {
		return nil, err
	}
	var res ListToolsResult
	if err := c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool 转发 tools/call。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if err := c.EnsureInitialized(ctx); err != nil {
		return "", true, err
	}
	var res CallToolResult
	if err := c.call(ctx, "tools/call", CallToolParams{Name: name, Arguments: args}, &res); err != nil {
		return "", true, err
	}
	var sb strings.Builder
	for _, b := range res.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), res.IsError, nil
}

// mcpRemoteTool 外部 MCP 工具的 tooluse.Tool 适配。
type mcpRemoteTool struct {
	client *Client
	def    MCPTool
}

func (t *mcpRemoteTool) Name() string {
	return "mcp_" + sanitizeMCPToken(t.client.cfg.Name) + "_" + sanitizeMCPToken(t.def.Name)
}
func (t *mcpRemoteTool) Category() tooluse.ToolCategory { return tooluse.CategoryBusiness }
func (t *mcpRemoteTool) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", t.client.cfg.Name, t.def.Description)
}

// Parameters inputSchema(map) → ToolParameters（尽力映射，未知形状退化为自由对象）。
func (t *mcpRemoteTool) Parameters() tooluse.ToolParameters {
	params := tooluse.ToolParameters{Type: "object"}
	if t.def.InputSchema == nil {
		return params
	}
	if v, ok := t.def.InputSchema["type"].(string); ok {
		params.Type = v
	}
	if raw, ok := t.def.InputSchema["properties"]; ok {
		if props, ok := raw.(map[string]any); ok {
			params.Properties = map[string]tooluse.ToolParam{}
			for k, pv := range props {
				pm, _ := pv.(map[string]any)
				tp := tooluse.ToolParam{}
				if pm != nil {
					if s, ok := pm["type"].(string); ok {
						tp.Type = s
					}
					if s, ok := pm["description"].(string); ok {
						tp.Description = s
					}
				}
				params.Properties[k] = tp
			}
		}
	}
	if raw, ok := t.def.InputSchema["required"].([]any); ok {
		for _, r := range raw {
			if s, ok := r.(string); ok {
				params.Required = append(params.Required, s)
			}
		}
	}
	return params
}

// Execute 转发 tools/call。
func (t *mcpRemoteTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	text, isErr, err := t.client.CallTool(ctx, t.def.Name, args)
	if err != nil {
		return tooluse.ErrorResult(t.Name(), err), err
	}
	if isErr {
		err := fmt.Errorf("mcp 工具报告错误: %s", truncateStr(text, 300))
		return tooluse.ErrorResult(t.Name(), err), nil
	}
	return tooluse.SuccessResult(t.Name(), map[string]any{"text": text}), nil
}

func sanitizeMCPToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DiscoverAndRegister 连接 server、发现工具并注册到 registry。
// server 不可达返回错误（调用方记日志，不影响其他工具注册）。
func DiscoverAndRegister(ctx context.Context, cfg ClientConfig, registry *tooluse.ToolRegistry) (int, error) {
	if registry == nil {
		return 0, fmt.Errorf("tool registry 为 nil")
	}
	c := NewClient(cfg)
	tools, err := c.ListTools(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, def := range tools {
		if def.Name == "" {
			continue
		}
		t := &mcpRemoteTool{client: c, def: def}
		if err := registry.Register(t); err != nil {
			// 已注册且同名 = 该工具本来就在（重新启用/重复发现）——这是「可用」而不是失败。
			// 早先把这里一律跳过，导致关→开一次之后 tool_count 掉成 0（API 冒烟抓到的真缺陷）。
			if errors.Is(err, tooluse.ErrToolAlreadyExists) {
				n++
			}
			continue
		}
		n++
	}
	return n, nil
}

// clientProtocolVersion 我方声明的 MCP 协议版本（与服务端 NegotiateProtocolVersion 的候选集一致）。
const clientProtocolVersion = "2024-11-05"

// EnsureInitialized 完成 MCP 握手：initialize → notifications/initialized。
//
// 为什么必须做：MCP 规范要求 tools/list、tools/call 之前先 initialize，合规 server
// （含本仓 mcp.Server 自己）在未握手时一律回 -32002。缺这一步的客户端看起来"协议写完了"
// 但对任何真 server 都不可用——本地单测若 mock 掉传输层就发现不了。
//
// 失败不缓存：server 可能稍后才起来，下一次发现可重试（熔断由 call 的失败计数负责）。
func (c *Client) EnsureInitialized(ctx context.Context) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	var res InitializeResult
	if err := c.call(ctx, "initialize", InitializeParams{
		ProtocolVersion: clientProtocolVersion,
		ClientInfo:      Implementation{Name: "hivemtk", Version: "1.0"},
	}, &res); err != nil {
		// 「已经初始化过」不是失败：服务端会话可能被复用（同一 server 被重复发现、
		// 重新启用后再发现、或实现方按会话保留握手态）。规范允许服务端拒绝重复
		// initialize，客户端必须把这当作握手已完成，否则关一次再开就整体不可用。
		if !isAlreadyInitialized(err) {
			return fmt.Errorf("mcp %s initialize 失败: %w", c.cfg.Name, err)
		}
	}
	// 通知无 id、服务端通常不回 body：不发也行（不少实现接受），发了更贴规范；
	// 失败不阻断握手（部分 server 对未知通知返回错误体）。
	c.notify(ctx, "notifications/initialized")

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// isAlreadyInitialized 判定错误是否为「服务端已处于初始化态」（-32003 / already initialized）。
func isAlreadyInitialized(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already initialized") || strings.Contains(msg, "-32003")
}

// notify 发 JSON-RPC 通知（无 id）。任何响应体/错误都不影响调用方。
func (c *Client) notify(ctx context.Context, method string) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	c.mu.Lock()
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
