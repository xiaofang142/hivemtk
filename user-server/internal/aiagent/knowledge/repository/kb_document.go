package repository

import (
	"context"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"

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

// NewKBDocumentRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7 刀4：创建知识库文档仓储装配收口——句柄解析收回仓储层，service 不再经手。
func NewKBDocumentRepositoryDefault() *KBDocumentRepository {
	return NewKBDocumentRepository(_db.GetDB())
}

// Transaction 事务接缝（仓储同形接缝）：回调拿到事务句柄执行原子读写。
// AV-03 钉住的 service 层唯一合法 Transaction 回调（DeleteDocument 级联删除）
// 经此接缝保持原样——回调体仍在 service（业务原子性语义），句柄解析收回仓储。
func (r *KBDocumentRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
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
	if pageSize > 100 {
		// 上界与 pagination.MaxPageSize 对齐：page_size=100000 在这里唯一的作用
		// 是把整张表读进内存（调用方 service.ListDocuments 只做了下界判断）。
		pageSize = 100
	}

	var docs []*model.KBDocument
	var total int64
	q := r.db.WithContext(ctx).Model(&model.KBDocument{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// UpdateStatusFields 更新文档状态字段
func (r *KBDocumentRepository) UpdateStatusFields(ctx context.Context, id uint, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&model.KBDocument{}).
		Where("id = ?", id).
		Updates(fields).Error
}
