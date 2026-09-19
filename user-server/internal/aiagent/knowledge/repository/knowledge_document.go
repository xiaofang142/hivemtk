package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

// KnowledgeDocumentRepository 知识库文档仓储(产品维度)
//
// 按智能体隔离改造
//   - 新增 ListByAgent / ListShared / ListByKB / MatchByAgent
//   - ListWithFilter 新增 AgentID 字段 (nil=不过滤, &0=仅共享, &X=该智能体)
//   - 严格隔离语义: agentID > 0 仅匹配 (agent_id=X OR agent_id IS NULL) AND enabled
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

// ListFilter 文档列表筛选
//
// KnowledgeDocument.ProductID 是 string（与 RagProduct.ID 同为 UUID），前端直接传入，无需 HashStringToInt64 转换。
//
// AgentID 字段
//   - nil:  不过滤 (兼容旧调用)
//   - &0:   仅查共享 (agent_id IS NULL)
//   - &X:   仅查该智能体 (agent_id = X)
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
// 严格隔离语义, 仅 agent_id = ? 严格匹配, 不含共享 (agent_id IS NULL)
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

// ListShared 列出全部共享知识库文档 (agent_id IS NULL)
func (r *KnowledgeDocumentRepository) ListShared(ctx context.Context, limit int) ([]*model.KnowledgeDocument, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var docs []*model.KnowledgeDocument
	err := r.db.WithContext(ctx).
		Where("agent_id IS NULL AND status = ?", 1).
		Order("id DESC").
		Limit(limit).
		Find(&docs).Error
	return docs, err
}

// ListByKB 按知识库 ID 列出 (: 查某 KB 下挂载的知识库文档)
//
// 简化实现: 直接按 agent_id 过滤 (KBType=rag 假设)
// 完整实现需 JOIN agent_kb_bindings + knowledge_bases, 此处保留简化
func (r *KnowledgeDocumentRepository) ListByKB(ctx context.Context, kbID uint, agentID uint, limit int) ([]*model.KnowledgeDocument, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := r.db.WithContext(ctx).Where("status = ?", 1)
	if agentID > 0 {
		q = q.Where("agent_id = ?", agentID)
	}
	var docs []*model.KnowledgeDocument
	if err := q.Order("id DESC").Limit(limit).Find(&docs).Error; err != nil {
		return nil, err
	}
	return docs, nil
}

// MatchByAgent 按智能体严格 1:1 匹配 (: 强 1对1 改造)
//
// 行为:
//   - agentID == 0  -> 返回 (nil, nil)
//   - 仅匹配 status = 1 AND agent_id = ? 的文档
//   - 移除"空数组=全局"分支: 任何 agent 都必须显式绑定才能匹配
//
// SQL: WHERE status = 1 AND agent_id = ?  (走 idx_knowledge_doc_agent_id 索引)
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
	return r.db.WithContext(ctx).Save(doc).Error
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
	return r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// UpdateChunkStats 更新分段统计
func (r *KnowledgeDocumentRepository) UpdateChunkStats(ctx context.Context, id uint64, chunkCount, totalTokens int) error {
	return r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"chunk_count":  chunkCount,
			"total_tokens": totalTokens,
			"updated_at":   time.Now(),
		}).Error
}

// Delete 删除文档
func (r *KnowledgeDocumentRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KnowledgeDocument{}).Error
}

// DeleteByProduct 根据产品删除所有文档
func (r *KnowledgeDocumentRepository) DeleteByProduct(ctx context.Context, productID string) error {
	return r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).Where("product_id = ?", productID).
		Delete(&model.KnowledgeDocument{}).Error
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

func (r *KnowledgeDocumentRepository) CountByMerchant(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).Count(&count).Error; err != nil {
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
// 使用 COALESCE 避免无记录时返回 NULL，productID=0 表示不按 product 过滤。
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
// 过滤掉空 category（避免未分类聚合干扰 TopN），productID=0 表示不按 product 过滤。
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
// productID=0 表示不按 product 过滤。
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
