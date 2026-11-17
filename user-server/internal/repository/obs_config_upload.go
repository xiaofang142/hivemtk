package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ObsUploadConfigRepository 是签发入口的受控读取，不改变管理端默认配置接口。
type ObsUploadConfigRepository struct {
	database *gorm.DB
}

func NewObsUploadConfigRepository() *ObsUploadConfigRepository {
	return NewObsUploadConfigRepositoryWithDB(db.GetDB())
}

func NewObsUploadConfigRepositoryWithDB(database *gorm.DB) *ObsUploadConfigRepository {
	return &ObsUploadConfigRepository{database: database}
}

func (r *ObsUploadConfigRepository) GetDefaultForUpload(ctx context.Context) (*model.ObsConfig, error) {
	if r == nil || r.database == nil {
		return nil, gorm.ErrInvalidDB
	}
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, errors.New("upload configuration requires a tenant context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("upload configuration requires a confirmed tenant")
	}
	var cfg model.ObsConfig
	err := r.database.WithContext(tenant.OnlyTenant(ctx, tenantID)).
		Where("tenant_id = ? AND is_default = ? AND status = ?", tenantID, true, model.ObsStatusActive).
		Order("created_at ASC").First(&cfg).Error
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg.AccessKey = dbencrypt.Decrypt(cfg.AccessKey)
	cfg.SecretKey = dbencrypt.Decrypt(cfg.SecretKey)
	return &cfg, nil
}
