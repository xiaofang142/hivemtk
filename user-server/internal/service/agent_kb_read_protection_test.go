package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 页面普通更新不调用全量替换时，真实绑定及生产答案缓存的 KB/版本选择均保留。
// 此处验证 resolveFAQKB 的绑定消费者，不把它描述为 FAQ 内容表的外键检索。
func TestAgentKBBindingReadProtection(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIAgent{}, &model.KnowledgeBase{}, &model.AgentKBBinding{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 1)
	agentRepo := repository.NewAIAgentRepositoryWithDB(database)
	agents := NewAIAgentServiceWithRepo(agentRepo)
	agent := &model.AIAgent{AgentCode: "kb-read-protection", Name: "原名称", Persona: "受控人设", AgentMode: "passive", Status: 1}
	if err := agents.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	kbRepo := repository.NewKnowledgeBaseRepository(database)
	original := &model.KnowledgeBase{KBCode: "read-original", Name: "原知识库", Type: model.KnowledgeBaseTypeFAQ, OwnerType: model.KnowledgeBaseOwnerShared, Enabled: boolPtr(true), Version: 7}
	replacement := &model.KnowledgeBase{KBCode: "read-replacement", Name: "新知识库", Type: model.KnowledgeBaseTypeFAQ, OwnerType: model.KnowledgeBaseOwnerShared, Enabled: boolPtr(true), Version: 9}
	for _, kb := range []*model.KnowledgeBase{original, replacement} {
		if err := kbRepo.Create(ctx, kb); err != nil {
			t.Fatal(err)
		}
	}
	bindings := NewAgentKBBindingServiceWithRepos(repository.NewAgentKBBindingRepository(database))
	consumer := &SmartCSOrchestrator{kbRepo: kbRepo}
	check := func(want *model.KnowledgeBase) {
		t.Helper()
		rows, err := bindings.ListByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		selected := consumer.resolveFAQKB(ctx, &AgentContext{AgentID: agent.ID})
		if want == nil {
			if len(rows) != 0 || selected != nil {
				t.Fatalf("显式清空后的绑定=%+v 消费者=%+v", rows, selected)
			}
			return
		}
		if len(rows) != 1 || rows[0] == nil || rows[0].KBID != want.ID || selected == nil || selected.ID != want.ID || selected.Version != want.Version {
			t.Fatalf("绑定/消费者未使用同一 KB 版本: rows=%+v selected=%+v want=%+v", rows, selected, want)
		}
	}
	if err := bindings.ReplaceByAgent(ctx, agent.ID, []uint{original.ID}); err != nil {
		t.Fatal(err)
	}
	check(original)
	readFailure := errors.New("受控绑定读取故障")
	const callback = "agent-kb-read-protection-failure"
	if err := database.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "agent_kb_bindings" {
			tx.AddError(readFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(callback) })
	if _, err := bindings.ListByAgent(ctx, agent.ID); !errors.Is(err, readFailure) {
		t.Fatalf("预期真实仓储读取失败，得到 %v", err)
	}
	if err := database.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	agent.Name = "读取失败后只修改名称"
	if err := agents.Update(ctx, agent); err != nil {
		t.Fatal(err)
	}
	check(original)
	// 另一编辑者改绑后，未改选择的普通更新也不能覆盖它。
	if err := bindings.ReplaceByAgent(ctx, agent.ID, []uint{replacement.ID}); err != nil {
		t.Fatal(err)
	}
	agent.Name = "保留并发改绑"
	if err := agents.Update(ctx, agent); err != nil {
		t.Fatal(err)
	}
	check(replacement)
	if err := bindings.ReplaceByAgent(ctx, agent.ID, nil); err != nil {
		t.Fatal(err)
	}
	check(nil)
}
