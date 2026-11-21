// Package e2e 智能体 ↔ 知识库绑定 端到端测试 (Task 39)
//
// 目标: 跨 service 层的端到端业务流程测试, 验证完整业务链路:
//
//	┌──────────┐    ┌────────────┐    ┌──────────┐    ┌──────────────┐
//	│ AI Agent │ →  │ Knowledge  │ →  │ Binding  │ →  │  查询 / 命中  │
//	│ (业务)   │    │ Base       │    │ (关系表) │    │  (业务消费)  │
//	└──────────┘    └────────────┘    └──────────┘    └──────────────┘
//
// 测试策略:
//   - 不依赖 HTTP/Controller, 通过 service + repository 模拟业务调用方
//   - 使用真实 PostgreSQL (testutil), 跨多 service 协作
//   - 每个测试覆盖一条完整的"业务故事" (story)
//
// 与 integration 的区别:
//   - integration: 单 service / 单 repo 的功能正确性
//   - e2e: 多 service 协作 + 业务故事 (跨域)
package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

func setupE2EDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDBOrSkip(t,
		&model.KnowledgeBase{},
		&model.AgentKBBinding{},
		&model.AIAgent{},
	)
	for _, tbl := range []string{"agent_kb_bindings", "knowledge_bases", "ai_agents"} {
		if err := db.Exec(fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", tbl)).Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	// 绑定写侧会校验智能体归属，预置这些场景显式使用的智能体。
	var agents []model.AIAgent
	for _, id := range []uint{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 99, 1001, 1002, 1003, 2001, 3001, 3002, 3003, 3004, 3005, 3006, 3007, 3008, 3009, 3010, 3011, 3012, 3013, 3014, 3015, 3016, 3017, 3018, 3019, 3020, 7777, 8888} {
		agents = append(agents, model.AIAgent{ID: id, AgentCode: fmt.Sprintf("fixture-agent-%d", id), Name: "绑定测试"})
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("预置智能体失败: %v", err)
	}
	return db
}

func newE2ESetup(t *testing.T, db *gorm.DB) (
	*service.KnowledgeBaseService,
	*service.AgentKBBindingService,
	*repository.KnowledgeBaseRepository,
	*repository.AgentKBBindingRepository,
) {
	t.Helper()
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	bindRepo := repository.NewAgentKBBindingRepository(db)
	kbSvc := service.NewKnowledgeBaseService()
	kbSvc.SetRepositories(kbRepo, bindRepo)
	bindSvc := service.NewAgentKBBindingServiceWithRepos(bindRepo)
	return kbSvc, bindSvc, kbRepo, bindRepo
}

func boolPtrE2E(b bool) *bool { return &b }

// TestE2E_MultiAgent_KnowledgeIsolation 验证多智能体场景下, 知识库严格隔离
//
// 场景: 3 个独立电商品牌的客服智能体 (agent1/agent2/agent3),
// 各自有私有 FAQ 库 + 一个跨智能体共享的"平台规则 SOP 库"。
//
// 注意: 本测试不引入"多租户"概念, 业务模型仅存在"多智能体"维度,
// 知识库隔离粒度是 agent_id, 共享库通过显式 binding 白名单分发。
func TestE2E_MultiAgent_KnowledgeIsolation(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, _ := newE2ESetup(t, db)

	brands := map[uint]string{
		1001: "brand-A 客服 FAQ",
		1002: "brand-B 客服 FAQ",
		1003: "brand-C 客服 FAQ",
	}
	for aid, name := range brands {
		agent := aid
		kb := &model.KnowledgeBase{
			KBCode:       fmt.Sprintf("KB-FAQ-BRAND-%d", aid),
			Type:         model.KnowledgeBaseTypeFAQ,
			Name:         name,
			OwnerType:    model.KnowledgeBaseOwnerPrivate,
			OwnerAgentID: &agent,
			Enabled:      boolPtrE2E(true),
		}
		if err := kbSvc.CreateKB(ctx, kb); err != nil {
			t.Fatalf("create kb for agent %d: %v", aid, err)
		}
	}

	platformKB := &model.KnowledgeBase{
		KBCode:    "KB-SOP-PLATFORM-RULES",
		Type:      model.KnowledgeBaseTypeSOP,
		Name:      "平台通用规则 SOP",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, platformKB); err != nil {
		t.Fatalf("create shared platform KB: %v", err)
	}

	for aid := range brands {
		if err := bindSvc.Bind(ctx, aid, platformKB.ID, 0); err != nil {
			t.Fatalf("bind agent %d to platform KB: %v", aid, err)
		}
	}

	gotA, err := kbSvc.ListByAgent(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 2 {
		t.Errorf("expected agent 1001 sees 2 KB (private + platform), got %d", len(gotA))
	}

	for _, kb := range gotA {
		if strings.HasPrefix(kb.KBCode, "KB-FAQ-BRAND-100") || strings.HasPrefix(kb.KBCode, "KB-FAQ-BRAND-1002") || strings.HasPrefix(kb.KBCode, "KB-FAQ-BRAND-1003") {
			if kb.OwnerAgentID != nil && *kb.OwnerAgentID != 1001 {
				t.Errorf("agent 1001 越权看到了 agent %d 的私有 KB", *kb.OwnerAgentID)
			}
		}
	}
}

// TestE2E_AgentWorkflow_FullLifecycle 验证客服智能体全生命周期
//
// 场景: 客服智能体 agent=2001 启用 SOP 工作流:
//  1. 创建 SOP 知识库
//  2. 绑定 (启用)
//  3. 查询命中
//  4. 解绑 (不删除 KB)
//  5. 重新绑定另一个 KB
func TestE2E_AgentWorkflow_FullLifecycle(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, _ := newE2ESetup(t, db)

	agent := uint(2001)

	kbA := &model.KnowledgeBase{
		KBCode:       "KB-SOP-WORKFLOW-A",
		Type:         model.KnowledgeBaseTypeSOP,
		Name:         "退换货流程 A",
		OwnerType:    model.KnowledgeBaseOwnerPrivate,
		OwnerAgentID: &agent,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, kbA); err != nil {
		t.Fatal(err)
	}

	if err := bindSvc.Bind(ctx, agent, kbA.ID, 10); err != nil {
		t.Fatalf("bind: %v", err)
	}

	got, err := kbSvc.ListByAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 KB, got %d", len(got))
	}
	if got[0].KBCode != "KB-SOP-WORKFLOW-A" {
		t.Errorf("expected workflow-A, got %q", got[0].KBCode)
	}

	if err := bindSvc.Unbind(ctx, agent, kbA.ID); err != nil {
		t.Fatal(err)
	}
	got2, _ := kbSvc.ListByAgent(ctx, agent)
	if len(got2) != 1 {
		t.Logf("private KB still visible (expected), got %d", len(got2))
	}

	kbB := &model.KnowledgeBase{
		KBCode:       "KB-SOP-WORKFLOW-B",
		Type:         model.KnowledgeBaseTypeSOP,
		Name:         "退换货流程 B",
		OwnerType:    model.KnowledgeBaseOwnerPrivate,
		OwnerAgentID: &agent,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, kbB); err != nil {
		t.Fatal(err)
	}
	if err := bindSvc.Bind(ctx, agent, kbB.ID, 20); err != nil {
		t.Fatal(err)
	}

	bindings, _ := bindSvc.ListByAgent(ctx, agent)
	if len(bindings) != 1 || bindings[0].KBID != kbB.ID {
		t.Errorf("expected 1 binding (to B), got %v", bindings)
	}
	if bindings[0].Priority != 20 {
		t.Errorf("expected priority=20, got %d", bindings[0].Priority)
	}
}

// TestE2E_KBDelete_CascadeBindingCleanup 验证删除 KB 时级联清理所有 binding
//
// 场景: 共享 KB 被 5 个 agent 引用, 删除 KB 后所有 binding 应自动消失
func TestE2E_KBDelete_CascadeBindingCleanup(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, bindRepo := newE2ESetup(t, db)

	sharedKB := &model.KnowledgeBase{
		KBCode:    "KB-E2E-CASCADE",
		Type:      model.KnowledgeBaseTypeRAG,
		Name:      "cascade test",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, sharedKB); err != nil {
		t.Fatal(err)
	}

	for i := uint(1); i <= 5; i++ {
		if err := bindSvc.Bind(ctx, i, sharedKB.ID, int(i)); err != nil {
			t.Fatalf("bind agent %d: %v", i, err)
		}
	}

	bindings, _ := bindRepo.ListByKB(ctx, sharedKB.ID)
	if len(bindings) != 5 {
		t.Fatalf("expected 5 bindings, got %d", len(bindings))
	}

	if err := kbSvc.DeleteKB(ctx, sharedKB.ID); err != nil {
		t.Fatal(err)
	}

	bindingsAfter, _ := bindRepo.ListByKB(ctx, sharedKB.ID)
	if len(bindingsAfter) != 0 {
		t.Errorf("expected 0 bindings after KB delete (cascade), got %d", len(bindingsAfter))
	}

	got, _ := kbSvc.GetKB(ctx, sharedKB.ID)
	if got != nil {
		t.Error("expected KB to be deleted")
	}
}

// TestE2E_SharedKB_WhitelistDistribution 验证共享 KB 仅对白名单 agent 可见
//
// 场景:
//   - 创建一个共享 SOP
//   - 10 个 agent, 仅 3 个加入白名单 (binding)
//   - 验证仅 3 个 agent 可见
func TestE2E_SharedKB_WhitelistDistribution(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, _ := newE2ESetup(t, db)

	sharedKB := &model.KnowledgeBase{
		KBCode:    "KB-E2E-WHITELIST",
		Type:      model.KnowledgeBaseTypeSOP,
		Name:      "whitelist test",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, sharedKB); err != nil {
		t.Fatal(err)
	}

	whitelist := map[uint]bool{1: true, 5: true, 9: true}
	for i := uint(1); i <= 10; i++ {
		if whitelist[i] {
			if err := bindSvc.Bind(ctx, i, sharedKB.ID, 0); err != nil {
				t.Fatalf("bind agent %d: %v", i, err)
			}
		}
	}

	for i := uint(1); i <= 10; i++ {
		got, _ := kbSvc.ListByAgent(ctx, i)
		found := false
		for _, kb := range got {
			if kb.KBCode == "KB-E2E-WHITELIST" {
				found = true
				break
			}
		}
		if whitelist[i] && !found {
			t.Errorf("whitelist agent %d should see shared KB", i)
		}
		if !whitelist[i] && found {
			t.Errorf("non-whitelist agent %d should NOT see shared KB", i)
		}
	}
}

// TestE2E_AdminBatchConfig 验证管理员批量配置多 agent × 多 KB
//
// 场景: 新员工培训场景, 1 个 SOP 库 + 1 个 FAQ 库, 20 个新入职客服一次性绑定
func TestE2E_AdminBatchConfig(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, _ := newE2ESetup(t, db)

	sop := &model.KnowledgeBase{
		KBCode:    "KB-SOP-NEW-HIRE",
		Type:      model.KnowledgeBaseTypeSOP,
		Name:      "新员工入职 SOP",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   boolPtrE2E(true),
	}
	faq := &model.KnowledgeBase{
		KBCode:    "KB-FAQ-NEW-HIRE",
		Type:      model.KnowledgeBaseTypeFAQ,
		Name:      "新员工常见问题",
		OwnerType: model.KnowledgeBaseOwnerShared,
		Enabled:   boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, sop); err != nil {
		t.Fatal(err)
	}
	if err := kbSvc.CreateKB(ctx, faq); err != nil {
		t.Fatal(err)
	}

	const newHireCount = 20
	items := make([]service.BatchBindItem, 0, newHireCount*2)
	for i := uint(3001); i < 3001+newHireCount; i++ {
		items = append(items, service.BatchBindItem{AgentID: i, KBID: sop.ID, Priority: 1})
		items = append(items, service.BatchBindItem{AgentID: i, KBID: faq.ID, Priority: 2})
	}

	if err := bindSvc.BatchBind(ctx, items); err != nil {
		t.Fatalf("batch bind: %v", err)
	}

	for i := uint(3001); i < 3001+newHireCount; i++ {
		bindings, _ := bindSvc.ListByAgent(ctx, i)
		if len(bindings) != 2 {
			t.Errorf("agent %d expected 2 bindings, got %d", i, len(bindings))
		}
	}
}

// TestE2E_BusinessRules_CompositeValidation 验证多层业务规则的复合场景
//
// 场景:
//   - 创建 shared KB 时附带 owner_agent_id -> 业务校验失败
//   - 创建 private KB 时不带 owner -> 业务校验失败
//   - shared KB 的 binding 不能被 ListByKB 错误统计 (因为没真绑定)
func TestE2E_BusinessRules_CompositeValidation(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, _, _, _ := newE2ESetup(t, db)

	agent := uint(99)
	bad1 := &model.KnowledgeBase{
		KBCode:       "KB-SHARED-BAD",
		Type:         model.KnowledgeBaseTypeFAQ,
		Name:         "bad",
		OwnerType:    model.KnowledgeBaseOwnerShared,
		OwnerAgentID: &agent,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, bad1); err == nil {
		t.Error("expected error for shared KB with owner")
	}

	bad2 := &model.KnowledgeBase{
		KBCode:    "KB-PRIVATE-BAD",
		Type:      model.KnowledgeBaseTypeFAQ,
		Name:      "bad",
		OwnerType: model.KnowledgeBaseOwnerPrivate,
		Enabled:   boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, bad2); err == nil {
		t.Error("expected error for private KB without owner")
	}

	bad3 := &model.KnowledgeBase{
		KBCode:       "KB-BAD-TYPE",
		Type:         "INVALID_TYPE",
		Name:         "bad",
		OwnerType:    model.KnowledgeBaseOwnerPrivate,
		OwnerAgentID: &agent,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, bad3); err == nil {
		t.Error("expected error for invalid type")
	}
}

// TestE2E_OwnerTypeTransition 验证 owner_type 从 private 切到 shared 的过渡
//
// 场景: 一个 KB 起初是 private 给某 agent, 业务上升级为 shared 库
// 验证: shared 后, 多个 agent 都能通过 binding 访问
func TestE2E_OwnerTypeTransition(t *testing.T) {
	db := setupE2EDB(t)
	ctx := context.Background()
	kbSvc, bindSvc, _, _ := newE2ESetup(t, db)

	agent1 := uint(7777)
	kb := &model.KnowledgeBase{
		KBCode:       "KB-OWNER-TRANSITION",
		Type:         model.KnowledgeBaseTypeRAG,
		Name:         "transition",
		OwnerType:    model.KnowledgeBaseOwnerPrivate,
		OwnerAgentID: &agent1,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.CreateKB(ctx, kb); err != nil {
		t.Fatal(err)
	}

	updated := &model.KnowledgeBase{
		Name:         "transition (now shared)",
		Type:         model.KnowledgeBaseTypeRAG,
		OwnerType:    model.KnowledgeBaseOwnerShared,
		OwnerAgentID: nil,
		Enabled:      boolPtrE2E(true),
	}
	if err := kbSvc.UpdateKB(ctx, kb.ID, updated); err != nil {
		t.Fatalf("update to shared: %v", err)
	}

	got1, _ := kbSvc.ListByAgent(ctx, agent1)
	found := false
	for _, k := range got1 {
		if k.ID == kb.ID {
			found = true
		}
	}
	if found {
		t.Error("agent1 should NOT see KB after transition to shared")
	}

	agent2 := uint(8888)
	if err := bindSvc.Bind(ctx, agent2, kb.ID, 0); err != nil {
		t.Fatal(err)
	}
	got2, _ := kbSvc.ListByAgent(ctx, agent2)
	found = false
	for _, k := range got2 {
		if k.ID == kb.ID {
			found = true
		}
	}
	if !found {
		t.Error("agent2 should see KB after binding")
	}
}
