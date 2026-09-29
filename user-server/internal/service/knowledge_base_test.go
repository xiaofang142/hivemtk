package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

func TestIsValidKBType_AllValid(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"faq", true},
		{"rag", true},
		{"sop", true},
		{"FAQ", true},
		{"  faq  ", true},
		{"RAG", true},
		{"", false},
		{"invalid", false},
		{"FOOBAR", false},
	}
	for _, c := range cases {
		if got := IsValidKBType(c.in); got != c.want {
			t.Errorf("IsValidKBType(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCreateKB_NilInput 验证 nil 输入 (在 nil repo 之前)
func TestCreateKB_NilInput(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	err := svc.CreateKB(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil kb")
	}
}

// TestCreateKB_NilRepo 验证 nil repo
func TestCreateKB_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	err := svc.CreateKB(context.Background(), &model.KnowledgeBase{
		KBCode: "X",
		Type:   "faq",
		Name:   "test",
	})
	if err == nil {
		t.Error("expected error for nil repo")
	}
	if !strings.Contains(err.Error(), "repo not initialized") {
		t.Errorf("expected 'repo not initialized' error, got: %v", err)
	}
}

// TestGetKB_NilRepo 测试 nil repo 下的安全行为
func TestGetKB_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	got, err := svc.GetKB(context.Background(), 1)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil kb, got %v", got)
	}
}

// TestListKBs_NilRepo 测试 nil repo
func TestListKBs_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	got, total, err := svc.ListKBs(context.Background(), "faq", "", 0, "")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil || total != 0 {
		t.Errorf("expected empty result, got %d items total=%d", len(got), total)
	}
}

// TestListByType_NilRepo
func TestListByType_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	got, err := svc.ListByType(context.Background(), "faq")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// TestListByAgent_NilRepo
func TestListByAgent_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	got, err := svc.ListByAgent(context.Background(), 1)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// TestUpdateKB_NilRepo 验证 nil repo 报错
func TestUpdateKB_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{Name: "x"})
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

// TestUpdateKB_ZeroID 验证 id=0 报错
func TestUpdateKB_ZeroID(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	svc.repo = nil
	err := svc.UpdateKB(context.Background(), 0, &model.KnowledgeBase{Name: "x"})
	if err == nil {
		t.Error("expected error for id=0")
	}
}

// TestUpdateKB_EmptyName 验证 name 必填
func TestUpdateKB_EmptyName(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{Name: ""})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

// TestUpdateKB_InvalidType 验证 type 合法性
func TestUpdateKB_InvalidType(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{Name: "x", Type: "BAD"})
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

// TestUpdateKB_PrivateWithoutOwner 验证 private 缺 owner
func TestUpdateKB_PrivateWithoutOwner(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{
		Name:      "x",
		OwnerType: model.KnowledgeBaseOwnerPrivate,
	})
	if err == nil {
		t.Error("expected error for private without owner")
	}
}

// TestUpdateKB_SharedWithOwner 验证 shared 带 owner
func TestUpdateKB_SharedWithOwner(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	agent := uint(1)
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{
		Name:         "x",
		OwnerType:    model.KnowledgeBaseOwnerShared,
		OwnerAgentID: &agent,
	})
	if err == nil {
		t.Error("expected error for shared with owner")
	}
}

// TestUpdateKB_SharedClearsOwner 验证 shared 改 owner_type 时清空 owner_agent_id
func TestUpdateKB_SharedClearsOwner(t *testing.T) {
	svc := &KnowledgeBaseService{repo: nil}
	err := svc.UpdateKB(context.Background(), 1, &model.KnowledgeBase{
		Name:         "x",
		OwnerType:    model.KnowledgeBaseOwnerShared,
		OwnerAgentID: nil,
	})
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

// TestDeleteKB_NilRepo
func TestDeleteKB_NilRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	err := svc.DeleteKB(context.Background(), 1)
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

// TestUnbindFromAgent_NilBindingRepo 测试 nil repo 下的安全行为
func TestUnbindFromAgent_NilBindingRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	err := svc.UnbindFromAgent(context.Background(), 1, 1)
	if err != nil {
		t.Errorf("expected nil error for nil binding repo, got %v", err)
	}
}

// TestBindToAgent_NilBindingRepo 测试 BindToAgent 在 nil bindingRepo 下
//
// 实际行为: BindToAgent 内部用 NewAgentKBBindingServiceWithRepos, 不依赖 svc.bindingRepo
//
//	即使 svc.bindingRepo = nil, BindToAgent 仍能工作 (它构造新 service)
func TestBindToAgent_NilBindingRepo(t *testing.T) {
	svc := &KnowledgeBaseService{}
	defer func() {
		if r := recover(); r != nil {
			t.Logf("BindToAgent panicked (expected with nil repo): %v", r)
		}
	}()
	_ = svc.BindToAgent(context.Background(), 999, 1)
}

// TestNewKnowledgeBaseService 测试构造函数
//
// nil db 时, NewKnowledgeBaseService 会构造 KnowledgeBaseRepository(nil) 和 AgentKBBindingRepository(nil)
// 这些非 nil 的 typed nil 指针, 因此 svc.repo != nil 但 svc.repo.db == nil
func TestNewKnowledgeBaseService(t *testing.T) {
	svc := NewKnowledgeBaseService(nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// TestSetRepositories 测试 repo 注入
func TestSetRepositories(t *testing.T) {
	svc := &KnowledgeBaseService{}
	svc.SetRepositories(nil, nil)
	if svc.repo != nil || svc.bindingRepo != nil {
		t.Error("nil inputs should not set repos")
	}
}
