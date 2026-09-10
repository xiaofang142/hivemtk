// email_account_repo.go 邮件账号仓储（五层 L5）
package repository

import (
	"context"

	"gorm.io/gorm"
)

// EmailAccountRow email_accounts 行（与 service.EmailAccount 字段一致，gorm 扫描用）
type EmailAccountRow struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Name       string `gorm:"type:varchar(100)" json:"name"`
	Host       string `gorm:"type:varchar(255);not null" json:"host"`
	Port       int    `gorm:"default:465" json:"port"`
	Username   string `gorm:"type:varchar(255)" json:"username"`
	Password   string `gorm:"type:varchar(255)" json:"-"`
	FromAddr   string `gorm:"type:varchar(255);not null" json:"from_addr"`
	FromName   string `gorm:"type:varchar(100)" json:"from_name"`
	UseSSL     bool   `gorm:"default:true" json:"use_ssl"`
	DailyQuota int    `gorm:"default:500" json:"daily_quota"`
	DailyUsed  int    `gorm:"default:0" json:"daily_used"`
	Status     string `gorm:"type:varchar(20);default:'active'" json:"status"`
}

// TableName 指定表名
func (EmailAccountRow) TableName() string { return "email_accounts" }

// EmailAccountRepository 邮件账号查询/配额计数仓储
type EmailAccountRepository struct {
	db *gorm.DB
}

// NewEmailAccountRepository 构造
func NewEmailAccountRepository(db *gorm.DB) *EmailAccountRepository {
	return &EmailAccountRepository{db: db}
}

// GetByID 按 ID 取账号；不存在返回 (nil, err)
func (r *EmailAccountRepository) GetByID(ctx context.Context, id uint) (*EmailAccountRow, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var acc EmailAccountRow
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// ListActiveBelowQuota 按 daily_used 升序列出有余量的 active 账号
func (r *EmailAccountRepository) ListActiveBelowQuota(ctx context.Context) ([]EmailAccountRow, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var candidates []EmailAccountRow
	err := r.db.WithContext(ctx).
		Where("status = ? AND daily_used < daily_quota", "active").
		Order("daily_used ASC").
		Find(&candidates).Error
	return candidates, err
}

// IncDailyUsed 配额计数自增（发信成功后调用；失败不阻断发信，由调用方留痕）
func (r *EmailAccountRepository) IncDailyUsed(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(&EmailAccountRow{}).
		Where("id = ?", id).
		UpdateColumn("daily_used", gorm.Expr("daily_used + 1")).Error
}
