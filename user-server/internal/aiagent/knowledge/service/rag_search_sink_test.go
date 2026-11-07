// rag_search_sink_test.go —— RAG 检索读口下沉仓储后的读写契约回归
// （AV-04c 批次7 刀4）。
//
// rag_searcher 系的三段直查（chunk 权重 / BM25-lite 扫描 / 向量召回）下沉
// knowledgeRepo（SQL/谓词/排序/可见性闸逐字保留），三处句柄传递接缝
// （CachedEmbeddingClient / HybridSearcher / would-hide 观测）经 kbsearch 与仓储
// 收口后，本文件锁 service 侧检索路径：混合检索器就绪、BM25 产品内扫描命中
// 种子行、权重读取、SearchIndexWithConfig 端到端返回（假 embedding 桩）。
package service

import (
	"context"
	"testing"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 混合检索器经 kbsearch 接缝构造就绪；仓储 Connected 探针替代 s.db == nil 判断。
func TestRagSearchSink_HybridWired(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.KnowledgeChunk{})
	s := NewRagSearcher()
	if !s.chunkRepo.Connected() {
		t.Fatal("有库时分段仓储应判 Connected")
	}
	if s.hybridSearcher == nil {
		t.Fatal("构造期应经 kbsearch 接缝装配 HybridSearcher")
	}
}

// BM25 产品内扫描（含版本闸 WhereVisible）与权重读取契约。
func TestRagSearchSink_BM25ScanAndWeights(t *testing.T) {
	db := testutil.NewTestDB(t, &model.KnowledgeChunk{})
	ctx := context.Background()
	repo := knowledgeRepo.NewKnowledgeChunkRepository(db)

	chunks := []model.KnowledgeChunk{
		{ProductID: "p1", Content: "退货政策七天内可退", Weight: 2, EmbeddingSource: "tei"},
		{ProductID: "p1", Content: "运费说明满99包邮", Weight: 1, EmbeddingSource: "tei"},
		{ProductID: "p2", Content: "其它产品内容", Weight: 1, EmbeddingSource: "tei"},
	}
	if err := db.Create(&chunks).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := repo.BM25ScanRowsByProduct(ctx, "p1", BM25ScanLimit())
	if err != nil {
		t.Fatalf("BM25ScanRowsByProduct: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("p1 产品扫描应命中 2 行，got %d", len(rows))
	}

	weights, err := repo.ChunkWeights(ctx, []uint64{chunks[0].ID, chunks[1].ID})
	if err != nil {
		t.Fatalf("ChunkWeights: %v", err)
	}
	got := map[uint64]float64{}
	for _, w := range weights {
		got[w.ID] = w.Weight
	}
	if got[chunks[0].ID] != 2 || got[chunks[1].ID] != 1 {
		t.Fatalf("权重契约不符: %+v", got)
	}
}

// SearchIndexWithConfig 端到端（假 embedding 桩，向量召回路径）：
// 构造接缝（kbsearch → ragretrieval.NewHybridSearcher）在注入配置下可运行。
func TestRagSearchSink_SearchIndexWithConfig(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.KnowledgeChunk{})
	s := NewRagSearcherWithRepo(nil)
	s.embeddingService = kbSvcFakeEmb{}
	if !s.chunkRepo.Connected() && s.chunkRepo != nil {
		t.Fatal("nil 仓储应保持 nil 语义")
	}

	// nil 仓储：SearchIndexWithConfig 报 db 未初始化（原 nil 句柄语义）。
	if _, err := s.SearchIndexWithConfig(context.Background(), "p1", "q", 5, kbSvcFakeEmb{}, nil); err == nil {
		t.Fatal("nil 仓储应报 db 未初始化")
	}

	// 就绪仓储：端到端可运行（假向量召回 0 行是合法结果）。
	s2 := NewRagSearcher()
	s2.embeddingService = kbSvcFakeEmb{}
	chunks, err := s2.SearchIndexWithConfig(context.Background(), "p-empty", "任意查询", 5, kbSvcFakeEmb{}, nil)
	if err != nil {
		t.Fatalf("SearchIndexWithConfig: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("空产品应 0 命中，got %d", len(chunks))
	}
}

// compile-time refs：防依赖误删（与 kb_gate 夹具同源的假 embedding 桩继续可用）。
var (
	_ llm.EmbeddingServiceInterface = kbSvcFakeEmb{}
	_ *ragretrieval.HybridSearcher
	_ *gorm.DB
)
