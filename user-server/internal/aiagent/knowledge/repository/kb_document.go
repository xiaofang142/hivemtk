package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/repository/scope"

	"gorm.io/gorm"
)

// KBDocumentRepository 知识库文档仓储(工作区维度,对应 KBDocument 表)
type KBDocumentRepository struct {
	db *gorm.DB
}

// NewKBDocumentRepository 创建知识库文档仓储
func NewKBDocumentRepository(db *gorm.DB) *KBDocumentRepository {
	return &KBDocumentRepository{db: db}
}

// Create 创建文档
func (r *KBDocumentRepository) Create(ctx context.Context, doc *model.KBDocument) error {
	return r.db.WithContext(ctx).Create(doc).Error
}

// GetByID 根据 ID 获取文档
func (r *KBDocumentRepository) GetByID(ctx context.Context, id uint) (*model.KBDocument, error) {
	var doc model.KBDocument
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

// List 列出文档(分页)
func (r *KBDocumentRepository) List(ctx context.Context, page, pageSize int) ([]*model.KBDocument, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var docs []*model.KBDocument
	var total int64
	q := r.db.WithContext(ctx).Model(&model.KBDocument{}).Scopes(scope.TeamScoped(ctx))
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// DeleteByID 按 ID 删除文档
func (r *KBDocumentRepository) DeleteByID(ctx context.Context, id uint) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&model.KBDocument{}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KBDocument{}).Error
}

// UpdateStatusFields 更新文档状态字段
func (r *KBDocumentRepository) UpdateStatusFields(ctx context.Context, id uint, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&model.KBDocument{}).Scopes(scope.TeamScoped(ctx)).
		Where("id = ?", id).
		Updates(fields).Error
}
