package service

import (
	"context"

	"errors"

	"fmt"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/tracing"
)

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
		// 派生带租户的 ctx（认领要 UPDATE message_hub，on 模式下无租户即被守卫拒绝），
		// 口径同 bridge_outbound.go 的同名调用点。
		pubCtx := tenant.BackgroundWithTenant(ctx)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[SSE] GlobalSSEPublisher panic recovered in DeliverOutbound: %v", r)
				}
			}()
			GlobalSSEPublisher(
				pubCtx,
				h.Platform, h.AccountID,
				uint64(h.ID),
				h.ConversationID,
				h.MsgType,
				h.ReceiverID,
				h.Content,
				h.MediaURL,
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
	AffectedCount    int `json:"affected_count"`
	AckedItemsCount  int `json:"acked_items_count"`
	FailedItemsCount int `json:"failed_items_count"`
	// UnknownItemsCount CH-37b 刀4（R2/R6）：本次翻转为 unknown 终态的行数（结果不明，
	// 永不自动重发、待人工/API 对账）。additive 字段：旧客户端忽略未知 JSON 键不受影响。
	UnknownItemsCount int               `json:"unknown_items_count"`
	DuplicateCount    int               `json:"duplicate_count"`
	NotFoundCount     int               `json:"not_found_count"`
	NotInScopeCount   int               `json:"not_in_scope_count"`
	Items             []AckOutboundItem `json:"items"`
}

func (s *InboxIngressService) AckOutboundDeliveredDetailed(
	ctx context.Context,
	channel, accountID string,
	msgIDs []string,
	conversationID string,
	terminalStatus string,
	perItem map[string]BridgeOutboundAckInput,
) (*AckOutboundResult, error) {
	return s.ackOutboundDetailed(ctx, channel, accountID, msgIDs, conversationID, terminalStatus, perItem, nil)
}

// AckOutboundClaimFence CH-37b 刀2 claim 栅栏入参（乐观并发）：msg_id → 调用方认领时所见的
// claimed_at。值必须取自认领返回行的 DB 读值（ClaimPendingOutbound 返回行的 ClaimedAt），
// 不得用本地时钟拼造——PG timestamptz 是微秒精度，本地时钟值与库内存储值无法对上。
//
// 语义：仅当行当前 inflight 且 claimed_at 与所记一致才翻转；行已被回收/重领的迟到 ack
// （防「30s 重领后旧 acker 覆盖」）或已回 pending 的行不翻转，按 duplicate 分类回执
// （不翻转是硬保证，分类只影响回执读数）。键不在本批 msgIDs 内的条目不参与谓词。
type AckOutboundClaimFence map[string]time.Time

// AckOutboundDeliveredDetailedFenced CH-37b 刀2：带 claim 栅栏的详细 ack。
// 与 AckOutboundDeliveredDetailed 同一实现体，仅条件更新谓词不同（见 repository.
// AckOutboundDeliveredBatchReturningWithStatusFenced）；fence 为 nil/空等价 legacy 无栅栏。
func (s *InboxIngressService) AckOutboundDeliveredDetailedFenced(
	ctx context.Context,
	channel, accountID string,
	msgIDs []string,
	conversationID string,
	terminalStatus string,
	perItem map[string]BridgeOutboundAckInput,
	fence AckOutboundClaimFence,
) (*AckOutboundResult, error) {
	return s.ackOutboundDetailed(ctx, channel, accountID, msgIDs, conversationID, terminalStatus, perItem, fence)
}

// ackOutboundDetailed 详细 ack 共同实现体。fence 非 nil 且非空时走 repo 栅栏版条件更新，
// 其余路径与 legacy 完全一致（fence=nil 即 AckOutboundDeliveredDetailed 原语义）。
func (s *InboxIngressService) ackOutboundDetailed(
	ctx context.Context,
	channel, accountID string,
	msgIDs []string,
	conversationID string,
	terminalStatus string,
	perItem map[string]BridgeOutboundAckInput,
	fence AckOutboundClaimFence,
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
	// CH-37b 刀4（R2/R6）：terminalStatus 三值——delivered/failed 之外接受 unknown
	// （「已提交但结果不明」，落库 status='unknown'+push_error='ack_unknown'，永不自动重发，
	// 对账走 ReconcileUnknownOutbound）。词表收口 model.IsBridgeAckTerminalStatus。
	if !model.IsBridgeAckTerminalStatus(terminalStatus) {
		return nil, fmt.Errorf("invalid terminal status: %q (must be delivered, failed or unknown)", terminalStatus)
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
		var updatedIDs []string
		var affected int64
		var err error
		if len(fence) > 0 {
			// CH-37b 刀2 claim 栅栏：仅当行当前 inflight 且 claimed_at 与调用方所见一致才翻转。
			updatedIDs, affected, err = s.hubRepo.AckOutboundDeliveredBatchReturningWithStatusFenced(ctx, channel, accountID, conversationID, terminalStatus, toAck, fence)
		} else {
			updatedIDs, affected, err = s.hubRepo.AckOutboundDeliveredBatchReturningWithStatus(ctx, channel, accountID, conversationID, terminalStatus, toAck)
		}
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
			// 行级回执状态随终态走：delivered→acked、failed→failed、unknown→unknown
			// （CH-37b 刀4：unknown 是独立终态，不冒充 acked/failed，独立计量见 UnknownItemsCount）。
			st := model.BridgeAckStatusAcked
			switch terminalStatus {
			case model.BridgeAckStatusFailed:
				st = model.BridgeAckStatusFailed
			case model.BridgeAckStatusUnknown:
				st = model.BridgeAckStatusUnknown
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
	unknownItemsCount := 0
	for _, item := range result.Items {
		switch item.Status {
		case model.BridgeAckStatusAcked, model.BridgeAckStatusFailed, model.BridgeAckStatusUnknown:
			if _, ok := updatedIDSet[item.MsgID]; ok {
				switch item.Status {
				case model.BridgeAckStatusFailed:
					failedItemsCount++
				case model.BridgeAckStatusUnknown:
					unknownItemsCount++
				default:
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
	result.UnknownItemsCount = unknownItemsCount

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
		// Expected 文案随终态分叉（CH-37b 刀4）：unknown 不是「已确认送达/失败」，
		// 是「结果不明待对账」，文案不能冒充确定性结论。
		expected := "pending → " + terminalStatus + "（桥接已确认送达/失败）"
		if terminalStatus == model.BridgeAckStatusUnknown {
			expected = "pending → unknown（桥端结果不明：永不自动重发，待人工/API 对账）"
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
			Expected:   expected,
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

// ReconcileUnknownOutbound CH-37b 刀4（R2/R6）对账出口（受控迁移，unknown 的唯一出路）：
// 把 (channel, accountID) 下指定 id 的 unknown 行显式迁移到 delivered 或 failed，
// push_error 留操作来源 'reconciled_unknown:<operator>'。
//
// 约束与语义：
//   - 只接受 unknown 行：repo 谓词 status='unknown'，与其余任何取行/翻转路径零交集，
//     迟到 ack / 回收 / exhaust 对 unknown 行的 0 触碰由此不受对账影响；
//   - target 只许 delivered/failed（unknown→unknown 无意义，其余值报错）；
//   - operator 必填（人工账号/API 身份）——对账是显式人工动作，不留来源的迁移拒绝执行；
//   - 按**行 id** 定位而非 msg_id：msg_id 是内容哈希、可跨会话命中多行，受控迁移必须
//     指到具体行；调用方（管理侧 API/运维）从 unknown 列表查询取得 id；
//   - 租户作用域强制：ctx 须携带租户（on 模式下隔离 callback 对无租户 ctx fail-closed
//     拒绝，行数 0 + error；off 模式单租户直通，与仓内 message_hub 写路径同口径）。
//     跨租户行不在谓词命中集合，受影响行数 0。
//
// 返回实际迁移行数（幂等：非 unknown 行不计入，不报错——重复对账天然收敛）。
// 管理侧 HTTP API 刀5 起在 /api/inbox/outbound-unknown*（admin 组）。
func (s *InboxIngressService) ReconcileUnknownOutbound(ctx context.Context, channel, accountID string, ids []uint, target, operator string) (int64, error) {
	if s.hubRepo == nil {
		return 0, errors.New("reconcile failed: hub repo not configured")
	}
	affected, err := s.hubRepo.ReconcileUnknownOutbound(ctx, channel, accountID, ids, target, operator)
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		logger.Infof("[Inbox] unknown 对账迁移: channel=%s account=%s ids=%v target=%s operator=%q affected=%d",
			channel, accountID, ids, target, operator, affected)
	}
	return affected, nil
}

// OutboundUnknownListResult CH-37b 刀5：unknown 清单 + 总数（一问两答，管理面一次拉平）。
type OutboundUnknownListResult struct {
	Total int64                           `json:"total"`
	Rows  []repository.OutboundUnknownRow `json:"rows"`
}

// ListOutboundUnknownOutbound CH-37b 刀5：unknown 行清单（对账待办读数）。
// 调用方（管理侧 API）先取清单拿行 id，再走 ReconcileUnknownOutbound 显式迁移；
// 同样的 (channel, accountID, unknown) 谓词，清单与迁移看到的行集合一致。
// hubRepo 未装配返回 ErrDBNotReady——管理读口不静默给空（空清单会被误读为「无待办」）。
func (s *InboxIngressService) ListOutboundUnknownOutbound(ctx context.Context, channel, accountID string, limit, offset int) (*OutboundUnknownListResult, error) {
	if s.hubRepo == nil {
		return nil, repository.ErrDBNotReady
	}
	rows, err := s.hubRepo.ListOutboundUnknown(ctx, channel, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	total, err := s.hubRepo.CountOutboundUnknown(ctx, channel, accountID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []repository.OutboundUnknownRow{}
	}
	return &OutboundUnknownListResult{Total: total, Rows: rows}, nil
}

// CountOutboundUnknownOutbound CH-37b 刀5：unknown 行数直读（仪表盘/巡检轮询用，
// 不拉行清单）。hubRepo 未装配同样 ErrDBNotReady。
func (s *InboxIngressService) CountOutboundUnknownOutbound(ctx context.Context, channel, accountID string) (int64, error) {
	if s.hubRepo == nil {
		return 0, repository.ErrDBNotReady
	}
	return s.hubRepo.CountOutboundUnknown(ctx, channel, accountID)
}
