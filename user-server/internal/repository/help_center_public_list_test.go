// help_center_public_list_test.go 公开文章列表的摘要查询扫描量回归（六轮 A12）
package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// chunkScanObserver 实现 gorm 的 logger.Interface，只为把"每条 SQL 实际扫回多少行"
// 记下来。修法前后的返回 map 完全一致，唯一能被观测的差异就是扫描行数。
// 用 Session{Logger:} 挂：gorm 的 Session 会浅拷贝 Config，钩子不会漏给
// 同二进制里的其他用例（本仓测试库是进程级共享句柄）。
type chunkScanObserver struct {
	mu      sync.Mutex
	pairs   []string
	enabled bool
}

func (o *chunkScanObserver) LogMode(gormlogger.LogLevel) gormlogger.Interface { return o }
func (o *chunkScanObserver) Info(context.Context, string, ...any)             {}
func (o *chunkScanObserver) Warn(context.Context, string, ...any)             {}
func (o *chunkScanObserver) Error(context.Context, string, ...any)            {}

func (o *chunkScanObserver) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, rows := fc()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.enabled {
		o.pairs = append(o.pairs, fmt.Sprintf("%d\t%s", rows, sql))
	}
}

// chunkRowsOnly 只统计" FROM knowledge_chunks "且不属于文档表的那条查询，
// 即摘要查询本身（列表查询带 q 时 WHERE 子句里也会出现 knowledge_chunks 子查询，
// 靠 knowledge_documents 排除掉）。
func (o *chunkScanObserver) chunkRowsOnly() (int64, int, []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var total int64
	n := 0
	var matched []string
	for _, p := range o.pairs {
		parts := strings.SplitN(p, "\t", 2)
		rows := int64(0)
		if _, err := fmt.Sscanf(parts[0], "%d", &rows); err != nil {
			continue
		}
		sql := parts[1]
		if strings.Contains(sql, `FROM "knowledge_chunks"`) && !strings.Contains(sql, "knowledge_documents") {
			total += rows
			n++
			matched = append(matched, sql)
		}
	}
	return total, n, matched
}

func TestListPublicArticles_SummaryQueryScansOneRowPerDocument(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KnowledgeDocument{}, &model.KnowledgeChunk{})
	ctx := context.Background()

	const docCount, chunkCount = 3, 5
	docs := make([]model.KnowledgeDocument, 0, docCount)
	for i := 0; i < docCount; i++ {
		docs = append(docs, model.KnowledgeDocument{
			Title:         fmt.Sprintf("帮助中心文章 %d", i),
			Category:      "使用指南",
			PublicVisible: true,
			HCStatus:      "published",
		})
	}
	if err := db.WithContext(ctx).Create(&docs).Error; err != nil {
		t.Fatalf("建文档夹具: %v", err)
	}
	chunks := make([]model.KnowledgeChunk, 0, docCount*chunkCount)
	expected := map[uint64]string{}
	for _, d := range docs {
		for i := 0; i < chunkCount; i++ {
			content := fmt.Sprintf("文档 %d 的第 %d 段正文", d.ID, i)
			if i == 0 {
				expected[d.ID] = content
			}
			chunks = append(chunks, model.KnowledgeChunk{
				DocumentID: d.ID,
				ChunkIndex: i,
				Content:    content,
				CharCount:  len(content),
			})
		}
	}
	if err := db.WithContext(ctx).Create(&chunks).Error; err != nil {
		t.Fatalf("建切片夹具: %v", err)
	}

	obs := &chunkScanObserver{}
	// 夹具写入走的是原始 db；开始观测前才挂上记录器，避免把建表/插入算进扫描量。
	repo := NewHelpCenterRepositoryWithDB(db.Session(&gorm.Session{Logger: obs}))
	obs.mu.Lock()
	obs.enabled = true
	obs.mu.Unlock()

	rows, summaries, err := repo.ListPublicArticles(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("ListPublicArticles: %v", err)
	}
	if len(rows) != docCount {
		t.Fatalf("列表行数=%d，期望 %d（夹具没落地就等于断言是空的）", len(rows), docCount)
	}
	if len(summaries) != docCount {
		t.Fatalf("摘要条数=%d，期望 %d", len(summaries), docCount)
	}
	for id, want := range expected {
		if got := summaries[id]; got != want {
			t.Errorf("文档 %d 的摘要=%q，期望首切片 %q", id, got, want)
		}
	}

	scanned, queries, matched := obs.chunkRowsOnly()
	if queries == 0 {
		t.Fatal("没捕获到摘要查询 ⇒ 断言是空的，观测器挂错了地方")
	}
	if scanned > int64(len(rows)) {
		t.Errorf("摘要查询一次扫回 %d 行，每文档只需 1 行（≤%d）；未认证的公开列表按 limit×每文档切片数放大读取全部正文。SQL=%s",
			scanned, len(rows), strings.Join(matched, " | "))
	}
}
