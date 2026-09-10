package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

// SystemConfigKVRepository KV 配置仓储接口
type SystemConfigKVRepository interface {
	// Available 报告底层 DB 是否可用（nil 探测收敛到 repository 层）。
	Available() bool
	Get(ctx context.Context, key string) (string, error)

	Upsert(ctx context.Context, key, value string) (string, error)

	EnsureTable(ctx context.Context) error
}

type systemConfigKVRepo struct {
	db *gorm.DB
}

// NewSystemConfigKVRepository 构造
func NewSystemConfigKVRepository() SystemConfigKVRepository {
	return &systemConfigKVRepo{db: db.GetDB()}
}

// Available 报告底层 DB 是否可用。
func (r *systemConfigKVRepo) Available() bool {
	return r != nil && r.db != nil
}

func (r *systemConfigKVRepo) Get(ctx context.Context, key string) (string, error) {
	var row model.SystemConfigKV
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return row.Value, nil
}

func (r *systemConfigKVRepo) Upsert(ctx context.Context, key, value string) (string, error) {
	now := time.Now()
	row := model.SystemConfigKV{
		Key:       key,
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"value":      value,
			"updated_at": now,
		}),
	}).Create(&row).Error
	if err != nil {
		if ensureErr := r.EnsureTable(ctx); ensureErr != nil {
			return "", err
		}
		err = r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "key"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"value":      value,
				"updated_at": now,
			}),
		}).Create(&row).Error
		if err != nil {
			return "", err
		}
	}
	return value, nil
}

func (r *systemConfigKVRepo) EnsureTable(ctx context.Context) error {
	stmt := `CREATE TABLE IF NOT EXISTS system_config_kv (
		key VARCHAR(100) PRIMARY KEY,
		value TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	return r.db.WithContext(ctx).Exec(stmt).Error
}

var _ SystemConfigKVRepository = (*systemConfigKVRepo)(nil)
