package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	InboxHumanLockKey = "hivemtk:lock:human:"

	InboxAILockKey = "hivemtk:lock:ai_processing:"

	InboxPendingKey = "hivemtk:pending:"

	InboxLockTTL = 24 * time.Hour

	InboxAILockTTL = 15 * time.Second

	InboxPendingTTL = 5 * time.Minute

	IngestLockKey = "hivemtk:lock:ingest:"

	IngestLockTTL = 25 * time.Second

	InboxContentDedupKey = "hivemtk:dedup:content:"

	InboxContentDedupTTL = 5 * time.Minute

	InboxSenderContentDedupKey = "hivemtk:dedup:sender-content:"

	InboxReplyWindow = 5 * time.Minute

	InboxBackfillFutureTolerance = 5 * time.Second

	InboxAIProcessingKey = "hivemtk:ai_processing:"

	InboxAIProcessingTTL = 2 * time.Minute

	RecheckDelayAfterRelease = 800 * time.Millisecond
)

type AITrigger interface {
	TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption)
}

type TriggerInboundOption func(*TriggerInboundMeta)

type TriggerInboundMeta struct {
	SenderName string
	IsGroup    bool
	GroupID    string
	GroupName  string
	// SessionWebhook 钉钉机器人临时回复通道（官方协议：回复即 POST 此 URL）。
	// 由入站回调捕获，经 sendOutbound case ChannelDingTalk 消费；其他渠道为空。
	SessionWebhook          string
	SessionWebhookExpiredAt int64
	// ChannelMsgID 渠道平台原始消息 ID（如 QQ 事件 id）。
	// QQ 被动回复必须关联原消息 msg_id（5 分钟窗口 5 条额度），
	// 经 sendOutbound case ChannelQQ 消费；其他渠道为空。
	ChannelMsgID string
}

func WithSenderName(name string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.SenderName = name }
}

func WithGroup(groupID, groupName string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) {
		m.IsGroup = true
		m.GroupID = groupID
		m.GroupName = groupName
	}
}

func WithSessionWebhook(url string, expiredAt int64) TriggerInboundOption {
	return func(m *TriggerInboundMeta) {
		m.SessionWebhook = url
		m.SessionWebhookExpiredAt = expiredAt
	}
}

// WithChannelMsgID 传递渠道平台原始消息 ID（QQ 被动回复依赖）
func WithChannelMsgID(msgID string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.ChannelMsgID = msgID }
}

type InboxIngressResult struct {
	Accepted    bool   `json:"accepted"`
	HumanLocked bool   `json:"human_locked"`
	QueuedForAI bool   `json:"queued_for_ai"`
	SessionID   string `json:"session_id"`
	Reason      string `json:"reason,omitempty"`
}

type InboxIngressService struct {
	hubRepo       *repository.MessageHubRepository
	cache         cache.Cache
	triggerCh     chan string
	aiTrigger     AITrigger
	inboxSvc      *InboxService
	leadMiningSvc *Service
}

func NewInboxIngressService() *InboxIngressService {
	return NewInboxIngressServiceWithDB(dbUtil.GetDB(), nil)
}

func NewInboxIngressServiceWithDB(db *gorm.DB, c cache.Cache) *InboxIngressService {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	var hubRepo *repository.MessageHubRepository
	if db != nil {
		hubRepo = repository.NewMessageHubRepositoryWithDB(db)
	}
	return &InboxIngressService{
		hubRepo:   hubRepo,
		cache:     c,
		triggerCh: make(chan string, 1024),
	}
}

func (s *InboxIngressService) TriggerChannel(ctx context.Context) <-chan string {
	return s.triggerCh
}

func (s *InboxIngressService) SetAITrigger(t AITrigger) {
	s.aiTrigger = t
}

func (s *InboxIngressService) SetInboxService(svc *InboxService) {
	s.inboxSvc = svc
}

func (s *InboxIngressService) SetLeadMining(svc *Service) {
	s.leadMiningSvc = svc
}

func (s *InboxIngressService) IsSessionHumanLocked(ctx context.Context, sessionID string, recentUserMsg ...string) (bool, error) {
	if s.cache == nil || sessionID == "" {
		return false, nil
	}
	key := InboxHumanLockKey + sessionID
	v, err := s.cache.Get(ctx, key)
	switch {
	case err == nil:
		return v == "true", nil
	case errors.Is(err, cache.ErrCacheMiss):
		return false, nil
	default:
		content := ""
		for _, c := range recentUserMsg {
			if c != "" {
				content = c
				break
			}
		}
		if content != "" && (MatchTransferKeywords(content) || MatchExplicitKeywords(content)) {
			logger.Errorf("[Inbox] Redis 故障且最近用户消息命中转人工关键词，保守判定为人工接管 session=%s", sessionID)
			return true, nil
		}
		logger.Errorf("[Inbox] Redis 故障且无转人工关键词命中，放行 AI 路由 session=%s", sessionID)
		return false, nil
	}
}

func (s *InboxIngressService) LockSessionForHuman(ctx context.Context, sessionID, reason string) error {
	if s.cache == nil || sessionID == "" {
		return errors.New("cache unavailable")
	}
	key := InboxHumanLockKey + sessionID
	if err := s.cache.Set(ctx, key, "true", InboxLockTTL); err != nil {
		return err
	}
	if reason != "" {
		_ = s.cache.Set(ctx, InboxHumanLockKey+"reason:"+sessionID, reason, 24*time.Hour)
	}
	logger.Infof("[Inbox] 会话 %s 已被人工接管: %s", sessionID, reason)
	return nil
}

func (s *InboxIngressService) UnlockSessionForHuman(ctx context.Context, sessionID string) error {
	if s.cache == nil || sessionID == "" {
		return nil
	}
	_ = s.cache.Delete(ctx, InboxHumanLockKey+sessionID)
	_ = s.cache.Delete(ctx, InboxHumanLockKey+"reason:"+sessionID)
	if inboxLockMgr != nil {
		inboxLockMgr.removeDeadline(sessionID)
	}
	return nil
}

func (s *InboxIngressService) RenewSessionHumanLock(ctx context.Context, sessionID string, ttl time.Duration) error {
	if s.cache == nil || sessionID == "" {
		return errors.New("cache or sessionID unavailable")
	}
	if ttl <= 0 {
		ttl = InboxLockTTL
	}
	if err := s.cache.Set(ctx, InboxHumanLockKey+sessionID, "true", ttl); err != nil {
		return err
	}
	if inboxLockMgr != nil {
		inboxLockMgr.setDeadline(sessionID, time.Now().Add(ttl))
	}
	logger.Infof("[Inbox] session=%s 人工锁已续期 ttl=%s", sessionID, ttl)
	return nil
}

type inboxHumanLockExpiryManager struct {
	mu        sync.RWMutex
	deadlines map[string]time.Time
	started   bool
}

var inboxLockMgr *inboxHumanLockExpiryManager
var inboxLockMgrOnce sync.Once

func getInboxLockMgr() *inboxHumanLockExpiryManager {
	inboxLockMgrOnce.Do(func() {
		inboxLockMgr = &inboxHumanLockExpiryManager{deadlines: make(map[string]time.Time)}
	})
	return inboxLockMgr
}

func (m *inboxHumanLockExpiryManager) setDeadline(sessionID string, deadline time.Time) {
	m.mu.Lock()
	m.deadlines[sessionID] = deadline
	m.mu.Unlock()
}

func (m *inboxHumanLockExpiryManager) removeDeadline(sessionID string) {
	m.mu.Lock()
	delete(m.deadlines, sessionID)
	m.mu.Unlock()
}

func (m *inboxHumanLockExpiryManager) checkExpired(ctx context.Context, c cache.Cache) {
	now := time.Now()
	m.mu.RLock()
	var expired []string
	for sid, dl := range m.deadlines {
		if now.After(dl) {
			expired = append(expired, sid)
		}
	}
	m.mu.RUnlock()
	for _, sid := range expired {
		if c != nil {
			exists, err := c.Exists(ctx, InboxHumanLockKey+sid)
			if err != nil || exists {
				continue
			}
		}
		m.mu.Lock()
		delete(m.deadlines, sid)
		m.mu.Unlock()
		logger.Infof("[Inbox] session=%s 人工锁已超时释放，AI 恢复服务", sid)
	}
}

func StartInboxHumanLockExpiryChecker(ctx context.Context, c cache.Cache, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				getInboxLockMgr().checkExpired(ctx, c)
			}
		}
	}()
}

func (s *InboxIngressService) tryAcquireAILock(ctx context.Context, sessionID string) (bool, error) {
	if s.cache == nil || sessionID == "" {
		return true, nil
	}
	key := InboxAILockKey + sessionID
	return s.cache.SetNX(ctx, key, "busy", InboxAILockTTL)
}

func (s *InboxIngressService) ReleaseAILock(ctx context.Context, sessionID string) {
	if s.cache == nil || sessionID == "" {
		return
	}
	_ = s.cache.Delete(ctx, InboxAILockKey+sessionID)
}

func (s *InboxIngressService) IsSessionAIBusy(ctx context.Context, sessionID string) (bool, error) {
	if s.cache == nil || sessionID == "" {
		return false, nil
	}
	v, err := s.cache.Get(ctx, InboxAILockKey+sessionID)
	if err != nil {
		return false, nil
	}
	return v == "busy", nil
}

func (s *InboxIngressService) AppendPendingMessage(ctx context.Context, sessionID string, content string) error {
	if s.cache == nil || sessionID == "" {
		return nil
	}
	return s.cache.LPush(ctx, InboxPendingKey+sessionID, content, InboxPendingTTL)
}

func (s *InboxIngressService) PopPendingMessages(ctx context.Context, sessionID string) ([]string, error) {
	if s.cache == nil || sessionID == "" {
		return nil, nil
	}
	key := InboxPendingKey + sessionID

	items, err := s.cache.PopAll(ctx, key)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *InboxIngressService) NormalizeEvent(ctx context.Context, event *model.MessageEvent) error {
	if event == nil {
		return errors.New("event is nil")
	}
	if event.EventID == "" {
		event.EventID = uuid.NewString()
	}
	if event.SessionID == "" {

		event.SessionID = event.Channel + ":" + event.SenderID
	}
	if event.Channel == "" {
		return fmt.Errorf("invalid channel (empty)")
	}
	if event.SenderID == "" {

		if event.SenderName != "" {
			event.SenderID = event.SenderName
		} else {
			event.SenderID = event.Channel + ":unknown"
		}
	}

	if event.ConversationID == "" {
		if event.SessionID != "" {
			event.ConversationID = event.SessionID
		} else {
			accountID := ""
			if event.Extra != nil {
				if v, ok := event.Extra["account_id"].(string); ok {
					accountID = v
				}
			}
			if accountID == "" {
				accountID = event.Channel + ":unknown"
			}
			event.ConversationID = event.Channel + ":" + accountID
		}
	}
	if event.MsgType == "" {
		event.MsgType = model.MsgTypeText
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return nil
}

func (s *InboxIngressService) HandleIngressMessage(ctx context.Context, event *model.MessageEvent) (*InboxIngressResult, error) {
	result := &InboxIngressResult{
		SessionID: event.SessionID,
	}
	if err := s.NormalizeEvent(ctx, event); err != nil {
		return result, err
	}
	result.SessionID = event.SessionID

	isSystemMsg := event.SenderType == "system"

	humanLocked, _ := s.IsSessionHumanLocked(ctx, event.SessionID, event.Content)
	if humanLocked {
		result.HumanLocked = true
		result.Accepted = true
		result.Reason = "session is human-locked; bypass AI routing"
		if err := s.persistMessage(ctx, event); err != nil {
			return result, fmt.Errorf("持久化消息失败: %w", err)
		}
		return result, nil
	}

	if s.hubRepo != nil {
		existing, err := s.hubRepo.GetByMsgID(ctx, event.EventID)
		if err == nil && existing != nil && existing.ConversationID == event.ConversationID {
			result.Accepted = true
			result.QueuedForAI = false

			incomingDir := "inbound"
			if event.SenderType == "self" || event.SenderType == "agent" {
				incomingDir = "outbound"
			}
			if existing.Direction != incomingDir && existing.Direction != "" {
				logger.Ctx(ctx).Warn().
					Str("event_id", event.EventID).
					Str("conv_id", event.ConversationID).
					Str("db_direction", existing.Direction).
					Str("incoming_direction", incomingDir).
					Msg("[Inbox] 钩子2：msg_id 已存在且方向冲突，以 DB 方向为准（幂等跳过）")
				result.Reason = "msg_id exists with different direction; DB direction preserved"
			} else {
				result.Reason = "msg_id already exists in DB; idempotent skip"
			}
			logger.Ctx(ctx).Info().
				Str("channel", event.Channel).
				Str("conv_id", event.ConversationID).
				Str("event_id", event.EventID).
				Msg("[Inbox] 钩子2：msg_id 已存在，幂等跳过")
			return result, nil
		}
		if err == nil && existing != nil && existing.ConversationID != event.ConversationID {
			logger.Ctx(ctx).Info().
				Str("event_id", event.EventID).
				Str("conv_id", event.ConversationID).
				Str("existing_conv_id", existing.ConversationID).
				Msg("[Inbox] 钩子2：msg_id 跨会话命中（algo2 同 channel+content），不跳过，各自入库")
		}
	}

	if decision, derr := s.interceptInbound(ctx, event); derr != nil {
		logger.Ctx(ctx).Warn().Err(derr).Str("event_id", event.EventID).Msg("[Inbox] interceptInbound 出错，放行（不阻断业务）")
	} else if decision != nil && decision.Blocked {
		result.Accepted = true
		result.QueuedForAI = false
		result.Reason = fmt.Sprintf("intercepted by middleware: %s (self_echo=%v dup=%v)", decision.Reason, decision.IsSelfEcho, decision.IsDup)
		logger.Ctx(ctx).Info().
			Str("event_id", event.EventID).
			Bool("self_echo", decision.IsSelfEcho).
			Bool("dup", decision.IsDup).
			Str("reason", decision.Reason).
			Msg("[Inbox] 中间件拦截：消息被去重/回环拦截，不穿透业务层")
		return result, nil
	}

	if err := s.persistMessage(ctx, event); err != nil {
		return result, fmt.Errorf("持久化消息失败: %w", err)
	}
	result.Accepted = true

	if isSystemMsg {
		result.QueuedForAI = false
		result.Reason = "sender_type=system; persisted only (系统消息不触发 AI)"
		logger.Ctx(ctx).Info().
			Str("channel", event.Channel).
			Str("conv_id", event.ConversationID).
			Str("event_id", event.EventID).
			Msg("[Inbox] 系统消息：仅落库不触发 AI")
		return result, nil
	}

	if s.hubRepo != nil {
		unreplied, withinWindow, err := s.hubRepo.HasUnrepliedCustomerMessage(ctx, event.ConversationID, InboxReplyWindow)
		if err != nil {

			logger.Ctx(ctx).Warn().Err(err).
				Str("conv_id", event.ConversationID).
				Msg("[Inbox] 钩子3：查询最后一条消息方向失败，fail-open 放行本次 AI 触发")
		} else if !unreplied {
			result.QueuedForAI = false
			result.Reason = "last message is outbound (平台自己发的); not triggering AI"
			logger.Ctx(ctx).Info().
				Str("channel", event.Channel).
				Str("conv_id", event.ConversationID).
				Str("event_id", event.EventID).
				Msg("[Inbox] 钩子3：最后一条是平台自己发的，不触发 AI")
			return result, nil
		} else if !withinWindow {
			result.QueuedForAI = false
			result.Reason = "last inbound outside 5min window; not triggering AI (历史消息)"
			logger.Ctx(ctx).Info().
				Str("channel", event.Channel).
				Str("conv_id", event.ConversationID).
				Str("event_id", event.EventID).
				Msg("[Inbox] 钩子3：最后一条 inbound 超过 5 分钟，历史消息不触发 AI")
			return result, nil
		}
	}

	s.triggerAIWithDebounce(ctx, event)
	result.QueuedForAI = true
	result.Reason = "trigger AI customer service"
	return result, nil
}

func (s *InboxIngressService) triggerAIForEvent(ctx context.Context, event *model.MessageEvent) {
	accountID := "default"
	if event.Extra != nil {
		if v, ok := event.Extra["account_id"].(string); ok && v != "" {
			accountID = v
		}
	}
	if s.aiTrigger == nil {
		logger.Ctx(ctx).Error().
			Str("channel", event.Channel).
			Str("account_id", accountID).
			Str("session_id", event.SessionID).
			Msg("[Inbox] aiTrigger 未配置 — 桥接入站消息不会创建 customer_sessions / 不会生成 AI 回复。请检查 router.Setup() 中 bridgeIngressSvc.SetAITrigger(webhookSvc) 是否在 bridge WS 注册前调用。")
		return
	}
	opts := make([]TriggerInboundOption, 0, 3)
	if event.SenderName != "" {
		opts = append(opts, WithSenderName(event.SenderName))
	}
	if event.IsGroup {
		opts = append(opts, WithGroup(event.GroupID, groupNameOf(event)))
	}
	if event.Extra != nil {
		if v, ok := event.Extra["session_webhook"].(string); ok && v != "" {
			var exp int64
			switch t := event.Extra["session_webhook_expired_at"].(type) {
			case int64:
				exp = t
			case float64:
				exp = int64(t)
			}
			opts = append(opts, WithSessionWebhook(v, exp))
		}
		if v, ok := event.Extra["channel_msg_id"].(string); ok && v != "" {
			opts = append(opts, WithChannelMsgID(v))
		}
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Ctx(ctx).Error().
					Interface("panic", r).
					Str("channel", event.Channel).
					Str("account_id", accountID).
					Str("conv_id", event.ConversationID).
					Str("event_id", event.EventID).
					Str("sender", event.SenderID).
					Msg("[Inbox] aiTrigger.TriggerInboundAI panic recovered — AI 链路已断开，请查 root cause")
			}
		}()
		logger.Ctx(ctx).Info().
			Str("channel", event.Channel).
			Str("account_id", accountID).
			Str("conv_id", event.ConversationID).
			Str("event_id", event.EventID).
			Str("sender", event.SenderID).
			Int("content_len", len(event.Content)).
			Msg("[Inbox] aiTrigger.TriggerInboundAI start")

		if s.cache != nil && event.ConversationID != "" {
			aiKey := InboxAIProcessingKey + event.ConversationID
			acquired, lerr := s.cache.SetNX(ctx, aiKey, "1", InboxAIProcessingTTL)
			if lerr != nil {
				logger.Ctx(ctx).Warn().Err(lerr).
					Str("conv_id", event.ConversationID).
					Msg("[Inbox] 设置 AI 处理中标记失败（放行本次触发，但可能重复触发）")
			} else if !acquired {
				logger.Ctx(ctx).Info().
					Str("conv_id", event.ConversationID).
					Msg("[Inbox] AI 已在进行中（分布式排他命中），跳过本次触发避免重复回复")
				return
			}
		}
		s.aiTrigger.TriggerInboundAI(ctx, event.Channel, accountID, event.ConversationID, event.SenderID, event.Content, event.EventID, opts...)
	}()
}

func (s *InboxIngressService) ReleaseAIProcessingFlag(ctx context.Context, conversationID string) {
	if s == nil || s.cache == nil || conversationID == "" {
		return
	}
	aiKey := InboxAIProcessingKey + conversationID
	if err := s.cache.Delete(ctx, aiKey); err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("conv_id", conversationID).
			Msg("[Inbox] 释放 AI 处理中标记失败")
	}
}

func (s *InboxIngressService) withIngestLock(ctx context.Context, conversationID string, fn func() error) (bool, error) {
	if s.cache == nil || conversationID == "" {
		return true, fn()
	}
	key := IngestLockKey + conversationID
	token := uuid.NewString()
	const retries = 4
	for i := 0; i < retries; i++ {
		ok, err := s.cache.SetNX(ctx, key, token, IngestLockTTL)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("conv_id", conversationID).
				Msg("[Ingest] 锁后端异常，退化为直接执行（DB 唯一约束兜底去重）")
			return true, fn()
		}
		if ok {
			defer s.cache.ReleaseLock(ctx, key, token)
			if ferr := fn(); ferr != nil {
				return true, ferr
			}
			return true, nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	logger.Ctx(ctx).Warn().
		Str("conv_id", conversationID).
		Msg("[Ingest] 入库锁被占用（重试耗尽），跳过本次（前端将重新上报，DB 唯一约束保证幂等）")
	return false, nil
}

func (s *InboxIngressService) RecheckUnrepliedAndTrigger(ctx context.Context, conversationID, sessionID string) {
	if s == nil || conversationID == "" {
		return
	}

	select {
	case <-time.After(RecheckDelayAfterRelease):
	case <-ctx.Done():
		return
	}

	if sessionID != "" {
		if humanLocked, _ := s.IsSessionHumanLocked(ctx, sessionID); humanLocked {
			logger.Ctx(ctx).Info().
				Str("conv_id", conversationID).
				Str("session_id", sessionID).
				Msg("[Inbox][Recheck] 会话已被人工接管，跳过补触发")
			return
		}
	}

	if s.hubRepo == nil {
		return
	}
	unreplied, withinWindow, err := s.hubRepo.HasUnrepliedCustomerMessage(ctx, conversationID, InboxReplyWindow)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("conv_id", conversationID).
			Msg("[Inbox][Recheck] 查询未回复消息失败，跳过补触发")
		return
	}
	if !unreplied {

		return
	}
	if !withinWindow {

		return
	}

	if s.cache != nil {
		aiKey := InboxAIProcessingKey + conversationID
		if exists, _ := s.cache.Exists(ctx, aiKey); exists {
			logger.Ctx(ctx).Info().
				Str("conv_id", conversationID).
				Msg("[Inbox][Recheck] 新一轮 AI 已在处理中，跳过补触发")
			return
		}
	}

	last, err := s.hubRepo.GetLastInboundByConversation(ctx, conversationID)
	if err != nil || last == nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("conv_id", conversationID).
			Msg("[Inbox][Recheck] 获取最后一条客户消息失败，跳过补触发")
		return
	}

	ev := &model.MessageEvent{
		EventID:        uuid.NewString(),
		Channel:        last.Platform,
		ConversationID: conversationID,
		SessionID:      sessionID,
		SenderID:       last.SenderID,
		SenderName:     last.SenderName,
		Content:        last.Content,
		MsgType:        last.MsgType,
		IsGroup:        last.IsGroup,
		GroupID:        last.GroupID,
		Timestamp:      last.SentAt,
		Extra:          map[string]any{"account_id": last.AccountID},
	}
	logger.Ctx(ctx).Info().
		Str("conv_id", conversationID).
		Str("event_id", ev.EventID).
		Str("sender", last.SenderID).
		Msg("[Inbox][Recheck] 检测到 AI 推理期间遗漏的未回复客户消息，补触发 AI")
	s.triggerAIForEvent(ctx, ev)
}

type InboxIngressBatchResult struct {
	PerEvent    []*InboxIngressResult `json:"per_event"`
	TriggeredAI bool                  `json:"triggered_ai"`
	Reason      string                `json:"reason,omitempty"`
}

func (s *InboxIngressService) HandleIngressBatch(ctx context.Context, events []*model.MessageEvent) (*InboxIngressBatchResult, error) {
	batchResult := &InboxIngressBatchResult{
		PerEvent: make([]*InboxIngressResult, len(events)),
	}
	if len(events) == 0 {
		return batchResult, nil
	}

	type indexedEvent struct {
		idx   int
		event *model.MessageEvent
	}
	groups := make(map[string][]indexedEvent)
	for i, ev := range events {

		if ev != nil {
			_ = s.NormalizeEvent(ctx, ev)
			convID := ev.ConversationID
			if convID == "" {
				convID = "_no_conv_"
			}
			groups[convID] = append(groups[convID], indexedEvent{idx: i, event: ev})
		}
	}

	for convID, groupEvents := range groups {
		var newInboundContents []string
		var firstInboundEvent *model.MessageEvent
		for _, ie := range groupEvents {
			ev := ie.event

			r, err := s.handleIngressSingleForBatch(ctx, ev)
			batchResult.PerEvent[ie.idx] = r
			if err != nil {
				r.Reason = fmt.Sprintf("batch handle error: %v", err)
				continue
			}

			if r.Accepted && r.QueuedForAI && ev.SenderType != "self" && ev.SenderType != "agent" && ev.SenderType != "system" {
				newInboundContents = append(newInboundContents, ev.Content)
				if firstInboundEvent == nil {
					firstInboundEvent = ev
				}
			}
		}

		if len(newInboundContents) == 0 || firstInboundEvent == nil {
			continue
		}

		humanLocked, _ := s.IsSessionHumanLocked(ctx, firstInboundEvent.SessionID, firstInboundEvent.Content)
		if humanLocked {
			continue
		}

		if s.cache != nil {
			aiKey := InboxAIProcessingKey + convID
			if exists, _ := s.cache.Exists(ctx, aiKey); exists {
				logger.Ctx(ctx).Info().
					Str("conv_id", convID).
					Msg("[Inbox][Batch] AI 处理中（标记存在），跳过本次触发避免重复回复")
				continue
			}
		}

		mergedEvent := *firstInboundEvent
		if len(newInboundContents) > 1 {
			mergedEvent.Content = strings.Join(newInboundContents, "\n")
			mergedEvent.EventID = uuid.NewString()
			logger.Ctx(ctx).Info().
				Str("channel", mergedEvent.Channel).
				Str("conv_id", convID).
				Int("merged_count", len(newInboundContents)).
				Int("merged_content_len", len(mergedEvent.Content)).
				Msg("[Inbox][Batch] 合并多条 inbound 消息触发一次 AI")
		}
		s.triggerAIForEvent(ctx, &mergedEvent)
		batchResult.TriggeredAI = true
		batchResult.Reason = fmt.Sprintf("batch: %d messages merged, 1 AI trigger", len(newInboundContents))
	}
	return batchResult, nil
}

func (s *InboxIngressService) handleIngressSingleForBatch(ctx context.Context, event *model.MessageEvent) (*InboxIngressResult, error) {
	result := &InboxIngressResult{
		SessionID: event.SessionID,
	}

	isSystemMsg := event.SenderType == "system"

	humanLocked, _ := s.IsSessionHumanLocked(ctx, event.SessionID, event.Content)
	if humanLocked {
		result.HumanLocked = true
		result.Accepted = true
		result.Reason = "session is human-locked; bypass AI routing"
		if err := s.persistMessage(ctx, event); err != nil {
			return result, fmt.Errorf("持久化消息失败: %w", err)
		}
		return result, nil
	}

	if s.hubRepo != nil {

		if event.EventID != "" {
			if existing, err := s.hubRepo.GetByMsgID(ctx, event.EventID); err == nil && existing != nil && existing.ConversationID == event.ConversationID {
				result.Accepted = true
				result.QueuedForAI = false
				result.Reason = "msg_id already exists in DB; idempotent skip"
				return result, nil
			}
		}

		if ch, ok := event.Extra["content_hash"].(string); ok && ch != "" {
			if existing, err := s.hubRepo.GetByMsgID(ctx, ch); err == nil && existing != nil && existing.ConversationID == event.ConversationID {
				result.Accepted = true
				result.QueuedForAI = false
				result.Reason = "content_hash already exists in DB (platform outbound echo); idempotent skip"
				return result, nil
			}
		}
	}

	if decision, derr := s.interceptInbound(ctx, event); derr != nil {
		logger.Ctx(ctx).Warn().Err(derr).Str("event_id", event.EventID).Msg("[Inbox] interceptInbound 出错，放行（不阻断业务）")
	} else if decision != nil && decision.Blocked {
		result.Accepted = true
		result.QueuedForAI = false
		result.Reason = fmt.Sprintf("intercepted by middleware: %s (self_echo=%v dup=%v)", decision.Reason, decision.IsSelfEcho, decision.IsDup)
		logger.Ctx(ctx).Info().
			Str("event_id", event.EventID).
			Bool("self_echo", decision.IsSelfEcho).
			Bool("dup", decision.IsDup).
			Str("reason", decision.Reason).
			Msg("[Inbox] 中间件拦截（批次）：消息被去重/回环拦截，不穿透业务层")
		return result, nil
	}

	if err := s.persistMessage(ctx, event); err != nil {
		return result, fmt.Errorf("持久化消息失败: %w", err)
	}
	result.Accepted = true

	if isSystemMsg {
		result.QueuedForAI = false
		result.Reason = "sender_type=system; persisted only"
		return result, nil
	}

	if event.SenderType == "self" || event.SenderType == "agent" {
		result.QueuedForAI = false
		result.Reason = "sender_type=self/agent; persisted only (平台自己发的不触发 AI)"
		return result, nil
	}

	result.QueuedForAI = true
	result.Reason = "batched; will be merged and triggered at batch end"
	return result, nil
}
