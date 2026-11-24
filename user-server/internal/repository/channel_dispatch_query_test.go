package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// AUDIT-01 回归：渠道分发选号依赖的两个窄查询与 Go 原生内存过滤语义一致。
// 反例矩阵：租户隔离 / 未命中 / 版本门 / 会话键与客户键分支 / 最新会话排序。
func TestChannelRepository_GetByTenantAndCode(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Channel{})
	repo := NewChannelRepositoryWithDB(db)
	ctx := context.Background()

	if err := repo.Create(ctx, &model.Channel{TenantID: 1, Name: "wx", Code: "dispatch-wx", Type: "dm", Enabled: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetByTenantAndCode(ctx, 1, "dispatch-wx")
	if err != nil || got == nil || got.Code != "dispatch-wx" {
		t.Fatalf("命中失败: got=%+v err=%v", got, err)
	}
	// 反例：错租户不得命中（GetByCode 跨租户，本方法必须租户隔离）。
	got, err = repo.GetByTenantAndCode(ctx, 2, "dispatch-wx")
	if err != nil || got != nil {
		t.Fatalf("跨租户泄漏: got=%+v err=%v", got, err)
	}
	// 反例：不存在的 code 返回 (nil,nil)，不报错。
	got, err = repo.GetByTenantAndCode(ctx, 1, "not-exist")
	if err != nil || got != nil {
		t.Fatalf("未命中应 (nil,nil): got=%+v err=%v", got, err)
	}
}

func TestInboxConversationRepository_FindLatestByPlatformAndPeer(t *testing.T) {
	db := testutil.NewTestDB(t, &model.InboxConversation{})
	repo := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	// 同平台同会话两条，按 id DESC 应取最新；不同平台/租户不得混入。
	rows := []model.InboxConversation{
		{TenantID: 1, Platform: "wx", AccountID: "1", CustomerID: "c1", ConversationID: "conv-1", Status: "open"},
		{TenantID: 1, Platform: "wx", AccountID: "2", CustomerID: "c1", ConversationID: "conv-1", Status: "open"},
		{TenantID: 2, Platform: "wx", AccountID: "9", CustomerID: "c1", ConversationID: "conv-1", Status: "open"},
		{TenantID: 1, Platform: "tg", AccountID: "3", CustomerID: "c1", ConversationID: "conv-1", Status: "open"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	got, err := repo.FindLatestByPlatformAndPeer(ctx, 1, "wx", "conv-1", "conv-1")
	if err != nil || got == nil || got.AccountID != "2" {
		t.Fatalf("会话键最新命中失败: got=%+v err=%v", got, err)
	}
	// 客户键分支：无 conversation_id 时按 customer_id 查，不得取到别租户/平台。
	got, err = repo.FindLatestByPlatformAndPeer(ctx, 1, "wx", "", "c1")
	if err != nil || got == nil || got.TenantID != 1 || got.Platform != "wx" {
		t.Fatalf("客户键命中失败: got=%+v err=%v", got, err)
	}
	// 反例：未命中返回 gorm.ErrRecordNotFound（由调用方 swallow），不返回别的会话。
	got, err = repo.FindLatestByPlatformAndPeer(ctx, 1, "wx", "no-such", "no-such")
	if err == nil || got != nil {
		t.Fatalf("未命中应报错+nil: got=%+v err=%v", got, err)
	}
}
