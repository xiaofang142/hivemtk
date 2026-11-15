package service

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	aiDebounceGatePrefix  = "hivemtk:ai:debounce:"
	aiDebounceMaxSeconds  = 30
	aiDebounceMaxMessages = 20
)

func aiDebounceSeconds(event *model.MessageEvent) int {
	// 参数中心优先（2026-11-14 参数化并默认启用 12s：客户连发消息合并一批应答，
	// 防逐条轰炸——机制本就存在于本文件，此前仅 env 可开、生产恒 0 形同虚设）。
	def := BridgeAIDebounceSeconds(context.Background())
	if v := os.Getenv("AI_DEBOUNCE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			def = n
		}
	}
	if event != nil && event.Extra != nil {
		if v, ok := event.Extra["ai_debounce_seconds"]; ok {
			switch t := v.(type) {
			case int:
				if t >= 0 {
					return min(t, aiDebounceMaxSeconds)
				}
			case int64:
				if t >= 0 {
					return min(int(t), aiDebounceMaxSeconds)
				}
			case float64:
				if t >= 0 {
					return min(int(t), aiDebounceMaxSeconds)
				}
			}
		}
	}
	if def < 0 {
		return 0
	}
	return min(def, aiDebounceMaxSeconds)
}

func (s *InboxIngressService) triggerAIWithDebounce(ctx context.Context, event *model.MessageEvent) {
	seconds := aiDebounceSeconds(event)
	content := strings.TrimSpace(event.Content)

	if seconds <= 0 || s.cache == nil || content == "" ||
		MatchTransferKeywords(content) || MatchExplicitKeywords(content) {
		s.triggerAIForEvent(ctx, event)
		return
	}
	sessionID := event.SessionID
	if sessionID == "" {
		s.triggerAIForEvent(ctx, event)
		return
	}

	// 自适应合并（2026-11-15 首响时延优化）：合并窗只对「爆发中」的后续消息生效——
	// 上一条同会话 inbound 距本条 < 合并窗（客户正在连发）→ 进窗合并；否则（静默后
	// 首条）→ 立即触发，省掉整窗首响延迟（生产实测单条消息因固定窗多等 12s，
	// 端到端 35-40s 的贡献项之一）。
	if s.hubRepo != nil {
		before := event.Timestamp
		if before.IsZero() {
			before = time.Now().Add(time.Millisecond) // 无时间戳时取全部（含本条由时间严格小于排除失败→按连发处理，保守）
		}
		if prev, perr := s.hubRepo.PrevInboundCreatedAt(ctx, event.Channel, resolveAccountID(event), event.ConversationID, before); perr == nil && !prev.IsZero() {
			if since := time.Since(prev); since >= time.Duration(seconds)*time.Second {
				logger.Ctx(ctx).Info().
					Str("session_id", sessionID).
					Str("conv_id", event.ConversationID).
					Str("since_prev", since.String()).
					Msg("[Inbox][Debounce] 会话静默后首条消息，跳过合并窗立即触发（首响提速）")
				s.triggerAIForEvent(ctx, event)
				return
			}
		}
	}

	if err := s.AppendPendingMessage(ctx, sessionID, content); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("session_id", sessionID).Msg("[Inbox][Debounce] append pending failed, fallback to immediate trigger")
		s.triggerAIForEvent(ctx, event)
		return
	}

	gate := aiDebounceGatePrefix + sessionID
	gateToken := uuid.NewString()
	ok, err := s.cache.SetNX(ctx, gate, gateToken, time.Duration(seconds)*time.Second+time.Second)
	if err != nil {

		logger.Ctx(ctx).Warn().Err(err).Str("session_id", sessionID).Msg("[Inbox][Debounce] gate backend error, fallback to immediate trigger")

		s.drainOnePending(ctx, sessionID)
		s.triggerAIForEvent(ctx, event)
		return
	}
	if !ok {
		logger.Ctx(ctx).Info().
			Str("session_id", sessionID).
			Str("conv_id", event.ConversationID).
			Int("debounce_seconds", seconds).
			Msg("[Inbox][Debounce] window active, message coalesced (waiting for window close)")
		return
	}

	latestContent := content
	mergedEvent := *event

	if event.Extra != nil {
		extraCopy := make(map[string]any, len(event.Extra)+1)
		for k, v := range event.Extra {
			extraCopy[k] = v
		}
		mergedEvent.Extra = extraCopy
	}
	time.AfterFunc(time.Duration(seconds)*time.Second, func() {

		bctx := context.Background()
		pending, perr := s.PopPendingMessages(bctx, sessionID)

		_, _ = s.cache.ReleaseLock(bctx, gate, gateToken)
		if perr != nil {
			logger.Ctx(bctx).Warn().Err(perr).Str("session_id", sessionID).Msg("[Inbox][Debounce] pop pending failed, trigger with latest only")
			pending = []string{latestContent}
		}
		if len(pending) == 0 {
			pending = []string{latestContent}
		}

		pending = coalescePending(pending)

		mergedEvent.Content = strings.Join(pending, "\n")
		if mergedEvent.Extra == nil {
			mergedEvent.Extra = map[string]any{}
		}
		mergedEvent.Extra["ai_debounce_merged"] = len(pending)

		logger.Ctx(bctx).Info().
			Str("session_id", sessionID).
			Str("conv_id", mergedEvent.ConversationID).
			Int("merged_count", len(pending)).
			Int("content_len", len(mergedEvent.Content)).
			Msg("[Inbox][Debounce] window closed, triggering AI with merged messages")

		s.triggerAIForEvent(bctx, &mergedEvent)
	})
}

func (s *InboxIngressService) drainOnePending(ctx context.Context, sessionID string) {
	if s.cache == nil || sessionID == "" {
		return
	}
	if _, err := s.cache.LPop(ctx, InboxPendingKey+sessionID); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("session_id", sessionID).Msg("[Inbox][Debounce] drain pending failed (TTL will reclaim)")
	}
}

func coalescePending(pending []string) []string {
	if len(pending) > aiDebounceMaxMessages {
		pending = pending[:aiDebounceMaxMessages]
	}
	for i, j := 0, len(pending)-1; i < j; i, j = i+1, j-1 {
		pending[i], pending[j] = pending[j], pending[i]
	}
	return pending
}
