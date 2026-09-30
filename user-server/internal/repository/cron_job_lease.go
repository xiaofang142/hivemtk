package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// cronLeaseStaleAfter 心跳超过这个时长没更新就视为持有者已退出，别的进程可接管。
// 取 3 分钟：抢到租约的进程每分钟续一次（见 service 侧的清扫节拍），
// 于是一次网络抖动/慢查询最多让出一拍，而进程真死后最多 3 分钟就有人接上。
const cronLeaseStaleAfter = 3 * time.Minute

// ErrInvalidCronLeaseArgs 任务名或持有者标识为空：判不出该把租约给谁，宁可报错也不落一行脏数据
var ErrInvalidCronLeaseArgs = errors.New("cron lease: job_name 与 workerID 均不得为空")

// CronJobLeaseRepository 后台任务跨进程租约仓库
type CronJobLeaseRepository struct {
	db *gorm.DB
}

func NewCronJobLeaseRepository() *CronJobLeaseRepository {
	return &CronJobLeaseRepository{db: _db.GetDB()}
}

func NewCronJobLeaseRepositoryWithDB(db *gorm.DB) *CronJobLeaseRepository {
	return &CronJobLeaseRepository{db: db}
}

func (r *CronJobLeaseRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Hold 抢占或续约租约，返回本轮该不该由调用方干活。
//
// 三种情形都会让本行 owner 变成 workerID 并刷心跳：租约行还不存在（先建行再抢）、
// 本来就是自己持有（续约）、持有者心跳已陈旧（接管僵尸租约）。
// 其余情形返回 false——别的进程活着且还在续约，调用方本轮什么都不做。
//
// 建行用 OnConflict DoNothing：两个进程同时首次抢占会有一方插入失败，但紧跟的
// UPDATE 才是决胜点（WHERE 里带陈旧条件），不能拿插入成败当判据。
func (r *CronJobLeaseRepository) Hold(ctx context.Context, jobName, workerID string) (bool, error) {
	if r == nil || r.db == nil {
		return false, gorm.ErrInvalidDB
	}
	if jobName == "" || workerID == "" {
		return false, ErrInvalidCronLeaseArgs
	}
	now := time.Now()
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.CronJobLease{JobName: jobName}).Error; err != nil {
		return false, err
	}
	res := r.db.WithContext(ctx).Exec(
		`UPDATE cron_job_leases
		    SET owner = ?, heartbeat_at = ?
		  WHERE job_name = ?
		    AND (owner = '' OR owner IS NULL OR owner = ? OR heartbeat_at IS NULL
		         OR heartbeat_at < ?)`,
		workerID, now, jobName, workerID, now.Add(-cronLeaseStaleAfter))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Release 交回租约：只清自己持有的那一行，别的进程已接管时不会误清。
// 进程关停时调用，省掉下一任最多 3 分钟的接管等待。
func (r *CronJobLeaseRepository) Release(ctx context.Context, jobName, workerID string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Exec(
		`UPDATE cron_job_leases SET owner = '', heartbeat_at = NULL
		  WHERE job_name = ? AND owner = ?`, jobName, workerID).Error
}

// Holder 读回当前持有者与心跳（状态端点/排查用）。行不存在时 found=false。
func (r *CronJobLeaseRepository) Holder(ctx context.Context, jobName string) (owner string, heartbeatAt *time.Time, found bool, err error) {
	if r == nil || r.db == nil {
		return "", nil, false, gorm.ErrInvalidDB
	}
	var row model.CronJobLease
	q := r.db.WithContext(ctx).Where("job_name = ?", jobName).First(&row)
	if q.Error != nil {
		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return "", nil, false, nil
		}
		return "", nil, false, q.Error
	}
	return row.Owner, row.HeartbeatAt, true, nil
}
