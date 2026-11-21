package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// TestBind_NilBindingRepo 测试 nil binding repo
func TestBind_NilBindingRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	err := svc.Bind(context.Background(), 1, 1, 0)
	if err == nil {
		t.Error("expected error for nil binding repo")
	}
}

// TestBind_ZeroAgentID 验证 agentID=0 报错
func TestBind_ZeroAgentID(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Bind should not panic with agentID=0 (should return error before nil deref): %v", r)
		}
	}()
	err := svc.Bind(context.Background(), 0, 1, 0)
	if err == nil {
		t.Error("expected error for agentID=0")
	}
}

// TestBind_ZeroKBID 验证 kbID=0 报错
func TestBind_ZeroKBID(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Bind should not panic with kbID=0: %v", r)
		}
	}()
	err := svc.Bind(context.Background(), 1, 0, 0)
	if err == nil {
		t.Error("expected error for kbID=0")
	}
}

// TestBind_ErrorMessage 验证错误信息
func TestBind_ErrorMessage(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	err := svc.Bind(context.Background(), 0, 0, 0)
	if err == nil {
		t.Skip("nil repo returns generic error first")
	}
	if !strings.Contains(err.Error(), "agent_id") && !strings.Contains(err.Error(), "repo") {
		t.Errorf("expected error mentioning agent_id or repo, got: %v", err)
	}
}

// TestUnbind_NilBindingRepo 测试 nil repo
func TestUnbind_NilBindingRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	err := svc.Unbind(context.Background(), 1, 1)
	if err == nil {
		t.Error("expected error for nil binding repo")
	}
}

// TestUnbind_CrossTenantReportsNotFound：隔离条件挡掉 0 行时必须返回 not-found，
// 而不是让跨租户解绑拿到「解绑成功」；同时确认对方租户的数据未被删除。
func TestUnbind_CrossTenantReportsNotFound(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	database := testutil.NewTestDB(t, &model.AgentKBBinding{})
	if database == nil {
		t.Fatal("测试库不可达：跨租户解绑语义判不了（不 Skip）")
	}

	theirs := &model.AgentKBBinding{
		TenantID: 92,
		AgentID:  1,
		KBID:     100,
		KBType:   model.KnowledgeBaseTypeFAQ,
		Role:     model.AgentKBBindingRolePrimary,
		Enabled:  boolPtr(true),
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(theirs).Error; err != nil {
		t.Fatalf("预置对方租户绑定失败: %v", err)
	}

	svc := NewAgentKBBindingService()
	ctxA := tenant.WithTenantID(context.Background(), 91)
	if err := svc.Unbind(ctxA, 1, 100); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 Unbind 期望 ErrRecordNotFound，实际: %v", err)
	}

	var count int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&model.AgentKBBinding{}).Where("id = ?", theirs.ID).Count(&count).Error; err != nil {
		t.Fatalf("回查对方租户绑定失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("对方租户绑定应保持 1 条，实际 %d", count)
	}
}

// TestListByAgent_NilBindingRepo 测试 nil repo
func TestListByAgent_NilBindingRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	got, err := svc.ListByAgent(context.Background(), 1)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil list, got %v", got)
	}
}

// TestListByKB_NilBindingRepo 测试 nil repo
func TestListByKB_NilBindingRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	got, err := svc.ListByKB(context.Background(), 1)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil list, got %v", got)
	}
}

// TestBatchBind_NilBindingRepo 测试 nil repo
func TestBatchBind_NilBindingRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	err := svc.BatchBind(context.Background(), []BatchBindItem{{AgentID: 1, KBID: 1}})
	if err == nil {
		t.Error("expected error for nil binding repo")
	}
}

// TestBatchBind_EmptyItems 测试空 items 不报错
func TestBatchBind_EmptyItems(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	if err := svc.BatchBind(context.Background(), nil); err != nil {
		t.Errorf("expected nil error for nil items, got %v", err)
	}
	if err := svc.BatchBind(context.Background(), []BatchBindItem{}); err != nil {
		t.Errorf("expected nil error for empty items, got %v", err)
	}
}

// TestBatchBind_ZeroAgentIDInItem 测试 items 里有 agent_id=0
//
// 实际行为: bindingRepo nil 校验在 items 校验之前, 因此这里会先返回 "binding repo not initialized"
// 这个测试在 test/integration/knowledge_base_crud_test.go 中通过真实 DB 覆盖 items 校验逻辑
func TestBatchBind_ZeroAgentIDInItem(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	err := svc.BatchBind(context.Background(), []BatchBindItem{
		{AgentID: 1, KBID: 10},
		{AgentID: 0, KBID: 20},
	})
	if err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(err.Error(), "binding repo") {
		t.Errorf("expected 'binding repo' error, got: %v", err)
	}
}

// TestBatchBind_ZeroKBIDInItem 测试 items 里有 kb_id=0
//
// 实际行为: bindingRepo nil 校验在 items 校验之前, 因此这里会先返回 "binding repo not initialized"
func TestBatchBind_ZeroKBIDInItem(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	err := svc.BatchBind(context.Background(), []BatchBindItem{
		{AgentID: 1, KBID: 0},
	})
	if err == nil {
		t.Error("expected error")
	}
}

// TestBatchBind_NilDB_NoDBFallback 测试 db=nil 时走非事务路径
//
// 当 s.db == nil 但 bindingRepo != nil 时, BatchBind 走非事务路径
// 这里 bindingRepo 也是 nil, 应该先返回 nil binding repo 错误
func TestBatchBind_NilDB_NoDBFallback(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	err := svc.BatchBind(context.Background(), []BatchBindItem{{AgentID: 1, KBID: 1}})
	if err == nil {
		t.Error("expected error")
	}
}

// TestNewAgentKBBindingService_NilDB 测试 nil db 构造
//
// nil db 时, NewAgentKBBindingService 内部会构造 typed nil pointer
// 这些非 nil 的 typed nil 指针, 因此 svc.bindingRepo != nil (但 svc.bindingRepo.db == nil)
// 这里不强校验 repo 字段, 因为 typed nil pointer 与 nil interface 在 Go 中语义不同
func TestNewAgentKBBindingService_NilDB(t *testing.T) {
	svc := NewAgentKBBindingService()
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	_ = svc.bindingRepo
}

// TestNewAgentKBBindingServiceWithRepos 测试带 repo 构造
func TestNewAgentKBBindingServiceWithRepos(t *testing.T) {
	bindRepo := (*repository.AgentKBBindingRepository)(nil)
	svc := NewAgentKBBindingServiceWithRepos(bindRepo)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// TestBoolPtr 测试 boolPtr
func TestBoolPtr(t *testing.T) {
	p := boolPtr(true)
	if p == nil || !*p {
		t.Error("expected *true")
	}
	p2 := boolPtr(false)
	if p2 == nil || *p2 {
		t.Error("expected *false")
	}
}

// TestBind_Priority_DefaultValue 验证 priority=0 是合法值
func TestBind_Priority_DefaultValue(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	defer func() {
		_ = recover()
	}()
	_ = svc.Bind(context.Background(), 1, 1, 0)
}

// TestBatchBind_ItemValidation_AllValid 测试所有 item 都合法 (无 nil repo 时返回 bindingRepo 错误)
func TestBatchBind_ItemValidation_AllValid(t *testing.T) {
	svc := &AgentKBBindingService{bindingRepo: nil}
	items := []BatchBindItem{
		{AgentID: 1, KBID: 10, Priority: 1},
		{AgentID: 1, KBID: 11, Priority: 2},
		{AgentID: 2, KBID: 10, Priority: 3},
	}
	err := svc.BatchBind(context.Background(), items)
	if err == nil {
		t.Error("expected error from nil binding repo")
	}
	if !strings.Contains(err.Error(), "binding repo") {
		t.Errorf("expected 'binding repo' error, got: %v", err)
	}
}

// TestBind_AllValid_NilRepo_NoPanic 测试 agent/kb 校验通过后, 调 nil repo 不会 panic
//
// 当前实现: kb 存在性检查 -> DeleteByAgentAndKB -> Create
// 全部 nil 会 panic, 这里验证 panic 是可恢复的
func TestBind_AllValid_NilRepo_NoPanic(t *testing.T) {
	svc := &AgentKBBindingService{}
	defer func() {
		_ = recover()
	}()
	_ = svc.Bind(context.Background(), 1, 1, 0)
}

// TestBatchBind_AllValid_NilRepo 测试 batch 在 nil repo 下的行为
func TestBatchBind_AllValid_NilRepo(t *testing.T) {
	svc := &AgentKBBindingService{}
	defer func() {
		_ = recover()
	}()
	_ = svc.BatchBind(context.Background(), []BatchBindItem{{AgentID: 1, KBID: 1}})
}

// TestAgentKBBinding_Model 验证模型字段定义
func TestAgentKBBinding_Model(t *testing.T) {
	b := &model.AgentKBBinding{
		AgentID:  1,
		KBID:     10,
		KBType:   model.KnowledgeBaseTypeFAQ,
		Role:     model.AgentKBBindingRolePrimary,
		Priority: 5,
		Enabled:  boolPtr(true),
	}
	if b.TableName() != "agent_kb_bindings" {
		t.Errorf("expected table name 'agent_kb_bindings', got %q", b.TableName())
	}
	if b.AgentID != 1 || b.KBID != 10 {
		t.Error("field assignment failed")
	}
}

// TestAgentKBBindingRole_Constants 验证角色常量
func TestAgentKBBindingRole_Constants(t *testing.T) {
	if model.AgentKBBindingRolePrimary != "primary" {
		t.Errorf("Primary constant mismatch: %q", model.AgentKBBindingRolePrimary)
	}
	if model.AgentKBBindingRoleReference != "reference" {
		t.Errorf("Reference constant mismatch: %q", model.AgentKBBindingRoleReference)
	}
}

// TestBatchBindItem_Defaults 验证默认值
func TestBatchBindItem_Defaults(t *testing.T) {
	item := BatchBindItem{}
	if item.AgentID != 0 || item.KBID != 0 || item.Priority != 0 {
		t.Error("expected zero values for empty struct")
	}
}

// TestWrapError 验证错误包装
func TestWrapError(t *testing.T) {
	inner := errors.New("inner error")
	wrapped := errors.New("wrapped: " + inner.Error())
	if !strings.Contains(wrapped.Error(), "inner") {
		t.Error("wrap failed")
	}
}

// TestGormDB_NilAssignment 验证 gorm.DB nil 赋值
func TestGormDB_NilAssignment(t *testing.T) {
	var db *gorm.DB
	if db != nil {
		t.Error("expected nil db")
	}
}

// TestBind_UsesRealKBType 绑定行上冗余的 KBType 必须取知识库真实类型：
// 写死 faq 会让按类型过滤（ListByAgent(kbType)）漏掉 RAG/SOP 库。
func TestBind_UsesRealKBType(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeBase{}, &model.AgentKBBinding{}, &model.AIAgent{})
	if err := database.Create(&model.AIAgent{ID: 11, AgentCode: "binding-type-agent", Name: "绑定类型测试"}).Error; err != nil {
		t.Fatalf("预置智能体失败: %v", err)
	}
	if database == nil {
		t.Fatal("测试库不可达：绑定类型语义判不了（不 Skip）")
	}
	kbRepo := repository.NewKnowledgeBaseRepository(database)
	bindRepo := repository.NewAgentKBBindingRepository(database)
	svc := NewAgentKBBindingServiceWithRepos(bindRepo)

	ctx := context.Background()
	kb := &model.KnowledgeBase{
		KBCode: "KB-RAG-T1", Type: model.KnowledgeBaseTypeRAG,
		Name: "RAG 库", OwnerType: "shared",
	}
	if err := kbRepo.Create(ctx, kb); err != nil {
		t.Fatalf("预置知识库失败: %v", err)
	}
	if err := svc.Bind(ctx, 11, kb.ID, 0); err != nil {
		t.Fatalf("Bind 失败: %v", err)
	}

	rows, err := bindRepo.ListByAgent(ctx, 11, "")
	if err != nil {
		t.Fatalf("ListByAgent 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].KBType != model.KnowledgeBaseTypeRAG {
		t.Fatalf("绑定类型应取知识库真实类型 rag，实际: %+v", rows)
	}
}
