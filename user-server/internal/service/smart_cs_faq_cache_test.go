package service

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// vec1024WithCos 造一对 1024 维向量里的第二个：与 [1,0,...] 的余弦正好是 cos。
// 维度必须是 1024，rag_answer_cache.query_vector 就是 vector(1024)，
// 插别的维度会在 Put 那一步报 PG 错，看起来像"缓存坏了"其实是夹具形状不对。
func vec1024WithCos(cos float64) []float32 {
	v := make([]float32, 1024)
	v[0] = float32(cos)
	v[1] = float32(math.Sqrt(1 - cos*cos))
	return v
}

func vec1024Unit() []float32 {
	v := make([]float32, 1024)
	v[0] = 1
	return v
}

// newFAQCacheKB 建一行知识库并回读它的主键：缓存键的 kb_id 就是这个主键的十进制串，
// 与编排器 resolveFAQKB 之后 strconv.FormatUint(kb.ID) 的口径一致。
func newFAQCacheKB(t *testing.T, database *gorm.DB, code string) string {
	t.Helper()
	if err := database.Exec(`
		INSERT INTO knowledge_bases (kb_code, type, name, owner_type, enabled, created_at, updated_at)
		VALUES (?, 'faq', '缓存挂载对照库', 'shared', true, ?, ?)`, code, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("插入对照 KB 失败: %v", err)
	}
	var kbID uint
	if err := database.Raw(`SELECT id FROM knowledge_bases WHERE kb_code = ?`, code).
		Scan(&kbID).Error; err != nil || kbID == 0 {
		t.Fatalf("回读对照 KB 失败: id=%d err=%v", kbID, err)
	}
	return strconv.FormatUint(uint64(kbID), 10)
}

func newOrchestratorWithThreshold(threshold float64) *SmartCSOrchestrator {
	return NewSmartCSOrchestrator(nil, &OrchestratorConfig{
		ConfidenceThreshold: threshold,
		EnableAutoReply:     false,
		MaxAIConsecutive:    1,
	}, nil)
}

// TestSetFAQAnswerCache_InjectsBothHalves 缓存服务与向量化服务必须成对挂上。
//
// 这一格守的是"接线之后确实有人能把它挂上"：过去这两个字段抄的是包级全局
// globalFAQCache/globalFAQEmbedder，而全局唯一的写点 service.SetGlobalFAQAnswerCache
// 实测全仓零调用方，于是构造出来的每个编排器都拿到两个 nil，
// HandleIncomingWithAgent 里那句 `o.faqCache != nil && o.faqEmbedder != nil` 恒假。
func TestSetFAQAnswerCache_InjectsBothHalves(t *testing.T) {
	o := newOrchestratorWithThreshold(0.5)
	if o.faqCache != nil || o.faqEmbedder != nil {
		t.Fatal("新建的编排器不该自带缓存：挂载必须是一个显式动作")
	}

	svc := ragcache.NewFAQAnswerCacheService(nil, nil, 0, 0)
	embedder := llm.NewEmbeddingService()
	o.SetFAQAnswerCache(svc, embedder)
	if o.faqCache != svc {
		t.Errorf("SetFAQAnswerCache 没把缓存服务挂上：字段 = %T", o.faqCache)
	}
	if o.faqEmbedder != embedder {
		t.Errorf("SetFAQAnswerCache 没把向量化服务挂上：字段 = %T", o.faqEmbedder)
	}
}

// TestLookupFAQAnswerCache_ExactHitServesCachedAnswer 走真库：写一条进 rag_answer_cache，
// 再用同一个 query 向量查，编排器必须把缓存答案当作本轮回答交回来（跳过生成的那条出口）。
//
// 置信度阈值取 0.5：精确层命中的相似度按定义是 1（见 rag/cache/service.go 里那条注释），
// 这一格同时也是那个口径的现场证据 —— 精确命中若交回 0，这里就会变成"没命中"。
func TestLookupFAQAnswerCache_ExactHitServesCachedAnswer(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeBase{}, &ragcache.RAGAnswerCache{})
	ctx := context.Background()
	kbIDStr := newFAQCacheKB(t, database, "kb-faq-cache-mount")

	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0, 0)
	vec := vec1024Unit()
	const cachedAnswer = "挂载对照答案：满三百减三十。"
	if err := cacheSvc.Store(ctx, ragcache.StoreRequest{
		KBID: kbIDStr, PromptVersion: faqPromptVersion, QueryVector: vec,
		Answer: cachedAnswer, FromKnowledgeBase: true,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	o := newOrchestratorWithThreshold(0.5)
	o.SetFAQAnswerCache(cacheSvc, llm.NewEmbeddingService())

	result := &HandleResult{SessionID: "s-cache-mount"}
	got, hit := o.lookupFAQAnswerCache(ctx, kbIDStr, faqPromptVersion, vec, result)
	if !hit {
		t.Fatal("精确层已有一条同向量的答案，编排器却判为未命中（缓存挂了也没被用到）")
	}
	if got.Reply != cachedAnswer {
		t.Errorf("回的是生成答案而不是缓存答案：got=%q want=%q", got.Reply, cachedAnswer)
	}
	if !got.AIReplied || got.HandlerType != model.HandlerTypeAI {
		t.Errorf("缓存命中应记成 AI 已答：handler=%v aiReplied=%v", got.HandlerType, got.AIReplied)
	}
	if math.Abs(got.Confidence-1) > 1e-9 {
		t.Errorf("精确命中的置信度应为 1，got=%.6f", got.Confidence)
	}
}

// TestLookupFAQAnswerCache_SimilarityBelowConfidenceFallsThrough 语义层命中但相似度低于
// 编排器置信度阈值时，缓存不得成为"免检通道"：交回未命中，让正常降级链继续跑。
func TestLookupFAQAnswerCache_SimilarityBelowConfidenceFallsThrough(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeBase{}, &ragcache.RAGAnswerCache{})
	ctx := context.Background()
	kbIDStr := newFAQCacheKB(t, database, "kb-faq-cache-falls-through")

	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0, 0)
	if err := cacheSvc.Store(ctx, ragcache.StoreRequest{
		KBID: kbIDStr, PromptVersion: faqPromptVersion, QueryVector: vec1024WithCos(0.98),
		Answer: "不该被采用的缓存答案。", FromKnowledgeBase: true,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	o := newOrchestratorWithThreshold(0.99)
	o.SetFAQAnswerCache(cacheSvc, llm.NewEmbeddingService())

	result := &HandleResult{SessionID: "s-cache-falls-through"}
	got, hit := o.lookupFAQAnswerCache(ctx, kbIDStr, faqPromptVersion, vec1024Unit(), result)
	if hit {
		t.Fatalf("相似度 0.98 < 阈值 0.99，本不该采用缓存：got=%+v", got)
	}
	if result.Reply != "" {
		t.Errorf("未命中时不该改动 result.Reply：%q", result.Reply)
	}
}
