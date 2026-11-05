package repository

// web_embed 会话定位读侧与失败轨迹写回（2026-11-05 五层审计下沉后的回归）。
// 原查询/写回在 service 侧用 dbUtil.GetDB() 直写；下沉到仓储后行为口径必须保持不变。

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupWebEmbedRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.CustomerSession{}, &model.SessionMessage{}, &model.MessageHub{})
	for _, table := range []string{"session_messages", "customer_sessions", "message_hub"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("清空 %s: %v", table, err)
		}
	}
	return db
}

func TestFindDeliverableWebEmbedSession_ExactThenFallback(t *testing.T) {
	db := setupWebEmbedRepoTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	now := time.Now()

	older := &model.CustomerSession{
		SessionID: "sess-old", Platform: model.PlatformWebEmbed, AccountID: "acc-1",
		Status: model.SessionStatusWaiting, LastMessageAt: &now,
	}
	if err := db.Create(older).Error; err != nil {
		t.Fatalf("seed older: %v", err)
	}
	newerAt := now.Add(time.Minute)
	newer := &model.CustomerSession{
		SessionID: "sess-new", Platform: model.PlatformWebEmbed, AccountID: "acc-1",
		Status: model.SessionStatusHumanHandling, LastMessageAt: &newerAt,
	}
	if err := db.Create(newer).Error; err != nil {
		t.Fatalf("seed newer: %v", err)
	}

	// 1) 精确命中：按 chat session_id 取，不受排序影响
	got, err := repo.FindDeliverableWebEmbedSession(ctx, "sess-old", "acc-1")
	if err != nil || got == nil || got.SessionID != "sess-old" {
		t.Fatalf("精确命中失败: got=%+v err=%v", got, err)
	}

	// 2) 回退：会话键缺失时取该账号最近一条未结束会话
	got, err = repo.FindDeliverableWebEmbedSession(ctx, "missing", "acc-1")
	if err != nil || got == nil || got.SessionID != "sess-new" {
		t.Fatalf("回退取最近会话失败: got=%+v err=%v", got, err)
	}

	// 3) 已结束会话不参与回退
	if err := db.Model(&model.CustomerSession{}).
		Where("session_id IN ?", []string{"sess-old", "sess-new"}).
		Update("status", model.SessionStatusResolved).Error; err != nil {
		t.Fatalf("置为已结束: %v", err)
	}
	if _, err := repo.FindDeliverableWebEmbedSession(ctx, "missing", "acc-1"); err == nil {
		t.Fatal("全部会话已结束时不应命中")
	}
}

func TestUpdateHubExtra_PreservesStatus(t *testing.T) {
	db := setupWebEmbedRepoTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	hub := &model.MessageHub{
		Platform: "web_embed", AccountID: "acc-1", MsgID: "msg-1",
		Direction: "outbound", Status: "pending", Content: "hi",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed hub: %v", err)
	}

	if err := repo.UpdateHubExtra(ctx, hub.ID, model.JSONMap{"web_embed_error": "boom"}); err != nil {
		t.Fatalf("UpdateHubExtra: %v", err)
	}

	var got model.MessageHub
	if err := db.First(&got, hub.ID).Error; err != nil {
		t.Fatalf("回读 hub: %v", err)
	}
	if got.Status != "pending" {
		t.Fatalf("失败轨迹写回不得改 status，得到 %q", got.Status)
	}
	if got.Extra["web_embed_error"] != "boom" {
		t.Fatalf("extra 未写回: %+v", got.Extra)
	}
}
