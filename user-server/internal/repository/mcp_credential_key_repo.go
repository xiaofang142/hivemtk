package repository

import (
	"context"

	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// mcp_credential_key_repo.go MCP 凭证钥匙（1:N）与管理审计仓储（L4）。
//
// 2026-10-11 新增：Client ID 与 API Key 解耦后，钥匙的增删改查/状态流转在这里；
// MCPCredentialAuditLog 是管理面专用账本（reveal 是 GET，全局 AuditMiddleware 拦不到）。

// MCPCredentialKeyRepo 钥匙仓储。
type MCPCredentialKeyRepo struct{ db *gorm.DB }

func NewMCPCredentialKeyRepo() *MCPCredentialKeyRepo {
	return &MCPCredentialKeyRepo{db: db.GetDB()}
}

// Available 分诊纪律同 MCPCredentialRepo：句柄缺失（503）与查询报错（500）分开。
func (r *MCPCredentialKeyRepo) Available() bool { return r != nil && r.db != nil }

func (r *MCPCredentialKeyRepo) Create(ctx context.Context, row *model.MCPCredentialKey) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// CountByCredential 统计凭证名下钥匙数（签发上限用）。
func (r *MCPCredentialKeyRepo) CountByCredential(ctx context.Context, credentialID uint) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.MCPCredentialKey{}).
		Where("credential_id = ?", credentialID).Count(&cnt).Error
	return cnt, err
}

func (r *MCPCredentialKeyRepo) ListByCredential(ctx context.Context, credentialID uint) ([]model.MCPCredentialKey, error) {
	var rows []model.MCPCredentialKey
	err := r.db.WithContext(ctx).
		Where("credential_id = ?", credentialID).Order("id ASC").Find(&rows).Error
	return rows, err
}

// GetByID 取钥匙行（不校验归属——归属校验在 service/controller 判 credentialID）。
func (r *MCPCredentialKeyRepo) GetByID(ctx context.Context, id uint) (*model.MCPCredentialKey, error) {
	var row model.MCPCredentialKey
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetByHash 按 sha256 hex 查钥匙（guard 校验路径的主查找）。
func (r *MCPCredentialKeyRepo) GetByHash(ctx context.Context, keyHash string) (*model.MCPCredentialKey, error) {
	var row model.MCPCredentialKey
	if err := r.db.WithContext(ctx).Where("key_hash = ?", keyHash).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *MCPCredentialKeyRepo) Update(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.MCPCredentialKey{}).Where("id = ?", id).Updates(updates).Error
}

func (r *MCPCredentialKeyRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.MCPCredentialKey{}, id).Error
}

// DeleteByCredential 凭证删除时的级联清理。
func (r *MCPCredentialKeyRepo) DeleteByCredential(ctx context.Context, credentialID uint) error {
	return r.db.WithContext(ctx).
		Where("credential_id = ?", credentialID).
		Delete(&model.MCPCredentialKey{}).Error
}

// TouchLastUsed 回写钥匙最近使用（guard 节流后调用）。
func (r *MCPCredentialKeyRepo) TouchLastUsed(ctx context.Context, id uint, at any) error {
	return r.db.WithContext(ctx).Model(&model.MCPCredentialKey{}).
		Where("id = ?", id).Update("last_used_at", at).Error
}

// MCPCredentialAuditLogRepo 管理面操作审计仓储。
type MCPCredentialAuditLogRepo struct{ db *gorm.DB }

func NewMCPCredentialAuditLogRepo() *MCPCredentialAuditLogRepo {
	return &MCPCredentialAuditLogRepo{db: db.GetDB()}
}

func (r *MCPCredentialAuditLogRepo) Available() bool { return r != nil && r.db != nil }

func (r *MCPCredentialAuditLogRepo) Create(ctx context.Context, row *model.MCPCredentialAuditLog) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(row).Error
}

// List 分页查询。action/targetType/credentialID 均可空（不限定）。
func (r *MCPCredentialAuditLogRepo) List(ctx context.Context, action, targetType string, credentialID *uint, page, size int) ([]model.MCPCredentialAuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.db.WithContext(ctx).Model(&model.MCPCredentialAuditLog{})
	if action != "" {
		q = q.Where("action = ?", action)
	}
	if targetType != "" {
		q = q.Where("target_type = ?", targetType)
	}
	if credentialID != nil && *credentialID > 0 {
		q = q.Where("credential_id = ?", *credentialID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.MCPCredentialAuditLog
	err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
