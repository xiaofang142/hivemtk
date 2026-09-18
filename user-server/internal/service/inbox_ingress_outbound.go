package service

import (
	"context"

	"errors"

	"fmt"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/tracing"
)

func (s *InboxIngressService) ListFailedOutbound(ctx context.Context, channel, accountID string) ([]*model.MessageHub, error) {
	if s.hubRepo == nil {
		return nil, nil
	}
	list, _, err := s.hubRepo.ListByHubQuery(ctx, repository.HubListQuery{
		Platform:  channel,
		AccountID: accountID,
		Direction: "outbound",
		Status:    model.BridgeAckStatusFailed,
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// MarkOutboundDelivered 将离线补发成功的出站消息标记为已送达，
// 避免重复补发与坐席 UI 长期显示 failed。
func (s *InboxIngressService) MarkOutboundDelivered(ctx context.Context, hub *model.MessageHub) error {
	if s.hubRepo == nil || hub == nil {
		return nil
	}
	hub.Status = model.BridgeAckStatusDelivered
	return s.hubRepo.Update(ctx, hub)
}

func (s *InboxIngressService) DeliverOutbound(ctx context.Context, h *model.MessageHub) error {
	if h == nil {
		return errors.New("nil hub message")
	}
	if h.Direction == "" {
		h.Direction = "outbound"
	}
	if h.Status == "" {
		h.Status = "pending"
	}
	if h.SentAt.IsZero() {
		h.SentAt = time.Now()
	}
	if h.TraceID == "" {
		h.TraceID = tracing.LinkOutboundTraceID(ctx, h.ConversationID)
	}
	if err := s.hubRepo.Create(ctx, h); err != nil {
		return err
	}

	if GlobalSSEPublisher != nil && h != nil && h.ID != 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[SSE] GlobalSSEPublisher panic recovered in DeliverOutbound: %v", r)
				}
			}()
			GlobalSSEPublisher(
				h.Platform, h.AccountID,
				uint64(h.ID),
				h.ConversationID,
				h.MsgType,
				h.ReceiverID,
				h.Content,
				h.IsAIReply,
				h.SentAt,
			)
		}()
	}

	tracing.RecordNode(ctx, tracing.NodeSpan{
		TraceID:        h.TraceID,
		ConversationID: h.ConversationID,
		AccountID:      h.AccountID,
		Channel:        h.Platform,
		Node:           tracing.NodeOutboundEnqueue,
		Direction:      h.Direction,
		MsgID:          h.MsgID,
		Input: map[string]any{
			"channel":     h.Platform,
			"account_id":  h.AccountID,
			"conv_id":     h.ConversationID,
			"content_len": len(h.Content),
			"direction":   h.Direction,
			"is_ai_reply": h.IsAIReply,
		},
		Output: map[string]any{
			"msg_id": h.MsgID,
			"status": h.Status,
			"id":     h.ID,
		},
		Expected: "手动/代发回复落库 outbox(status=pending)，待下行出库",
		Status:   tracing.StatusOk,
	})

	if s.inboxSvc != nil {
		if _, err := s.inboxSvc.UpsertFromHubMessage(ctx, h); err != nil {
			logger.Warnf("[Inbox] 人工代发同步统一收件箱失败(conv=%s): %v", h.ConversationID, err)
		}
	}
	return nil
}

// ListPendingOutbound 查询某账号在某桥接渠道下"出站且待下发"的消息（下发队列）。
// 供桥接扩展独立轮询（通道C·下发轮询）拉取后转发到对应网页渠道。
// 仅返回 status='pending' 的出站消息；已 delivered 的被前端确认后排除，failed 的走离线补发。
func (s *InboxIngressService) ListPendingOutbound(ctx context.Context, channel, accountID string) ([]*model.MessageHub, error) {
	if s.hubRepo == nil {
		return nil, nil
	}
	list, _, err := s.hubRepo.ListByHubQuery(ctx, repository.HubListQuery{
		Platform:  channel,
		AccountID: accountID,
		Direction: "outbound",
		Status:    "pending",
		OrderBy:   "id ASC",
		PageSize:  50,
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *InboxIngressService) ListPendingOutboundLimit(ctx context.Context, channel, accountID string, limit int) ([]*model.MessageHub, error) {
	if s.hubRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	list, _, err := s.hubRepo.ListByHubQuery(ctx, repository.HubListQuery{
		Platform:  channel,
		AccountID: accountID,
		Direction: "outbound",
		Status:    "pending",
		OrderBy:   "id ASC",
		PageSize:  limit,
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// InboxOutboundClaimTimeout inflight 行最大存活：超过则被 ClaimPendingOutbound 惰性回收为 pending。
// 须大于单轮「下发+ack」时延（前端 pollInterval≈3s、发送+网络往返通常 < 数秒），留足余量。
const InboxOutboundClaimTimeout = 30 * time.Second

// ClaimPendingOutbound 服务端权威认领下发消息（根除重复转发）。详见 repository.ClaimPendingOutbound。
// 由桥接 GetBridgeOutbox 调用，取代旧的「仅 SELECT pending 不排他」查询。
func (s *InboxIngressService) ClaimPendingOutbound(ctx context.Context, channel, accountID string, limit int) ([]*model.MessageHub, error) {
	if s.hubRepo == nil {
		return nil, nil
	}
	rows, err := s.hubRepo.ClaimPendingOutbound(ctx, channel, accountID, limit, InboxOutboundClaimTimeout)
	if err != nil {
		return nil, err
	}
	out := make([]*model.MessageHub, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}

// AckOutboundDelivered 将扩展确认已下发的出站消息标记为 delivered（通道B·状态上报）。
// 仅对归属当前 (channel, accountID) 的 msg_id 生效，防止越权标记他人消息。
// 返回本次实际翻转为 delivered 的行数（幂等：已 delivered 的不计入）。
//
// 注意：msg_id 由 (channel+content) 生成，同一内容可出现在多个会话（复合唯一索引 (msg_id, conversation_id)
// 允许同 msg_id 跨会话存储）。因此必须批量更新该 (channel, account_id) 下所有匹配 msg_id 的 pending 行，
// 而非仅 GetByMsgID 返回的单行——否则跨会话的重复出站消息会永远停留在 pending，污染 stuck_unreachable 监控。
func (s *InboxIngressService) AckOutboundDelivered(ctx context.Context, channel, accountID string, msgIDs []string) (int, error) {
	if s.hubRepo == nil || len(msgIDs) == 0 {
		return 0, nil
	}

	affected, err := s.hubRepo.AckOutboundDeliveredBatch(ctx, channel, accountID, msgIDs)
	if err != nil {
		return 0, err
	}

	ackTimer := tracing.StartSpan()
	for _, id := range msgIDs {
		if id == "" {
			continue
		}
		hub, _ := s.hubRepo.GetByMsgID(ctx, id)
		var traceID, convID string
		if hub != nil {
			traceID, convID = hub.TraceID, hub.ConversationID
		}
		tracing.RecordNode(ctx, tracing.NodeSpan{
			TraceID:        traceID,
			ConversationID: convID,
			AccountID:      accountID,
			Channel:        channel,
			Node:           tracing.NodeDeliveredAck,
			Direction:      "outbound",
			MsgID:          id,
			Input: map[string]any{
				"channel":    channel,
				"account_id": accountID,
				"msg_id":     id,
			},
			Output: map[string]any{
				"status": model.BridgeAckStatusDelivered,
			},
			DurationMs: ackTimer.ElapsedMs(),
			Expected:   "pending → delivered（桥接已成功转发到网页）",
			Status:     tracing.StatusOk,
		})
	}
	return int(affected), nil
}

type AckOutboundItem struct {
	MsgID  string `json:"msg_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type AckOutboundResult struct {
	AffectedCount    int               `json:"affected_count"`
	AckedItemsCount  int               `json:"acked_items_count"`
	FailedItemsCount int               `json:"failed_items_count"`
	DuplicateCount   int               `json:"duplicate_count"`
	NotFoundCount    int               `json:"not_found_count"`
	NotInScopeCount  int               `json:"not_in_scope_count"`
	Items            []AckOutboundItem `json:"items"`
}

func (s *InboxIngressService) AckOutboundDeliveredDetailed(
	ctx context.Context,
	channel, accountID string,
	msgIDs []string,
	conversationID string,
	terminalStatus string,
	perItem map[string]BridgeOutboundAckInput,
) (*AckOutboundResult, error) {
	result := &AckOutboundResult{
		Items: make([]AckOutboundItem, 0, len(msgIDs)),
	}
	if s.hubRepo == nil {
		return nil, errors.New("ack failed: hub repo not configured")
	}
	if len(msgIDs) == 0 {
		return result, nil
	}

	if terminalStatus == "" {
		terminalStatus = model.BridgeAckStatusDelivered
	}
	if terminalStatus != model.BridgeAckStatusDelivered && terminalStatus != model.BridgeAckStatusFailed {
		return nil, fmt.Errorf("invalid terminal status: %q (must be delivered or failed)", terminalStatus)
	}

	seen := make(map[string]struct{}, len(msgIDs))
	deduped := make([]string, 0, len(msgIDs))
	for _, id := range msgIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		return result, nil
	}

	rows, err := s.hubRepo.GetByMsgIDsInScopeWithConv(ctx, channel, accountID, conversationID, deduped)
	if err != nil {
		return nil, err
	}
	hubByMsgID := make(map[string]*model.MessageHub, len(rows))
	for i := range rows {
		h := rows[i]
		hubByMsgID[h.MsgID] = &h
	}

	toAck := make([]string, 0, len(deduped))
	for _, id := range deduped {
		h, exists := hubByMsgID[id]
		if !exists {
			result.Items = append(result.Items, AckOutboundItem{MsgID: id, Status: model.BridgeAckStatusNotFound})
			result.NotFoundCount++
			continue
		}
		if h.Status == terminalStatus {
			result.Items = append(result.Items, AckOutboundItem{MsgID: id, Status: model.BridgeAckStatusDuplicate})
			result.DuplicateCount++
			continue
		}
		toAck = append(toAck, id)
	}

	updatedIDSet := make(map[string]struct{}, len(toAck))
	if len(toAck) > 0 {
		updatedIDs, affected, err := s.hubRepo.AckOutboundDeliveredBatchReturningWithStatus(ctx, channel, accountID, conversationID, terminalStatus, toAck)
		if err != nil {
			return nil, err
		}
		result.AffectedCount = int(affected)
		for _, id := range updatedIDs {
			updatedIDSet[id] = struct{}{}
		}

		for _, id := range toAck {
			if _, ok := updatedIDSet[id]; !ok {
				continue
			}
			st := model.BridgeAckStatusAcked
			if terminalStatus == model.BridgeAckStatusFailed {
				st = model.BridgeAckStatusFailed
			}
			item := AckOutboundItem{MsgID: id, Status: st}
			if perItem != nil {
				if inp, ok := perItem[id]; ok && inp.Error != "" {
					item.Error = inp.Error
				}
			}
			result.Items = append(result.Items, item)
		}

		for _, id := range toAck {
			if _, ok := updatedIDSet[id]; ok {
				continue
			}
			result.Items = append(result.Items, AckOutboundItem{MsgID: id, Status: model.BridgeAckStatusDuplicate})
			result.DuplicateCount++
		}
	}

	finalItems := make([]AckOutboundItem, 0, len(result.Items))
	ackedItemsCount := 0
	failedItemsCount := 0
	for _, item := range result.Items {
		switch item.Status {
		case model.BridgeAckStatusAcked, model.BridgeAckStatusFailed:
			if _, ok := updatedIDSet[item.MsgID]; ok {
				if item.Status == model.BridgeAckStatusFailed {
					failedItemsCount++
				} else {
					ackedItemsCount++
				}
				if perItem != nil {
					if inp, ok := perItem[item.MsgID]; ok && inp.Error != "" {
						item.Error = inp.Error
					}
				}
				finalItems = append(finalItems, item)
			} else {
				finalItems = append(finalItems, AckOutboundItem{MsgID: item.MsgID, Status: model.BridgeAckStatusDuplicate})
				result.DuplicateCount++
			}
		default:
			finalItems = append(finalItems, item)
		}
	}
	result.Items = finalItems
	result.AckedItemsCount = ackedItemsCount
	result.FailedItemsCount = failedItemsCount

	if conversationID == "" && len(deduped) > 0 {
		var suspected []string
		for _, it := range result.Items {
			if it.Status == model.BridgeAckStatusNotFound {
				suspected = append(suspected, it.MsgID)
			}
		}
		if len(suspected) > 0 {
			anyExists, err := s.hubRepo.AnyExistsByMsgIDs(ctx, channel, suspected)
			if err == nil {
				newItems := make([]AckOutboundItem, 0, len(result.Items))
				notInScope := 0
				for _, it := range result.Items {
					if it.Status == model.BridgeAckStatusNotFound && anyExists[it.MsgID] {
						it.Status = model.BridgeAckStatusNotInScope
						notInScope++
						result.NotFoundCount--
					}
					newItems = append(newItems, it)
				}
				result.Items = newItems
				result.NotInScopeCount = notInScope
			}
		}
	}

	ackTimer := tracing.StartSpan()
	for _, item := range result.Items {
		if item.Status == model.BridgeAckStatusNotFound || item.Status == model.BridgeAckStatusNotInScope {
			continue
		}
		h := hubByMsgID[item.MsgID]
		var traceID, convID string
		if h != nil {
			traceID, convID = h.TraceID, h.ConversationID
		}
		var traceStatus string
		switch item.Status {
		case model.BridgeAckStatusAcked:
			traceStatus = tracing.StatusOk
		case model.BridgeAckStatusFailed:
			traceStatus = tracing.StatusAbnormal
		case model.BridgeAckStatusDuplicate:
			traceStatus = tracing.StatusSkipped
		default:
			traceStatus = tracing.StatusOk
		}
		tracing.RecordNode(ctx, tracing.NodeSpan{
			TraceID:        traceID,
			ConversationID: convID,
			AccountID:      accountID,
			Channel:        channel,
			Node:           tracing.NodeDeliveredAck,
			Direction:      "outbound",
			MsgID:          item.MsgID,
			Input: map[string]any{
				"channel":         channel,
				"account_id":      accountID,
				"msg_id":          item.MsgID,
				"ack_status":      item.Status,
				"terminal":        terminalStatus,
				"error":           item.Error,
				"conversation_id": conversationID,
			},
			Output: map[string]any{
				"status": terminalStatus,
			},
			DurationMs: ackTimer.ElapsedMs(),
			Expected:   "pending → " + terminalStatus + "（桥接已确认送达/失败）",
			Status:     traceStatus,
		})
	}
	return result, nil
}

// BridgeOutboundAckInput v2 协议入参中间结构（service 层解耦用，避免 handler 私有类型穿透）。
// 来自 handler_http.BridgeOutboxAckItem（v2 items[]），按 msg_id 索引。
type BridgeOutboundAckInput struct {
	MsgID          string
	ConversationID string
	Status         string
	Error          string
}
