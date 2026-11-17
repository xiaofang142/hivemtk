package ragretrieval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
)

type observationVectorSearch struct{ err error }

func (v observationVectorSearch) SearchVector(context.Context, string, []float32, int) ([]Chunk, error) {
	if v.err != nil {
		return nil, v.err
	}
	return []Chunk{{ID: "chunk-vector", DocumentID: "doc-vector", Content: "合成知识事实", Score: .9}}, nil
}

type blockingObservationWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockingObservationWriter) CreateObservation(ctx context.Context, row *model.KnowledgeSearchLog) error {
	if _, ok := ctx.Deadline(); !ok || row.Query != "" {
		return errors.New("观测必须有截止时间且无查询正文")
	}
	w.started <- struct{}{}
	select {
	case <-w.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRetrievalObservation_BlockedTraceDoesNotBlockSearch(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeSearchLog{}, &model.MessageTrace{})
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	t.Cleanup(stop)
	if err := database.Callback().Create().Before("gorm:create").Register("test:block-retrieval-trace", func(tx *gorm.DB) {
		if tx.Statement.Table == "message_trace" {
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx := tracing.WithCarrier(tenant.OnlyTenant(context.Background(), 1), &tracing.Carrier{
		TenantID: 1, TraceID: "blocked-trace", Channel: "douyin", AccountID: "account", ConversationID: "conversation",
	})
	searcher := NewHybridSearcher(database, &mockEmbeddingService{}, nil, nil, nil, nil)
	searcher.vectorSearcher = observationVectorSearch{}
	searcher.keywordSearcher = observationKeywordSearch{}
	done := make(chan error, 1)
	utils.SafeGo(ctx, "test.retrieval.nonblocking", func(ctx context.Context) {
		rows, err := searcher.SearchIndex(ctx, "42", "合成检索", 3)
		if err == nil && len(rows) == 0 {
			err = errors.New("检索结果不应因观测减少")
		}
		done <- err
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		stop()
		<-done
		t.Fatal("追踪落库被阻塞时，搜索仍必须立即返回业务结果")
	}
	stop()
	if !waitUntil(func() bool { return searcher.ObservationStats().InFlight == 0 }, time.Second) {
		t.Fatal("释放存储后有界观测必须结束")
	}
}

func TestRetrievalObservation_DatabaseDeadlineEndsBackgroundWork(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeSearchLog{}, &model.MessageTrace{})
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLock()
	tx, err := pool.BeginTx(lockCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(lockCtx, "LOCK TABLE message_trace IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx := tracing.WithCarrier(tenant.OnlyTenant(context.Background(), 18), &tracing.Carrier{
		TenantID: 18, TraceID: "retrieval-deadline", Channel: "xiaohongshu",
		AccountID: "deadline-account", ConversationID: "deadline-conversation",
	})
	searcher := NewHybridSearcher(database, &mockEmbeddingService{}, nil, nil, nil, nil)
	searcher.vectorSearcher = observationVectorSearch{}
	searcher.keywordSearcher = observationKeywordSearch{}
	started := time.Now()
	rows, err := searcher.SearchIndex(ctx, "42", "合成截止验证", 3)
	if err != nil || len(rows) == 0 {
		t.Fatalf("观测数据库被锁住仍必须返回检索结果：%v", err)
	}
	if time.Since(started) >= 500*time.Millisecond {
		t.Fatal("后台观测不得让主搜索等待数据库锁")
	}
	// 始终持有锁，直到后台自身截止退出，不能先解锁制造成功假象。
	if !waitUntil(func() bool { return searcher.ObservationStats().InFlight == 0 }, 3*time.Second) {
		t.Fatal("两秒截止后后台必须结束并释放并发名额")
	}
	stats := searcher.ObservationStats()
	if stats.Failed != 1 || stats.Persisted != 0 || stats.Dropped != 0 {
		t.Fatalf("截止只降低测量覆盖，不冒充成功写入：%+v", stats)
	}
	_ = tx.Rollback()
	for _, table := range []string{"message_trace", "knowledge_search_logs"} {
		var count int64
		if err := database.WithContext(tenant.OnlyTenant(context.Background(), 18)).Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("截止后释放锁不应继续补写%s：%d", table, count)
		}
	}
}

func TestRetrievalObservation_CapacityIsBoundedAndVisible(t *testing.T) {
	w := &blockingObservationWriter{started: make(chan struct{}, 4), release: make(chan struct{})}
	var once sync.Once
	stop := func() { once.Do(func() { close(w.release) }) }
	t.Cleanup(stop)
	searcher := NewHybridSearcherWithInterfaces(observationVectorSearch{}, observationKeywordSearch{}, nil, .5, .5)
	searcher.observationWriter = w
	ctx := tenant.OnlyTenant(context.Background(), 1)
	for i := 0; i < 6; i++ {
		rows, err := searcher.SearchIndex(ctx, "42", "容量测试", 3)
		if err != nil || len(rows) == 0 {
			t.Fatal("日志容量耗尽不得改变业务结果")
		}
	}
	stats := searcher.ObservationStats()
	if stats.InFlight != 4 || stats.Dropped != 2 {
		t.Fatalf("并发必须不超过4且覆盖损失可见：%+v", stats)
	}
	stop()
	if !waitUntil(func() bool { return searcher.ObservationStats().InFlight == 0 }, time.Second) {
		t.Fatal("观测任务必须释放并发名额")
	}
	if searcher.ObservationStats().Persisted != 4 {
		t.Fatal("成功记录计数必须与实际写入一致")
	}
}

type observationKeywordSearch struct{}

func (observationKeywordSearch) SearchKeyword(context.Context, string, string, int) ([]Chunk, error) {
	return []Chunk{{ID: "chunk-keyword", DocumentID: "doc-keyword", Content: "合成关键词事实", Score: .8}}, nil
}

type resultObservationWriter struct {
	err        error
	panicValue bool
}

func (w resultObservationWriter) CreateObservation(context.Context, *model.KnowledgeSearchLog) error {
	if w.panicValue {
		panic("合成凭证不应进入日志")
	}
	return w.err
}

func TestRetrievalObservation_FailuresReduceCoverageNotResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer resultObservationWriter
	}{
		{name: "存储失败", writer: resultObservationWriter{err: errors.New("合成写入失败")}},
		{name: "存储异常", writer: resultObservationWriter{panicValue: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			searcher := NewHybridSearcherWithInterfaces(observationVectorSearch{}, observationKeywordSearch{}, nil, .5, .5)
			searcher.observationWriter = tc.writer
			rows, err := searcher.SearchIndex(tenant.OnlyTenant(context.Background(), 1), "42", "合成问题", 3)
			if err != nil || len(rows) != 1 {
				t.Fatal("观测失败不得改变检索结果")
			}
			if !waitUntil(func() bool { return searcher.ObservationStats().InFlight == 0 }, time.Second) {
				t.Fatal("失败观测必须释放名额")
			}
			stats := searcher.ObservationStats()
			if stats.Failed != 1 || stats.Persisted != 0 {
				t.Fatalf("失败覆盖计数必须正确：%+v", stats)
			}
		})
	}
}

func TestRetrievalObservation_FailedCallsHaveMeasuredDuration(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeSearchLog{}, &model.MessageTrace{})
	ctx := tenant.OnlyTenant(context.Background(), 1)
	searcher := NewHybridSearcher(database, &mockEmbeddingService{}, &mockReranker{err: errors.New("重排故障")}, nil, nil, nil)
	searcher.vectorSearcher = observationVectorSearch{err: errors.New("向量查询故障")}
	searcher.keywordSearcher = observationKeywordSearch{}
	searcher.config.EnableRerank = true
	rows, err := searcher.SearchIndex(ctx, "42", "合成问题", 3)
	if err != nil || len(rows) != 1 || rows[0].Score != 1 {
		t.Fatal("失败仍应按原融合降级规则返回")
	}
	var logs []model.KnowledgeSearchLog
	if !waitUntil(func() bool { return database.Find(&logs).Error == nil && len(logs) == 1 }, time.Second) {
		t.Fatal("失败阶段也必须有观测")
	}
	row := logs[0]
	if row.VectorLatencyMs == nil || !row.VectorFailed || row.RerankLatencyMs == nil || !row.RerankFailed {
		t.Fatal("实际调用失败是已测量，不是未执行")
	}
	if row.RewriteLatencyMs != nil || row.EmbeddingFailed || row.BM25Failed {
		t.Fatal("未执行与正常阶段不得误标失败")
	}
	if row.StartedAt == nil || row.CompletedAt == nil || row.CompletedAt.Before(*row.StartedAt) {
		t.Fatal("检索操作时间必须正向")
	}
}

func TestRetrievalObservation_UnboundTenantCannotBorrowCarrier(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺租户", ctx: context.Background()},
		{name: "父载体不同租户", ctx: tracing.WithCarrier(tenant.OnlyTenant(context.Background(), 18), &tracing.Carrier{TenantID: 19})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := retrievalObservationContext(tc.ctx); ok {
				t.Fatal("不得借用别的租户归属写日志")
			}
		})
	}
	ctx := tracing.WithCarrier(tenant.OnlyTenant(context.Background(), 18), &tracing.Carrier{TenantID: 18, TurnID: "未知归属轮次", AnswerAttempt: 2})
	observed, ok := retrievalObservationContext(ctx)
	if !ok {
		t.Fatal("有租户的知识检索仍可记录")
	}
	c := tracing.CarrierFromContext(observed)
	if c.TenantID != 18 || c.TurnID != "" || c.AnswerAttempt != 0 {
		t.Fatal("缺会话边界不猜轮次")
	}
}

func TestRetrievalObservation_UUIDAndMeasuredPhases(t *testing.T) {
	for _, tc := range []struct {
		name, product string
		embedErr      error
	}{
		{name: "UUID知识库", product: "cb35812c-cd41-4e23-b4a8-1d28929291b7"},
		{name: "数字字符串", product: "42"},
		{name: "全库查询嵌入失败仍有观测", embedErr: errors.New("合成嵌入故障")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.KnowledgeSearchLog{}, &model.MessageTrace{})
			// 补上旧SQL使用但旧模型遗漏的列，以单独证明UUID强制转数字的错误。
			for _, column := range []string{"vector_count", "bm25_count", "fused_count", "rerank_count", "vector_latency_ms", "bm25_latency_ms"} {
				if err := database.Exec("ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS " + column + " BIGINT").Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			carrier := &tracing.Carrier{
				TenantID: tenant.DefaultTenantID, TraceID: "retrieval-test", TurnID: tracing.BusinessTurnID(1, "xiaohongshu", "account", "conversation", "inbound"),
				Channel: "xiaohongshu", AccountID: "account", ConversationID: "conversation", AnswerAttempt: 2,
			}
			ctx = tracing.WithCarrier(ctx, carrier)
			searcher := NewHybridSearcher(database, &mockEmbeddingService{err: tc.embedErr}, nil, nil, nil, nil)
			searcher.vectorSearcher = observationVectorSearch{}
			searcher.keywordSearcher = observationKeywordSearch{}
			started := time.Now()
			result, err := searcher.SearchIndex(ctx, tc.product, "客户合成查询，不应记录正文", 3)
			if err != nil || len(result) == 0 {
				t.Fatalf("观测不得影响原关键词降级结果：结果=%d，错误=%v", len(result), err)
			}
			var logs []model.KnowledgeSearchLog
			if !waitUntil(func() bool {
				return database.Find(&logs).Error == nil && len(logs) > 0
			}, time.Second) {
				t.Fatal("成功检索必须留下兼容字符串知识库ID的日志")
			}
			if len(logs) != 1 || logs[0].ProductID != tc.product || logs[0].Query != "" || logs[0].QueryHash == "" {
				t.Fatal("观测必须保留知识库身份，仅记录查询哈希而非客户正文")
			}
			encoded, err := json.Marshal(logs[0])
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"embedding_latency_ms", "bm25_latency_ms", "rrf_latency_ms", "parent_latency_ms", "started_at", "completed_at"} {
				if fields[field] == nil {
					t.Errorf("实际执行阶段必须记录测量（包括真实0）：%s", field)
				}
			}
			if fields["turn_id"] != carrier.TurnID || fields["answer_attempt"] != float64(2) || fields["duration_measured"] != true {
				t.Error("检索必须关联同租户会话的本轮及生成尝试")
			}
			if fields["rerank_latency_ms"] != nil {
				t.Error("没有执行重排时必须是缺测，而非0毫秒")
			}
			if tc.embedErr != nil && fields["vector_latency_ms"] != nil {
				t.Error("嵌入失败后未执行向量检索，不得伪造其耗时")
			} else if tc.embedErr == nil && fields["vector_latency_ms"] == nil {
				t.Error("实际向量检索必须独立于嵌入测量")
			}
			if logs[0].CreatedAt.Before(started) {
				t.Error("日志应由本次操作产生")
			}
		})
	}
}
