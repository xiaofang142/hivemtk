package service

import (
	"context"

	"runtime/debug"
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
		// 非文本消息的 msg_id 用带类型口径（002-multi-type-messages）：event_id 为空时
		// 兜底生成的哈希并入 msg_type+media_url，否则同会话两张图（content 同为「[图片]」
		// 占位符）哈希碰撞，第二条被钩子2 判重吞掉。文本消息与旧口径逐字节一致。
		MsgID:     firstNonEmpty(event.EventID, ContentHashMsgIDTyped(event.Channel, event.ConversationID, event.MsgType, event.MediaURL, event.Content)),
		Platform:  event.Channel,
		AccountID: accountID,
		Direction: direction,
		// WA / TG / QQ 三渠道共同的落库点，且不经 hub.Push → Normalize 的词表校验：
		// event.MsgType 是渠道官方名（WA 的 document/sticker…），原样写进去就是写一行
		// 工作台「按类型筛选」（msg_type = ?）与 by_msg_type 统计永远筛不到的记录。
		// 这里只归一不校验：校验会把整条消息丢掉。
		MsgType:        InboundHubMsgType(event.MsgType),
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ReceiverID:     event.ReceiverID,
		Content:        event.Content,
		MediaURL:       model.TruncateMediaURL(event.MediaURL),
		ConversationID: event.ConversationID,
		IsGroup:        event.IsGroup,
		GroupID:        event.GroupID,
		IsAIReply:      event.IsAIReply,
		AIAgent:        event.AIAgent,
		IsRead:         false,
		SentAt:         sentAt,
		Extra:          nil,
	}

	// dedup_hash 列与 interceptInbound 的 Redis 窗口键共用 MediaAwareDedupInput（F1 收口）：
	// 媒体消息并入 msg_type+media_url，键与列可互相对账；文本消息逐字节与旧口径一致。
	hub.DedupHash = ContentHashWithSender(event.Channel, s.senderKeyForDedup(event), MediaAwareDedupInput(event.MsgType, event.MediaURL, event.Content))

	if hub.Direction == "inbound" {
		hub.Status = "received"
	}

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
					logger.Errorf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			if s.leadMiningSvc != nil {
				s.leadMiningSvc.Enqueue(hub)
			}
			if err := websocket.BroadcastToAgents(ctx, websocket.TypeNewMessage, map[string]any{
				"msg_id":          hub.MsgID,
				"platform":        hub.Platform,
				"conversation_id": hub.ConversationID,
				"direction":       hub.Direction,
				"sender_name":     hub.SenderName,
				"content":         hub.Content,
				"sent_at":         hub.SentAt,
			}); err != nil {
				logger.Warnf("[Ingest] 广播新消息给客服失败 conv=%s: %v", hub.ConversationID, err)
			}

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
			if _, uerr := s.inboxSvc.UpsertFromHubMessageTx(ctx, hub, s.hubRepo); uerr != nil {
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

	// 钩子2.5 全部按内容判等，是「内容即身份」时代的产物。上报方一旦用 `mh:<hash>#<n>` 这种
	// 发生次数身份显式声明「这是同一句话的第二条」（§8.3-17），内容维度就没有权判它重复；
	// 上面的钩子2（msg_id 精确判等）不受影响，所以同一帧重发依旧只有一行。
	if s.hubRepo != nil && event.Content != "" && event.Channel != "" && !eventAssertsDistinctMessage(event.EventID) {
		echoSince := time.Now().Add(-InboxOutboundEchoWindow)
		canonicalHash := ContentHashMsgID(event.Channel, event.ConversationID, event.Content)
		if existing, err := s.hubRepo.GetByContentHash(ctx, canonicalHash, event.ConversationID, echoSince); err == nil && existing != nil && existing.ConversationID == event.ConversationID {
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
		if existing, err := s.hubRepo.GetByPlatformContent(ctx, event.Channel, event.Content, event.ConversationID, echoSince); err == nil && existing != nil {
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

		if existing, err := s.hubRepo.GetByPlatformContentNormalized(ctx, event.Channel, event.Content, event.ConversationID, echoSince); err == nil && existing != nil {
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
		// 同 persistMessage：非文本空 event_id 兜底哈希并入类型与媒体（002-multi-type-messages）。
		MsgID:     firstNonEmpty(event.EventID, ContentHashMsgIDTyped(event.Channel, event.ConversationID, event.MsgType, event.MediaURL, event.Content)),
		Platform:  event.Channel,
		AccountID: accountID,
		Direction: direction,
		// WA / TG / QQ 三渠道共同的落库点，且不经 hub.Push → Normalize 的词表校验：
		// event.MsgType 是渠道官方名（WA 的 document/sticker…），原样写进去就是写一行
		// 工作台「按类型筛选」（msg_type = ?）与 by_msg_type 统计永远筛不到的记录。
		// 这里只归一不校验：校验会把整条消息丢掉。
		MsgType:        InboundHubMsgType(event.MsgType),
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ReceiverID:     event.ReceiverID,
		Content:        event.Content,
		MediaURL:       model.TruncateMediaURL(event.MediaURL),
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

	// 历史帧是回放事实，不是待下发队列：这里绝不发布 SSE。发布会让扩展把同一条文案
	// 重新打进页面，下一轮窗口历史再把它带回来，形成自我放大的重复发送回环。

	if s.inboxSvc != nil {

		if _, err := s.inboxSvc.UpsertFromHubMessage(ctx, hub); err != nil {
			logger.Warnf("[Inbox] 桥接历史消息同步统一收件箱失败(conv=%s): %v", event.ConversationID, err)
		}
	}
	return nil
}
