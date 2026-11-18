package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestCardSend_RegistersInteractionContext 发送成功必须登记 pending 行，
// 且带 platform_msg_id / 动作集合 / 到期时间（否则点击永远定位不到卡片）。
func TestCardSend_RegistersInteractionContext(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ChannelInteraction{})
	repo := repository.NewChannelInteractionRepository(db)
	s := &WebhookService{cardInteractionRepo: repo}
	ctx := context.Background()

	card := model.RichCard{
		Title: "订单确认",
		Buttons: []model.CardButton{
			{Text: "确认", Action: "order.confirm"},
			{Text: "取消", Action: "order.cancel"},
			{Text: "详情", URL: "https://example.com/o/1"},
		},
	}
	id := planCardInteraction(card)
	if id == "" {
		t.Fatal("有动作按钮的卡片必须分配交互 id")
	}
	if len(id) > 64 {
		t.Fatalf("交互 id 必须能塞进 Telegram callback_data（64 字节），实际 %d", len(id))
	}
	hub := &model.MessageHub{TenantID: 3, ConversationID: "chat-1"}
	s.registerCardInteraction(ctx, ChannelTelegram, "acct-card", "tg-msg-77", id, card, hub)

	var row model.ChannelInteraction
	if err := db.Where("interaction_id=?", id).First(&row).Error; err != nil {
		t.Fatalf("交互行未落库: %v", err)
	}
	if row.Status != model.ChannelInteractionPending {
		t.Fatalf("状态应为 pending，实际 %q", row.Status)
	}
	if row.PlatformMsgID != "tg-msg-77" || row.AccountID != "acct-card" || row.Channel != string(ChannelTelegram) {
		t.Fatalf("定位字段丢失：%+v", row)
	}
	if row.TenantID != 3 || row.ConversationID != "chat-1" {
		t.Fatalf("租户/会话未带入：%+v", row)
	}
	if row.ExpiresAt == nil {
		t.Fatal("必须写到期时间，否则清理 worker 无法收口")
	}
	if ttl := time.Until(*row.ExpiresAt); ttl < 6*24*time.Hour || ttl > 8*24*time.Hour {
		t.Fatalf("默认 TTL 应约 7 天，实际剩余 %v", ttl)
	}
	acts, _ := row.Payload["actions"].([]any)
	if len(acts) != 2 {
		t.Fatalf("动作集合应只含有 Action 的按钮（2 个），实际 %+v", row.Payload["actions"])
	}
}

// TestCardSend_NoActionButtonsSkipsRegistration 纯链接卡片不产生交互行。
func TestCardSend_NoActionButtonsSkipsRegistration(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ChannelInteraction{})
	repo := repository.NewChannelInteractionRepository(db)
	s := &WebhookService{cardInteractionRepo: repo}
	ctx := context.Background()

	card := model.RichCard{Title: "通知", Buttons: []model.CardButton{{Text: "详情", URL: "https://example.com"}}}
	interactionID := planCardInteraction(card)
	if interactionID != "" {
		t.Fatalf("无动作按钮不应分配交互 id，实际 %q", interactionID)
	}
	s.registerCardInteraction(ctx, ChannelTelegram, "acct-noaction", "tg-msg-78", "", card, nil)

	var n int64
	db.Model(&model.ChannelInteraction{}).Where("account_id=?", "acct-noaction").Count(&n)
	if n != 0 {
		t.Fatalf("无动作卡片不应落交互行，实际 %d 行", n)
	}
}

// TestInteractionExpiryWorker_MarksExpired 只收口已过期且仍 pending 的行。
func TestInteractionExpiryWorker_MarksExpired(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ChannelInteraction{})
	repo := repository.NewChannelInteractionRepository(db)
	task := NewChannelInteractionExpiryTask(repo)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	answered := time.Now().Add(-time.Hour)
	rows := []model.ChannelInteraction{
		{Channel: "telegram", InteractionID: "ci-expired", Status: model.ChannelInteractionPending, ExpiresAt: &past},
		{Channel: "telegram", InteractionID: "ci-alive", Status: model.ChannelInteractionPending, ExpiresAt: &future},
		{Channel: "telegram", InteractionID: "ci-done", Status: model.ChannelInteractionAnswered, ExpiresAt: &past, AnsweredAt: &answered},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("预置交互行: %v", err)
		}
	}

	expired, err := task.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if expired != 1 {
		t.Fatalf("只应过期 1 行，实际 %d", expired)
	}
	status := func(id string) string {
		var r model.ChannelInteraction
		if err := db.Where("interaction_id=?", id).First(&r).Error; err != nil {
			t.Fatalf("查 %s: %v", id, err)
		}
		return r.Status
	}
	if status("ci-expired") != model.ChannelInteractionExpired {
		t.Fatal("过期行未置 expired")
	}
	if status("ci-alive") != model.ChannelInteractionPending {
		t.Fatal("未到期行不得被改")
	}
	if status("ci-done") != model.ChannelInteractionAnswered {
		t.Fatal("已答复行不得被改成 expired")
	}
}

// TestInteractionExpiryWorker_StopIsIdempotent 重复 Stop 不 panic、能等到循环退出。
func TestInteractionExpiryWorker_StopIsIdempotent(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ChannelInteraction{})
	task := NewChannelInteractionExpiryTask(repository.NewChannelInteractionRepository(db))
	task.interval = time.Hour
	ctx := context.Background()
	task.Start(ctx)

	done := make(chan struct{})
	go func() {
		task.Stop(ctx)
		task.Stop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 重复调用未返回（可能重复 close 或 wg 死锁）")
	}
}
