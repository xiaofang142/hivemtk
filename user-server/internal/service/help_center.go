package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ksvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"time"

	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// HelpCenterService 帮助中心服务
type HelpCenterService struct {
	repo *repository.HelpCenterRepository
}

// NewHelpCenterService 构造
func NewHelpCenterService(gdb *gorm.DB) *HelpCenterService {
	return &HelpCenterService{repo: repository.NewHelpCenterRepository(gdb)}
}

// NewHelpCenterServiceFromGlobal 便捷构造（使用全局 DB）
func NewHelpCenterServiceFromGlobal() *HelpCenterService { return NewHelpCenterService(db.GetDB()) }

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
	return s.repo.GetPublicArticle(ctx, id)
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
