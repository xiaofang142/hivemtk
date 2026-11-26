package service

// knowledge_base_g13_test.go P0-3/G13 在**上传链路**上的对账（API 冒烟抓到的漏网面）。
//
// 缺陷形态：`/api/rag/import` 走 KnowledgeBaseService.processDocumentAsync，
// 该链路此前 `content := string(contentBytes)`——PDF/docx 的二进制字节被当正文分片
// 进索引，且永不失败。另一条 pipeline（knowledge_service_pipeline.go）早先已按 G13 修好，
// 但两条上传链路各写一份实现，修 A 不等于修 B。
//
// 判据：结构化后缀（.pdf）读取失败/不可解析 ⇒ status=failed 且错误信息含「文本提取失败」；
// 纯文本（.txt）仍正常入库。

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

func waitKBDocStatus(t *testing.T, svc *KnowledgeBaseService, id uint, want model.KBDocumentStatus) *model.KBDocument {
	t.Helper()
	_ = svc // 轮询改走仓储（批次7 刀4：service 不再持 db 句柄），全局即测试库
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		doc, err := knowledgerepo.NewKBDocumentRepository(db.GetDB()).GetByID(context.Background(), id)
		if err == nil && doc.Status == want {
			return doc
		}
		time.Sleep(50 * time.Millisecond)
	}
	doc, _ := knowledgerepo.NewKBDocumentRepository(db.GetDB()).GetByID(context.Background(), id)
	t.Fatalf("文档 %d 未在 20s 内进入 %s（实际 %s / err=%s）", id, want, doc.Status, doc.ErrorMsg)
	return doc
}

func TestKBImport_FakePDFMarksFailed(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.KBDocument{}) // 装配全局测试句柄（轮询改走仓储）
	svc := NewKnowledgeBaseService()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake.pdf")
	// 合法 PDF 魔数 + 垃圾正文：ExtractText 解析不出文本 ⇒ 必须失败，不得当正文入库
	if err := os.WriteFile(path, []byte("%PDF-1.4\nthis is not a real pdf body"), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	doc := &model.KBDocument{Title: "伪PDF", FilePath: path, FileType: ".pdf", Status: model.KBDocumentStatusPending}
	if err := svc.docs.Create(context.Background(), doc); err != nil {
		t.Fatalf("种文档失败: %v", err)
	}

	svc.processDocumentAsyncWithQA(context.Background(), doc.ID, path, false)
	got := waitKBDocStatus(t, svc, doc.ID, model.KBDocumentStatusFailed)
	if !strings.Contains(got.ErrorMsg, "文本提取失败") {
		t.Errorf("错误信息应指明文本提取失败，实际 %q", got.ErrorMsg)
	}
	if strings.Contains(got.Content, "%PDF") {
		t.Errorf("二进制原文被写进 content 字段（G13 回归）")
	}
}

func TestKBImport_PlainTextStillIndexes(t *testing.T) {
	requireEmbeddingService(t) // 真向量用例：无栈环境显式跳过（审计交付配套）

	_ = testutil.NewTestDB(t, &model.KBDocument{}) // 装配全局测试句柄（轮询改走仓储）
	svc := NewKnowledgeBaseService()
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("正常纯文本正文内容。", 40)), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	doc := &model.KBDocument{Title: "正常", FilePath: path, FileType: ".txt", Status: model.KBDocumentStatusPending}
	if err := svc.docs.Create(context.Background(), doc); err != nil {
		t.Fatalf("种文档失败: %v", err)
	}
	svc.processDocumentAsyncWithQA(context.Background(), doc.ID, path, false)
	got := waitKBDocStatus(t, svc, doc.ID, model.KBDocumentStatusIndexed)
	if got.ChunkCount == 0 {
		t.Errorf("正常纯文本应有分段，实际 0")
	}
}

// requireEmbeddingService 探测本机 embedding 服务可达性（2026-11-17 审计交付配套）：
// 这些是**真向量集成用例**，依赖 127.0.0.1:8208 推理栈；本机按 LOCAL-ENV.md 硬约束
// 不部署推理栈 ⇒ 显式 Skip（不再假红），有栈机器/CI 装栈后照常执行。
func requireEmbeddingService(t *testing.T) {
	t.Helper()
	if os.Getenv("HIVEMTK_FORCE_EMBEDDING_TESTS") == "1" {
		return
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8208", 500*time.Millisecond)
	if err != nil {
		t.Skipf("本机未部署推理栈（127.0.0.1:8208 不可达，LOCAL-ENV.md 硬约束）：真向量用例跳过；装栈后或设 HIVEMTK_FORCE_EMBEDDING_TESTS=1 重跑")
	}
	_ = conn.Close()
}
