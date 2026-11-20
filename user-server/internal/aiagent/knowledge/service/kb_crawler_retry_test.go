package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	kbcrawler "hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// These upstream regressions use real transactions now that crawling commits
// source/document/checkpoint state together. Only external model/HTTP edges and
// explicit failure injection are controlled fixtures.
type fa12CrawlerFixture struct {
	t                                            *testing.T
	db                                           *gorm.DB
	queryErr, chunkErr, bodyErr, batchErr, qaErr error
	queryCalls, creates, httpCalls               int
	contexts                                     []context.Context
}

func newFA12CrawlerFixture(t *testing.T) *fa12CrawlerFixture {
	t.Helper()
	_, db := setupCrawlerSinkTestDB(t)
	f := &fa12CrawlerFixture{t: t, db: db}
	const name = "test:fa12_upstream_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		f.queryCalls++
		f.contexts = append(f.contexts, tx.Statement.Context)
		if f.queryErr != nil {
			tx.AddError(f.queryErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		f.contexts = append(f.contexts, tx.Statement.Context)
		if tx.Statement.Table == "kb_documents" {
			f.creates++
		}
		if tx.Statement.Table == "qa_candidates" && f.qaErr != nil {
			tx.AddError(f.qaErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		f.contexts = append(f.contexts, tx.Statement.Context)
		switch tx.Statement.Table {
		case "kb_document_chunks":
			if f.chunkErr != nil {
				tx.AddError(f.chunkErr)
			}
		case "kb_documents":
			if fields, ok := tx.Statement.Dest.(map[string]any); ok && fields["content"] != nil && f.bodyErr != nil {
				tx.AddError(f.bodyErr)
			}
		case "kb_crawl_batches":
			if f.batchErr != nil {
				tx.AddError(f.batchErr)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Callback().Query().Remove(name)
		db.Callback().Create().Remove(name)
		db.Callback().Update().Remove(name)
	})
	return f
}

func (f *fa12CrawlerFixture) source(id uint, qa bool) *model.KBCrawlerSource {
	f.t.Helper()
	return crawlerRecoverySource(f.t, f.db, tenant.WithTenantID(context.Background(), id), qa)
}

func (f *fa12CrawlerFixture) doc(src *model.KBCrawlerSource) model.KBDocument {
	f.t.Helper()
	ctx := tenant.WithTenantID(context.Background(), src.TenantID)
	var page model.KBCrawlPage
	if err := f.db.WithContext(ctx).Where("source_id = ?", src.ID).First(&page).Error; err != nil {
		f.t.Fatal(err)
	}
	var doc model.KBDocument
	if err := f.db.WithContext(ctx).First(&doc, page.DocumentID).Error; err != nil {
		f.t.Fatal(err)
	}
	return doc
}

func (f *fa12CrawlerFixture) rows(id uint) []model.QACandidate {
	f.t.Helper()
	var rows []model.QACandidate
	if err := f.db.WithContext(tenant.WithTenantID(context.Background(), id)).Find(&rows).Error; err != nil {
		f.t.Fatal(err)
	}
	return rows
}

type fa12RoundTripper func(*http.Request) (*http.Response, error)

func (rt fa12RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return rt(req) }

func (f *fa12CrawlerFixture) crawler() (*CrawlerService, *fa12EmbeddingClient) {
	kb := NewKnowledgeBaseServiceWithDocs(knowledgeRepo.NewKBDocumentRepository(f.db))
	embedding := &fa12EmbeddingClient{}
	kb.vector = ragretrieval.NewVectorizer(1, embedding)
	kb.indexer = ragretrieval.NewInMemoryIndexManager(1)
	svc := NewCrawlerServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(f.db), kb)
	svc.http = &http.Client{Transport: fa12RoundTripper(func(req *http.Request) (*http.Response, error) {
		f.httpCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<html><title>FA12</title><main>" + strings.Repeat("tenant controlled crawler content ", 40) + "</main></html>")), Request: req}, nil
	})}
	return svc, embedding
}

func fa12HashKey(src *model.KBCrawlerSource) string {
	return fmt.Sprintf("%d/%d/%t", src.TenantID, src.ID, src.QASplitEnabled == nil || *src.QASplitEnabled)
}

func TestFA12Crawler_LookupErrorDoesNotCreate(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	src := f.source(7, false)
	svc, _ := f.crawler()
	f.queryErr = errors.New("lookup unavailable")
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 7), src, src.StartURL, &kbcrawler.Page{Content: "body"})
	if !errors.Is(err, f.queryErr) || f.creates != 0 {
		t.Fatalf("lookup error must not create: %v creates=%d", err, f.creates)
	}
}

func TestFA12Crawler_SystemScopeIsRemoved(t *testing.T) {
	withTenantModeKB(t, tenant.ModeOn)
	f := newFA12CrawlerFixture(t)
	src := f.source(7, false)
	svc, _ := f.crawler()
	f.contexts = nil
	_, _, err := svc.IngestCrawledPage(tenant.WithSystemScope(context.Background()), src, src.StartURL, &kbcrawler.Page{Content: strings.Repeat("content ", 30)})
	if err != nil {
		t.Fatal(err)
	}
	for i, ctx := range f.contexts {
		if id, ok := tenant.FromContext(ctx); !ok || id != 7 || tenant.IsSystemScope(ctx) {
			t.Fatalf("boundary %d lost source tenant", i)
		}
	}
}

type fa12QAClient struct {
	err    error
	calls  int
	ids    []uint
	output string
}

func (c *fa12QAClient) Complete(ctx context.Context, _ string) (string, error) {
	c.calls++
	id, _ := tenant.FromContext(ctx)
	c.ids = append(c.ids, id)
	if c.err != nil {
		return "", c.err
	}
	if c.output != "" {
		return c.output, nil
	}
	return `[{"question":"FA12?","answer":"tenant controlled crawler content","answer_anchor":"tenant controlled crawler content"}]`, nil
}

func TestFA12Crawler_QAFailureRetriesSameHash(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	src := f.source(7, true)
	svc, _ := f.crawler()
	qa := &fa12QAClient{err: errors.New("QA unavailable")}
	svc.SetQASplitService(NewQASplitServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(f.db), knowledgeRepo.NewQACandidateRepository(f.db), qa))
	ctx := tenant.WithTenantID(context.Background(), 7)
	var counters crawlCounters
	if err := svc.crawlOne(ctx, src, src.StartURL, &counters); err == nil {
		t.Fatal("QA failure must be returned")
	}
	if svc.prevHash(fa12HashKey(src), src.StartURL) != "" || counters != (crawlCounters{}) || len(f.rows(7)) != 0 || f.doc(src).Status != model.KBDocumentStatusFailed {
		t.Fatal("QA failure was counted as completed")
	}
	qa.err = nil
	if err := svc.crawlOne(ctx, src, src.StartURL, &counters); err != nil {
		t.Fatal(err)
	}
	rows := f.rows(7)
	if len(rows) != 1 || rows[0].TenantID != 7 || rows[0].Status != model.QACandidateStatusPendingReview || counters.changed != 1 {
		t.Fatalf("QA retry did not commit: %+v %+v", rows, counters)
	}
	for _, id := range qa.ids {
		if id != 7 {
			t.Fatalf("QA lost tenant: %v", qa.ids)
		}
	}
}

func TestFA12Crawler_ExplicitQARequiresService(t *testing.T) {
	for _, defaultEnabled := range []bool{false, true} {
		t.Run(fmt.Sprint(defaultEnabled), func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			src := f.source(7, true)
			if defaultEnabled {
				src.QASplitEnabled = nil
			}
			svc, _ := f.crawler()
			var counters crawlCounters
			err := svc.crawlOne(tenant.WithTenantID(context.Background(), 7), src, src.StartURL, &counters)
			if err == nil || counters != (crawlCounters{}) || svc.prevHash(fa12HashKey(src), src.StartURL) != "" || f.doc(src).Status != model.KBDocumentStatusFailed {
				t.Fatalf("missing QA service must remain failed: %v", err)
			}
		})
	}
}

func TestFA12Crawler_QATerminalContract(t *testing.T) {
	for _, name := range []string{"candidate_write_failure", "empty_output", "disabled"} {
		t.Run(name, func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			src := f.source(7, name != "disabled")
			svc, _ := f.crawler()
			qa := &fa12QAClient{output: "[]"}
			svc.SetQASplitService(NewQASplitServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(f.db), knowledgeRepo.NewQACandidateRepository(f.db), qa))
			if name == "candidate_write_failure" {
				qa.output = ""
				f.qaErr = errors.New("candidate write unavailable")
			}
			var counters crawlCounters
			err := svc.crawlOne(tenant.WithTenantID(context.Background(), 7), src, src.StartURL, &counters)
			if name == "candidate_write_failure" {
				if !errors.Is(err, f.qaErr) || counters != (crawlCounters{}) || f.doc(src).Status != model.KBDocumentStatusFailed {
					t.Fatalf("candidate failure was counted as completed: %v %+v", err, counters)
				}
				return
			}
			if err != nil || counters.newPages != 1 || f.doc(src).Status != model.KBDocumentStatusIndexed || len(f.rows(7)) != 0 {
				t.Fatalf("valid QA terminal result failed: %v %+v", err, counters)
			}
			if name == "disabled" && qa.calls != 0 {
				t.Fatal("disabled QA called model")
			}
		})
	}
}

func TestFA12Crawler_VerifyTerminal(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	ctx := tenant.WithTenantID(context.Background(), 7)
	doc := model.KBDocument{TenantID: 7, Title: "terminal"}
	if err := f.db.WithContext(ctx).Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []model.KBDocumentStatus{model.KBDocumentStatusPending, model.KBDocumentStatusProcessing, model.KBDocumentStatusFailed, model.KBDocumentStatusIndexed} {
		t.Run(string(status), func(t *testing.T) {
			if err := f.db.WithContext(ctx).Model(&doc).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			err := svc.verifyDocumentTerminal(ctx, doc.ID, "https://example.test/page")
			if (err == nil) != (status == model.KBDocumentStatusIndexed) {
				t.Fatalf("terminal %s: %v", status, err)
			}
		})
	}
}

func TestFA12Crawler_IngestRejectsNonterminalDocument(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	src := f.source(7, false)
	svc, _ := f.crawler()
	const name = "test:fa12_nonterminal_document"
	if err := f.db.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		fields, ok := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table == "kb_documents" && ok && fields["status"] == model.KBDocumentStatusIndexed {
			tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE kb_documents SET status = ? WHERE tenant_id = ? AND status = ?", model.KBDocumentStatusProcessing, 7, model.KBDocumentStatusIndexed).Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Callback().Update().Remove(name) })
	err := svc.crawlOne(tenant.WithTenantID(context.Background(), 7), src, src.StartURL, &crawlCounters{})
	if err == nil || !strings.Contains(err.Error(), "索引未达成功终态") {
		t.Fatalf("nonterminal document must not complete the page: %v", err)
	}
	if doc := f.doc(src); doc.Status != model.KBDocumentStatusFailed {
		t.Fatalf("nonterminal failure must remain queryable, got %s", doc.Status)
	}
	var page model.KBCrawlPage
	ctx := tenant.WithTenantID(context.Background(), 7)
	if err := f.db.WithContext(ctx).Where("source_id = ?", src.ID).First(&page).Error; err != nil {
		t.Fatal(err)
	}
	if page.CompletedHash != "" || page.ErrorMsg == "" {
		t.Fatalf("nonterminal page must remain retryable: %+v", page)
	}
}

func TestFA12Crawler_FailureInvalidatesPreviousHash(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	src := f.source(7, false)
	svc, embed := f.crawler()
	ctx := tenant.WithTenantID(context.Background(), 7)
	var counters crawlCounters
	if err := svc.crawlOne(ctx, src, src.StartURL, &counters); err != nil {
		t.Fatal(err)
	}
	svc.http = &http.Client{Transport: fa12RoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<main>" + strings.Repeat("changed failed content ", 50) + "</main>")), Header: make(http.Header), Request: req}, nil
	})}
	embed.err = errors.New("vector unavailable")
	if err := svc.crawlOne(ctx, src, src.StartURL, &counters); err == nil {
		t.Fatal("vector failure counted as success")
	}
	if svc.prevHash(fa12HashKey(src), src.StartURL) != "" || counters.newPages != 1 || counters.changed != 0 {
		t.Fatalf("failed change retained previous completion: %+v", counters)
	}
}

func TestFA12Crawler_TwoTenantsRetryAndServiceRestart(t *testing.T) {
	withTenantModeKB(t, tenant.ModeOn)
	f := newFA12CrawlerFixture(t)
	srcA, srcB := f.source(7, false), f.source(8, false)
	svc, embed := f.crawler()
	ctxA, ctxB := tenant.WithTenantID(context.Background(), 7), tenant.WithTenantID(context.Background(), 8)
	var countA, countB crawlCounters
	embed.err = errors.New("vector unavailable")
	if err := svc.crawlOne(ctxA, srcA, srcA.StartURL, &countA); err == nil {
		t.Fatal("tenant A failure hidden")
	}
	embed.err = nil
	if err := svc.crawlOne(ctxB, srcB, srcB.StartURL, &countB); err != nil {
		t.Fatal(err)
	}
	if f.doc(srcA).Status != model.KBDocumentStatusFailed || f.doc(srcB).Status != model.KBDocumentStatusIndexed {
		t.Fatal("cross tenant contamination")
	}
	restarted, _ := f.crawler()
	if err := restarted.crawlOne(ctxA, srcA, srcA.StartURL, &countA); err != nil {
		t.Fatal(err)
	}
	if f.doc(srcA).Status != model.KBDocumentStatusIndexed || countA.changed != 1 {
		t.Fatal("restart did not recover original document")
	}
	if err := restarted.crawlOne(ctxA, srcA, srcA.StartURL, &countA); err != nil || countA.unchanged != 1 {
		t.Fatalf("completed hash not reused: %v %+v", err, countA)
	}
}

func TestFA12Crawler_MissingSourceTenantFailsClosed(t *testing.T) {
	withTenantModeKB(t, tenant.ModeOn)
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 7), &model.KBCrawlerSource{ID: 1}, "https://example.test/page", &kbcrawler.Page{Content: "body"})
	if err == nil || f.queryCalls != 0 || f.creates != 0 {
		t.Fatalf("missing tenant accessed database: %v", err)
	}
}

func TestFA12Crawler_UpdateErrorsCanRetry(t *testing.T) {
	for _, name := range []string{"chunk", "body"} {
		t.Run(name, func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			src := f.source(7, false)
			svc, _ := f.crawler()
			ctx := tenant.WithTenantID(context.Background(), 7)
			id, _, err := svc.IngestCrawledPage(ctx, src, src.StartURL, &kbcrawler.Page{Content: strings.Repeat("old ", 40)})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.db.WithContext(ctx).Create(&kbDocumentChunkFixture{TenantID: 7, DocumentID: id, ContentHash: "old-chunk", Status: "active"}).Error; err != nil {
				t.Fatal(err)
			}
			failure := errors.New("controlled update failure")
			if name == "chunk" {
				f.chunkErr = failure
			} else {
				f.bodyErr = failure
			}
			var counters crawlCounters
			if err := svc.crawlOne(ctx, src, src.StartURL, &counters); !errors.Is(err, failure) {
				t.Fatalf("update failure lost: %v", err)
			}
			if counters != (crawlCounters{}) || svc.prevHash(fa12HashKey(src), src.StartURL) != "" {
				t.Fatal("failed update completed")
			}
			f.chunkErr, f.bodyErr = nil, nil
			if err := svc.crawlOne(ctx, src, src.StartURL, &counters); err != nil || counters.changed != 1 {
				t.Fatalf("retry failed: %v %+v", err, counters)
			}
		})
	}
}

func TestFA12Crawler_RejectsMismatchedTenant(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 8), &model.KBCrawlerSource{ID: 1, TenantID: 7}, "https://example.test/page", &kbcrawler.Page{Content: "body"})
	if err == nil || f.queryCalls != 0 || f.creates != 0 {
		t.Fatalf("mismatched tenant accessed database: %v", err)
	}
}

func TestFA12Crawler_BatchPersistenceAndPartialRetry(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			src := f.source(7, false)
			svc, _ := f.crawler()
			ctx := tenant.WithTenantID(context.Background(), 7)
			batch, err := svc.CreateBatch(ctx, src.ID, "manual")
			if err != nil {
				t.Fatal(err)
			}
			if partial {
				if err := f.db.WithContext(ctx).Model(batch).Update("status", model.KBCrawlBatchStatusPartiallyFailed).Error; err != nil {
					t.Fatal(err)
				}
				retry, err := svc.RetryBatch(ctx, batch.ID)
				if err != nil || retry.RetryCount != 1 || retry.Status != model.KBCrawlBatchStatusPending {
					t.Fatalf("partial retry: %+v %v", retry, err)
				}
			} else {
				f.batchErr = errors.New("batch write unavailable")
				if err := svc.RunBatch(ctx, batch.ID); !errors.Is(err, f.batchErr) || f.httpCalls != 0 {
					t.Fatalf("running write failure: %v requests=%d", err, f.httpCalls)
				}
			}
		})
	}
}
