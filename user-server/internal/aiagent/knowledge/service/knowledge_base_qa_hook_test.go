package service

// knowledge_base_qa_hook_test.go P1 观察项②「上传文档可选 QA 增强」的验收。
//
// 判据三格：
//  1. qa_split=true → 文档入库成功后投递一次拆分任务（docID 与文档行一致）；
//  2. qa_split=false（默认）→ 一次都不投递（行为与历史完全一致）；
//  3. 要了增强但服务未装配 → 不 panic、不投递，只记日志（降级不阻塞入库）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// newKBImportTestService 建 KB 服务 + 文件夹具；返回已注入的投递替身。
func newKBImportTestService(t *testing.T) (*KnowledgeBaseService, *recordingQA, string) {
	t.Helper()
	_ = testutil.NewTestDB(t, &model.KBDocument{})
	svc := NewKnowledgeBaseService()

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	body := strings.Repeat("这是一段用于验证上传文档 QA 增强投递时机的正文内容。", 40)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	rec := &recordingQA{}
	svc.qaEnqueue = rec.enqueue
	return svc, rec, path
}

type recordingQA struct {
	mu   sync.Mutex
	ids  []uint
	wait chan struct{}
}

func (r *recordingQA) enqueue(docID, _ uint) {
	r.mu.Lock()
	r.ids = append(r.ids, docID)
	if r.wait != nil {
		close(r.wait)
		r.wait = nil
	}
	r.mu.Unlock()
}

func (r *recordingQA) snapshot() []uint {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint(nil), r.ids...)
}

func TestKBImport_QASplitEnqueuedOnlyWhenRequested(t *testing.T) {
	svc, rec, path := newKBImportTestService(t)
	// 先持住 channel 再启动：替身在投递后会把 rec.wait 置 nil，
	// select 直接读字段会读到 nil 通道（永远阻塞）——这正是第一版超时的原因。
	wait := make(chan struct{})
	rec.wait = wait

	if err := svc.docs.Create(context.Background(), &model.KBDocument{
		Title: "带增强", FilePath: path, FileType: ".txt", Status: model.KBDocumentStatusPending,
	}); err != nil {
		t.Fatalf("种文档失败: %v", err)
	}
	// 直接驱动处理流水线（跳过 multipart，投递时机才是被测点）
	svc.processDocumentAsyncWithQA(context.Background(), 1, path, true)

	select {
	case <-wait:
	case <-time.After(20 * time.Second):
		t.Fatal("超时：开启 QA 增强后未投递拆分任务")
	}
	ids := rec.snapshot()
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("投递记录 = %v，期望恰好一次且 docID=1", ids)
	}
}

func TestKBImport_QASplitOffKeepsLegacyBehavior(t *testing.T) {
	svc, rec, path := newKBImportTestService(t)
	if err := svc.docs.Create(context.Background(), &model.KBDocument{
		Title: "默认关", FilePath: path, FileType: ".txt", Status: model.KBDocumentStatusPending,
	}); err != nil {
		t.Fatalf("种文档失败: %v", err)
	}
	svc.processDocumentAsyncWithQA(context.Background(), 1, path, false)

	// 等文档处理完成再断言（异步流水线；处理完仍不该有投递）
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if doc, err := knowledgerepo.NewKBDocumentRepository(db.GetDB()).GetByID(context.Background(), 1); err == nil && doc.Status == model.KBDocumentStatusIndexed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("未请求增强却投递了拆分任务: %v", got)
	}
}

func TestKBImport_QASplitRequestedButNotWired(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KBDocument{})
	svc := NewKnowledgeBaseService() // 不注入 qaEnqueue/qaSplit
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("未装配拆分服务时的正文。", 30)), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	if err := svc.docs.Create(context.Background(), &model.KBDocument{
		Title: "未装配", FilePath: path, FileType: ".txt", Status: model.KBDocumentStatusPending,
	}); err != nil {
		t.Fatalf("种文档失败: %v", err)
	}
	// 不应 panic；文档仍应正常入库
	svc.processDocumentAsyncWithQA(context.Background(), 1, path, true)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var doc model.KBDocument
		if err := db.First(&doc, 1).Error; err == nil && doc.Status == model.KBDocumentStatusIndexed {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("未装配拆分服务时文档未能正常入库（增强不该阻塞主链路）")
}
