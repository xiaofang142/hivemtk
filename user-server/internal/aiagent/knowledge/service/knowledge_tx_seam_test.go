// knowledge_tx_seam_test.go —— DeleteDocument 事务接缝保留与级联删除契约回归
// （AV-04c 批次7 刀4）。
//
// knowledge_base.go 的 tx:74 Transaction 回调是 AV-03 钉住的 service 层唯一合法
// 事务档接缝：句柄解析改经 KBDocumentRepository.Transaction（仓储同形接缝，
// 回调体逐字未动）后，本文件锁契约：文档+分段同生共死（事务原子性）、
// NotFound 透传、FilePath 文件清理语义不变。
package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
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
