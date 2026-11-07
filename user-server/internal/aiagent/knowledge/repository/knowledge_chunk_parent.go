package repository

import (
	"context"
	_db "hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/aiagent/knowledge/model"

	"gorm.io/gorm"
)

// KnowledgeChunkParentRepository 父块仓储（父子分段的父侧）。
//
// 不参与发布制可见性判定：父块只在「子块已命中」之后被回拼，命中面（子块）
// 已经过 kbrelease 过滤，父块再判一次是重复口径；也不参与计数（计数面只看子块）。
type KnowledgeChunkParentRepository struct {
	db *gorm.DB
}

// NewKnowledgeChunkParentRepository 创建父块仓储
func NewKnowledgeChunkParentRepository(db *gorm.DB) *KnowledgeChunkParentRepository {
	return &KnowledgeChunkParentRepository{db: db}
}

// NewKnowledgeChunkParentRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7 刀4：KnowledgeChunkParentRepository 装配收口装配收口——句柄解析收回仓储层，service 不再经手。
func NewKnowledgeChunkParentRepositoryDefault() *KnowledgeChunkParentRepository {
	return NewKnowledgeChunkParentRepository(_db.GetDB())
}

// BatchCreate 批量写入父块，回写各自的自增 ID（子块要引用）。
func (r *KnowledgeChunkParentRepository) BatchCreate(ctx context.Context, parents []model.KnowledgeChunkParent) error {
	if len(parents) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(&parents, 100).Error
}

// DeleteByDocumentID 随文档删除/重切清理父块（与子块同一生命周期）。
func (r *KnowledgeChunkParentRepository) DeleteByDocumentID(ctx context.Context, documentID uint64) error {
	return r.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Delete(&model.KnowledgeChunkParent{}).Error
}

// GetByIDs 按 ID 批量取父块（检索回拼用；一次查询覆盖本轮所有命中）。
func (r *KnowledgeChunkParentRepository) GetByIDs(ctx context.Context, ids []uint64) ([]model.KnowledgeChunkParent, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []model.KnowledgeChunkParent
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error
	return out, err
}
