package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type reindexControllerFixture struct {
	router    http.Handler
	db        *gorm.DB
	productID string
	ctx       context.Context
}

func setupReindexControllerFixture(t *testing.T, withFailingDocument bool) *reindexControllerFixture {
	t.Helper()
	dsn := os.Getenv("FA12_PG_DSN")
	if dsn == "" {
		t.Skip("FA12_PG_DSN 未设置：仅使用明确授权的合成 PostgreSQL")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Host != "/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x" || cfg.Port != 55412 || cfg.User != "fa12_runner" || cfg.Database != "fa12_synthetic" {
		t.Fatal("拒绝连接非授权合成 PostgreSQL")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	basePool, err := base.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = basePool.Close() })
	schema := fmt.Sprintf("fa13_controller_%d", time.Now().UnixNano())
	if err := base.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Errorf("清理本测试 schema 失败: %v", err)
		}
	})
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.RagProduct{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{}); err != nil {
		t.Fatal(err)
	}
	if err := tenancyidx.InitTenantRegistry(db); err != nil {
		t.Fatal(err)
	}
	tenancyidx.RegisterTenantCallbacks(db)
	oldMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.SetMode(oldMode)
		tenancyidx.ResetRegistryForTest()
	})
	ctx := tenant.WithTenantID(context.Background(), 413)
	productID := "fa13-controller-product"
	product := model.RagProduct{TenantID: 413, ID: productID, Name: "FA-13 controller synthetic", IsActive: true}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	if withFailingDocument {
		doc := model.KnowledgeDocument{
			TenantID: 413, ProductID: productID, Title: "missing-source", FilePath: filepath.Join(t.TempDir(), "missing.txt"),
			SourceType: knowledgemodel.SourceTypeUpload, EmbedStatus: knowledgemodel.EmbedStatusPending, Metadata: "{}",
		}
		if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
			t.Fatalf("创建失败文档夹具失败: %v", err)
		}
	}

	svc := knowledgesvc.NewKnowledgeServiceWithDeps(knowledgesvc.KnowledgeServiceDeps{
		RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	ctrl := &KnowledgeWorkspaceController{kbService: svc}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/products/:product_id/rebuild-index", ctrl.RebuildProductIndex)
	return &reindexControllerFixture{router: router, db: db, productID: productID, ctx: ctx}
}

// 产品重建遇到单篇失败时 controller 必须返回错误，而非 task-started/success。
func TestRebuildProductIndex_ReturnsFailureWhenDocumentReindexFails(t *testing.T) {
	fixture := setupReindexControllerFixture(t, true)
	req := httptest.NewRequest(http.MethodPost, "/products/"+fixture.productID+"/rebuild-index", nil).
		WithContext(fixture.ctx)
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("文档重建失败时 HTTP 应为 500，实际=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析错误响应失败: %v; body=%s", err, recorder.Body.String())
	}
	if fmt.Sprint(response.Code) == "0" || strings.Contains(response.Message, "重建完成") {
		t.Fatalf("失败响应不可呈现为成功，code=%v message=%q", response.Code, response.Message)
	}
	var doc model.KnowledgeDocument
	if err := fixture.db.WithContext(fixture.ctx).Where("product_id = ?", fixture.productID).First(&doc).Error; err != nil {
		t.Fatalf("查询失败文档结果失败: %v", err)
	}
	if doc.EmbedStatus != knowledgemodel.EmbedStatusFailed || doc.ReindexRetryCount != 1 {
		t.Fatalf("失败响应应对应可查询 failed/1 终态，实际=%s/%d", doc.EmbedStatus, doc.ReindexRetryCount)
	}
}

// 空产品全部处理完成后 controller 才能返回 200 和完成文案。
func TestRebuildProductIndex_ReturnsSuccessAfterCompletion(t *testing.T) {
	fixture := setupReindexControllerFixture(t, false)
	req := httptest.NewRequest(http.MethodPost, "/products/"+fixture.productID+"/rebuild-index", nil).
		WithContext(fixture.ctx)
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("全部文档处理完成后 HTTP 应为 200，实际=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析成功响应失败: %v; body=%s", err, recorder.Body.String())
	}
	if fmt.Sprint(response.Code) != "0" || response.Message != "重建完成" {
		t.Fatalf("成功响应应仅在处理完成后给出，code=%v message=%q", response.Code, response.Message)
	}
}
