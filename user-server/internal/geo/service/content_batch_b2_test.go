package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B2 验收（规划 §4.8 B2）：批量生成与单条共用 GenerateContent；逐条容错；
// 超上限截断回传 truncated_to；失败单条回传 error（前端据此标记重试）。
//
// 无 LLM key 的测试环境里 GenerateContent 走 heuristic 降级（model=heuristic_fallback），
// 恰好覆盖"逐条都有产出"的批次语义；LLM 侧故障容错由 err 分支保证（同 pipeline 口径）。
func newBatchContentSvc(t *testing.T) *ContentService {
	t.Helper()
	db := geoTestDB(t)
	return NewContentService(
		repository.NewGeoArticleRepositoryWithDB(db),
		repository.NewGeoOptimizationRepositoryWithDB(db),
		repository.NewGeoAPICallRepositoryWithDB(db),
		newGeoKBRepo(db),
		repository.NewGeoConfigRepositoryWithDB(db),
		NewLLMAdapter(),
	)
}

func TestGenerateContentBatch_BoundedAndPerItem(t *testing.T) {
	svc := newBatchContentSvc(t)
	res := svc.GenerateContentBatch(context.Background(), dto.BatchGenerateRequest{
		Keywords:  []string{"crm 对比", "crm 教程", "crm 推荐"},
		BrandName: "Acme",
		Lang:      "zh",
	})
	require.Len(t, res.Items, 3)
	assert.Equal(t, 3, res.Total)
	assert.Zero(t, res.TruncatedTo)
	// heuristic 降级路径下逐条应有产出并落库
	for _, it := range res.Items {
		assert.True(t, it.OK, "keyword=%s error=%s", it.Keyword, it.Error)
		assert.NotEmpty(t, it.Article.Title)
		assert.NotEmpty(t, it.Article.Content)
	}
	assert.Equal(t, 3, res.Succeeded)
	assert.Equal(t, 0, res.Failed)
}

func TestGenerateContentBatch_Truncation(t *testing.T) {
	svc := newBatchContentSvc(t)
	kws := make([]string, 25)
	for i := range kws {
		kws[i] = "关键词占位"
	}
	res := svc.GenerateContentBatch(context.Background(), dto.BatchGenerateRequest{Keywords: kws})
	assert.Equal(t, 25, res.Total)
	assert.Equal(t, BatchGenerateMaxKeywords, res.TruncatedTo, "超上限必须显式回传截断值")
	assert.Len(t, res.Items, BatchGenerateMaxKeywords)
}

func TestGenerateContentBatch_EmptyKeywords(t *testing.T) {
	// 绑定层 required,min=1 已挡空批；服务层再兜底为空结果（不 panic）
	svc := newBatchContentSvc(t)
	res := svc.GenerateContentBatch(context.Background(), dto.BatchGenerateRequest{})
	assert.Equal(t, 0, res.Total)
	assert.Empty(t, res.Items)
}
