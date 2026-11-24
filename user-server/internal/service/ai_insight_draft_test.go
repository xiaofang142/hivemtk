package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

type mockAIInsightDraftRepo struct {
	drafts      map[string]*model.AIInsightDraft
	createErr   error
	getErr      error
	updateErr   error
	lastStatus  string
	lastReview  string
	lastApplied string
}

func newMockAIInsightDraftRepo() *mockAIInsightDraftRepo {
	return &mockAIInsightDraftRepo{drafts: map[string]*model.AIInsightDraft{}}
}

func (m *mockAIInsightDraftRepo) Create(_ context.Context, d *model.AIInsightDraft) error {
	if m.createErr != nil {
		return m.createErr
	}
	cp := *d
	m.drafts[d.DraftID] = &cp
	return nil
}
func (m *mockAIInsightDraftRepo) List(_ context.Context, _, _, _ string, _, _ int) ([]model.AIInsightDraft, int64, error) {
	out := make([]model.AIInsightDraft, 0, len(m.drafts))
	for _, d := range m.drafts {
		out = append(out, *d)
	}
	return out, int64(len(out)), nil
}
func (m *mockAIInsightDraftRepo) UpdateStatus(ctx context.Context, draftID, status string, confirmedBy *uint) error {
	return m.UpdateReview(ctx, draftID, status, confirmedBy, "", "")
}
func (m *mockAIInsightDraftRepo) UpdateReview(_ context.Context, draftID, status string, confirmedBy *uint, reviewNote, appliedActionJSON string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	d, ok := m.drafts[draftID]
	if !ok {
		return errors.New("not found")
	}
	d.Status = status
	if confirmedBy != nil {
		d.ConfirmedBy = confirmedBy
	}
	d.ReviewNote = reviewNote
	if appliedActionJSON != "" {
		d.AppliedActionJSON = json.RawMessage(appliedActionJSON)
	}
	m.lastStatus = status
	m.lastReview = reviewNote
	m.lastApplied = appliedActionJSON
	return nil
}
func (m *mockAIInsightDraftRepo) GetByDraftID(_ context.Context, draftID string) (*model.AIInsightDraft, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	d, ok := m.drafts[draftID]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *d
	return &cp, nil
}

func TestAIInsightHasEvidence(t *testing.T) {
	if hasEvidence(nil) {
		t.Fatal("nil should have no evidence")
	}
	if hasEvidence(json.RawMessage(`[]`)) {
		t.Fatal("empty array should have no evidence")
	}
	if !hasEvidence(json.RawMessage(`[{"type":"customer"}]`)) {
		t.Fatal("non-empty array should have evidence")
	}
}

func TestAIInsightCreateRejectsActionWithoutEvidence(t *testing.T) {
	repo := newMockAIInsightDraftRepo()
	svc := &AIInsightDraftService{repo: repo}
	err := svc.Create(context.Background(), &model.AIInsightDraft{
		EntityType: "customer", EntityID: "c1", InsightType: "profile", Title: "t",
		ProposedActionJSON: json.RawMessage(`{"type":"followup_suggestion","customer_id":"c1"}`),
		EvidenceJSON:       json.RawMessage(`[]`),
	})
	if err == nil {
		t.Fatal("expected error for actionable draft without evidence")
	}
}

func TestAIInsightConfirmRejectsNoEvidence(t *testing.T) {
	repo := newMockAIInsightDraftRepo()
	repo.drafts["d1"] = &model.AIInsightDraft{DraftID: "d1", Status: "draft", EvidenceJSON: json.RawMessage(`[]`), ProposedActionJSON: json.RawMessage(`{}`)}
	svc := &AIInsightDraftService{repo: repo}
	_, err := svc.Confirm(context.Background(), "d1", 1, "ok")
	if err == nil {
		t.Fatal("expected no-evidence confirm error")
	}
}

func TestAIInsightRejectUpdatesStatus(t *testing.T) {
	repo := newMockAIInsightDraftRepo()
	repo.drafts["d2"] = &model.AIInsightDraft{DraftID: "d2", Status: "draft", EvidenceJSON: json.RawMessage(`[{"type":"customer"}]`)}
	svc := &AIInsightDraftService{repo: repo}
	if err := svc.Reject(context.Background(), "d2", 2, "bad"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if repo.lastStatus != "rejected" || repo.lastReview != "bad" {
		t.Fatalf("unexpected review update: status=%s review=%s", repo.lastStatus, repo.lastReview)
	}
}

func TestAIInsightSanitizeHidesActionWithoutEvidence(t *testing.T) {
	svc := &AIInsightDraftService{repo: newMockAIInsightDraftRepo()}
	d := &model.AIInsightDraft{
		EvidenceJSON:       json.RawMessage(`[]`),
		ProposedActionJSON: json.RawMessage(`{"type":"followup_suggestion","customer_id":"c1"}`),
	}
	out := svc.sanitizeForDisplay(d)
	if string(out.ProposedActionJSON) != "{}" {
		t.Fatalf("expected proposed_action hidden, got %s", string(out.ProposedActionJSON))
	}
}
