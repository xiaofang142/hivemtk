// qa_candidate.go QA 候选对仓储（AV-04c 批次7 刀3 自 service/kb_qa_split.go 下沉）。
//
// qa_candidates 的落库/列表/审核回写 SQL 逐字保留；状态机
// （QACandidateTransitionAllowed）与 LLM 拆分逻辑留在 service 层。
package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// QACandidateRepository QA 候选对仓储
type QACandidateRepository struct {
	db *gorm.DB
}

// NewQACandidateRepository 创建 QA 候选仓储
func NewQACandidateRepository(db *gorm.DB) *QACandidateRepository {
	return &QACandidateRepository{db: db}
}

// NewQACandidateRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
func NewQACandidateRepositoryDefault() *QACandidateRepository {
	return NewQACandidateRepository(_db.GetDB())
}

// Connected 句柄就绪探针（纯内存访问器，无 DB 操作）。
func (r *QACandidateRepository) Connected() bool { return r != nil && r.db != nil }

// CreateBatch 批量落候选行（显式写 created_at/updated_at，见 service 侧注释）。
func (r *QACandidateRepository) CreateBatch(ctx context.Context, rows *[]*model.QACandidate) error {
	return r.db.WithContext(ctx).Create(rows).Error
}

// List 候选列表（source_type/status 过滤，T1 前端复用同一接口语义；
// Count 与 Find 沿原实现同链复用）。
func (r *QACandidateRepository) List(ctx context.Context, sourceType, status string, page, pageSize int) ([]model.QACandidate, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.QACandidate{})
	if sourceType != "" {
		q = q.Where("source_type = ?", sourceType)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.QACandidate
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}

// GetByID 按主键取候选（not-found 返回 gorm.ErrRecordNotFound，controller 依赖
// errhttp.IsNotFoundError 映射 404，语义由调用方保持）。
func (r *QACandidateRepository) GetByID(ctx context.Context, id uint64) (*model.QACandidate, error) {
	var c model.QACandidate
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateReview 裁决回写（status + reviewer）。
func (r *QACandidateRepository) UpdateReview(ctx context.Context, id uint64, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.QACandidate{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateStatus 单字段状态回写（publish 链路）。
func (r *QACandidateRepository) UpdateStatus(ctx context.Context, id uint64, status string) error {
	return r.db.WithContext(ctx).Model(&model.QACandidate{}).Where("id = ?", id).Update("status", status).Error
}
