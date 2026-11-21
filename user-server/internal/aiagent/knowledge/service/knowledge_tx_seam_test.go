package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func setupTxSeamSvc(t *testing.T, extraModels ...any) (*KnowledgeBaseService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, append([]any{&model.KBDocument{}, &model.KnowledgeChunk{}}, extraModels...)...)
	svc := NewKnowledgeBaseServiceWithDocs(knowledgeRepo.NewKBDocumentRepository(db))
	return svc, db
}

// 级联删除：chunks 与文档同事务清空。
func TestTxSeam_DeleteDocumentCascade(t *testing.T) {
	svc, db := setupTxSeamSvc(t)
	ctx := context.Background()

	doc := &model.KBDocument{TenantID: 1, Title: "tx-doc", Content: "body", FileType: ".txt", Status: model.KBDocumentStatusIndexed}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("seed doc: %v", err)
	}
	chunks := []model.KnowledgeChunk{
		{DocumentID: uint64(doc.ID), Content: "c1", Weight: 1},
		{DocumentID: uint64(doc.ID), Content: "c2", Weight: 1},
	}
	if err := db.Create(&chunks).Error; err != nil {
		t.Fatalf("seed chunks: %v", err)
	}

	if err := svc.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	var nChunks, nDocs int64
	if err := db.Model(&model.KnowledgeChunk{}).Where("document_id = ?", doc.ID).Count(&nChunks).Error; err != nil || nChunks != 0 {
		t.Fatalf("chunks 应随事务删除: n=%d err=%v", nChunks, err)
	}
	if err := db.Model(&model.KBDocument{}).Where("id = ?", doc.ID).Count(&nDocs).Error; err != nil || nDocs != 0 {
		t.Fatalf("文档应删除: n=%d err=%v", nDocs, err)
	}
}

// NotFound 透传：不存在文档返回 gorm.ErrRecordNotFound（原错误路径不变）。
func TestTxSeam_DeleteDocumentNotFound(t *testing.T) {
	svc, _ := setupTxSeamSvc(t)
	err := svc.DeleteDocument(context.Background(), 99999)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("应透传 gorm.ErrRecordNotFound 语义，got %v", err)
	}
}

// FilePath 非空时同步清理落盘文件（原语义保留：删除失败仅忽略，不阻断）。
func TestTxSeam_DeleteDocumentRemovesFileAndIndex(t *testing.T) {
	svc, db := setupTxSeamSvc(t)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("写夹具: %v", err)
	}
	doc := &model.KBDocument{TenantID: 1, Title: "file-doc", Content: "body", FilePath: path, FileType: ".txt", Status: model.KBDocumentStatusIndexed}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("seed doc: %v", err)
	}
	indexID := seedTxSeamIndex(t, svc, doc.ID)

	if err := svc.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("落盘文件应被清理: err=%v", err)
	}
	if _, err := svc.indexer.GetIndexStats(ctx, indexID); err == nil {
		t.Fatal("索引应被清理")
	}
}

func TestTxSeam_DeleteDocumentScopesBothTables(t *testing.T) {
	svc, db := setupTxSeamSvc(t)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })

	ownerCtx := tenant.WithTenantID(context.Background(), 71)
	otherCtx := tenant.WithTenantID(context.Background(), 72)
	doc := model.KBDocument{TenantID: 71, Title: "owned doc"}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	chunks := []model.KnowledgeChunk{
		{TenantID: 71, DocumentID: uint64(doc.ID), Content: "owned chunk"},
		{TenantID: 72, DocumentID: uint64(doc.ID), Content: "foreign chunk"},
	}
	if err := db.Create(&chunks).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteDocument(otherCtx, doc.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("外租户文档应视为不存在: %v", err)
	}
	tenant.SetMode(tenant.ModeOn)
	err := svc.DeleteDocument(context.Background(), doc.ID)
	tenant.SetMode(tenant.ModeOff)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("多租户模式无租户时应视为不存在: %v", err)
	}
	if err := svc.DeleteDocument(ownerCtx, doc.ID); err != nil {
		t.Fatalf("本租户应能删除: %v", err)
	}
	var remaining []model.KnowledgeChunk
	if err := db.Where("document_id = ?", doc.ID).Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != chunks[1].ID {
		t.Fatalf("应仅保留外租户分段: %+v", remaining)
	}
}

// Restrict the document delete using an actual PostgreSQL foreign key.
type txSeamDocumentReference struct {
	ID         uint
	DocumentID uint
}

func TestTxSeam_DeleteDocumentRollbackPreservesFileAndIndex(t *testing.T) {
	svc, db := setupTxSeamSvc(t, &txSeamDocumentReference{})
	ctx := tenant.ContextWithDefault(context.Background())
	if err := db.Exec(`ALTER TABLE tx_seam_document_references ADD CONSTRAINT tx_seam_document_restrict
		FOREIGN KEY (document_id) REFERENCES kb_documents(id) ON DELETE RESTRICT`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`ALTER TABLE tx_seam_document_references DROP CONSTRAINT tx_seam_document_restrict`).Error; err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "protected.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := model.KBDocument{TenantID: tenant.DefaultTenantID, Title: "protected doc", FilePath: path}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	chunk := model.KnowledgeChunk{TenantID: doc.TenantID, DocumentID: uint64(doc.ID), Content: "must roll back"}
	if err := db.Create(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	ref := txSeamDocumentReference{DocumentID: doc.ID}
	if err := db.Create(&ref).Error; err != nil {
		t.Fatal(err)
	}
	// The reusable test schema must not retain the blocking row after this test.
	t.Cleanup(func() {
		if err := db.Delete(&ref).Error; err != nil {
			t.Error(err)
		}
	})
	indexID := seedTxSeamIndex(t, svc, doc.ID)
	err := db.Connection(func(conn *gorm.DB) error {
		// The test pool can run in replica mode, which disables foreign-key triggers.
		conn = conn.Session(&gorm.Session{NewDB: true})
		var previousRole string
		if err := conn.Raw(`SELECT current_setting('session_replication_role')`).Scan(&previousRole).Error; err != nil {
			t.Fatal(err)
		}
		if previousRole != "origin" {
			if err := conn.Exec(`SET session_replication_role = 'origin'`).Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := conn.Exec(`SELECT set_config('session_replication_role', ?, false)`, previousRole).Error; err != nil {
					t.Error(err)
				}
			}()
		}
		svc.docs = knowledgeRepo.NewKBDocumentRepository(conn)
		return svc.DeleteDocument(ctx, doc.ID)
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("应由 PostgreSQL 拒绝删除有引用的文档: %v", err)
	}
	if err := db.First(&model.KBDocument{}, doc.ID).Error; err != nil {
		t.Fatalf("文档应保留: %v", err)
	}
	if err := db.First(&model.KnowledgeChunk{}, chunk.ID).Error; err != nil {
		t.Fatalf("分段删除应回滚: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("回滚后应保留落盘文件: %v", err)
	}
	if _, err := svc.indexer.GetIndexStats(ctx, indexID); err != nil {
		t.Fatalf("回滚后应保留向量索引: %v", err)
	}
}

func seedTxSeamIndex(t *testing.T, svc *KnowledgeBaseService, docID uint) string {
	t.Helper()
	indexID := fmt.Sprintf("kb_%d", docID)
	if err := svc.indexer.BuildIndex(context.Background(), indexID, []ragretrieval.Chunk{{Embedding: make([]float32, EmbeddingDim())}}); err != nil {
		t.Fatal(err)
	}
	return indexID
}
