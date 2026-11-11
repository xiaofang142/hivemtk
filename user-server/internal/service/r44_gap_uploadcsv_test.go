package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// r44 评测集 CSV 上传的行数上限与事务化用例。
//
// 【改前行为】UploadCSV 逐行 CreateQuestion 且无事务：中断/中途失败留下部分行；
// 行数无上限，配合 multipart 豁免（body_limit.go）可单请求写几十万行。
// 本组用例钉死三格：行数上限在**任何写库之前**触发；事务中途失败零残留；
// imported 语义不变（成功=实际入库行数，header/空行跳过不计）。
func newRagEvalGapTestService(t *testing.T) (*RagEvalGapService, *gorm.DB, *repository.RagEvalGapRepo) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.RagEvalQuestion{}, &model.RagEvalRun{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	repo := repository.NewRagEvalGapRepoWithDB(database)
	svc := NewRagEvalGapService(repo, nil, nil)
	return svc, database, repo
}

func countRagEvalQuestions(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.RagEvalQuestion{}).Count(&n).Error; err != nil {
		t.Fatalf("清点 rag_eval_questions 失败: %v", err)
	}
	return n
}

// 负例②：有效数据行超上限必须在写库前拒绝，零残留。
func TestUploadCSV_RowLimitRejects_BeforeAnyWrite(t *testing.T) {
	svc, database, _ := newRagEvalGapTestService(t)

	var sb strings.Builder
	sb.WriteString("question,answer\n")
	for i := 0; i <= maxEvalCSVRows; i++ {
		sb.WriteString("Q, A\n")
	}
	n, err := svc.UploadCSV(context.Background(), strings.NewReader(sb.String()), "prod-row-limit")
	if err == nil {
		t.Fatalf("超过 maxEvalCSVRows=%d 行必须拒绝，实际放行 imported=%d", maxEvalCSVRows, n)
	}
	if n != 0 {
		t.Fatalf("拒绝时 imported 应为 0，实际 %d", n)
	}
	if got := countRagEvalQuestions(t, database); got != 0 {
		t.Fatalf("上限拒绝必须发生在任何写库之前，实际残留 %d 行", got)
	}
}

// 防改坏守卫：header 跳过 + 空行跳过 + imported=有效行数（语义与改前一致）。
func TestUploadCSV_HappyPath_ImportedSemantics(t *testing.T) {
	svc, database, _ := newRagEvalGapTestService(t)

	csv := "question,answer\nQ1,A1\n\nQ2,\n"
	n, err := svc.UploadCSV(context.Background(), strings.NewReader(csv), "prod-happy")
	if err != nil {
		t.Fatalf("合法 CSV 不应报错: %v", err)
	}
	if n != 2 {
		t.Fatalf("imported 应为 2（header/空行跳过），实际 %d", n)
	}
	if got := countRagEvalQuestions(t, database); got != 2 {
		t.Fatalf("入库行数应为 2，实际 %d", got)
	}
	var q model.RagEvalQuestion
	if err := database.Where("question = ?", "Q2").First(&q).Error; err != nil {
		t.Fatalf("回查 Q2 失败: %v", err)
	}
	if q.Answer != "" || q.ProductID != "prod-happy" {
		t.Fatalf("字段映射不符：answer=%q product_id=%q", q.Answer, q.ProductID)
	}
}

// 负例②：中途失败零残留——第 2 行违反 product_id varchar(64) 约束时，
// 第 1 行（已成功的）必须被整体回滚，不允许留部分行。
func TestUploadCSV_FailurePath_ZeroResidualRows(t *testing.T) {
	svc, database, _ := newRagEvalGapTestService(t)

	// service 层所有行共用同一 product_id，>64 字符时首行即失败；
	// 「中途失败」这一格由 TestRagEvalGapRepo_CreateQuestionsInTx_MidBatchRollback
	// 在仓储层用 [合法, 超长, 合法] 混批直接钉死。这里钉的是端到端契约：
	// 失败 → error + imported=0 + 表内零残留。
	longProductID := strings.Repeat("p", 65)
	n, err := svc.UploadCSV(context.Background(), strings.NewReader("question,answer\nQ1,A1\nQ2,A2\n"), longProductID)
	if err == nil {
		t.Fatalf("product_id 超 varchar(64) 必须失败，实际 imported=%d", n)
	}
	if got := countRagEvalQuestions(t, database); got != 0 {
		t.Fatalf("失败必须整体回滚零残留，实际残留 %d 行", got)
	}
}
