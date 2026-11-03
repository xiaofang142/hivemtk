package repository

import (
	"context"
	"strings"
	"errors"
	"hivemtk-user/internal/aiagent/knowledge/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

// KnowledgeDocumentRepository 知识库文档仓储(产品维度)
//
// 智能体隔离语义：
//   - AgentID (nil=不过滤, &0=仅共享, &X=该智能体)
//   - 严格隔离: agentID > 0 仅匹配 (agent_id=X OR agent_id IS NULL) AND enabled
//   - 共享 = agent_id IS NULL, 由显式白名单控制
type KnowledgeDocumentRepository struct {
	db *gorm.DB
}

// NewKnowledgeDocumentRepository 创建知识库文档仓储
func NewKnowledgeDocumentRepository(db *gorm.DB) *KnowledgeDocumentRepository {
	return &KnowledgeDocumentRepository{db: db}
}

// Create 创建文档
func (r *KnowledgeDocumentRepository) Create(ctx context.Context, doc *model.KnowledgeDocument) error {
	if doc.SourceType == "" {
		doc.SourceType = model.SourceTypeUpload
	}
	if doc.EmbedStatus == "" {
		doc.EmbedStatus = model.EmbedStatusPending
	}
	if doc.Status == 0 {
		doc.Status = 1
	}
	if doc.Tags == "" {
		doc.Tags = "[]"
	}
	if doc.Metadata == "" {
		doc.Metadata = "{}"
	}
	return r.db.WithContext(ctx).Create(doc).Error
}

// GetByID 根据 ID 获取文档
func (r *KnowledgeDocumentRepository) GetByID(ctx context.Context, id uint64) (*model.KnowledgeDocument, error) {
	var doc model.KnowledgeDocument
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("knowledge document not found")
		}
		return nil, err
	}
	return &doc, nil
}

// GetByProductAndID 根据产品 ID + 文档 ID 获取(独立部署)
//
// KnowledgeDocument.ProductID 是 string（与 RagProduct.ID 同为 UUID），前端直接传入，无需 HashStringToInt64 转换。
// productID="" 表示不按 product 过滤。
func (r *KnowledgeDocumentRepository) GetByProductAndID(ctx context.Context, productID string, id int64) (*model.KnowledgeDocument, error) {
	var doc model.KnowledgeDocument
	q := r.db.WithContext(ctx).Where("id = ?", id)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("knowledge document not found")
		}
		return nil, err
	}
	return &doc, nil
}

// GetByIDs 批量按 ID 取文档（返回 id -> 文档 映射）。
// 用于检索结果按文档维度后置过滤（分类/标签）：命中分段只带 document_id，
// 而 category/tags 在文档表上，逐条 GetByID 会产生 N 次查询。
func (r *KnowledgeDocumentRepository) GetByIDs(ctx context.Context, ids []uint64) (map[uint64]*model.KnowledgeDocument, error) {
	out := make(map[uint64]*model.KnowledgeDocument, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var docs []*model.KnowledgeDocument
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&docs).Error; err != nil {
		return nil, err
	}
	for _, d := range docs {
		out[d.ID] = d
	}
	return out, nil
}

// ListFilter 文档列表筛选
//
// KnowledgeDocument.ProductID 是 string（与 RagProduct.ID 同为 UUID），前端直接传入，无需 HashStringToInt64 转换。
// AgentID: nil=不过滤, &0=仅查共享 (agent_id IS NULL), &X=仅查该智能体 (agent_id = X)。
type ListFilter struct {
	ProductID   string
	EmbedStatus string
	SourceType  string
	Category    string
	Keyword     string
	AgentID     *uint
	Page        int
	PageSize    int
}

// List 列出文档
func (r *KnowledgeDocumentRepository) List(ctx context.Context, filter ListFilter) ([]*model.KnowledgeDocument, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{})
	if filter.ProductID != "" {
		q = q.Where("product_id = ?", filter.ProductID)
	}
	if filter.EmbedStatus != "" {
		q = q.Where("embed_status = ?", filter.EmbedStatus)
	}
	if filter.SourceType != "" {
		q = q.Where("source_type = ?", filter.SourceType)
	}
	if filter.Category != "" {
		q = q.Where("category = ?", filter.Category)
	}
	if filter.Keyword != "" {
		q = q.Where("title LIKE ?", "%"+filter.Keyword+"%")
	}
	if filter.AgentID != nil {
		if *filter.AgentID == 0 {
			q = q.Where("agent_id IS NULL")
		} else {
			q = q.Where("agent_id = ?", *filter.AgentID)
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var docs []*model.KnowledgeDocument
	offset := (filter.Page - 1) * filter.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// ListByAgent 列出某智能体的知识库文档 (强 1:1, 不含共享)
//
// 严格隔离: 仅 agent_id = ? 严格匹配, 不含共享 (agent_id IS NULL)
func (r *KnowledgeDocumentRepository) ListByAgent(ctx context.Context, agentID uint, limit int) ([]*model.KnowledgeDocument, error) {
	if agentID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}
	var docs []*model.KnowledgeDocument
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND status = ?", agentID, 1).
		Order("id DESC").
		Limit(limit).
		Find(&docs).Error
	return docs, err
}

// MatchByAgent 按智能体严格 1:1 匹配
//
// 行为:
//   - agentID == 0  -> 返回 (nil, nil)
//   - 仅匹配 status = 1 AND agent_id = ? 的文档（走 idx_knowledge_doc_agent_id 索引）
//   - 任何 agent 都必须显式绑定才能匹配
func (r *KnowledgeDocumentRepository) MatchByAgent(ctx context.Context, agentID uint, limit int) ([]*model.KnowledgeDocument, error) {
	if agentID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}
	var docs []*model.KnowledgeDocument
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND status = ?", agentID, 1).
		Order("id DESC").
		Limit(limit).
		Find(&docs).Error
	return docs, err
}

// Update 更新文档
func (r *KnowledgeDocumentRepository) Update(ctx context.Context, doc *model.KnowledgeDocument) error {
	doc.UpdatedAt = time.Now()
	return _db.SaveWholeRow(r.db.WithContext(ctx), doc)
}

// UpdateStatus 更新文档状态
func (r *KnowledgeDocumentRepository) UpdateStatus(ctx context.Context, id uint64, status model.EmbedStatus, progress int, errMsg string) error {
	updates := map[string]any{
		"embed_status":   status,
		"embed_progress": progress,
		"error_msg":      errMsg,
		"updated_at":     time.Now(),
	}
	if status == model.EmbedStatusIndexed {
		now := time.Now()
		updates["last_index_at"] = now
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(updates))
}

// UpdateChunkStats 更新分段统计
func (r *KnowledgeDocumentRepository) UpdateChunkStats(ctx context.Context, id uint64, chunkCount, totalTokens int) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"chunk_count":  chunkCount,
			"total_tokens": totalTokens,
			"updated_at":   time.Now(),
		}))
}

// Delete 删除文档（命中 0 行即不存在：已删/别租户，不能回“删除成功”）
func (r *KnowledgeDocumentRepository) Delete(ctx context.Context, id uint64) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KnowledgeDocument{}))
}

// CountByProduct 统计产品文档数
func (r *KnowledgeDocumentRepository) CountByProduct(ctx context.Context, productID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).Where("product_id = ?", productID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *KnowledgeDocumentRepository) CountByMerchant(ctx context.Context, productID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{})
	if strings.TrimSpace(productID) != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountTodayImports 今日导入数（独立部署下统计全量）
func (r *KnowledgeDocumentRepository) CountTodayImports(ctx context.Context) (int64, error) {
	var count int64
	todayStart := timeutil.BusinessToday()
	if err := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("created_at >= ?", todayStart).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CategoryStat 分类统计（repository 层结构体，供 docRepo.CategoryStats 返回）
type CategoryStat struct {
	Category string `gorm:"column:category" json:"category"`
	Count    int64  `gorm:"column:count" json:"count"`
}

// DocHit 文档命中统计（repository 层结构体，供 docRepo.TopHitDocuments 返回）
type DocHit struct {
	ID          uint64 `gorm:"column:id" json:"id"`
	Title       string `gorm:"column:title" json:"title"`
	SearchCount int64  `gorm:"column:search_count" json:"search_count"`
	HitCount    int64  `gorm:"column:hit_count" json:"hit_count"`
}

// SumTotalTokens 累计 token 总和
//
// 使用 COALESCE 避免无记录时返回 NULL，productID="" 表示不按 product 过滤。
func (r *KnowledgeDocumentRepository) SumTotalTokens(ctx context.Context, productID string) (int64, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Select("COALESCE(SUM(total_tokens), 0)")
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// CategoryStats 按 category 统计文档数量，返回前 N
//
// 过滤掉空 category（避免未分类聚合干扰 TopN），productID="" 表示不按 product 过滤。
func (r *KnowledgeDocumentRepository) CategoryStats(ctx context.Context, productID string, limit int) ([]CategoryStat, error) {
	if limit <= 0 {
		limit = 20
	}
	var results []CategoryStat
	q := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Select("category, COUNT(*) as count").
		Where("category IS NOT NULL AND category != ''").
		Group("category").
		Order("count DESC").
		Limit(limit)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

// TopHitDocuments 命中次数最多的文档（Top N）
//
// productID="" 表示不按 product 过滤。
func (r *KnowledgeDocumentRepository) TopHitDocuments(ctx context.Context, productID string, limit int) ([]DocHit, error) {
	if limit <= 0 {
		limit = 10
	}
	var results []DocHit
	q := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Select("id, title, search_count, hit_count").
		Order("hit_count DESC, id DESC").
		Limit(limit)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}
