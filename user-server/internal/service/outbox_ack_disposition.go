package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

// 真实投递回执 → 逐入站消息处置台账的唯一推进口。
//
// deliverAIResult 只把 disposition 推到 processing（"已交给投递层"），从不把
// sendOutbound 的 accepted 当成平台已收到。本文件补上缺失的后半段：只有 channelbus
// outbox ack / bridge ack 明确报 delivered 时才推进到 replied，并同时落真实出站消息
// ID 与送达时间。failed/unknown/duplicate/not_found 一律不动台账。
//
// 反查链：ack 的 (tenant, platform, account, conversation, msg_id) → message_hub
// 出站行的 extra.source_message_id（入站消息号）→ 逐消息处置记录。缺任一环即
// fail-closed：不猜会话、不猜消息、不伪造送达时间。

// OutboxAckHubLookup 是 ACK 反查出站行的窄口；*repository.MessageHubRepository
// 隐式实现。只暴露按 (platform, account, conversation, msg_ids) 取出站行的能力。
type OutboxAckHubLookup interface {
	GetByMsgIDsInScopeWithConv(ctx context.Context, platform, accountID, conversationID string, msgIDs []string) ([]model.MessageHub, error)
}

// OutboxAckDispositionInput 来自 ACK 请求的一条权威定位信息。
// Status 取线路协议终态（delivered/failed/unknown）；缺省由 handler 归一为 delivered。
type OutboxAckDispositionInput struct {
	MsgID          string
	ConversationID string
	Status         string
}

// OutboxAckDisposition 是 ACK handler 依赖的窄口，便于用 fake 做 focused TDD。
type OutboxAckDisposition interface {
	Apply(ctx context.Context, platform, accountID string, inputs []OutboxAckDispositionInput) (int, error)
}

// outboxAckDispositionApplier 把真实 ACK 落到逐消息处置台账。
type outboxAckDispositionApplier struct {
	hubs         OutboxAckHubLookup
	dispositions ConversationDispositionStore
	now          func() time.Time
}

func NewOutboxAckDispositionApplier(hubs OutboxAckHubLookup, dispositions ConversationDispositionStore) OutboxAckDisposition {
	return &outboxAckDispositionApplier{hubs: hubs, dispositions: dispositions, now: time.Now}
}

// Apply 返回本次真实推进到 replied 的记录数。任何错误只影响台账推进，
// 由调用方记日志；已确认的 outbox ACK 结果不被回滚或改写。
func (a *outboxAckDispositionApplier) Apply(ctx context.Context, platform, accountID string, inputs []OutboxAckDispositionInput) (int, error) {
	if a == nil || a.hubs == nil || a.dispositions == nil {
		return 0, nil // 未装配：保持存量 ACK 行为
	}
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return 0, err // 无租户上下文 fail-closed
	}

	// 只处理真实 delivered；按 ACK 自报的会话分组反查，空会话仍可反查（v1 请求），
	// 由出站行自己的 conversation_id 提供权威会话。
	groups := make(map[string][]string)
	order := make([]string, 0)
	for _, in := range inputs {
		if in.Status != model.BridgeAckStatusDelivered {
			continue
		}
		msgID := strings.TrimSpace(in.MsgID)
		if msgID == "" {
			continue
		}
		conv := strings.TrimSpace(in.ConversationID)
		if _, ok := groups[conv]; !ok {
			order = append(order, conv)
		}
		groups[conv] = append(groups[conv], msgID)
	}

	deliveredAt := time.Now()
	if a.now != nil {
		deliveredAt = a.now()
	}
	applied := 0
	for _, conv := range order {
		rows, err := a.hubs.GetByMsgIDsInScopeWithConv(ctx, platform, accountID, conv, groups[conv])
		if err != nil {
			return applied, err
		}
		for i := range rows {
			hub := rows[i]
			n, err := a.applyHub(ctx, tid, &hub, deliveredAt)
			if err != nil {
				return applied, err
			}
			applied += n
		}
	}
	return applied, nil
}

func (a *outboxAckDispositionApplier) applyHub(ctx context.Context, tid uint, hub *model.MessageHub, deliveredAt time.Time) (int, error) {
	if hub == nil || hub.Direction != "outbound" {
		return 0, nil
	}
	sourceID := hubStringExtra(hub.Extra, "source_message_id")
	convID := strings.TrimSpace(hub.ConversationID)
	ackMsgID := strings.TrimSpace(hub.MsgID)
	if sourceID == "" || convID == "" || ackMsgID == "" {
		return 0, nil
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(hub.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return 0, nil
	}
	ref := ConversationDispositionRef{
		Platform: hub.Platform, ChannelAccountID: uint(accountID),
		ConversationID: convID, MessageID: sourceID,
	}
	if err := validateConversationDispositionRef(ref); err != nil {
		return 0, nil
	}

	current, err := a.dispositions.Get(ctx, tid, ref.Platform, ref.ChannelAccountID, ref.ConversationID, ref.MessageID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, nil // 未知 ACK：台账里没有对应入站消息，no-op
	}
	if current.TenantID != 0 && current.TenantID != tid {
		return 0, nil // 跨租户 fail-closed（仓储隔离之外的二次防线）
	}
	if current.Status != model.ConversationDispositionProcessing {
		return 0, nil // pending 未投递、replied 重复 ACK、skipped/merged 都不动
	}

	changed, err := applyDispositionTransition(current, DispositionTransition{
		Status:         model.ConversationDispositionReplied,
		ReplyMessageID: ackMsgID,
		DeliveredAt:    &deliveredAt,
		Reason:         "outbox_ack_delivered",
	})
	if err != nil || !changed {
		return 0, err
	}
	if err := a.dispositions.Save(ctx, current); err != nil {
		if errors.Is(err, ErrConversationDispositionVersionConflict) {
			return 0, nil // 并发重复 ACK：CAS 失败即 no-op
		}
		return 0, err
	}
	return 1, nil
}
