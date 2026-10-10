package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

// HeaderMcpSessionID MCP 会话标识响应头/请求头名
const HeaderMcpSessionID = "Mcp-Session-Id"

// DefaultSessionIdleTTL MCP 会话空闲过期时长（参数中心未配置时的兜底）
const DefaultSessionIdleTTL = 30 * time.Minute

var sessionIdleTTLProvider = func() time.Duration { return DefaultSessionIdleTTL }

// SetSessionIdleTTLProvider 由装配层注入参数中心读取口（nil 视为不注入）
func SetSessionIdleTTLProvider(fn func() time.Duration) {
	if fn != nil {
		sessionIdleTTLProvider = fn
	}
}

// SessionIdleTTL 非正值一律回落兜底：TTL ≤0 会让 storeSession 一进来就把所有在册会话
// 清掉，MCP 客户端每次 initialize 都被当成新会话，服务端状态反复重置。
func SessionIdleTTL() time.Duration {
	v := sessionIdleTTLProvider()
	if v <= 0 {
		return DefaultSessionIdleTTL
	}
	return v
}

// HTTPHandler MCP Streamable HTTP 最小子集处理器。
//
// 每个会话持有一个独立 Server 实例（避免 initialize 状态跨客户端污染），
// 由 SessionManager 按会话 ID 存取。
type HTTPHandler struct {
	registry *tooluse.ToolRegistry

	mu       sync.Mutex
	sessions map[string]*mcpHTTPSession
}

type mcpHTTPSession struct {
	server   *Server
	lastSeen time.Time
}

// NewHTTPHandler 构造 MCP HTTP handler
func NewHTTPHandler(registry *tooluse.ToolRegistry) *HTTPHandler {
	return &HTTPHandler{
		registry: registry,
		sessions: make(map[string]*mcpHTTPSession),
	}
}

// ServeHTTP 实现 http.Handler（仅 POST）
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed: MCP streamable HTTP requires POST", http.StatusMethodNotAllowed)
		return
	}

	if !acceptHeaderOK(r.Header.Get("Accept")) {
		http.Error(w, "Not Acceptable: client must accept application/json or text/event-stream", http.StatusNotAcceptable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req JSONRPCRequest
	_ = json.Unmarshal(body, &req)

	if req.Method == "initialize" {
		h.handleInitializeRequest(w, r, body)
		return
	}

	srv := h.lookupSession(r.Header.Get(HeaderMcpSessionID))
	if srv == nil {
		http.Error(w, "Bad Request: missing or invalid "+HeaderMcpSessionID+" header", http.StatusBadRequest)
		return
	}
	resp, _ := srv.HandleRequest(r.Context(), body)
	h.writeJSONRPCResponse(w, resp)
}

func (h *HTTPHandler) handleInitializeRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	srv := NewServer(h.registry)
	resp, _ := srv.HandleRequest(r.Context(), body)

	var rpcResp JSONRPCResponse
	if json.Unmarshal(resp, &rpcResp) == nil && rpcResp.Error == nil && srv.SessionID() != "" {
		h.storeSession(srv.SessionID(), srv)
		w.Header().Set(HeaderMcpSessionID, srv.SessionID())
	}
	h.writeJSONRPCResponse(w, resp)
}

func (h *HTTPHandler) writeJSONRPCResponse(w http.ResponseWriter, resp []byte) {
	if len(resp) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func acceptHeaderOK(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if mt == "application/json" || mt == "text/event-stream" {
			return true
		}
	}
	return false
}

func (h *HTTPHandler) storeSession(id string, srv *Server) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for sid, s := range h.sessions {
		if now.Sub(s.lastSeen) > SessionIdleTTL() {
			delete(h.sessions, sid)
		}
	}
	h.sessions[id] = &mcpHTTPSession{server: srv, lastSeen: now}
}

func (h *HTTPHandler) lookupSession(id string) *Server {
	if id == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	if !ok {
		return nil
	}
	s.lastSeen = time.Now()
	return s.server
}
