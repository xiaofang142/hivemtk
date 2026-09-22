package repository

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupKBTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KnowledgeBase{}, &model.AgentKBBinding{})
	if err := db.Exec("TRUNCATE TABLE knowledge_bases RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate knowledge_bases: %v", err)
	}
	if err := db.Exec("TRUNCATE TABLE agent_kb_bindings RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate agent_kb_bindings: %v", err)
	}
	return db
}

func setupKBRepoWithTX(t *testing.T) (repo *KnowledgeBaseRepository, tx *gorm.DB, cleanup func()) {
	t.Helper()
	db := setupKBTestDB(t)
	tx = db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	repo = NewKnowledgeBaseRepository(tx)
	cleanup = func() { tx.Rollback() }
	return
}

func newTestKB(code, kbType, ownerType string, agentID *uint) *model.KnowledgeBase {
	return &model.KnowledgeBase{
		KBCode:       code,
		Type:         kbType,
		Name:         "test-" + code,
		OwnerType:    ownerType,
		OwnerAgentID: agentID,
		Enabled:      boolPtr(true),
	}
}

func TestKnowledgeBaseRepository_CreateAndGetByID(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agent := uint(7)
	kb := newTestKB("KB-FAQ-001", model.KnowledgeBaseTypeFAQ, model.KnowledgeBaseOwnerPrivate, &agent)
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatal(err)
	}
	if kb.ID == 0 {
		t.Error("expected auto-increment ID")
	}
	got, err := repo.GetByID(ctx, kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.KBCode != "KB-FAQ-001" {
		t.Errorf("KBCode mismatch: got %q", got.KBCode)
	}
}

func TestKnowledgeBaseRepository_GetByCode(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	kb := newTestKB("KB-RAG-SHARED", model.KnowledgeBaseTypeRAG, model.KnowledgeBaseOwnerShared, nil)
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByCode(ctx, "KB-RAG-SHARED")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.Type != model.KnowledgeBaseTypeRAG {
		t.Errorf("Type mismatch: got %q", got.Type)
	}
}

func TestKnowledgeBaseRepository_ListByType(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for _, code := range []string{"KB-FAQ-1", "KB-FAQ-2", "KB-RAG-1"} {
		kbType := model.KnowledgeBaseTypeFAQ
		if code == "KB-RAG-1" {
			kbType = model.KnowledgeBaseTypeRAG
		}
		kb := newTestKB(code, kbType, model.KnowledgeBaseOwnerShared, nil)
		if err := repo.Create(ctx, kb); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.ListByType(ctx, model.KnowledgeBaseTypeFAQ, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 FAQ KB, got %d", len(got))
	}
}

func TestKnowledgeBaseRepository_ListByAgent(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1, a2 := uint(1), uint(2)
	if err := repo.Create(ctx, newTestKB("K1", "faq", "private", &a1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K2", "rag", "private", &a1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K3", "faq", "private", &a2)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K4", "faq", "shared", nil)); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListByAgent(ctx, a1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 KB for agent=1, got %d", len(got))
	}
	for _, kb := range got {
		if kb.OwnerAgentID == nil || *kb.OwnerAgentID != 1 {
			t.Errorf("expected owner_agent_id=1, got %v", kb.OwnerAgentID)
		}
	}
}

func TestKnowledgeBaseRepository_ListShared(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1 := uint(1)
	if err := repo.Create(ctx, newTestKB("K1", "faq", "shared", nil)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K2", "rag", "shared", nil)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K3", "faq", "private", &a1)); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListShared(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 shared, got %d", len(got))
	}
	for _, kb := range got {
		if kb.OwnerType != model.KnowledgeBaseOwnerShared {
			t.Errorf("expected shared, got %q", kb.OwnerType)
		}
	}
}

func TestKnowledgeBaseRepository_Update(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	kb := newTestKB("K1", "faq", "shared", nil)
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatal(err)
	}
	kb.Name = "updated name"
	if err := repo.Update(ctx, kb.ID, kb); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, kb.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		// GetByID 把 record-not-found 归成 (nil, nil)：没有这一判，下一行 panic 掉整个二进制。
		t.Fatalf("GetByID 交回 (nil, nil) ⇒ 按 id=%d 没查到行", kb.ID)
	}
	if got.Name != "updated name" {
		t.Errorf("expected updated name, got %q", got.Name)
	}
}

func TestKnowledgeBaseRepository_Delete(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	kb := newTestKB("K1", "faq", "shared", nil)
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, kb.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetByID(ctx, kb.ID)
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestKnowledgeBaseRepository_CountByAgent(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1, a2 := uint(1), uint(2)
	if err := repo.Create(ctx, newTestKB("K1", "faq", "private", &a1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K2", "rag", "private", &a1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestKB("K3", "faq", "private", &a2)); err != nil {
		t.Fatal(err)
	}
	c1, _ := repo.CountByAgent(ctx, a1)
	if c1 != 2 {
		t.Errorf("expected count=2 for agent=1, got %d", c1)
	}
	c2, _ := repo.CountByAgent(ctx, a2)
	if c2 != 1 {
		t.Errorf("expected count=1 for agent=2, got %d", c2)
	}
}

// TestKnowledgeBaseRepository_GetByID_NotFound 验证 GetByID 不存在返回 nil
func TestKnowledgeBaseRepository_GetByID_NotFound(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	got, err := repo.GetByID(ctx, 99999)
	if err != nil {
		t.Fatalf("expected nil error for not-found, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent ID, got %+v", got)
	}
}

// TestKnowledgeBaseRepository_GetByCode_NotFound 验证 GetByCode 不存在返回 nil
func TestKnowledgeBaseRepository_GetByCode_NotFound(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	got, err := repo.GetByCode(ctx, "KB-NON-EXISTENT")
	if err != nil {
		t.Fatalf("expected nil error for not-found, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent code, got %+v", got)
	}
}

// TestKnowledgeBaseRepository_DuplicateCode 验证 kb_code 唯一约束
func TestKnowledgeBaseRepository_DuplicateCode(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agent := uint(1)
	kb1 := newTestKB("KB-DUP-001", "faq", "private", &agent)
	if err := repo.Create(ctx, kb1); err != nil {
		t.Fatal(err)
	}

	kb2 := newTestKB("KB-DUP-001", "rag", "shared", nil)
	err := repo.Create(ctx, kb2)
	if err == nil {
		t.Fatal("expected duplicate kb_code error")
	}
}

// TestKnowledgeBaseRepository_List_FilterByType 验证 List 按 type 过滤
func TestKnowledgeBaseRepository_List_FilterByType(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for _, code := range []string{"F1", "F2", "R1", "S1"} {
		kbType := "faq"
		switch code {
		case "R1":
			kbType = "rag"
		case "S1":
			kbType = "sop"
		}
		if err := repo.Create(ctx, newTestKB(code, kbType, "shared", nil)); err != nil {
			t.Fatal(err)
		}
	}
	kbs, total, err := repo.List(ctx, KBListFilter{Type: "faq"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for type=faq, got %d", total)
	}
	if len(kbs) != 2 {
		t.Errorf("expected 2 items, got %d", len(kbs))
	}
}

// TestKnowledgeBaseRepository_List_FilterByOwnerType 验证 List 按 owner_type 过滤
func TestKnowledgeBaseRepository_List_FilterByOwnerType(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1 := uint(1)
	_ = repo.Create(ctx, newTestKB("P1", "faq", "private", &a1))
	_ = repo.Create(ctx, newTestKB("P2", "faq", "private", &a1))
	_ = repo.Create(ctx, newTestKB("S1", "faq", "shared", nil))
	_ = repo.Create(ctx, newTestKB("S2", "faq", "shared", nil))

	kbs, total, err := repo.List(ctx, KBListFilter{OwnerType: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for owner_type=shared, got %d", total)
	}
	if len(kbs) != 2 {
		t.Errorf("expected 2 items, got %d", len(kbs))
	}
}

// TestKnowledgeBaseRepository_List_FilterByEnabled 验证 List 按 enabled 过滤
func TestKnowledgeBaseRepository_List_FilterByEnabled(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	enabled := false
	_ = repo.Create(ctx, newTestKB("E1", "faq", "shared", nil))
	kb2 := newTestKB("D1", "faq", "shared", nil)
	kb2.Enabled = &enabled
	_ = repo.Create(ctx, kb2)

	kbs, total, err := repo.List(ctx, KBListFilter{Enabled: &[]bool{true}[0]})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected total=1 for enabled=true, got %d", total)
	}
	if len(kbs) != 1 || kbs[0].KBCode != "E1" {
		t.Errorf("expected only E1, got %v", kbs)
	}
}

// TestKnowledgeBaseRepository_List_FilterByAgent 验证 List 按 OwnerAgentID 过滤
func TestKnowledgeBaseRepository_List_FilterByAgent(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1, a2 := uint(100), uint(200)
	_ = repo.Create(ctx, newTestKB("A1K1", "faq", "private", &a1))
	_ = repo.Create(ctx, newTestKB("A1K2", "faq", "private", &a1))
	_ = repo.Create(ctx, newTestKB("A2K1", "faq", "private", &a2))

	kbs, total, err := repo.List(ctx, KBListFilter{OwnerAgentID: &a1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for agent=100, got %d", total)
	}
	if len(kbs) != 2 {
		t.Errorf("expected 2 items, got %d", len(kbs))
	}
}

// TestKnowledgeBaseRepository_List_LimitDefault 验证 List 默认 limit
func TestKnowledgeBaseRepository_List_LimitDefault(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = repo.Create(ctx, newTestKB(fmt.Sprintf("L%d", i), "faq", "shared", nil))
	}
	_, total, err := repo.List(ctx, KBListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
}

// TestKnowledgeBaseRepository_List_Pagination 验证 List 分页
func TestKnowledgeBaseRepository_List_Pagination(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = repo.Create(ctx, newTestKB(fmt.Sprintf("P%d", i), "faq", "shared", nil))
	}
	kbs, total, err := repo.List(ctx, KBListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(kbs) != 2 {
		t.Errorf("expected 2 items per page, got %d", len(kbs))
	}
}

// TestKnowledgeBaseRepository_ListByAgent_ZeroAgentID 验证 ListByAgent 传入 0
func TestKnowledgeBaseRepository_ListByAgent_ZeroAgentID(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	got, err := repo.ListByAgent(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for agentID=0, got %v", got)
	}
}

// TestKnowledgeBaseRepository_ListByAgent_IncludesSharedBinding 验证 agent 通过 binding 看到 shared KB
func TestKnowledgeBaseRepository_ListByAgent_IncludesSharedBinding(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	a1 := uint(11)
	_ = repo.Create(ctx, newTestKB("P1", "faq", "private", &a1))

	shared := newTestKB("S1", "rag", "shared", nil)
	_ = repo.Create(ctx, shared)

	got, _ := repo.ListByAgent(ctx, a1)
	if len(got) != 1 {
		t.Fatalf("expected 1 (only P1), got %d", len(got))
	}

	bindingRepo := NewAgentKBBindingRepository(repo.db)
	binding := &model.AgentKBBinding{
		AgentID: a1,
		KBID:    shared.ID,
		KBType:  "rag",
		Role:    "primary",
		Enabled: boolPtr(true),
	}
	if err := bindingRepo.Create(ctx, binding); err != nil {
		t.Fatal(err)
	}

	got, _ = repo.ListByAgent(ctx, a1)
	if len(got) != 2 {
		t.Fatalf("expected 2 (P1 + S1), got %d", len(got))
	}
	found := false
	for _, kb := range got {
		if kb.KBCode == "S1" {
			found = true
		}
	}
	if !found {
		t.Error("expected S1 visible after binding")
	}
}

// TestKnowledgeBaseRepository_ListByType_Limit 验证 ListByType 限制
func TestKnowledgeBaseRepository_ListByType_Limit(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = repo.Create(ctx, newTestKB(fmt.Sprintf("T%d", i), "faq", "shared", nil))
	}
	got, err := repo.ListByType(ctx, "faq", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(got))
	}
}

// TestKnowledgeBaseRepository_ListShared_FilterByType 验证 ListShared 按类型过滤
func TestKnowledgeBaseRepository_ListShared_FilterByType(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	_ = repo.Create(ctx, newTestKB("F1", "faq", "shared", nil))
	_ = repo.Create(ctx, newTestKB("R1", "rag", "shared", nil))
	_ = repo.Create(ctx, newTestKB("F2", "faq", "shared", nil))

	got, err := repo.ListShared(ctx, "faq")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 faq-shared, got %d", len(got))
	}
	for _, kb := range got {
		if kb.Type != "faq" {
			t.Errorf("expected type=faq, got %q", kb.Type)
		}
	}
}

// TestKnowledgeBaseRepository_Update_NonExistent 验证 Update 不存在 ID
func TestKnowledgeBaseRepository_Update_NonExistent(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	kb := newTestKB("X", "faq", "shared", nil)
	kb.Name = "updated"
	_ = repo.Update(ctx, 99999, kb)
}

// TestKnowledgeBaseRepository_Delete_NonExistent 验证 Delete 不存在 ID 不报错
func TestKnowledgeBaseRepository_Delete_NonExistent(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	if err := repo.Delete(ctx, 99999); err != nil {
		t.Errorf("expected nil error for non-existent delete, got %v", err)
	}
}

// TestKnowledgeBaseRepository_CountByAgent_NoKB 验证 CountByAgent 0 KB
func TestKnowledgeBaseRepository_CountByAgent_NoKB(t *testing.T) {
	repo, _, done := setupKBRepoWithTX(t)
	defer done()
	ctx := context.Background()

	c, err := repo.CountByAgent(ctx, 99999)
	if err != nil {
		t.Fatal(err)
	}
	if c != 0 {
		t.Errorf("expected count=0 for agent with no KB, got %d", c)
	}
}
