package rag_service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

// RAGService RAG服务
type RAGService struct {
	llmService *llm.LLMService
	ragEngine  *rag_core.RAGEngine

	threeTier ThreeTierSearcher
}

// ThreeTierSearcher 三级检索接口（由 rag_retrieval.RAGThreeTierService 实现）
// 接口隔离，避免 rag_service 直接依赖 rag_retrieval 包
type ThreeTierSearcher interface {
	Search(ctx context.Context, kbID, query string, topK int) (*ThreeTierResult, error)
	Stats() ThreeTierStats
}

// ThreeTierResult 三级检索结果
type ThreeTierResult struct {
	Query     string           `json:"query"`
	Chunks    []rag_core.Chunk `json:"chunks"`
	Source    string           `json:"source"`
	Score     float64          `json:"score"`
	LatencyMs int64            `json:"latency_ms"`
	FromCache bool             `json:"from_cache"`
}

// ThreeTierStats 三级检索统计
type ThreeTierStats struct {
	L1Hits int64 `json:"l1_hits"`
	L2Hits int64 `json:"l2_hits"`
	L3Hits int64 `json:"l3_hits"`
	L4Hits int64 `json:"l4_hits"`
	Misses int64 `json:"misses"`
	Total  int64 `json:"total"`
	AvgMs  int64 `json:"avg_ms"`
}

// NewRAGService 创建新的RAG服务
func NewRAGService(llmService *llm.LLMService, ragEngine *rag_core.RAGEngine) *RAGService {
	return &RAGService{
		llmService: llmService,
		ragEngine:  ragEngine,
	}
}

// SetThreeTier 注入三级检索（可选）
func (r *RAGService) SetThreeTier(t ThreeTierSearcher) {
	r.threeTier = t
}

// HasThreeTier 是否已注入三级检索
func (r *RAGService) HasThreeTier() bool {
	return r.threeTier != nil
}

func (r *RAGService) retrieve(ctx context.Context, kbID, query string, topK int) ([]rag_core.Chunk, string, error) {
	if r.threeTier != nil && kbID != "" {
		res, err := r.threeTier.Search(ctx, kbID, query, topK)
		if err == nil {
			return res.Chunks, res.Source, nil
		}
	}
	chunks, err := r.ragEngine.Search(ctx, query, topK)
	if err != nil {
		return nil, "", err
	}
	return chunks, "engine", nil
}

// QueryRequest RAG查询请求
type QueryRequest struct {
	Query     string              `json:"query"`
	RAGConfig *rag_core.RAGConfig `json:"rag_config,omitempty"`
	LLMConfig *llm.LLMConfig      `json:"llm_config,omitempty"`
	Context   map[string]any      `json:"context,omitempty"`
	KBID      string              `json:"kb_id,omitempty"`
	TopK      int                 `json:"top_k,omitempty"`
}

// QueryResponse RAG查询响应
type QueryResponse struct {
	Answer     string           `json:"answer"`
	References []rag_core.Chunk `json:"references"`
	Metadata   map[string]any   `json:"metadata"`
	ExecTime   time.Duration    `json:"exec_time"`
}

// Query 执行RAG查询
func (r *RAGService) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	startTime := time.Now()

	if req.RAGConfig != nil {
		err := r.ragEngine.UpdateConfig(req.RAGConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to update RAG config: %w", err)
		}
	}

	llmConfig := req.LLMConfig
	if llmConfig == nil {
		llmConfig = r.llmService.GetDefaultConfig()
	}

	err := r.llmService.ValidateConfig(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM config: %w", err)
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 0
	}
	chunks, source, err := r.retrieve(ctx, req.KBID, req.Query, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}

	contextStr := buildContextString(chunks)

	prompt := buildRAGPrompt(req.Query, contextStr, req.Context)

	answer, err := r.llmService.Generate(ctx, llmConfig, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate answer: %w", err)
	}

	response := &QueryResponse{
		Answer:     answer,
		References: chunks,
		Metadata:   make(map[string]any),
		ExecTime:   time.Since(startTime),
	}

	response.Metadata["query_time"] = response.ExecTime.Seconds()
	response.Metadata["retrieved_chunks"] = len(chunks)
	response.Metadata["llm_model"] = llmConfig.Model
	response.Metadata["retrieval_source"] = source
	if r.threeTier != nil {
		response.Metadata["retrieval_mode"] = "three_tier"
	} else {
		response.Metadata["retrieval_mode"] = "engine"
	}

	return response, nil
}

func buildContextString(chunks []rag_core.Chunk) string {
	if len(chunks) == 0 {
		return "未找到相关文档。"
	}

	var contextStr string
	contextStr += "参考信息:\n"
	for i, chunk := range chunks {
		contextStr += fmt.Sprintf("[%d] 来源: %s (相似度: %.2f)\n%s\n\n",
			i+1, chunk.DocumentID, chunk.Score, chunk.Content)
	}

	return contextStr
}

func buildRAGPrompt(query, contextStr string, contextData map[string]any) string {
	prompt := fmt.Sprintf(`基于以下参考信息回答问题。如果参考信息不足以回答，请明确说明。

参考信息:
%s

问题: %s

回答:`, contextStr, query)

	if len(contextData) > 0 {
		contextJSON, _ := json.Marshal(contextData)
		prompt += fmt.Sprintf("\n\n额外上下文: %s", string(contextJSON))
	}

	return prompt
}

// AddKnowledgeBaseDocument 添加知识库文档
func (r *RAGService) AddKnowledgeBaseDocument(ctx context.Context, doc rag_core.Document) error {
	docs := []rag_core.Document{doc}
	return r.ragEngine.AddDocuments(ctx, docs)
}

// BatchAddKnowledgeBaseDocuments 批量添加知识库文档
func (r *RAGService) BatchAddKnowledgeBaseDocuments(ctx context.Context, docs []rag_core.Document) error {
	return r.ragEngine.AddDocuments(ctx, docs)
}

// DeleteKnowledgeBaseDocument 删除知识库文档
func (r *RAGService) DeleteKnowledgeBaseDocument(ctx context.Context, docID string) error {
	return r.ragEngine.DeleteDocument(ctx, docID)
}

// StructuredQuery 结构化RAG查询
func (r *RAGService) StructuredQuery(ctx context.Context, req *QueryRequest, responseSchema any) (any, error) {
	startTime := time.Now()

	if req.RAGConfig != nil {
		err := r.ragEngine.UpdateConfig(req.RAGConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to update RAG config: %w", err)
		}
	}

	llmConfig := req.LLMConfig
	if llmConfig == nil {
		llmConfig = r.llmService.GetDefaultConfig()
	}

	llmConfig.ResponseFormat = "json_object"

	err := r.llmService.ValidateConfig(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM config: %w", err)
	}

	chunks, source, err := r.retrieve(ctx, req.KBID, req.Query, req.TopK)
	if err != nil {
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}

	contextStr := buildContextString(chunks)

	prompt := buildStructuredRAGPrompt(req.Query, contextStr, req.Context, responseSchema)

	result, err := r.llmService.GenerateStructured(ctx, llmConfig, prompt, responseSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to generate structured answer: %w", err)
	}

	metadata := make(map[string]any)
	metadata["exec_time"] = time.Since(startTime).Seconds()
	metadata["retrieved_chunks"] = len(chunks)
	metadata["llm_model"] = llmConfig.Model
	metadata["retrieval_source"] = source
	if r.threeTier != nil {
		metadata["retrieval_mode"] = "three_tier"
	} else {
		metadata["retrieval_mode"] = "engine"
	}

	if resultMap, ok := result.(map[string]any); ok {
		resultMap["_metadata"] = metadata
		result = resultMap
	}

	return result, nil
}

// ThreeTierStats 透传三级检索统计（便于 Controller 暴露）
func (r *RAGService) ThreeTierStats() (ThreeTierStats, bool) {
	if r.threeTier == nil {
		return ThreeTierStats{}, false
	}
	return r.threeTier.Stats(), true
}

func buildStructuredRAGPrompt(query, contextStr string, contextData map[string]any, schema any) string {
	schemaJSON, _ := json.Marshal(schema)

	prompt := fmt.Sprintf(`基于以下参考信息回答问题，并按照指定的JSON格式返回结果。

参考信息:
%s

问题: %s

请严格按照以下JSON Schema返回结果:
%s

回答:`, contextStr, query, string(schemaJSON))

	if len(contextData) > 0 {
		contextJSON, _ := json.Marshal(contextData)
		prompt += fmt.Sprintf("\n\n额外上下文: %s", string(contextJSON))
	}

	return prompt
}
