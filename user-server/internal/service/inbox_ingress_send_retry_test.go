package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestRecheck_PendingSendRetrySuppressesRegeneration 投递重试排队中不得再另生成一份回复。
//
// 场景（2026-09-19 断网实测的同一条链路）：AI 回复投递失败 → 一条 send_retry 记录进入
// 延迟出站队列等待重投；此时会话最后一条仍是客户入站行，recheck 会判定"未回复"再
// 生成一份新回复。两条路都走得通的话，网络恢复后客户会收到"旧回复 + 新回复"两条。
// 排队中的那行就是这条回复本身，recheck 必须让位。
func TestRecheck_PendingSendRetrySuppressesRegeneration(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.DelayedOutboundReply{})
	now := time.Now()
	if err := db.Create(&model.MessageHub{
		MsgID: "in-retry", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "帮我查下订单", ConversationID: "conv-retry-lane", SentAt: now.Add(-2 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	rec := &model.DelayedOutboundReply{
		Platform: "telegram", AccountID: "5", ConversationID: "conv-retry-lane",
		SenderID: "cust1", Content: "已生成但没投出去的回复",
		SendAt: now.Add(60 * time.Second), Status: model.DelayedStatusPending,
		Kind: model.DelayedKindSendRetry,
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("seed retry row: %v", err)
	}

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-retry-lane", "")
	if tr.called != 0 {
		t.Fatalf("重试队列里已有这条回复，recheck 不得再触发一次 AI，实际调用 %d 次", tr.called)
	}

	// 反向对照：终态（superseded/failed）或静默丢弃的行不得长期压制补触发，
	// 否则这条客户消息从此再也没人回。
	if err := db.Model(rec).Update("status", model.DelayedStatusSuperseded).Error; err != nil {
		t.Fatalf("force superseded: %v", err)
	}
	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-retry-lane", "")
	if tr.called != 1 {
		t.Fatalf("重试队列已终结时应恢复补触发，实际调用 %d 次", tr.called)
	}
}
