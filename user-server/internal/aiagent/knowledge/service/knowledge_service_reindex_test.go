package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 合成 PostgreSQL 集成夹具：只建/清理本测试创建的 schema，不触碰 testutil 进程库。
func reindexSyntheticDB(t *testing.T) (*gorm.DB, context.Context) {
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
	schema := fmt.Sprintf("fa13_reindex_%d", time.Now().UnixNano())
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
	if err := db.AutoMigrate(&model.KnowledgeDocument{}, &model.KnowledgeChunk{}, &model.RagProduct{}); err != nil {
		t.Fatal(err)
	}
	// TDD 前置兼容列：当前 model 尚无计数声明；行为测试预先提供目标列，
	// 使 RED 来自缺页/缺任务与计数行为，而不是 DDL 编译或列缺失。
	if err := db.Exec(`ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS reindex_retry_count bigint NOT NULL DEFAULT 0`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS reindex_lease_until timestamptz`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS reindex_owner_token varchar(64) NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatal(err)
	}
	// 本测试 schema 用 text-backed vector domain 代替未安装的 pgvector 扩展；
	// 仅允许服务走通向量保存链路，不验证 pgvector 运算或性能。
	if err := db.Exec(`CREATE DOMAIN vector AS text`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS embedding text, ADD COLUMN IF NOT EXISTS embed_status text, ADD COLUMN IF NOT EXISTS embedding_source text`).Error; err != nil {
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
	return db, tenant.WithTenantID(context.Background(), 413)
}

// 产品重建必须在固定边界内遍历每篇文档一次，并把缺失源文件变成可查询失败。
func TestRebuildIndex_VisitsAllDocumentsAndPersistsMissingSourceFailure(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	product := model.RagProduct{ID: "fa13-product", Name: "FA-13 synthetic", IsActive: true}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	const documentCount = 1001
	docs := make([]model.KnowledgeDocument, documentCount)
	missingPath := t.TempDir() + "/missing-source.txt"
	emptyPath := filepath.Join(t.TempDir(), "empty-source.txt")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatalf("创建空源文件失败: %v", err)
	}
	for i := range docs {
		filePath := missingPath
		if i == documentCount-1 {
			filePath = emptyPath
		}
		docs[i] = model.KnowledgeDocument{
			TenantID:    413,
			ProductID:   product.ID,
			Title:       fmt.Sprintf("doc-%04d", i),
			FilePath:    filePath,
			SourceType:  model.SourceTypeUpload,
			EmbedStatus: model.EmbedStatusPending,
			Metadata:    "{}",
		}
	}
	if err := db.WithContext(ctx).Create(&docs).Error; err != nil {
		t.Fatalf("创建 %d 篇合成文档失败: %v", documentCount, err)
	}

	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
		RagRepo:   repository.NewRagConfigRepository(db),
		DocRepo:   repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	err := svc.RebuildIndex(ctx, product.ID)
	if err == nil {
		t.Error("至少一篇源文件缺失时产品重建必须返回失败，不能报告成功")
	}
	var got []struct {
		ID          uint64
		EmbedStatus string `gorm:"column:embed_status"`
		ErrorMsg    string `gorm:"column:error_msg"`
		RetryCount  int    `gorm:"column:reindex_retry_count"`
	}
	if err := db.WithContext(ctx).Table("knowledge_documents").
		Select("id, embed_status, error_msg, reindex_retry_count").
		Where("product_id = ?", product.ID).Order("id ASC").Scan(&got).Error; err != nil {
		t.Fatalf("查询合成文档终态失败: %v", err)
	}
	if len(got) != documentCount {
		t.Fatalf("应遍历全部 %d 篇文档，实际查到 %d 篇", documentCount, len(got))
	}
	var failed, claimed int
	for _, doc := range got {
		if doc.EmbedStatus != string(model.EmbedStatusFailed) || !strings.Contains(doc.ErrorMsg, "源") {
			continue
		}
		failed++
		if doc.RetryCount == 1 {
			claimed++
		}
	}
	if failed != documentCount || claimed != documentCount {
		t.Errorf("期望 %d 篇可查询失败且各认领一次，实际失败=%d，各认领一次=%d", documentCount, failed, claimed)
	}
	if err := svc.Reindex(ctx, product.ID, got[0].ID); err == nil {
		t.Fatal("失败文档再次显式重试时仍缺源文件，必须返回失败")
	}
	var retried struct {
		RetryCount int `gorm:"column:reindex_retry_count"`
	}
	if err := db.WithContext(ctx).Table("knowledge_documents").Select("reindex_retry_count").Where("id = ?", got[0].ID).Scan(&retried).Error; err != nil {
		t.Fatalf("查询显式重试计数失败: %v", err)
	}
	if retried.RetryCount != 2 {
		t.Fatalf("失败文档显式重试后计数应为 2，实际=%d", retried.RetryCount)
	}
}

// 固定水位排除之后新增的 ID，且并发 worker 对同一篇文档最多只有一个认领成功。
func TestReindexCursorWatermarkAndAtomicClaim(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	repo := repository.NewKnowledgeDocumentRepository(db)
	productID := "fa13-watermark-product"
	first := model.KnowledgeDocument{TenantID: 413, ProductID: productID, Title: "before", Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&first).Error; err != nil {
		t.Fatalf("创建水位前文档失败: %v", err)
	}
	frozenIDs, err := repo.ListProductDocumentIDsSnapshot(ctx, productID, 10)
	if err != nil {
		t.Fatalf("读取产品文档快照失败: %v", err)
	}
	second := model.KnowledgeDocument{TenantID: 413, ProductID: productID, Title: "after", Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&second).Error; err != nil {
		t.Fatalf("创建水位后文档失败: %v", err)
	}
	if len(frozenIDs) != 1 || frozenIDs[0] != first.ID {
		t.Fatalf("快照应冻结初始可见文档 ID=%d，实际=%v", first.ID, frozenIDs)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := repo.ClaimForReindex(ctx, productID, first.ID)
			results <- claimed
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发认领失败: %v", err)
		}
	}
	var claimed int
	for ok := range results {
		if ok {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("两个并发 worker 中恰好一个应认领成功，实际成功数=%d", claimed)
	}
	stored, err := repo.GetByProductAndID(ctx, productID, int64(first.ID))
	if err != nil {
		t.Fatalf("查询认领后的文档失败: %v", err)
	}
	if stored.EmbedStatus != model.EmbedStatusProcessing || stored.ReindexRetryCount != 1 {
		t.Fatalf("文档状态和次数应为 processing/1，实际=%s/%d", stored.EmbedStatus, stored.ReindexRetryCount)
	}
}

// 向量提供方失败必须转为可查询失败并保留一次重建尝试。
func TestReindex_EmbeddingFailureIsQueryableAndRetryable(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic embedding failure", http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	product := model.RagProduct{
		ID:       "fa13-embedding-product",
		Name:     "FA-13 embedding synthetic",
		IsActive: true,
		EmbeddingProviderConfig: model.EmbeddingProviderConfig{
			BaseURL:   provider.URL,
			Model:     "synthetic-embedding",
			Dimension: 1024,
		},
	}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	doc := model.KnowledgeDocument{
		TenantID:    413,
		ProductID:   product.ID,
		Title:       "embedding failure doc",
		EmbedStatus: model.EmbedStatusPending,
		Metadata:    "{}",
	}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatalf("创建合成文档失败: %v", err)
	}
	chunk := model.KnowledgeChunk{
		TenantID:   413,
		DocumentID: doc.ID,
		ProductID:  product.ID,
		ChunkIndex: 0,
		Content:    "embedding failure input",
		Metadata:   "{}",
	}
	if err := db.WithContext(ctx).Create(&chunk).Error; err != nil {
		t.Fatalf("创建合成分片失败: %v", err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
		RagRepo:   repository.NewRagConfigRepository(db),
		DocRepo:   repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	if err := svc.Reindex(ctx, product.ID, doc.ID); err == nil {
		t.Fatal("向量提供方返回 503 时重建必须返回失败")
	}
	stored, err := repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(ctx, product.ID, int64(doc.ID))
	if err != nil {
		t.Fatalf("查询向量失败状态失败: %v", err)
	}
	if stored.EmbedStatus != model.EmbedStatusFailed || !strings.Contains(stored.ErrorMsg, "embedding") {
		t.Fatalf("期望可查询向量失败状态，实际 status=%q error=%q", stored.EmbedStatus, stored.ErrorMsg)
	}
	if stored.ReindexRetryCount != 1 {
		t.Fatalf("向量失败后重建计数应为 1，实际=%d", stored.ReindexRetryCount)
	}
}

// 索引完成状态首次写入失败时必须落失败状态；若失败状态也写不进去，必须传播两次错误。
func TestReindex_FinalStatusWriteFailurePersistsOrPropagates(t *testing.T) {
	for _, failFailedStatusWrite := range []bool{false, true} {
		name := "failed-state-write-succeeds"
		if failFailedStatusWrite {
			name = "both-status-writes-fail"
		}
		t.Run(name, func(t *testing.T) {
			db, ctx := reindexSyntheticDB(t)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Input []string `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "invalid embedding request", http.StatusBadRequest)
					return
				}
				data := make([]map[string]any, len(request.Input))
				for i := range data {
					data[i] = map[string]any{"index": i, "embedding": make([]float32, 1024)}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "synthetic"})
			}))
			defer provider.Close()
			product := model.RagProduct{
				ID: "fa13-final-status-product", Name: "FA-13 final status synthetic", IsActive: true,
				EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024},
			}
			if err := db.WithContext(ctx).Create(&product).Error; err != nil {
				t.Fatalf("创建合成产品失败: %v", err)
			}
			doc := model.KnowledgeDocument{TenantID: 413, ProductID: product.ID, Title: "final status failure", EmbedStatus: model.EmbedStatusPending, Metadata: "{}"}
			if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
				t.Fatalf("创建合成文档失败: %v", err)
			}
			chunk := model.KnowledgeChunk{TenantID: 413, DocumentID: doc.ID, ProductID: product.ID, ChunkIndex: 0, Content: "final status failure input", Metadata: "{}"}
			if err := db.WithContext(ctx).Create(&chunk).Error; err != nil {
				t.Fatalf("创建合成分片失败: %v", err)
			}

			var indexedWriteFailed atomic.Bool
			var failedWriteFailed atomic.Bool
			callbackName := fmt.Sprintf("fa13_final_status_fault_%d", time.Now().UnixNano())
			if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table != "knowledge_documents" {
					return
				}
				updates, ok := tx.Statement.Dest.(map[string]any)
				if !ok {
					return
				}
				switch fmt.Sprint(updates["embed_status"]) {
				case string(model.EmbedStatusIndexed):
					if indexedWriteFailed.CompareAndSwap(false, true) {
						tx.AddError(errors.New("synthetic indexed status write failure"))
					}
				case string(model.EmbedStatusFailed):
					if failFailedStatusWrite && failedWriteFailed.CompareAndSwap(false, true) {
						tx.AddError(errors.New("synthetic failed status write failure"))
					}
				}
			}); err != nil {
				t.Fatalf("注册状态写故障注入回调失败: %v", err)
			}

			svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
				RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
				ChunkRepo: repository.NewKnowledgeChunkRepository(db),
			})
			reindexErr := svc.Reindex(ctx, product.ID, doc.ID)
			if reindexErr == nil || !indexedWriteFailed.Load() {
				t.Fatalf("终态索引状态写入失败必须返回错误，err=%v faultInjected=%v", reindexErr, indexedWriteFailed.Load())
			}
			stored, err := repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(ctx, product.ID, int64(doc.ID))
			if err != nil {
				t.Fatalf("查询终态写失败后的文档失败: %v", err)
			}
			if failFailedStatusWrite {
				if !failedWriteFailed.Load() || stored.EmbedStatus != model.EmbedStatusProcessing || !strings.Contains(reindexErr.Error(), "持久化失败状态失败") {
					t.Fatalf("两次状态写失败必须传播 fallback 错误并保留原 processing 行，status=%q err=%v", stored.EmbedStatus, reindexErr)
				}
				return
			}
			if stored.EmbedStatus != model.EmbedStatusFailed || !strings.Contains(stored.ErrorMsg, "更新文档索引状态失败") {
				t.Fatalf("fallback 状态必须可查询为 failed 并包含终态写错误，status=%q error=%q", stored.EmbedStatus, stored.ErrorMsg)
			}
			if stored.ReindexRetryCount != 1 {
				t.Fatalf("状态写失败后重建计数应保持 1，实际=%d", stored.ReindexRetryCount)
			}
		})
	}
}
