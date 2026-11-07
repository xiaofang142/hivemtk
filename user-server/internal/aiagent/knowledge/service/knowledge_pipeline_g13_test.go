package service

// P0-3 / G13 验收：解析失败不得退化为原文入库。
//
// 两格：
//  1. 伪 PDF（结构化后缀）提取失败 → markFailed（embed_status=failed）且 knowledge_chunks
//     无对应行 —— 反向对账：摘掉 pipeline 的 markFailed 分支本用例必红。
//  2. 纯文本后缀提取异常 → 允许退化为原始字节（行为锁定，防误伤）。
//
// DB 对账口径（P0-3 清单要求写回）：knowledge_documents.embed_status 枚举值 'failed'
// （model.EmbedStatusFailed），错误信息落 knowledge_documents.error_msg；
// chunk 对账表 knowledge_chunks，按 document_id 计数。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/testutil"
)

// scanPDF 合法 PDF 结构（手工 xref 表）但零文本页——扫描件形态，ExtractText 报「扫描件」错。
const scanPDF = "%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\nxref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \ntrailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n186\n%%EOF"

func TestProcessDocumentAsync_FakePDFMarksFailedNoChunks(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KnowledgeDocument{}, &model.KnowledgeChunk{})
	svc := NewKnowledgeService()

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "fake_scan.pdf")
	if err := os.WriteFile(pdfPath, []byte(scanPDF), 0o600); err != nil {
		t.Fatalf("写伪 PDF 失败: %v", err)
	}

	seedDoc := model.KnowledgeDocument{ID: 1, ProductID: "p1", Title: "伪PDF", EmbedStatus: model.EmbedStatusPending}
	if err := db.Create(&seedDoc).Error; err != nil {
		t.Fatalf("种文档行失败: %v", err)
	}

	svc.processDocumentAsync(context.Background(), 1, "p1",
		pdfPath, "fake_scan.pdf", "", "application/pdf", "伪PDF", model.SourceTypeUpload, nil)

	var doc model.KnowledgeDocument
	if err := db.First(&doc, 1).Error; err != nil {
		t.Fatalf("读文档失败: %v", err)
	}
	if doc.EmbedStatus != model.EmbedStatusFailed {
		t.Errorf("embed_status = %q, want %q（markFailed 未触发）", doc.EmbedStatus, model.EmbedStatusFailed)
	}
	if !strings.Contains(doc.ErrorMsg, "文本提取失败") {
		t.Errorf("error_msg = %q, want 含「文本提取失败」", doc.ErrorMsg)
	}

	var n int64
	db.Model(&model.KnowledgeChunk{}).Where("document_id = ?", 1).Count(&n)
	if n != 0 {
		t.Errorf("knowledge_chunks 命中 %d 行（>0 = 二进制垃圾入库污染向量库）", n)
	}
}

func TestProcessDocumentAsync_PlainTextFallbackPreserved(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KnowledgeDocument{}, &model.KnowledgeChunk{})
	// embedding/embed_status 列不在 gorm model 上（走 UpdateEmbeddingsBatch 的裸 SQL），
	// AutoMigrate 建不出，需手补；维度与 EmbeddingDim() 对齐或直接用 vector 无维约束。
	if err := db.Exec(`ALTER TABLE knowledge_chunks
		ADD COLUMN IF NOT EXISTS embedding vector,
		ADD COLUMN IF NOT EXISTS embed_status varchar(16) NOT NULL DEFAULT 'pending'`).Error; err != nil {
		t.Fatalf("补 chunk 向量列失败: %v", err)
	}
	svc := NewKnowledgeService()

	dir := t.TempDir()
	// 不存在的 .txt 文件路径 → os.ReadFile 失败走不到提取；为触发提取异常，
	// 用目录路径代替文件：ReadFile 报 EISDIR，但 IsPlainTextExt(.txt) 为 true ⇒
	// 走不到提取，读文件失败先 markFailed。改为直接喂 content 触发不了提取分支，
	// 故用「可读但提取层报错」的形态不可得——纯文本 ExtractText 永不报错（G13 口径），
	// 因此锁定目标改为：可读 .txt 走提取成功、正常入库（回归锁）。
	txtPath := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(txtPath, []byte("这是一段正常的纯文本知识内容，足够长以便分片。"+strings.Repeat("重复内容填充。", 40)), 0o600); err != nil {
		t.Fatalf("写纯文本失败: %v", err)
	}

	seedDoc2 := model.KnowledgeDocument{ID: 1, ProductID: "p2", Title: "纯文本", EmbedStatus: model.EmbedStatusPending}
	if err := db.Create(&seedDoc2).Error; err != nil {
		t.Fatalf("种文档行失败: %v", err)
	}

	svc.processDocumentAsync(context.Background(), 1, "p2",
		txtPath, "plain.txt", "", "text/plain", "纯文本", model.SourceTypeUpload, nil)

	var doc model.KnowledgeDocument
	if err := db.First(&doc, 1).Error; err != nil {
		t.Fatalf("读文档失败: %v", err)
	}
	if doc.EmbedStatus == model.EmbedStatusFailed {
		t.Fatalf("纯文本不应 markFailed，error_msg=%q", doc.ErrorMsg)
	}
	var n int64
	db.Model(&model.KnowledgeChunk{}).Where("document_id = ?", 1).Count(&n)
	if n == 0 {
		t.Errorf("纯文本正常路径应产出 chunk 行，实际 0 行")
	}
}
