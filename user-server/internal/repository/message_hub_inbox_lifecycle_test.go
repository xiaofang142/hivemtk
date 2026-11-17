package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 会话生命周期（waiting_since / first_reply_at / last_activity_at）的状态机，
// 对齐 Chatwoot 在 message 回调里维护 conversation 的口径：
//   - 客户消息：等待起点「从无到有」写入，连发不刷新；
//   - 我方消息：等待清空，首响只写一次；
//   - 任意消息：last_activity_at 前进。
func TestInboxConversation_UpsertFromMessage_Lifecycle(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	t0 := time.Now().Add(-10 * time.Minute).Truncate(time.Millisecond)

	upsert := func(from string, at time.Time) {
		t.Helper()
		if err := r.UpsertFromMessage(ctx, UpsertFromMessageInput{
			Platform:           "wechat",
			AccountID:          "acc_life",
			CustomerID:         "c_life",
			ConversationID:     "conv:c_life",
			LastMessagePreview: from + " 消息",
			LastMessageAt:      at,
			LastMessageFrom:    from,
		}); err != nil {
			t.Fatalf("upsert(%s): %v", from, err)
		}
	}
	load := func() *model.InboxConversation {
		t.Helper()
		conv, err := r.FindByPlatformAccountCustomer(ctx, "wechat", "acc_life", "c_life")
		if err != nil || conv == nil {
			t.Fatalf("load: %v", err)
		}
		return conv
	}

	// ① 首条客户消息：开始等待，尚无首响，活动时间=本条。
	upsert("customer", t0)
	conv := load()
	if conv.WaitingSince == nil || !conv.WaitingSince.Equal(t0) {
		t.Fatalf("首条客户消息应写入 waiting_since=%v, 得 %v", t0, conv.WaitingSince)
	}
	if conv.FirstReplyAt != nil {
		t.Fatalf("尚未回复不应有 first_reply_at, 得 %v", conv.FirstReplyAt)
	}
	if conv.LastActivityAt == nil || !conv.LastActivityAt.Equal(t0) {
		t.Fatalf("last_activity_at 应为 %v, 得 %v", t0, conv.LastActivityAt)
	}

	// ② 客户连发：等待起点不能被刷新（SLA 看最早那条）。
	t1 := t0.Add(2 * time.Minute)
	upsert("customer", t1)
	conv = load()
	if !conv.WaitingSince.Equal(t0) {
		t.Fatalf("客户连发不应刷新 waiting_since：期望 %v, 得 %v", t0, conv.WaitingSince)
	}
	if !conv.LastActivityAt.Equal(t1) {
		t.Fatalf("last_activity_at 应前进到 %v, 得 %v", t1, conv.LastActivityAt)
	}

	// ③ 首条我方消息：等待收口，首响落 t2。
	t2 := t0.Add(5 * time.Minute)
	upsert("agent", t2)
	conv = load()
	if conv.WaitingSince != nil {
		t.Fatalf("我方消息后 waiting_since 应清空, 得 %v", conv.WaitingSince)
	}
	if conv.FirstReplyAt == nil || !conv.FirstReplyAt.Equal(t2) {
		t.Fatalf("首响应为 %v, 得 %v", t2, conv.FirstReplyAt)
	}

	// ④ 再一条我方消息：first_reply_at 只写一次。
	upsert("agent", t0.Add(6*time.Minute))
	conv = load()
	if !conv.FirstReplyAt.Equal(t2) {
		t.Fatalf("first_reply_at 不应被覆盖：期望 %v, 得 %v", t2, conv.FirstReplyAt)
	}

	// ⑤ 客户再次来信：重新开始等待（新一轮 SLA）。
	t3 := t0.Add(20 * time.Minute)
	upsert("customer", t3)
	conv = load()
	if conv.WaitingSince == nil || !conv.WaitingSince.Equal(t3) {
		t.Fatalf("客户再次来信应重新写入 waiting_since=%v, 得 %v", t3, conv.WaitingSince)
	}
	if !conv.FirstReplyAt.Equal(t2) {
		t.Fatalf("first_reply_at 仍是 %v, 得 %v", t2, conv.FirstReplyAt)
	}
}

// 由我方消息开场的会话（主动触达/补录）：首响即本条，且不进入等待态。
func TestInboxConversation_UpsertFromMessage_AgentFirst(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	at := time.Now().Truncate(time.Millisecond)

	if err := r.UpsertFromMessage(ctx, UpsertFromMessageInput{
		Platform: "telegram", AccountID: "acc_af", CustomerID: "c_af",
		ConversationID: "conv:c_af", LastMessagePreview: "您好",
		LastMessageAt: at, LastMessageFrom: "agent",
	}); err != nil {
		t.Fatal(err)
	}
	conv, err := r.FindByPlatformAccountCustomer(ctx, "telegram", "acc_af", "c_af")
	if err != nil || conv == nil {
		t.Fatal(err)
	}
	if conv.WaitingSince != nil {
		t.Fatalf("我方开场不应处于等待态, 得 %v", conv.WaitingSince)
	}
	if conv.FirstReplyAt == nil || !conv.FirstReplyAt.Equal(at) {
		t.Fatalf("我方开场首响应为本条 %v, 得 %v", at, conv.FirstReplyAt)
	}
	if conv.Status != "open" {
		t.Fatalf("我方开场状态应为 open, 得 %s", conv.Status)
	}
}

// 旧路径 UpdateLastMessage 也必须推进活动时间（它拿不到方向，只做活动维度）。
func TestInboxConversation_UpdateLastMessage_AdvancesActivity(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	t0 := time.Now().Add(-time.Hour)

	conv := newConv("wechat", "acc_ul", "c_ul", "open", t0)
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}
	at := t0.Add(30 * time.Minute)
	if err := r.UpdateLastMessage(ctx, conv.ID, "补录消息", at, 1); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastActivityAt == nil || !got.LastActivityAt.Equal(at) {
		t.Fatalf("last_activity_at 应为 %v, 得 %v", at, got.LastActivityAt)
	}
}
