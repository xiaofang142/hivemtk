package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestMain 统一本测试二进制的环境前提：
// 固定字段加密密钥，使凭据加解密行为与测试执行顺序/子集无关
// （crypto 包 sync.Once 进程级缓存，必须在首次 Encrypt 前生效）。
func TestMain(m *testing.M) {
	os.Setenv("FIELD_ENCRYPTION_KEY", "geo-test-encryption-key-32-bytes-min!")
	os.Exit(m.Run())
}

func geoTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.GeoKeyword{},
		&model.GeoArticle{},
		&model.GeoPlatformAccount{},
		&model.GeoPublishRecord{},
		&model.GeoKnowledgeDocument{},
		&model.GeoWorkflow{},
		&model.GeoWorkflowExecution{},
		&model.GeoWorkflowTemplate{},
	)
}

func TestKBService(t *testing.T) {
	db := geoTestDB(t)
	repo := newGeoKBRepo(db)
	svc := NewKBService(repo, nil)

	ctx := context.Background()

	doc, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
		Title:   "品牌介绍",
		Content: "我们是一家专注于GEO优化的公司",
		DocType: "reference",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if doc.ID == "" {
		t.Fatal("doc ID should be generated")
	}

	results, err := svc.Search(ctx, "GEO", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(results))
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("expected at least 1 doc, got %d", len(list))
	}

	if err := svc.Delete(ctx, doc.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestPlatformService_ListPlatforms(t *testing.T) {
	db := geoTestDB(t)
	svc := NewPlatformService(newGeoAccountRepo(db), newGeoRecordRepo(db), newGeoArticleRepo(db))

	platforms := svc.ListPlatforms(context.Background())
	if len(platforms) == 0 {
		t.Fatal("expected non-empty platform list")
	}

	found := false
	for _, p := range platforms {
		if p.Name == "github_readme" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected github_readme in platform list")
	}
}

func TestPlatformService_SaveAccount(t *testing.T) {
	ctx := context.Background()
	db := geoTestDB(t)

	svc := NewPlatformService(newGeoAccountRepo(db), newGeoRecordRepo(db), newGeoArticleRepo(db))

	account, err := svc.SaveAccount(context.Background(), &dto.SavePlatformAccountRequest{
		Platform:    "github_readme",
		AccountName: "testuser",
		Credentials: map[string]string{"access_token": "ghp_test123"},
	})
	if err != nil {
		t.Fatalf("SaveAccount failed: %v", err)
	}
	if account.ID == "" {
		t.Fatal("account ID should be generated")
	}

	list, total, err := svc.ListAccounts(context.Background(), "github_readme", 1, 10)
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 account, got %d", total)
	}
	if len(list) == 0 || list[0].AccountName != "testuser" {
		t.Fatal("expected account with name 'testuser'")
	}
	if !list[0].HasCredentials {
		t.Fatal("expected has_credentials=true")
	}
	if strings.Contains(list[0].AccountID, "ghp_") {
		t.Fatal("credentials must never leak into response")
	}

	stored, err := newGeoAccountRepo(db).GetByPlatformAndName(ctx, "github_readme", "testuser")
	if err != nil {
		t.Fatalf("GetByPlatformAndName failed: %v", err)
	}
	if strings.Contains(stored.Config, "ghp_test123") {
		t.Fatal("stored credentials must be encrypted at rest")
	}
}

func TestPlatformService_SaveAccount_DefaultsAccountName(t *testing.T) {
	db := geoTestDB(t)
	svc := NewPlatformService(newGeoAccountRepo(db), newGeoRecordRepo(db), newGeoArticleRepo(db))

	account, err := svc.SaveAccount(context.Background(), &dto.SavePlatformAccountRequest{
		Platform:    "webhook",
		Credentials: map[string]string{"url": "https://example.com/hook"},
	})
	if err != nil {
		t.Fatalf("SaveAccount without account_name should succeed: %v", err)
	}
	if account.AccountName != "webhook" {
		t.Fatalf("AccountName = %q, want platform fallback", account.AccountName)
	}
}

func TestEstimateCostUSD(t *testing.T) {

	usd, cny := EstimateCostUSD(context.Background(), "gpt-4o", 1_000_000, 500_000)
	if usd != 2.5+5.0 {
		t.Fatalf("expected gpt-4o cost 7.5 USD, got %v", usd)
	}
	if cny != usd*usdCnyRate {
		t.Fatalf("CNY should be USD * rate, got %v vs %v", cny, usd)
	}

	usd2, _ := EstimateCostUSD(context.Background(), "gpt-4o-2024-08-06", 1_000_000, 0)
	if usd2 != 2.5 {
		t.Fatalf("expected versioned gpt-4o input cost 2.5 USD, got %v", usd2)
	}

	usdMini, _ := EstimateCostUSD(context.Background(), "gpt-4o-mini", 1_000_000, 0)
	if usdMini != 0.15 {
		t.Fatalf("expected gpt-4o-mini input cost 0.15 USD, got %v", usdMini)
	}

	usd3, _ := EstimateCostUSD(context.Background(), "unknown-model-x", 1_000_000, 0)
	if usd3 != fallbackPriceIn {
		t.Fatalf("expected fallback price %v, got %v", fallbackPriceIn, usd3)
	}

	if usd4, _ := EstimateCostUSD(context.Background(), "gpt-4o", 0, 0); usd4 != 0 {
		t.Fatalf("expected zero cost for zero tokens, got %v", usd4)
	}
}

func TestTechConfigService(t *testing.T) {
	svc := NewTechConfigService()

	robots := svc.GenerateRobots(&RobotsConfig{
		SiteURL:  "https://example.com",
		Disallow: []string{"/admin", "/api"},
		Allow:    []string{"/public"},
	})
	if !strings.Contains(robots, "Disallow: /admin") {
		t.Fatal("expected Disallow: /admin in robots.txt")
	}
	if !strings.Contains(robots, "Sitemap:") {
		t.Fatal("expected Sitemap directive")
	}

	sitemap := svc.GenerateSitemap(&SitemapConfig{
		SiteURL: "https://example.com",
		URLs:    []string{"/", "/about", "/blog"},
	})
	if !strings.Contains(sitemap, "https://example.com/") {
		t.Fatal("expected site URL in sitemap")
	}
	if !strings.Contains(sitemap, "<urlset") {
		t.Fatal("expected urlset tag")
	}
}

func TestMetricsService(t *testing.T) {
	svc := NewMetricsService()

	content := `根据2024年报告显示，GEO优化能提升品牌曝光率30%。
例如，某品牌使用GEO后，搜索量增长了2倍。
我们的解决方案已被100个客户采用，满意度达到95%。`

	result := svc.Analyze(content, "GEO", "我们的品牌")
	if result.TrustSignals == 0 {
		t.Fatal("expected trust signals in content")
	}
	if result.AuthorityScore == 0 {
		t.Fatal("expected non-zero authority score")
	}
	if result.Structure.Headings == 0 && result.TextLength == 0 {
		t.Fatal("expected non-zero metrics")
	}
}

func TestWorkflowService(t *testing.T) {
	db := geoTestDB(t)
	llmFactory := NewLLMAdapter()
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepository(db),
		repository.NewGeoContentTaskRepository(db),
		llmFactory,
	)

	ctx := context.Background()

	wf, err := svc.Create(ctx, &dto.SaveWorkflowRequest{
		Name: "测试工作流",
		Steps: []dto.WorkflowStep{
			{Name: "step1", Type: "content_generate", Params: map[string]interface{}{"topic": "GEO", "brand": "test"}},
		},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create workflow failed: %v", err)
	}
	if wf.ID == "" {
		t.Fatal("workflow ID should be generated")
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("expected at least 1 workflow, got %d", len(list))
	}

	got, err := svc.Get(ctx, wf.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "测试工作流" {
		t.Fatalf("expected name '测试工作流', got '%s'", got.Name)
	}

	result, err := svc.Run(ctx, wf.ID)
	if err != nil && result == nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result != nil && result.Status == "" {
		t.Fatal("expected non-empty run status")
	}
}

func newGeoKBRepo(db *gorm.DB) repository.GeoKnowledgeDocumentRepository {
	return repository.NewGeoKnowledgeDocumentRepositoryWithDB(db)
}

func newGeoAccountRepo(db *gorm.DB) repository.GeoPlatformAccountRepository {
	return repository.NewGeoPlatformAccountRepositoryWithDB(db)
}

func newGeoRecordRepo(db *gorm.DB) repository.GeoPublishRecordRepository {
	return repository.NewGeoPublishRecordRepositoryWithDB(db)
}

func newGeoArticleRepo(db *gorm.DB) repository.GeoArticleRepository {
	return repository.NewGeoArticleRepositoryWithDB(db)
}

func newGeoWFRepo(db *gorm.DB) repository.GeoWorkflowRepository {
	return repository.NewGeoWorkflowRepositoryWithDB(db)
}

func newGeoExecRepo(db *gorm.DB) repository.GeoWorkflowExecutionRepository {
	return repository.NewGeoWorkflowExecutionRepositoryWithDB(db)
}

func newGeoTplRepo(db *gorm.DB) repository.GeoWorkflowTemplateRepository {
	return repository.NewGeoWorkflowTemplateRepositoryWithDB(db)
}

// 公开 FAQ 只能检索 is_public 文档；内部文档默认不可见。
func TestKBPublicBoundary(t *testing.T) {
	db := geoTestDB(t)
	repo := newGeoKBRepo(db)
	svc := NewKBService(repo, nil)
	ctx := context.Background()

	privateDoc, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
		Title: "内部报价", Content: "内部底价 8 折，禁止外传", DocType: "reference",
	})
	if err != nil {
		t.Fatalf("Save private: %v", err)
	}
	publicDoc, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
		Title: "产品介绍", Content: "HiveMtk 是开源私域营销系统", DocType: "reference",
		IsPublic: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Save public: %v", err)
	}

	if res, err := svc.SearchPublic(ctx, "内部", 10); err != nil || len(res) != 0 {
		t.Fatalf("公开检索不应命中内部文档: n=%d err=%v", len(res), err)
	}
	if res, err := svc.SearchPublic(ctx, "HiveMtk", 10); err != nil || len(res) != 1 {
		t.Fatalf("公开检索应命中公开文档: n=%d err=%v", len(res), err)
	}
	// 内部检索仍能看到全部
	if res, err := svc.Search(ctx, "内部", 10); err != nil || len(res) != 1 {
		t.Fatalf("内部检索应命中私有文档: n=%d err=%v", len(res), err)
	}

	// 关闭公开后立即从公开检索消失
	if _, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
		ID: publicDoc.ID, Title: publicDoc.Title, Content: publicDoc.Content, IsPublic: boolPtr(false),
	}); err != nil {
		t.Fatalf("关闭公开失败: %v", err)
	}
	if res, _ := svc.SearchPublic(ctx, "HiveMtk", 10); len(res) != 0 {
		t.Fatalf("关闭公开后仍可检索到: n=%d", len(res))
	}
	_ = privateDoc
}

func boolPtr(v bool) *bool { return &v }
