// data_export_repo.go GDPR DSAR 导出数据汇聚仓储（五层 L5）
package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// DataExportRepository DSAR 导出查询收口
type DataExportRepository struct {
	db *gorm.DB
}

// NewDataExportRepository 构造
func NewDataExportRepository(db *gorm.DB) *DataExportRepository {
	return &DataExportRepository{db: db}
}

// GetCustomer 按主键取客户
func (r *DataExportRepository) GetCustomer(ctx context.Context, customerID string) (*model.Customer, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var customer model.Customer
	if err := r.db.WithContext(ctx).Where("id = ?", customerID).First(&customer).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

// ListSessionsByOneID 按 one_id 升序取全部会话
func (r *DataExportRepository) ListSessionsByOneID(ctx context.Context, oneID string) ([]*model.CustomerSession, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("one_id = ?", oneID).
		Order("created_at ASC").
		Find(&sessions).Error
	return sessions, err
}

// ListMessagesBySessionIDs 按 session_id 集合升序取全部消息
func (r *DataExportRepository) ListMessagesBySessionIDs(ctx context.Context, sessionIDs []string) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var messages []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id IN ?", sessionIDs).
		Order("created_at ASC").
		Find(&messages).Error
	return messages, err
}

// ListAllTags 全量标签定义（名称升序）
func (r *DataExportRepository) ListAllTags(ctx context.Context) ([]*model.CustomerTag, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var tags []*model.CustomerTag
	err := r.db.WithContext(ctx).
		Order("name ASC").
		Find(&tags).Error
	return tags, err
}

// ListMemoriesByCustomer 按客户取记忆条目（创建时间升序）
func (r *DataExportRepository) ListMemoriesByCustomer(ctx context.Context, customerID string) ([]*model.MemoryItem, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var memories []*model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("customer_id = ?", customerID).
		Order("created_at ASC").
		Find(&memories).Error
	return memories, err
}
