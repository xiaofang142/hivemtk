// job_lock.go GEO 定时任务分布式锁仓储（AV-04c 批次3）。
//
// 下沉缘由：geo/service/job_manager.go 此前持 *gorm.DB 字段（经 SetJobManagerDB
// 注入句柄），只为调 dbutil.TryAdvisoryLock / ReleaseAdvisoryLock 两个函数
// —— G-ARCH 3.2 违规（service 层握句柄）。锁的获取/释放下沉到本仓储，
// service 层改持 GeoJobLockRepository 接口（句柄解析留在仓储层）。
//
// 锁载体与判读沿 internal/pkg/db/advisory_lock.go 原样：
// pg_try_advisory_lock / pg_advisory_unlock 都是会话级、不读写任何租户表行，
// 裸 ctx 不构成隔离 callback 触点（原 job_manager.go acquireDistributedLock
// 注释的同一判读，随查询一起下沉）。
package repository

import (
	"context"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoJobLockRepository GEO 定时任务分布式锁仓储接口。
//
// 语义契约（与改前 job_manager.go 直调 dbutil 两函数逐项一致）：
//   - TryLock 返回 acquired=false 表示锁被其他实例持有（**不是错误**），
//     调用方跳过本轮；错误表示锁状态未知，调用方记日志后放行本轮
//     （宁多跑一轮，不可静默停摆——原错误路径不变）。
//   - Unlock 释放失败仅吞掉错误（原 `_ = dbutil.ReleaseAdvisoryLock(...)` 同形）。
//   - key 是调用方派生的 int64 锁键（job_manager.advisoryLockKey 派生，本层不解读）。
type GeoJobLockRepository interface {
	TryLock(ctx context.Context, key int64) (bool, error)
	Unlock(ctx context.Context, key int64)
}

type geoJobLockRepo struct{ db *gorm.DB }

// NewGeoJobLockRepository 默认构造（全局句柄，仓库既有惯例）。
func NewGeoJobLockRepository() GeoJobLockRepository {
	return &geoJobLockRepo{db: _db.GetDB()}
}

// NewGeoJobLockRepositoryWithDB 用装配/测试注入的连接构造
// （cron.InitCron 装配点传 NewGeoJobLockRepositoryWithDB(原句柄)，连接不变）。
func NewGeoJobLockRepositoryWithDB(db *gorm.DB) GeoJobLockRepository {
	return &geoJobLockRepo{db: db}
}

func (r *geoJobLockRepo) TryLock(ctx context.Context, key int64) (bool, error) {
	return _db.TryAdvisoryLock(ctx, r.db, key)
}

func (r *geoJobLockRepo) Unlock(ctx context.Context, key int64) {
	_ = _db.ReleaseAdvisoryLock(ctx, r.db, key)
}
