package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAgentKBBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.AgentKBBinding{}, &model.KnowledgeBase{})
	if err := db.Exec("TRUNCATE TABLE agent_kb_bindings RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate agent_kb_bindings: %v", err)
	}
	return db
}

func setupAgentKBBindingRepoWithTX(t *testing.T) (repo *AgentKBBindingRepository, tx *gorm.DB, cleanup func()) {
	t.Helper()
	db := setupAgentKBBindingTestDB(t)
	tx = db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	repo = NewAgentKBBindingRepository(tx)
	cleanup = func() { tx.Rollback() }
	return
}

func newTestBinding(agentID, kbID uint, kbType, role string) *model.AgentKBBinding {
	return &model.AgentKBBinding{
		AgentID:  agentID,
		KBID:     kbID,
		KBType:   kbType,
		Role:     role,
		Priority: 0,
		Enabled:  boolPtr(true),
	}
}

func TestAgentKBBindingRepository_CreateAndCheckExists(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	b := newTestBinding(1, 100, model.KnowledgeBaseTypeFAQ, model.AgentKBBindingRolePrimary)
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if b.ID == 0 {
		t.Error("expected auto-increment ID")
	}
	exists, err := repo.CheckExists(ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected exists=true")
	}
	exists, _ = repo.CheckExists(ctx, 1, 999)
	if exists {
		t.Error("expected exists=false for non-existing")
	}
}

func TestAgentKBBindingRepository_DuplicateConflict(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Create(ctx, newTestBinding(1, 100, "faq", "primary")); err != nil {
		t.Fatal(err)
	}
	err := repo.Create(ctx, newTestBinding(1, 100, "faq", "primary"))
	if err == nil {
		t.Error("expected duplicate error")
	}
}

func TestAgentKBBindingRepository_ListByAgent(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Create(ctx, newTestBinding(1, 100, "faq", "primary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestBinding(1, 101, "rag", "primary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestBinding(1, 102, "sop", "reference")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestBinding(2, 200, "faq", "primary")); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByAgent(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 bindings for agent=1, got %d", len(got))
	}

	faqs, _ := repo.ListByAgent(ctx, 1, "faq")
	if len(faqs) != 1 {
		t.Errorf("expected 1 FAQ binding, got %d", len(faqs))
	}
}

func TestAgentKBBindingRepository_ListByKB(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Create(ctx, newTestBinding(1, 100, "faq", "primary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestBinding(2, 100, "faq", "primary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestBinding(3, 200, "faq", "primary")); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.ListByKB(ctx, 100)
	if len(got) != 2 {
		t.Errorf("expected 2 agents bound to KB=100, got %d", len(got))
	}
}

func TestAgentKBBindingRepository_DeleteByAgentKB(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Create(ctx, newTestBinding(1, 100, "faq", "primary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByAgentKB(ctx, 1, 100); err != nil {
		t.Fatal(err)
	}
	exists, _ := repo.CheckExists(ctx, 1, 100)
	if exists {
		t.Error("expected deleted")
	}
}

func TestAgentKBBindingRepository_Update(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	b := newTestBinding(1, 100, "faq", "primary")
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.Role = model.AgentKBBindingRoleReference
	b.Priority = 10
	if err := repo.Update(ctx, b.ID, b); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetByAgentKB(ctx, 1, 100)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.Role != model.AgentKBBindingRoleReference {
		t.Errorf("Role not updated: got %q", got.Role)
	}
	if got.Priority != 10 {
		t.Errorf("Priority not updated: got %d", got.Priority)
	}
}

func TestAgentKBBindingRepository_GetByAgentKB_NotFound(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	got, err := repo.GetByAgentKB(ctx, 999, 999)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for not-found")
	}
}

// TestAgentKBBindingRepository_DeleteByID 验证按 ID 删除
func TestAgentKBBindingRepository_DeleteByID(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	b := newTestBinding(1, 100, "faq", "primary")
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	exists, _ := repo.CheckExists(ctx, 1, 100)
	if exists {
		t.Error("expected deleted")
	}
}

// TestAgentKBBindingRepository_DeleteByAgent 验证级联删除某智能体所有绑定
func TestAgentKBBindingRepository_DeleteByAgent(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := repo.Create(ctx, newTestBinding(1, uint(100+i), "faq", "primary")); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.Create(ctx, newTestBinding(2, 200, "faq", "primary"))

	if err := repo.DeleteByAgent(ctx, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ListByAgentAll(ctx, 1)
	if len(got) != 0 {
		t.Errorf("expected 0 for agent=1, got %d", len(got))
	}
	got2, _ := repo.ListByAgentAll(ctx, 2)
	if len(got2) != 1 {
		t.Errorf("expected 1 for agent=2, got %d", len(got2))
	}
}

// TestAgentKBBindingRepository_DeleteByAgent_ZeroAgentID 验证 0 agentID 安全
func TestAgentKBBindingRepository_DeleteByAgent_ZeroAgentID(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.DeleteByAgent(ctx, 0); err != nil {
		t.Errorf("expected nil error for agentID=0, got %v", err)
	}
}

// TestAgentKBBindingRepository_DeleteByKB 验证级联删除某知识库所有绑定
func TestAgentKBBindingRepository_DeleteByKB(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := repo.Create(ctx, newTestBinding(uint(i+1), 100, "faq", "primary")); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.Create(ctx, newTestBinding(1, 200, "faq", "primary"))

	if err := repo.DeleteByKB(ctx, 100); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ListByKB(ctx, 100)
	if len(got) != 0 {
		t.Errorf("expected 0 bindings for KB=100, got %d", len(got))
	}
	got2, _ := repo.ListByKB(ctx, 200)
	if len(got2) != 1 {
		t.Errorf("expected 1 binding for KB=200, got %d", len(got2))
	}
}

// TestAgentKBBindingRepository_DeleteByKB_ZeroKBID 验证 0 kbID 安全
func TestAgentKBBindingRepository_DeleteByKB_ZeroKBID(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.DeleteByKB(ctx, 0); err != nil {
		t.Errorf("expected nil error for kbID=0, got %v", err)
	}
}

// TestAgentKBBindingRepository_ListByAgentAll 验证 ListByAgentAll 不过滤 enabled
func TestAgentKBBindingRepository_ListByAgentAll(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	disabled := false
	b1 := newTestBinding(1, 100, "faq", "primary")
	b1.Enabled = &disabled
	_ = repo.Create(ctx, b1)
	_ = repo.Create(ctx, newTestBinding(1, 101, "faq", "primary"))

	got, err := repo.ListByAgentAll(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 (including disabled), got %d", len(got))
	}
}

// TestAgentKBBindingRepository_CheckExists_False 验证不存在的组合
func TestAgentKBBindingRepository_CheckExists_False(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	_ = repo.Create(ctx, newTestBinding(1, 100, "faq", "primary"))
	exists, err := repo.CheckExists(ctx, 1, 999)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected exists=false for non-existing KB")
	}
	exists, _ = repo.CheckExists(ctx, 999, 100)
	if exists {
		t.Error("expected exists=false for non-existing agent")
	}
}

// TestAgentKBBindingRepository_Update_Priority 验证 priority 更新
func TestAgentKBBindingRepository_Update_Priority(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	b := newTestBinding(1, 100, "faq", "primary")
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.Priority = 99
	if err := repo.Update(ctx, b.ID, b); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByAgentKB(ctx, 1, 100)
	if err != nil {
		t.Fatalf("GetByAgentKB: %v", err)
	}
	if got == nil {
		// GetByAgentKB 把 record-not-found 归成 (nil, nil)：没有这一判，下一行的
		// nil 解引用会 panic 掉整个测试二进制，把「没查到」伪装成崩溃、连带带走同包其它用例。
		t.Fatal("GetByAgentKB 交回 (nil, nil) ⇒ 按 (agent_id,kb_id)=(1,100) 没查到行")
	}
	if got.Priority != 99 {
		t.Errorf("expected priority=99, got %d", got.Priority)
	}
}

// TestAgentKBBindingRepository_ListByAgent_PriorityOrdering 验证按 priority DESC 排序
func TestAgentKBBindingRepository_ListByAgent_PriorityOrdering(t *testing.T) {
	repo, _, done := setupAgentKBBindingRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i, p := range []int{5, 10, 1, 7} {
		b := newTestBinding(1, uint(100+i), "faq", "primary")
		b.Priority = p
		_ = repo.Create(ctx, b)
	}
	got, _ := repo.ListByAgent(ctx, 1, "")
	if len(got) != 4 {
		t.Fatalf("expected 4, got %d", len(got))
	}
	expected := []int{10, 7, 5, 1}
	for i, b := range got {
		if b.Priority != expected[i] {
			t.Errorf("position %d: expected priority=%d, got %d", i, expected[i], b.Priority)
		}
	}
}
