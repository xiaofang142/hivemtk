package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SOPDispatchClaimRepository 定时器派发认领凭据仓储（FA-45）。
type SOPDispatchClaimRepository struct {
	db *gorm.DB
}

func NewSOPDispatchClaimRepository(db *gorm.DB) *SOPDispatchClaimRepository {
	return &SOPDispatchClaimRepository{db: db}
}

func NewSOPDispatchClaimRepositoryDefault() *SOPDispatchClaimRepository {
	return NewSOPDispatchClaimRepository(GetDB())
}

// Available 报告认领凭据表是否已迁移。未迁移时调用方走 legacy 直投路径，
// 使滚动发布期（新代码先于新表）与未建此表的测试继续工作。
func (r *SOPDispatchClaimRepository) Available() bool {
	if r == nil || r.db == nil {
		return false
	}
	return r.db.Migrator().HasTable(&model.SOPDispatchClaim{})
}

// Acquire 以 TimerID 唯一键抢占认领；已存在时返回既有行（acquired=false），
// 由调用方按 status/lease 决定跳过、续租或重派。
func (r *SOPDispatchClaimRepository) Acquire(ctx context.Context, c *model.SOPDispatchClaim) (acquired bool, err error) {
	if r == nil || r.db == nil {
		return false, errors.New("sop dispatch claim repository not initialized")
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(c)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// GetByTimerID 读取某定时器的认领凭据（不存在返回 nil,nil）。
func (r *SOPDispatchClaimRepository) GetByTimerID(ctx context.Context, timerID uint) (*model.SOPDispatchClaim, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop dispatch claim repository not initialized")
	}
	var c model.SOPDispatchClaim
	err := r.db.WithContext(ctx).Where("timer_id = ?", timerID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RenewExpired 续租一条租约已过期的 pending claim：Attempts+1、刷新 LeaseUntil。
// CAS 条件 status=pending AND lease_until<=now 保证多实例只有一个抢到。
func (r *SOPDispatchClaimRepository) RenewExpired(ctx context.Context, id uint, now time.Time, lease time.Duration, maxAttempts int) (rows int64, attempts int, err error) {
	if r == nil || r.db == nil {
		return 0, 0, errors.New("sop dispatch claim repository not initialized")
	}
	var c model.SOPDispatchClaim
	if err = r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error; err != nil {
		return 0, 0, err
	}
	if c.Attempts >= maxAttempts {
		res := r.db.WithContext(ctx).Model(&model.SOPDispatchClaim{}).
			Where("id = ? AND status = ? AND lease_until <= ?", id, model.SOPDispatchClaimPending, now).
			Update("status", model.SOPDispatchClaimFailed)
		return res.RowsAffected, c.Attempts, res.Error
	}
	next := c.Attempts + 1
	res := r.db.WithContext(ctx).Model(&model.SOPDispatchClaim{}).
		Where("id = ? AND status = ? AND lease_until <= ?", id, model.SOPDispatchClaimPending, now).
		Updates(map[string]any{"lease_until": now.Add(lease), "attempts": next})
	return res.RowsAffected, next, res.Error
}

// Release 立即把租约置为已过期（投递失败时调用，下一轮即可恢复重派）。
func (r *SOPDispatchClaimRepository) Release(ctx context.Context, id uint, now time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("sop dispatch claim repository not initialized")
	}
	return r.db.WithContext(ctx).Model(&model.SOPDispatchClaim{}).
		Where("id = ? AND status = ?", id, model.SOPDispatchClaimPending).
		Update("lease_until", now.Add(-time.Second)).Error
}

// Confirm pending→confirmed，确认投递已被队列真正接收。
func (r *SOPDispatchClaimRepository) Confirm(ctx context.Context, id uint, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop dispatch claim repository not initialized")
	}
	res := r.db.WithContext(ctx).Model(&model.SOPDispatchClaim{}).
		Where("id = ? AND status = ?", id, model.SOPDispatchClaimPending).
		Updates(map[string]any{"status": model.SOPDispatchClaimConfirmed, "confirmed_at": &now})
	return res.RowsAffected, res.Error
}

// FindExpired 扫描租约已过期的 pending claim（按 timer_id 排序，多实例 CAS 续租）。
func (r *SOPDispatchClaimRepository) FindExpired(ctx context.Context, now time.Time, limit int) ([]model.SOPDispatchClaim, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop dispatch claim repository not initialized")
	}
	if limit < 1 {
		limit = 100
	}
	var list []model.SOPDispatchClaim
	err := r.db.WithContext(ctx).
		Where("status = ? AND lease_until <= ?", model.SOPDispatchClaimPending, now).
		Order("timer_id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}
