package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// PromptRepo PromptCandidate + PromptABTest 仓库
type PromptRepo struct {
	db *gorm.DB
}

// NewPromptRepo 构造
func NewPromptRepo() *PromptRepo {
	return &PromptRepo{db: _db.GetDB()}
}

// NewPromptRepoWithDB 注入 DB（测试用）
func NewPromptRepoWithDB(db *gorm.DB) *PromptRepo {
	return &PromptRepo{db: db}
}

// ListVersions 按 id / sop_id / sop_node_id 查询 prompt 版本列表
// idStr: 既作为字符串匹配 id，也作为 uint 匹配 sop_id
func (r *PromptRepo) ListVersions(ctx context.Context, idStr string, sopID uint, sopNodeID string, status string) ([]model.PromptCandidate, error) {
	q := r.db.WithContext(ctx).Model(&model.PromptCandidate{}).
		Where("id = ? OR sop_id = ? OR sop_node_id = ?", idStr, sopID, idStr)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var versions []model.PromptCandidate
	if err := q.Order("created_at DESC").Find(&versions).Error; err != nil {
		return nil, err
	}
	return versions, nil
}

// RetireActiveBySOPNode 将同一 sop_node_id 下状态为 active 的版本降为 retired
func (r *PromptRepo) RetireActiveBySOPNode(ctx context.Context, sopNodeID string) error {
	return r.db.WithContext(ctx).
		Model(&model.PromptCandidate{}).
		Where("sop_node_id = ? AND status = ?", sopNodeID, model.PromptCandidateStatusActive).
		Update("status", model.PromptCandidateStatusRetired).Error
}

// CreateCandidate 创建新版本
func (r *PromptRepo) CreateCandidate(ctx context.Context, c *model.PromptCandidate) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// ListABTests 列出所有 A/B 实验（可按 status 过滤）
func (r *PromptRepo) ListABTests(ctx context.Context, status string) ([]model.PromptABTest, error) {
	q := r.db.WithContext(ctx).Model(&model.PromptABTest{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var experiments []model.PromptABTest
	if err := q.Order("created_at DESC").Find(&experiments).Error; err != nil {
		return nil, err
	}
	return experiments, nil
}

// Create 创建 Prompt 候选（CRUD 基础方法）
func (r *PromptRepo) Create(ctx context.Context, p *model.PromptCandidate) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// GetByID 按 ID 查询 Prompt 候选
func (r *PromptRepo) GetByID(ctx context.Context, id uint) (*model.PromptCandidate, error) {
	var p model.PromptCandidate
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// List 分页查询 Prompt 候选（可按 status / sop_node_id / sop_id 过滤）
func (r *PromptRepo) List(ctx context.Context, page, pageSize int, status, sopNodeID string, sopID uint) ([]model.PromptCandidate, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.PromptCandidate{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if sopNodeID != "" {
		q = q.Where("sop_node_id = ?", sopNodeID)
	}
	if sopID > 0 {
		q = q.Where("sop_id = ?", sopID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.PromptCandidate
	offset := (page - 1) * pageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
