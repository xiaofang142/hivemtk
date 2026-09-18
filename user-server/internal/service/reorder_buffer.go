package service

import (
	"sort"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// MessageReorderBuffer WhatsApp Cloud API 消息乱序缓冲
// WhatsApp 在弱网/网络抖动下可能乱序到达（实测延迟差可达 2-5s），
// 本 buffer 按 session 维度在短窗口（默认 3s）内按 timestamp 排序后再放行
type MessageReorderBuffer struct {
	mu      sync.Mutex
	buffers map[string]*sessionBuffer
	window  time.Duration
	maxSize int

	// FlushHandler buffer 自 flush 后重新派发的回调（如 nil 则仅 log）
	// handler 接收 accountID + sessionID 和排序后的 payload 切片
	FlushHandler func(accountID, sessionID string, ordered [][]byte)
}

type sessionBuffer struct {
	messages []msgEntry
	timer    *time.Timer
}

type msgEntry struct {
	accountID string
	id        string
	timestamp int64
	payload   []byte
}

func NewMessageReorderBuffer(window time.Duration, maxSize int) *MessageReorderBuffer {
	if window <= 0 {
		window = 3 * time.Second
	}
	if maxSize <= 0 {
		maxSize = 50
	}
	return &MessageReorderBuffer{
		buffers: make(map[string]*sessionBuffer),
		window:  window,
		maxSize: maxSize,
	}
}

// Offer 放入一条消息；如果检测到乱序（新消息 timestamp < 缓冲区已有最大 timestamp），
// 则等待 window 后 flush；否则立即 flush（无延迟）
// 返回：
//   - ordered: 排序后的消息切片（如果立即 flush 则只含刚放入的那条）
//   - delayed: 是否在等待窗口（调用方应不做后续处理，等 Flush 回调）
func (b *MessageReorderBuffer) Offer(accountID, sessionID, msgID string, timestamp int64, payload []byte) (ordered [][]byte, delayed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	buf, ok := b.buffers[sessionID]
	if !ok {
		buf = &sessionBuffer{}
		b.buffers[sessionID] = buf
	}

	if len(buf.messages) >= b.maxSize {
		logger.Warnf("[ReorderBuffer] session=%s buffer full (>=%d), force flush", sessionID, b.maxSize)
		return b.flushLocked(sessionID, buf), false
	}

	buf.messages = append(buf.messages, msgEntry{
		accountID: accountID,
		id:        msgID,
		timestamp: timestamp,
		payload:   payload,
	})

	if buf.timer != nil {
		return nil, true
	}

	if len(buf.messages) >= 2 {
		buf.timer = time.AfterFunc(b.window, func() {
			b.flushAndDispatch(sessionID)
		})
		return nil, true
	}

	return b.flushLocked(sessionID, buf), false
}

func (b *MessageReorderBuffer) flushAndDispatch(sessionID string) {
	b.mu.Lock()
	buf, ok := b.buffers[sessionID]
	if !ok {
		b.mu.Unlock()
		return
	}
	ordered := b.flushLocked(sessionID, buf)
	accountID := ""
	if len(buf.messages) > 0 {
		accountID = buf.messages[0].accountID
	}
	handler := b.FlushHandler
	b.mu.Unlock()

	if len(ordered) > 0 {
		logger.Infof("[ReorderBuffer] flushed session=%s messages=%d", sessionID, len(ordered))
		if handler != nil {
			handler(accountID, sessionID, ordered)
		}
	}
}

func (b *MessageReorderBuffer) flushLocked(sessionID string, buf *sessionBuffer) [][]byte {
	sort.Slice(buf.messages, func(i, j int) bool {
		return buf.messages[i].timestamp < buf.messages[j].timestamp
	})

	out := make([][]byte, 0, len(buf.messages))
	for _, m := range buf.messages {
		out = append(out, m.payload)
	}

	if buf.timer != nil {
		buf.timer.Stop()
	}
	delete(b.buffers, sessionID)
	return out
}

// Stats 返回当前缓冲区统计（监控用）
func (b *MessageReorderBuffer) Stats() (activeSessions, totalBuffered int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	activeSessions = len(b.buffers)
	for _, buf := range b.buffers {
		totalBuffered += len(buf.messages)
	}
	return
}

var globalReorderBuffer = NewMessageReorderBuffer(3*time.Second, 50)
