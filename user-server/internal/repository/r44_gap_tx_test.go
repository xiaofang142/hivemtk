package repository

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// CreateQuestionsInTx 事务化判据：中途失败零残留。
//
// 【改前行为】UploadCSV 逐行 CreateQuestion 无事务，第 n 行失败时前 n-1 行
// 已提交（部分行残留）；本刀收口为整批单事务，任一行失败整体回滚。
// 用 [合法, 超长 product_id, 合法] 混批构造"第 2 行失败"的中途失败场景：
// 回滚生效时三行全无；若事务边界失效（假事务/自动提交），第 1 行会残留。
func TestRagEvalGapRepo_CreateQuestionsInTx_MidBatchRollback(t *testing.T) {
	database := testutil.NewTestDB(t, &model.RagEvalQuestion{}, &model.RagEvalRun{})
	repo := NewRagEvalGapRepoWithDB(database)

	count := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		if err := database.Model(&model.RagEvalQuestion{}).Count(&n).Error; err != nil {
			t.Fatalf("清点失败: %v", err)
		}
		return n
	}

	batch := []*model.RagEvalQuestion{
		{TenantID: 1, ProductID: "prod-tx-ok-1", Question: "Q1", Answer: "A1"},
		// product_id varchar(64)：65 字符在 Postgres 上必报
		// "value too long for type character varying(64)"
		{TenantID: 1, ProductID: strings.Repeat("p", 65), Question: "Q-bad", Answer: "A"},
		{TenantID: 1, ProductID: "prod-tx-ok-2", Question: "Q3", Answer: "A3"},
	}
	err := repo.CreateQuestionsInTx(context.Background(), batch)
	if err == nil {
		t.Fatal("混批中含超长 product_id 必须报错")
	}
	if got := count(t); got != 0 {
		t.Fatalf("中途失败必须整体回滚零残留，实际残留 %d 行（假事务回归！）", got)
	}

	// 正常批次照常入库。
	okBatch := []*model.RagEvalQuestion{
		{TenantID: 1, ProductID: "prod-tx-ok-3", Question: "Q4", Answer: "A4"},
		{TenantID: 1, ProductID: "prod-tx-ok-3", Question: "Q5", Answer: "A5"},
	}
	if err := repo.CreateQuestionsInTx(context.Background(), okBatch); err != nil {
		t.Fatalf("合法批次不应报错: %v", err)
	}
	if got := count(t); got != 2 {
		t.Fatalf("正常批次应入库 2 行，实际 %d", got)
	}
}
