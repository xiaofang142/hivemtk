package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/aiagent/knowledge/model"
	_db "hivemtk-user/internal/pkg/db"
	"strings"
	"time"

	"gorm.io/gorm"
)

// KnowledgeFeedbackRepository 知识库反馈仓储
type KnowledgeFeedbackRepository struct {
	db *gorm.DB
}

// NewKnowledgeFeedbackRepository 创建反馈仓储
func NewKnowledgeFeedbackRepository(db *gorm.DB) *KnowledgeFeedbackRepository {
	return &KnowledgeFeedbackRepository{db: db}
}

// NewKnowledgeFeedbackRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7 刀4：service 装配收口——句柄解析收回仓储层，service 不再经手。
func NewKnowledgeFeedbackRepositoryDefault() *KnowledgeFeedbackRepository {
	return NewKnowledgeFeedbackRepository(_db.GetDB())
}

// Create 创建反馈
func (r *KnowledgeFeedbackRepository) Create(ctx context.Context, fb *model.KnowledgeFeedback) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(fb).Error
}

// FeedbackListFilter 反馈列表筛选
type FeedbackListFilter struct {
	ProductID string
	Rating    int
	HasRating bool
	// Keyword 按查询文本模糊匹配；此前前端有搜索框但请求里没带该参数，导致「搜索」按钮点了没有效果。
	Keyword  string
	Page     int
	PageSize int
}

// FeedbackStats 反馈评价分布（按 product_id 全量聚合，不是当前页）
// 兼容两套评分：Playground 的三态 1/0/-1，以及早期 e2e 数据的 2~5 星。
type FeedbackStats struct {
	Total   int64
	Good    int64
	Neutral int64
	Bad     int64
}

// List 列出反馈
func (r *KnowledgeFeedbackRepository) List(ctx context.Context, filter FeedbackListFilter) ([]model.KnowledgeFeedback, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, nil
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		// 与 pagination.MaxPageSize 一致：无上界时 page_size=100000 会把整张表读进内存。
		filter.PageSize = 100
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeFeedback{})
	if filter.ProductID != "" {
		q = q.Where("product_id = ?", filter.ProductID)
	}
	if filter.HasRating {
		q = q.Where("rating = ?", filter.Rating)
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		q = q.Where("query ILIKE ?", "%"+kw+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.KnowledgeFeedback
	offset := (filter.Page - 1) * filter.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// Stats 统计评价分布。
// 前端原先按「当前页」自己数，翻到第 2 页总数就变小；且 2~5 星的老数据既不算相关也不算不相关，
// 于是「总反馈数 7 / 相关 3 / 一般 1 / 不相关 0」对不上。这里按 product_id 全量聚合一次给前端。
func (r *KnowledgeFeedbackRepository) Stats(ctx context.Context, productID string) (FeedbackStats, error) {
	var s FeedbackStats
	if r == nil || r.db == nil {
		return s, nil
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeFeedback{})
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	err := q.Select(`COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN rating IN (1,4,5) THEN 1 ELSE 0 END),0) AS good,
		COALESCE(SUM(CASE WHEN rating IN (0,3) THEN 1 ELSE 0 END),0) AS neutral,
		COALESCE(SUM(CASE WHEN rating IN (-1,2) THEN 1 ELSE 0 END),0) AS bad`).Scan(&s).Error
	return s, err
}

// KnowledgeAPITokenRepository 知识库 API Token 仓储
type KnowledgeAPITokenRepository struct {
	db *gorm.DB
}

// NewKnowledgeAPITokenRepository 创建 Token 仓储
func NewKnowledgeAPITokenRepository(db *gorm.DB) *KnowledgeAPITokenRepository {
	return &KnowledgeAPITokenRepository{db: db}
}

// NewKnowledgeAPITokenRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7 刀4：service 装配收口——句柄解析收回仓储层，service 不再经手。
func NewKnowledgeAPITokenRepositoryDefault() *KnowledgeAPITokenRepository {
	return NewKnowledgeAPITokenRepository(_db.GetDB())
}

// Create 创建 Token
func (r *KnowledgeAPITokenRepository) Create(ctx context.Context, tok *model.KnowledgeAPIToken) error {
	if r == nil || r.db == nil {
		return errors.New("数据库未初始化")
	}
	return r.db.WithContext(ctx).Create(tok).Error
}

// ListByProduct 列出 Token（按 product_id 过滤，productID 为空时列出全部）
func (r *KnowledgeAPITokenRepository) ListByProduct(ctx context.Context, productID string) ([]model.KnowledgeAPIToken, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeAPIToken{})
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	var list []model.KnowledgeAPIToken
	if err := q.Order("created_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// DisableByID 启停 Token（enabled: 1=启用 0=禁用）
func (r *KnowledgeAPITokenRepository) DisableByID(ctx context.Context, id uint64, enabled int) error {
	if r == nil || r.db == nil {
		return nil
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.KnowledgeAPIToken{}).
		Where("id = ?", id).
		Updates(map[string]any{"enabled": enabled}))
}

// FindByToken 按 hashed token 查找
func (r *KnowledgeAPITokenRepository) FindByToken(ctx context.Context, hashed string) (*model.KnowledgeAPIToken, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("数据库未初始化")
	}
	var tok model.KnowledgeAPIToken
	if err := r.db.WithContext(ctx).Where("token = ?", hashed).First(&tok).Error; err != nil {
		return nil, err
	}
	return &tok, nil
}

// IncrementUsage 原子更新使用统计（use_count + 1, last_used_at = now）
func (r *KnowledgeAPITokenRepository) IncrementUsage(ctx context.Context, id uint64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.KnowledgeAPIToken{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"use_count":    gorm.Expr("use_count + 1"),
			"last_used_at": time.Now(),
		}).Error
}

// ExternalImportJobRepository 外部导入任务仓储
type ExternalImportJobRepository struct {
	db *gorm.DB
}

// NewExternalImportJobRepository 创建外部导入任务仓储
func NewExternalImportJobRepository(db *gorm.DB) *ExternalImportJobRepository {
	return &ExternalImportJobRepository{db: db}
}

// NewExternalImportJobRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7 刀4：service 装配收口——句柄解析收回仓储层，service 不再经手。
func NewExternalImportJobRepositoryDefault() *ExternalImportJobRepository {
	return NewExternalImportJobRepository(_db.GetDB())
}

// Create 创建任务
func (r *ExternalImportJobRepository) Create(ctx context.Context, job *model.ExternalImportJob) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(job).Error
}

// UpdateStatusByJobNo 按 job_no 更新任务状态字段
func (r *ExternalImportJobRepository) UpdateStatusByJobNo(ctx context.Context, jobNo string, updates map[string]any) error {
	if r == nil || r.db == nil {
		return nil
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.ExternalImportJob{}).
		Where("job_no = ?", jobNo).
		Updates(updates))
}

// ExternalJobListFilter 任务列表筛选
type ExternalJobListFilter struct {
	ProductID string
	Page      int
	PageSize  int
}

// List 列出任务
func (r *ExternalImportJobRepository) List(ctx context.Context, filter ExternalJobListFilter) ([]model.ExternalImportJob, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, nil
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		// 与 pagination.MaxPageSize 一致：无上界时 page_size=100000 会把整张表读进内存。
		filter.PageSize = 100
	}
	q := r.db.WithContext(ctx).Model(&model.ExternalImportJob{})
	if filter.ProductID != "" {
		q = q.Where("product_id = ?", filter.ProductID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.ExternalImportJob
	offset := (filter.Page - 1) * filter.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
