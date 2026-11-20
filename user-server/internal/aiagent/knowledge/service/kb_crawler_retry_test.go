package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	kbcrawler "hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

// 受控仓储夹具替换 GORM 的执行回调，不连接 PostgreSQL、不建表、不运行 destructive helper。
// 状态跨 CrawlerService 实例保留，仅用于验证服务/仓储边界，不充当真实 DB 隔离证据。
type fa12CrawlerFixture struct {
	t           *testing.T
	db          *gorm.DB
	docs        map[uint]*model.KBDocument
	nextID      uint
	queryErr    error
	chunkErr    error
	bodyErr     error
	statusFail  model.KBDocumentStatus
	batchErr    error
	batch       *model.KBCrawlBatch
	source      *model.KBCrawlerSource
	statusWrite func(model.KBDocumentStatus) error
	queryCalls  int
	httpCalls   int
	creates     int
	updates     int
	qaRows      []*model.QACandidate
	qaErr       error
	contexts    []context.Context
}

func newFA12CrawlerFixture(t *testing.T) *fa12CrawlerFixture {
	t.Helper()
	f := &fa12CrawlerFixture{t: t, docs: make(map[uint]*model.KBDocument), nextID: 1}
	var err error
	f.db, err = gorm.Open(postgres.New(postgres.Config{DSN: "host=invalid.fa12.test user=fake dbname=fake sslmode=disable"}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("构建无连接 GORM 夹具失败: %v", err)
	}
	pool, err := f.db.DB()
	if err != nil {
		t.Fatalf("读取受控连接池失败: %v", err)
	}
	t.Cleanup(func() {
		if count := pool.Stats().OpenConnections; count != 0 {
			t.Errorf("夹具不应连接真实 PostgreSQL，连接数=%d", count)
		}
		_ = pool.Close()
	})
	for name, register := range map[string]func() error{
		"query":  func() error { return f.db.Callback().Query().Replace("gorm:query", f.query) },
		"create": func() error { return f.db.Callback().Create().Replace("gorm:create", f.create) },
		"update": func() error { return f.db.Callback().Update().Replace("gorm:update", f.update) },
	} {
		if err := register(); err != nil {
			t.Fatalf("替换 %s 回调失败: %v", name, err)
		}
	}
	return f
}

func (f *fa12CrawlerFixture) scoped(tx *gorm.DB) (uint, bool) {
	f.contexts = append(f.contexts, tx.Statement.Context)
	id, ok := tenant.FromContext(tx.Statement.Context)
	if !ok || tenant.IsSystemScope(tx.Statement.Context) {
		_ = tx.AddError(errors.New("controlled store: tenant scope required"))
		return 0, false
	}
	return id, true
}

func fa12WhereValues(expr clause.Expression) []any {
	switch e := expr.(type) {
	case clause.Where:
		var out []any
		for _, sub := range e.Exprs {
			out = append(out, fa12WhereValues(sub)...)
		}
		return out
	case clause.Expr:
		return e.Vars
	case clause.Eq:
		return []any{e.Value}
	case clause.IN:
		return e.Values
	}
	return nil
}

func (f *fa12CrawlerFixture) query(tx *gorm.DB) {
	id, ok := f.scoped(tx)
	if !ok {
		return
	}
	f.queryCalls++
	if f.queryErr != nil {
		_ = tx.AddError(f.queryErr)
		return
	}
	values := fa12WhereValues(tx.Statement.Clauses["WHERE"].Expression)
	if count, ok := tx.Statement.Dest.(*int64); ok {
		*count = 0
		tx.RowsAffected = 1
		return
	}
	if batch, ok := tx.Statement.Dest.(*model.KBCrawlBatch); ok && f.batch != nil {
		*batch = *f.batch
		tx.RowsAffected = 1
		return
	}
	if source, ok := tx.Statement.Dest.(*model.KBCrawlerSource); ok && f.batch != nil {
		if f.source != nil {
			*source = *f.source
			tx.RowsAffected = 1
			return
		}
		*source = model.KBCrawlerSource{ID: 1, TenantID: id, StartURL: "https://example.test/page", SourceType: "url", MaxPages: 1, Status: model.KBCrawlerSourceStatusActive}
		tx.RowsAffected = 1
		return
	}
	for _, doc := range f.docs {
		if doc.TenantID != id || len(values) == 0 {
			continue
		}
		if fmt.Sprint(values[0]) == fmt.Sprint(doc.ID) || values[0] == doc.Title {
			*tx.Statement.Dest.(*model.KBDocument) = *doc
			tx.RowsAffected = 1
			return
		}
	}
	_ = tx.AddError(gorm.ErrRecordNotFound)
}

func (f *fa12CrawlerFixture) create(tx *gorm.DB) {
	id, ok := f.scoped(tx)
	if !ok {
		return
	}
	switch dest := tx.Statement.Dest.(type) {
	case *model.KBDocument:
		if dest.TenantID != id {
			_ = tx.AddError(errors.New("controlled store: document tenant mismatch"))
			return
		}
		dest.ID = f.nextID
		f.nextID++
		copy := *dest
		f.docs[dest.ID] = &copy
		f.creates++
	case *[]*model.QACandidate:
		if f.qaErr != nil {
			_ = tx.AddError(f.qaErr)
			return
		}
		for _, row := range *dest {
			if row.TenantID != id {
				_ = tx.AddError(errors.New("controlled store: QA tenant mismatch"))
				return
			}
			copy := *row
			f.qaRows = append(f.qaRows, &copy)
		}
	case *model.KBCrawlBatch:
		dest.ID = 99
	default:
		f.t.Fatalf("未支持的 create 类型 %T", dest)
	}
	tx.RowsAffected = 1
}

func (f *fa12CrawlerFixture) update(tx *gorm.DB) {
	id, ok := f.scoped(tx)
	if !ok {
		return
	}
	f.updates++
	if tx.Statement.Table == "kb_document_chunks" {
		if f.chunkErr != nil {
			_ = tx.AddError(f.chunkErr)
		}
		return
	}
	if tx.Statement.Table == "kb_crawl_batches" {
		if f.batchErr != nil {
			_ = tx.AddError(f.batchErr)
			return
		}
		if err := tx.Statement.Context.Err(); err != nil {
			_ = tx.AddError(err)
			return
		}
		if f.batch != nil {
			fields := tx.Statement.Dest.(map[string]any)
			if status, ok := fields["status"].(string); ok {
				f.batch.Status = status
			}
			if started, ok := fields["started_at"].(time.Time); ok {
				f.batch.StartedAt = &started
			}
			if finished, ok := fields["finished_at"].(time.Time); ok {
				f.batch.FinishedAt = &finished
			}
			if message, ok := fields["error_msg"].(string); ok {
				f.batch.ErrorMsg = message
			}
			tx.RowsAffected = 1
		}
		return
	}
	fields := tx.Statement.Dest.(map[string]any)
	if tx.Statement.Table == "kb_crawler_sources" {
		if f.source != nil {
			if cursor, ok := fields["last_crawl_at"].(time.Time); ok {
				f.source.LastCrawlAt = &cursor
			}
			if batchNo, ok := fields["last_batch_id"].(string); ok {
				f.source.LastBatchID = batchNo
			}
		}
		tx.RowsAffected = 1
		return
	}
	if _, hasContent := fields["content"]; hasContent && f.bodyErr != nil {
		_ = tx.AddError(f.bodyErr)
		return
	}
	if status, ok := fields["status"].(model.KBDocumentStatus); ok && status == f.statusFail {
		f.statusFail = ""
		_ = tx.AddError(errors.New("controlled status write failure"))
		return
	}
	if status, ok := fields["status"].(model.KBDocumentStatus); ok && f.statusWrite != nil {
		if err := f.statusWrite(status); err != nil {
			_ = tx.AddError(err)
			return
		}
	}
	values := fa12WhereValues(tx.Statement.Clauses["WHERE"].Expression)
	for _, doc := range f.docs {
		if doc.TenantID != id || len(values) == 0 || fmt.Sprint(values[0]) != fmt.Sprint(doc.ID) {
			continue
		}
		if status, ok := fields["status"].(model.KBDocumentStatus); ok {
			doc.Status = status
		}
		if content, ok := fields["content"].(string); ok {
			doc.Content = content
		}
		if message, ok := fields["error_msg"].(string); ok {
			doc.ErrorMsg = message
		}
		tx.RowsAffected = 1
		return
	}
}

type fa12RoundTripper func(*http.Request) (*http.Response, error)

func (rt fa12RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return rt(req) }

func (f *fa12CrawlerFixture) crawler() (*CrawlerService, *fa12EmbeddingClient) {
	f.t.Helper()
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

// 查询故障不能当作文档不存在，避免重复创建并误记成功。
func TestFA12Crawler_LookupErrorDoesNotCreate(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	f.queryErr = errors.New("lookup unavailable")
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 7), &model.KBCrawlerSource{ID: 1, TenantID: 7}, "https://example.test/page", &kbcrawler.Page{Content: "body"})
	if err == nil || !errors.Is(err, f.queryErr) || f.creates != 0 {
		t.Fatalf("查询错误应原样返回且不新建，err=%v creates=%d", err, f.creates)
	}
}

// 每个数据访问边界必须钉死源租户，系统豁免不能传递到文档处理。
func TestFA12Crawler_SystemScopeIsRemoved(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	_, _, err := svc.IngestCrawledPage(tenant.WithSystemScope(context.Background()), &model.KBCrawlerSource{ID: 1, TenantID: 7}, "https://example.test/page", &kbcrawler.Page{Content: strings.Repeat("content ", 30)})
	if err != nil {
		t.Fatalf("恢复源租户后应成功，err=%v", err)
	}
	for i, ctx := range f.contexts {
		id, ok := tenant.FromContext(ctx)
		if !ok || id != 7 || tenant.IsSystemScope(ctx) {
			t.Fatalf("第 %d 个数据边界租户错误: id=%d ok=%v system=%v", i, id, ok, tenant.IsSystemScope(ctx))
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

// QA 失败时既不能记 hash 也不能计成功，同 hash 再抓必须能恢复到候选已落库。
func TestFA12Crawler_QAFailureRetriesSameHash(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	qa := &fa12QAClient{err: errors.New("QA unavailable")}
	svc.SetQASplitService(NewQASplitServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(f.db), knowledgeRepo.NewQACandidateRepository(f.db), qa))
	src := &model.KBCrawlerSource{ID: 1, TenantID: 7, KBID: 10}
	ctx := tenant.WithTenantID(context.Background(), 7)
	var counters crawlCounters
	if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); err == nil {
		t.Fatal("QA 失败时不能宣称爬取成功")
	}
	if svc.prevHash(1, "https://example.test/page") != "" || !reflect.DeepEqual(counters, crawlCounters{}) || len(f.qaRows) != 0 {
		t.Fatalf("QA 失败不能记 hash/计成功/产候选，counters=%+v rows=%d", counters, len(f.qaRows))
	}
	if f.docs[1].Status != model.KBDocumentStatusFailed {
		t.Fatalf("QA 失败应保留 failed 状态，got=%s", f.docs[1].Status)
	}
	qa.err = nil
	if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); err != nil {
		t.Fatalf("同 hash 重抓应恢复，err=%v", err)
	}
	if len(f.qaRows) != 1 || f.qaRows[0].TenantID != 7 || f.qaRows[0].Status != model.QACandidateStatusPendingReview || counters.changed != 1 || counters.unchanged != 0 {
		t.Fatalf("QA 未达候选终态，rows=%+v counters=%+v", f.qaRows, counters)
	}
	if !reflect.DeepEqual(qa.ids, []uint{7, 7, 7}) {
		t.Fatalf("QA 初试/重试均应携带租户7，got=%v", qa.ids)
	}
}

// 源明确要求 QA 时，缺少服务装配也不能把未执行的 QA 标记完成。
func TestFA12Crawler_ExplicitQARequiresService(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	enabled := true
	src := &model.KBCrawlerSource{ID: 1, TenantID: 7, QASplitEnabled: &enabled}
	var counters crawlCounters
	err := svc.crawlOne(tenant.WithTenantID(context.Background(), 7), src, "https://example.test/page", &counters)
	if err == nil || counters != (crawlCounters{}) || svc.prevHash(1, "https://example.test/page") != "" || f.docs[1].Status != model.KBDocumentStatusFailed {
		t.Fatalf("QA 未装配应保留失败且可重试，err=%v counters=%+v doc=%+v", err, counters, f.docs[1])
	}
}

// QA 候选写失败不能计成功；合法空产出与显式关闭 QA 都是明确完成态。
func TestFA12Crawler_QATerminalContract(t *testing.T) {
	for _, name := range []string{"candidate_write_failure", "empty_output", "disabled"} {
		t.Run(name, func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			svc, _ := f.crawler()
			qa := &fa12QAClient{output: "[]"}
			svc.SetQASplitService(NewQASplitServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(f.db), knowledgeRepo.NewQACandidateRepository(f.db), qa))
			src := &model.KBCrawlerSource{ID: 1, TenantID: 7}
			if name == "candidate_write_failure" {
				qa.output = ""
				f.qaErr = errors.New("candidate write unavailable")
			} else if name == "disabled" {
				disabled := false
				src.QASplitEnabled = &disabled
			}
			var counters crawlCounters
			err := svc.crawlOne(tenant.WithTenantID(context.Background(), 7), src, "https://example.test/page", &counters)
			if name == "candidate_write_failure" {
				if !errors.Is(err, f.qaErr) || counters != (crawlCounters{}) || svc.prevHash(1, "https://example.test/page") != "" || f.docs[1].Status != model.KBDocumentStatusFailed {
					t.Fatalf("候选未持久化不能完成，err=%v counters=%+v", err, counters)
				}
				return
			}
			if err != nil || counters.newPages != 1 || f.docs[1].Status != model.KBDocumentStatusIndexed || len(f.qaRows) != 0 {
				t.Fatalf("明确QA终态应成功，err=%v counters=%+v", err, counters)
			}
			if name == "disabled" && qa.calls != 0 {
				t.Fatalf("关闭QA不能调用模型，calls=%d", qa.calls)
			}
		})
	}
}

// 终态核对只允许 indexed，pending/processing/failed 都不能提前记成功。
func TestFA12Crawler_VerifyTerminal(t *testing.T) {
	for _, status := range []model.KBDocumentStatus{model.KBDocumentStatusPending, model.KBDocumentStatusProcessing, model.KBDocumentStatusFailed, model.KBDocumentStatusIndexed} {
		t.Run(string(status), func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			svc, _ := f.crawler()
			f.docs[1] = &model.KBDocument{ID: 1, TenantID: 7, Status: status, ErrorMsg: "controlled failure"}
			err := svc.verifyDocumentTerminal(tenant.WithTenantID(context.Background(), 7), 1, "https://example.test/page")
			if (err == nil) != (status == model.KBDocumentStatusIndexed) {
				t.Fatalf("终态%s核对不符，err=%v", status, err)
			}
		})
	}
}

// 已成功页面后续失败必须失效旧 hash，避免内容回滚时被旧成功快照挡住。
func TestFA12Crawler_FailureInvalidatesPreviousHash(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, embed := f.crawler()
	src := &model.KBCrawlerSource{ID: 1, TenantID: 7}
	ctx := tenant.WithTenantID(context.Background(), 7)
	var counters crawlCounters
	if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); err != nil {
		t.Fatalf("首轮成功失败: %v", err)
	}
	svc.http = &http.Client{Transport: fa12RoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<main>" + strings.Repeat("changed failed content ", 50) + "</main>")), Header: make(http.Header), Request: req}, nil
	})}
	embed.err = errors.New("vector unavailable")
	if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); err == nil {
		t.Fatal("向量失败不应算成功")
	}
	if hash := svc.prevHash(1, "https://example.test/page"); hash != "" {
		t.Fatalf("失败必须失效旧成功 hash，got=%s", hash)
	}
	if counters.newPages != 1 || counters.changed != 0 {
		t.Fatalf("失败不能增加成功计数，got=%+v", counters)
	}
}

// 两个租户抓同 URL 也只能访问各自文档；新进程服务实例必须从持久失败状态重建。
func TestFA12Crawler_TwoTenantsRetryAndServiceRestart(t *testing.T) {
	withTenantModeKB(t, tenant.ModeOn)
	f := newFA12CrawlerFixture(t)
	svc, embed := f.crawler()
	srcA := &model.KBCrawlerSource{ID: 1, TenantID: 7}
	srcB := &model.KBCrawlerSource{ID: 2, TenantID: 8}
	ctxA, ctxB := tenant.WithTenantID(context.Background(), 7), tenant.WithTenantID(context.Background(), 8)
	var countA, countB crawlCounters
	embed.err = errors.New("vector unavailable")
	if err := svc.crawlOne(ctxA, srcA, "https://example.test/page", &countA); err == nil {
		t.Fatal("租户A向量失败必须可见")
	}
	if f.docs[1].Status != model.KBDocumentStatusFailed || svc.prevHash(1, "https://example.test/page") != "" || countA != (crawlCounters{}) {
		t.Fatalf("失败状态/计数/hash不符，doc=%+v counters=%+v", f.docs[1], countA)
	}
	embed.err = nil
	if err := svc.crawlOne(ctxB, srcB, "https://example.test/page", &countB); err != nil {
		t.Fatalf("租户B不受A影响: %v", err)
	}
	if f.docs[1].TenantID != 7 || f.docs[1].Status != model.KBDocumentStatusFailed || f.docs[2].TenantID != 8 || f.docs[2].Status != model.KBDocumentStatusIndexed {
		t.Fatalf("跨租户互相污染: %+v %+v", f.docs[1], f.docs[2])
	}
	// 仅重建服务/索引，不重置受控存储；模拟进程重启丢失所有内存 hash。
	restarted, _ := f.crawler()
	if restarted.prevHash(1, "https://example.test/page") != "" {
		t.Fatal("新服务不应继承旧 hash")
	}
	if err := restarted.crawlOne(ctxA, srcA, "https://example.test/page", &countA); err != nil {
		t.Fatalf("重建服务后同 hash 应恢复A失败文档: %v", err)
	}
	if len(f.docs) != 2 || f.docs[1].Status != model.KBDocumentStatusIndexed || countA.changed != 1 || countA.unchanged != 0 {
		t.Fatalf("重启恢复应重用原文档且成功，docs=%+v counters=%+v", f.docs, countA)
	}
	before := f.queryCalls
	if err := restarted.crawlOne(ctxA, srcA, "https://example.test/page", &countA); err != nil || countA.unchanged != 1 || f.queryCalls != before {
		t.Fatalf("完整成功后同 hash 才应跳过，err=%v counters=%+v", err, countA)
	}
}

// 多租户模式缺源租户必须在访问存储之前 fail-closed，不能继承调用方租户。
func TestFA12Crawler_MissingSourceTenantFailsClosed(t *testing.T) {
	withTenantModeKB(t, tenant.ModeOn)
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 7), &model.KBCrawlerSource{ID: 1}, "https://example.test/page", &kbcrawler.Page{Content: "body"})
	if err == nil || f.queryCalls != 0 || f.creates != 0 {
		t.Fatalf("缺源租户必须 fail-closed，err=%v queries=%d creates=%d", err, f.queryCalls, f.creates)
	}
}

// 仓储旧分片失效/正文更新错误不得吞掉，修复依赖后同页可重试。
func TestFA12Crawler_UpdateErrorsCanRetry(t *testing.T) {
	for _, name := range []string{"chunk", "body"} {
		t.Run(name, func(t *testing.T) {
			f := newFA12CrawlerFixture(t)
			svc, _ := f.crawler()
			src := &model.KBCrawlerSource{ID: 1, TenantID: 7}
			ctx := tenant.WithTenantID(context.Background(), 7)
			_, _, err := svc.IngestCrawledPage(ctx, src, "https://example.test/page", &kbcrawler.Page{Content: strings.Repeat("old ", 40)})
			if err != nil {
				t.Fatalf("预置文档失败: %v", err)
			}
			failure := errors.New("controlled update failure")
			if name == "chunk" {
				f.chunkErr = failure
			} else {
				f.bodyErr = failure
			}
			var counters crawlCounters
			if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); !errors.Is(err, failure) {
				t.Fatalf("更新错误必须透传，got=%v", err)
			}
			if counters != (crawlCounters{}) || svc.prevHash(1, "https://example.test/page") != "" {
				t.Fatalf("错误不能记成功，got=%+v", counters)
			}
			f.chunkErr, f.bodyErr = nil, nil
			if err := svc.crawlOne(ctx, src, "https://example.test/page", &counters); err != nil || counters.changed != 1 {
				t.Fatalf("更新失败后同 hash 重抓应成功，err=%v counters=%+v", err, counters)
			}
		})
	}
}

// 显式请求租户与源租户矛盾时，不能先读写请求租户再切到源租户。
func TestFA12Crawler_RejectsMismatchedTenant(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	svc, _ := f.crawler()
	_, _, err := svc.IngestCrawledPage(tenant.WithTenantID(context.Background(), 8), &model.KBCrawlerSource{ID: 1, TenantID: 7}, "https://example.test/page", &kbcrawler.Page{Content: "body"})
	if err == nil || f.queryCalls != 0 || f.creates != 0 {
		t.Fatalf("冲突租户必须在访问存储前拒绝，err=%v queries=%d creates=%d", err, f.queryCalls, f.creates)
	}
}

// running 持久化失败时不能开始抓取；partially_failed 必须能通过重试入口恢复。
func TestFA12Crawler_BatchPersistenceAndPartialRetry(t *testing.T) {
	t.Run("running_write_failure", func(t *testing.T) {
		f := newFA12CrawlerFixture(t)
		f.batch = &model.KBCrawlBatch{ID: 1, SourceID: 1, Status: model.KBCrawlBatchStatusPending}
		f.batchErr = errors.New("batch write unavailable")
		svc, _ := f.crawler()
		err := svc.RunBatch(tenant.WithTenantID(context.Background(), 7), 1)
		if !errors.Is(err, f.batchErr) || f.httpCalls != 0 {
			t.Fatalf("running 回写失败应停止并保留原错误，got=%v httpCalls=%d", err, f.httpCalls)
		}
	})
	t.Run("partially_failed_retry", func(t *testing.T) {
		f := newFA12CrawlerFixture(t)
		f.batch = &model.KBCrawlBatch{ID: 1, SourceID: 1, Status: model.KBCrawlBatchStatusPartiallyFailed}
		svc, _ := f.crawler()
		retry, err := svc.RetryBatch(tenant.WithTenantID(context.Background(), 7), 1)
		if err != nil || retry.RetryCount != 1 || retry.Status != model.KBCrawlBatchStatusPending {
			t.Fatalf("部分失败必须可重试，got=%+v err=%v", retry, err)
		}
	})
}
