package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestLeadReactProductionResolvers_ScopedMinimalSummaries(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{}, &model.CustomerSession{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	judgements := []*model.LeadMiningJudgement{
		{TenantID: 7201, JudgementID: "a-lead", Platform: "telegram", AccountID: "acct-a", ConversationID: "conv-a", SenderID: "u-a", Summary: "A采购意向", IntentScore: 80, LeadRole: "demand"},
		{TenantID: 7201, JudgementID: "b-lead", Platform: "telegram", AccountID: "acct-b", ConversationID: "conv-a", SenderID: "u-a", Summary: "B供货意向", IntentScore: 70, LeadRole: "supply"},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&judgements).Error; err != nil {
		t.Fatalf("准备判定夹具失败: %v", err)
	}
	sessions := []*model.CustomerSession{
		{TenantID: 7201, SessionID: "a-session", Platform: model.ChannelTelegram, AccountID: "acct-a", UserID: "u-a", UserName: "Alice", Status: model.SessionStatusResolved, LastMessage: "A私密消息", MessageCount: 3},
		{TenantID: 7201, SessionID: "b-session", Platform: model.ChannelTelegram, AccountID: "acct-b", UserID: "u-a", UserName: "Bob", Status: model.SessionStatusClosed, LastMessage: "B私密消息", MessageCount: 9},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&sessions).Error; err != nil {
		t.Fatalf("准备会话夹具失败: %v", err)
	}

	resolver, err := NewLeadReactResolver(repository.NewLeadMiningJudgementRepositoryWithDB(database), repository.NewCustomerSessionRepositoryWithDB(database))
	if err != nil {
		t.Fatalf("构造生产 resolver 失败: %v", err)
	}
	ctx := tenant.WithTenantID(context.Background(), 7201)
	task := LeadReactTask{
		Platform: "telegram", CanonicalAccountID: 11, ChannelID: 3,
		AccountIDs: []string{"11", "acct-a"}, SourceRef: "acct-a", ScopeValid: true,
		ConversationID: "conv-a", SenderID: "u-a", CurrentMessage: "这个能买吗",
	}

	leadJSON, err := resolver.HistoricalLeads(ctx, task, "")
	if err != nil {
		t.Fatalf("历史线索取证失败: %v", err)
	}
	if !strings.Contains(leadJSON, "A采购意向") || strings.Contains(leadJSON, "B供货意向") || strings.Contains(leadJSON, "context_json") || strings.Contains(leadJSON, "result_json") {
		t.Fatalf("历史线索摘要越界或未做字段白名单: %s", leadJSON)
	}
	customerJSON, err := resolver.Customer360(ctx, task, "u-a")
	if err != nil {
		t.Fatalf("客户360取证失败: %v", err)
	}
	for _, forbidden := range []string{"B私密消息", "A私密消息", "Bob", "message_history", "orders", "clues", "user_phone", "user_email"} {
		if strings.Contains(customerJSON, forbidden) {
			t.Fatalf("客户360摘要暴露禁止字段 %q: %s", forbidden, customerJSON)
		}
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(customerJSON), &summary); err != nil {
		t.Fatalf("客户360摘要不是合法 JSON: %v", err)
	}
	if summary["session_count"] != float64(1) || summary["user_id"] != "u-a" {
		t.Fatalf("客户360摘要范围/计数错误: %+v", summary)
	}
}

func TestNewLeadReactResolver_EmptyDependenciesFailVisible(t *testing.T) {
	if _, err := NewLeadReactResolver(nil, nil); err == nil {
		t.Fatal("空依赖必须返回明确错误")
	}
	if _, err := NewLeadReactResolver(
		repository.NewLeadMiningJudgementRepositoryWithDB(nil),
		repository.NewCustomerSessionRepositoryWithDB(nil),
	); err == nil {
		t.Fatal("nil DB 依赖必须返回明确错误")
	}
}

func TestLeadReactProductionResolver_StatusCountsCoverAllSessions(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.CustomerSession{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	rows := make([]*model.CustomerSession, 0, 12)
	for i := 0; i < 12; i++ {
		status := model.SessionStatusResolved
		if i == 11 {
			status = model.SessionStatusClosed
		}
		rows = append(rows, &model.CustomerSession{
			TenantID: 7251, SessionID: "status-" + string(rune('a'+i)),
			Platform: model.ChannelTelegram, AccountID: "acct-a", UserID: "u-a", Status: status,
		})
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&rows).Error; err != nil {
		t.Fatalf("准备会话夹具失败: %v", err)
	}
	resolver, err := NewLeadReactResolver(
		repository.NewLeadMiningJudgementRepositoryWithDB(database),
		repository.NewCustomerSessionRepositoryWithDB(database),
	)
	if err != nil {
		t.Fatalf("构造 resolver 失败: %v", err)
	}
	raw, err := resolver.Customer360(tenant.WithTenantID(context.Background(), 7251), LeadReactTask{
		Platform: "telegram", CanonicalAccountID: 11, ChannelID: 3,
		AccountIDs: []string{"11", "acct-a"}, SourceRef: "acct-a", ScopeValid: true,
		SenderID: "u-a",
	}, "u-a")
	if err != nil {
		t.Fatalf("客户360取证失败: %v", err)
	}
	var summary struct {
		SessionCount int            `json:"session_count"`
		StatusCounts map[string]int `json:"status_counts"`
		Recent       []any          `json:"recent_sessions"`
	}
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatalf("解析摘要失败: %v", err)
	}
	if summary.SessionCount != 12 || summary.StatusCounts["resolved"] != 11 || summary.StatusCounts["closed"] != 1 || len(summary.Recent) != 10 {
		t.Fatalf("状态统计必须覆盖全部会话，最近列表仍有界: %+v", summary)
	}
}
