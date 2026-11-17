package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// TestInboxConversation_SnoozeSetsUntil snooze 是条件迁移：只有可处理状态才能进入，
// 且到期时间必须落库（否则永远不会被 worker 收回）。
func TestInboxConversation_SnoozeSetsUntil(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	conv := &model.InboxConversation{
		TenantID: 1, Platform: "wechat", AccountID: "acc-sn", CustomerID: "c-sn",
		ConversationID: "conv-sn", Status: model.InboxStatusOpen,
	}
	if err := db.Create(conv).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	until := time.Now().Add(time.Hour).Truncate(time.Second)
	ok, err := r.SnoozeInboxConversation(ctx, conv.ID, until)
	if err != nil || !ok {
		t.Fatalf("snooze: ok=%v err=%v", ok, err)
	}

	var got model.InboxConversation
	if err := db.First(&got, conv.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Status != model.InboxStatusSnoozed {
		t.Fatalf("status 应为 snoozed，实际 %q", got.Status)
	}
	if got.SnoozedUntil == nil || !got.SnoozedUntil.Equal(until) {
		t.Fatalf("snoozed_until 应为 %v，实际 %v", until, got.SnoozedUntil)
	}

	// 已 snoozed 的再 snooze 不生效（重复点击不应无限延后提醒）。
	ok, err = r.SnoozeInboxConversation(ctx, conv.ID, until.Add(time.Hour))
	if err != nil {
		t.Fatalf("second snooze: %v", err)
	}
	if ok {
		t.Fatal("已 snoozed 的会话不应被再次 snooze")
	}
}

// TestInboxQuery_SnoozedNotInUnreadCount snoozed 不是未读：客户并没有新消息。
func TestInboxQuery_SnoozedNotInUnreadCount(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	rows := []*model.InboxConversation{
		{TenantID: 1, Platform: "wechat", AccountID: "a", CustomerID: "c1", Status: model.InboxStatusUnread, UnreadCount: 2},
		{TenantID: 1, Platform: "wechat", AccountID: "a", CustomerID: "c2", Status: model.InboxStatusSnoozed, SnoozedUntil: ptrTime(time.Now().Add(time.Hour))},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	stats, err := r.GetStats(ctx,
		[]string{"unread", "open", "assigned", "closed"},
		[]string{"assigned"}, "customer", time.Now())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Unread != 1 {
		t.Fatalf("未读统计只应包含 unread 行，实际 %d", stats.Unread)
	}

	list, total, err := r.ListByQuery(ctx, InboxConversationQuery{Status: model.InboxStatusSnoozed})
	if err != nil {
		t.Fatalf("list snoozed: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("snoozed 筛选应命中 1 行，实际 total=%d len=%d", total, len(list))
	}
}

// TestSnoozeExpiry_ReopensOnce 到期重置是条件更新：并发两次只重置一次。
func TestSnoozeExpiry_ReopensOnce(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	past := time.Now().Add(-time.Minute)
	conv := &model.InboxConversation{
		TenantID: 1, Platform: "wechat", AccountID: "a-exp", CustomerID: "c-exp",
		Status: model.InboxStatusSnoozed, SnoozedUntil: &past,
	}
	if err := db.Create(conv).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	type res struct {
		n   int64
		err error
	}
	out := make(chan res, 2)
	for i := 0; i < 2; i++ {
		go func() {
			n, err := r.ReopenDueSnoozed(ctx, time.Now())
			out <- res{n, err}
		}()
	}
	var total int64
	for i := 0; i < 2; i++ {
		got := <-out
		if got.err != nil {
			t.Fatalf("reopen: %v", got.err)
		}
		total += got.n
	}
	if total != 1 {
		t.Fatalf("并发两次应合计只重置 1 行，实际 %d", total)
	}

	var after model.InboxConversation
	if err := db.First(&after, conv.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if after.Status != model.InboxStatusOpen || after.SnoozedUntil != nil {
		t.Fatalf("到期后应为 open 且清空 snoozed_until，实际 status=%q until=%v", after.Status, after.SnoozedUntil)
	}
}

// TestInboxConversation_ReopenIncrementsCount closed → 新入站重开时计数 +1。
func TestInboxConversation_ReopenIncrementsCount(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	upsert := func(from string, at time.Time) {
		t.Helper()
		if err := r.UpsertFromMessage(ctx, UpsertFromMessageInput{
			Platform: "wechat", AccountID: "acc-reopen", CustomerID: "c-reopen",
			ConversationID: "conv-reopen", LastMessagePreview: from,
			LastMessageAt: at, LastMessageFrom: from,
		}); err != nil {
			t.Fatalf("upsert(%s): %v", from, err)
		}
	}
	now := time.Now().Truncate(time.Millisecond)
	upsert("customer", now)
	upsert("agent", now.Add(time.Minute))

	// 人工关闭（模拟工作台操作）。
	if err := db.Model(&model.InboxConversation{}).
		Where("platform = ? AND customer_id = ?", "wechat", "c-reopen").
		Updates(map[string]any{"status": model.InboxStatusClosed, "closed_at": now.Add(2 * time.Minute)}).Error; err != nil {
		t.Fatalf("close: %v", err)
	}

	upsert("customer", now.Add(3*time.Minute))

	var conv model.InboxConversation
	if err := db.Where("platform = ? AND customer_id = ?", "wechat", "c-reopen").First(&conv).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if conv.Status != model.InboxStatusUnread {
		t.Fatalf("重开后应为 unread，实际 %q", conv.Status)
	}
	if conv.ReopenCount != 1 {
		t.Fatalf("重开计数应为 1，实际 %d", conv.ReopenCount)
	}
	if conv.ClosedAt != nil {
		t.Fatal("重开应清空 closed_at")
	}
}

// TestInboxQuery_AwaitingFirstReply 已首响/已关闭/已 snoozed 都不算「客户在等」，并按等待时长升序。
func TestInboxQuery_AwaitingFirstReply(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)

	mk := func(customer string, waiting *time.Time, firstReply, closedAt *time.Time, status string) {
		t.Helper()
		row := &model.InboxConversation{
			TenantID: 1, Platform: "wechat", AccountID: "acc-wait", CustomerID: customer,
			Status: status, WaitingSince: waiting, FirstReplyAt: firstReply, ClosedAt: closedAt,
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", customer, err)
		}
	}
	t1 := base
	t2 := base.Add(30 * time.Minute)
	first := base.Add(10 * time.Minute)
	mk("waiting-old", &t1, nil, nil, model.InboxStatusOpen)
	mk("waiting-new", &t2, nil, nil, model.InboxStatusUnread)
	mk("replied", &t1, &first, nil, model.InboxStatusOpen)
	mk("closed", &t1, nil, &first, model.InboxStatusClosed)
	mk("snoozed", &t1, nil, nil, model.InboxStatusSnoozed)

	list, total, err := r.ListByQuery(ctx, InboxConversationQuery{Filter: "awaiting_first_reply"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("应命中 2 条待首响，实际 total=%d len=%d", total, len(list))
	}
	if list[0].CustomerID != "waiting-old" {
		t.Fatalf("应按等待时长升序（等最久的在前），实际首条 %q", list[0].CustomerID)
	}
	if list[1].CustomerID != "waiting-new" {
		t.Fatalf("第二条应为 waiting-new，实际 %q", list[1].CustomerID)
	}
}

// TestFirstReplyMetrics_ExcludesUnanswered 未回复样本不得计入分母，脏数据（负时长）也排除。
func TestFirstReplyMetrics_ExcludesUnanswered(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	base := time.Now().Add(-2 * time.Hour)

	mk := func(customer string, waiting, firstReply *time.Time) {
		t.Helper()
		row := &model.InboxConversation{
			TenantID: 1, Platform: "feishu", AccountID: "acc-metrics", CustomerID: customer,
			Status: model.InboxStatusOpen, WaitingSince: waiting, FirstReplyAt: firstReply,
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", customer, err)
		}
	}
	// 60s / 120s / 180s 三个有效样本 + 一个未回复 + 一个负时长脏数据
	mk("ok-60", timePtr(base), timePtr(base.Add(60*time.Second)))
	mk("ok-120", timePtr(base), timePtr(base.Add(120*time.Second)))
	mk("ok-180", timePtr(base), timePtr(base.Add(180*time.Second)))
	mk("never-replied", timePtr(base), nil)
	mk("dirty-negative", timePtr(base), timePtr(base.Add(-30*time.Second)))

	m, err := r.FirstReplyMetrics(ctx, "feishu", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if m.SampleCount != 3 {
		t.Fatalf("只应统计 3 个有效样本，实际 %d", m.SampleCount)
	}
	if m.P50Seconds != 120 {
		t.Fatalf("p50 应为 120s，实际 %v", m.P50Seconds)
	}
	if m.P95Seconds < 170 || m.P95Seconds > 190 {
		t.Fatalf("p95 应接近 180s，实际 %v", m.P95Seconds)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
