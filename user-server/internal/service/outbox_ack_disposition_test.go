package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// 真实 outbox ACK 才能把逐入站消息处置从 processing 推进到 replied。
// 这些用例先于实现写下（strict TDD）：Ack 反查 → 源消息定位 → CAS 迁移。

func ackDispositionTestContext() context.Context {
	return tenant.WithTenantID(context.Background(), 7)
}

func ackDispKey(tenantID, accountID uint, platform, conversationID, messageID string) string {
	return fmt.Sprintf("%d|%d|%s|%s|%s", tenantID, accountID, platform, conversationID, messageID)
}

func ackDispositionTestHub(status string) model.MessageHub {
	return model.MessageHub{
		TenantID:       7,
		Platform:       "douyin",
		MsgID:          "douyin-out-ack-1",
		Direction:      "outbound",
		Status:         status,
		AccountID:      "42",
		ConversationID: "conv-1",
		Extra: model.JSONMap{
			"source_message_id": "hub:101",
			"correlation_id":    "corr-1",
			"origin":            OutboundOriginAI,
		},
	}
}

func ackDispositionTestProcessing() *model.ConversationMessageDisposition {
	return &model.ConversationMessageDisposition{
		TenantID: 7, Platform: "douyin", ChannelAccountID: 42,
		ConversationID: "conv-1", MessageID: "hub:101", MessageCursor: "hub:101",
		BusLLMSessionIDBlind: "blind-1", Status: model.ConversationDispositionProcessing,
		Version: 1, Reason: "delivery_accepted",
	}
}

// ackDispositionStoreFake 模拟生产仓储的租户隔离与 version CAS；
// Save 递增版本并计数，用来证明重复 ACK 一次都没写库。
type ackDispositionStoreFake struct {
	items map[string]*model.ConversationMessageDisposition
	saves int
}

func (s *ackDispositionStoreFake) Get(_ context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error) {
	item := s.items[ackDispKey(tenantID, accountID, platform, conversationID, messageID)]
	if item == nil {
		return nil, nil
	}
	copy := *item
	if item.DeliveredAt != nil {
		at := *item.DeliveredAt
		copy.DeliveredAt = &at
	}
	return &copy, nil
}

func (s *ackDispositionStoreFake) Save(_ context.Context, item *model.ConversationMessageDisposition) error {
	if item == nil {
		return errors.New("nil disposition")
	}
	key := ackDispKey(item.TenantID, item.ChannelAccountID, item.Platform, item.ConversationID, item.MessageID)
	if existing := s.items[key]; existing != nil && item.Version != existing.Version {
		return ErrConversationDispositionVersionConflict
	}
	item.Version++
	copy := *item
	s.items[key] = &copy
	s.saves++
	return nil
}

func (s *ackDispositionStoreFake) List(_ context.Context, _ uint, _ string, _ uint, _ string) ([]model.ConversationMessageDisposition, error) {
	return nil, nil
}

type ackHubLookupFake struct {
	rows []model.MessageHub
}

func (f *ackHubLookupFake) GetByMsgIDsInScopeWithConv(_ context.Context, platform, accountID, conversationID string, msgIDs []string) ([]model.MessageHub, error) {
	wanted := make(map[string]struct{}, len(msgIDs))
	for _, id := range msgIDs {
		wanted[id] = struct{}{}
	}
	rows := make([]model.MessageHub, 0)
	for _, row := range f.rows {
		if _, ok := wanted[row.MsgID]; !ok {
			continue
		}
		if row.Platform != platform || row.AccountID != accountID {
			continue
		}
		if conversationID != "" && row.ConversationID != conversationID {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func ackDispositionTestApplier(store *ackDispositionStoreFake, rows []model.MessageHub, now time.Time) *outboxAckDispositionApplier {
	return &outboxAckDispositionApplier{
		hubs:         &ackHubLookupFake{rows: rows},
		dispositions: store,
		now:          func() time.Time { return now },
	}
}

func ackDispositionTestAck(hub model.MessageHub) []OutboxAckDispositionInput {
	return []OutboxAckDispositionInput{{
		MsgID: hub.MsgID, ConversationID: hub.ConversationID, Status: model.BridgeAckStatusDelivered,
	}}
}

func TestOutboxAckDispositionMovesProcessingToReplied(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	store := &ackDispositionStoreFake{items: map[string]*model.ConversationMessageDisposition{}}
	store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")] = ackDispositionTestProcessing()
	hub := ackDispositionTestHub(model.BridgeAckStatusDelivered)
	applier := ackDispositionTestApplier(store, []model.MessageHub{hub}, now)

	applied, err := applier.Apply(ackDispositionTestContext(), "douyin", "42", ackDispositionTestAck(hub))
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if applied != 1 {
		t.Fatalf("真实 delivered ACK 应推进一条处置，实际 %d", applied)
	}
	got := store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")]
	if got == nil {
		t.Fatal("处置记录丢失")
	}
	if got.Status != model.ConversationDispositionReplied {
		t.Fatalf("状态应为 replied，实际 %q", got.Status)
	}
	if got.ReplyMessageID != hub.MsgID {
		t.Fatalf("必须落真实出站消息 ID，实际 %q", got.ReplyMessageID)
	}
	if got.DeliveredAt == nil || !got.DeliveredAt.Equal(now) {
		t.Fatalf("必须落真实送达时间，实际 %+v", got.DeliveredAt)
	}
	if got.Version != 2 {
		t.Fatalf("CAS 版本应递增到 2，实际 %d", got.Version)
	}
	if store.saves != 1 {
		t.Fatalf("应恰好写库一次，实际 %d", store.saves)
	}
	if wm := ComputeConversationReplyWatermark([]model.ConversationMessageDisposition{*got}); wm != "hub:101" {
		t.Fatalf("replied 才推进回复水位，实际 %q", wm)
	}
}

func TestOutboxAckDispositionDuplicateAckIsNoOp(t *testing.T) {
	first := time.Unix(1700000000, 0).UTC()
	later := first.Add(time.Hour)
	store := &ackDispositionStoreFake{items: map[string]*model.ConversationMessageDisposition{}}
	key := ackDispKey(7, 42, "douyin", "conv-1", "hub:101")
	store.items[key] = ackDispositionTestProcessing()
	hub := ackDispositionTestHub(model.BridgeAckStatusDelivered)
	applier := ackDispositionTestApplier(store, []model.MessageHub{hub}, first)
	in := ackDispositionTestAck(hub)

	if n, err := applier.Apply(ackDispositionTestContext(), "douyin", "42", in); err != nil || n != 1 {
		t.Fatalf("首次 ACK: n=%d err=%v", n, err)
	}
	deliveredAt := *store.items[key].DeliveredAt

	applier.now = func() time.Time { return later }
	n, err := applier.Apply(ackDispositionTestContext(), "douyin", "42", in)
	if err != nil || n != 0 {
		t.Fatalf("重复 ACK 必须幂等 no-op: n=%d err=%v", n, err)
	}
	got := store.items[key]
	if got.Status != model.ConversationDispositionReplied || got.Version != 2 {
		t.Fatalf("重复 ACK 不得再次写库: %+v", got)
	}
	if !got.DeliveredAt.Equal(deliveredAt) {
		t.Fatalf("重复 ACK 不得改写送达时间: %v → %v", deliveredAt, got.DeliveredAt)
	}
	if store.saves != 1 {
		t.Fatalf("重复 ACK 不得产生第二次写库，实际 %d", store.saves)
	}
}

func TestOutboxAckDispositionWrongScopeRejected(t *testing.T) {
	hub := ackDispositionTestHub(model.BridgeAckStatusDelivered)
	wrongConv := func() model.MessageHub { h := hub; h.ConversationID = "conv-other"; return h }()
	inbound := func() model.MessageHub { h := hub; h.Direction = "inbound"; return h }()
	noSource := func() model.MessageHub { h := hub; h.Extra = model.JSONMap{}; return h }()
	cases := []struct {
		name     string
		platform string
		account  string
		inputs   []OutboxAckDispositionInput
		hubRows  []model.MessageHub
		tenantID uint
	}{
		{
			name: "wrong account", platform: "douyin", account: "99",
			inputs: ackDispositionTestAck(hub), hubRows: []model.MessageHub{hub}, tenantID: 7,
		},
		{
			name: "wrong platform", platform: "telegram", account: "42",
			inputs: ackDispositionTestAck(hub), hubRows: []model.MessageHub{hub}, tenantID: 7,
		},
		{
			name: "wrong conversation", platform: "douyin", account: "42",
			inputs: ackDispositionTestAck(wrongConv), hubRows: []model.MessageHub{hub}, tenantID: 7,
		},
		{
			name: "cross tenant", platform: "douyin", account: "42",
			inputs: ackDispositionTestAck(hub), hubRows: []model.MessageHub{hub}, tenantID: 8,
		},
		{
			name: "not outbound", platform: "douyin", account: "42",
			inputs: ackDispositionTestAck(hub), hubRows: []model.MessageHub{inbound}, tenantID: 7,
		},
		{
			name: "missing source message id", platform: "douyin", account: "42",
			inputs: ackDispositionTestAck(hub), hubRows: []model.MessageHub{noSource}, tenantID: 7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &ackDispositionStoreFake{items: map[string]*model.ConversationMessageDisposition{}}
			store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")] = ackDispositionTestProcessing()
			applier := ackDispositionTestApplier(store, tc.hubRows, time.Unix(1700000000, 0).UTC())
			ctx := tenant.WithTenantID(context.Background(), tc.tenantID)
			n, err := applier.Apply(ctx, tc.platform, tc.account, tc.inputs)
			if err != nil || n != 0 {
				t.Fatalf("错误作用域必须被拒绝: n=%d err=%v", n, err)
			}
			if got := store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")]; got == nil || got.Status != model.ConversationDispositionProcessing {
				t.Fatalf("错误作用域不得推进处置: %+v", got)
			}
			if store.saves != 0 {
				t.Fatalf("错误作用域不得写库，实际 %d", store.saves)
			}
		})
	}
}

func TestOutboxAckDispositionOnlyDeliveredAdvances(t *testing.T) {
	hub := ackDispositionTestHub(model.BridgeAckStatusDelivered)
	for _, status := range []string{model.BridgeAckStatusFailed, model.BridgeAckStatusUnknown, model.BridgeAckStatusDuplicate} {
		t.Run(status, func(t *testing.T) {
			store := &ackDispositionStoreFake{items: map[string]*model.ConversationMessageDisposition{}}
			store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")] = ackDispositionTestProcessing()
			applier := ackDispositionTestApplier(store, []model.MessageHub{hub}, time.Unix(1700000000, 0).UTC())
			n, err := applier.Apply(ackDispositionTestContext(), "douyin", "42", []OutboxAckDispositionInput{{
				MsgID: hub.MsgID, ConversationID: hub.ConversationID, Status: status,
			}})
			if err != nil || n != 0 {
				t.Fatalf("非 delivered ACK 不得推进: n=%d err=%v", n, err)
			}
			if got := store.items[ackDispKey(7, 42, "douyin", "conv-1", "hub:101")]; got == nil || got.Status != model.ConversationDispositionProcessing {
				t.Fatalf("非 delivered ACK 后状态应保持 processing: %+v", got)
			}
			if store.saves != 0 {
				t.Fatalf("非 delivered ACK 不得写库，实际 %d", store.saves)
			}
		})
	}
}

func TestOutboxAckDispositionUnknownInboundNoOp(t *testing.T) {
	hub := ackDispositionTestHub(model.BridgeAckStatusDelivered)
	store := &ackDispositionStoreFake{items: map[string]*model.ConversationMessageDisposition{}}
	applier := ackDispositionTestApplier(store, []model.MessageHub{hub}, time.Unix(1700000000, 0).UTC())
	n, err := applier.Apply(ackDispositionTestContext(), "douyin", "42", ackDispositionTestAck(hub))
	if err != nil || n != 0 {
		t.Fatalf("台账缺失（未知 ACK）必须 no-op: n=%d err=%v", n, err)
	}
	if store.saves != 0 {
		t.Fatalf("未知 ACK 不得写库，实际 %d", store.saves)
	}
}
