package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PilotContractKey 复用租户 KV 模型，不创建另一套店铺或能力表。
const PilotContractKey = "pilot.contract"

var ErrPilotContractConflict = errors.New("pilot contract revision conflict")

type PilotContractRepository interface {
	Load(ctx context.Context) (string, error)
	Save(ctx context.Context, previous, updated string) error
}

type pilotContractRepo struct {
	db *gorm.DB
	kv SystemConfigKVRepository
}

func NewPilotContractRepository(database *gorm.DB) PilotContractRepository {
	return &pilotContractRepo{db: database, kv: NewSystemConfigKVRepositoryWithDB(database)}
}

// pilotContractScope 要求显式租户，包含离线模式；不继承 SystemScope 豁免。
func pilotContractScope(ctx context.Context) (context.Context, uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, 0, ErrKVTenantContextMissing
	}
	return tenant.OnlyTenant(ctx, id), id, nil
}

func (r *pilotContractRepo) Load(ctx context.Context) (string, error) {
	scoped, _, err := pilotContractScope(ctx)
	if err != nil {
		return "", err
	}
	if r == nil || r.db == nil {
		return "", ErrKVStoreUnavailable
	}
	return r.kv.Get(scoped, PilotContractKey)
}

// Save 单条条件写，避免两个管理员的同一修订互相覆盖；不自动创建/修复表。
func (r *pilotContractRepo) Save(ctx context.Context, previous, updated string) error {
	scoped, id, err := pilotContractScope(ctx)
	if err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return ErrKVStoreUnavailable
	}
	var result *gorm.DB
	if previous == "" {
		row := model.SystemConfigKV{TenantID: id, Key: PilotContractKey, Value: updated, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		result = r.db.WithContext(scoped).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	} else {
		result = r.db.WithContext(scoped).Model(&model.SystemConfigKV{}).
			Where("tenant_id = ? AND key = ? AND value = ?", id, PilotContractKey, previous).
			Updates(map[string]any{"value": updated, "updated_at": time.Now()})
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrPilotContractConflict
	}
	return nil
}
