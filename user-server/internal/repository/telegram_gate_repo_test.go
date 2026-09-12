// R75 架构下沉回归测试：TelegramGroupMemberRepository.ListStalledRestricted
// （原 service/telegram_gate.go RecoverStalled 直连 s.db 查询下沉仓储化）
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestTelegramMember_ListStalledRestricted(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	now := time.Now()
	restrictedSoon := now.Add(1 * time.Minute) // 禁言中+临近到期 → 应命中
	restrictedFar := now.Add(24 * time.Hour)   // 禁言中+到期远 → 不应命中
	pendingSoon := now.Add(1 * time.Minute)    // 待验证（非 restricted）→ 不应命中
	members := []*model.TelegramGroupMember{
		{AccountID: 1, ChatID: "c1", UserID: "u1", FullName: "hit", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedSoon},
		{AccountID: 1, ChatID: "c1", UserID: "u2", FullName: "far", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedFar},
		{AccountID: 1, ChatID: "c1", UserID: "u3", FullName: "pending", JoinStatus: model.TGMemberPending, Authorized: false, ExpiresAt: &pendingSoon},
		{AccountID: 1, ChatID: "c1", UserID: "u4", FullName: "authed", JoinStatus: model.TGMemberRestricted, Authorized: true, ExpiresAt: &restrictedSoon},
		{AccountID: 1, ChatID: "c1", UserID: "u5", FullName: "noexp", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: nil},
		{AccountID: 2, ChatID: "c2", UserID: "u6", FullName: "hit2", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedSoon},
	}
	for _, m := range members {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	got, err := repo.ListStalledRestricted(ctx, now.Add(2*time.Minute), 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应命中 u1/u6 两条, 实际 %d 条: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.UserID] = true
	}
	if !seen["u1"] || !seen["u6"] {
		t.Fatalf("命中集合应={u1,u6}, 实际=%v", seen)
	}

	// limit 生效
	limited, err := repo.ListStalledRestricted(ctx, now.Add(2*time.Minute), 1)
	if err != nil {
		t.Fatalf("limit 查询失败: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit=1 应返回 1 条, 实际 %d", len(limited))
	}

	// before 早于所有 expires_at → 空结果不报错
	empty, err := repo.ListStalledRestricted(ctx, now.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("空结果查询失败: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("应返回空, 实际 %d", len(empty))
	}
}
