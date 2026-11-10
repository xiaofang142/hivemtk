package repository

// job_lock.go GEO 定时任务分布式锁（会话级 advisory lock）。
//
// 判读沿 internal/pkg/db/advisory_lock.go：pg_try_advisory_lock / pg_advisory_unlock
// 都是**会话级**、不读写任何租户表行（裸 ctx 不构成隔离 callback 的触点）。
//
// ⚠️ 2026-11-10 修：本仓储此前是 TryLock / Unlock 两个独立方法，各自
// `r.db.WithContext(ctx)` ⇒ **从池里随机取连接**。会话级锁的加锁与解锁落在
// 不同会话上时，pg_advisory_unlock 返回 false 但**不报错**，锁永久滞留在
// 加锁那条连接上。实证：`negative_monitor`（锁键 negative_monitor/1）与
// llm_usage_daily 聚合（830083）各自泄漏一把锁，此后每次都读成"已被其他
// 实例锁定"并静默跳过 —— 任务再也不跑，日志只有一句 skip。
//
// 现在改为 Acquire 返回**句柄**：句柄内部扣住那条独占连接，Release 用它解锁。
// 契约：
//   - Acquire 返回 acquired=false 表示锁被其他实例持有（**不是错误**），调用方跳过本轮；
//   - 返回 error 表示锁状态未知，调用方记日志后放行本轮（宁多跑一轮，不可静默停摆）；
//   - Release 可重复调用；解锁失败（含 unlock 返回 false）经 error 上报，不再吞掉。
//   - key 是调用方派生的 int64 锁键（job_manager.advisoryLockKey 派生，本层不解读）。

import (
	"context"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoJobLockRepository GEO 定时任务分布式锁仓储接口。
type GeoJobLockRepository interface {
	// Acquire 尝试取锁并返回持有独占连接的句柄。acquired=false 时 handle 为 nil。
	Acquire(ctx context.Context, key int64) (*_db.AdvisoryLockHandle, bool, error)
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

func (r *geoJobLockRepo) Acquire(ctx context.Context, key int64) (*_db.AdvisoryLockHandle, bool, error) {
	return _db.TryAdvisoryLockOnConn(ctx, r.db, key)
}
