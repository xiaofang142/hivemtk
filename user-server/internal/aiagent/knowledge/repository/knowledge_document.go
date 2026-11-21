package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/knowledge/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const knowledgeReindexLeaseDuration = 15 * time.Minute

var ErrKnowledgeDocumentClaimLost = errors.New("knowledge document processing claim lost")

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

// NewKnowledgeDocumentRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7：service/openapi.go 装配收口——句柄解析收回仓储层。
func NewKnowledgeDocumentRepositoryDefault() *KnowledgeDocumentRepository {
	return NewKnowledgeDocumentRepository(_db.GetDB())
}

// Connected 句柄就绪探针（纯内存访问器，无 DB 操作；替代原 service 侧 s.db == nil 判断）。
func (r *KnowledgeDocumentRepository) Connected() bool { return r != nil && r.db != nil }

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

// ReindexCursor is the stable key used by a full knowledge reindex.
// CreatedAt alone is not unique, so ID is the tie-breaker for both the
// frozen boundary and subsequent pages.
type ReindexCursor struct {
	CreatedAt time.Time
	ID        uint64
	MaxID     uint64
}

// GetReindexBoundary returns the newest document key present when a reindex
// starts. Documents inserted after this point are intentionally deferred to a
// later run.
func (r *KnowledgeDocumentRepository) GetReindexBoundary(ctx context.Context, productID string) (*ReindexCursor, error) {
	var boundary ReindexCursor
	err := r.db.WithContext(ctx).
		Model(&model.KnowledgeDocument{}).
		Select("created_at, id, MAX(id) OVER () AS max_id").
		Where("product_id = ?", productID).
		Order("created_at DESC, id DESC").
		Limit(1).
		Scan(&boundary).Error
	if err != nil {
		return nil, err
	}
	if boundary.ID == 0 {
		return nil, nil
	}
	return &boundary, nil
}

// ListReindexPage lists one stable page inside a frozen boundary. The
// repository owns the cursor predicates so callers cannot accidentally fall
// back to offset pagination while documents are being updated concurrently.
func (r *KnowledgeDocumentRepository) ListReindexPage(ctx context.Context, productID string, boundary ReindexCursor, after *ReindexCursor, limit int) ([]*model.KnowledgeDocument, error) {
	if limit < 1 {
		limit = 100
	}
	q := r.db.WithContext(ctx).
		Where("product_id = ?", productID).
		Where("id <= ?", boundary.MaxID).
		Where("(created_at < ? OR (created_at = ? AND id <= ?))", boundary.CreatedAt, boundary.CreatedAt, boundary.ID)
	if after != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))", after.CreatedAt, after.CreatedAt, after.ID)
	}
	var docs []*model.KnowledgeDocument
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&docs).Error; err != nil {
		return nil, err
	}
	return docs, nil
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

// ListProductDocumentIDsSnapshot freezes the product work set in one read-only
// PostgreSQL REPEATABLE READ transaction. Rows committed after its first SELECT
// are excluded regardless of their sequence IDs. The transaction is closed
// before callers begin per-document work.
func (r *KnowledgeDocumentRepository) ListProductDocumentIDsSnapshot(ctx context.Context, productID string, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 1000
	}
	ids := make([]uint64, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var afterID uint64
		for {
			page := make([]uint64, 0, limit)
			if err := tx.Model(&model.KnowledgeDocument{}).
				Where("product_id = ? AND id > ?", productID, afterID).
				Order("id ASC").Limit(limit).Pluck("id", &page).Error; err != nil {
				return err
			}
			if len(page) == 0 {
				return nil
			}
			ids = append(ids, page...)
			afterID = page[len(page)-1]
		}
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ClaimForReindex atomically claims a document and increments its durable owner generation.
// A fresh processing lease is protected; an absent or expired lease is recoverable.
func (r *KnowledgeDocumentRepository) ClaimForReindex(ctx context.Context, productID string, id uint64) (bool, error) {
	_, claimed, err := r.ClaimForReindexWithLease(ctx, productID, id, knowledgeReindexLeaseDuration)
	return claimed, err
}

func newKnowledgeOwnerToken() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// ClaimForProcessing acquires the shared async/Reindex owner without changing the Reindex retry count.
func (r *KnowledgeDocumentRepository) ClaimForProcessing(ctx context.Context, productID string, id uint64, lease time.Duration) (string, bool, error) {
	return r.claim(ctx, productID, id, lease, false)
}

func (r *KnowledgeDocumentRepository) ClaimForReindexWithLease(ctx context.Context, productID string, id uint64, lease time.Duration) (string, bool, error) {
	return r.claim(ctx, productID, id, lease, true)
}

func (r *KnowledgeDocumentRepository) claim(ctx context.Context, productID string, id uint64, lease time.Duration, incrementRetryCount bool) (string, bool, error) {
	if lease <= 0 {
		lease = knowledgeReindexLeaseDuration
	}
	owner, err := newKnowledgeOwnerToken()
	if err != nil {
		return "", false, fmt.Errorf("生成索引 owner token 失败: %w", err)
	}
	claimed := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doc model.KnowledgeDocument
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND product_id = ?", id, productID).First(&doc).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		if doc.EmbedStatus == model.EmbedStatusProcessing && doc.ReindexLeaseUntil != nil && doc.ReindexLeaseUntil.After(now) {
			return nil
		}
		updates := map[string]any{
			"embed_status": model.EmbedStatusProcessing, "embed_progress": 0,
			"error_msg": "", "reindex_owner_token": owner,
			"reindex_lease_until": now.Add(lease), "updated_at": now,
		}
		if incrementRetryCount {
			updates["reindex_retry_count"] = gorm.Expr("reindex_retry_count + 1")
			updates["reindex_attempts"] = gorm.Expr("reindex_attempts + 1")
		}
		result := tx.Model(&model.KnowledgeDocument{}).
			Where("id = ? AND product_id = ?", id, productID).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		claimed = result.RowsAffected == 1
		return nil
	})
	if err != nil {
		return "", false, err
	}
	if !claimed {
		return "", false, nil
	}
	return owner, true, nil
}

// EnsureOwner rejects missing, superseded, terminal, or expired processing claims.
func (r *KnowledgeDocumentRepository) EnsureOwner(ctx context.Context, id uint64, owner string) error {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ? AND reindex_owner_token = ? AND embed_status = ? AND reindex_lease_until > ?", id, owner, model.EmbedStatusProcessing, time.Now()).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrKnowledgeDocumentClaimLost
	}
	return nil
}

// lockCurrentOwner serializes short owner-sensitive writes against a reclaim.
func lockCurrentOwner(ctx context.Context, tx *gorm.DB, id uint64, owner string) error {
	var doc model.KnowledgeDocument
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND reindex_owner_token = ? AND embed_status = ? AND reindex_lease_until > ?", id, owner, model.EmbedStatusProcessing, time.Now()).
		First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrKnowledgeDocumentClaimLost
	}
	return err
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

// Update 只更新管理端可编辑的元信息，避免旧对象覆盖索引任务状态与 owner。
func (r *KnowledgeDocumentRepository) Update(ctx context.Context, doc *model.KnowledgeDocument) error {
	doc.UpdatedAt = time.Now()
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", doc.ID).
		Updates(map[string]any{
			"title": doc.Title, "category": doc.Category, "tags": doc.Tags,
			"priority": doc.Priority, "updated_at": doc.UpdatedAt,
		}))
}

// UpdateStatus 更新文档状态
func (r *KnowledgeDocumentRepository) UpdateStatus(ctx context.Context, id uint64, status model.EmbedStatus, progress int, errMsg string) error {
	now := time.Now()
	updates := map[string]any{
		"embed_status":   status,
		"embed_progress": progress,
		"error_msg":      errMsg,
		"updated_at":     now,
	}
	if status != model.EmbedStatusProcessing {
		updates["reindex_lease_until"] = nil
	}
	if status == model.EmbedStatusIndexed {
		updates["last_index_at"] = now
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(updates))
}

// UpdateStatusForOwner updates progress or terminal state only for the live generation.
func (r *KnowledgeDocumentRepository) UpdateStatusForOwner(ctx context.Context, id uint64, owner string, status model.EmbedStatus, progress int, errMsg string) error {
	now := time.Now()
	updates := map[string]any{
		"embed_status": status, "embed_progress": progress, "error_msg": errMsg, "updated_at": now,
	}
	if status != model.EmbedStatusProcessing {
		updates["reindex_lease_until"] = nil
	}
	if status == model.EmbedStatusIndexed {
		updates["last_index_at"] = now
	}
	result := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ? AND reindex_owner_token = ? AND embed_status = ? AND reindex_lease_until > ?", id, owner, model.EmbedStatusProcessing, now).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrKnowledgeDocumentClaimLost
	}
	return nil
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

func (r *KnowledgeDocumentRepository) UpdateChunkStatsForOwner(ctx context.Context, id uint64, owner string, chunkCount, totalTokens int) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ? AND reindex_owner_token = ? AND embed_status = ? AND reindex_lease_until > ?", id, owner, model.EmbedStatusProcessing, now).
		Updates(map[string]any{"chunk_count": chunkCount, "total_tokens": totalTokens, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrKnowledgeDocumentClaimLost
	}
	return nil
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

func (r *KnowledgeDocumentRepository) BeginReindex(ctx context.Context, id uint64) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"reindex_attempts": gorm.Expr("reindex_attempts + 1"),
			"updated_at":       time.Now(),
		}))
}

// CompleteReindex 只写入重建结果，保留处理期间用户修改的文档元信息。
func (r *KnowledgeDocumentRepository) CompleteReindex(ctx context.Context, id uint64, chunkCount, totalTokens int) error {
	now := time.Now()
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeDocument{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"embed_status":   model.EmbedStatusIndexed,
			"embed_progress": 100,
			"error_msg":      "",
			"chunk_count":    chunkCount,
			"total_tokens":   totalTokens,
			"last_index_at":  now,
			"updated_at":     now,
		}))
}
