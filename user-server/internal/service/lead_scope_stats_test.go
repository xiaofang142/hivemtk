package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupLeadScopeStatsDB(t *testing.T) context.Context {
	t.Helper()
	db := testutil.NewTestDB(t, &model.LeadMiningJudgement{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：线索统计范围不能跳过")
	}
	sys := tenant.WithSystemScope(context.Background())
	rows := []*model.LeadMiningJudgement{
		{TenantID: 1, JudgementID: "j-a1", AccountID: "acct-a", Matched: true, FeedbackStatus: "valid"},
		{TenantID: 1, JudgementID: "j-a2", AccountID: "acct-a", Matched: false},
		{TenantID: 1, JudgementID: "j-b1", AccountID: "acct-b", Matched: true, FeedbackStatus: "false_positive"},
		{TenantID: 1, JudgementID: "j-empty", AccountID: "", Matched: true},
	}
	if err := db.WithContext(sys).Create(&rows).Error; err != nil {
		t.Fatalf("准备判定夹具失败: %v", err)
	}
	return tenant.WithTenantID(context.Background(), 1)
}

// 受限员工统计只能看到授权账号的判定，空账号历史与未授权账号不泄露。
func TestLeadScopeStats_Restricted(t *testing.T) {
	ctx := setupLeadScopeStatsDB(t)
	repo := repository.NewLeadMiningJudgementRepository()
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	stats, err := repo.GetQualityStatsScoped(ctx, scope.AccountIDs, 7)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats["total"] != int64(2) || stats["matched_total"] != int64(1) {
		t.Fatalf("统计应仅含 acct-a 两行/一命中，实际 %+v", stats)
	}
}

// 空授权必须拒绝统计。
func TestLeadScopeStats_EmptyScopeRejected(t *testing.T) {
	ctx := setupLeadScopeStatsDB(t)
	repo := repository.NewLeadMiningJudgementRepository()
	_, err := repo.GetQualityStatsScoped(ctx, []string{}, 7)
	if !errors.Is(err, repository.ErrLeadScopeDenied) {
		t.Fatalf("空授权统计应拒绝，实际 err=%v", err)
	}
}

// 管理员正例：全量统计含所有账号。
func TestLeadScopeStats_Admin(t *testing.T) {
	ctx := setupLeadScopeStatsDB(t)
	repo := repository.NewLeadMiningJudgementRepository()
	stats, err := repo.GetQualityStatsScoped(ctx, nil, 9)
	if err != nil {
		t.Fatalf("管理员统计失败: %v", err)
	}
	if stats["total"] != int64(4) {
		t.Fatalf("管理员应见全部 4 行，实际 %+v", stats)
	}
}
