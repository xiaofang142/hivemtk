package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/redis/go-redis/v9"
)

const (
	HumanLockKey       = "hivemtk:lock:human:"
	HumanReasonKey     = "hivemtk:session:escalate_reason:"
	MerchantNotifQueue = "hivemtk:queue:merchant_notif"
	HumanLockReasonTTL = 24 * time.Hour
	// HumanLockDefaultTTL 人工锁默认 TTL：到期自动释放，AI 恢复服务。
	// 可通过 SetLockTTL 覆盖。
	HumanLockDefaultTTL = 24 * time.Hour
	// LockExpiryCheckInterval 后台锁过期检查默认间隔
	LockExpiryCheckInterval = time.Minute
)

// EscalationEvent 转人工事件载荷（推送到商户通知队列）
type EscalationEvent struct {
	Event      string    `json:"event"`
	SessionID  string    `json:"session_id"`
	Reason     string    `json:"reason"`
	Severity   string    `json:"severity"`
	Timestamp  time.Time `json:"timestamp"`
	Channel    string    `json:"channel,omitempty"`
	CustomerID string    `json:"customer_id,omitempty"`
	AgentCode  string    `json:"agent_code,omitempty"`
}

// HumanEscalationManager 转人工协同状态机中枢
//
// 在 B5 时机被后台 AgentRuntime 状态机调用：检测到危机感 >= 4 / 情绪=angry / 强转人工意图
// 任何一个条件被命中，立即触发熔断。
type HumanEscalationManager struct {
	cache cache.Cache

	notifier Notifier

	lockTTL time.Duration

	mu    sync.RWMutex
	stats EscalationStats

	lockDeadlines map[string]time.Time
}

// Notifier 通知接口（解耦消息总线实现）
type Notifier interface {
	Notify(ctx context.Context, event *EscalationEvent) error
}

// EscalationStats 转人工统计
type EscalationStats struct {
	TotalTriggers      int64
	TotalRejections    int64
	TotalNotifications int64
	LastTriggerAt      time.Time
	LastSessionID      string
	TriggersByReason   sync.Map
}

// NewHumanEscalationManager 构造转人工管理器
func NewHumanEscalationManager(c cache.Cache) *HumanEscalationManager {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	return &HumanEscalationManager{
		cache:         c,
		notifier:      NewCacheNotifier(c),
		lockTTL:       HumanLockDefaultTTL,
		lockDeadlines: make(map[string]time.Time),
	}
}

// SetLockTTL 覆盖人工锁 TTL（<=0 时恢复默认值）
func (h *HumanEscalationManager) SetLockTTL(ttl time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ttl <= 0 {
		ttl = HumanLockDefaultTTL
	}
	h.lockTTL = ttl
}

// SetNotifier 自定义通知实现（如 邮件 / 钉钉 / 飞书 webhook）
func (h *HumanEscalationManager) SetNotifier(ctx context.Context, n Notifier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notifier = n
}

// TriggerCensorshipEscalation 触发转人工熔断
//
// 入参：
//   - sessionID: 会话唯一 ID
//   - reason: 触发原因（如 "crisis_high:骗子" / "angry_sentiment" / "handoff_human_intent"）
//
// 流程：
//  1. 下发 Redis 分布式锁（TTL 默认 24h，到期自动释放）
//  2. 记录转人工原因（24h TTL）
//  3. 推送"叮咚"通知到商户异步总线
//  4. 记录统计
//
// 返回：
//   - error: 仅在 Redis 锁下发失败时返回
func (h *HumanEscalationManager) TriggerCensorshipEscalation(ctx context.Context, sessionID, reason string) error {
	if sessionID == "" {
		return errors.New("sessionID required")
	}
	if h.cache == nil {
		return errors.New("cache unavailable")
	}

	h.mu.RLock()
	lockTTL := h.lockTTL
	h.mu.RUnlock()

	lockKey := HumanLockKey + sessionID
	if err := h.cache.Set(ctx, lockKey, "true", lockTTL); err != nil {
		h.mu.Lock()
		h.stats.TotalRejections++
		h.mu.Unlock()
		return fmt.Errorf("set human lock failed: %w", err)
	}

	h.mu.Lock()
	if h.lockDeadlines == nil {
		h.lockDeadlines = make(map[string]time.Time)
	}
	h.lockDeadlines[sessionID] = time.Now().Add(lockTTL)
	h.mu.Unlock()

	reasonKey := HumanReasonKey + sessionID
	_ = h.cache.Set(ctx, reasonKey, reason, HumanLockReasonTTL)

	event := &EscalationEvent{
		Event:     "TRANSFER_TO_HUMAN",
		SessionID: sessionID,
		Reason:    reason,
		Severity:  severityFromReason(reason),
		Timestamp: time.Now(),
	}
	if h.notifier != nil {
		if err := h.notifier.Notify(ctx, event); err != nil {
			logger.Warnf("[HumanEscalation] notify failed session=%s err=%v", sessionID, err)
		}
	}

	h.mu.Lock()
	h.stats.TotalTriggers++
	h.stats.LastTriggerAt = time.Now()
	h.stats.LastSessionID = sessionID
	if v, ok := h.stats.TriggersByReason.Load(reason); ok {
		h.stats.TriggersByReason.Store(reason, v.(int64)+1)
	} else {
		h.stats.TriggersByReason.Store(reason, int64(1))
	}
	h.mu.Unlock()

	logger.Infof("[HumanEscalation] session=%s 触发转人工熔断，原因=%s", sessionID, reason)
	return nil
}

// RecentUserMessageFetcher 最近一条用户消息文本获取器。
// 由调用方提供（如 inbox_ingress 消息处理入口处已有消息上下文），
// 仅在 Redis 故障降级判断时被调用，用于 fail-safe 方向判定。
type RecentUserMessageFetcher func() string

// IsSessionLockedForHuman 检查会话是否被人工接管
//
// 用作 AI 大模型调用前的"前置阻断器"：
//   - 返回 true：AI 彻底闭嘴，消息直接路由到坐席工作台
//   - 返回 false：进入正常 AI 路由
//
// 缓存降级策略（fail-safe 保守方向）：
//   - key 不存在（redis.Nil）：未锁定，放行 AI
//   - Redis 故障：若调用方传入的最近一条用户消息命中转人工关键词
//     （复用 nlp_keywords.go Transfer 词表），保守返回 true（fail-closed 转人工）+ ERROR 日志；
//     否则返回 false 放行 AI + WARN 日志
func (h *HumanEscalationManager) IsSessionLockedForHuman(ctx context.Context, sessionID string, recentUserMsg ...RecentUserMessageFetcher) bool {
	if h.cache == nil || sessionID == "" {
		return false
	}
	key := HumanLockKey + sessionID
	val, err := h.cache.Get(ctx, key)
	switch {
	case err == nil:
		return val == "true"
	case errors.Is(err, redis.Nil), errors.Is(err, cache.ErrCacheMiss):

		return false
	default:
		return h.fallbackOnCacheFailure(sessionID, recentUserMsg...)
	}
}

func (h *HumanEscalationManager) fallbackOnCacheFailure(sessionID string, fetchers ...RecentUserMessageFetcher) bool {
	for _, f := range fetchers {
		if f == nil {
			continue
		}
		if MatchTransferKeywords(f()) {
			logger.Errorf("[HumanEscalation] Redis 故障且最近用户消息命中转人工关键词，保守判定为人工接管 session=%s", sessionID)
			return true
		}
	}
	logger.Warnf("[HumanEscalation] Redis 故障且无转人工关键词命中，放行 AI 路由 session=%s", sessionID)
	return false
}

// RenewSessionHumanLock 续期人工锁（坐席工作台活跃时调用）
//
// 接入点说明：应在坐席对会话的活跃操作处调用（如 unified_inbox 坐席回复 /
// ws_agent_executor 坐席接管动作），防止坐席处理期间锁到期被后台释放。
// ttl <= 0 时使用当前默认 lockTTL。
func (h *HumanEscalationManager) RenewSessionHumanLock(ctx context.Context, sessionID string, ttl time.Duration) error {
	if h.cache == nil || sessionID == "" {
		return errors.New("cache or sessionID unavailable")
	}
	h.mu.RLock()
	if ttl <= 0 {
		ttl = h.lockTTL
	}
	h.mu.RUnlock()

	if err := h.cache.Set(ctx, HumanLockKey+sessionID, "true", ttl); err != nil {
		return err
	}
	h.mu.Lock()
	h.lockDeadlines[sessionID] = time.Now().Add(ttl)
	h.mu.Unlock()
	logger.Infof("[HumanEscalation] session=%s 人工锁已续期 ttl=%s", sessionID, ttl)
	return nil
}

// StartLockExpiryChecker 启动后台锁过期检查 goroutine（项目惯例：ticker 循环）。
// 锁到期释放时向商户通知队列推送"人工锁已超时释放，AI 恢复服务"事件。
func (h *HumanEscalationManager) StartLockExpiryChecker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = LockExpiryCheckInterval
	}
	utils.SafeGo(ctx, "human_escalation.lock_expiry_checker", func(ctx context.Context) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.checkExpiredLocks(ctx)
			}
		}
	})
}

func (h *HumanEscalationManager) checkExpiredLocks(ctx context.Context) {
	now := time.Now()
	h.mu.RLock()
	var expired []string
	for sid, deadline := range h.lockDeadlines {
		if now.After(deadline) {
			expired = append(expired, sid)
		}
	}
	h.mu.RUnlock()

	for _, sid := range expired {
		exists, err := h.cache.Exists(ctx, HumanLockKey+sid)
		if err != nil {
			continue
		}
		if exists {

			h.mu.RLock()
			lockTTL := h.lockTTL
			h.mu.RUnlock()
			h.mu.Lock()
			h.lockDeadlines[sid] = time.Now().Add(lockTTL)
			h.mu.Unlock()
			continue
		}
		h.mu.Lock()
		delete(h.lockDeadlines, sid)
		h.mu.Unlock()
		h.notifyLockExpired(ctx, sid)
	}
}

func (h *HumanEscalationManager) notifyLockExpired(ctx context.Context, sessionID string) {
	event := &EscalationEvent{
		Event:     "HUMAN_LOCK_EXPIRED",
		SessionID: sessionID,
		Reason:    "人工锁已超时释放，AI 恢复服务",
		Severity:  "low",
		Timestamp: time.Now(),
	}
	if h.notifier != nil {
		if err := h.notifier.Notify(ctx, event); err != nil {
			logger.Warnf("[HumanEscalation] 锁超时通知推送失败 session=%s err=%v", sessionID, err)
		}
	}
	logger.Infof("[HumanEscalation] session=%s 人工锁已超时释放，AI 恢复服务", sessionID)
}

// GetEscalationReason 获取转人工原因
func (h *HumanEscalationManager) GetEscalationReason(ctx context.Context, sessionID string) (string, error) {
	if h.cache == nil || sessionID == "" {
		return "", errors.New("cache unavailable")
	}
	reason, err := h.cache.Get(ctx, HumanReasonKey+sessionID)
	if err != nil {
		return "", err
	}
	return reason, nil
}

// ReleaseHumanLock 解除人工接管锁（坐席主动"解决"会话时调用）
func (h *HumanEscalationManager) ReleaseHumanLock(ctx context.Context, sessionID string) error {
	if h.cache == nil || sessionID == "" {
		return errors.New("cache or sessionID unavailable")
	}
	if err := h.cache.Delete(ctx, HumanLockKey+sessionID); err != nil {
		return err
	}
	_ = h.cache.Delete(ctx, HumanReasonKey+sessionID)
	h.mu.Lock()
	delete(h.lockDeadlines, sessionID)
	h.mu.Unlock()
	logger.Infof("[HumanEscalation] session=%s 已解除人工接管", sessionID)
	return nil
}

// GetStats 获取统计
//
// 返回指针避免 sync.Map 被值拷贝（go vet: copies lock value）
// 调用方在 RLock 期间读取快照，读取完成后应尽快释放（不要再持有指针跨锁）
func (h *HumanEscalationManager) GetStats(ctx context.Context) *EscalationStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return &h.stats
}

func severityFromReason(reason string) string {
	switch {
	case len(reason) == 0:
		return "low"
	case hasAnySubstring(reason, []string{"high_risk", "handoff_human", "scam", "fraud", "lawsuit"}):
		return "high"
	case hasAnySubstring(reason, []string{"medium_risk", "complaint", "angry"}):
		return "medium"
	default:
		return "low"
	}
}

func hasAnySubstring(s string, subs []string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// CacheNotifier 基于 Cache 的默认通知器
type CacheNotifier struct {
	cache cache.Cache
	queue string
}

// NewCacheNotifier 构造默认通知器
func NewCacheNotifier(c cache.Cache) *CacheNotifier {
	return &CacheNotifier{cache: c, queue: MerchantNotifQueue}
}

// Notify 通过 LPush 推送到商户通知队列
func (n *CacheNotifier) Notify(ctx context.Context, event *EscalationEvent) error {
	if n.cache == nil {
		return errors.New("cache unavailable")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return n.cache.LPush(ctx, n.queue, string(payload), 0)
}

var (
	globalEscalationMgr *HumanEscalationManager
	onceEscalationMgr   sync.Once
)

// GetGlobalEscalationManager 获取全局转人工管理器
func GetGlobalEscalationManager() *HumanEscalationManager {
	onceEscalationMgr.Do(func() {
		globalEscalationMgr = NewHumanEscalationManager(cache.GetGlobalCache())
	})
	return globalEscalationMgr
}
