package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestLeadReactRepositories_ScopeByTenantPlatformAccountAliasesAndBoundary(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{}, &model.CustomerSession{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}

	rows := []*model.LeadMiningJudgement{
		{TenantID: 7101, JudgementID: "canonical-conv", Platform: "telegram", AccountID: "11", ConversationID: "same-conv", SenderID: "same-user", Summary: "规范账号会话"},
		{TenantID: 7101, JudgementID: "alias-conv", Platform: "telegram", AccountID: "acct-a", ConversationID: "same-conv", SenderID: "same-user", Summary: "别名账号会话"},
		{TenantID: 7101, JudgementID: "other-sender-conv", Platform: "telegram", AccountID: "acct-a", ConversationID: "same-conv", SenderID: "other-user", Summary: "同会话其他发送人"},
		{TenantID: 7101, JudgementID: "other-conv", Platform: "telegram", AccountID: "acct-b", ConversationID: "same-conv", SenderID: "same-user", Summary: "其他账号会话"},
		{TenantID: 7102, JudgementID: "other-tenant", Platform: "telegram", AccountID: "11", ConversationID: "same-conv", SenderID: "same-user", Summary: "其他租户会话"},
		{TenantID: 7101, JudgementID: "canonical-group", Platform: "telegram", AccountID: "11", ConversationID: "group-conv", GroupID: "same-group", IsGroup: true, SenderID: "member-a", Summary: "规范账号群"},
		{TenantID: 7101, JudgementID: "alias-group", Platform: "telegram", AccountID: "acct-a", ConversationID: "group-conv", GroupID: "same-group", IsGroup: true, SenderID: "member-b", Summary: "别名账号群"},
		{TenantID: 7101, JudgementID: "other-group", Platform: "telegram", AccountID: "acct-b", ConversationID: "group-conv", GroupID: "same-group", IsGroup: true, SenderID: "member-a", Summary: "其他账号群"},
		{TenantID: 7101, JudgementID: "sender-fallback", Platform: "telegram", AccountID: "acct-a", SenderID: "peer", Summary: "无会话私聊"},
		{TenantID: 7101, JudgementID: "explicit-conv", Platform: "telegram", AccountID: "acct-a", ConversationID: "other-conv", SenderID: "peer", Summary: "不得被 sender 兜底夹带"},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&rows).Error; err != nil {
		t.Fatalf("准备判定夹具失败: %v", err)
	}
	sessions := []*model.CustomerSession{
		{TenantID: 7101, SessionID: "canonical-session", Platform: model.ChannelTelegram, AccountID: "11", UserID: "same-user", UserName: "A", Status: model.SessionStatusResolved},
		{TenantID: 7101, SessionID: "alias-session", Platform: model.ChannelTelegram, AccountID: "acct-a", UserID: "same-user", UserName: "A", Status: model.SessionStatusClosed},
		{TenantID: 7101, SessionID: "other-session", Platform: model.ChannelTelegram, AccountID: "acct-b", UserID: "same-user", UserName: "B", Status: model.SessionStatusPending},
		{TenantID: 7102, SessionID: "other-tenant-session", Platform: model.ChannelTelegram, AccountID: "11", UserID: "same-user", UserName: "Other", Status: model.SessionStatusPending},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&sessions).Error; err != nil {
		t.Fatalf("准备会话夹具失败: %v", err)
	}

	ctx := tenant.WithTenantID(context.Background(), 7101)
	judgements := NewLeadMiningJudgementRepositoryWithDB(database)
	got, err := judgements.ListRecentForReAct(ctx, "telegram", []string{"11", "acct-a"}, "same-conv", "", "same-user", false, "", 20)
	if err != nil || len(got) != 2 {
		t.Fatalf("canonical+alias 会话应返回两条，实际 %+v,err=%v", got, err)
	}
	got, err = judgements.ListRecentForReAct(ctx, "telegram", []string{"11", "acct-a"}, "group-conv", "same-group", "member-a", true, "", 20)
	if err != nil || len(got) != 2 {
		t.Fatalf("canonical+alias 群应返回两条，实际 %+v,err=%v", got, err)
	}
	got, err = judgements.ListRecentForReAct(ctx, "telegram", []string{"11", "acct-a"}, "", "", "peer", false, "", 20)
	if err != nil || len(got) != 1 || got[0].JudgementID != "sender-fallback" {
		t.Fatalf("sender 兜底不得夹带明确会话，实际 %+v,err=%v", got, err)
	}
	if _, err := judgements.ListRecentForReAct(ctx, "telegram", []string{"11"}, "", "", "", true, "", 20); err == nil {
		t.Fatal("群查询缺 group_id 必须拒绝")
	}
	if _, err := judgements.ListRecentForReAct(tenant.WithSystemScope(ctx), "telegram", []string{"11"}, "same-conv", "", "same-user", false, "", 20); err == nil {
		t.Fatal("SystemScope 必须 fail-closed")
	}

	sessionRepo := NewCustomerSessionRepositoryWithDB(database)
	gotSessions, total, err := sessionRepo.ListRecentForReAct(ctx, model.ChannelTelegram, []string{"11", "acct-a"}, "same-user", 10)
	if err != nil || total != 2 || len(gotSessions) != 2 {
		t.Fatalf("canonical+alias 会话摘要应返回两条，实际 %+v,total=%d,err=%v", gotSessions, total, err)
	}
	statusCounts, err := sessionRepo.CountByStatusForReAct(ctx, model.ChannelTelegram, []string{"11", "acct-a"}, "same-user")
	if err != nil || statusCounts[model.SessionStatusResolved] != 1 || statusCounts[model.SessionStatusClosed] != 1 {
		t.Fatalf("状态统计应覆盖 canonical+alias，实际 %+v,err=%v", statusCounts, err)
	}
	if _, _, err := sessionRepo.ListRecentForReAct(context.Background(), model.ChannelTelegram, []string{"11"}, "same-user", 10); err == nil {
		t.Fatal("多租户下缺租户上下文必须 fail-closed")
	}
	if _, _, err := sessionRepo.ListRecentForReAct(tenant.WithSystemScope(ctx), model.ChannelTelegram, []string{"11"}, "same-user", 10); err == nil {
		t.Fatal("Customer360 SystemScope 必须 fail-closed")
	}
}
