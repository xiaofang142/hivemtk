package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

// ImportCredentialsRepository 在知识域内存取租户导入配置，不向service暴露数据库。
type ImportCredentialsRepository struct {
	database *gorm.DB
}

func NewImportCredentialsRepository() *ImportCredentialsRepository {
	return NewImportCredentialsRepositoryWithDB(db.GetDB())
}

func NewImportCredentialsRepositoryWithDB(database *gorm.DB) *ImportCredentialsRepository {
	return &ImportCredentialsRepository{database: database}
}

func (r *ImportCredentialsRepository) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	if r == nil || r.database == nil {
		return nil, 0, gorm.ErrInvalidDB
	}
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, 0, errors.New("knowledge import credentials require a tenant context")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	id := tenant.WriteTenantID(ctx)
	if id == 0 {
		return nil, 0, errors.New("knowledge import credentials require a confirmed tenant")
	}
	return r.database.WithContext(tenant.OnlyTenant(ctx, id)), id, nil
}

func (r *ImportCredentialsRepository) Get(ctx context.Context, key string) (string, error) {
	database, id, err := r.scoped(ctx)
	if err != nil {
		return "", err
	}
	var row model.SystemConfigKV
	err = database.Where("tenant_id = ? AND key = ?", id, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

func (r *ImportCredentialsRepository) Upsert(ctx context.Context, key, value string) error {
	database, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	row := model.SystemConfigKV{TenantID: id, Key: key, Value: value, CreatedAt: now, UpdatedAt: now}
	write := func() error {
		return database.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "key"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"value": value, "updated_at": now,
			}),
		}).Create(&row).Error
	}
	if err := write(); err != nil {
		// 保留旧库配置表缺失时的幂等建表兼容，不移除已有租户限定。
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ensureErr := database.AutoMigrate(&model.SystemConfigKV{}); ensureErr != nil {
			return err
		}
		return write()
	}
	return nil
}
