package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/testutil"
)

func setupReindexServiceDB(t *testing.T) (*model.KnowledgeDocument, *KnowledgeService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.RagProduct{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{})
	t.Cleanup(func() { testutil.ResetSchema(t, db, &model.KnowledgeChunk{}) })
	if err := db.Exec(`ALTER TABLE knowledge_chunks
		ADD COLUMN IF NOT EXISTS embedding vector,
		ADD COLUMN IF NOT EXISTS embed_status varchar(16) NOT NULL DEFAULT 'pending',
		ADD COLUMN IF NOT EXISTS embedding_source varchar(16) NOT NULL DEFAULT 'tei'`).Error; err != nil {
		t.Fatalf("prepare chunk vector columns: %v", err)
	}
	productID := "fa13-service"
	doc := &model.KnowledgeDocument{
		ProductID:   productID,
		Title:       "FA13 service test",
		SourceType:  model.SourceTypeUpload,
		EmbedStatus: model.EmbedStatusPending,
		Status:      1,
	}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("create service test document: %v", err)
	}
	svc := NewKnowledgeService()
	svc.reindexEmbed = func(context.Context, *llm.EmbeddingService, *llm.EmbeddingConfig, []string) ([][]float32, error) {
		return nil, errors.New("test seam not configured")
	}
	return doc, svc, db
}

func reindexVectors(n int) [][]float32 {
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = make([]float32, EmbeddingDim())
		vecs[i][0] = float32(i + 1)
	}
	return vecs
}

func assertReindexAttempts(t *testing.T, db *gorm.DB, id uint64, want int) {
	t.Helper()
	var doc model.KnowledgeDocument
	if err := db.First(&doc, id).Error; err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if got, ok := fields["reindex_attempts"].(float64); !ok || int(got) != want {
		t.Errorf("queryable reindex_attempts = %v, want %d", fields["reindex_attempts"], want)
	}
}

func TestReindexPersistsAttemptsAcrossFailureAndRetry(t *testing.T) {
	doc, svc, db := setupReindexServiceDB(t)
	chunk := model.KnowledgeChunk{DocumentID: doc.ID, ProductID: doc.ProductID, Content: "retry count"}
	if err := db.Create(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Reindex(context.Background(), doc.ProductID, doc.ID); err == nil {
		t.Fatal("first attempt should expose provider failure")
	}
	assertReindexAttempts(t, db, doc.ID, 1)
	svc.reindexEmbed = func(_ context.Context, _ *llm.EmbeddingService, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
		return reindexVectors(len(texts)), nil
	}
	if err := svc.Reindex(context.Background(), doc.ProductID, doc.ID); err != nil {
		t.Fatal(err)
	}
	assertReindexAttempts(t, db, doc.ID, 2)
}

func TestReindexReadableSourceCompletesBeforeReturning(t *testing.T) {
	doc, svc, db := setupReindexServiceDB(t)
	dir := t.TempDir()
	doc.FilePath = filepath.Join(dir, "source.txt")
	doc.FileName = "source.txt"
	if err := os.WriteFile(doc.FilePath, []byte("同步重建测试内容"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := svc.docRepo.Update(context.Background(), doc); err != nil {
		t.Fatalf("update source path: %v", err)
	}

	svc.reindexEmbed = func(_ context.Context, _ *llm.EmbeddingService, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
		return reindexVectors(len(texts)), nil
	}

	if err := svc.Reindex(context.Background(), doc.ProductID, doc.ID); err != nil {
		t.Fatalf("reindex readable source: %v", err)
	}
	var indexedChunks int64
	if err := db.Table("knowledge_chunks").Where("document_id = ? AND embedding IS NOT NULL AND embed_status = 'indexed'", doc.ID).Count(&indexedChunks).Error; err != nil {
		t.Fatal(err)
	}
	if indexedChunks == 0 {
		t.Fatal("PostgreSQL index vectors must be persisted before Reindex returns")
	}

	got, err := svc.docRepo.GetByProductAndID(context.Background(), doc.ProductID, int64(doc.ID))
	if err != nil {
		t.Fatalf("read reindexed document: %v", err)
	}
	if got.EmbedStatus != model.EmbedStatusIndexed {
		t.Fatalf("readable source should be indexed synchronously: got %q", got.EmbedStatus)
	}
}

func TestReindexEmbeddingFailurePersistsFailedStatus(t *testing.T) {
	doc, svc, db := setupReindexServiceDB(t)
	chunk := &model.KnowledgeChunk{DocumentID: doc.ID, ProductID: doc.ProductID, ChunkIndex: 0, Content: "existing chunk"}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	wantErr := errors.New("embedding provider failed")
	svc.reindexEmbed = func(context.Context, *llm.EmbeddingService, *llm.EmbeddingConfig, []string) ([][]float32, error) {
		return nil, wantErr
	}

	err := svc.Reindex(context.Background(), doc.ProductID, doc.ID)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Reindex error = %v, want embedding error", err)
	}
	var got model.KnowledgeDocument
	if err := db.First(&got, doc.ID).Error; err != nil {
		t.Fatalf("read failed document: %v", err)
	}
	if got.EmbedStatus != model.EmbedStatusFailed || !strings.Contains(got.ErrorMsg, wantErr.Error()) {
		t.Fatalf("embedding failure should be queryable: status=%q error=%q", got.EmbedStatus, got.ErrorMsg)
	}
}

func TestReindexDatabaseFailuresAreReturnedAndRetryable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		table     string
		condition string
	}{
		{"vector write", "knowledge_chunks", "NEW.embedding IS NOT NULL"},
		{"final state", "knowledge_documents", "NEW.embed_status = 'indexed'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, svc, db := setupReindexServiceDB(t)
			chunk := model.KnowledgeChunk{DocumentID: doc.ID, ProductID: doc.ProductID, Content: "retry me"}
			if err := db.Create(&chunk).Error; err != nil {
				t.Fatal(err)
			}
			svc.reindexEmbed = func(_ context.Context, _ *llm.EmbeddingService, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
				return reindexVectors(len(texts)), nil
			}
			installReindexFailureTrigger(t, db, tc.table, tc.condition)
			err := svc.Reindex(context.Background(), doc.ProductID, doc.ID)
			if err == nil || !strings.Contains(err.Error(), "fa13 injected write failure") {
				t.Fatalf("database failure must be returned: %v", err)
			}
			var failed model.KnowledgeDocument
			if err := db.First(&failed, doc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if failed.EmbedStatus != model.EmbedStatusFailed || failed.ErrorMsg == "" {
				t.Fatalf("write failure not queryable: %+v", failed)
			}
			assertReindexAttempts(t, db, doc.ID, 1)
			if err := db.Exec("DROP TRIGGER fa13_reindex_failure ON " + tc.table).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.Reindex(context.Background(), doc.ProductID, doc.ID); err != nil {
				t.Fatalf("second attempt should recover: %v", err)
			}
			var recovered model.KnowledgeDocument
			if err := db.First(&recovered, doc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if recovered.EmbedStatus != model.EmbedStatusIndexed || recovered.ErrorMsg != "" {
				t.Fatalf("retry must clear failure: %+v", recovered)
			}
			assertReindexAttempts(t, db, doc.ID, 2)
		})
	}
}

func installReindexFailureTrigger(t *testing.T, db *gorm.DB, table, condition string) {
	t.Helper()
	if err := db.Exec(`CREATE OR REPLACE FUNCTION fa13_reindex_failure() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'fa13 injected write failure'; END;
		$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP TRIGGER IF EXISTS fa13_reindex_failure ON " + table).Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec("DROP FUNCTION IF EXISTS fa13_reindex_failure()").Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("CREATE TRIGGER fa13_reindex_failure BEFORE UPDATE ON " + table +
		" FOR EACH ROW WHEN (" + condition + ") EXECUTE FUNCTION fa13_reindex_failure()").Error; err != nil {
		t.Fatal(err)
	}
	// The shared PostgreSQL test pool uses session_replication_role=replica.
	if err := db.Exec("ALTER TABLE " + table + " ENABLE ALWAYS TRIGGER fa13_reindex_failure").Error; err != nil {
		t.Fatal(err)
	}
}

func TestReindexEmptySourceAndFailedStatusWrite(t *testing.T) {
	for _, blockStatus := range []bool{false, true} {
		t.Run(fmt.Sprint(blockStatus), func(t *testing.T) {
			doc, svc, db := setupReindexServiceDB(t)
			doc.FilePath = filepath.Join(t.TempDir(), "empty.txt")
			doc.FileName = "empty.txt"
			if err := os.WriteFile(doc.FilePath, []byte(" \n\t"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := svc.docRepo.Update(context.Background(), doc); err != nil {
				t.Fatal(err)
			}
			if blockStatus {
				installReindexFailureTrigger(t, db, "knowledge_documents", "NEW.embed_status = 'failed'")
			}
			err := svc.Reindex(context.Background(), doc.ProductID, doc.ID)
			if err == nil || !strings.Contains(err.Error(), "文档内容为空") {
				t.Fatalf("empty-source error must be returned: %v", err)
			}
			if blockStatus && !strings.Contains(err.Error(), "fa13 injected write failure") {
				t.Fatalf("status-write error must also be returned: %v", err)
			}
		})
	}
}

func TestReindexDoesNotOverwriteConcurrentDocumentEdits(t *testing.T) {
	doc, svc, db := setupReindexServiceDB(t)
	chunk := model.KnowledgeChunk{DocumentID: doc.ID, ProductID: doc.ProductID, Content: "stored chunk", TokenCount: 7}
	if err := db.Create(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	svc.reindexEmbed = func(_ context.Context, _ *llm.EmbeddingService, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
		if err := db.Model(&model.KnowledgeDocument{}).Where("id = ?", doc.ID).Update("title", "edited while embedding").Error; err != nil {
			return nil, err
		}
		return reindexVectors(len(texts)), nil
	}
	if err := svc.Reindex(context.Background(), doc.ProductID, doc.ID); err != nil {
		t.Fatal(err)
	}
	var got model.KnowledgeDocument
	if err := db.First(&got, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Title != "edited while embedding" || got.TotalTokens != 7 {
		t.Fatalf("finalization overwrote edits or lost stats: title=%q tokens=%d", got.Title, got.TotalTokens)
	}
}

func TestRebuildIndexProcessesAllDocumentsAndSurfacesMissingSources(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.RagProduct{},
		&model.KnowledgeDocument{},
		&model.KnowledgeChunk{},
	)

	productID := "fa13-reindex-all"
	if err := db.Create(&model.RagProduct{
		ID:       productID,
		Name:     "FA13 reindex test product",
		IsActive: true,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	docs := make([]model.KnowledgeDocument, 1001)
	for i := range docs {
		docs[i] = model.KnowledgeDocument{
			ProductID:   productID,
			Title:       fmt.Sprintf("missing source %d", i),
			FilePath:    fmt.Sprintf("/definitely-missing/fa13-%d.txt", i),
			SourceType:  model.SourceTypeUpload,
			EmbedStatus: model.EmbedStatusPending,
			Status:      1,
		}
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatalf("create documents: %v", err)
	}
	// Updating the status also changes updated_at: cursor traversal must remain
	// stable, and every initial row must be visited exactly once.
	if err := db.Exec(`CREATE TABLE fa13_attempts(document_id bigint);
		CREATE OR REPLACE FUNCTION fa13_count_attempt() RETURNS trigger AS $$
		BEGIN INSERT INTO fa13_attempts VALUES (NEW.id); RETURN NEW; END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fa13_count_attempt AFTER UPDATE OF reindex_attempts ON knowledge_documents
		FOR EACH ROW EXECUTE FUNCTION fa13_count_attempt();
		ALTER TABLE knowledge_documents ENABLE ALWAYS TRIGGER fa13_count_attempt`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TRIGGER IF EXISTS fa13_count_attempt ON knowledge_documents")
		db.Exec("DROP FUNCTION IF EXISTS fa13_count_attempt()")
		db.Exec("DROP TABLE IF EXISTS fa13_attempts")
	})

	err := NewKnowledgeService().RebuildIndex(context.Background(), productID)
	if err == nil {
		t.Error("RebuildIndex should return the missing-source error")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("missing-source failure must not be reported as cancellation: %v", err)
	}

	var failed int64
	if err := db.Model(&model.KnowledgeDocument{}).
		Where("product_id = ? AND embed_status = ?", productID, model.EmbedStatusFailed).
		Count(&failed).Error; err != nil {
		t.Fatalf("count failed documents: %v", err)
	}
	if failed != int64(len(docs)) {
		t.Fatalf("all documents must be attempted and marked failed: got %d, want %d", failed, len(docs))
	}
	var counts struct{ Attempts, DistinctDocs int64 }
	if err := db.Table("fa13_attempts").Select("COUNT(*) AS attempts, COUNT(DISTINCT document_id) AS distinct_docs").Scan(&counts).Error; err != nil {
		t.Fatal(err)
	}
	if counts.Attempts != 1001 || counts.DistinctDocs != 1001 {
		t.Fatalf("each initial document must be attempted once: %+v", counts)
	}
	t.Logf("initial=%d attempted=%d distinct=%d failed=%d at %s", len(docs), counts.Attempts, counts.DistinctDocs, failed, time.Now().Format(time.RFC3339))
}

func TestRebuildIndexContinuesAfterErrorsAndDefersNewDocuments(t *testing.T) {
	doc, svc, db := setupReindexServiceDB(t)
	if err := db.Create(&model.RagProduct{ID: doc.ProductID, Name: "batch boundary test", IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	docs := []model.KnowledgeDocument{*doc}
	for i := 1; i < 101; i++ {
		docs = append(docs, model.KnowledgeDocument{ProductID: doc.ProductID, Title: fmt.Sprint(i), EmbedStatus: model.EmbedStatusPending})
	}
	if err := db.Create(docs[1:]).Error; err != nil {
		t.Fatal(err)
	}
	chunks := make([]model.KnowledgeChunk, len(docs))
	for i, initial := range docs {
		chunks[i] = model.KnowledgeChunk{DocumentID: initial.ID, ProductID: doc.ProductID, Content: fmt.Sprintf("document-%d", initial.ID)}
	}
	if err := db.Create(&chunks).Error; err != nil {
		t.Fatal(err)
	}
	firstErr, laterErr := errors.New("first embedding failure"), errors.New("later embedding failure")
	visits := make(map[string]int)
	var added []model.KnowledgeDocument
	svc.reindexEmbed = func(_ context.Context, _ *llm.EmbeddingService, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
		visits[texts[0]]++
		switch len(visits) {
		case 1:
			added = []model.KnowledgeDocument{
				{ProductID: doc.ProductID, Title: "new", EmbedStatus: model.EmbedStatusPending},
				{ProductID: doc.ProductID, Title: "backdated new", EmbedStatus: model.EmbedStatusPending, CreatedAt: doc.CreatedAt.Add(-time.Hour)},
			}
			if err := db.Create(&added).Error; err != nil {
				return nil, err
			}
			return nil, firstErr
		case 2:
			return nil, laterErr
		}
		return reindexVectors(len(texts)), nil
	}
	if err := svc.RebuildIndex(context.Background(), doc.ProductID); !errors.Is(err, firstErr) {
		t.Fatalf("batch must return its first error: %v", err)
	}
	if len(visits) != len(docs) {
		t.Fatalf("processed %d distinct documents, want %d", len(visits), len(docs))
	}
	for content, count := range visits {
		if count != 1 {
			t.Errorf("%s processed %d times", content, count)
		}
	}
	var indexed, failed, deferred int64
	if err := db.Model(&model.KnowledgeDocument{}).Where("embed_status = ?", model.EmbedStatusIndexed).Count(&indexed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.KnowledgeDocument{}).Where("embed_status = ?", model.EmbedStatusFailed).Count(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.KnowledgeDocument{}).Where("id IN ? AND embed_status = ?", []uint64{added[0].ID, added[1].ID}, model.EmbedStatusPending).Count(&deferred).Error; err != nil {
		t.Fatal(err)
	}
	if indexed != 99 || failed != 2 || deferred != 2 {
		t.Fatalf("indexed=%d failed=%d deferred=%d", indexed, failed, deferred)
	}
	t.Logf("attempts=%d indexed=%d failed=%d deferred=%d first_error=%s", len(visits), indexed, failed, deferred, firstErr)
}
