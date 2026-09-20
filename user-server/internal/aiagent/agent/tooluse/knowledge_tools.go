package tooluse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

// KnowledgeToolDeps 知识工具依赖
type KnowledgeToolDeps struct {
	KnowledgeService *knowledgesvc.KnowledgeService
	RagSearcher      *knowledgesvc.RagSearcher
	RagRepo          *knowledgerepo.RagConfigRepository
	DocRepo          *knowledgerepo.KnowledgeDocumentRepository
	SearchLogRepo    *knowledgerepo.KnowledgeSearchLogRepository
	FeedbackRepo     *knowledgerepo.KnowledgeFeedbackRepository
	ChunkRepo        *knowledgerepo.KnowledgeChunkRepository
	DB               *gorm.DB
}

// NewKnowledgeToolDeps 创建知识工具依赖（使用全局 DB）
func NewKnowledgeToolDeps() KnowledgeToolDeps {
	gdb := db.GetDB()
	ks := knowledgesvc.NewKnowledgeService()
	return KnowledgeToolDeps{
		KnowledgeService: ks,
		RagSearcher:      knowledgesvc.NewRagSearcher(),
		RagRepo:          knowledgerepo.NewRagConfigRepository(gdb),
		DocRepo:          knowledgerepo.NewKnowledgeDocumentRepository(gdb),
		SearchLogRepo:    knowledgerepo.NewKnowledgeSearchLogRepository(gdb),
		FeedbackRepo:     knowledgerepo.NewKnowledgeFeedbackRepository(gdb),
		ChunkRepo:        knowledgerepo.NewKnowledgeChunkRepository(gdb),
		DB:               gdb,
	}
}

// NewKnowledgeToolDepsWithDB 创建知识工具依赖（带 DB，用于测试）
func NewKnowledgeToolDepsWithDB(gdb *gorm.DB) KnowledgeToolDeps {
	ks := knowledgesvc.NewKnowledgeServiceWithDB(gdb)
	return KnowledgeToolDeps{
		KnowledgeService: ks,
		RagSearcher:      knowledgesvc.NewRagSearcherWithDB(gdb),
		RagRepo:          knowledgerepo.NewRagConfigRepository(gdb),
		DocRepo:          knowledgerepo.NewKnowledgeDocumentRepository(gdb),
		SearchLogRepo:    knowledgerepo.NewKnowledgeSearchLogRepository(gdb),
		FeedbackRepo:     knowledgerepo.NewKnowledgeFeedbackRepository(gdb),
		ChunkRepo:        knowledgerepo.NewKnowledgeChunkRepository(gdb),
		DB:               gdb,
	}
}

// BuildKnowledgeTools 构造全部 4 个知识工具（不注册到 Registry）
//
// 调用方：KnowledgeToolProvider.Provide()
func BuildKnowledgeTools(deps KnowledgeToolDeps) []Tool {
	return []Tool{
		NewRagSearchTool(deps),
		NewKnowledgeFeedbackTool(deps),
		NewKnowledgeAddDocTool(deps),
		NewKnowledgeListKBTool(deps),
	}
}

// RegisterKnowledgeTools 注册所有 4 个知识工具到 registry
func RegisterKnowledgeTools(registry *ToolRegistry, deps KnowledgeToolDeps) error {
	tools := BuildKnowledgeTools(deps)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("注册知识工具 %s 失败：%w", t.Name(), err)
		}
	}
	return nil
}

// MustRegisterKnowledgeTools 注册所有知识工具，出错 panic
func MustRegisterKnowledgeTools(registry *ToolRegistry, deps KnowledgeToolDeps) {
	if err := RegisterKnowledgeTools(registry, deps); err != nil {
		panic(err)
	}
}

// RagSearchTool RAG 检索工具
type RagSearchTool struct {
	BaseTool
	deps KnowledgeToolDeps
}

// NewRagSearchTool 创建 RAG 检索工具
func NewRagSearchTool(deps KnowledgeToolDeps) *RagSearchTool {
	return &RagSearchTool{
		BaseTool: BaseTool{
			NameVal:        "rag.search",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryKnowledge,
			DescriptionVal: "在指定知识库（RAG 产品）中检索与查询相关的文档分段。返回 top_k 个最相关的分段（含 score、content、document_id）。用于客服答疑、销售话术推荐、知识查询等场景。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"product_id":       {Type: "string", Description: "RAG 产品 ID（知识库 UUID）"},
					"query":            {Type: "string", Description: "检索查询文本（用户问题或关键词）"},
					"top_k":            {Type: "integer", Description: "返回分段数量上限（默认 5，最大 20）", Default: 5},
					"threshold":        {Type: "number", Description: "相似度阈值（0-1，默认 0.3）。低于此分数的分段将被过滤。", Default: 0.3},
					"session_id":       {Type: "string", Description: "会话 ID（可选，用于关联检索日志与反馈）"},
					"metadata_filters": {Type: "object", Description: "附加字段过滤（可选）。例如 {\"customer_id\":\"123\",\"order_id\":\"A01\"}，将检索收敛到特定业务上下文（某客户的订单知识等）。"},
				},
				Required: []string{"query"},
			},
		},
		deps: deps,
	}
}

// Execute 执行检索
func (t *RagSearchTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"query"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	productID := getArgString(args, "product_id")
	query := getArgString(args, "query")
	if strings.TrimSpace(query) == "" {
		return ErrorResult(t.Name(), errors.New("query 不能为空")), errors.New("query 不能为空")
	}

	topK, _ := GetIntArg(args, "top_k")
	if topK <= 0 {
		topK = 5
	}
	if topK > 20 {
		topK = 20
	}

	threshold := 0.3
	if v, ok := args["threshold"].(float64); ok {
		threshold = v
	}
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}

	sessionID := getArgString(args, "session_id")

	metadataFilters := parseMetadataFilters(args["metadata_filters"])

	productNumericID := productID

	start := time.Now()
	chunks, err := t.deps.RagSearcher.SearchIndex(ctx, productNumericID, query, topK, metadataFilters)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	latencyMs := int(time.Since(start).Milliseconds())

	filtered := make([]knowledgesvc.MerchantRAGChunk, 0, len(chunks))
	var maxScore, minScore, sumScore float64
	for _, c := range chunks {
		if c.Score < threshold {
			continue
		}
		filtered = append(filtered, c)
		if c.Score > maxScore {
			maxScore = c.Score
		}
		if minScore == 0 || c.Score < minScore {
			minScore = c.Score
		}
		sumScore += c.Score
	}
	avgScore := 0.0
	if len(filtered) > 0 {
		avgScore = sumScore / float64(len(filtered))
	}

	if len(filtered) == 0 && len(chunks) > 0 {
		n := topK
		if n > len(chunks) {
			n = len(chunks)
		}
		filtered = chunks[:n]
		maxScore, minScore, sumScore = 0, 0, 0
		for _, c := range filtered {
			if c.Score > maxScore {
				maxScore = c.Score
			}
			if minScore == 0 || c.Score < minScore {
				minScore = c.Score
			}
			sumScore += c.Score
		}
		if len(filtered) > 0 {
			avgScore = sumScore / float64(len(filtered))
		}
		log.Printf("[rag.search][降级] 阈值=%.2f 过滤后为空，降级返回原始 topK=%d 候选（产品=%s query=%q）", threshold, n, productNumericID, query)
	}

	if t.deps.ChunkRepo != nil {
		go func(chunks []knowledgesvc.MerchantRAGChunk) {
			bgCtx := context.Background()
			ids := make([]uint64, 0, len(chunks))
			for _, c := range chunks {
				ids = append(ids, c.ID)
			}
			if len(ids) > 0 {
				_ = t.deps.ChunkRepo.IncrementHitCount(bgCtx, ids)
			}
		}(filtered)
	}

	if t.deps.SearchLogRepo != nil {
		queryHash := hashQuery(query)
		logEntry := &model.KnowledgeSearchLog{
			ProductID:           productNumericID,
			Query:               query,
			QueryHash:           queryHash,
			TopK:                topK,
			SimilarityThreshold: threshold,
			ResultCount:         len(filtered),
			MaxScore:            maxScore,
			MinScore:            minScore,
			AvgScore:            avgScore,
			LatencyMs:           latencyMs,
			Hit:                 0,
			Source:              "tooluse.rag_search",
			SessionID:           sessionID,
		}
		if len(filtered) > 0 {
			logEntry.Hit = 1
		}
		_ = t.deps.SearchLogRepo.Create(ctx, logEntry)
	}

	return SuccessResult(t.Name(), map[string]any{
		"chunks":     filtered,
		"total":      len(filtered),
		"top_k":      topK,
		"threshold":  threshold,
		"max_score":  maxScore,
		"min_score":  minScore,
		"avg_score":  avgScore,
		"latency_ms": latencyMs,
		"query":      query,
	}), nil
}

func hashQuery(query string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(query))))
	return hex.EncodeToString(h[:])
}

func parseMetadataFilters(raw any) map[string]string {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

// KnowledgeFeedbackTool 知识反馈工具
type KnowledgeFeedbackTool struct {
	BaseTool
	deps KnowledgeToolDeps
}

// NewKnowledgeFeedbackTool 创建知识反馈工具
func NewKnowledgeFeedbackTool(deps KnowledgeToolDeps) *KnowledgeFeedbackTool {
	return &KnowledgeFeedbackTool{
		BaseTool: BaseTool{
			NameVal:        "knowledge.feedback",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryKnowledge,
			DescriptionVal: "对 RAG 检索结果进行反馈（helpful/bad/补充评论），用于持续学习优化召回质量。可在客服结束对话后由智能体自动调用，或由用户主动标记。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"product_id":  {Type: "string", Description: "RAG 产品 ID（知识库 UUID）"},
					"query":       {Type: "string", Description: "原始检索查询（用于关联检索日志）"},
					"rating":      {Type: "string", Description: "反馈评分：helpful（有帮助）/ bad（无帮助）/ neutral（一般）", Enum: []string{"helpful", "bad", "neutral"}},
					"document_id": {Type: "integer", Description: "相关文档 ID（可选，精确到文档级反馈）"},
					"chunk_id":    {Type: "integer", Description: "相关分段 ID（可选，精确到分段级反馈）"},
					"comment":     {Type: "string", Description: "补充评论（可选，如正确答案、错误原因等）"},
					"session_id":  {Type: "string", Description: "会话 ID（可选，用于关联检索日志）"},
					"operator":    {Type: "string", Description: "操作人（可选，AI Agent 名称或用户 ID）"},
				},
				Required: []string{"rating"},
			},
		},
		deps: deps,
	}
}

// Execute 执行反馈
func (t *KnowledgeFeedbackTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"rating"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	if t.deps.DB == nil {
		return ErrorResult(t.Name(), errors.New("knowledge.feedback 工具需要 DB 依赖")), errors.New("knowledge.feedback 工具需要 DB 依赖")
	}

	productID := getArgString(args, "product_id")
	query := getArgString(args, "query")
	ratingStr := getArgString(args, "rating")
	comment := getArgString(args, "comment")
	sessionID := getArgString(args, "session_id")
	operator := getArgString(args, "operator")
	if operator == "" {
		operator = "ai_agent"
	}

	var rating int
	switch strings.ToLower(ratingStr) {
	case "helpful":
		rating = 1
	case "bad":
		rating = -1
	case "neutral":
		rating = 0
	default:
		return ErrorResult(t.Name(), fmt.Errorf("rating 必须是 helpful/bad/neutral，实际：%s", ratingStr)), fmt.Errorf("rating 必须是 helpful/bad/neutral，实际：%s", ratingStr)
	}

	var documentID, chunkID *uint64
	if docID, ok := GetIntArgSafe(args, "document_id"); ok && docID > 0 {
		u := uint64(docID)
		documentID = &u
	}
	if cid, ok := GetIntArgSafe(args, "chunk_id"); ok && cid > 0 {
		u := uint64(cid)
		chunkID = &u
	}

	fb := &model.KnowledgeFeedback{
		ProductID:  productID,
		Query:      query,
		QueryHash:  hashQuery(query),
		DocumentID: documentID,
		ChunkID:    chunkID,
		Rating:     rating,
		Comment:    comment,
		Operator:   operator,
		SessionID:  sessionID,
	}
	if t.deps.FeedbackRepo == nil {
		return ErrorResult(t.Name(), fmt.Errorf("feedback repo is nil")), fmt.Errorf("feedback repo is nil")
	}
	if err := t.deps.FeedbackRepo.Create(ctx, fb); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	return SuccessResult(t.Name(), map[string]any{
		"feedback_id": fb.ID,
		"rating":      ratingStr,
		"recorded":    true,
		"message":     "反馈已记录，将用于优化召回质量",
	}), nil
}

// KnowledgeAddDocTool 添加知识文档工具
type KnowledgeAddDocTool struct {
	BaseTool
	deps KnowledgeToolDeps
}

// NewKnowledgeAddDocTool 创建添加知识文档工具
func NewKnowledgeAddDocTool(deps KnowledgeToolDeps) *KnowledgeAddDocTool {
	return &KnowledgeAddDocTool{
		BaseTool: BaseTool{
			NameVal:        "knowledge.add_doc",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryKnowledge,
			DescriptionVal: "向指定知识库添加文档（文本/URL）。添加后自动触发异步分片+向量化+入索引流水线。用于销售/客服在对话中即时沉淀知识、补充产品FAQ等场景。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"product_id":  {Type: "string", Description: "RAG 产品 ID（知识库 UUID）"},
					"title":       {Type: "string", Description: "文档标题"},
					"content":     {Type: "string", Description: "文档内容（source_type=text 时必填）"},
					"source_type": {Type: "string", Description: "来源类型：text（文本）/ url（网页抓取）", Enum: []string{"text", "url"}, Default: "text"},
					"source_ref":  {Type: "string", Description: "来源引用（source_type=url 时为 URL 地址）"},
					"category":    {Type: "string", Description: "文档分类（可选，如 FAQ/产品文档/销售话术）"},
					"tags":        {Type: "array", Items: &ToolParam{Type: "string"}, Description: "标签数组（可选）"},
					"operator":    {Type: "string", Description: "操作人（可选，AI Agent 名称或用户 ID）"},
				},
				Required: []string{"product_id", "title"},
			},
		},
		deps: deps,
	}
}

// Execute 执行添加文档
func (t *KnowledgeAddDocTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"product_id", "title"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	if t.deps.KnowledgeService == nil {
		return ErrorResult(t.Name(), errors.New("knowledge.add_doc 工具需要 KnowledgeService 依赖")), errors.New("knowledge.add_doc 工具需要 KnowledgeService 依赖")
	}

	productID := getArgString(args, "product_id")
	title := getArgString(args, "title")
	content := getArgString(args, "content")
	sourceType := getArgString(args, "source_type")
	if sourceType == "" {
		sourceType = "text"
	}
	sourceRef := getArgString(args, "source_ref")
	category := getArgString(args, "category")
	operator := getArgString(args, "operator")
	if operator == "" {
		operator = "ai_agent"
	}
	tags := getArgStringSlice(args, "tags")

	var modelSourceType model.SourceType
	switch sourceType {
	case "text":
		modelSourceType = model.SourceTypeText
		if strings.TrimSpace(content) == "" {
			return ErrorResult(t.Name(), errors.New("source_type=text 时 content 不能为空")), errors.New("source_type=text 时 content 不能为空")
		}
	case "url":
		modelSourceType = model.SourceTypeURL
		if sourceRef == "" {
			return ErrorResult(t.Name(), errors.New("source_type=url 时 source_ref（URL）不能为空")), errors.New("source_type=url 时 source_ref（URL）不能为空")
		}
		if !strings.HasPrefix(sourceRef, "http://") && !strings.HasPrefix(sourceRef, "https://") {
			return ErrorResult(t.Name(), errors.New("source_ref 必须以 http:// 或 https:// 开头")), errors.New("source_ref 必须以 http:// 或 https:// 开头")
		}
	default:
		return ErrorResult(t.Name(), fmt.Errorf("不支持的 source_type：%s（仅支持 text/url）", sourceType)), fmt.Errorf("不支持的 source_type：%s（仅支持 text/url）", sourceType)
	}

	req := &knowledgesvc.ImportRequest{
		ProductID:  productID,
		SourceType: modelSourceType,
		Title:      title,
		Content:    content,
		SourceRef:  sourceRef,
		Category:   category,
		Tags:       tags,
		Operator:   operator,
	}

	result, err := t.deps.KnowledgeService.Import(ctx, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	return SuccessResult(t.Name(), map[string]any{
		"document_id": result.DocumentID,
		"title":       result.Title,
		"status":      result.Status,
		"source_type": result.SourceType,
		"created_at":  result.CreatedAt,
		"message":     "文档已入队，异步分片+向量化+索引流水线已启动",
	}), nil
}

// KnowledgeListKBTool 列出知识库工具
type KnowledgeListKBTool struct {
	BaseTool
	deps KnowledgeToolDeps
}

// NewKnowledgeListKBTool 创建列出知识库工具
func NewKnowledgeListKBTool(deps KnowledgeToolDeps) *KnowledgeListKBTool {
	return &KnowledgeListKBTool{
		BaseTool: BaseTool{
			NameVal:        "knowledge.list_kb",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryKnowledge,
			DescriptionVal: "列出当前部署实例下所有可用的知识库（RAG 产品），含文档数、分段数、最近导入/检索时间。用于智能体选择目标知识库、运营查看知识库健康度等场景。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"include_stats": {Type: "boolean", Description: "是否包含统计字段（doc_count/chunk_count），默认 true", Default: true},
				},
			},
		},
		deps: deps,
	}
}

// Execute 执行列出知识库
func (t *KnowledgeListKBTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.deps.RagRepo == nil {
		return ErrorResult(t.Name(), errors.New("knowledge.list_kb 工具需要 RagRepo 依赖")), errors.New("knowledge.list_kb 工具需要 RagRepo 依赖")
	}

	includeStats := true
	if v, ok := args["include_stats"].(bool); ok {
		includeStats = v
	}

	products, err := t.deps.RagRepo.ListRagProducts(ctx)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	type KBItem struct {
		ID                  string     `json:"id"`
		Name                string     `json:"name"`
		Description         string     `json:"description"`
		Category            string     `json:"category"`
		EmbeddingModel      string     `json:"embedding_model"`
		LLMModel            string     `json:"llm_model"`
		TopK                int        `json:"top_k"`
		SimilarityThreshold float64    `json:"similarity_threshold"`
		DocCount            int        `json:"doc_count"`
		ChunkCount          int64      `json:"chunk_count"`
		LastImportAt        *time.Time `json:"last_import_at"`
		LastSearchAt        *time.Time `json:"last_search_at"`
		SearchCount         int64      `json:"search_count"`
		IsActive            bool       `json:"is_active"`
		CreatedAt           time.Time  `json:"created_at"`
	}

	items := make([]KBItem, 0, len(products))
	for _, p := range products {
		item := KBItem{
			ID:                  p.ID,
			Name:                p.Name,
			Description:         p.Description,
			Category:            p.Category,
			EmbeddingModel:      p.EmbeddingModel,
			LLMModel:            p.LLMModel,
			TopK:                p.TopK,
			SimilarityThreshold: p.SimilarityThreshold,
			DocCount:            p.DocCount,
			ChunkCount:          p.ChunkCount,
			LastImportAt:        p.LastImportAt,
			LastSearchAt:        p.LastSearchAt,
			SearchCount:         p.SearchCount,
			IsActive:            p.IsActive,
			CreatedAt:           p.CreatedAt,
		}
		if !includeStats {
			item.DocCount = 0
			item.ChunkCount = 0
			item.LastImportAt = nil
			item.LastSearchAt = nil
			item.SearchCount = 0
		}
		items = append(items, item)
	}

	return SuccessResult(t.Name(), map[string]any{
		"knowledge_bases": items,
		"total":           len(items),
	}), nil
}
