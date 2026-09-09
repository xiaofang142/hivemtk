// help_center_repo.go 帮助中心聚合查询仓储（五层 L5）
package repository

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// HCArticleRowRepo 文章列表行（repo 层）
type HCArticleRowRepo struct {
	ID        uint64    `gorm:"column:id"`
	Title     string    `gorm:"column:title"`
	Category  string    `gorm:"column:category"`
	Summary   string    `gorm:"column:summary"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// HCCategoryRow 分类聚合行
type HCCategoryRow struct {
	Category string `gorm:"column:category"`
	Cnt      int64  `gorm:"column:cnt"`
}

// HCTopArticleRow 热门文章行
type HCTopArticleRow struct {
	ID    uint64 `gorm:"column:id"`
	Title string `gorm:"column:title"`
	Views int64  `gorm:"column:views"`
}

// HelpCenterTestRecordRow 检索测试记录行（写入）；直接复用 model.HelpCenterTestRecord
type HelpCenterTestRecordRow = model.HelpCenterTestRecord

// HelpCenterRepository 帮助中心查询/记录仓储
type HelpCenterRepository struct {
	db *gorm.DB
}

// NewHelpCenterRepository 构造
func NewHelpCenterRepository(db *gorm.DB) *HelpCenterRepository {
	return &HelpCenterRepository{db: db}
}

// CategoryStats 公开可见文档分类聚合（数量降序）
func (r *HelpCenterRepository) CategoryStats(ctx context.Context) ([]HCCategoryRow, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []HCCategoryRow
	err := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Select("COALESCE(NULLIF(category,''),'未分类') AS category, COUNT(*) AS cnt").
		Where("(public_visible = ? OR hc_status = ?)", true, "published").
		Group("category").Order("cnt DESC").
		Scan(&rows).Error
	return rows, err
}

// TopArticlesByViews 按访问量排序的已发布文章
func (r *HelpCenterRepository) TopArticlesByViews(ctx context.Context, limit int) ([]HCTopArticleRow, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	out := []HCTopArticleRow{}
	err := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Select("id, title, hc_views AS views").
		Where("hc_status = ? AND deleted_at IS NULL", "published").
		Order("hc_views DESC").Limit(limit).
		Scan(&out).Error
	return out, err
}

// IncArticleViews 公开详情访问计数（原子自增）
func (r *HelpCenterRepository) IncArticleViews(ctx context.Context, id uint64) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).
		Exec("UPDATE knowledge_documents SET hc_views = hc_views + 1 WHERE id = ?", id).Error
}

// ListPublicArticles 公开文章列表（分类过滤+标题/切片搜索），返回行+每文档首切片摘要
func (r *HelpCenterRepository) ListPublicArticles(ctx context.Context, category, q string, limit int) ([]HCArticleRowRepo, map[uint64]string, error) {
	if r == nil || r.db == nil {
		return nil, nil, gorm.ErrInvalidDB
	}
	qry := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Select("id, title, COALESCE(NULLIF(category,''),'未分类') AS category, updated_at").
		Where("(public_visible = ? OR hc_status = ?)", true, "published")
	if category != "" && category != "未分类" {
		qry = qry.Where("category = ?", category)
	} else if category == "未分类" {
		qry = qry.Where("category = ''")
	}
	if q != "" {
		like := "%" + q + "%"
		qry = qry.Where("title ILIKE ? OR id IN (SELECT document_id FROM knowledge_chunks WHERE content ILIKE ?)", like, like)
	}
	rows := []HCArticleRowRepo{}
	if err := qry.Order("updated_at DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}

	summaries := map[uint64]string{}
	if len(rows) > 0 {
		ids := make([]uint64, 0, len(rows))
		for _, a := range rows {
			ids = append(ids, a.ID)
		}
		type ck struct {
			DocumentID uint64 `gorm:"column:document_id"`
			Content    string `gorm:"column:content"`
		}
		var cks []ck
		if err := r.db.WithContext(ctx).
			Table("knowledge_chunks").
			Select("document_id, content").
			Where("document_id IN ?", ids).
			Order("document_id ASC, chunk_index ASC").Find(&cks).Error; err == nil {
			seen := map[uint64]bool{}
			for _, c := range cks {
				if seen[c.DocumentID] {
					continue
				}
				seen[c.DocumentID] = true
				summaries[c.DocumentID] = c.Content
			}
		}
	}
	return rows, summaries, nil
}

// GetPublicArticle 公开文章详情（含全文拼接）
func (r *HelpCenterRepository) GetPublicArticle(ctx context.Context, id uint64) (map[string]any, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var doc struct {
		ID        uint64
		Title     string
		Category  string
		UpdatedAt time.Time
	}
	err := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Select("id, title, COALESCE(NULLIF(category,''),'未分类') AS category, updated_at").
		Where("id = ? AND (public_visible = ? OR hc_status = ?)", id, true, "published").
		Scan(&doc).Error
	if err != nil {
		return nil, err
	}
	if doc.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var cks []struct {
		Content string
	}
	if err := r.db.WithContext(ctx).
		Table("knowledge_chunks").
		Select("content").
		Where("document_id = ?", id).
		Order("chunk_index ASC").Limit(100).
		Scan(&cks).Error; err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, c := range cks {
		sb.WriteString(c.Content)
		sb.WriteString("\n\n")
	}
	return map[string]any{
		"id": doc.ID, "title": doc.Title, "category": doc.Category,
		"updated_at": doc.UpdatedAt, "content": sb.String(),
	}, nil
}

// SetArticleVisibility 管理端切换发布状态
func (r *HelpCenterRepository) SetArticleVisibility(ctx context.Context, docID uint64, visible bool) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	res := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Where("id = ?", docID).
		Update("public_visible", visible)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetArticleStatus 状态机切换（draft/published/archived，双向同步 public_visible）
func (r *HelpCenterRepository) SetArticleStatus(ctx context.Context, docID uint64, status string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	res := r.db.WithContext(ctx).
		Table("knowledge_documents").
		Where("id = ?", docID).
		Updates(map[string]any{"hc_status": status, "public_visible": status == "published"})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateTestRecord 落一条检索测试记录
func (r *HelpCenterRepository) CreateTestRecord(ctx context.Context, rec *HelpCenterTestRecordRow) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(rec).Error
}
