package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// ExternalKBConnectorRepo 第三方知识库连接器仓储（L3 controller 禁直连 DB）
type ExternalKBConnectorRepo struct{ db *gorm.DB }

func NewExternalKBConnectorRepo() *ExternalKBConnectorRepo {
	return &ExternalKBConnectorRepo{db: db.GetDB()}
}

func (r *ExternalKBConnectorRepo) List(ctx context.Context, limit int) ([]model.ExternalKBConnector, error) {
	var rows []model.ExternalKBConnector
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *ExternalKBConnectorRepo) GetByID(ctx context.Context, id uint) (*model.ExternalKBConnector, error) {
	var row model.ExternalKBConnector
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *ExternalKBConnectorRepo) Create(ctx context.Context, row *model.ExternalKBConnector) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *ExternalKBConnectorRepo) Update(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExternalKBConnector{}).Where("id = ?", id).Updates(updates).Error
}

func (r *ExternalKBConnectorRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ExternalKBConnector{}, id).Error
}

func (r *ExternalKBConnectorRepo) FindItem(ctx context.Context, connectorID uint, docKey string) (*model.ExternalKBSyncItem, error) {
	var row model.ExternalKBSyncItem
	err := r.db.WithContext(ctx).
		Where("connector_id = ? AND doc_key = ?", connectorID, docKey).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *ExternalKBConnectorRepo) CreateItem(ctx context.Context, item *model.ExternalKBSyncItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *ExternalKBConnectorRepo) UpdateItem(ctx context.Context, connectorID uint, docKey string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExternalKBSyncItem{}).
		Where("connector_id = ? AND doc_key = ?", connectorID, docKey).Updates(updates).Error
}

// MCPCredentialRepo MCP 凭证对仓储
type MCPCredentialRepo struct{ db *gorm.DB }

func NewMCPCredentialRepo() *MCPCredentialRepo {
	return &MCPCredentialRepo{db: db.GetDB()}
}

func (r *MCPCredentialRepo) List(ctx context.Context, limit int) ([]model.MCPCredential, error) {
	var rows []model.MCPCredential
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *MCPCredentialRepo) GetByClientID(ctx context.Context, clientID string) (*model.MCPCredential, error) {
	var row model.MCPCredential
	err := r.db.WithContext(ctx).
		Where("client_id = ? AND enabled = ?", clientID, true).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *MCPCredentialRepo) Create(ctx context.Context, row *model.MCPCredential) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *MCPCredentialRepo) Update(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.MCPCredential{}).Where("id = ?", id).Updates(updates).Error
}

func (r *MCPCredentialRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.MCPCredential{}, id).Error
}

func (r *MCPCredentialRepo) TouchLastUsed(ctx context.Context, clientID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.MCPCredential{}).
		Where("client_id = ?", clientID).Update("last_used_at", at).Error
}
