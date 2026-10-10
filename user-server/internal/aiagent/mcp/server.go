package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

const (
	ProtocolVersionLegacy = "2025-06-18"
	ProtocolVersionLatest = "2025-11-25"
)

// ProtocolVersion 服务端最新支持版本（历史常量名，保持兼容）
const ProtocolVersion = ProtocolVersionLatest

// SupportedProtocolVersions 服务端支持的协议版本列表
var SupportedProtocolVersions = []string{ProtocolVersionLegacy, ProtocolVersionLatest}

// NegotiateProtocolVersion 协议版本协商：
// 客户端请求版本在支持范围内 → 原样返回；否则返回最新支持版。
func NegotiateProtocolVersion(requested string) string {
	for _, v := range SupportedProtocolVersions {
		if requested == v {
			return v
		}
	}
	return ProtocolVersionLatest
}

// ServerInfo 服务端元信息
const ServerInfo = "hivemtk-tooluse-mcp"

// ServerVersion 服务端版本
const ServerVersion = "1.0.0"

// JSON-RPC 2.0 标准错误码
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	// MCP 自定义错误码
	ErrCodeNotInitialized = -32002
	ErrCodeAlreadyInit    = -32003
)

// JSONRPCRequest JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 2.0 错误
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InitializeParams initialize 方法参数
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// ClientCapabilities 客户端能力
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability Roots 能力
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability Sampling 能力
type SamplingCapability struct{}

// Implementation 客户端/服务端实现信息
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult initialize 方法结果
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// ServerCapabilities 服务端能力
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability Tools 能力
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability Resources 能力
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability Prompts 能力
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPTool MCP 协议工具描述（与 tooluse.Tool 的桥接）
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ListToolsResult tools/list 方法结果
type ListToolsResult struct {
	Tools      []MCPTool `json:"tools"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

// CallToolParams tools/call 方法参数
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult tools/call 方法结果
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// ContentBlock 内容块
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     any    `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Server MCP 服务端
type Server struct {
	registry    *tooluse.ToolRegistry
	initialized bool
	mu          sync.RWMutex

	sessionID string

	clientInfo    Implementation
	clientCaps    ClientCapabilities
	initStartedAt time.Time

	// allowedTools 凭证级工具授权范围（2026-10-10）。
	// nil/空 = 不限制（单值链路、向后兼容）；非空时 tools/list 只返回白名单内工具，
	// tools/call 对白名单外工具返回 JSON-RPC error 而非执行。
	allowedTools map[string]bool

	// auditSink 调用审计写入钩子（2026-10-10）：一次 tools/call 的成败快照。
	// 由 controller 注入（写库在哪一层由注入方决定，本包不碰 DB）；
	// 未注入（nil）时审计静默跳过——单测与无管理端的部署不受影响。
	auditSink func(AuditRecord)
}

// AuditRecord 一次 MCP 工具调用的审计快照（参数摘要化后落库，见 model.MCPCallAudit）。
type AuditRecord struct {
	ClientID  string
	Method    string
	Tool      string
	Allowed   bool // false = 被凭证 scope 拒绝，未执行
	OK        bool
	ErrorMsg  string
	ArgsHash  string
	LatencyMS int64
	IP        string
}

// NewServer 构造 MCP 服务端
func NewServer(registry *tooluse.ToolRegistry) *Server {
	return &Server{
		registry: registry,
	}
}

// SetAllowedTools 设置该请求凭证的工具白名单（逗号分隔，空串=不限制）。
// 由 controller 在构造 Server 后、HandleRequest 前调用 —— scope 来自 MCPGuard 写入的上下文。
func (s *Server) SetAllowedTools(commaSep string) {
	commaSep = strings.TrimSpace(commaSep)
	if commaSep == "" {
		s.allowedTools = nil
		return
	}
	m := map[string]bool{}
	for _, t := range strings.Split(commaSep, ",") {
		if v := strings.TrimSpace(t); v != "" {
			m[v] = true
		}
	}
	if len(m) > 0 {
		s.allowedTools = m
	}
}

// AllowedTool 返回该工具名是否在当前凭证的授权范围内。
// 未设白名单（nil）时一律 true（兼容存量凭证与单值链路）。
func (s *Server) AllowedTool(name string) bool {
	if s.allowedTools == nil {
		return true
	}
	return s.allowedTools[name]
}

// SetAuditSink 注入审计钩子；传 nil 即摘掉（测试与未装配环境）。
// 审计在调用方（controller）构造 Server 后注入，与凭证信息同源。
func (s *Server) SetAuditSink(fn func(AuditRecord)) {
	s.auditSink = fn
}

// audit 记录一次调用（sink 未注入或为空时 no-op；审计失败不上抛——记录是旁路职责）。
func (s *Server) audit(rec AuditRecord) {
	if s.auditSink == nil {
		return
	}
	s.auditSink(rec)
}

// HandleRequest 处理单个 JSON-RPC 2.0 请求
//
// 返回：JSON-RPC 2.0 响应（已序列化）
//
// 错误处理：
//   - 未初始化的客户端调用非 initialize 方法 → ErrCodeNotInitialized
//   - 重复 initialize → ErrCodeAlreadyInit
//   - 方法未实现 → ErrCodeMethodNotFound
func (s *Server) HandleRequest(ctx context.Context, rawReq []byte) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return s.errorResponse(nil, ErrCodeParse, "parse error", err.Error()), nil
	}
	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, ErrCodeInvalidRequest, "jsonrpc must be 2.0", nil), nil
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "notifications/initialized":

		return nil, nil
	case "ping":
		return s.successResponse(req.ID, map[string]any{}), nil
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.successResponse(req.ID, map[string]any{"resources": []any{}}), nil
	case "resources/read":
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, "resources/read not implemented", nil), nil
	case "prompts/list":
		return s.successResponse(req.ID, map[string]any{"prompts": []any{}}), nil
	case "prompts/get":
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, "prompts/get not implemented", nil), nil
	default:
		return s.errorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method not found: %s", req.Method), nil), nil
	}
}

func (s *Server) handleInitialize(_ context.Context, req JSONRPCRequest) ([]byte, error) {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, ErrCodeInvalidParams, "invalid initialize params", err.Error()), nil
		}
	}

	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return s.errorResponse(req.ID, ErrCodeAlreadyInit, "already initialized", nil), nil
	}
	s.initialized = true
	s.sessionID = newSessionID()
	s.clientInfo = params.ClientInfo
	s.clientCaps = params.Capabilities
	s.initStartedAt = time.Now()
	s.mu.Unlock()

	result := InitializeResult{
		ProtocolVersion: NegotiateProtocolVersion(params.ProtocolVersion),
		Capabilities: ServerCapabilities{
			Tools:     &ToolsCapability{ListChanged: false},
			Resources: &ResourcesCapability{},
			Prompts:   &PromptsCapability{},
		},
		ServerInfo: Implementation{
			Name:    ServerInfo,
			Version: ServerVersion,
		},
	}
	return s.successResponse(req.ID, result), nil
}

func (s *Server) handleToolsList(_ context.Context, req JSONRPCRequest) ([]byte, error) {
	if errResp, ok := s.checkInitialized(req); !ok {
		return errResp, nil
	}
	tools := s.listTools()
	return s.successResponse(req.ID, ListToolsResult{Tools: tools}), nil
}

func (s *Server) handleToolsCall(ctx context.Context, req JSONRPCRequest) ([]byte, error) {
	if errResp, ok := s.checkInitialized(req); !ok {
		return errResp, nil
	}
	var params CallToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, ErrCodeInvalidParams, "invalid call params", err.Error()), nil
		}
	}
	if params.Name == "" {
		return s.errorResponse(req.ID, ErrCodeInvalidParams, "tool name required", nil), nil
	}

	start := time.Now()
	result, err := s.callTool(ctx, params)
	latency := time.Since(start).Milliseconds()

	// 审计留痕：不论成败与是否被 scope 拒绝都记一条（Allowed 区分"被拒"与"真执行"）。
	if s.auditSink != nil {
		ok := err == nil && result != nil && !result.IsError
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		s.audit(AuditRecord{
			Method:    "tools/call",
			Tool:      params.Name,
			Allowed:   s.AllowedTool(params.Name),
			OK:        ok,
			ErrorMsg:  truncateMsg(msg, 300),
			ArgsHash:  hashArgs(params.Arguments),
			LatencyMS: latency,
		})
	}

	if err != nil {

		return s.successResponse(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: err.Error()}},
			IsError: true,
		}), nil
	}
	return s.successResponse(req.ID, result), nil
}

// hashArgs 参数摘要：sha256 前 24 字符。MCP 参数常带手机号/邮箱/正文，
// 原文入库等于复制一份敏感数据，审计只需要"这条调用长这样"的可对账指纹。
func hashArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}

// truncateMsg 审计错误文案截断（防止渠道把超长响应文本写进审计表）。
func truncateMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Server) listTools() []MCPTool {
	if s.registry == nil {
		return []MCPTool{}
	}
	tools := s.registry.List()
	out := make([]MCPTool, 0, len(tools))
	for _, t := range tools {
		if !s.AllowedTool(t.Name()) {
			continue // 凭证 scope 过滤：白名单外的工具不暴露给 tools/list
		}
		params := t.Parameters()
		out = append(out, MCPTool{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: convertParameters(params),
		})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, params CallToolParams) (*CallToolResult, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("tool registry not configured")
	}
	// 凭证级 scope 拦截：白名单外工具一律拒绝执行（不发消息、不写客户数据）。
	if !s.AllowedTool(params.Name) {
		return nil, fmt.Errorf("工具 %q 不在当前凭证的授权范围内", params.Name)
	}
	tool, err := s.registry.Get(params.Name)
	if err != nil {
		return nil, fmt.Errorf("tool not found: %s", params.Name)
	}

	toolCtx := context.WithValue(ctx, toolNameKey{}, params.Name)
	toolResult, err := tool.Execute(toolCtx, params.Arguments)
	if err != nil {
		return nil, fmt.Errorf("tool execution failed: %w", err)
	}

	text := toolResultToText(toolResult)
	return &CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
		IsError: !toolResult.Success,
	}, nil
}

func (s *Server) checkInitialized(req JSONRPCRequest) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.initialized {
		return s.errorResponse(req.ID, ErrCodeNotInitialized, "server not initialized", nil), false
	}
	return nil, true
}

// IsInitialized 返回初始化状态（供 HTTP handler 决策）
func (s *Server) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// SessionID 返回 initialize 成功后生成的会话标识（未初始化为空串）。
// HTTP 层将其写入 Mcp-Session-Id 响应头（spec：仅允许可见 ASCII 0x21-0x7E，
// hex 编码满足该约束）。
func (s *Server) SessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {

		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}

func (s *Server) successResponse(id json.RawMessage, result any) []byte {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	return data
}

func (s *Server) errorResponse(id json.RawMessage, code int, message string, data any) []byte {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	out, _ := json.Marshal(resp)
	return out
}

type toolNameKey struct{}

func toolResultToText(r tooluse.ToolResult) string {
	if r.Data == nil {
		return r.Error
	}
	data, err := json.Marshal(r.Data)
	if err != nil {
		return r.Error
	}
	return string(data)
}

func convertParameters(p tooluse.ToolParameters) map[string]any {
	out := map[string]any{
		"type": "object",
	}
	if len(p.Properties) > 0 {
		props := make(map[string]any, len(p.Properties))
		for name, def := range p.Properties {
			prop := map[string]any{
				"type":        def.Type,
				"description": def.Description,
			}
			if len(def.Enum) > 0 {
				prop["enum"] = def.Enum
			}
			if def.Default != nil {
				prop["default"] = def.Default
			}
			props[name] = prop
		}
		out["properties"] = props
	}
	if len(p.Required) > 0 {
		out["required"] = p.Required
	}
	return out
}
