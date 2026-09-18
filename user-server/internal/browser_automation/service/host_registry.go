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

// consecutiveCmdTimeoutSick 连续命令超时判病阈值：2 条=几乎必然假死（合法慢命令
// 最长 60s 且罕见连续两条超时；单条慢命令超时属页面异常，非链路假死）。
const consecutiveCmdTimeoutSick = 2

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

	// R4（R25 真机暴露，R26 产品化）命令级健康探针：nm-host 假死新形态=WS/心跳全活
	// （TCP 半双工未断、ping/pong 正常）但 stdio→扩展方向断链——命令帧有去无回。
	// 心跳测不出这种"传输活、应用死"，唯一可靠信号=命令超时本身：连续 N 条超时即判假死，
	// 服务端主动 close（复用断连清理钩子+nm-host WS 退避重连），把人工 pkill 变自愈。
	cmdTimeouts int

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

// 心跳常量（D4a/G4）：半开 TCP（VPN 抖动/机器睡眠唤醒）无应用层探测时，
// Request 挂满命令超时且断连清理钩子不触发（TCP 未断）。ping/pong + 读超时把它变确定事件。
const (
	hostPingInterval = 30 * time.Second
	hostReadTimeout  = 90 * time.Second // 容忍 2 个 ping 周期丢帧
)

// pingLoop 服务端定时 ping（gorilla 客户端默认自动回 pong，nm-host 无需改动）。
// 连接关闭随 closed 退出；写失败（半开连对端已死）即 close——触发既有断连清理钩子。
func (c *HostConn) pingLoop() {
	t := time.NewTicker(hostPingInterval)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				logger.Warnf("[BrowserHost] ping 失败 user=%d（判死连接）: %v", c.UserID, err)
				c.close()
				return
			}
		}
	}
}

// writeJSON 串行写帧
func (c *HostConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(v)
}

// readLoop 读循环：按 req_id 投递回包；连接断开时收尾。
// D4a：读超时 90s，收到 pong 即重置——僵尸连接最迟 90s 判定并触发清理钩子。
func (c *HostConn) readLoop() {
	defer c.close()
	c.conn.SetReadLimit(4 << 20) // 扩展→Host 方向官方上限 4GB 太大，命令回包 4MiB 封顶已绰绰（截图 base64 实测 <2MiB）
	_ = c.conn.SetReadDeadline(time.Now().Add(hostReadTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(hostReadTimeout))
	})
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
		if c.conn != nil { // 测试构造的探针连接无真实 WS（R4）
			_ = c.conn.Close()
		}
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
	go hc.pingLoop() // D4a：心跳探测，僵尸连接最迟 90s 判死
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
		// R4 探针：超时计数（有去无回=假死信号）；达阈值主动判死本连接触发自愈。
		conn.noteCmdTimeout(cmd["action"])
		return nil, fmt.Errorf("host 命令超时（%s，action=%v）", timeout, cmd["action"])
	case res := <-ch:
		conn.noteCmdAlive() // 回包到达=应用面活着，清零计数
		if !res.OK {
			return nil, errors.New(strings.TrimSpace(res.Error))
		}
		if res.Data == nil {
			res.Data = map[string]any{}
		}
		return res.Data, nil
	}
}

// noteCmdTimeout 命令超时计数；连续达阈值→判假死，服务端主动 close 该连接。
// R27-2 真机修正（session199/204-208 两轮实证）：只 close 服务端 WS 不够——nm-host 会秒级
// 重连重注册成「僵尸注册」（WS 活、应用死照旧），端到端服务不恢复。正确自愈链必须让
// **host 进程退出**：Chrome 感知 port 死 → SW onDisconnect 重连 → connectNative 拉起全新 host。
// 故 close 前先发 __host_shutdown 控制帧（pumpLoop 拦截、转发前终结，WS 活着才判得准）。
func (c *HostConn) noteCmdTimeout(action any) {
	c.pendingMu.Lock()
	c.cmdTimeouts++
	n := c.cmdTimeouts
	c.pendingMu.Unlock()
	if n >= consecutiveCmdTimeoutSick {
		logger.Warnf("[BrowserHost] 连续 %d 条命令超时（最近 action=%v）判 Host 应用面假死：下发 shutdown 帧+断开连接触发自愈 user=%d pid=%d", n, action, c.UserID, c.PID)
		// 控制帧尽力送达（判病前提=WS 传输可用）；失败也无妨，close 仍兜底回收服务端状态。
		// c.conn==nil（单测探针连接）时跳过帧只走 close。
		if c.conn != nil {
			_ = c.writeJSON(map[string]any{"action": "__host_shutdown__", "reason": "sick_probe", "req_id": ""})
		}
		c.close()
	}
}

// noteCmdAlive 回包/正常到达即证明应用面存活，清零超时计数。
func (c *HostConn) noteCmdAlive() {
	c.pendingMu.Lock()
	c.cmdTimeouts = 0
	c.pendingMu.Unlock()
}
