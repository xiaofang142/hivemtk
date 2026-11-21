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
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// PostgreSQL 的 sequence 在事务提交前分配 ID。这里先预留低 ID，再提交高 ID；
// 查询回调在重建已经读到其边界后提交低 ID，验证一次 RebuildIndex 的真实成员集合。
func TestRebuildIndexUsesSnapshotVisibleMembership(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	product := model.RagProduct{ID: "fa13-snapshot-product", Name: "FA-13 snapshot synthetic", VectorTable: "fa13_snapshot_primary", IsActive: true}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}

	lateTx := db.WithContext(ctx).Begin()
	if lateTx.Error != nil {
		t.Fatal(lateTx.Error)
	}
	late := model.KnowledgeDocument{
		TenantID: 413, ProductID: product.ID, Title: "late lower id",
		FilePath: filepath.Join(t.TempDir(), "missing-late.txt"), Metadata: "{}",
	}
	if err := lateTx.Create(&late).Error; err != nil {
		_ = lateTx.Rollback().Error
		t.Fatalf("预留未提交的低序列 ID 失败: %v", err)
	}
	high := model.KnowledgeDocument{
		TenantID: 413, ProductID: product.ID, Title: "visible higher id",
		FilePath: filepath.Join(t.TempDir(), "missing-high.txt"), Metadata: "{}",
	}
	if err := db.WithContext(ctx).Create(&high).Error; err != nil {
		_ = lateTx.Rollback().Error
		t.Fatalf("创建已提交的高 ID 文档失败: %v", err)
	}
	if late.ID >= high.ID {
		t.Fatalf("测试前置条件错误：未提交文档 ID=%d 应小于已提交 ID=%d", late.ID, high.ID)
	}
	otherProduct := model.RagProduct{ID: "fa13-snapshot-other-product", Name: "other product", VectorTable: "fa13_snapshot_other", IsActive: true}
	if err := db.WithContext(ctx).Create(&otherProduct).Error; err != nil {
		_ = lateTx.Rollback().Error
		t.Fatalf("创建隔离产品失败: %v", err)
	}
	otherProductDoc := model.KnowledgeDocument{
		TenantID: 413, ProductID: otherProduct.ID, Title: "different product",
		FilePath: filepath.Join(t.TempDir(), "missing-other-product.txt"), Metadata: "{}",
	}
	if err := db.WithContext(ctx).Create(&otherProductDoc).Error; err != nil {
		_ = lateTx.Rollback().Error
		t.Fatalf("创建其他产品文档失败: %v", err)
	}
	otherTenantDoc := model.KnowledgeDocument{
		TenantID: 414, ProductID: product.ID, Title: "different tenant",
		FilePath: filepath.Join(t.TempDir(), "missing-other-tenant.txt"), Metadata: "{}",
	}
	if err := db.WithContext(tenant.WithSystemScope(ctx)).Create(&otherTenantDoc).Error; err != nil {
		_ = lateTx.Rollback().Error
		t.Fatalf("创建隔离租户文档失败: %v", err)
	}

	var committed atomic.Bool
	callbackName := fmt.Sprintf("fa13_snapshot_commit_%d", time.Now().UnixNano())
	commitLate := func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "knowledge_documents" || !committed.CompareAndSwap(false, true) {
			return
		}
		if err := lateTx.Commit().Error; err != nil {
			t.Errorf("边界查询后提交低 ID 文档失败: %v", err)
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, commitLate); err != nil {
		t.Fatalf("注册查询同步点失败: %v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, commitLate); err != nil {
		_ = db.Callback().Query().Remove(callbackName)
		t.Fatalf("注册 Scan 同步点失败: %v", err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
		RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	firstRunErr := svc.RebuildIndex(ctx, product.ID)
	_ = db.Callback().Query().Remove(callbackName)
	_ = db.Callback().Row().Remove(callbackName)
	if !committed.Load() {
		_ = lateTx.Rollback().Error
		t.Fatal("重建未在读取 work-set 边界后触发低 ID 提交同步点")
	}
	if firstRunErr == nil {
		t.Fatal("缺失源文件应使同步重建返回失败")
	}

	readAttempts := func() (int, int) {
		t.Helper()
		var got []struct {
			ID    uint64 `gorm:"column:id"`
			Count int    `gorm:"column:reindex_retry_count"`
		}
		if err := db.WithContext(ctx).Table("knowledge_documents").
			Select("id, reindex_retry_count").Where("product_id = ?", product.ID).Order("id ASC").Scan(&got).Error; err != nil {
			t.Fatalf("查询两篇文档的认领次数失败: %v", err)
		}
		if len(got) != 2 || got[0].ID != late.ID || got[1].ID != high.ID {
			t.Fatalf("提交后应存在两篇按 ID 排序的文档，实际=%+v", got)
		}
		return got[0].Count, got[1].Count
	}
	lateCount, highCount := readAttempts()
	if lateCount != 0 || highCount != 1 {
		t.Fatalf("snapshot 起始可见集合应只含高 ID 一次，首次认领次数 low/high=%d/%d", lateCount, highCount)
	}
	var isolatedCounts []struct {
		ID    uint64 `gorm:"column:id"`
		Count int    `gorm:"column:reindex_retry_count"`
	}
	if err := db.WithContext(tenant.WithSystemScope(ctx)).Table("knowledge_documents").
		Select("id, reindex_retry_count").Where("id IN ?", []uint64{otherProductDoc.ID, otherTenantDoc.ID}).Order("id ASC").Scan(&isolatedCounts).Error; err != nil {
		t.Fatalf("查询隔离文档认领次数失败: %v", err)
	}
	if len(isolatedCounts) != 2 || isolatedCounts[0].Count != 0 || isolatedCounts[1].Count != 0 {
		t.Fatalf("重建不得跨产品或租户认领文档，实际=%+v", isolatedCounts)
	}

	if err := svc.RebuildIndex(ctx, product.ID); err == nil {
		t.Fatal("下一次重建应处理新提交但源缺失的低 ID 文档并返回失败")
	}
	lateCount, highCount = readAttempts()
	if lateCount != 1 || highCount != 2 {
		t.Fatalf("下一次重建应各处理当前可见文档一次，累计认领次数 low/high=%d/%d", lateCount, highCount)
	}
}

// processing 状态有新鲜租约时不可被抢占；过期后并发 worker 只能原子地重领一次。
func TestReindexLeaseReclaimsExpiredProcessingExactlyOnce(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	repo := repository.NewKnowledgeDocumentRepository(db)
	doc := model.KnowledgeDocument{TenantID: 413, ProductID: "fa13-lease-product", Title: "leased", Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatalf("创建合成文档失败: %v", err)
	}

	claimed, err := repo.ClaimForReindex(ctx, doc.ProductID, doc.ID)
	if err != nil || !claimed {
		t.Fatalf("首次认领应成功，claimed=%v err=%v", claimed, err)
	}
	var lease struct {
		LeaseUntil *time.Time `gorm:"column:reindex_lease_until"`
	}
	if err := db.WithContext(ctx).Table("knowledge_documents").Select("reindex_lease_until").Where("id = ?", doc.ID).Scan(&lease).Error; err != nil {
		t.Fatalf("查询初始租约失败: %v", err)
	}
	if lease.LeaseUntil == nil || !lease.LeaseUntil.After(time.Now()) {
		t.Errorf("processing 认领必须同时写入未来租约，实际=%v", lease.LeaseUntil)
	}
	claimed, err = repo.ClaimForReindex(ctx, doc.ProductID, doc.ID)
	if err != nil || claimed {
		t.Errorf("新鲜 processing 租约不得被抢占，claimed=%v err=%v", claimed, err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{DocRepo: repo, ChunkRepo: repository.NewKnowledgeChunkRepository(db)})
	if err := svc.Reindex(ctx, doc.ProductID, doc.ID); err == nil {
		t.Error("新鲜 processing 租约不得经公开 Reindex 入口被抢占")
	}

	if err := db.WithContext(ctx).Table("knowledge_documents").Where("id = ?", doc.ID).
		Update("reindex_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("设置合成过期租约失败: %v", err)
	}
	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, claimErr := repo.ClaimForReindex(ctx, doc.ProductID, doc.ID)
			results <- ok
			errs <- claimErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("过期租约并发重领失败: %v", err)
		}
	}
	reclaimed := 0
	for ok := range results {
		if ok {
			reclaimed++
		}
	}
	if reclaimed != 1 {
		t.Fatalf("过期 processing 必须恰好重领一次，实际成功数=%d", reclaimed)
	}
	stored, err := repo.GetByProductAndID(ctx, doc.ProductID, int64(doc.ID))
	if err != nil {
		t.Fatalf("查询过期租约重领结果失败: %v", err)
	}
	if stored.EmbedStatus != model.EmbedStatusProcessing || stored.ReindexRetryCount != 2 {
		t.Fatalf("重领应保持 processing 并将次数增至 2，实际=%s/%d", stored.EmbedStatus, stored.ReindexRetryCount)
	}

	if err := db.WithContext(ctx).Table("knowledge_documents").Where("id = ?", doc.ID).
		Update("reindex_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("再次设置合成过期租约失败: %v", err)
	}
	if err := svc.Reindex(ctx, doc.ProductID, doc.ID); err == nil {
		t.Fatal("过期任务通过公开 Reindex 入口重领后，缺失源文件应返回失败")
	}
	failed, err := repo.GetByProductAndID(ctx, doc.ProductID, int64(doc.ID))
	if err != nil {
		t.Fatalf("查询过期任务的失败结果失败: %v", err)
	}
	if failed.EmbedStatus != model.EmbedStatusFailed || failed.ReindexRetryCount != 3 {
		t.Fatalf("公开 Reindex 重领失败后应持久化 failed/3，实际=%s/%d", failed.EmbedStatus, failed.ReindexRetryCount)
	}
	if err := svc.Reindex(ctx, doc.ProductID, doc.ID); err == nil {
		t.Fatal("失败文档的显式公开重试应再次失败")
	}
	failed, err = repo.GetByProductAndID(ctx, doc.ProductID, int64(doc.ID))
	if err != nil || failed.ReindexRetryCount != 4 {
		t.Fatalf("失败状态应可显式重试并递增计数至 4，文档=%+v err=%v", failed, err)
	}
}

// 失败应保留可查询终态并可显式重试；取消父请求后使用保留租户/trace 的限时清理上下文。
func TestReindexCancellationPersistsFailureAndAllowsRetry(t *testing.T) {
	for _, withChunks := range []bool{true, false} {
		name := "zero-chunk-pipeline"
		if withChunks {
			name = "stored-chunks"
		}
		t.Run(name, func(t *testing.T) {
			db, baseCtx := reindexSyntheticDB(t)
			started := make(chan struct{})
			release := make(chan struct{})
			var startOnce sync.Once
			var providerCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if providerCalls.Add(1) == 1 {
					startOnce.Do(func() { close(started) })
					select {
					case <-r.Context().Done():
					case <-release:
					}
					http.Error(w, "synthetic canceled embedding", http.StatusServiceUnavailable)
					return
				}
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
			defer close(release)
			product := model.RagProduct{
				ID: "fa13-cancel-product", Name: "FA-13 cancellation synthetic", IsActive: true,
				EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024},
			}
			if err := db.WithContext(baseCtx).Create(&product).Error; err != nil {
				t.Fatalf("创建合成产品失败: %v", err)
			}
			path := ""
			if !withChunks {
				path = filepath.Join(t.TempDir(), "source.txt")
				if err := os.WriteFile(path, []byte(strings.Repeat("synthetic text for reindex cancellation. ", 8)), 0o600); err != nil {
					t.Fatalf("创建零分片源文件失败: %v", err)
				}
			}
			doc := model.KnowledgeDocument{
				TenantID: 413, ProductID: product.ID, Title: "canceled reindex", FilePath: path,
				FileName: filepath.Base(path), SourceType: model.SourceTypeUpload, EmbedStatus: model.EmbedStatusPending, Metadata: "{}",
			}
			if err := db.WithContext(baseCtx).Create(&doc).Error; err != nil {
				t.Fatalf("创建合成文档失败: %v", err)
			}
			if withChunks {
				chunk := model.KnowledgeChunk{TenantID: 413, DocumentID: doc.ID, ProductID: product.ID, Content: "cancel embedding input", Metadata: "{}"}
				if err := db.WithContext(baseCtx).Create(&chunk).Error; err != nil {
					t.Fatalf("创建合成分片失败: %v", err)
				}
			}

			const traceID = "fa13-canceled-reindex-trace"
			failedWrite := make(chan struct {
				traceID  string
				deadline time.Time
				err      error
			}, 1)
			callbackName := fmt.Sprintf("fa13_canceled_status_%d", time.Now().UnixNano())
			if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement == nil || tx.Statement.Table != "knowledge_documents" {
					return
				}
				updates, ok := tx.Statement.Dest.(map[string]any)
				if !ok || fmt.Sprint(updates["embed_status"]) != string(model.EmbedStatusFailed) {
					return
				}
				deadline, _ := tx.Statement.Context.Deadline()
				select {
				case failedWrite <- struct {
					traceID  string
					deadline time.Time
					err      error
				}{logger.TraceIDFromContext(tx.Statement.Context), deadline, tx.Statement.Context.Err()}:
				default:
				}
			}); err != nil {
				t.Fatalf("注册失败状态观察回调失败: %v", err)
			}

			ctx, cancel := context.WithCancel(logger.WithTraceID(baseCtx, traceID))
			svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
				RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
				ChunkRepo: repository.NewKnowledgeChunkRepository(db),
			})
			reindexResult := make(chan error, 1)
			go func() { reindexResult <- svc.Reindex(ctx, product.ID, doc.ID) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				cancel()
				_ = db.Callback().Update().Remove(callbackName)
				t.Fatal("重建未进入合成向量提供方")
			}
			cancel()
			select {
			case err := <-reindexResult:
				if err == nil || !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
					t.Fatalf("取消的向量处理应返回取消错误，实际=%v", err)
				}
			case <-time.After(5 * time.Second):
				_ = db.Callback().Update().Remove(callbackName)
				t.Fatal("取消后的重建未在有限时间内返回")
			}
			_ = db.Callback().Update().Remove(callbackName)

			queryCtx := tenant.WithTenantID(context.Background(), 413)
			stored, err := repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(queryCtx, product.ID, int64(doc.ID))
			if err != nil {
				t.Fatalf("取消后查询文档失败: %v", err)
			}
			if stored.EmbedStatus != model.EmbedStatusFailed || stored.ReindexRetryCount != 1 || stored.ErrorMsg == "" {
				t.Errorf("取消后应留下失败状态和一次尝试，实际 status/count/error=%q/%d/%q", stored.EmbedStatus, stored.ReindexRetryCount, stored.ErrorMsg)
			}
			select {
			case evidence := <-failedWrite:
				remaining := time.Until(evidence.deadline)
				if evidence.traceID != traceID || remaining <= 0 || remaining > reindexFailureWriteTimeout || evidence.err != nil {
					t.Fatalf("失败回写必须保留 trace、租户上下文且使用未取消的有限期限，实际=%+v", evidence)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("未观察到失败状态持久化")
			}
			if err := svc.Reindex(queryCtx, product.ID, doc.ID); err != nil {
				t.Fatalf("取消后 failed 文档应能通过公开 Reindex 成功重试: %v", err)
			}
			stored, err = repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(queryCtx, product.ID, int64(doc.ID))
			if err != nil || stored.EmbedStatus != model.EmbedStatusIndexed || stored.ReindexRetryCount != 2 {
				t.Fatalf("取消后的显式重试应持久化 indexed/2，文档=%+v err=%v", stored, err)
			}
		})
	}
}

// Reindex 先获得文档后，异步入口即使随后启动也不能覆盖 owner 或重复调用 embedding。
func TestAsyncProcessingAndReindexShareAtomicOwnership(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		started <- struct{}{}
		<-release
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
	defer releaseOnce.Do(func() { close(release) })
	product := model.RagProduct{
		ID: "fa13-shared-owner-product", Name: "FA-13 shared owner synthetic", IsActive: true,
		EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024},
	}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	content := strings.Repeat("shared owner barrier content. ", 8)
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("创建合成源文件失败: %v", err)
	}
	doc := model.KnowledgeDocument{TenantID: 413, ProductID: product.ID, Title: "shared owner", FilePath: path, FileName: filepath.Base(path), SourceType: model.SourceTypeUpload, Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatalf("创建合成文档失败: %v", err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{
		RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db),
		ChunkRepo: repository.NewKnowledgeChunkRepository(db),
	})
	reindexDone := make(chan error, 1)
	go func() { reindexDone <- svc.Reindex(ctx, product.ID, doc.ID) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Reindex 未到达合成 embedding 屏障")
	}
	asyncDone := make(chan struct{})
	go func() {
		// An already-processing duplicate must stop at the ownership gate before
		// attempting to parse even empty input or changing the winner's state.
		svc.processDocumentAsync(ctx, doc.ID, product.ID, "", "", "", doc.MimeType, doc.Title, doc.SourceType, nil)
		close(asyncDone)
	}()
	select {
	case <-asyncDone:
	case <-time.After(5 * time.Second):
		t.Fatal("已处理文档的重复异步 worker 未在有限时间内结束")
	}
	stored, err := repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(ctx, product.ID, int64(doc.ID))
	if err != nil {
		t.Fatalf("查询 embedding 屏障中的文档 owner 状态失败: %v", err)
	}
	if stored.EmbedStatus != model.EmbedStatusProcessing {
		t.Errorf("重复异步 worker 不得覆盖获胜 owner 的 processing 状态，实际=%s", stored.EmbedStatus)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-reindexDone:
		if err != nil {
			t.Errorf("获胜的 Reindex 应完成: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("释放 embedding 屏障后 Reindex 未结束")
	}
	select {
	case <-asyncDone:
	case <-time.After(5 * time.Second):
		t.Fatal("异步 worker 未结束")
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("async 与 Reindex 竞争必须只产生一次 embedding side effect，实际调用数=%d", providerCalls.Load())
	}
}

// 长配置超时下，live async worker 的租约必须覆盖实际处理上下文，而非固定 15 分钟。
func TestAsyncProcessingLeaseCoversConfiguredTimeout(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	const configuredTimeout = 2 * time.Hour
	SetConfigReader(&fakeReader{durVal: configuredTimeout})
	t.Cleanup(func() { SetConfigReader(nil) })
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
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
	defer releaseOnce.Do(func() { close(release) })
	product := model.RagProduct{ID: "fa13-long-lease-product", Name: "FA-13 long lease synthetic", IsActive: true, EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024}}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	content := strings.Repeat("long configured timeout. ", 8)
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("创建合成源文件失败: %v", err)
	}
	doc := model.KnowledgeDocument{TenantID: 413, ProductID: product.ID, Title: "long lease", FilePath: path, FileName: filepath.Base(path), SourceType: model.SourceTypeUpload, Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatalf("创建合成文档失败: %v", err)
	}
	svc := NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db), ChunkRepo: repository.NewKnowledgeChunkRepository(db)})
	done := make(chan struct{})
	go func() {
		svc.processDocumentAsync(ctx, doc.ID, product.ID, path, doc.FileName, content, doc.MimeType, doc.Title, doc.SourceType, nil)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("异步 worker 未到达合成 embedding 屏障")
	}
	var lease struct {
		Until *time.Time `gorm:"column:reindex_lease_until"`
	}
	if err := db.WithContext(ctx).Table("knowledge_documents").Select("reindex_lease_until").Where("id = ?", doc.ID).Scan(&lease).Error; err != nil {
		t.Fatalf("查询运行中异步任务租约失败: %v", err)
	}
	remaining := time.Duration(0)
	if lease.Until != nil {
		remaining = time.Until(*lease.Until)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("释放 embedding 屏障后异步 worker 未结束")
	}
	if lease.Until == nil || remaining < configuredTimeout-time.Minute {
		t.Fatalf("异步租约必须覆盖配置的 %s 超时，实际剩余=%s lease=%v", configuredTimeout, remaining, lease.Until)
	}
}

// 租约过期后新 owner 完成写入时，旧 embedding 请求恢复不得覆盖新向量或终态。
func TestReindexExpiredOwnerCannotOverwriteNewOwner(t *testing.T) {
	db, ctx := reindexSyntheticDB(t)
	firstEmbedStarted := make(chan struct{})
	releaseFirstEmbed := make(chan struct{})
	var releaseOnce sync.Once
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := providerCalls.Add(1)
		if call == 1 {
			close(firstEmbedStarted)
			<-releaseFirstEmbed
		}
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid embedding request", http.StatusBadRequest)
			return
		}
		value := float32(call)
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			vector := make([]float32, 1024)
			vector[0] = value
			data[i] = map[string]any{"index": i, "embedding": vector}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "synthetic"})
	}))
	defer provider.Close()
	defer releaseOnce.Do(func() { close(releaseFirstEmbed) })
	product := model.RagProduct{ID: "fa13-stale-owner-product", Name: "FA-13 stale owner synthetic", IsActive: true, EmbeddingProviderConfig: model.EmbeddingProviderConfig{BaseURL: provider.URL, Model: "synthetic", Dimension: 1024}}
	if err := db.WithContext(ctx).Create(&product).Error; err != nil {
		t.Fatalf("创建合成产品失败: %v", err)
	}
	doc := model.KnowledgeDocument{TenantID: 413, ProductID: product.ID, Title: "stale owner", EmbedStatus: model.EmbedStatusPending, Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatalf("创建合成文档失败: %v", err)
	}
	chunk := model.KnowledgeChunk{TenantID: 413, DocumentID: doc.ID, ProductID: product.ID, ChunkIndex: 0, Content: "stale owner embedding input", Metadata: "{}"}
	if err := db.WithContext(ctx).Create(&chunk).Error; err != nil {
		t.Fatalf("创建合成分片失败: %v", err)
	}
	service := func() *KnowledgeService {
		return NewKnowledgeServiceWithDeps(KnowledgeServiceDeps{RagRepo: repository.NewRagConfigRepository(db), DocRepo: repository.NewKnowledgeDocumentRepository(db), ChunkRepo: repository.NewKnowledgeChunkRepository(db)})
	}
	oldDone := make(chan error, 1)
	go func() { oldDone <- service().Reindex(ctx, product.ID, doc.ID) }()
	select {
	case <-firstEmbedStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("旧 owner 未到达向量化屏障")
	}
	if err := db.WithContext(ctx).Table("knowledge_documents").Where("id = ?", doc.ID).Update("reindex_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("设置旧 owner 租约过期失败: %v", err)
	}
	newOwnerRepo := repository.NewKnowledgeDocumentRepository(db)
	claimed, err := newOwnerRepo.ClaimForReindex(ctx, product.ID, doc.ID)
	if err != nil || !claimed {
		releaseOnce.Do(func() { close(releaseFirstEmbed) })
		t.Fatalf("新 owner 应成功重领过期文档，claimed=%v err=%v", claimed, err)
	}
	newOwnerDoc, err := newOwnerRepo.GetByProductAndID(ctx, product.ID, int64(doc.ID))
	if err != nil {
		releaseOnce.Do(func() { close(releaseFirstEmbed) })
		t.Fatalf("读取新 owner 代际失败: %v", err)
	}
	newVector := make([]float32, 1024)
	newVector[0] = 2
	if err := repository.NewKnowledgeChunkRepository(db).UpdateEmbeddingsBatchWithSource(ctx, []model.KnowledgeChunk{chunk}, [][]float32{newVector}, "tei"); err != nil {
		releaseOnce.Do(func() { close(releaseFirstEmbed) })
		t.Fatalf("新 owner 持久化向量失败: %v", err)
	}
	if err := repository.NewKnowledgeDocumentRepository(db).UpdateStatus(ctx, doc.ID, model.EmbedStatusIndexed, 100, ""); err != nil {
		releaseOnce.Do(func() { close(releaseFirstEmbed) })
		t.Fatalf("新 owner 写入索引终态失败: %v", err)
	}
	var newerVector string
	if err := db.WithContext(ctx).Table("knowledge_chunks").Select("embedding").Where("id = ?", chunk.ID).Scan(&newerVector).Error; err != nil {
		t.Fatalf("读取新 owner 向量失败: %v", err)
	}
	if !strings.HasPrefix(newerVector, "[2") {
		t.Fatalf("新 owner 向量应先持久化，实际=%q", newerVector)
	}
	releaseOnce.Do(func() { close(releaseFirstEmbed) })
	select {
	case err := <-oldDone:
		if err == nil {
			t.Error("失去租约的旧 owner 应收到 fencing 错误")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("旧 owner 恢复后未结束")
	}
	var finalVector string
	if err := db.WithContext(ctx).Table("knowledge_chunks").Select("embedding").Where("id = ?", chunk.ID).Scan(&finalVector).Error; err != nil {
		t.Fatalf("读取最终向量失败: %v", err)
	}
	stored, err := repository.NewKnowledgeDocumentRepository(db).GetByProductAndID(ctx, product.ID, int64(doc.ID))
	if err != nil {
		t.Fatalf("读取最新 owner 文档终态失败: %v", err)
	}
	if finalVector != newerVector || stored.EmbedStatus != model.EmbedStatusIndexed || stored.ReindexRetryCount != newOwnerDoc.ReindexRetryCount || providerCalls.Load() != 1 {
		t.Fatalf("旧 owner 不得覆盖新向量或终态，first/newer=%q/%q status=%q calls=%d", newerVector, finalVector, stored.EmbedStatus, providerCalls.Load())
	}
}
