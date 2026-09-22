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

// NewSystemConfigKVRepository 构造。
//
// 注意它捕获的是**调用那一刻**的 db.GetDB()：装配顺序不对时拿到的是 nil，
// 而 nil *gorm.DB 在 Get 里不是 error 而是 panic。需要句柄确定性的装配点用下面那个。
func NewSystemConfigKVRepository() SystemConfigKVRepository {
	return &systemConfigKVRepo{db: db.GetDB()}
}

// NewSystemConfigKVRepositoryWithDB 用显式句柄构造（与商机/报价/审批各仓储的 WithDB 同一口径）。
//
// 存在的理由是装配点的句柄一致性：报价竖的模板与话术指针读的是这张表，装配函数手里已经
// 有一把 db，再走全局句柄就等于"版本行读 A 库、模板读 B 库"这种只在多库环境才暴露的错。
func NewSystemConfigKVRepositoryWithDB(gormDB *gorm.DB) SystemConfigKVRepository {
	return &systemConfigKVRepo{db: gormDB}
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
