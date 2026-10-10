package agent_runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type TokenBudget struct {
	mu            sync.RWMutex
	maxPerSession int64
	used          atomic.Int64
	windowSize    time.Duration
	maxPerWindow  int64
	windowStart   time.Time
	windowUsed    atomic.Int64
	windowHistory []windowRecord
	totalConsumed atomic.Int64
	totalRequests atomic.Int64
	blockedCount  atomic.Int64
}

type windowRecord struct {
	start time.Time
	used  int64
}

type TokenBudgetConfig struct {
	MaxPerSession       int64 `json:"max_per_session"`
	MaxPerMinute        int64 `json:"max_per_minute"`
	MaxTokensPerRequest int64 `json:"max_tokens_per_request"`
}

var DefaultTokenBudgetConfig = TokenBudgetConfig{
	MaxPerSession:       100000,
	MaxPerMinute:        500000,
	MaxTokensPerRequest: 8000,
}

func (b *TokenBudget) Consume(tokens int64) error {
	if tokens <= 0 {
		return nil
	}

	if b.maxPerSession > 0 && tokens > b.maxPerSession {
		b.blockedCount.Add(1)
		return &BudgetExceededError{
			Reason:    "per_request_limit",
			Limit:     b.maxPerSession,
			Requested: tokens,
		}
	}

	if b.maxPerSession > 0 {
		newUsed := b.used.Add(tokens)
		if newUsed > b.maxPerSession {
			b.used.Add(-tokens)
			b.blockedCount.Add(1)
			return &BudgetExceededError{
				Reason:    "session_limit",
				Limit:     b.maxPerSession,
				Used:      newUsed - tokens,
				Requested: tokens,
			}
		}
	} else {
		b.used.Add(tokens)
	}

	if err := b.checkWindow(tokens); err != nil {
		b.used.Add(-tokens)
		b.blockedCount.Add(1)
		return err
	}

	b.totalConsumed.Add(tokens)
	b.totalRequests.Add(1)
	return nil
}

func (b *TokenBudget) checkWindow(tokens int64) error {
	if b.maxPerWindow <= 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	windowStart := b.windowStart

	if now.Sub(windowStart) >= b.windowSize {
		b.windowStart = now
		b.windowUsed.Store(0)
	}

	newWindowUsed := b.windowUsed.Add(tokens)
	if newWindowUsed > b.maxPerWindow {
		b.windowUsed.Add(-tokens)
		return &BudgetExceededError{
			Reason:    "window_limit",
			Limit:     b.maxPerWindow,
			Used:      newWindowUsed - tokens,
			Requested: tokens,
		}
	}

	b.windowHistory = append(b.windowHistory, windowRecord{
		start: now,
		used:  tokens,
	})

	cutoff := now.Add(-b.windowSize)
	i := 0
	for ; i < len(b.windowHistory); i++ {
		if b.windowHistory[i].start.After(cutoff) || b.windowHistory[i].start.Equal(cutoff) {
			break
		}
	}
	if i > 0 {
		b.windowHistory = b.windowHistory[i:]
	}

	return nil
}

func (b *TokenBudget) Stats() TokenBudgetStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return TokenBudgetStats{
		UsedPerSession:  b.used.Load(),
		MaxPerSession:   b.maxPerSession,
		UsedPerWindow:   b.windowUsed.Load(),
		MaxPerWindow:    b.maxPerWindow,
		TotalConsumed:   b.totalConsumed.Load(),
		TotalRequests:   b.totalRequests.Load(),
		BlockedCount:    b.blockedCount.Load(),
		WindowStartTime: b.windowStart,
	}
}

func (b *TokenBudget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.used.Store(0)
	b.windowUsed.Store(0)
	b.totalConsumed.Store(0)
	b.totalRequests.Store(0)
	b.blockedCount.Store(0)
	b.windowStart = time.Now()
	b.windowHistory = make([]windowRecord, 0, 60)
}

func (b *TokenBudget) SetLimit(maxPerSession, maxPerWindow int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.maxPerSession = maxPerSession
	b.maxPerWindow = maxPerWindow
}

type TokenBudgetStats struct {
	UsedPerSession  int64     `json:"used_per_session"`
	MaxPerSession   int64     `json:"max_per_session"`
	UsedPerWindow   int64     `json:"used_per_window"`
	MaxPerWindow    int64     `json:"max_per_window"`
	TotalConsumed   int64     `json:"total_consumed"`
	TotalRequests   int64     `json:"total_requests"`
	BlockedCount    int64     `json:"blocked_count"`
	WindowStartTime time.Time `json:"window_start_time"`
}

type BudgetExceededError struct {
	Reason    string `json:"reason"`
	Limit     int64  `json:"limit"`
	Used      int64  `json:"used"`
	Requested int64  `json:"requested"`
}

func (e *BudgetExceededError) Error() string {
	switch e.Reason {
	case "session_limit":
		return fmt.Sprintf("session token budget exceeded: used=%d, limit=%d, requested=%d", e.Used, e.Limit, e.Requested)
	case "window_limit":
		return fmt.Sprintf("window token budget exceeded: used=%d, limit=%d, requested=%d", e.Used, e.Limit, e.Requested)
	case "per_request_limit":
		return fmt.Sprintf("per-request token limit exceeded: limit=%d, requested=%d", e.Limit, e.Requested)
	default:
		return fmt.Sprintf("token budget exceeded: %s", e.Reason)
	}
}
