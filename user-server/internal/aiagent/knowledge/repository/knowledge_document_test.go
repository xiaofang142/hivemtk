package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupDocTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KnowledgeDocument{})
	if err := db.Exec("TRUNCATE TABLE knowledge_documents RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate knowledge_documents: %v", err)
	}
	return db
}

func setupDocRepoWithTX(t *testing.T) (repo *KnowledgeDocumentRepository, tx *gorm.DB, cleanup func()) {
	t.Helper()
	db := setupDocTestDB(t)
	tx = db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	repo = NewKnowledgeDocumentRepository(tx)
	cleanup = func() { tx.Rollback() }
	return
}

func TestKnowledgeDocumentRepository_AgentIsolation_ListByAgent(t *testing.T) {
	repo, _, done := setupDocRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	agentB := uint(20)
	docs := []*model.KnowledgeDocument{
		{Title: "shared_doc1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "shared_doc2", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "agentA_doc1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
		{Title: "agentA_doc2", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
		{Title: "agentB_doc1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentB},
	}
	for _, d := range docs {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ListByAgent(ctx, agentA, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("ListByAgent(agentA) expected 2, got %d", len(got))
	}

	got, err = repo.ListByAgent(ctx, agentB, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("ListByAgent(agentB) expected 1, got %d", len(got))
	}

	got, err = repo.ListByAgent(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("ListByAgent(0) expected nil, got %d", len(got))
	}
}

func TestKnowledgeDocumentRepository_ReindexCursorIsStableAndFrozen(t *testing.T) {
	db := setupDocTestDB(t)
	repo := NewKnowledgeDocumentRepository(db)
	ctx := context.Background()
	productID := "fa13-cursor-stability"
	createdAt := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	docs := make([]model.KnowledgeDocument, 4)
	for i := range docs {
		docs[i] = model.KnowledgeDocument{
			ProductID:   productID,
			Title:       "cursor doc",
			SourceType:  model.SourceTypeUpload,
			EmbedStatus: model.EmbedStatusPending,
			Status:      1,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		}
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatalf("create cursor docs: %v", err)
	}

	boundary, err := repo.GetReindexBoundary(ctx, productID)
	if err != nil {
		t.Fatalf("get reindex boundary: %v", err)
	}
	if boundary == nil || !boundary.CreatedAt.Equal(createdAt) || boundary.ID != docs[3].ID {
		t.Fatalf("boundary should use timestamp and highest ID: got %+v, want time=%s id=%d", boundary, createdAt, docs[3].ID)
	}

	newer := model.KnowledgeDocument{
		ProductID:   productID,
		Title:       "added after boundary",
		SourceType:  model.SourceTypeUpload,
		EmbedStatus: model.EmbedStatusPending,
		Status:      1,
		CreatedAt:   createdAt.Add(time.Microsecond),
		UpdatedAt:   createdAt.Add(time.Microsecond),
	}
	if err := db.Create(&newer).Error; err != nil {
		t.Fatalf("create post-boundary doc: %v", err)
	}
	backdated := newer
	backdated.ID = 0
	backdated.CreatedAt = createdAt.Add(-time.Hour)
	if err := db.Create(&backdated).Error; err != nil {
		t.Fatalf("create backdated post-boundary doc: %v", err)
	}

	var gotIDs []uint64
	var after *ReindexCursor
	for {
		page, pageErr := repo.ListReindexPage(ctx, productID, *boundary, after, 2)
		if pageErr != nil {
			t.Fatalf("list reindex page: %v", pageErr)
		}
		for _, doc := range page {
			gotIDs = append(gotIDs, doc.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		after = &ReindexCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	if len(gotIDs) != len(docs) {
		t.Fatalf("frozen cursor should return only initial docs: got %d IDs %v", len(gotIDs), gotIDs)
	}
	for i := range docs {
		want := docs[len(docs)-1-i].ID
		if gotIDs[i] != want {
			t.Fatalf("stable order at position %d: got %d, want %d (all timestamps equal)", i, gotIDs[i], want)
		}
	}
}

func TestKnowledgeDocumentRepository_ReindexCursorTenantIsolation(t *testing.T) {
	db := setupDocTestDB(t)
	repo := NewKnowledgeDocumentRepository(db)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	for _, ctx := range []context.Context{ctxA, ctxB} {
		if err := repo.Create(ctx, &model.KnowledgeDocument{ProductID: "shared-product", Title: "tenant doc"}); err != nil {
			t.Fatal(err)
		}
	}
	boundary, err := repo.GetReindexBoundary(ctxA, "shared-product")
	if err != nil || boundary == nil {
		t.Fatalf("tenant boundary = %+v, err = %v", boundary, err)
	}
	page, err := repo.ListReindexPage(ctxA, "shared-product", *boundary, nil, 10)
	if err != nil || len(page) != 1 || page[0].TenantID != 41 {
		t.Fatalf("tenant page = %+v, err = %v", page, err)
	}
	if _, err := repo.GetReindexBoundary(context.Background(), "shared-product"); err == nil {
		t.Fatal("boundary without tenant must fail closed")
	}
	if _, err := repo.ListReindexPage(context.Background(), "shared-product", *boundary, nil, 10); err == nil {
		t.Fatal("page without tenant must fail closed")
	}
	docID := page[0].ID
	if err := repo.BeginReindex(ctxB, docID); err == nil {
		t.Fatal("another tenant must not increment attempts")
	}
	if err := repo.CompleteReindex(ctxB, docID, 10, 20); err == nil {
		t.Fatal("another tenant must not finalize reindex")
	}
	if err := repo.BeginReindex(context.Background(), docID); err == nil {
		t.Fatal("attempt write without tenant must fail closed")
	}
	if err := repo.CompleteReindex(context.Background(), docID, 10, 20); err == nil {
		t.Fatal("finalization without tenant must fail closed")
	}
	for i := 0; i < 2; i++ {
		if err := repo.BeginReindex(ctxA, docID); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CompleteReindex(ctxA, docID, 2, 8); err != nil {
		t.Fatal(err)
	}
	doc, err := repo.GetByID(ctxA, docID)
	if err != nil || doc.ReindexAttempts != 2 || doc.EmbedStatus != model.EmbedStatusIndexed || doc.ChunkCount != 2 {
		t.Fatalf("reindex writes = %+v, err = %v", doc, err)
	}
	otherBoundary, err := repo.GetReindexBoundary(ctxB, "shared-product")
	if err != nil || otherBoundary == nil {
		t.Fatalf("other tenant boundary = %+v, err = %v", otherBoundary, err)
	}
	other, err := repo.GetByID(ctxB, otherBoundary.ID)
	if err != nil || other.ReindexAttempts != 0 || other.EmbedStatus == model.EmbedStatusIndexed {
		t.Fatalf("other tenant changed = %+v, err = %v", other, err)
	}
}

func TestKnowledgeDocumentRepository_AgentIsolation_MatchByAgent(t *testing.T) {
	repo, _, done := setupDocRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	agentB := uint(20)
	docs := []*model.KnowledgeDocument{
		{Title: "shared", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "agentA1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
		{Title: "agentB1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentB},
	}
	for _, d := range docs {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.MatchByAgent(ctx, agentA, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("MatchByAgent(agentA) expected 1, got %d", len(got))
	}
	if got[0].AgentID == nil || *got[0].AgentID != agentA {
		t.Errorf("expected agentA's own doc, got %v", got[0].AgentID)
	}

	got, err = repo.MatchByAgent(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("MatchByAgent(0) expected nil, got %d", len(got))
	}
}

func TestKnowledgeDocumentRepository_AgentIsolation_ListWithFilter(t *testing.T) {
	repo, _, done := setupDocRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	docs := []*model.KnowledgeDocument{
		{Title: "shared", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "agentA1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
		{Title: "agentA2", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
	}
	for _, d := range docs {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	agentZero := uint(0)
	got, _, err := repo.List(ctx, ListFilter{AgentID: &agentZero, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("List AgentID=&0 expected 1 (shared), got %d", len(got))
	}

	got, _, err = repo.List(ctx, ListFilter{AgentID: &agentA, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("List AgentID=&10 expected 2, got %d", len(got))
	}

	got, _, err = repo.List(ctx, ListFilter{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("List no AgentID expected 3, got %d", len(got))
	}
}
