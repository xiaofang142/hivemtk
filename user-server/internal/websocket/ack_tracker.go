package websocket

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
)

// PendingAck 待 ACK 跟踪表
// key: 会话 ID（visitor/agent 都用）
// value: seq -> 首次发送时间
type PendingAck struct {
	mu    sync.RWMutex
	items map[string]map[uint64]time.Time
}

// NewPendingAck 创建 ACK 跟踪表
func NewPendingAck() *PendingAck {
	return &PendingAck{items: make(map[string]map[uint64]time.Time)}
}

// Track 记录一条待 ACK 消息
func (p *PendingAck) Track(sessionID string, seq uint64) {
	if p == nil || sessionID == "" || seq == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.items[sessionID]; !ok {
		p.items[sessionID] = make(map[uint64]time.Time)
	}
	p.items[sessionID][seq] = time.Now()

	if snapshot, ok := p.items[sessionID]; ok {
		pendingRedis.asyncSetJSON(sessionID, snapshot)
	}
}

// Ack 客户端确认收到 seq，可传多个（批量 ACK）
//
// 返回被清理的 seq 数量。
func (p *PendingAck) Ack(sessionID string, seqs ...uint64) int {
	if p == nil || sessionID == "" {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pending, ok := p.items[sessionID]
	if !ok {
		return 0
	}
	count := 0
	for _, s := range seqs {
		if _, exists := pending[s]; exists {
			delete(pending, s)
			count++
		}
	}

	pendingRedis.asyncSetJSON(sessionID, pending)
	if len(pending) == 0 {
		delete(p.items, sessionID)
	}
	return count
}

// Pending 列出该 sessionID 下所有未 ACK 的 seq（按 firstSeen 升序）
func (p *PendingAck) Pending(sessionID string) []uint64 {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	pending, ok := p.items[sessionID]
	if !ok {
		return nil
	}
	out := make([]uint64, 0, len(pending))
	for s := range pending {
		out = append(out, s)
	}
	return out
}

// PendingSince 拉取 seq > sinceSeq 的所有未 ACK（用于重连后查缺补漏）
func (p *PendingAck) PendingSince(sessionID string, sinceSeq uint64) []uint64 {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	merged := make(map[uint64]time.Time, len(p.items[sessionID]))
	for s, ts := range p.items[sessionID] {
		merged[s] = ts
	}
	p.mu.RUnlock()

	for s, ts := range pendingRedis.loadRemote(sessionID) {
		if _, exists := merged[s]; !exists {
			merged[s] = ts
		}
	}
	if len(merged) == 0 {
		return nil
	}
	out := make([]uint64, 0, len(merged))
	for s := range merged {
		if s > sinceSeq {
			out = append(out, s)
		}
	}
	return out
}

// Drop 客户端断开时清空该 sessionID 的所有 pending（避免内存泄漏）
func (p *PendingAck) Drop(sessionID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, sessionID)
}

var globalAckTracker = NewPendingAck()

// GlobalPendingAck 获取全局 ACK 跟踪器（供 visitor_handler.go 使用）
func GlobalPendingAck() *PendingAck {
	return globalAckTracker
}

type pendingRedisBacked struct {
	enabled func() bool
	backend func() cache.Cache
}

var pendingRedis = &pendingRedisBacked{
	enabled: func() bool { return seqIsRedis() && !redisDegraded.Load() },
	backend: func() cache.Cache { return seqBackend() },
}

func pendingKey(sessionID string) string { return "mtk:ws:pending:" + sessionID }

func (p *pendingRedisBacked) asyncSetJSON(sessionID string, snapshot map[uint64]time.Time) {
	if !p.enabled() {
		return
	}
	c := p.backend()
	if c == nil {
		return
	}
	// snapshot 可能直接别名调用方（Track/Ack）在持有 p.mu 时传入的实时 map
	// p.items[sessionID]。异步 goroutine 在 p.mu 之外对 snapshot 做 json.Marshal(map)，
	// 与后续 Track/Ack 对该 map 的写入并发 → 数据竞争 + fatal: concurrent map
	// iteration and map write（不可 recover，整个进程崩溃）。因此必须在派生 goroutine
	// 之前（仍在调用方锁内）做一次深拷贝，让后台只读私有副本。
	if snapshot == nil {
		return
	}
	isolated := make(map[uint64]time.Time, len(snapshot))
	for k, v := range snapshot {
		isolated[k] = v
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = c.SetJSON(ctx, pendingKey(sessionID), isolated, 24*time.Hour)
	}()
}

func (p *pendingRedisBacked) loadRemote(sessionID string) map[uint64]time.Time {
	if !p.enabled() {
		return nil
	}
	c := p.backend()
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var remote map[uint64]time.Time
	if err := c.GetJSON(ctx, pendingKey(sessionID), &remote); err != nil {
		return nil
	}
	return remote
}
