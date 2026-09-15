package repository

import (
	"context"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// RagHealthRepository RAG 健康度仓储接口
type RagHealthRepository interface {
	CountKnowledgeChunks(ctx context.Context) (int64, error)
}

type ragHealthRepo struct {
	db *gorm.DB
}

// NewRagHealthRepository 创建 RAG 健康度仓储
//
// ARC-01：db 为 nil 时返回 nil 接口，service 侧以 repo == nil 作为未初始化守卫。
func NewRagHealthRepository(db *gorm.DB) RagHealthRepository {
	if db == nil {
		return nil
	}
	return &ragHealthRepo{db: db}
}

func (r *ragHealthRepo) CountKnowledgeChunks(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&model.KnowledgeChunk{}).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

var _ RagHealthRepository = (*ragHealthRepo)(nil)
