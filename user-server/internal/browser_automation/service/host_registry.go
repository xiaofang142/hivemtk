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

	// F3（批5e）「注册在线」与「可服务」在协议上是两件事：真机 session237/238/253 三次实测
	// ——host 完成 register、host/status 报 count=1/版本正常，而命令帧有去无回（扩展 SW 端口面
	// 失效 / 实例更换后旧 host 带陈旧端口重注册），每条命令烧满 30s 超时，代价固定 2×30s。
	// servable=最近一次「应用面确曾回包」的证据：注册探针通过或任一命令回包即置真，注册时置假。
	servableMu  sync.Mutex
	servable    bool
	lastCmdOKAt time.Time

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

// 心跳常量见 timeouts.go（A6 收口单表；D4a/G4 语义：半开 TCP 无应用层探测时，
// Request 挂满命令超时且断连清理钩子不触发——ping/pong + 读超时把它变确定事件）。

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
			_ = c.conn.SetWriteDeadline(time.Now().Add(hostWriteDeadline))
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
	_ = c.conn.SetWriteDeadline(time.Now().Add(hostWriteDeadline))
	return c.conn.WriteJSON(v)
}

// readLoop 读循环：按 req_id 投递回包；连接断开时收尾。
// D4a：读超时 90s，收到 pong 即重置——僵尸连接最迟 90s 判定并触发清理钩子。
func (c *HostConn) readLoop() {
	defer c.close()
	c.conn.SetReadLimit(hostFrameReadLimit) // 批9：与 cmd/nm-host 的边缘上限配对，见 timeouts.go 常量注释
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
	ctx, cancel := context.WithTimeout(context.Background(), hostDisconnectHookTimeout)
	defer cancel()
	r.onDisconnectFn(ctx, userID)
}

// Register 登记连接；同用户旧连接存在则顶掉（Chrome 重启场景）
func (r *HostRegistry) Register(userID uint, version string, pid int, conn *websocket.Conn) *HostConn {
	hc := newHostConn(userID, version, pid, conn, r)
	r.mu.Lock()
	if old, ok := r.conns[userID]; ok && old != hc {
		// F1：驱逐是「谁把谁顶掉」的关键现场——扩展在服务的连接被一次手工启动抢走时，
		// 只看到超时看不到替换；两个 pid 同排才归因得出来。
		logger.Warnf("[BrowserHost] Host 重复注册 user=%d：驱逐旧连接 version=%s pid=%d，新 version=%s pid=%d",
			userID, old.Version, old.PID, version, pid)
		go old.close() // 旧连接退出（其 close 会在 unregister 中因指针不等而跳过）
	}
	r.conns[userID] = hc
	r.mu.Unlock()
	logger.Infof("[BrowserHost] Host 已注册 user=%d version=%s pid=%d", userID, version, pid)
	go hc.readLoop()
	go hc.pingLoop() // D4a：心跳探测，僵尸连接最迟 90s 判死
	go r.probeServable(hc)
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

// ConnectedUserIDs 本进程当前持有 Host 连接的用户集（批9 重试归属门用）。
// 注意口径是「连接在本进程」，不是「servable」：servable 是探针给的短期健康信号，
// 归属门要回答的是「这条重试只有我能跑」，连接在就归我管，探针未过自有下发路径归因。
func (r *HostRegistry) ConnectedUserIDs() []uint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]uint, 0, len(r.conns))
	for uid := range r.conns {
		out = append(out, uid)
	}
	return out
}

// Status 概览（admin 用）
func (r *HostRegistry) Status() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]map[string]any, 0, len(r.conns))
	for _, c := range r.conns {
		out = append(out, c.statusFields())
	}
	return out
}

// MyStatus 当前用户的 Host 状态（普通登录用户视角，只读自己）
func (r *HostRegistry) MyStatus(userID uint) map[string]any {
	return r.StatusFor(userID, false)
}

// StatusFor host/status 统一响应形状：无论 admin 还是普通用户，count/hosts/online/servable
// 恒在。扩展 popup 的状态点判据是 count>0（历史契约只见过 admin 形状），普通用户分支少给
// count 会让它把「已连接」永久显示成「Host 离线」。
// all=true 看全量（admin），否则只看该用户自己的连接。
func (r *HostRegistry) StatusFor(userID uint, all bool) map[string]any {
	hosts := []map[string]any{}
	if all {
		hosts = append(hosts, r.Status()...)
	} else if c, ok := r.GetConn(userID); ok {
		hosts = append(hosts, c.statusFields())
	}
	var servable bool
	var lastOK any
	for _, h := range hosts {
		if s, _ := h["servable"].(bool); s {
			servable = true
		}
		// last_cmd_ok_at 取全量里最近一次证据（RFC3339 同格式，字符串序即时序）
		if v, _ := h["last_cmd_ok_at"].(string); v != "" {
			p, _ := lastOK.(string)
			if v > p {
				lastOK = v
			}
		}
	}
	return map[string]any{
		"online":         len(hosts) > 0,
		"servable":       servable,
		"last_cmd_ok_at": lastOK,
		"count":          len(hosts),
		"hosts":          hosts,
		"user_id":        userID,
	}
}

// statusFields 状态读出位：online=WS 注册在场，servable=应用面回包证据（见 F3 字段注释）。
// 判「能不能干活」要用 servable + last_cmd_ok_at，online 单独为 true 不构成可服务证明。
func (c *HostConn) statusFields() map[string]any {
	c.servableMu.Lock()
	servable, lastOK := c.servable, c.lastCmdOKAt
	c.servableMu.Unlock()
	var lastOKStr any
	if !lastOK.IsZero() {
		lastOKStr = lastOK.Format(time.RFC3339)
	}
	return map[string]any{
		"online":         true,
		"servable":       servable,
		"last_cmd_ok_at": lastOKStr,
		"user_id":        c.UserID,
		"version":        c.Version,
		"pid":            c.PID,
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
		c.forceSelfHeal("sick_probe")
	}
}

// noteCmdAlive 回包/正常到达即证明应用面存活，清零超时计数。
func (c *HostConn) noteCmdAlive() {
	c.pendingMu.Lock()
	c.cmdTimeouts = 0
	c.pendingMu.Unlock()
	c.markServable()
}

// markServable 记下「应用面确曾回包」的证据与时点（F3 读出位）。
func (c *HostConn) markServable() {
	c.servableMu.Lock()
	c.servable = true
	c.lastCmdOKAt = time.Now()
	c.servableMu.Unlock()
}

// forceSelfHeal 走「让 host 进程退出」的自愈链：WS 活着才发得出控制帧，
// 帧失败也无妨，close 仍回收服务端状态（conn==nil 的单测探针连接只走 close）。
func (c *HostConn) forceSelfHeal(reason string) {
	if c.conn != nil {
		_ = c.writeJSON(map[string]any{"action": "__host_shutdown__", "reason": reason, "req_id": ""})
	}
	c.close()
}

// probeServable 注册期可服务探针（F3）：注册成功只证明 WS 与 register 帧到达，
// 不证明 stdio→扩展→回传这条应用面链能走通——真机三次实测「在线但不可服务」，
// 首个用户任务因此白烧 2×30s 才由 A5 判病自愈。这里用零副作用最轻原语
// `tab_exists(tab_id=0)`（扩展侧 !tabId → 直接 {exists:false}，不碰任何 tab、不查登录态）
// 把「不可服务」的发现成本从 60s 用户可见失败压到探针超时，并复用同一条自愈链。
// 探针超时不走 Request → 不喂 cmdTimeouts 计数（判病结论由本函数自己负责，避免二次计数）。
func (r *HostRegistry) probeServable(c *HostConn) {
	reqID := uuid.NewString()
	ch := c.registerPending(reqID)
	defer c.removePending(reqID)
	if err := c.writeJSON(map[string]any{"action": "tab_exists", "tab_id": 0, "req_id": reqID}); err != nil {
		return // 写不进去=连接已死，readLoop/pingLoop 那条路会收尾
	}
	select {
	case res := <-ch:
		// 判据是「有无回包」而不是「回包是否成功」：应用面假死的形态就是帧有去无回；
		// 只要帧走通了 host→扩展→host→WS 整条链，即便扩展报错（如极旧 bundle 不认
		// tab_exists）也证明这条链可服务——据此判死会制造重启活锁。
		c.markServable()
		if !res.OK {
			logger.Warnf("[BrowserHost] 注册探针回包异常但链路可用 user=%d pid=%d: %s", c.UserID, c.PID, res.Error)
			return
		}
		logger.Infof("[BrowserHost] 注册探针通过（应用面可服务）user=%d pid=%d", c.UserID, c.PID)
		return
	case <-time.After(hostServProbeTimeout):
		logger.Warnf("[BrowserHost] 注册探针 %s 无回包：应用面不可服务，下发 shutdown 帧+断开连接提前自愈（不等用户任务去撞 2×30s）user=%d pid=%d",
			hostServProbeTimeout, c.UserID, c.PID)
	case <-c.closed:
		return
	}
	c.forceSelfHeal("unservable_probe")
}
