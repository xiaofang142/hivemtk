// knowledge_tx_seam_test.go —— DeleteDocument 事务接缝保留与级联删除契约回归
// （AV-04c 批次7 刀4）。
//
// DeleteWithChunks 将级联删除事务封装在仓储层；本文件锁契约：
// 文档+分段同生共死（事务原子性）、
// NotFound 透传、FilePath 文件清理语义不变。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupTxSeamSvc(t *testing.T) (*KnowledgeBaseService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KBDocument{}, &model.KnowledgeChunk{})
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
	if err == nil {
		t.Fatal("不存在文档应返回错误")
	}
	if err != nil && err.Error() != "record not found" {
		t.Fatalf("应透传 gorm.ErrRecordNotFound 语义，got %v", err)
	}
}

// FilePath 非空时同步清理落盘文件（原语义保留：删除失败仅忽略，不阻断）。
func TestTxSeam_DeleteDocumentRemovesFile(t *testing.T) {
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

	if err := svc.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("落盘文件应被清理: err=%v", err)
	}
}

func TestTxSeam_DeleteDocumentRollback(t *testing.T) {
	for _, table := range []string{"knowledge_chunks", "kb_documents"} {
		t.Run(table, func(t *testing.T) {
			svc, db := setupTxSeamSvc(t)
			path := filepath.Join(t.TempDir(), "keep.txt")
			if err := os.WriteFile(path, []byte("keep on rollback"), 0o600); err != nil {
				t.Fatal(err)
			}
			doc := &model.KBDocument{TenantID: 1, Title: "rollback", FilePath: path}
			if err := db.Create(doc).Error; err != nil {
				t.Fatal(err)
			}
			chunk := &model.KnowledgeChunk{DocumentID: uint64(doc.ID), Content: "keep chunk"}
			if err := db.Create(chunk).Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New("delete rollback probe")
			if err := db.Callback().Delete().Before("gorm:delete").Register("fa12_delete_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := svc.DeleteDocument(context.Background(), doc.ID); !errors.Is(err, injected) {
				t.Fatalf("expected original delete failure, got %v", err)
			}
			for _, row := range []any{&model.KBDocument{}, &model.KnowledgeChunk{}} {
				var count int64
				if err := db.Model(row).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("rollback must preserve %T: count=%d err=%v", row, count, err)
				}
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("rollback must preserve source file: %v", err)
			}
		})
	}
}

func TestTxSeam_DeleteDocumentTenantIsolation(t *testing.T) {
	svc, db := setupTxSeamSvc(t)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	a := tenant.WithTenantID(context.Background(), 41)
	b := tenant.WithTenantID(context.Background(), 42)
	doc := &model.KBDocument{Title: "tenant owned"}
	if err := db.WithContext(a).Create(doc).Error; err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{b, context.Background()} {
		if err := svc.DeleteDocument(ctx, doc.ID); err == nil {
			t.Fatal("foreign or missing tenant must not delete a document")
		}
	}
	if _, err := svc.GetDocument(a, doc.ID); err != nil {
		t.Fatalf("rejected delete must preserve document: %v", err)
	}
	if err := svc.DeleteDocument(a, doc.ID); err != nil {
		t.Fatal(err)
	}
}
