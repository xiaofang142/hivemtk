package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	ksvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"time"

	"hivemtk-user/internal/repository"
)

// HelpCenterService 帮助中心服务
type HelpCenterService struct {
	repo *repository.HelpCenterRepository
}

// NewHelpCenterService 构造（无参仓储工厂，对齐 message_hub 先例）
func NewHelpCenterService() *HelpCenterService {
	return &HelpCenterService{repo: repository.NewHelpCenterRepository()}
}

// NewHelpCenterServiceFromGlobal 历史别名（装配语义已并入无参构造）
func NewHelpCenterServiceFromGlobal() *HelpCenterService { return NewHelpCenterService() }

// HCArticleRow 文章列表行
type HCArticleRow struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Summary   string    `json:"summary"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Categories 分类聚合
func (s *HelpCenterService) Categories(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.repo.CategoryStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"category": r.Category, "count": r.Cnt})
	}
	return out, nil
}

// Articles 文章列表（分类过滤+关键词搜索）
func (s *HelpCenterService) Articles(ctx context.Context, category, q string, limit int) ([]*HCArticleRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, summaries, err := s.repo.ListPublicArticles(ctx, category, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*HCArticleRow, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		row := &HCArticleRow{ID: r.ID, Title: r.Title, Category: r.Category, UpdatedAt: r.UpdatedAt}
		if raw, ok := summaries[r.ID]; ok {
			summary := strings.TrimSpace(raw)
			rr := []rune(summary)
			if len(rr) > 180 {
				summary = string(rr[:180]) + "…"
			}
			row.Summary = summary
		}
		out = append(out, row)
	}
	return out, nil
}

// ArticleDetail 文章详情（正文=chunks 拼接）
func (s *HelpCenterService) ArticleDetail(ctx context.Context, id uint64) (map[string]any, error) {
	detail, err := s.repo.GetPublicArticle(ctx, id)
	if err != nil {
		return nil, err
	}
	// 正文来自知识库 chunk 原文。行业演示种子（scripts/seed/bulk_seed_industries.py）
	// 里的话术是模板，含 {hours}/{puffs} 这类占位符，直接输出会把模板语法露给终端用户。
	// 这里做一次展示层兜底：已知占位符填演示值，未知占位符去掉花括号只留词干。
	if body, ok := detail["content"].(string); ok {
		detail["content"] = sanitizeDemoPlaceholders(body)
	}
	return detail, nil
}

// demoPlaceholderValues 是行业演示内容的占位符取值表（与种子脚本的话术口径一致）。
// 新增占位符时两边一起加：这里负责存量数据，种子脚本负责新写入的数据。
var demoPlaceholderValues = map[string]string{
	"hours":          "2",
	"seconds":        "3",
	"days":           "3",
	"puffs":          "500",
	"temp_range":     "10–25℃",
	"hundreds":       "数百",
	"tens":           "数十",
	"capacity":       "650",
	"power":          "8",
	"ohm":            "1.2",
	"ml":             "2",
	"mg":             "20",
	"mg2":            "5",
	"months":         "24",
	"interval":       "30",
	"fee":            "10",
	"fee2":           "15",
	"type":           "5",
	"hypoallergenic": "低敏",
}

var demoPlaceholderRe = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]{0,31})\}`)

// sanitizeDemoPlaceholders 把 {token} 形式的模板占位符变成可读文本。
func sanitizeDemoPlaceholders(text string) string {
	if !strings.Contains(text, "{") {
		return text
	}
	return demoPlaceholderRe.ReplaceAllStringFunc(text, func(m string) string {
		key := m[1 : len(m)-1]
		if v, ok := demoPlaceholderValues[key]; ok {
			return v
		}
		// 未知占位符：去掉花括号，保留词干，避免露出模板语法
		return key
	})
}

// SetArticleVisibility 管理端切换发布状态
func (s *HelpCenterService) SetArticleVisibility(ctx context.Context, docID uint64, visible bool) error {
	return s.repo.SetArticleVisibility(ctx, docID, visible)
}

// SetArticleStatus 状态机切换（draft/published/archived，双向同步 public_visible）
func (s *HelpCenterService) SetArticleStatus(ctx context.Context, docID uint64, status string) error {
	if status != "draft" && status != "published" && status != "archived" {
		return fmt.Errorf("非法状态: %s（仅 draft/published/archived）", status)
	}
	return s.repo.SetArticleStatus(ctx, docID, status)
}

// IncArticleViews 公开详情访问计数（原子自增）
func (s *HelpCenterService) IncArticleViews(ctx context.Context, id uint64) {
	_ = s.repo.IncArticleViews(ctx, id)
}

// TopArticles 按访问量排序（效果统计）
func (s *HelpCenterService) TopArticles(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.repo.TopArticlesByViews(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"id": r.ID, "title": r.Title, "views": r.Views})
	}
	return out, nil
}

func (s *HelpCenterService) Search(ctx context.Context, keyword string, limit int) ([]*HCArticleRow, error) {
	return s.Articles(ctx, "", keyword, limit)
}

// RetrievalTest 检索测试（Dify Retrieval Testing 对标）+ 记录落库
func (s *HelpCenterService) RetrievalTest(ctx context.Context, productID, query string, topK int) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query 必填")
	}
	if topK <= 0 || topK > 20 {
		topK = 5
	}
	searcher := ksvc.NewRagSearcher()
	chunks, err := searcher.Search(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		results = append(results, map[string]any{
			"chunk_id": ch.ChunkID, "content": ch.Content, "score": ch.Score,
		})
	}
	raw, _ := json.Marshal(results)
	rec := &model.HelpCenterTestRecord{
		ProductID: productID, Query: query, TopK: topK, Hits: len(results), Results: string(raw),
	}
	_ = s.repo.CreateTestRecord(ctx, rec)
	return map[string]any{
		"query": query, "top_k": topK, "hits": len(results), "results": results,
		"record_id": rec.ID,
	}, nil
}
