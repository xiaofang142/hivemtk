package egolite

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EgoRunnerRegistry runner 注册表（调研 §6.5 定案：连接治理照抄 HostRegistry，
// 但物理独立——断连钩子指向 egolite_executions，绝不与 host-ws 共享实现实例）。
//
// 与 HostRegistry 的关键差异：
//   - per-user 仍一连接（同一用户起第二个 runner 走顶号仲裁）；
//   - 无命令帧 req_id 回包——下行是 task spec（执行单元），上行是事件行流；
//   - 假死判病骨架版不做（M2 T6 接 execution 超时看门狗后补连续超时计数）。

var (
	// ErrEgoRunnerOffline 该用户 runner 未连接（controller 转 409 引导，ErrHostOffline 同型）
	ErrEgoRunnerOffline = errors.New("ego-runner 未连接：请确认本机已安装 ego lite 并运行 ego-runner" +
		"（安装引导见 user-web ego-lite 页面）")
)

// EgoRunnerConn 一条 runner WS 连接（与 user_id 一一绑定）
type EgoRunnerConn struct {
	UserID     uint
	Version    string
	DeviceID   string
	Hostname   string
	EgoVersion string
	Workspace  string

	conn    *websocket.Conn
	writeMu sync.Mutex
	reg     *EgoRunnerRegistry

	registeredAt time.Time
}

func newEgoRunnerConn() time.Time { return time.Now() }

// SendSpec 下发帧（写经 writeMu 串行 + 单帧写时限，HostConn 同纪律——
// 半开 TCP 上无时限的 WriteJSON 会把 ping/下发协程一起挂死）
func (c *EgoRunnerConn) SendSpec(frame any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(egoPingWriteDeadline))
	return c.conn.WriteJSON(frame)
}

// EgoRunnerRegistry per-user runner 连接表
type EgoRunnerRegistry struct {
	mu       sync.RWMutex
	conns    map[uint]*EgoRunnerConn
	hook     func(ctx context.Context, userID uint)
	hookOnce sync.Once
}

func NewEgoRunnerRegistry() *EgoRunnerRegistry {
	return &EgoRunnerRegistry{conns: make(map[uint]*EgoRunnerConn)}
}

// SetDisconnectHook 注册断连清理（M2 T6 里指向 egolite_executions 的超时收口；
// 骨架版仅记日志——不共享 host-ws 的 FailRunningByUser，那是 browser_sessions 的语义）
func (r *EgoRunnerRegistry) SetDisconnectHook(fn func(ctx context.Context, userID uint)) {
	r.mu.Lock()
	r.hook = fn
	r.mu.Unlock()
}

func (r *EgoRunnerRegistry) onDisconnect(ctx context.Context, userID uint) {
	r.mu.RLock()
	hook := r.hook
	r.mu.RUnlock()
	if hook != nil {
		hook(ctx, userID)
	}
}

// RunnerMeta Register 的入参载体（不带锁字段，避免锁拷贝）
type RunnerMeta struct {
	Version    string
	DeviceID   string
	Hostname   string
	EgoVersion string
	Workspace  string
	conn       *websocket.Conn
}

// Register 登记连接；同用户已有连接时执行顶号仲裁（host_registry 同型）：
// 上次 register 10s 内又被顶 ⇒ 抖动拒绝；其余新连接获胜。
func (r *EgoRunnerRegistry) Register(userID uint, m RunnerMeta) (*EgoRunnerConn, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.conns[userID]; ok {
		if time.Since(old.lastSeen()) < 10*time.Second {
			return nil, "takeover_thrash_refused", errors.New("同用户 runner 连接抖动，拒绝新连接")
		}
		_ = old.conn.Close()
		delete(r.conns, userID)
	}
	c := &EgoRunnerConn{
		UserID:       userID,
		Version:      m.Version,
		DeviceID:     m.DeviceID,
		Hostname:     m.Hostname,
		EgoVersion:   m.EgoVersion,
		Workspace:    m.Workspace,
		conn:         m.conn,
		reg:          r,
		registeredAt: time.Now(),
	}
	r.conns[userID] = c
	return c, "registered", nil
}

func (c *EgoRunnerConn) lastSeen() time.Time { return c.registeredAt }

// Unregister 断连清理（WS handler 读循环退出时调用）
func (r *EgoRunnerRegistry) Unregister(ctx context.Context, userID uint, c *EgoRunnerConn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.conns[userID]; ok && cur == c {
		delete(r.conns, userID)
		go r.onDisconnect(ctx, userID)
		return true
	}
	return false
}

// GetConn 取该用户的 runner 连接（下发 task spec 前判在线）
func (r *EgoRunnerRegistry) GetConn(userID uint) (*EgoRunnerConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[userID]
	return c, ok
}

// BroadcastUpgrade 向全部在线 runner 下发 ego-browser 升级指令（specs/015 C10；
// runner 侧执行 `ego-browser upgrade` 并回传结果事件）。返回成功送达的连接数。
func (r *EgoRunnerRegistry) BroadcastUpgrade() int {
	r.mu.RLock()
	conns := make([]*EgoRunnerConn, 0, len(r.conns))
	for _, c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.RUnlock()
	n := 0
	for _, c := range conns {
		if err := c.SendSpec(map[string]any{"type": "upgrade"}); err == nil {
			n++
		}
	}
	return n
}

// EnsureOnline 下发前判在线（ErrEgoRunnerOffline → controller 409）
func (r *EgoRunnerRegistry) EnsureOnline(userID uint) error {
	if _, ok := r.GetConn(userID); !ok {
		return ErrEgoRunnerOffline
	}
	return nil
}

// StatusFor 状态查询（admin 全量 / 用户看自己，HostController.GetStatus 同型响应形状）
func (r *EgoRunnerRegistry) StatusFor(userID uint, all bool) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hosts := make([]map[string]any, 0, len(r.conns))
	count := 0
	for uid, c := range r.conns {
		if !all && uid != userID {
			continue
		}
		count++
		hosts = append(hosts, map[string]any{
			"user_id":     uid,
			"version":     c.Version,
			"device_id":   c.DeviceID,
			"hostname":    c.Hostname,
			"ego_version": c.EgoVersion,
			"workspace":   c.Workspace,
		})
	}
	return map[string]any{"count": count, "runners": hosts}
}
