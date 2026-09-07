package service

import (
	"testing"

	"hivemtk-user/internal/dto"
	textutil "hivemtk-user/internal/pkg/utils/text"
)

func TestNewContextBudget_Default8K(t *testing.T) {
	b := NewContextBudget(8192)
	if b.TotalContext != 8192 {
		t.Errorf("TotalContext=%d, want 8192", b.TotalContext)
	}
	if b.BaseTokens != defaultBaseTokens {
		t.Errorf("BaseTokens=%d, want %d", b.BaseTokens, defaultBaseTokens)
	}
	if b.ReserveTokens != defaultReserveTokens {
		t.Errorf("ReserveTokens=%d, want %d", b.ReserveTokens, defaultReserveTokens)
	}
	// 剩余 4692 token, RAG 70% = 3284
	if b.BudgetRAG == 0 || b.BudgetPrompt == 0 {
		t.Error("BudgetRAG/BudgetPrompt 不应为 0")
	}
	t.Logf("8k ctx: RAG=%d Prompt=%d", b.BudgetRAG, b.BudgetPrompt)
}

func TestNewContextBudget_Small2K(t *testing.T) {
	b := NewContextBudget(2048)
	t.Logf("2k ctx: RAG=%d Prompt=%d Base=%d Reserve=%d", b.BudgetRAG, b.BudgetPrompt, b.BaseTokens, b.ReserveTokens)
	if b.BudgetPrompt < 0 {
		t.Error("BudgetPrompt 不应为负数")
	}
}

func TestNewContextBudget_ZeroFallback(t *testing.T) {
	b := NewContextBudget(0)
	if b.TotalContext != defaultContextWindow {
		t.Errorf("zero → TotalContext=%d, want %d", b.TotalContext, defaultContextWindow)
	}
}

func TestTruncateRAGChunks_SortedByScore(t *testing.T) {
	b := NewContextBudget(8192)

	// 造 5 个块: 每个 500 中字 = 250 token + 20 = 270 token
	chunks := []dto.RAGChunk{}
	for i := 0; i < 5; i++ {
		content := ""
		for j := 0; j < 500; j++ {
			content += "中"
		}
		chunks = append(chunks, dto.RAGChunk{
			Content: content,
			Score:   0.5 + float64(i)*0.1, // 0.5, 0.6, 0.7, 0.8, 0.9
		})
	}

	result, dropped := b.TruncateRAGChunks(chunks)
	t.Logf("输入 %d chunks, 裁剪后 %d, dropped=%d, RAG 预算=%d",
		len(chunks), len(result), dropped, b.RAGTokenBudget())

	if len(result) == 0 {
		t.Fatal("裁剪后 chunks 为空!")
	}

	// 验证 score 降序
	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Errorf("第 %d 个 score=%.2f > 第 %d 个 score=%.2f (应降序)",
				i, result[i].Score, i-1, result[i-1].Score)
		}
	}

	// 验证总 token 在预算内
	totalTokens := 0
	for _, c := range result {
		totalTokens += textutil.EstimateTokens(c.Content) + 20
	}
	t.Logf("裁剪后总 token=%d / 预算=%d", totalTokens, b.RAGTokenBudget())
	if totalTokens > b.RAGTokenBudget() {
		t.Errorf("裁剪后总 token=%d 超预算 %d!", totalTokens, b.RAGTokenBudget())
	}
}

func TestTruncateRAGChunks_Empty(t *testing.T) {
	b := NewContextBudget(8192)
	result, dropped := b.TruncateRAGChunks(nil)
	if result != nil || dropped != 0 {
		t.Errorf("nil 输入应返回 nil,0, 实际 %v,%d", result, dropped)
	}
}
