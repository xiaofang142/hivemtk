package service

import (
	"sync"
	"time"
)

// LM-09：按账号/会话保存待分析水位；内存态只做唤醒，耐久所有权仍归 BridgeLeadTask。
// 「仅入队」不得被当成分析完成：每个水位需显式 NoteJudged 才从待处理移除。
type LeadTailWatermark struct {
	mu     sync.Mutex
	states map[string]*leadTailState
}

type leadTailState struct {
	watermark uint64
	lastAt    time.Time
	pending   map[uint64]bool
}

func NewLeadTailWatermark() *LeadTailWatermark {
	return &LeadTailWatermark{states: make(map[string]*leadTailState)}
}

func (w *LeadTailWatermark) state(key string) *leadTailState {
	if w.states == nil {
		w.states = make(map[string]*leadTailState)
	}
	s := w.states[key]
	if s == nil {
		s = &leadTailState{pending: make(map[uint64]bool)}
		w.states[key] = s
	}
	if s.pending == nil {
		s.pending = make(map[uint64]bool)
	}
	return s
}

// NoteEnqueued 记录一次入队；水位只前进，旧消息迟到不回退。
func (w *LeadTailWatermark) NoteEnqueued(key string, seq uint64, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.state(key)
	s.pending[seq] = true
	if seq >= s.watermark && !at.Before(s.lastAt) {
		s.watermark, s.lastAt = seq, at
	}
}

// NoteQueueFull 队列满时保留请求；真实消费者可据水位补扫，不静默丢弃。
func (w *LeadTailWatermark) NoteQueueFull(key string, seq uint64, at time.Time) {
	w.NoteEnqueued(key, seq, at)
}

// NoteJudged 耐久保存判定后才移除待处理；迟到旧任务不影响新水位。
func (w *LeadTailWatermark) NoteJudged(key string, seq uint64, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.state(key)
	delete(s.pending, seq)
	if seq > s.watermark && at.After(s.lastAt) {
		s.watermark, s.lastAt = seq, at
	}
}

func (w *LeadTailWatermark) Watermark(key string) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state(key).watermark
}

func (w *LeadTailWatermark) Pending(key string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.state(key).pending)
}

func (w *LeadTailWatermark) HasUnfinished(key string) bool {
	return w.Pending(key) > 0
}

// ShouldFlush 最大等待有确定结果：最后一条入队后超过 maxWait 即触发尾判。
func (w *LeadTailWatermark) ShouldFlush(key string, now time.Time, maxWait time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.state(key)
	return len(s.pending) > 0 && !s.lastAt.IsZero() && now.Sub(s.lastAt) >= maxWait
}
