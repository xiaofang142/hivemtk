package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"hivemtk-user/internal/channelbot/onebot"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// onebot_ws.go OneBot v11 反向 WebSocket 连接表。
//
// 协议端（NapCat/Lagrange）主动连到 /ws/onebot/:account_id，服务端借此把
// send_private_msg/send_group_msg 直接推过去，省掉 HTTP 回环；没有连接时
// sender 自动回落 HTTP API。
//
// 连接纪律：
//   - 同一 self_id 重连要顶掉旧连接（协议端重启后旧 TCP 可能还挂着）；
//   - 连接表以 account_id 为键，Send 找不到即回落；
//   - 读循环只做心跳/存活维护，业务消息仍以 HTTP 上报为准。

const oneBotWSPongWait = 90 * time.Second

// OneBotWSConn 单个反向 WS 连接（写操作串行化）。
type OneBotWSConn struct {
	accountID uint
	selfID    string
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once

	// FA-CHANNEL-02：echo → 平台响应回执通道，发送注册、读循环投递。
	pendingMu   sync.Mutex
	pending     map[string]chan oneBotWSReply
	echoSeq     uint64 // pendingMu 保护，生成稳定唯一 echo
	sendTimeout time.Duration // 0 = 默认 30s；测试可注入短超时
}

// oneBotWSReply OneBot v11 动作响应（带 echo 关联）。
// status/retcode 区分「平台已接受」与「平台拒绝」，data.message_id 是业务回执。
type oneBotWSReply struct {
	Status  string         `json:"status"`
	RetCode int            `json:"retcode"`
	Data    map[string]any `json:"data"`
	Message string         `json:"message"`
	Echo    string         `json:"echo"`
}

const defaultOneBotWSSendTimeout = 30 * time.Second

// Close 关闭连接（幂等）。
func (c *OneBotWSConn) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}

// WriteJSON 串行写一帧 JSON。
func (c *OneBotWSConn) WriteJSON(v any) error {
	if c == nil {
		return errors.New("onebot ws conn nil")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// OneBotWSManager 反向 WS 连接表。
type OneBotWSManager struct {
	mu       sync.RWMutex
	byID     map[uint]*OneBotWSConn
	bySelfID map[string]*OneBotWSConn
	onOnline func(accountID uint, online bool)
}

func newOneBotWSManager() *OneBotWSManager {
	return &OneBotWSManager{
		byID:     map[uint]*OneBotWSConn{},
		bySelfID: map[string]*OneBotWSConn{},
	}
}

// SetOnlineHook 连接/断开回调（用于刷新 last_online_at；测试可注入）。
func (m *OneBotWSManager) SetOnlineHook(fn func(accountID uint, online bool)) {
	m.mu.Lock()
	m.onOnline = fn
	m.mu.Unlock()
}

// Register 登记连接：同 account 或同 self_id 的旧连接会被顶掉。
func (m *OneBotWSManager) Register(accountID uint, selfID string, conn *websocket.Conn) *OneBotWSConn {
	c := &OneBotWSConn{accountID: accountID, selfID: selfID, conn: conn, closed: make(chan struct{}), pending: make(map[string]chan oneBotWSReply)}
	m.mu.Lock()
	old := m.byID[accountID]
	if old == nil && selfID != "" {
		old = m.bySelfID[selfID]
	}
	m.byID[accountID] = c
	if selfID != "" {
		if prev, ok := m.bySelfID[selfID]; ok && prev != c {
			// 同 self_id 换账号重连：旧账号在 byID 里的表项也必须摘掉，
			// 否则 Connect(旧账号) 会对着已关闭连接报 true。
			if m.byID[prev.accountID] == prev {
				delete(m.byID, prev.accountID)
			}
			prev.Close()
		}
		m.bySelfID[selfID] = c
	}
	hook := m.onOnline
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if hook != nil {
		hook(accountID, true)
	}
	return c
}

// Unregister 仅当表内仍是同一连接时移除（避免旧连接断开时误删新连接）。
func (m *OneBotWSManager) Unregister(accountID uint, c *OneBotWSConn) {
	m.mu.Lock()
	if m.byID[accountID] == c {
		delete(m.byID, accountID)
		if c.selfID != "" && m.bySelfID[c.selfID] == c {
			delete(m.bySelfID, c.selfID)
		}
	}
	hook := m.onOnline
	m.mu.Unlock()
	c.Close()
	if hook != nil {
		hook(accountID, false)
	}
}

// Connected 账号当前是否有活跃反向 WS 连接。
func (m *OneBotWSManager) Connected(accountID uint) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.byID[accountID]
	return ok
}

// Send 通过反向 WS 调 OneBot API（action + params）。
// FA-CHANNEL-02：写成功不等于平台受理——必须等 echo 关联的响应回执；
// 区分「写入失败」「平台拒绝」「结果不明（超时/断线）」，超时/断线不盲重发。
func (m *OneBotWSManager) Send(ctx context.Context, accountID uint, action string, params map[string]any) error {
	_, err := m.sendWithReply(ctx, accountID, action, params)
	return err
}

// sendWithReply 发送动作并等待平台响应，返回平台回执（含 data.message_id）。
func (m *OneBotWSManager) sendWithReply(ctx context.Context, accountID uint, action string, params map[string]any) (*oneBotWSReply, error) {
	if m == nil {
		return nil, errors.New("onebot ws manager nil")
	}
	m.mu.RLock()
	c := m.byID[accountID]
	m.mu.RUnlock()
	if c == nil {
		return nil, errors.New("onebot ws 未连接")
	}

	// 1. 生成唯一 echo 并注册回执通道（注册在写之前，避免响应抢跑丢失）。
	c.pendingMu.Lock()
	c.echoSeq++
	echo := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(c.echoSeq, 10)
	ch := make(chan oneBotWSReply, 1)
	if c.pending == nil {
		c.pending = make(map[string]chan oneBotWSReply)
	}
	c.pending[echo] = ch
	c.pendingMu.Unlock()
	unregister := func() {
		c.pendingMu.Lock()
		delete(c.pending, echo)
		c.pendingMu.Unlock()
	}

	payload := onebot.BuildWSPayload(action, params, echo)
	var frame map[string]any
	if err := json.Unmarshal(payload, &frame); err != nil {
		unregister()
		return nil, err
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := c.WriteJSON(frame); err != nil {
		unregister()
		return nil, fmt.Errorf("onebot ws 写入失败: %w", err)
	}

	// 2. 等待平台响应：区分接受 / 拒绝 / 结果不明，且绝不自动重发。
	timeout := c.sendTimeout
	if timeout <= 0 {
		timeout = defaultOneBotWSSendTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case reply := <-ch:
		unregister()
		if reply.Status != "ok" || reply.RetCode != 0 {
			return &reply, fmt.Errorf("onebot 平台拒绝: status=%s retcode=%d message=%s", reply.Status, reply.RetCode, reply.Message)
		}
		return &reply, nil
	case <-ctx.Done():
		unregister()
		return nil, fmt.Errorf("onebot 结果不明（上下文取消，未重发）: %w", ctx.Err())
	case <-c.closed:
		unregister()
		return nil, errors.New("onebot 结果不明（连接断开，未重发）")
	case <-timer.C:
		unregister()
		return nil, fmt.Errorf("onebot 结果不明（等待平台响应超时 %s，未重发）", timeout)
	}
}

// RunReadLoop 读循环：刷新 pong 期限，按 echo 把平台响应投递给等待中的 Send；
// 无 echo 的事件帧/心跳帧忽略，未知 echo 丢弃；断开时自动注销（唤醒等待者）。
func (m *OneBotWSManager) RunReadLoop(accountID uint, c *OneBotWSConn) {
	defer m.Unregister(accountID, c)
	_ = c.conn.SetReadDeadline(time.Now().Add(oneBotWSPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(oneBotWSPongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			logger.Infof("[OneBotWS] account=%d read loop exit: %v", accountID, err)
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(oneBotWSPongWait))

		var reply oneBotWSReply
		if err := json.Unmarshal(data, &reply); err != nil || reply.Echo == "" {
			continue // 事件帧/心跳帧/非 JSON：不参与动作回执
		}
		c.pendingMu.Lock()
		ch := c.pending[reply.Echo]
		c.pendingMu.Unlock()
		if ch != nil {
			select {
			case ch <- reply: // 非阻塞投递，避免慢接收者拖死读循环
			default:
			}
		}
	}
}

// OneBotWSManager 取（并按需初始化）反向 WS 连接表。
func (s *WebhookService) OneBotWSManager() *OneBotWSManager {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onebotWS == nil {
		s.onebotWS = newOneBotWSManager()
		mgr := s.onebotWS
		mgr.SetOnlineHook(func(accountID uint, online bool) {
			if s.onebotRepo == nil {
				return
			}
			acc, err := s.onebotRepo.GetByID(context.Background(), accountID)
			if err != nil || acc == nil {
				return
			}
			now := time.Now()
			if online {
				acc.LastOnlineAt = &now
			}
			_ = s.onebotRepo.Update(context.Background(), acc)
		})
	}
	return s.onebotWS
}

// OneBotAccountForWS 反向 WS 握手取账号（不存在返回 nil）。
func (s *WebhookService) OneBotAccountForWS(ctx context.Context, accountID string) *model.OneBotAccount {
	return s.oneBotAccount(ctx, accountID)
}
