// sales_cockpit_repo.go 驾驶舱聚合查询仓储（五层 L5）
package repository

import (
	"context"

	"gorm.io/gorm"
)

// SalesCockpitRepository 驾驶舱聚合 SQL 收口
type SalesCockpitRepository struct {
	db *gorm.DB
}

// NewSalesCockpitRepository 构造
func NewSalesCockpitRepository(db *gorm.DB) *SalesCockpitRepository {
	return &SalesCockpitRepository{db: db}
}

// CountWhere 任意表条件计数（表名与条件由调用方传入，仅内部聚合使用，无用户输入拼接）
func (r *SalesCockpitRepository) CountWhere(ctx context.Context, table, cond string, args ...any) int64 {
	if r.db == nil {
		return 0
	}
	var n int64
	if err := r.db.WithContext(ctx).Table(table).Where(cond, args...).Count(&n).Error; err != nil {
		return 0
	}
	return n
}

// GroupQuery 任意分组聚合查询（只读 SELECT，供驾驶舱统计卡片）
func (r *SalesCockpitRepository) GroupQuery(ctx context.Context, sql string, args ...any) []map[string]any {
	if r.db == nil {
		return []map[string]any{}
	}
	out := []map[string]any{}
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return []map[string]any{}
	}
	return out
}
