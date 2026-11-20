package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	kbcrawler "hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type crawlerTransportFunc func(*http.Request) (*http.Response, error)

func (f crawlerTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCrawlerRecovery_RejectsFailedRunningWrite(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	src := &model.KBCrawlerSource{StartURL: "https://example.com", SourceType: "url", MaxPages: 1,
		SiteVerified: true, Status: model.KBCrawlerSourceStatusActive}
	if err := database.Create(src).Error; err != nil {
		t.Fatal(err)
	}
	batch, err := svc.CreateBatch(context.Background(), src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("合成 running 写入失败")
	if err := database.Callback().Update().Before("gorm:update").Register("test:reject_crawler_running", func(tx *gorm.DB) {
		if tx.Statement.Table == "kb_crawl_batches" {
			if fields, ok := tx.Statement.Dest.(map[string]any); ok && fields["status"] == model.KBCrawlBatchStatusRunning {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Callback().Update().Remove("test:reject_crawler_running") })
	requests := 0
	svc.http = &http.Client{Transport: crawlerTransportFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("测试禁止网络")
	})}
	if err := svc.RunBatch(context.Background(), batch.ID); !errors.Is(err, injected) {
		t.Fatalf("运行状态落库失败必须返回原错误，got %v", err)
	}
	if requests != 0 {
		t.Fatalf("批次未声明成功不应开始网络抓取，got %d", requests)
	}
}

func TestCrawlerRecovery_PartiallyFailedBatchCanRetry(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	batch := &model.KBCrawlBatch{SourceID: 1, BatchNo: "partial-fixture", Trigger: "manual",
		Status: model.KBCrawlBatchStatusPartiallyFailed}
	if err := database.Create(batch).Error; err != nil {
		t.Fatal(err)
	}
	retry, err := svc.RetryBatch(context.Background(), batch.ID)
	if err != nil || retry.RetryCount != 1 || retry.Status != model.KBCrawlBatchStatusPending {
		t.Fatalf("部分失败必须可进入持久化重试，retry=%+v err=%v", retry, err)
	}
}

func crawlerRecoverySource(t *testing.T, database *gorm.DB, ctx context.Context, qa bool) *model.KBCrawlerSource {
	t.Helper()
	src := &model.KBCrawlerSource{TenantID: tenant.WriteTenantID(ctx), StartURL: "https://example.com/docs", SourceType: "url", MaxPages: 1,
		QASplitEnabled: &qa, SiteVerified: true, Status: model.KBCrawlerSourceStatusActive}
	if err := database.WithContext(ctx).Create(src).Error; err != nil {
		t.Fatal(err)
	}
	return src
}

func crawlerRecoveryHTTP() *http.Client {
	return &http.Client{Transport: crawlerTransportFunc(func(req *http.Request) (*http.Response, error) {
		body := "User-agent: *\nAllow: /\n"
		if req.URL.Path != "/robots.txt" {
			body = "<title>合成文档</title><main>" + strings.Repeat("This is synthetic crawler content. ", 20) + "</main>"
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
}

func TestCrawlerRecovery_VectorFailureRetriesSameHash(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	svc.http = crawlerRecoveryHTTP()
	injected := errors.New("合成向量服务失败")
	embedding := &crawlerTestEmbedding{fail: injected}
	svc.kb.vector = ragretrieval.NewVectorizer(EmbeddingDim(), embedding)
	counts := crawlCounters{}
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); !errors.Is(err, injected) {
		t.Fatalf("向量失败必须向上传递: %v", err)
	}
	var page model.KBCrawlPage
	if err := database.First(&page).Error; err != nil {
		t.Fatal(err)
	}
	if page.CompletedHash != "" || page.ErrorMsg == "" {
		t.Fatalf("失败不得记完成: %+v", page)
	}
	embedding.fail = nil
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); err != nil {
		t.Fatal(err)
	}
	if embedding.calls != 2 || counts.unchanged != 0 {
		t.Fatalf("同内容失败后必须重试向量服务: calls=%d counts=%+v", embedding.calls, counts)
	}
	if err := database.First(&page, page.ID).Error; err != nil {
		t.Fatal(err)
	}
	if page.CompletedHash == "" || page.CompletedHash != page.ContentHash || page.ErrorMsg != "" {
		t.Fatalf("重试成功才记完成: %+v", page)
	}
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); err != nil {
		t.Fatal(err)
	}
	if embedding.calls != 2 || counts.unchanged != 1 {
		t.Fatalf("本进程成功内容可跳过: calls=%d counts=%+v", embedding.calls, counts)
	}
}

type crawlerContextEmbedding struct {
	crawlerTestEmbedding
	t      *testing.T
	cancel context.CancelFunc
}

func (e *crawlerContextEmbedding) Embed(ctx context.Context, _ *llm.EmbeddingConfig, _ []string) ([][]float32, error) {
	e.calls++
	if id, ok := tenant.FromContext(ctx); !ok || id != 41 {
		e.t.Errorf("embedding tenant = %d/%t, want 41/true", id, ok)
	}
	if _, ok := ctx.Deadline(); !ok {
		e.t.Error("embedding lost batch deadline")
	}
	e.cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		e.t.Errorf("embedding cancellation = %v, want context.Canceled", ctx.Err())
	}
	return nil, context.Canceled
}

func TestCrawlerRecovery_EmbeddingRetainsBoundedTenantContext(t *testing.T) {
	t.Setenv("TENANT_MODE", "on")
	svc, database := setupCrawlerSinkTestDB(t)
	ctx, cancel := context.WithCancel(tenant.WithTenantID(context.Background(), 41))
	defer cancel()
	src := crawlerRecoverySource(t, database, ctx, false)
	svc.http = crawlerRecoveryHTTP()
	embedding := &crawlerContextEmbedding{t: t, cancel: cancel}
	svc.kb.vector = ragretrieval.NewVectorizer(EmbeddingDim(), embedding)
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunBatch(ctx, batch.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunBatch cancellation = %v, want context.Canceled", err)
	}
	if embedding.calls != 1 {
		t.Fatalf("embedding calls = %d, want 1", embedding.calls)
	}
	readCtx := tenant.WithTenantID(context.Background(), 41)
	if err := database.WithContext(readCtx).First(batch, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	var doc model.KBDocument
	if err := database.WithContext(readCtx).First(&doc).Error; err != nil {
		t.Fatal(err)
	}
	var checkpoint model.KBCrawlPage
	if err := database.WithContext(readCtx).First(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if batch.Status != model.KBCrawlBatchStatusFailed || doc.Status != model.KBDocumentStatusFailed || checkpoint.CompletedHash != "" {
		t.Fatalf("canceled work must persist failure without completion: batch=%s document=%s completed=%q", batch.Status, doc.Status, checkpoint.CompletedHash)
	}
}

type crawlerRecoveryLLM struct {
	out   string
	fail  error
	calls int
}

func (f *crawlerRecoveryLLM) Complete(context.Context, string) (string, error) {
	f.calls++
	return f.out, f.fail
}

func TestCrawlerRecovery_RestartRebuildsIndexWithoutDuplicateQA(t *testing.T) {
	for _, output := range []string{`[{"question":"合成问题","answer":"合成答案","answer_anchor":"synthetic"}]`, `[]`} {
		t.Run(output, func(t *testing.T) {
			svc, database := setupCrawlerSinkTestDB(t)
			ctx := context.Background()
			src := crawlerRecoverySource(t, database, ctx, true)
			llm := &crawlerRecoveryLLM{out: output}
			svc.SetQASplitService(NewQASplitServiceWithRepos(svc.repo, knowledgeRepo.NewQACandidateRepository(database), llm))
			page := &kbcrawler.Page{Title: "文档", Content: "This is synthetic indexed content."}
			id, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page)
			if err != nil {
				t.Fatal(err)
			}
			var before int64
			if err := database.Model(&model.QACandidate{}).Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			// A fresh service has a new empty in-memory index, while durable QA remains.
			kb := NewKnowledgeBaseServiceWithDocs(knowledgeRepo.NewKBDocumentRepository(database))
			embedding := &crawlerTestEmbedding{}
			kb.vector = ragretrieval.NewVectorizer(EmbeddingDim(), embedding)
			restarted := NewCrawlerServiceWithRepos(svc.repo, kb)
			restarted.SetQASplitService(svc.qa)
			idAgain, _, err := restarted.IngestCrawledPage(ctx, src, src.StartURL, page)
			if err != nil || idAgain != id || embedding.calls != 1 || llm.calls != 1 {
				t.Fatalf("重启必须重建索引且不重复 QA: id=%d embed=%d qa=%d err=%v", idAgain, embedding.calls, llm.calls, err)
			}
			var after int64
			if err := database.Model(&model.QACandidate{}).Count(&after).Error; err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("重启重复创建候选: before=%d after=%d", before, after)
			}
		})
	}
}

func TestCrawlerRecovery_QAFailureAndCheckpointWriteAreRetryable(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, true)
	injected := errors.New("合成 QA 失败")
	llm := &crawlerRecoveryLLM{fail: injected, out: `[{"question":"Q","answer":"A"}]`}
	svc.SetQASplitService(NewQASplitServiceWithRepos(svc.repo, knowledgeRepo.NewQACandidateRepository(database), llm))
	page := &kbcrawler.Page{Title: "文档", Content: "Synthetic retryable content"}
	if _, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); !errors.Is(err, injected) {
		t.Fatalf("QA 失败必须传递: %v", err)
	}
	llm.fail = nil
	writeFailure := errors.New("合成检查点写入失败")
	if err := database.Callback().Update().Before("gorm:update").Register("test:reject_page_complete", func(tx *gorm.DB) {
		if tx.Statement.Table == "kb_crawl_pages" {
			if fields, ok := tx.Statement.Dest.(map[string]any); ok && fields["qa_hash"] != nil {
				tx.AddError(writeFailure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); !errors.Is(err, writeFailure) {
		t.Fatalf("检查点失败必须传递: %v", err)
	}
	var candidates int64
	if err := database.Model(&model.QACandidate{}).Count(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	if candidates != 0 {
		t.Fatalf("检查点失败必须回滚候选行: %d", candidates)
	}
	database.Callback().Update().Remove("test:reject_page_complete")
	if _, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&model.QACandidate{}).Count(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	if candidates != 1 {
		t.Fatalf("成功后应只创建一次候选: %d", candidates)
	}
}

func TestCrawlerRecovery_TenantIsolationAndMissingContext(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	a := tenant.WithTenantID(context.Background(), 41)
	b := tenant.WithTenantID(context.Background(), 42)
	srcA := crawlerRecoverySource(t, database, a, false)
	srcB := crawlerRecoverySource(t, database, b, false)
	page := &kbcrawler.Page{Title: "same", Content: "same synthetic content"}
	idA, _, err := svc.IngestCrawledPage(a, srcA, srcA.StartURL, page)
	if err != nil {
		t.Fatal(err)
	}
	idB, _, err := svc.IngestCrawledPage(b, srcB, srcB.StartURL, page)
	if err != nil || idA == idB {
		t.Fatalf("相同 URL 不得跨租户共用文档: %d/%d err=%v", idA, idB, err)
	}
	if _, _, err := svc.IngestCrawledPage(a, srcB, srcB.StartURL, page); err == nil {
		t.Fatal("必须拒绝跨租户来源")
	}
	if _, _, err := svc.IngestCrawledPage(context.Background(), srcA, srcA.StartURL, page); err == nil {
		t.Fatal("多租户缺上下文必须拒绝")
	}
	batch, err := svc.CreateBatch(a, srcA.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunBatch(b, batch.ID); err == nil {
		t.Fatal("B 不得执行 A 批次")
	}
}

func TestCrawlerRecovery_ClaimSerializesAndFencesExpiredWorker(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	first, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i, id := range []uint64{first.ID, second.ID} {
		workers.Add(1)
		go func(id uint64, token string) {
			defer workers.Done()
			<-start
			results <- svc.repo.ClaimBatch(ctx, id, token, time.Now())
		}(id, []string{"first", "second"}[i])
	}
	close(start)
	workers.Wait()
	close(results)
	success, busy := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, knowledgeRepo.ErrCrawlSourceBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || busy != 1 {
		t.Fatalf("同源并发只能有一个租约: success=%d busy=%d", success, busy)
	}
	var running model.KBCrawlBatch
	if err := database.Where("status = ?", model.KBCrawlBatchStatusRunning).First(&running).Error; err != nil {
		t.Fatal(err)
	}
	oldCtx := knowledgeRepo.WithCrawlLease(ctx, knowledgeRepo.CrawlLease{BatchID: running.ID, Token: running.LeaseToken})
	if err := database.Model(&running).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.ClaimBatch(ctx, running.ID, "recovered", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinishBatch(oldCtx, running.ID, map[string]any{"status": model.KBCrawlBatchStatusSucceeded}, map[string]any{"last_batch_id": "stale"}); !errors.Is(err, knowledgeRepo.ErrCrawlLeaseLost) {
		t.Fatalf("旧执行者必须被围栏拒绝: %v", err)
	}
	newCtx := knowledgeRepo.WithCrawlLease(ctx, knowledgeRepo.CrawlLease{BatchID: running.ID, Token: "recovered"})
	if err := svc.repo.FinishBatch(newCtx, running.ID, map[string]any{"status": model.KBCrawlBatchStatusSucceeded}, map[string]any{"last_batch_id": "recovered"}); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlerRecovery_ResumesPendingManualBatchAfterRestart(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewCrawlerServiceWithRepos(svc.repo, svc.kb)
	restarted.http = crawlerRecoveryHTTP()
	if err := restarted.RecoverPending(ctx); err != nil {
		t.Fatal(err)
	}
	var completed model.KBCrawlBatch
	if err := database.First(&completed, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.KBCrawlBatchStatusSucceeded || completed.PagesNew != 1 || completed.LeaseToken != "" {
		t.Fatalf("重启后未恢复完成手工任务: %+v", completed)
	}
}

func TestCrawlerRecovery_FailedChangeThenRevertDoesNotReuseOldHash(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	text := "original synthetic content "
	svc.http = &http.Client{Transport: crawlerTransportFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<title>test</title><main>" + strings.Repeat(text, 30) + "</main>")), Header: make(http.Header), Request: req}, nil
	})}
	embedding := &crawlerTestEmbedding{}
	svc.kb.vector = ragretrieval.NewVectorizer(EmbeddingDim(), embedding)
	counts := crawlCounters{}
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); err != nil {
		t.Fatal(err)
	}
	text = "changed synthetic content "
	embedding.fail = errors.New("合成变更索引失败")
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); err == nil {
		t.Fatal("变更索引应失败")
	}
	text = "original synthetic content "
	embedding.fail = nil
	if err := svc.crawlOne(ctx, src, src.StartURL, &counts); err != nil {
		t.Fatal(err)
	}
	if embedding.calls != 3 || counts.unchanged != 0 {
		t.Fatalf("恢复旧内容也必须重新完成: calls=%d counts=%+v", embedding.calls, counts)
	}
}

func TestCrawlerRecovery_DocumentStatusWriteFailuresDoNotComplete(t *testing.T) {
	for _, status := range []model.KBDocumentStatus{model.KBDocumentStatusProcessing, model.KBDocumentStatusIndexed} {
		t.Run(string(status), func(t *testing.T) {
			svc, database := setupCrawlerSinkTestDB(t)
			ctx := context.Background()
			src := crawlerRecoverySource(t, database, ctx, false)
			injected := errors.New("合成文档状态写入失败")
			if err := database.Callback().Update().Before("gorm:update").Register("test:reject_doc_status", func(tx *gorm.DB) {
				if fields, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "kb_documents" && fields["status"] == status {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			page := &kbcrawler.Page{Title: "test", Content: "synthetic content"}
			if _, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); !errors.Is(err, injected) {
				t.Fatalf("状态失败不得吞掉: %v", err)
			}
			var checkpoint model.KBCrawlPage
			if err := database.First(&checkpoint).Error; err != nil {
				t.Fatal(err)
			}
			if checkpoint.CompletedHash != "" || checkpoint.ErrorMsg == "" {
				t.Fatalf("失败不得完成: %+v", checkpoint)
			}
			database.Callback().Update().Remove("test:reject_doc_status")
			if _, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCrawlerRecovery_FinalizationFailureRollsBackSourceSummary(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	svc.http = crawlerRecoveryHTTP()
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("合成批次完成写入失败")
	if err := database.Callback().Update().Before("gorm:update").Register("test:reject_batch_finish", func(tx *gorm.DB) {
		if fields, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "kb_crawl_batches" && fields["status"] == model.KBCrawlBatchStatusSucceeded {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunBatch(ctx, batch.ID); !errors.Is(err, injected) {
		t.Fatalf("完成失败必须返回: %v", err)
	}
	if err := database.First(src, src.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(batch, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if src.LastCrawlAt != nil || batch.Status != model.KBCrawlBatchStatusRunning || batch.LeaseToken == "" {
		t.Fatalf("完成事务失败应保留恢复点且不更新源: src=%+v batch=%+v", src, batch)
	}
	database.Callback().Update().Remove("test:reject_batch_finish")
	if err := database.Model(batch).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverPending(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlerRecovery_LeaseCannotMutateAnotherSource(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	first := crawlerRecoverySource(t, database, ctx, false)
	second := crawlerRecoverySource(t, database, ctx, false)
	batch, err := svc.CreateBatch(ctx, first.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateBatch(ctx, second.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.ClaimBatch(ctx, batch.ID, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	leased := knowledgeRepo.WithCrawlLease(ctx, knowledgeRepo.CrawlLease{BatchID: batch.ID, Token: "owner"})
	if _, _, err := svc.IngestCrawledPage(leased, second, second.StartURL, &kbcrawler.Page{Content: "other"}); !errors.Is(err, knowledgeRepo.ErrCrawlLeaseLost) {
		t.Fatalf("租约不得覆盖另一来源: %v", err)
	}
	if err := svc.repo.FinishBatch(leased, other.ID, map[string]any{"status": model.KBCrawlBatchStatusSucceeded}, map[string]any{"last_batch_id": "wrong"}); !errors.Is(err, knowledgeRepo.ErrCrawlLeaseLost) {
		t.Fatalf("租约不得完成另一批次: %v", err)
	}
}

type crawlerFailingIndex struct {
	ragretrieval.IndexManagerInterface
	err error
}

func (index crawlerFailingIndex) BuildIndex(context.Context, string, []ragretrieval.Chunk) error {
	return index.err
}

func TestCrawlerRecovery_IndexFailureIsRetryable(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx := context.Background()
	src := crawlerRecoverySource(t, database, ctx, false)
	original := svc.kb.indexer
	injected := errors.New("合成索引写入失败")
	svc.kb.indexer = crawlerFailingIndex{IndexManagerInterface: original, err: injected}
	page := &kbcrawler.Page{Title: "合成标题", Content: strings.Repeat("Synthetic index retry content. ", 20)}
	docID, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page)
	if !errors.Is(err, injected) {
		t.Fatalf("索引错误必须传播: %v", err)
	}
	var doc model.KBDocument
	if err := database.First(&doc, docID).Error; err != nil {
		t.Fatal(err)
	}
	var checkpoint model.KBCrawlPage
	if err := database.Where("document_id = ?", docID).First(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if doc.Status != model.KBDocumentStatusFailed || checkpoint.CompletedHash != "" {
		t.Fatalf("索引失败不能写完成: doc=%+v checkpoint=%+v", doc, checkpoint)
	}
	svc.kb.indexer = original
	if retryID, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, page); err != nil || retryID != docID {
		t.Fatalf("相同内容必须可以重试原文档: id=%d err=%v", retryID, err)
	}
}

func TestCrawlerRecovery_CancellationFinalizesAsFailed(t *testing.T) {
	svc, database := setupCrawlerSinkTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := crawlerRecoverySource(t, database, ctx, false)
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	svc.http = &http.Client{Transport: crawlerTransportFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})}
	if err := svc.RunBatch(ctx, batch.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消必须向调用方返回: %v", err)
	}
	if err := database.First(batch, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if batch.Status != model.KBCrawlBatchStatusFailed || batch.FinishedAt == nil || batch.LeaseToken != "" {
		t.Fatalf("取消必须落失败终态并释放租约: %+v", batch)
	}
}
