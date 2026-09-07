package service

import (
	"sort"

	"hivemtk-user/internal/dto"
	textutil "hivemtk-user/internal/pkg/utils/text"
)

// 默认 context window（当 provider 没配置时的安全默认）
const (
	defaultContextWindow = 8192

	// L0: System Persona 硬保留预算（token）
	defaultBaseTokens = 1500

	// L3: Response Reserve 硬保留预算（token）
	defaultReserveTokens = 2000

	// L1: RAG/SOP/资产 预算占比（剩余的 70% 给 RAG，30% 给 Prompt）
	defaultRAGRatio = 0.7
)

// ContextBudget 统一 token 预算管理器
//
// 分层预算模型：
//
//	L0: System Persona    (BaseTokens)     —— 硬保留，不裁剪
//	L1: Asset/SOP/RAG     (BudgetRAG)      —— 按 relevance 排序后动态填充
//	L2: Current Turn      (BudgetPrompt)   —— buildPrompt 含历史对话
//	L3: Response Reserve  (ReserveTokens)  —— 给 LLM 输出，不裁剪
//
// Graceful Degradation 顺序（超限依次触发）：
//  1. 超 BudgetRAG  → 按 score 从低到高丢 chunk（keep top-K by score）
//  2. 超 BudgetPrompt → 丢旧历史消息（已有 fetchHistoryWithinTokenBudget）
//  3. 还是超        → 让 dispatcher fallback（现有机制）
type ContextBudget struct {
	TotalContext  int // 模型 ctx window (例: 8192)
	BaseTokens    int // L0 System Persona 硬保留
	ReserveTokens int // L3 Response Reserve 硬保留
	BudgetRAG     int // L1 RAG/SOP/资产 预算
	BudgetPrompt  int // L2 buildPrompt（含历史）预算
}

// NewContextBudget 从 provider context_window 或默认值创建
func NewContextBudget(ctxSize int) *ContextBudget {
	if ctxSize <= 0 {
		ctxSize = defaultContextWindow
	}

	// 硬保留：Base(Persona) + Reserve(Response)
	base := defaultBaseTokens
	reserve := defaultReserveTokens

	// 极小 ctx（如 2k）：按比例压缩硬保留，避免负数
	if base+reserve > ctxSize {
		scale := float64(ctxSize) / float64(base+reserve)
		base = int(float64(base) * scale)
		reserve = int(float64(reserve) * scale)
	}

	b := &ContextBudget{
		TotalContext:  ctxSize,
		BaseTokens:    base,
		ReserveTokens: reserve,
	}

	// 剩余空间分给 RAG + Prompt
	remaining := ctxSize - b.BaseTokens - b.ReserveTokens
	if remaining < 512 {
		b.BudgetPrompt = remaining
		b.BudgetRAG = 0
	} else {
		b.BudgetRAG = int(float64(remaining) * defaultRAGRatio)
		b.BudgetPrompt = remaining - b.BudgetRAG
	}
	return b
}

// EstimateTokens 包装 textutil.EstimateTokens
func (b *ContextBudget) EstimateTokens(s string) int {
	return textutil.EstimateTokens(s)
}

// RAGTokenBudget 返回 RAG 可使用的 token 预算
func (b *ContextBudget) RAGTokenBudget() int {
	return b.BudgetRAG
}

// PromptTokenBudget 返回 Prompt（含历史）可使用的 token 预算
func (b *ContextBudget) PromptTokenBudget() int {
	return b.BudgetPrompt
}

// TotalInputBudget 返回 System + Prompt + RAG 的总输入预算
func (b *ContextBudget) TotalInputBudget() int {
	return b.TotalContext - b.ReserveTokens
}

// EstimateRequestTokens 估算完整请求的 token
func (b *ContextBudget) EstimateRequestTokens(systemPrompt, userPrompt string, ragChunks []dto.RAGChunk) int {
	total := textutil.EstimateTokens(systemPrompt) + textutil.EstimateTokens(userPrompt)
	for _, c := range ragChunks {
		total += textutil.EstimateTokens(c.Content) + 20 // 编号前缀开销
	}
	return total
}

// TruncateRAGChunks 按 token 预算 + relevance score 裁剪 RAG chunks
//
// 先按 Score 降序排序（最相关的放前面），然后累加 token，超 BudgetRAG 就停。
// 返回裁剪后的 chunks 和被裁剪数。
func (b *ContextBudget) TruncateRAGChunks(chunks []dto.RAGChunk) ([]dto.RAGChunk, int) {
	if len(chunks) == 0 || b.BudgetRAG <= 0 {
		return chunks, 0
	}

	// 按 score 降序排序（原地）
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].Score > chunks[j].Score
	})

	var result []dto.RAGChunk
	used := 0
	for _, c := range chunks {
		cost := textutil.EstimateTokens(c.Content) + 20 // 编号前缀 + 换行开销
		if used+cost > b.BudgetRAG {
			break
		}
		used += cost
		result = append(result, c)
	}

	dropped := len(chunks) - len(result)
	return result, dropped
}

// IsInputSafe 估算输入是否在 context window 内
func (b *ContextBudget) IsInputSafe(systemPrompt, userPrompt string, ragChunks []dto.RAGChunk) (bool, int) {
	total := b.EstimateRequestTokens(systemPrompt, userPrompt, ragChunks)
	return total+b.ReserveTokens <= b.TotalContext, total
}
