package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// crmAIDraftStore 是 CRM-14 草稿仓储的最小替身：按草稿 ID 存快照，记录写回次数。
type crmAIDraftStore struct {
	drafts map[string]*model.AIInsightDraft
	writes map[string]int
}

func newCRMAIDraftStore() *crmAIDraftStore {
	return &crmAIDraftStore{drafts: map[string]*model.AIInsightDraft{}, writes: map[string]int{}}
}

func (s *crmAIDraftStore) Create(_ context.Context, d *model.AIInsightDraft) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	d.UpdatedAt = d.CreatedAt
	cp := *d
	s.drafts[d.DraftID] = &cp
	return nil
}
func (s *crmAIDraftStore) List(_ context.Context, _, _, _ string, _, _ int) ([]model.AIInsightDraft, int64, error) {
	out := make([]model.AIInsightDraft, 0, len(s.drafts))
	for _, d := range s.drafts {
		out = append(out, *d)
	}
	return out, int64(len(out)), nil
}
func (s *crmAIDraftStore) GetByDraftID(_ context.Context, id string) (*model.AIInsightDraft, error) {
	d, ok := s.drafts[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *d
	return &cp, nil
}
func (s *crmAIDraftStore) UpdateStatus(ctx context.Context, id, status string, by *uint) error {
	return s.UpdateReview(ctx, id, status, by, "", "")
}
func (s *crmAIDraftStore) UpdateReview(_ context.Context, id, status string, by *uint, note, applied string) error {
	d, ok := s.drafts[id]
	if !ok {
		return errors.New("not found")
	}
	d.Status = status
	if by != nil {
		d.ConfirmedBy = by
	}
	d.ReviewNote = note
	d.UpdatedAt = d.UpdatedAt.Add(time.Second)
	if applied != "" {
		d.AppliedActionJSON = json.RawMessage(applied)
	}
	s.writes[id]++
	return nil
}

type crmAIDraftPermission struct {
	customers map[string]*model.Customer
}

func (p *crmAIDraftPermission) GetByIDScoped(_ context.Context, scope CustomerDataScope, id string) (*model.Customer, error) {
	c, ok := p.customers[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	if !(&CustomerPermissionService{}).CanAccess(scope, c) {
		return nil, ErrCRMCustomerForbidden
	}
	return c, nil
}

func crmAISuggestionResult() *CustomerAIResult {
	return &CustomerAIResult{
		CustomerID: "c-1", PromptVersion: customerAIPromptVersion,
		Suggestions: []AISuggestion{{
			Kind: "opportunity", Content: "建议追加报价", Confidence: 0.7,
			ConfidenceSource: "model_estimate", RuleScore: 1, PromptVersion: customerAIPromptVersion,
			Evidence: []AIEvidence{{Type: "customer_field", Ref: "tag", Snippet: "高意向", Source: "customers", OccurredAt: time.Now().UTC().Format(time.RFC3339)}},
		}},
	}
}

// TestCRM14_CreateDraftFromAnalysis 画像建议必须落成带证据/版本的草稿；无证据结论不得生成可应用动作。
func TestCRM14_CreateDraftFromAnalysis(t *testing.T) {
	store := newCRMAIDraftStore()
	svc := &CRMAISuggestionDraftService{drafts: store, permission: &crmAIDraftPermission{customers: map[string]*model.Customer{
		"c-1": {ID: "c-1", SourceAccountID: "acc-1", Industry: "电子", OwnerUserID: crmAIUintPtr(7)},
	}}}
	res := crmAISuggestionResult()
	draft, err := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, Result: res})
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	if draft.Status != "draft" || draft.Confidence != 0.7 {
		t.Fatalf("草稿应为未确认且保留模型分数：%+v", draft)
	}
	if !strings.Contains(string(draft.EvidenceJSON), "customers") || !strings.Contains(string(draft.ContentJSON), customerAIPromptVersion) {
		t.Fatalf("草稿缺证据/版本快照：evidence=%s content=%s", draft.EvidenceJSON, draft.ContentJSON)
	}
	bad := crmAISuggestionResult()
	bad.Suggestions[0].Evidence = nil
	if _, err := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Result: bad}); err == nil {
		t.Fatal("无证据结论不得生成可应用草稿")
	}
}

// TestCRM14_ConfirmRequiresScopeAndVersion 确认时重验权限与对象版本；越权/版本漂移不得写业务。
func TestCRM14_ConfirmRequiresScopeAndVersion(t *testing.T) {
	store := newCRMAIDraftStore()
	svc := &CRMAISuggestionDraftService{drafts: store, permission: &crmAIDraftPermission{customers: map[string]*model.Customer{
		"c-1": {ID: "c-1", SourceAccountID: "acc-1", Industry: "电子", OwnerUserID: crmAIUintPtr(7)},
	}}}
	draft, err := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, Result: crmAISuggestionResult()})
	if err != nil {
		t.Fatalf("生成草稿失败: %v", err)
	}
	svc.apply = func(context.Context, uint, *model.AIInsightDraft) (map[string]any, error) {
		t.Fatal("越权确认不得写业务")
		return nil, nil
	}
	if _, err := svc.Confirm(context.Background(), draft.DraftID, 7, CRMAIDraftConfirmInput{
		Scope: CustomerDataScope{UserID: 8, RestrictedAccounts: true, AccountIDs: []string{"acc-2"}}, ExpectedVersion: draft.UpdatedAt.UTC().Format(time.RFC3339),
	}); err == nil {
		t.Fatal("其他员工确认应被拒绝")
	}
	wrongVersion := "stale"
	if _, err := svc.Confirm(context.Background(), draft.DraftID, 7, CRMAIDraftConfirmInput{
		Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, ExpectedVersion: wrongVersion,
	}); err == nil {
		t.Fatal("版本漂移确认应被拒绝")
	}
}

// TestCRM14_RejectDoesNotWriteBusiness 拒绝只改草稿状态，不触发任何业务写入。
func TestCRM14_RejectDoesNotWriteBusiness(t *testing.T) {
	store := newCRMAIDraftStore()
	svc := &CRMAISuggestionDraftService{drafts: store, permission: &crmAIDraftPermission{customers: map[string]*model.Customer{
		"c-1": {ID: "c-1", SourceAccountID: "acc-1", Industry: "电子", OwnerUserID: crmAIUintPtr(7)},
	}}}
	applyCalls := 0
	svc.apply = func(context.Context, uint, *model.AIInsightDraft) (map[string]any, error) {
		applyCalls++
		return map[string]any{"applied": true}, nil
	}
	draft, _ := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, Result: crmAISuggestionResult()})
	if err := svc.Reject(context.Background(), draft.DraftID, 7, "不合适"); err != nil {
		t.Fatalf("拒绝失败: %v", err)
	}
	if applyCalls != 0 {
		t.Fatalf("拒绝不得写业务，实际调用 %d 次", applyCalls)
	}
	if store.drafts[draft.DraftID].Status != "rejected" {
		t.Fatalf("拒绝状态未落库：%s", store.drafts[draft.DraftID].Status)
	}
}

// TestCRM14_ConfirmIdempotentAndAuditVisible 重复确认不重复建单；审计写失败必须显式可见。
func TestCRM14_ConfirmIdempotentAndAuditVisible(t *testing.T) {
	store := newCRMAIDraftStore()
	svc := &CRMAISuggestionDraftService{drafts: store, permission: &crmAIDraftPermission{customers: map[string]*model.Customer{
		"c-1": {ID: "c-1", SourceAccountID: "acc-1", Industry: "电子", OwnerUserID: crmAIUintPtr(7)},
	}}}
	applyCalls := 0
	svc.apply = func(_ context.Context, _ uint, _ *model.AIInsightDraft) (map[string]any, error) {
		applyCalls++
		return map[string]any{"applied": true, "type": "followup_suggestion"}, nil
	}
	svc.audit = func(context.Context, string, string, any) error { return nil }
	draft, _ := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, Result: crmAISuggestionResult()})
	in := CRMAIDraftConfirmInput{Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, ExpectedVersion: draft.UpdatedAt.UTC().Format(time.RFC3339)}
	first, err := svc.Confirm(context.Background(), draft.DraftID, 7, in)
	if err != nil || !first.Applied {
		t.Fatalf("首次确认应落地：%+v err=%v", first, err)
	}
	second, err := svc.Confirm(context.Background(), draft.DraftID, 7, in)
	if err != nil || second.Applied || applyCalls != 1 {
		t.Fatalf("重复确认不得重复建单：second=%+v applyCalls=%d err=%v", second, applyCalls, err)
	}
	if store.writes[draft.DraftID] != 1 || second.AppliedAction["type"] != "followup_suggestion" {
		t.Fatalf("重复确认只回读既有结果，不得重写状态：writes=%d result=%+v", store.writes[draft.DraftID], second)
	}
	revoked := in
	revoked.Scope.AccountIDs = []string{"acc-2"}
	if result, err := svc.Confirm(context.Background(), draft.DraftID, 7, revoked); err == nil || result != nil {
		t.Fatalf("权限撤销后不得回读已确认结果：result=%+v err=%v", result, err)
	}
	svc.audit = func(context.Context, string, string, any) error { return errors.New("audit down") }
	draft2, _ := svc.CreateFromAnalysis(context.Background(), CRMAIDraftInput{CustomerID: "c-1", Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, Result: crmAISuggestionResult()})
	res, err := svc.Confirm(context.Background(), draft2.DraftID, 7, CRMAIDraftConfirmInput{
		Scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acc-1"}}, ExpectedVersion: draft2.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err == nil || res == nil || !strings.Contains(err.Error(), "审计") {
		t.Fatalf("审计写失败必须显式返回，实际 res=%+v err=%v", res, err)
	}
}

func crmAIUintPtr(v uint) *uint { return &v }
