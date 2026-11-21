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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// 合成服务仅连接现有授权夹具；embedding 由本测试 HTTP server 提供。
func qualityReindexFixture(t *testing.T, barrier func()) (*gorm.DB, context.Context, *KnowledgeService, model.KnowledgeDocument) {
	t.Helper()
	db, ctx := reindexSyntheticDB(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if barrier != nil {
			barrier()
		}
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid synthetic input", http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": make([]float32, 1024)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "synthetic"})
	}))
	t.Cleanup(provider.Close)
	product := model.RagProduct{
		TenantID: 413, ID: "fa13-quality-product", Name: "FA13 quality synthetic", IsActive: true,
		EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024},
	}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "quality-source.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("FA13 quality progress input. ", 8)), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := model.KnowledgeDocument{
		TenantID: 413, ProductID: product.ID, Title: "original title", FilePath: path, FileName: filepath.Base(path),
		SourceType: model.SourceTypeText, EmbedStatus: model.EmbedStatusPending,
		Tags: `["old"]`, Category: "old category", Priority: 5, Metadata: `{"origin":"synthetic"}`,
	}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
		RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	// 这轮验证进度清理和元信息交错，不调用不相关的 contextual LLM。
	svc.llmSvc = nil
	return db, ctx, svc, doc
}

// 旧元信息对象在实际 Reindex 认领后保存，不能回滚 owner/lease/count/status/stats，原 worker 必须完成。
func TestDocumentMetadataUpdatePreservesActiveReindex(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	db, ctx, svc, doc := qualityReindexFixture(t, func() {
		once.Do(func() { close(started) })
		<-release
	})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	chunk := model.KnowledgeChunk{TenantID: 413, ProductID: doc.ProductID, DocumentID: doc.ID, Content: "metadata interleaving", Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	stale, err := svc.Get(ctx, doc.ProductID, int64(doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.Reindex(ctx, doc.ProductID, doc.ID) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Reindex 未到达认领后的 embedding 屏障")
	}
	active, err := svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.ReindexOwnerToken == "" || active.ReindexLeaseUntil == nil || active.ReindexRetryCount != 1 {
		t.Fatalf("真实 Reindex 应先持久化 owner/lease/count，doc=%+v", active)
	}
	if err := svc.docRepo.UpdateStatusForOwner(ctx, doc.ID, active.ReindexOwnerToken, model.EmbedStatusProcessing, 60, "live progress"); err != nil {
		t.Fatal(err)
	}
	if err := svc.docRepo.UpdateChunkStatsForOwner(ctx, doc.ID, active.ReindexOwnerToken, 7, 99); err != nil {
		t.Fatal(err)
	}
	active, err = svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale.Title, stale.Category, stale.Tags, stale.Priority = "edited title", "", "[]", 0
	if err := svc.Update(ctx, stale); err != nil {
		t.Fatalf("保存旧元信息失败: %v", err)
	}
	stored, err := svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "edited title" || stored.Category != "" || stored.Tags != "[]" || stored.Priority != 0 || !stored.UpdatedAt.After(doc.UpdatedAt) {
		t.Errorf("元信息更新（含零值）必须落库，doc=%+v", stored)
	}
	if stored.ReindexOwnerToken != active.ReindexOwnerToken || !reflect.DeepEqual(stored.ReindexLeaseUntil, active.ReindexLeaseUntil) ||
		stored.ReindexRetryCount != 1 || stored.EmbedStatus != active.EmbedStatus || stored.EmbedProgress != 60 || stored.ErrorMsg != "live progress" ||
		stored.ChunkCount != 7 || stored.TotalTokens != 99 || !reflect.DeepEqual(stored.LastIndexAt, active.LastIndexAt) {
		t.Errorf("旧元信息写不得覆盖活跃 owner/lease/count/status/progress/error/stats，active=%+v stored=%+v", active, stored)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("元信息保存后原 owner 应正常完成: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("释放屏障后原 worker 未结束")
	}
	stored, err = svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil || stored.EmbedStatus != model.EmbedStatusIndexed || stored.ReindexRetryCount != 1 || stored.Title != "edited title" || stored.ReindexOwnerToken != active.ReindexOwnerToken {
		t.Errorf("原 worker 终态必须 indexed/1 且保留元信息和 owner，doc=%+v err=%v", stored, err)
	}
}

// 定向元信息更新不能写受保护业务字段，不能跨租户或让已删行复活。
func TestDocumentMetadataUpdateScopeAndMissingRows(t *testing.T) {
	db, ctx, svc, doc := qualityReindexFixture(t, nil)
	original, err := svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := *original
	stale.Title, stale.Category, stale.Tags, stale.Priority = "edited", "edited category", `["edited"]`, 2
	stale.FilePath, stale.FileName, stale.SourceRef, stale.Metadata = "must-not-write", "must-not-write", "must-not-write", `{"bad":true}`
	stale.PublicVisible, stale.HCStatus, stale.HCViews, stale.SearchCount, stale.HitCount, stale.Status = true, "published", 22, 33, 44, 0
	if err := svc.Update(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "edited" || stored.Category != "edited category" || stored.Tags != `["edited"]` || stored.Priority != 2 {
		t.Errorf("实际允许的元信息字段应全部落库，doc=%+v", stored)
	}
	if stored.FilePath != original.FilePath || stored.FileName != original.FileName || stored.SourceRef != original.SourceRef || stored.Metadata != original.Metadata ||
		stored.PublicVisible != original.PublicVisible || stored.HCStatus != original.HCStatus || stored.HCViews != original.HCViews || stored.SearchCount != original.SearchCount || stored.HitCount != original.HitCount || stored.Status != original.Status {
		t.Errorf("元信息更新不得修改来源/文件/发布状态/统计等字段，doc=%+v", stored)
	}
	before := *stored
	wrongTenant := tenant.WithTenantID(context.Background(), 999)
	stale.Title = "wrong tenant"
	if err := svc.docRepo.Update(wrongTenant, &stale); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("其他租户更新应返回不存在，err=%v", err)
	}
	stored, err = svc.docRepo.GetByID(ctx, doc.ID)
	if err != nil || !reflect.DeepEqual(before, *stored) {
		t.Fatalf("其他租户更新不得改变任何字段，doc=%+v err=%v", stored, err)
	}
	if err := db.WithContext(ctx).Delete(&model.KnowledgeDocument{}, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.docRepo.Update(ctx, &stale); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("已删行更新应返回不存在，err=%v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.KnowledgeDocument{}).Where("id = ?", doc.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("元信息更新不得重新插入已删行，count=%d err=%v", count, err)
	}
}

// 四个进度点的单次错误/写前取消都要落 failed，并可立即显式重试；同时覆盖 async 获取 owner 的入口。
func TestPipelineProgressFailurePersistsAndRetries(t *testing.T) {
	for _, entry := range []string{"reindex", "async"} {
		for _, progress := range []int{10, 30, 60, 80} {
			for _, mode := range []string{"single-error", "cancel-before-update"} {
				t.Run(fmt.Sprintf("%s/%d/%s", entry, progress, mode), func(t *testing.T) {
					db, ctx, svc, doc := qualityReindexFixture(t, nil)
					workCtx, cancel := context.WithCancel(ctx)
					defer cancel()
					cause := errors.New(fmt.Sprintf("synthetic progress %d write failure", progress))
					if mode == "cancel-before-update" {
						cause = context.Canceled
					}
					injected, cleanupSeen, cleanupValid := false, false, false
					callback := "fa13_quality_progress"
					// 在 GORM 开事务前取消，让真实 BeginTx 返回取消错误；避免测试制造已开事务回滚竞态。
					if err := db.Callback().Update().Before("gorm:begin_transaction").Register(callback, func(tx *gorm.DB) {
						updates, ok := tx.Statement.Dest.(map[string]any)
						if tx.Statement.Table != "knowledge_documents" || !ok {
							return
						}
						if updates["embed_status"] == model.EmbedStatusFailed {
							cleanupSeen = true
							deadline, bounded := tx.Statement.Context.Deadline()
							id, boundTenant := tenant.FromContext(tx.Statement.Context)
							cleanupValid = tx.Statement.Context.Err() == nil && bounded && time.Until(deadline) > 0 && time.Until(deadline) <= reindexFailureWriteTimeout && boundTenant && id == 413
						}
						if !injected && updates["embed_status"] == model.EmbedStatusProcessing && updates["embed_progress"] == progress {
							injected = true
							if mode == "cancel-before-update" {
								cancel()
								return
							}
							tx.AddError(cause)
						}
					}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
					var runErr error
					expectedCount := 1
					if entry == "reindex" {
						runErr = svc.Reindex(workCtx, doc.ProductID, doc.ID)
					} else {
						expectedCount = 0
						runErr = svc.processDocument(workCtx, doc.ID, doc.ProductID, doc.FilePath, doc.FileName, "", doc.MimeType, doc.Title, doc.SourceType, nil)
					}
					if !injected || !errors.Is(runErr, cause) {
						t.Errorf("进度失败应保留原始 error，injected=%v err=%v cause=%v", injected, runErr, cause)
					}
					stored, err := svc.docRepo.GetByID(ctx, doc.ID)
					if err != nil {
						t.Fatal(err)
					}
					if !cleanupSeen || !cleanupValid || stored.EmbedStatus != model.EmbedStatusFailed || !strings.Contains(stored.ErrorMsg, cause.Error()) || stored.ReindexLeaseUntil != nil || stored.ReindexRetryCount != expectedCount {
						t.Errorf("进度 %d 失败必须用有效限时租户上下文落 failed/error 并清 lease，cleanup=%v/%v status=%s error=%q count=%d lease=%v", progress, cleanupSeen, cleanupValid, stored.EmbedStatus, stored.ErrorMsg, stored.ReindexRetryCount, stored.ReindexLeaseUntil)
					}
					if err := db.Callback().Update().Remove(callback); err != nil {
						t.Fatal(err)
					}
					if err := svc.Reindex(ctx, doc.ProductID, doc.ID); err != nil {
						t.Errorf("DB 恢复后应可立即显式重试: %v", err)
					}
					stored, err = svc.docRepo.GetByID(ctx, doc.ID)
					if err != nil || stored.EmbedStatus != model.EmbedStatusIndexed || stored.ReindexRetryCount != expectedCount+1 || stored.ErrorMsg != "" {
						t.Errorf("显式重试必须 indexed 且只增一次计数，doc=%+v err=%v", stored, err)
					}
				})
			}
		}
	}
}

// 持续写故障必须暴露原错和 failed 回写错；owner 更换后旧清理只能失败，不能覆盖新状态。
func TestPipelineProgressFailureCleanupErrorsAndOwnerFence(t *testing.T) {
	for _, progress := range []int{10, 30, 60, 80} {
		for _, mode := range []string{"persistent-error", "owner-replaced"} {
			t.Run(fmt.Sprintf("%d/%s", progress, mode), func(t *testing.T) {
				db, ctx, svc, doc := qualityReindexFixture(t, nil)
				cause, cleanupCause := errors.New("synthetic progress write failure"), errors.New("synthetic failed write failure")
				injected, cleanupSeen := false, false
				var replacement *model.KnowledgeDocument
				callback := "fa13_quality_progress_cleanup"
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					updates, ok := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table != "knowledge_documents" || !ok {
						return
					}
					if updates["embed_status"] == model.EmbedStatusFailed {
						cleanupSeen = true
						if mode == "persistent-error" {
							tx.AddError(cleanupCause)
						}
					}
					if injected || updates["embed_status"] != model.EmbedStatusProcessing || updates["embed_progress"] != progress {
						return
					}
					injected = true
					if mode == "owner-replaced" {
						if err := db.WithContext(ctx).Model(&model.KnowledgeDocument{}).Where("id = ?", doc.ID).Update("reindex_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
							t.Fatal(err)
						}
						owner, claimed, err := svc.docRepo.ClaimForReindexWithLease(ctx, doc.ProductID, doc.ID, time.Minute)
						if err != nil || !claimed {
							t.Fatalf("新的真实 owner 应成功认领，claimed=%v err=%v", claimed, err)
						}
						if err := svc.docRepo.UpdateStatusForOwner(ctx, doc.ID, owner, model.EmbedStatusProcessing, 75, "new owner state"); err != nil {
							t.Fatal(err)
						}
						replacement, err = svc.docRepo.GetByID(ctx, doc.ID)
						if err != nil {
							t.Fatal(err)
						}
					}
					tx.AddError(cause)
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
				runErr := svc.Reindex(ctx, doc.ProductID, doc.ID)
				if !injected || !cleanupSeen || !errors.Is(runErr, cause) {
					t.Errorf("失败出口必须尝试清理并保留原错，injected=%v cleanup=%v err=%v", injected, cleanupSeen, runErr)
				}
				stored, err := svc.docRepo.GetByID(ctx, doc.ID)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "persistent-error" {
					if !errors.Is(runErr, cleanupCause) || !strings.Contains(runErr.Error(), "持久化失败状态失败") || stored.EmbedStatus != model.EmbedStatusProcessing || stored.ReindexRetryCount != 1 {
						t.Errorf("持续故障必须传播清理错误且不能报成功，doc=%+v err=%v", stored, runErr)
					}
					return
				}
				if !errors.Is(runErr, repository.ErrKnowledgeDocumentClaimLost) || !reflect.DeepEqual(stored, replacement) {
					t.Errorf("旧 owner 清理不得修改新 owner 任意字段，new=%+v stored=%+v err=%v", replacement, stored, runErr)
				}
				if err := svc.docRepo.UpdateStatusForOwner(ctx, doc.ID, replacement.ReindexOwnerToken, model.EmbedStatusIndexed, 100, ""); err != nil {
					t.Errorf("新 owner 应仍能完成: %v", err)
				}
			})
		}
	}
}
