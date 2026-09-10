package service

import (
	"context"

	"log"
	"runtime/debug"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/websocket"
)

func (s *InboxIngressService) persistMessage(ctx context.Context, event *model.MessageEvent) error {
	if s.hubRepo == nil {
		return nil
	}
	accountID := "default"
	if event.Extra != nil {
		if v, ok := event.Extra["account_id"].(string); ok && v != "" {
			accountID = v
		}
	}

	now := time.Now()
	sentAt := event.Timestamp
	if sentAt.IsZero() {
		sentAt = now
	} else if sentAt.After(now.Add(InboxBackfillFutureTolerance)) {

		logger.Ctx(ctx).Info().
			Str("channel", event.Channel).
			Str("conv_id", event.ConversationID).
			Str("event_id", event.EventID).
			Time("orig_timestamp", event.Timestamp).
			Msg("[Inbox] 时序修正：timestamp 未来超过容差，修正为 now()")
		sentAt = now
	}

	direction := "inbound"
	if event.SenderType == "self" || event.SenderType == "agent" {
		direction = "outbound"
	}

	if direction == "outbound" && event.ConversationID != "" && (event.ReceiverID == "" || event.ReceiverID == event.SenderID) {
		event.ReceiverID = event.ConversationID
	}
	hub := &model.MessageHub{
		MsgID:          event.EventID,
		Platform:       event.Channel,
		AccountID:      accountID,
		Direction:      direction,
		MsgType:        event.MsgType,
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ReceiverID:     event.ReceiverID,
		Content:        event.Content,
		MediaURL:       event.MediaURL,
		ConversationID: event.ConversationID,
		IsGroup:        event.IsGroup,
		GroupID:        event.GroupID,
		IsAIReply:      event.IsAIReply,
		AIAgent:        event.AIAgent,
		IsRead:         false,
		SentAt:         sentAt,
		Extra:          nil,
	}

	hub.DedupHash = ContentHashWithSender(event.Channel, s.senderKeyForDedup(event), strings.TrimSpace(event.Content))

	if hub.TraceID == "" {
		if hub.Direction == "inbound" {
			hub.TraceID = tracing.LinkInboundTraceID(ctx, hub.ConversationID)
		} else {
			hub.TraceID = tracing.LinkOutboundTraceID(ctx, hub.ConversationID)
		}
	}
	if event.Extra != nil {
		extra := model.JSONMap{}
		for k, v := range event.Extra {
			extra[k] = v
		}
		hub.Extra = extra
	}
	if len(event.History) > 0 {
		if hub.Extra == nil {
			hub.Extra = model.JSONMap{}
		}
		hub.Extra["history"] = event.History
	}

	var persisted bool
	defer func() {
		if !persisted || hub == nil {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			if s.leadMiningSvc != nil {
				s.leadMiningSvc.Enqueue(hub)
			}
			websocket.BroadcastToAgents(websocket.TypeNewMessage, map[string]any{
				"msg_id":          hub.MsgID,
				"platform":        hub.Platform,
				"conversation_id": hub.ConversationID,
				"direction":       hub.Direction,
				"sender_name":     hub.SenderName,
				"content":         hub.Content,
				"sent_at":         hub.SentAt,
			})

			if hub.Direction == "inbound" && event.SessionID != "" {
				if s.feedbackRepo != nil {
					if rows, err := s.feedbackRepo.UpdateCustomerAcceptBySession(ctx, event.SessionID); err != nil {
						logger.Warnf("[feedback] UpdateCustomerAcceptBySession failed session=%s: %v", event.SessionID, err)
					} else if rows > 0 {
						logger.Debugf("[feedback] customer_accept marked session=%s rows=%d", event.SessionID, rows)
					}
				}
			}
		}()
	}()

	_, err := s.withIngestLock(ctx, event.ConversationID, func() error {

		if s.hubRepo != nil && event.ConversationID != "" {
			if anchor, aerr := s.hubRepo.GetLastByConversation(ctx, event.ConversationID); aerr == nil && anchor != nil {
				if sentAt.Before(anchor.SentAt) {
					logger.Ctx(ctx).Info().
						Str("channel", event.Channel).
						Str("conv_id", event.ConversationID).
						Str("event_id", event.EventID).
						Time("msg_timestamp", sentAt).
						Time("anchor_timestamp", anchor.SentAt).
						Msg("[Inbox] 时序锚点：消息 timestamp 早于锚点，标记为 backfill 历史堆积")
				}
			}
		}

		if s.inboxSvc != nil && hub.Direction == "inbound" {
			ingestTimer := tracing.StartSpan()
			if _, uerr := s.inboxSvc.UpsertFromHubMessageTx(context.Background(), hub, s.hubRepo); uerr != nil {
				if isDuplicateKey(uerr) {
					logger.Warnf("[Inbox] message_hub duplicate msg_id (idempotent skip): msg_id=%s session=%s",
						event.EventID, event.SessionID)
					return nil
				}
				return uerr
			}
			persisted = true

			tracing.RecordNode(ctx, tracing.NodeSpan{
				TraceID:        hub.TraceID,
				ConversationID: hub.ConversationID,
				AccountID:      hub.AccountID,
				Channel:        hub.Platform,
				Node:           tracing.NodeIngest,
				Direction:      hub.Direction,
				MsgID:          hub.MsgID,
				Input: map[string]any{
					"channel":     event.Channel,
					"account_id":  accountID,
					"conv_id":     event.ConversationID,
					"sender_id":   event.SenderID,
					"sender_type": event.SenderType,
					"event_id":    event.EventID,
					"content_len": len(event.Content),
					"direction":   hub.Direction,
				},
				Output: map[string]any{
					"msg_id": hub.MsgID,
					"status": hub.Status,
					"id":     hub.ID,
				},
				DurationMs: ingestTimer.ElapsedMs(),
				Expected:   "客户消息落库 message_hub + 同步 inbox_conversations（平台统一收件箱可见）",
				Status:     tracing.StatusOk,
			})

			tracing.RecordNode(ctx, tracing.NodeSpan{
				TraceID:        hub.TraceID,
				ConversationID: hub.ConversationID,
				AccountID:      hub.AccountID,
				Channel:        hub.Platform,
				Node:           tracing.NodeInboxSync,
				Direction:      hub.Direction,
				MsgID:          hub.MsgID,
				Input:          map[string]any{"conv_id": event.ConversationID},
				Output:         map[string]any{"synced": true, "id": hub.ID},
				Expected:       "inbox_conversations 已建立/更新（避免 sync_gap：桥接活跃但平台收件箱看不到）",
				Status:         tracing.StatusOk,
			})
			return nil
		}
		if cerr := s.hubRepo.Create(ctx, hub); cerr != nil {
			if isDuplicateKey(cerr) {
				logger.Warnf("[Inbox] message_hub duplicate msg_id (idempotent skip): msg_id=%s session=%s",
					event.EventID, event.SessionID)
				return nil
			}
			return cerr
		}
		persisted = true

		tracing.RecordNode(ctx, tracing.NodeSpan{
			TraceID:        hub.TraceID,
			ConversationID: hub.ConversationID,
			AccountID:      hub.AccountID,
			Channel:        hub.Platform,
			Node:           tracing.NodeIngest,
			Direction:      hub.Direction,
			MsgID:          hub.MsgID,
			Input: map[string]any{
				"channel":     event.Channel,
				"account_id":  accountID,
				"conv_id":     event.ConversationID,
				"sender_id":   event.SenderID,
				"sender_type": event.SenderType,
				"event_id":    event.EventID,
				"content_len": len(event.Content),
				"direction":   hub.Direction,
			},
			Output: map[string]any{
				"msg_id": hub.MsgID,
				"status": hub.Status,
				"id":     hub.ID,
			},
			Expected: "消息落库 message_hub",
			Status:   tracing.StatusOk,
		})
		return nil
	})
	return err
}

func (s *InboxIngressService) PersistBridgeHistory(ctx context.Context, event *model.MessageEvent, direction string) error {
	if err := s.NormalizeEvent(ctx, event); err != nil {
		return err
	}
	if direction == "" {
		direction = "inbound"
	}

	if direction == "outbound" && event.ConversationID != "" && (event.ReceiverID == "" || event.ReceiverID == event.SenderID) {
		event.ReceiverID = event.ConversationID
	}

	if s.hubRepo != nil && event.EventID != "" {
		if existing, err := s.hubRepo.GetByMsgID(ctx, event.EventID); err == nil && existing != nil && existing.ConversationID == event.ConversationID {
			logger.Ctx(ctx).Info().
				Str("module", "bridge").
				Str("event_id", event.EventID).
				Str("existing_direction", existing.Direction).
				Str("conv_id", event.ConversationID).
				Str("channel", event.Channel).
				Str("sender_id", event.SenderID).
				Msg("[Inbox] PersistBridgeHistory 钩子2 命中：msg_id 已存在，幂等跳过（防回环）")
			return nil
		}
	}

	if s.hubRepo != nil && event.Content != "" && event.Channel != "" {
		canonicalHash := ContentHashMsgID(event.Channel, event.ConversationID, event.Content)
		if existing, err := s.hubRepo.GetByContentHash(ctx, canonicalHash); err == nil && existing != nil && existing.ConversationID == event.ConversationID {
			logger.Ctx(ctx).Info().
				Str("module", "bridge").
				Str("canonical_hash", canonicalHash).
				Str("existing_msg_id", existing.MsgID).
				Str("existing_direction", existing.Direction).
				Str("conv_id", event.ConversationID).
				Str("channel", event.Channel).
				Str("sender_id", event.SenderID).
				Msg("[Inbox] PersistBridgeHistory 钩子2.5 命中：canonical contentHash 已存在，幂等跳过（防回环）")
			return nil
		}
		if existing, err := s.hubRepo.GetByPlatformContent(ctx, event.Channel, event.Content); err == nil && existing != nil {
			logger.Ctx(ctx).Info().
				Str("module", "bridge").
				Str("existing_msg_id", existing.MsgID).
				Str("existing_direction", existing.Direction).
				Str("conv_id", event.ConversationID).
				Str("channel", event.Channel).
				Str("sender_id", event.SenderID).
				Msg("[Inbox] PersistBridgeHistory 钩子2.5 命中：platform+content 已存在，幂等跳过（防回环）")
			return nil
		}

		if existing, err := s.hubRepo.GetByPlatformContentNormalized(ctx, event.Channel, event.Content); err == nil && existing != nil {
			logger.Ctx(ctx).Info().
				Str("module", "bridge").
				Str("existing_msg_id", existing.MsgID).
				Str("existing_direction", existing.Direction).
				Str("existing_conv_id", existing.ConversationID).
				Str("conv_id", event.ConversationID).
				Str("channel", event.Channel).
				Str("sender_id", event.SenderID).
				Msg("[Inbox] PersistBridgeHistory 钩子2.5 命中：normalized platform+content 已存在，幂等跳过（防回环）")
			return nil
		}
	}
	return s.persistHistoryMessage(ctx, event, direction)
}

func (s *InboxIngressService) persistHistoryMessage(ctx context.Context, event *model.MessageEvent, direction string) error {
	if s.hubRepo == nil {
		return nil
	}
	accountID := "default"
	if event.Extra != nil {
		if v, ok := event.Extra["account_id"].(string); ok && v != "" {
			accountID = v
		}
	}
	hub := &model.MessageHub{
		MsgID:          event.EventID,
		Platform:       event.Channel,
		AccountID:      accountID,
		Direction:      direction,
		MsgType:        event.MsgType,
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ReceiverID:     event.ReceiverID,
		Content:        event.Content,
		MediaURL:       event.MediaURL,
		ConversationID: event.ConversationID,
		IsGroup:        event.IsGroup,
		GroupID:        event.GroupID,

		IsAIReply: direction == "outbound",
		AIAgent:   event.AIAgent,
		IsRead:    direction == "outbound",
		SentAt:    event.Timestamp,
	}
	if event.Extra != nil {
		extra := model.JSONMap{}
		for k, v := range event.Extra {
			extra[k] = v
		}
		hub.Extra = extra

		if v, ok := event.Extra["status"].(string); ok && v != "" {
			hub.Status = v
		}
	}
	if err := s.hubRepo.Create(ctx, hub); err != nil {

		if isDuplicateKey(err) {
			logger.Warnf("[Inbox] message_hub duplicate msg_id (history idempotent skip): msg_id=%s session=%s",
				event.EventID, event.ConversationID)
			return nil
		}
		return err
	}

	if direction == "outbound" && GlobalSSEPublisher != nil && hub.ID != 0 {
		logger.Ctx(ctx).Debug().
			Int("hub_id", int(hub.ID)).
			Str("channel", hub.Platform).
			Str("account_id", hub.AccountID).
			Str("conv_id", hub.ConversationID).
			Msg("[SSE] persistHistoryMessage calling GlobalSSEPublisher")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[SSE] GlobalSSEPublisher panic recovered in persistHistoryMessage: %v", r)
				}
			}()
			GlobalSSEPublisher(
				hub.Platform, hub.AccountID,
				uint64(hub.ID),
				hub.ConversationID,
				hub.MsgType,
				hub.ReceiverID,
				hub.Content,
				hub.IsAIReply,
				hub.SentAt,
			)
		}()
	}

	if s.inboxSvc != nil {

		if _, err := s.inboxSvc.UpsertFromHubMessage(context.Background(), hub); err != nil {
			logger.Warnf("[Inbox] 桥接历史消息同步统一收件箱失败(conv=%s): %v", event.ConversationID, err)
		}
	}
	return nil
}
