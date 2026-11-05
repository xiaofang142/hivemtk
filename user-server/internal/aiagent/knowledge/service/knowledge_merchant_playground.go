package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// PlaygroundRequest 检索 Playground 请求
type PlaygroundRequest struct {
	ProductID           string            `json:"product_id"`
	Query               string            `json:"query"`
	TopK                int               `json:"top_k"`
	SimilarityThreshold float64           `json:"similarity_threshold"`
	FilterCategory      string            `json:"filter_category"`
	UseThreeTier        bool              `json:"use_three_tier"`
	UseRerank           bool              `json:"use_rerank"`
	Tags                []string          `json:"tags"`
	Extra               map[string]any    `json:"extra"`
	MetadataFilters     map[string]string `json:"metadata_filters"`
}

// PlaygroundChunk 命中分段
type PlaygroundChunk struct {
	ChunkID    uint64         `json:"chunk_id"`
	DocumentID uint64         `json:"document_id"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Score      float64        `json:"score"`
	Source     string         `json:"source"`
	FromCache  bool           `json:"from_cache"`
	Metadata   map[string]any `json:"metadata"`
}

// PlaygroundResult 检索 Playground 响应
type PlaygroundResult struct {
	Query     string            `json:"query"`
	Total     int               `json:"total"`
	Chunks    []PlaygroundChunk `json:"chunks"`
	MaxScore  float64           `json:"max_score"`
	MinScore  float64           `json:"min_score"`
	AvgScore  float64           `json:"avg_score"`
	LatencyMs int64             `json:"latency_ms"`
	Source    string            `json:"source"`
	FromCache bool              `json:"from_cache"`
	DebugInfo map[string]any    `json:"debug_info"`
}

// Playground 检索 Playground 入口
func (s *KnowledgeMerchantService) Playground(ctx context.Context, req *PlaygroundRequest) (*PlaygroundResult, error) {
	if req.ProductID == "" {
		return nil, errors.New("product_id 不能为空")
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("query 不能为空")
	}
	if req.TopK <= 0 {
		req.TopK = DefaultTopK()
	}
	if req.TopK > 50 {
		req.TopK = 50
	}
	if req.SimilarityThreshold < 0 {
		req.SimilarityThreshold = 0
	}
	if req.SimilarityThreshold > 1 {
		req.SimilarityThreshold = 1
	}
	productNumericID := req.ProductID
	start := time.Now()
	chunks, err := s.ragSearch.SearchIndex(ctx, productNumericID, req.Query, req.TopK, req.MetadataFilters)
	if err != nil {
		return nil, err
	}
	filtered := chunks[:0]
	for _, c := range chunks {
		if c.Score >= req.SimilarityThreshold {
			filtered = append(filtered, c)
		}
	}
	chunks = filtered

	// 分类 / 标签过滤：category 与 tags 存在 knowledge_documents 上，命中分段只带 document_id，
	// 因此检索后按文档维度后置过滤（此前这两个参数被静默忽略，选了也不影响结果）。
	chunks, docFiltered := s.applyDocLevelFilters(ctx, chunks, req.FilterCategory, req.Tags)

	pc := make([]PlaygroundChunk, 0, len(chunks))
	var max, min, sum float64
	for i, c := range chunks {
		if i == 0 {
			max = c.Score
			min = c.Score
		} else {
			if c.Score > max {
				max = c.Score
			}
			if c.Score < min {
				min = c.Score
			}
		}
		sum += c.Score
		title := ""
		if c.Metadata != nil {
			if t, ok := c.Metadata["title"].(string); ok {
				title = t
			}
		}
		pc = append(pc, PlaygroundChunk{
			ChunkID:    c.ID,
			DocumentID: c.DocumentID,
			Title:      title,
			Content:    c.Content,
			Score:      c.Score,
			Source:     "L2",
			Metadata:   c.Metadata,
		})
	}
	avg := 0.0
	if len(chunks) > 0 {
		avg = sum / float64(len(chunks))
	}

	_ = s.recordSearchLog(ctx, productNumericID, req.Query, req.TopK, req.SimilarityThreshold, len(chunks), max, min, avg, time.Since(start).Milliseconds())

	return &PlaygroundResult{
		Query:     req.Query,
		Total:     len(chunks),
		Chunks:    pc,
		MaxScore:  max,
		MinScore:  min,
		AvgScore:  avg,
		LatencyMs: time.Since(start).Milliseconds(),
		Source:    "L2",
		DebugInfo: map[string]any{
			"product_id":     req.ProductID,
			"top_k":          req.TopK,
			"threshold":      req.SimilarityThreshold,
			"use_three_tier": req.UseThreeTier,
			"use_rerank":     req.UseRerank,
			// 三级缓存 / 重排序：Playground 走的是商户 RAG 检索（SearchIndex），
			// 没有接入 rag/retrieval 的三级缓存与 rerank 供应商，勾选只记录参数。
			// 显式回报 effective=false，前端据此提示用户，避免"勾了却像没生效"的假功能。
			"use_three_tier_effective": false,
			"use_rerank_effective":     false,
			"filter_category":          strings.TrimSpace(req.FilterCategory),
			"tags":                     normalizedTags(req.Tags),
			"doc_filtered":             docFiltered,
		},
	}, nil
}

// applyDocLevelFilters 按文档维度后置过滤命中分段（分类 / 标签）。
//   - category 非空：仅保留文档 category 与之相等（忽略大小写）的分段；
//   - tags 非空：仅保留文档 tags 中命中任一标签（忽略大小写）的分段；
//   - 文档已被删除或批量查询失败时保留该分段（检索结果不能因过滤元数据缺失而静默丢光）。
//
// 返回过滤后的分段与被过滤掉的条数（写进 DebugInfo，便于排查"结果为什么变少"）。
func (s *KnowledgeMerchantService) applyDocLevelFilters(ctx context.Context, chunks []MerchantRAGChunk, category string, tags []string) ([]MerchantRAGChunk, int) {
	category = strings.TrimSpace(category)
	tags = normalizedTags(tags)
	if (category == "" && len(tags) == 0) || len(chunks) == 0 {
		return chunks, 0
	}
	s.ensureReposFromDB()
	if s.docRepo == nil {
		return chunks, 0
	}
	seen := make(map[uint64]struct{}, len(chunks))
	ids := make([]uint64, 0, len(chunks))
	for _, c := range chunks {
		if _, ok := seen[c.DocumentID]; ok {
			continue
		}
		seen[c.DocumentID] = struct{}{}
		ids = append(ids, c.DocumentID)
	}
	docs, err := s.docRepo.GetByIDs(ctx, ids)
	if err != nil {
		logger.Warnf("[knowledge-playground] 批量读取文档分类/标签失败，已跳过分类标签过滤: %v", err)
		return chunks, 0
	}
	out := make([]MerchantRAGChunk, 0, len(chunks))
	for _, c := range chunks {
		doc := docs[c.DocumentID]
		if doc == nil {
			out = append(out, c)
			continue
		}
		if category != "" && !strings.EqualFold(strings.TrimSpace(doc.Category), category) {
			continue
		}
		if len(tags) > 0 && !docHasAnyTag(doc.Tags, tags) {
			continue
		}
		out = append(out, c)
	}
	return out, len(chunks) - len(out)
}

// normalizedTags 去空白、去空串并去重（保留原大小写，比较时忽略大小写）。
func normalizedTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

// docHasAnyTag 判断文档 tags（jsonb 存成 JSON 数组字符串）是否命中任一目标标签。
func docHasAnyTag(rawTags string, targets []string) bool {
	rawTags = strings.TrimSpace(rawTags)
	if rawTags == "" || rawTags == "[]" {
		return false
	}
	var list []string
	if err := json.Unmarshal([]byte(rawTags), &list); err != nil {
		return false
	}
	for _, tag := range list {
		for _, target := range targets {
			if strings.EqualFold(strings.TrimSpace(tag), target) {
				return true
			}
		}
	}
	return false
}

func (s *KnowledgeMerchantService) recordSearchLog(ctx context.Context, productID string, query string, topK int, threshold float64, count int, max, min, avg float64, latencyMs int64) error {
	if s.db == nil {
		return nil
	}
	s.ensureReposFromDB()
	h := sha256.Sum256([]byte(query))
	logEntry := &model.KnowledgeSearchLog{
		ProductID:           productID,
		Query:               query,
		QueryHash:           hex.EncodeToString(h[:]),
		TopK:                topK,
		SimilarityThreshold: threshold,
		ResultCount:         count,
		MaxScore:            max,
		MinScore:            min,
		AvgScore:            avg,
		LatencyMs:           int(latencyMs),
		Hit:                 boolToInt(count > 0),
		Source:              "L2",
	}
	return s.searchLogRepo.Create(ctx, logEntry)
}
