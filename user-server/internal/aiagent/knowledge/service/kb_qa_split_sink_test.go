// kb_qa_split_sink_test.go —— QA 拆分落库路径下沉仓储后的读写契约回归
// （AV-04c 批次7 刀3）。
//
// kb_qa_split.go 的 kb_documents 读取与 qa_candidates 落库/列表/审核 SQL 下沉
// knowledgeRepo（KBCrawlerRepository.GetDocumentByID / QACandidateRepository）后，
// 本文件锁 service 侧契约：拆分落库（显式时间戳、task_id、来源标记）、审核状态机、
// 发布建 .qa 文档、D-25 not-found 语义不变。
package service

import (
	"context"
	"strings"
	"testing"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// qaFakeLLM 固定输出假 LLM（合法 JSON 数组）。
type qaFakeLLM struct{ out string }

func (f *qaFakeLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return f.out, nil
}

func setupQASplitSink(t *testing.T) (*QASplitService, *KnowledgeBaseService, *gorm.DB, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KBDocument{}, &model.QACandidate{})
	svc := NewQASplitServiceWithRepos(
		knowledgeRepo.NewKBCrawlerRepository(db),
		knowledgeRepo.NewQACandidateRepository(db),
		&qaFakeLLM{out: `[{"question":"Q1","answer":"A1","answer_anchor":"原文片段"},{"question":"  ","answer":"A2","answer_anchor":"x"}]`},
	)
	kb := NewKnowledgeBaseServiceWithDocs(knowledgeRepo.NewKBDocumentRepository(db))
	return svc, kb, db, context.Background()
}

// 拆分落库：读 kb_documents 正文 → LLM → 候选行（空 question 行被过滤）。
func TestQASplitSink_ProcessDocument(t *testing.T) {
	svc, _, db, ctx := setupQASplitSink(t)

	doc := &model.KBDocument{TenantID: 1, Title: "qa-doc", Content: "some crawled body", FileType: ".html", Status: model.KBDocumentStatusIndexed}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	if err := svc.ProcessDocument(ctx, doc.ID, 7); err != nil {
		t.Fatalf("ProcessDocument: %v", err)
	}

	var rows []model.QACandidate
	if err := db.Where("task_id LIKE ?", "qa_%").Find(&rows).Error; err != nil {
		t.Fatalf("读候选: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("全空白 question 应被过滤（期望 1 行），got %d", len(rows))
	}
	row := rows[0]
	if row.SourceType != model.QACandidateSourceTypeCrawl || row.KBID != 7 ||
		row.Status != model.QACandidateStatusPendingReview ||
		!strings.HasPrefix(row.SourceRef, "doc:") || row.CreatedAt.IsZero() {
		t.Fatalf("候选行契约不符: %+v", row)
	}
	if row.Question != "Q1" || row.Answer != "A1" || row.AnswerAnchor != "原文片段" {
		t.Fatalf("QA 对内容不符: %+v", row)
	}

	// 文档不存在：读文档失败错误路径不变。
	if err := svc.ProcessDocument(ctx, 99999, 7); err == nil || !strings.Contains(err.Error(), "读文档失败") {
		t.Fatalf("不存在文档应报读文档失败，got %v", err)
	}
	// 空内容文档拒绝。
	empty := &model.KBDocument{TenantID: 1, Title: "empty", Content: "", FileType: ".html"}
	if err := db.Create(empty).Error; err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	if err := svc.ProcessDocument(ctx, empty.ID, 7); err == nil || !strings.Contains(err.Error(), "无内容") {
		t.Fatalf("空内容应拒绝，got %v", err)
	}
}

// 审核状态机 + D-25 not-found 语义。
func TestQASplitSink_ReviewAndPublish(t *testing.T) {
	svc, kb, db, ctx := setupQASplitSink(t)

	seed := &model.QACandidate{TenantID: 1, SourceType: model.QACandidateSourceTypeCrawl, SourceRef: "doc:1", KBID: 1,
		Question: "Q", Answer: "A", Status: model.QACandidateStatusPendingReview}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 非法跃迁拒绝。
	if err := svc.ReviewQACandidate(ctx, seed.ID, model.QACandidateStatusIndexed, "rev"); err == nil {
		t.Fatal("pending_review → indexed 应拒绝（需先 approve）")
	}
	// approve。
	if err := svc.ReviewQACandidate(ctx, seed.ID, model.QACandidateStatusApproved, "rev"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var approved model.QACandidate
	if err := db.First(&approved, seed.ID).Error; err != nil || approved.Status != model.QACandidateStatusApproved || approved.Reviewer != "rev" {
		t.Fatalf("审核回写: %+v err=%v", approved, err)
	}
	// D-25：不存在候选 not-found。
	if err := svc.ReviewQACandidate(ctx, 99999, model.QACandidateStatusApproved, "rev"); err == nil {
		t.Fatal("不存在候选应拒绝")
	}

	// publish：建 .qa 文档 + 候选置 indexed。
	if err := svc.PublishQACandidate(ctx, seed.ID, kb); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var doc model.KBDocument
	if err := db.Where("file_type = ?", ".qa").First(&doc).Error; err != nil {
		t.Fatalf("读 .qa 文档: %v", err)
	}
	if doc.Title != "Q" || doc.Content != "A" {
		t.Fatalf(".qa 文档内容不符: %+v", doc)
	}
	var indexed model.QACandidate
	if err := db.First(&indexed, seed.ID).Error; err != nil || indexed.Status != model.QACandidateStatusIndexed {
		t.Fatalf("候选应置 indexed: %+v err=%v", indexed, err)
	}
	// 候选列表过滤。
	rows, total, err := svc.ListQACandidates(ctx, model.QACandidateSourceTypeCrawl, model.QACandidateStatusIndexed, 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != seed.ID {
		t.Fatalf("列表过滤: total=%d rows=%d err=%v", total, len(rows), err)
	}
}
