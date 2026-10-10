package mcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
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
}

// NewServer 构造 MCP 服务端
func NewServer(registry *tooluse.ToolRegistry) *Server {
	return &Server{
		registry: registry,
	}
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

	result, err := s.callTool(ctx, params)
	if err != nil {

		return s.successResponse(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: err.Error()}},
			IsError: true,
		}), nil
	}
	return s.successResponse(req.ID, result), nil
}

func (s *Server) listTools() []MCPTool {
	if s.registry == nil {
		return []MCPTool{}
	}
	tools := s.registry.List()
	out := make([]MCPTool, 0, len(tools))
	for _, t := range tools {
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
