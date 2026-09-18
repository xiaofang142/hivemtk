package service

// reach_send_pipeline_components.go 发送流水线各接口的进程内实现与适配器：
// 内存分片令牌桶限流器、内存审计日志、内存成本追踪、旅程追踪桥接、
// No-Op 实现，以及函数式/常败/不稳定渠道适配器（真实实现，非 mock 框架）。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type AllowAllSendPermission struct{}

func (AllowAllSendPermission) CheckSendPermission(ctx context.Context, req *ReachSendRequest) error {
	return nil
}

type NoOpSendRateLimiter struct{}

func (NoOpSendRateLimiter) Allow(ctx context.Context, key string, limit RateLimitSpec) bool {
	return true
}

type MemorySendRateLimiter struct {
	shards [rateLimiterShards]*rateLimiterShard
}

type rateLimiterShard struct {
	mu      sync.Mutex
	buckets map[string]*sendRateBucket
}

type sendRateBucket struct {
	tokens   float64
	lastFill time.Time
	qps      int
	burst    int
}

const (
	rateLimiterShards = 64

	rateLimiterBucketIdleTTL = 10 * time.Minute
)

var rateLimiterMaxBuckets = 4096

func NewMemorySendRateLimiter() *MemorySendRateLimiter {
	l := &MemorySendRateLimiter{}
	for i := range l.shards {
		l.shards[i] = &rateLimiterShard{buckets: make(map[string]*sendRateBucket)}
	}
	return l
}

func (l *MemorySendRateLimiter) shardOf(ctx context.Context, key string) *rateLimiterShard {
	var h uint32
	for i := 0; i < len(key); i++ {
		h = h*31 + uint32(key[i])
	}
	return l.shards[h%rateLimiterShards]
}

func (l *MemorySendRateLimiter) Allow(ctx context.Context, key string, limit RateLimitSpec) bool {
	if limit.QPS <= 0 && limit.Burst <= 0 {
		return true
	}
	s := l.shardOf(ctx, key)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	b, ok := s.buckets[key]
	if !ok || b.qps != limit.QPS || b.burst != limit.Burst || now.Sub(b.lastFill) > rateLimiterBucketIdleTTL {
		if !ok {
			if len(s.buckets) >= rateLimiterMaxBuckets {
				s.evictStalestLocked()
			}
		}
		b = &sendRateBucket{
			tokens:   float64(limit.Burst),
			lastFill: now,
			qps:      limit.QPS,
			burst:    limit.Burst,
		}
		s.buckets[key] = b
	}

	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = math.Min(float64(b.burst), b.tokens+elapsed*float64(b.qps))
	b.lastFill = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (l *MemorySendRateLimiter) totalBucketCount() int { //nolint:unused //// 仅被 *_test.go 引用，生产路径未用
	total := 0
	for i := range l.shards {
		s := l.shards[i]
		s.mu.Lock()
		total += len(s.buckets)
		s.mu.Unlock()
	}
	return total
}

func (s *rateLimiterShard) evictStalestLocked() {
	var staleKey string
	var staleTime time.Time
	first := true
	for k, b := range s.buckets {
		if first || b.lastFill.Before(staleTime) {
			staleKey = k
			staleTime = b.lastFill
			first = false
		}
	}
	if staleKey != "" {
		delete(s.buckets, staleKey)
	}
}

func (l *MemorySendRateLimiter) Reset(ctx context.Context, key string) {
	s := l.shardOf(ctx, key)
	s.mu.Lock()
	delete(s.buckets, key)
	s.mu.Unlock()
}

type MemorySendAuditLogger struct {
	mu      sync.Mutex
	entries []*SendAuditEntry
	maxSize int
}

type SendAuditEntry struct {
	Timestamp  time.Time
	OperatorID string
	Channel    string
	AccountID  string
	Recipient  string
	CustomerID string
	Content    string
	Success    bool
	MessageID  string
	Error      string
	DurationMs int64
}

func NewMemorySendAuditLogger(maxSize int) *MemorySendAuditLogger {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &MemorySendAuditLogger{maxSize: maxSize}
}

func (l *MemorySendAuditLogger) LogSend(ctx context.Context, req *ReachSendRequest, resp *SendResponse) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := &SendAuditEntry{
		Timestamp:  time.Now(),
		OperatorID: req.OperatorID,
		Channel:    req.Channel,
		AccountID:  req.AccountID,
		Recipient:  req.RecipientID,
		CustomerID: req.CustomerID,
		Content:    req.Content,
		Success:    resp.Success,
		MessageID:  resp.MessageID,
		Error:      resp.Error,
		DurationMs: resp.DurationMs,
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.maxSize {
		l.entries = l.entries[len(l.entries)-l.maxSize:]
	}
}

func (l *MemorySendAuditLogger) Entries(ctx context.Context) []*SendAuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*SendAuditEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

type NoOpSendCostTracker struct{}

func (NoOpSendCostTracker) Charge(ctx context.Context, channel string, req *ReachSendRequest) (float64, error) {
	return 0, nil
}

type MemorySendCostTracker struct {
	mu        sync.Mutex
	balance   float64
	costs     map[string]float64
	totalUsed float64
}

func NewMemorySendCostTracker(initialBalance float64) *MemorySendCostTracker {
	return &MemorySendCostTracker{
		balance: initialBalance,
		costs: map[string]float64{
			"sms":      0.05,
			"email":    0.001,
			"wecom":    0,
			"weixin":   0,
			"douyin":   0,
			"kuaishou": 0,
			"xhs":      0,
			"dingtalk": 0,
			"card":     0.01,
		},
	}
}

func (t *MemorySendCostTracker) SetCost(ctx context.Context, channel string, cost float64) {
	t.mu.Lock()
	t.costs[channel] = cost
	t.mu.Unlock()
}

func (t *MemorySendCostTracker) Charge(ctx context.Context, channel string, req *ReachSendRequest) (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cost := t.costs[channel]
	if t.balance < cost {
		return 0, ErrSendInsufficientCost
	}
	t.balance -= cost
	t.totalUsed += cost
	return cost, nil
}

func (t *MemorySendCostTracker) Balance(ctx context.Context) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.balance
}

func (t *MemorySendCostTracker) TotalUsed(ctx context.Context) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalUsed
}

type NoOpSendJourneyTracker struct{}

func (NoOpSendJourneyTracker) RecordTouch(ctx context.Context, customerID, channel, source string) error {
	return nil
}

type CustomerJourneySendTracker struct {
	Service *CustomerJourneyService
}

func (t CustomerJourneySendTracker) RecordTouch(ctx context.Context, customerID, channel, source string) error {
	if t.Service == nil || customerID == "" {
		return nil
	}
	t.Service.Touch(ctx, customerID, source)
	return nil
}

// FuncChannelAdapter 将普通发送函数适配为 ChannelAdapter 接口（真实实现，非 mock）。
type FuncChannelAdapter struct {
	SendFunc func(ctx context.Context, req *ReachSendRequest) (string, error)
	CallCnt  int32
}

// NewFuncChannelAdapter 创建函数式适配器
func NewFuncChannelAdapter(fn func(ctx context.Context, req *ReachSendRequest) (string, error)) *FuncChannelAdapter {
	return &FuncChannelAdapter{SendFunc: fn}
}

// Send 实现 ChannelAdapter
func (m *FuncChannelAdapter) Send(ctx context.Context, req *ReachSendRequest) (string, error) {
	atomic.AddInt32(&m.CallCnt, 1)
	if m.SendFunc != nil {
		return m.SendFunc(ctx, req)
	}
	return fmt.Sprintf("msg-%d", atomic.LoadInt32(&m.CallCnt)), nil
}

// Count 返回调用次数
func (m *FuncChannelAdapter) Count(ctx context.Context) int32 {
	return atomic.LoadInt32(&m.CallCnt)
}

// AlwaysFailAdapter 始终失败的适配器
type AlwaysFailAdapter struct {
	CallCnt int32
	Err     error
}

// NewAlwaysFailAdapter 创建始终失败的适配器
func NewAlwaysFailAdapter(err error) *AlwaysFailAdapter {
	if err == nil {
		err = errors.New("always fail")
	}
	return &AlwaysFailAdapter{Err: err}
}

// Send 实现 ChannelAdapter
func (a *AlwaysFailAdapter) Send(ctx context.Context, req *ReachSendRequest) (string, error) {
	atomic.AddInt32(&a.CallCnt, 1)
	return "", a.Err
}

// Count 返回调用次数
func (a *AlwaysFailAdapter) Count(ctx context.Context) int32 {
	return atomic.LoadInt32(&a.CallCnt)
}

// FlakyAdapter 不稳定适配器（前 N 次失败，之后成功）
type FlakyAdapter struct {
	CallCnt    int32
	FailBefore int32
	Err        error
}

// NewFlakyAdapter 创建不稳定适配器
func NewFlakyAdapter(failBefore int32) *FlakyAdapter {
	return &FlakyAdapter{
		FailBefore: failBefore,
		Err:        errors.New("flaky failure"),
	}
}

// Send 实现 ChannelAdapter
func (a *FlakyAdapter) Send(ctx context.Context, req *ReachSendRequest) (string, error) {
	cnt := atomic.AddInt32(&a.CallCnt, 1)
	if cnt <= a.FailBefore {
		return "", a.Err
	}
	return fmt.Sprintf("msg-flaky-%d", cnt), nil
}

// Count 返回调用次数
func (a *FlakyAdapter) Count(ctx context.Context) int32 {
	return atomic.LoadInt32(&a.CallCnt)
}
