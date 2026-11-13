package sla

import (
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/pkg/metrics"
)

// SLO SLO 定义
type SLO struct {
	Name        string
	Service     string
	SLITarget   float64
	Window      time.Duration
	Description string
	Evaluator   func(s SLOState) float64
}

// SLOState 当前 SLO 状态
type SLOState struct {
	Name           string
	TotalEvents    uint64
	SuccessEvents  uint64
	FailureEvents  uint64
	WindowStart    time.Time
	WindowEnd      time.Time
	Achievement    float64
	SLITarget      float64
	AllowedFailure uint64
	Remaining      uint64
	BudgetUsed     float64
}

// SLOTracker SLO 跟踪器
type SLOTracker struct {
	mu       sync.RWMutex
	defs     map[string]SLO
	counters map[string]*sloCounter
	alertCB  func(SLO, SLOState)
}

type sloCounter struct {
	mu         sync.Mutex
	window     time.Duration
	buckets    []sloBucket
	bucketSize time.Duration
	currentIdx int
	lastRotate time.Time
	curTotal   atomic.Uint64
	curSuccess atomic.Uint64
}

type sloBucket struct {
	Start   time.Time
	Total   uint64
	Success uint64
}

// NewSLOTracker 创建 SLO 跟踪器
func NewSLOTracker() *SLOTracker {
	return &SLOTracker{
		defs:     make(map[string]SLO),
		counters: make(map[string]*sloCounter),
	}
}

// Define 声明一个 SLO
func (t *SLOTracker) Define(s SLO) {
	if s.Window == 0 {
		s.Window = 30 * 24 * time.Hour
	}
	if s.Evaluator == nil {
		s.Evaluator = defaultEvaluator
	}
	t.mu.Lock()
	t.defs[s.Name] = s
	if _, ok := t.counters[s.Name]; !ok {
		bucketSize := s.Window / 100
		t.counters[s.Name] = &sloCounter{
			window:     s.Window,
			bucketSize: bucketSize,
			lastRotate: time.Now(),
		}
	}
	t.mu.Unlock()
}

// Record 记录一个事件（success=true/false）
func (t *SLOTracker) Record(name string, success bool) {
	t.mu.RLock()
	s, ok := t.defs[name]
	t.mu.RUnlock()
	if !ok {
		return
	}
	t.mu.RLock()
	counter := t.counters[name]
	t.mu.RUnlock()
	if counter == nil {
		return
	}
	counter.record(success)
	metrics.MustGetCounter("slo_events_total").WithLabel(name, boolStr(success)).Inc()
	state := t.State(name)
	if state.BudgetUsed >= 1.0 {
		if t.alertCB != nil {
			t.alertCB(s, state)
		}
	}
}

// OnBreach 注册 SLO 失败回调
func (t *SLOTracker) OnBreach(cb func(SLO, SLOState)) {
	t.mu.Lock()
	t.alertCB = cb
	t.mu.Unlock()
}

// State 获取 SLO 当前状态
func (t *SLOTracker) State(name string) SLOState {
	t.mu.RLock()
	s, ok := t.defs[name]
	counter := t.counters[name]
	t.mu.RUnlock()
	if !ok || counter == nil {
		return SLOState{}
	}
	stats := counter.aggregate(time.Now())
	state := SLOState{
		Name:          name,
		TotalEvents:   stats.total,
		SuccessEvents: stats.success,
		FailureEvents: stats.total - stats.success,
		WindowStart:   stats.windowStart,
		WindowEnd:     stats.windowEnd,
		SLITarget:     s.SLITarget,
	}
	state.Achievement = s.Evaluator(state)
	if state.TotalEvents == 0 {
		state.AllowedFailure = 0
		state.Remaining = 0
		state.BudgetUsed = 0
	} else {
		state.AllowedFailure = uint64(float64(state.TotalEvents)*(1.0-s.SLITarget) + 0.5)
		if state.FailureEvents > state.AllowedFailure {
			state.Remaining = 0
			state.BudgetUsed = 1.0
		} else if state.AllowedFailure == 0 {
			// AllowedFailure==0（SLITarget=1.0 且无失败）：0/0=NaN 会让
			// json.Marshal 失败、gin 静默写出 200 空响应体（2026-11-13
			// api_verify 实测 /api/manage/slo size=0 的根因）。无失败=预算未耗。
			state.Remaining = 0
			state.BudgetUsed = 0
		} else {
			state.Remaining = state.AllowedFailure - state.FailureEvents
			state.BudgetUsed = float64(state.FailureEvents) / float64(state.AllowedFailure)
		}
	}
	metrics.MustGetGauge("slo_achievement").WithLabel(name).SetFloat(state.Achievement)
	metrics.MustGetGauge("slo_error_budget_remaining").WithLabel(name).Set(int64(state.Remaining))
	metrics.MustGetGauge("slo_error_budget_used").WithLabel(name).SetFloat(state.BudgetUsed)
	return state
}

// AllStates 返回所有 SLO 状态
func (t *SLOTracker) AllStates() []SLOState {
	t.mu.RLock()
	names := make([]string, 0, len(t.defs))
	for n := range t.defs {
		names = append(names, n)
	}
	t.mu.RUnlock()
	out := make([]SLOState, 0, len(names))
	for _, n := range names {
		out = append(out, t.State(n))
	}
	return out
}

type sloAggregate struct {
	total       uint64
	success     uint64
	windowStart time.Time
	windowEnd   time.Time
}

func (c *sloCounter) record(success bool) {
	c.rotateIfNeeded()
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.currentIdx
	if idx >= len(c.buckets) {
		c.buckets = make([]sloBucket, 100)
		for i := range c.buckets {
			c.buckets[i].Start = time.Now().Add(-c.window).Add(time.Duration(i) * c.bucketSize)
		}
		idx = 99
		c.currentIdx = idx
	}
	c.buckets[idx].Total++
	if success {
		c.buckets[idx].Success++
		c.curSuccess.Add(1)
	}
	c.curTotal.Add(1)
}

func (c *sloCounter) rotateIfNeeded() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buckets) == 0 {
		c.buckets = make([]sloBucket, 100)
		for i := range c.buckets {
			c.buckets[i].Start = now.Add(-c.window).Add(time.Duration(i) * c.bucketSize)
		}
		c.currentIdx = 99
		c.lastRotate = now
		return
	}
	elapsed := now.Sub(c.lastRotate)
	if elapsed < c.bucketSize {
		return
	}
	steps := int(elapsed / c.bucketSize)
	if steps >= len(c.buckets) {
		c.buckets = make([]sloBucket, 100)
		c.currentIdx = 99
		c.lastRotate = now
		c.curTotal.Store(0)
		c.curSuccess.Store(0)
		return
	}
	for i := 0; i < steps; i++ {
		c.currentIdx = (c.currentIdx + 1) % len(c.buckets)
		c.buckets[c.currentIdx] = sloBucket{Start: now}
	}
	c.lastRotate = now
}

func (c *sloCounter) aggregate(now time.Time) sloAggregate {
	c.rotateIfNeeded()
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := now.Add(-c.window)
	var total, success uint64
	var minStart time.Time
	for _, b := range c.buckets {
		if b.Start.Before(cutoff) {
			continue
		}
		total += b.Total
		success += b.Success
		if minStart.IsZero() || b.Start.Before(minStart) {
			minStart = b.Start
		}
	}
	return sloAggregate{
		total:       total,
		success:     success,
		windowStart: minStart,
		windowEnd:   now,
	}
}

func defaultEvaluator(s SLOState) float64 {
	if s.TotalEvents == 0 {
		return 1.0
	}
	return float64(s.SuccessEvents) / float64(s.TotalEvents)
}

func boolStr(b bool) string {
	if b {
		return "success"
	}
	return "failure"
}

var initOnce sync.Once

// InitMetrics 注册 SLO 指标（私域部署用于应用层日志与采集）
func InitMetrics() {
	initOnce.Do(func() {
		metrics.NewCounter("slo_events_total", "Total SLO events by SLO name and result",
			[]string{"slo", "result"})
		metrics.NewGauge("slo_achievement", "Current SLO achievement ratio (0-1)",
			[]string{"slo"})
		metrics.NewGauge("slo_error_budget_remaining", "Remaining error budget (count)",
			[]string{"slo"})
		metrics.NewGauge("slo_error_budget_used", "Error budget used ratio (0-1)",
			[]string{"slo"})
	})
}
