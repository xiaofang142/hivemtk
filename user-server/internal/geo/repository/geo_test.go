package repository

import (
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupGeoTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.GeoKeyword{},
		&model.GeoKeywordGroup{},
		&model.GeoArticle{},
		&model.GeoOptimization{},
		&model.GeoVerifyResult{},
		&model.GeoAPICall{},
		&model.GeoConfig{},
		&model.GeoPlatformAccount{},
		&model.GeoPublishRecord{},
		&model.GeoKnowledgeDocument{},
		&model.GeoWorkflow{},
		&model.GeoWorkflowExecution{},
		&model.GeoWorkflowTemplate{},
	)
}

func TestGeoKeywordRepository(t *testing.T) {
	db := setupGeoTestDB(t)
	repo := NewGeoKeywordRepositoryWithDB(db)

	kw := &model.GeoKeyword{Source: "ai", Keyword: "GEO优化", SearchVolume: 1000, Difficulty: 5.5}
	if err := repo.Create(kw); err != nil {
		t.Fatalf("Create keyword failed: %v", err)
	}
	if kw.ID == "" {
		t.Fatal("keyword ID should be auto-generated")
	}

	list, total, err := repo.GetList("", "", "", "", "", "", 1, 10)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 keyword, got %d", total)
	}
	if len(list) == 0 {
		t.Fatal("expected non-empty list")
	}

	if err := repo.Delete(kw.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, total, _ = repo.GetList("", "", "", "", "", "", 1, 10)
	if total != 0 {
		t.Fatalf("expected 0 after delete, got %d", total)
	}
}

func TestGeoArticleRepository(t *testing.T) {
	db := setupGeoTestDB(t)
	repo := NewGeoArticleRepositoryWithDB(db)

	article := &model.GeoArticle{Title: "GEO测试文章", Content: "测试内容", Keyword: "GEO", Status: "draft"}
	if err := repo.Create(article); err != nil {
		t.Fatalf("Create article failed: %v", err)
	}

	got, err := repo.GetByID(article.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Title != "GEO测试文章" {
		t.Fatalf("expected title 'GEO测试文章', got '%s'", got.Title)
	}

	_, total, err := repo.GetList("", "", 1, 10)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 article, got %d", total)
	}
}

func TestGeoPlatformAccountRepository(t *testing.T) {
	db := setupGeoTestDB(t)
	repo := NewGeoPlatformAccountRepositoryWithDB(db)

	account := &model.GeoPlatformAccount{Platform: "github_readme", AccountName: "test-user", Status: "active"}
	if err := repo.Create(account); err != nil {
		t.Fatalf("Create account failed: %v", err)
	}

	_, total, err := repo.GetList("github_readme", 1, 10)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 account, got %d", total)
	}

	if err := repo.Delete(account.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestGeoKnowledgeDocumentRepository(t *testing.T) {
	db := setupGeoTestDB(t)
	repo := NewGeoKnowledgeDocumentRepositoryWithDB(db)

	doc := &model.GeoKnowledgeDocument{Title: "品牌FAQ", Content: "Q:什么是GEO？A:生成式引擎优化", DocType: "faq"}
	if err := repo.Create(doc); err != nil {
		t.Fatalf("Create doc failed: %v", err)
	}

	list, err := repo.GetList()
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("expected at least 1 doc, got %d", len(list))
	}

	if err := repo.Delete(doc.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestGeoWorkflowRepository(t *testing.T) {
	db := setupGeoTestDB(t)
	repo := NewGeoWorkflowRepositoryWithDB(db)

	wf := &model.GeoWorkflow{Name: "内容生产工作流", Enabled: true}
	wf.SetSteps([]map[string]interface{}{{"name": "step1", "type": "content_generate"}})
	if err := repo.Create(wf); err != nil {
		t.Fatalf("Create workflow failed: %v", err)
	}

	got, err := repo.GetByID(wf.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "内容生产工作流" {
		t.Fatalf("expected name '内容生产工作流', got '%s'", got.Name)
	}

	list, err := repo.GetList()
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("expected at least 1 workflow, got %d", len(list))
	}
}
