package service

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"
)

func toPgVector(vec []float32) string {
	parts := make([]string, len(vec))
	for i, f := range vec {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// TestRagSearcher_RealVectorSearch 向量检索链路测试（自包含，两种向量来源都跑）
//
// 使用测试库为知识分片灌入向量，验证 pgvector 余弦相似度召回。
// 不依赖外部 DB_* 环境变量，可在标准 go test 中运行（项目规则不允许跳过）。
//
// 向量来源随环境切换、断言强度也随之切换（见 embCfg 处的说明）：
//   - 真 TEI bge-m3（EMBEDDING_ALLOW_FALLBACK 未开）：结构 + 语义断言（相似度下限、关键词命中）
//   - CI 的 hash 兜底：结构 + 维度/非零/确定性/降序断言，语义断言无对象可量，
//     但既不降阈值也不跳过用例
func TestRagSearcher_RealVectorSearch(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeChunk{}, &model.KnowledgeDocument{})
	testmigrate.RunTestMigrations(t, database)
	s := NewRagSearcherWithDB(database)
	if s.db == nil {
		t.Fatalf("DB 未初始化")
	}

	// embCfg.AllowFallback 决定向量来源，且是二元的：EmbedWithLane 在
	// AllowFallback=true 时整批直接返回 hash 伪向量、根本不请求 provider
	// （embedding.go 的分支），false 时必定请求 provider。
	// 因此 false 等价于「这批向量真的带语义」，可断言相似度下限与关键词命中；
	// true（CI 注入 EMBEDDING_ALLOW_FALLBACK=true）时 hash 向量互相关似度≈0，
	// 语义断言量的是桩而不是代码 —— 换成不依赖语义的结构化断言，不降阈值、不跳过。
	embCfg := s.embeddingService.DefaultConfig()
	semanticVectors := !embCfg.AllowFallback
	if !semanticVectors {
		t.Logf("⚠️ 当前为 hash 兜底向量模式（EMBEDDING_ALLOW_FALLBACK），语义类断言降级为结构化断言")
	}

	seed := []struct {
		productID string
		content   string
	}{
		{"1", "7天无理由退货政策：收到商品后7天内可申请退货，运费由买家承担"},
		{"1", "质量问题退货：商品存在质量问题时可免费退货并补偿运费"},
		{"2", "发货时间：现货商品在付款后48小时内发货，预售商品以页面标注为准"},
		{"2", "快递配送：默认发顺丰，偏远地区发EMS，一般2-3天送达"},
	}
	ctx := context.Background()
	seedVecs := map[string][]float32{}
	for _, item := range seed {
		chunk := model.KnowledgeChunk{
			ProductID:  item.productID,
			Content:    item.content,
			ChunkIndex: 0,
		}
		if err := database.Create(&chunk).Error; err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		vec, err := s.embeddingService.EmbedOne(ctx, embCfg, item.content)
		if err != nil {
			t.Fatalf("embed chunk: %v", err)
		}
		seedVecs[item.content] = vec
		if err := database.Exec("UPDATE knowledge_chunks SET embedding = ?::vector WHERE id = ?", toPgVector(vec), chunk.ID).Error; err != nil {
			t.Fatalf("update embedding: %v", err)
		}
	}

	inGoBest := func(qVec []float32) (string, float64) {
		best, bestSim := "", -1.0
		for _, item := range seed {
			if sim := cosineSim(qVec, seedVecs[item.content]); sim > bestSim {
				bestSim = sim
				best = item.content
			}
		}
		return best, bestSim
	}

	checkVectorSearch := func(t *testing.T, query, keyword string) {
		qVec, err := s.embeddingService.EmbedOne(ctx, embCfg, query)
		if err != nil {
			t.Fatalf("embed query: %v", err)
		}
		best, bestSim := inGoBest(qVec)

		rows, err := s.vectorSearch(ctx, "", query, 3)
		if err != nil {
			t.Fatalf("vectorSearch 失败: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("vectorSearch 未返回任何 chunk")
		}
		top := rows[0]

		// 以下两条与向量来源无关：校验的是 pgvector 的 <=> 排序与打分本身
		if top.row.Content != best {
			t.Errorf("pgvector Top1=%q 与 in-Go 最相似分片=%q 不一致", top.row.Content, best)
		}
		want := cosineSim(qVec, seedVecs[top.row.Content])
		if d := top.score - want; d > 0.05 || d < -0.05 {
			t.Errorf("pgvector 余弦相似度=%.4f 与 in-Go=%.4f 偏差过大", top.score, want)
		}
		for i := 1; i < len(rows); i++ {
			if rows[i-1].score < rows[i].score {
				t.Errorf("vectorSearch 未按相似度降序返回: 第 %d 条 %.4f < 第 %d 条 %.4f",
					i, rows[i-1].score, i-1, rows[i].score)
			}
		}

		if semanticVectors {
			if top.score < 0.2 {
				t.Errorf("Top1 余弦相似度过低: %.4f (期望 >= 0.2, in-Go=%.4f)", top.score, bestSim)
			}
			if !contains(top.row.Content, keyword) {
				t.Errorf("Top1 内容与预期关键词无关: %s", top.row.Content)
			}
		} else {
			if len(qVec) != embCfg.Dimension {
				t.Errorf("query 向量维度=%d，与配置 %d 不一致（pgvector 列会直接报错）", len(qVec), embCfg.Dimension)
			}
			if n := l2Norm(qVec); n == 0 {
				t.Errorf("query 向量为零向量，相似度无定义")
			}
			repeat, err := s.embeddingService.EmbedOne(ctx, embCfg, query)
			if err != nil {
				t.Fatalf("embed query(重复): %v", err)
			}
			if d := cosineSim(qVec, repeat); d < 0.999 {
				t.Errorf("同一文本两次向量化不一致: cosine=%.6f", d)
			}
		}
		t.Logf("✅ query=%q Top1=%q cosine=%.4f (in-Go=%.4f)", query, top.row.Content, top.score, want)
	}

	t.Run("退货政策检索(pgvector余弦)", func(t *testing.T) {
		checkVectorSearch(t, "你们支持几天无理由退货", "退")
	})

	t.Run("发货时间检索(pgvector余弦)", func(t *testing.T) {
		checkVectorSearch(t, "多久能发货", "发货")
	})

	t.Run("公共Search返回相关内容", func(t *testing.T) {
		chunks, err := s.Search(ctx, "你们支持几天无理由退货", 3)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		if len(chunks) == 0 {
			t.Fatal("未返回任何 chunk")
		}
		if len(chunks) > 3 {
			t.Errorf("topK=3 却返回 %d 条", len(chunks))
		}
		// 语义断言与向量来源同源（与 checkVectorSearch 同一口径）。
		// 反例是第二十六轮 CI 首跑：同一段代码、同为 hash 兜底，
		// CI 的 Top1=「发货时间…」score=1.0000，本地 Top1=「7天无理由退货…」score=0.5789，
		// 红绿取决于运行期检索路径而非被测代码 —— 这种断言量的是桩，不是代码。
		if semanticVectors {
			if !contains(chunks[0].Content, "退") && !contains(chunks[0].Content, "换") {
				t.Errorf("Top1 内容与退货无关: %s", chunks[0].Content)
			}
		} else {
			// 兜底模式下改查召回集的形状：条条来自种子集、不重复、带正文与分数。
			seen := map[string]bool{}
			for _, c := range chunks {
				if _, ok := seedVecs[c.Content]; !ok {
					t.Errorf("Search 召回了种子集之外的分片: %q", c.Content)
				}
				if seen[c.Content] {
					t.Errorf("Search 返回重复分片: %q", c.Content)
				}
				seen[c.Content] = true
				if strings.TrimSpace(c.Content) == "" {
					t.Error("召回分片正文为空")
				}
			}
		}
		t.Logf("✅ Search Top1=%s score=%.4f", chunks[0].Content, chunks[0].Score)
	})

	t.Run("空 query 走 BM25 兜底", func(t *testing.T) {
		chunks, err := s.Search(ctx, "", 3)
		if err != nil {
			t.Fatalf("空 query 检索应不报错: %v", err)
		}
		_ = chunks
	})

	t.Run("SearchIndex 单产品过滤", func(t *testing.T) {
		chunks, err := s.SearchIndex(ctx, "1", "退货", 3, nil)
		if err != nil {
			t.Fatalf("SearchIndex 失败: %v", err)
		}
		if len(chunks) == 0 {
			t.Fatal("未返回任何 chunk")
		}
		found := false
		for _, c := range chunks {
			if contains(c.Content, "退") {
				found = true
			}
			if c.Score < 0 {
				t.Errorf("score 不能为负: %v", c.Score)
			}
		}
		if !found {
			t.Errorf("SearchIndex(退货) 未返回任何退货相关分片")
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// l2Norm 零向量的余弦相似度无定义（cosineSim 会直接返回 0），故单独校验范数。
func l2Norm(vec []float32) float64 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum)
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
