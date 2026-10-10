package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
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

func TestKnowledgeDocumentRepository_AgentIsolation_ListShared(t *testing.T) {
	repo, _, done := setupDocRepoWithTX(t)
	defer done()
	ctx := context.Background()

	agentA := uint(10)
	docs := []*model.KnowledgeDocument{
		{Title: "shared1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "shared2", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: nil},
		{Title: "agentA1", SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusIndexed, Status: 1, AgentID: &agentA},
	}
	for _, d := range docs {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ListShared(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("ListShared expected 2, got %d", len(got))
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
