package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ErrHostOffline Host 未连接（无 Host 用户的统一降级信号 → controller 转 409 引导）
var ErrHostOffline = errors.New("browser host 未连接，请在本机 Chrome 加载扩展并运行 cmd/nm-host/install.sh")

// CommandResult Host 回包
type CommandResult struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// HostConn 一条 NM Host WebSocket 连接（与 user_id 一一绑定）
// 并发约定：写帧经 writeMu 串行；读循环把回包按 req_id 投递到 pending chan。
type HostConn struct {
	UserID  uint
	Version string
	PID     int

	conn     *websocket.Conn
	writeMu  sync.Mutex
	registry *HostRegistry

	pendingMu sync.Mutex
	pending   map[string]chan *CommandResult

	closedOnce sync.Once
	closed     chan struct{}
}

func newHostConn(userID uint, version string, pid int, conn *websocket.Conn, reg *HostRegistry) *HostConn {
	return &HostConn{
		UserID:   userID,
		Version:  version,
		PID:      pid,
		conn:     conn,
		registry: reg,
		pending:  make(map[string]chan *CommandResult),
		closed:   make(chan struct{}),
	}
}

func (c *HostConn) Done() <-chan struct{} { return c.closed }

// writeJSON 串行写帧
func (c *HostConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(v)
}

// readLoop 读循环：按 req_id 投递回包；连接断开时收尾
func (c *HostConn) readLoop() {
	defer c.close()
	for {
		var frame struct {
			ReqID string         `json:"req_id"`
			OK    bool           `json:"ok"`
			Error string         `json:"error"`
			Data  map[string]any `json:"data"`
		}
		if err := c.conn.ReadJSON(&frame); err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Warnf("[BrowserHost] 连接读失败 user=%d: %v", c.UserID, err)
			}
			return
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[frame.ReqID]
		if ok {
			delete(c.pending, frame.ReqID)
		}
		c.pendingMu.Unlock()
		if !ok {
			continue // 超时已被放弃的回包，丢弃
		}
		ch <- &CommandResult{OK: frame.OK, Error: frame.Error, Data: frame.Data}
	}
}

func (c *HostConn) registerPending(reqID string) chan *CommandResult {
	ch := make(chan *CommandResult, 1)
	c.pendingMu.Lock()
	c.pending[reqID] = ch
	c.pendingMu.Unlock()
	return ch
}

func (c *HostConn) removePending(reqID string) {
	c.pendingMu.Lock()
	delete(c.pending, reqID)
	c.pendingMu.Unlock()
}

func (c *HostConn) close() {
	c.closedOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
		if c.registry != nil {
			c.registry.unregister(c.UserID, c)
			c.registry.onDisconnect(c.UserID)
		}
	})
}

// HostRegistry 在线 Host 注册表：userID → *HostConn。
// 命令只路由到归属 Host，绝不跨用户投递（多租户边界，见设计文档 §10）。
type HostRegistry struct {
	mu    sync.RWMutex
	conns map[uint]*HostConn

	// onDisconnect 断连清理钩子（由 routes 装配时注入 sessionRepo 清理逻辑）
	onDisconnectFn func(ctx context.Context, userID uint)
}

func NewHostRegistry() *HostRegistry {
	return &HostRegistry{conns: make(map[uint]*HostConn)}
}

// SetDisconnectHook 注入断连清理钩子（该用户所有 running session 置 failed）
func (r *HostRegistry) SetDisconnectHook(fn func(ctx context.Context, userID uint)) {
	r.onDisconnectFn = fn
}

func (r *HostRegistry) onDisconnect(userID uint) {
	if r.onDisconnectFn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.onDisconnectFn(ctx, userID)
}

// Register 登记连接；同用户旧连接存在则顶掉（Chrome 重启场景）
func (r *HostRegistry) Register(userID uint, version string, pid int, conn *websocket.Conn) *HostConn {
	hc := newHostConn(userID, version, pid, conn, r)
	r.mu.Lock()
	if old, ok := r.conns[userID]; ok && old != hc {
		go old.close() // 旧连接退出（其 close 会在 unregister 中因指针不等而跳过）
	}
	r.conns[userID] = hc
	r.mu.Unlock()
	logger.Infof("[BrowserHost] Host 已注册 user=%d version=%s pid=%d", userID, version, pid)
	go hc.readLoop()
	return hc
}

func (r *HostRegistry) unregister(userID uint, hc *HostConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.conns[userID]; ok && cur == hc {
		delete(r.conns, userID)
	}
}

func (r *HostRegistry) GetConn(userID uint) (*HostConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[userID]
	return c, ok
}

// EnsureOnline Host 在线检查
func (r *HostRegistry) EnsureOnline(userID uint) error {
	if _, ok := r.GetConn(userID); !ok {
		return ErrHostOffline
	}
	return nil
}

// Status 概览（admin 用）
func (r *HostRegistry) Status() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]map[string]any, 0, len(r.conns))
	for _, c := range r.conns {
		out = append(out, map[string]any{
			"user_id": c.UserID,
			"version": c.Version,
			"pid":     c.PID,
		})
	}
	return out
}

// MyStatus 当前用户的 Host 在线状态（普通登录用户用，只读自己）
func (r *HostRegistry) MyStatus(userID uint) map[string]any {
	c, ok := r.GetConn(userID)
	if !ok {
		return map[string]any{"online": false, "user_id": userID}
	}
	return map[string]any{
		"online":  true,
		"user_id": c.UserID,
		"version": c.Version,
		"pid":     c.PID,
	}
}

// Request 发命令帧并等回包（req_id 关联，带超时）
func (r *HostRegistry) Request(ctx context.Context, userID uint, timeout time.Duration, cmd map[string]any) (map[string]any, error) {
	conn, ok := r.GetConn(userID)
	if !ok {
		return nil, ErrHostOffline
	}
	reqID := uuid.New()
	cmd["req_id"] = reqID.String()

	ch := conn.registerPending(reqID.String())
	defer conn.removePending(reqID.String())

	if err := conn.writeJSON(cmd); err != nil {
		return nil, fmt.Errorf("发送命令到 Host 失败: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-conn.Done():
		return nil, ErrHostOffline
	case <-time.After(timeout):
		return nil, fmt.Errorf("Host 命令超时（%s，action=%v）", timeout, cmd["action"])
	case res := <-ch:
		if !res.OK {
			return nil, errors.New(strings.TrimSpace(res.Error))
		}
		if res.Data == nil {
			res.Data = map[string]any{}
		}
		return res.Data, nil
	}
}
