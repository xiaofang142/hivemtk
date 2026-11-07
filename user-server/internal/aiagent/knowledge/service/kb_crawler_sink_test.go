// kb_crawler_sink_test.go —— 爬取落库路径下沉仓储后的读写契约回归
// （AV-04c 批次7 刀3）。
//
// kb_crawler.go 的爬取源/批次/入库 SQL 下沉 knowledgeRepo.KBCrawlerRepository 后，
// 本文件锁 service 侧爬取落库路径：建源校验/配额、批次状态机、IngestCrawledPage
// 的「新建文档」与「变更重建（旧 chunks 标 superseded + 内容回写）」两分支、
// 错误路径语义（批次不存在/非 pending 不可执行/仅失败可重试）逐项不变。
package service

import (
	"context"
	"testing"

	kbcrawler "hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// kbDocumentChunkFixture 测试夹具：kb_document_chunks 行（正式模型在 root
// repository 包，aiagent 测试不引 root 包），列集只含本测试用到的列。
type kbDocumentChunkFixture struct {
	TenantID     uint   `gorm:"not null;default:1"`
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	DocumentID   uint   `gorm:"index;not null"`
	ChunkIndex   int    `gorm:"not null;default:0"`
	ContentHash  string `gorm:"type:varchar(64);not null"`
	ChunkContent string `gorm:"type:text"`
	Status       string `gorm:"type:varchar(20);default:'active';index"`
}

func (kbDocumentChunkFixture) TableName() string { return "kb_document_chunks" }

func setupCrawlerSinkTestDB(t *testing.T) (*CrawlerService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.KnowledgeBase{},
		&model.KBCrawlerSource{},
		&model.KBCrawlBatch{},
		&model.KBDocument{},
		&kbDocumentChunkFixture{},
	)
	repo := knowledgeRepo.NewKBCrawlerRepository(db)
	// kb 必须是真实服务：IngestCrawledPage/Publish 的后台 goroutine 调
	// kb.ProcessDocumentByID（nil 接收者会在 defer 恢复路径二次 panic）。
	kb := NewKnowledgeBaseServiceWithDocs(knowledgeRepo.NewKBDocumentRepository(db))
	svc := NewCrawlerServiceWithRepos(repo, kb)
	return svc, db
}

// 建源：归属校验 + 配额 + 默认值夹取，落库可查。
func TestCrawlerSink_CreateSource(t *testing.T) {
	svc, db := setupCrawlerSinkTestDB(t)
	ctx := context.Background()

	if err := db.Create(&model.KnowledgeBase{Name: "crawl-kb", Type: "rag"}).Error; err != nil {
		t.Fatalf("seed kb: %v", err)
	}

	src, err := svc.CreateSource(ctx, &CreateSourceInput{
		KBID:       1,
		SourceType: "url",
		StartURL:   "https://example.com/docs",
		MaxPages:   0, // 触发默认值夹取
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if src.ID == 0 || src.MaxPages != DefaultMaxPages || src.SiteVerifyToken == "" {
		t.Fatalf("建源默认值不符: %+v", src)
	}

	// 非法 source_type / scheme / KB 不存在三格拒绝路径不变。
	if _, err := svc.CreateSource(ctx, &CreateSourceInput{KBID: 1, SourceType: "ftp", StartURL: "https://a.com"}); err == nil {
		t.Fatal("source_type=ftp 应被拒绝")
	}
	if _, err := svc.CreateSource(ctx, &CreateSourceInput{KBID: 1, SourceType: "url", StartURL: "ftp://a.com"}); err == nil {
		t.Fatal("scheme=ftp 应被拒绝")
	}
	if _, err := svc.CreateSource(ctx, &CreateSourceInput{KBID: 99, SourceType: "url", StartURL: "https://a.com"}); err == nil || err.Error() != "目标知识库不存在" {
		t.Fatalf("KB 不存在应拒绝，got %v", err)
	}
}

// IngestCrawledPage 新建分支：落 kb_documents（.html 约定键）。
func TestCrawlerSink_IngestNewDocument(t *testing.T) {
	svc, db := setupCrawlerSinkTestDB(t)
	ctx := context.Background()

	src := &model.KBCrawlerSource{StartURL: "https://example.com"}
	if err := db.Create(src).Error; err != nil {
		t.Fatalf("seed src: %v", err)
	}

	docID, isNew, err := svc.IngestCrawledPage(ctx, src, "https://example.com/p1", &kbcrawler.Page{Title: "p1", Content: "hello world"})
	if err != nil || !isNew || docID == 0 {
		t.Fatalf("新建入库: id=%d isNew=%v err=%v", docID, isNew, err)
	}
	var doc model.KBDocument
	if err := db.First(&doc, docID).Error; err != nil {
		t.Fatalf("读回: %v", err)
	}
	if doc.Title != "https://example.com/p1" || doc.FileType != ".html" || doc.Content != "hello world" {
		t.Fatalf("约定键/内容不符: %+v", doc)
	}

	// 同 URL 再次入库 → 变更重建分支（isNew=false，内容回写）。
	docID2, isNew2, err := svc.IngestCrawledPage(ctx, src, "https://example.com/p1", &kbcrawler.Page{Title: "p1", Content: "changed content"})
	if err != nil || isNew2 || docID2 != docID {
		t.Fatalf("变更重建: id=%d isNew=%v err=%v", docID2, isNew2, err)
	}
	var updated model.KBDocument
	if err := db.First(&updated, docID).Error; err != nil {
		t.Fatalf("读回: %v", err)
	}
	// 只断言内容回写：后台 goroutine（ProcessDocumentByID）会把状态推进到
	// processing，与断言存在时序竞态，状态断言不在此锁。
	if updated.Content != "changed content" {
		t.Fatalf("变更内容未回写: %+v", updated)
	}
}

// 变更重建分支把旧 active chunks 标 superseded（仓储直查可证）。
func TestCrawlerSink_IngestSupersedesChunks(t *testing.T) {
	svc, db := setupCrawlerSinkTestDB(t)
	ctx := context.Background()

	src := &model.KBCrawlerSource{StartURL: "https://example.com"}
	if err := db.Create(src).Error; err != nil {
		t.Fatalf("seed src: %v", err)
	}
	docID, _, err := svc.IngestCrawledPage(ctx, src, "https://example.com/p2", &kbcrawler.Page{Title: "p2", Content: "v1"})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	chunk := &kbDocumentChunkFixture{DocumentID: docID, ContentHash: "hash-sink", ChunkContent: "old chunk", Status: "active"}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatalf("seed chunk: %v", err)
	}

	if _, isNew, err := svc.IngestCrawledPage(ctx, src, "https://example.com/p2", &kbcrawler.Page{Title: "p2", Content: "v2"}); err != nil || isNew {
		t.Fatalf("重建入库: err=%v isNew=%v", err, isNew)
	}
	var after kbDocumentChunkFixture
	if err := db.First(&after, chunk.ID).Error; err != nil {
		t.Fatalf("读回 chunk: %v", err)
	}
	if after.Status != "superseded" {
		t.Fatalf("旧 chunk 应标 superseded，got %q", after.Status)
	}
}

// 批次状态机：状态校验、重试上限、错误路径语义不变。
func TestCrawlerSink_BatchStateMachine(t *testing.T) {
	svc, db := setupCrawlerSinkTestDB(t)
	ctx := context.Background()

	src := &model.KBCrawlerSource{StartURL: "https://example.com", Status: model.KBCrawlerSourceStatusActive}
	if err := db.Create(src).Error; err != nil {
		t.Fatalf("seed src: %v", err)
	}

	// 未验证源拒绝触发（硬门槛）。
	if _, err := svc.CreateBatch(ctx, src.ID, "manual"); err == nil {
		t.Fatal("SiteVerified=false 应拒绝触发")
	}
	// 补验证后可触发。
	if err := db.Model(src).Update("site_verified", true).Error; err != nil {
		t.Fatalf("verify: %v", err)
	}
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil || batch.Status != model.KBCrawlBatchStatusPending {
		t.Fatalf("CreateBatch: %+v err=%v", batch, err)
	}

	// 非 pending 批次不可执行。
	if err := db.Model(batch).Update("status", model.KBCrawlBatchStatusRunning).Error; err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := svc.RunBatch(ctx, batch.ID); err == nil || err.Error() != "批次状态为 running，不可执行" {
		t.Fatalf("running 批次应拒绝执行，got %v", err)
	}

	// 仅 failed 批次可重试。
	if _, err := svc.RetryBatch(ctx, batch.ID); err == nil || err.Error() != "仅失败批次可重试" {
		t.Fatalf("running 批次应拒绝重试，got %v", err)
	}
	// 不存在批次。
	if _, err := svc.RetryBatch(ctx, 99999); err == nil || err.Error() != "批次不存在" {
		t.Fatalf("不存在批次应拒绝重试，got %v", err)
	}
	if err := svc.RunBatch(ctx, 99999); err == nil || err.Error() != "批次不存在" {
		t.Fatalf("不存在批次应拒绝执行，got %v", err)
	}

	// failed 批次重试建新行（RetryCount+1）。
	if err := db.Model(batch).Update("status", model.KBCrawlBatchStatusFailed).Error; err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	retry, err := svc.RetryBatch(ctx, batch.ID)
	if err != nil || retry.ID == batch.ID || retry.RetryCount != batch.RetryCount+1 {
		t.Fatalf("重试批次: %+v err=%v", retry, err)
	}
	// 达到重试上限拒绝。
	if err := db.Model(retry).Update("retry_count", MaxRetryPerBatch).Error; err != nil {
		t.Fatalf("set retry_count: %v", err)
	}
	if _, err := svc.RetryBatch(ctx, retry.ID); err == nil {
		t.Fatal("达到重试上限应拒绝")
	}

	// 源状态跃迁：合法跃迁回写，非法跃迁拒绝。
	if err := svc.TransitionSource(ctx, src.ID, model.KBCrawlerSourceStatusPaused); err != nil {
		t.Fatalf("active→paused: %v", err)
	}
	var paused model.KBCrawlerSource
	if err := db.First(&paused, src.ID).Error; err != nil || paused.Status != model.KBCrawlerSourceStatusPaused || paused.ErrorMsg != "" {
		t.Fatalf("跃迁回写: %+v err=%v", paused, err)
	}
	if err := svc.TransitionSource(ctx, src.ID, model.KBCrawlerSourceStatusActive); err != nil {
		t.Fatalf("paused→active: %v", err)
	}
	if err := svc.TransitionSource(ctx, src.ID, "bogus"); err == nil {
		t.Fatal("非法跃迁应拒绝")
	}
}
