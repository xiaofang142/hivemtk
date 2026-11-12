// advisory_lock.go — 跨实例互斥锁（GORM 形态）。
//
// 为什么不用 `db.Select("pg_try_advisory_lock(?)", key).Scan(&held)` 直写：
// GORM 的 Select 在**没有 Model/Table** 时会拼出 `SELECT <expr> FROM `
// （FROM 后面是空的）⇒ 结果集多一列空值，Scan 进 bool 报
// "unsupported data type: <nil>"，而且**锁根本没拿到**。
// POST /api/monitor/trace-eval/trigger 恒 500，就是这一句；
// geo/service/job_manager.go 的分布式锁是同形缺陷（且无测试覆盖，一直没暴露）。
//
// 正确形态见下面 advisoryLockFrom 的注释。
//
// 判读与 geo/service/job_manager.go 的分布式锁一致：
// pg_try_advisory_lock / pg_advisory_unlock 都不读写租户表行，
// 裸 ctx 不构成隔离 callback 的触点。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// advisoryLockFrom 是拼 FROM 时用的载体：**恒一行的派生表**。
//
// 为什么是派生表而不是某张真表：
//   - 没有 Model/Table 时，GORM 拼出 `SELECT <expr> FROM `（FROM 后面是空的）
//     ⇒ 结果集多一列空值，Scan 进 bool 报 "unsupported data type: <nil>"。
//   - 改成某张真表也不对：那张表要有数据才有行，`SELECT pg_try_advisory_lock(k)
//     FROM <空表>` 返回 0 行 ⇒ **锁函数根本没被求值** ⇒ 锁没拿到，
//     而返回的 false 会被调用方读成「已有实例在运行」⇒ 多实例保护形同虚设。
//   - 派生表恒一行 ⇒ 函数必被求值一次，锁语义正确。
//
// 派生表不触达任何租户表 ⇒ 不构成隔离 callback 的触点（与 geo job_manager
// 的同一判读一致：advisory lock 是会话级全局闸）。
const advisoryLockFrom = "(SELECT 1) AS t"

// TryAdvisoryLock 尝试获取会话级 advisory lock。
//
// 返回 acquired=false 表示锁已被别人持有（不是错误）。
// key 是 int64 锁键。
//
// ⚠️ 只能传**会话固定**的句柄（`db.Connection` 回调里的 conn、或事务 tx）。
// 传池句柄（`db.GetDB()` / 仓储的 r.db）是错的：pg_try_advisory_lock 是
// **会话级**锁，下一次 pg_advisory_unlock 可能落在池里另一条连接上 ⇒ 解锁
// 返回 false 但**不报错**，锁就永久滞留在原会话上，此后所有实例都读到
// "已被其他实例锁定"并静默跳过（2026-11-10 实证：negative_monitor 与
// llm_usage_daily 聚合各泄漏一把）。需要"持锁跨多次操作"时用
// TryAdvisoryLockOnConn（把连接锁在句柄里）。
func TryAdvisoryLock(ctx context.Context, gdb *gorm.DB, key int64) (bool, error) {
	if gdb == nil {
		return false, fmt.Errorf("db is nil")
	}
	var held bool
	if err := gdb.WithContext(ctx).Table(advisoryLockFrom).
		Select("pg_try_advisory_lock(?)", key).Scan(&held).Error; err != nil {
		return false, err
	}
	return held, nil
}

// ReleaseAdvisoryLock 释放会话级 advisory lock（必须在**加锁的同一会话**上调用）。
// 与 TryAdvisoryLock 同一约束：不要传池句柄。
func ReleaseAdvisoryLock(ctx context.Context, gdb *gorm.DB, key int64) error {
	if gdb == nil {
		return fmt.Errorf("db is nil")
	}
	var unlocked bool
	return gdb.WithContext(ctx).Table(advisoryLockFrom).
		Select("pg_advisory_unlock(?)", key).Scan(&unlocked).Error
}

// AdvisoryLockHandle 一条**独占连接**上的会话级 advisory lock 句柄。
//
// 存在理由就是上面那条约束：加锁与解锁必须落在同一个后端会话。句柄把那条
// 连接扣在手里（`*sql.Conn`），Release 用同一连接解锁后归还池。
//
// 容量代价（如实记录，别当零成本）：持锁期间占用池里 1 条连接，且在按需新建
// 连接时会占用 partial-conn 名额（database/sql 的 maxOpen 里预留 maxIdle 给空闲
// 连接）。极端情形：租户数 × 同时到点的不同任务数接近 MaxOpenConns 时，新连接
// 请求可能撞上 "connection timed out"。当前量级（单机默认 200 连接、GEO 任务 7 个、
// 租户数十级）远未触及；若将来租户数上量，应改为"把持锁连接数控制在固定小池"
// 或换用事务级锁 / Redis 租约，而不是放开这条约束。
//
// 收益：进程崩溃时 TCP 断开 ⇒ PG 自动释放该会话的全部 advisory lock，
// 不会出现"锁永远不还"的死状态（这正是池形态缺陷的最坏后果）。
type AdvisoryLockHandle struct {
	key     int64
	conn    *sql.Conn
	release sync.Once
	err     error
	// OnRelease 可选观察钩子（测试/桩用）：在真正解锁动作之后、归还连接之前调用。
	// 只为让不连库的接缝桩能证明"任务收口确实还了锁"，生产路径留空。
	OnRelease func()
}

// Key 锁键（排障用）。
func (h *AdvisoryLockHandle) Key() int64 {
	if h == nil {
		return 0
	}
	return h.key
}

// Release 用**加锁时那条连接**解锁并归还连接。可重复调用（第二次起是空操作）。
//
// 解锁结果会被判读：pg_advisory_unlock 返回 false = 本会话并不持有该锁
// （解锁落错会话的典型症状）。这与旧实现"吞掉返回值"的差别就是本次修法的
// 核心 —— 静默失败的解锁等于锁泄漏，必须出声。
func (h *AdvisoryLockHandle) Release(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.release.Do(func() {
		conn := h.conn
		h.conn = nil
		if conn == nil {
			if h.OnRelease != nil {
				h.OnRelease()
			}
			return
		}
		var unlocked bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", h.key).Scan(&unlocked); err != nil {
			h.err = fmt.Errorf("advisory unlock(%d) 失败: %w", h.key, err)
		} else if !unlocked {
			h.err = fmt.Errorf("advisory unlock(%d) 返回 false：本会话未持有该锁（加解锁不在同一会话）", h.key)
		}
		// 归还连接放在最后：万一 QueryRowContext 让连接变坏，Close 会丢弃它；
		// 丢掉坏连接不影响锁 —— 连接断开本身就会让 PG 释放会话锁。
		if cerr := conn.Close(); cerr != nil && h.err == nil {
			h.err = fmt.Errorf("advisory lock(%d) 归还连接失败: %w", h.key, cerr)
		}
		if h.OnRelease != nil {
			h.OnRelease()
		}
	})
	return h.err
}

// TryAdvisoryLockOnConn 取会话级 advisory lock，并把**承载它的那条独占连接**
// 一并返回。acquired=false 时返回 (nil, false, nil) —— 锁在别人手里，不是错误。
//
// 与 TryAdvisoryLock 的分工：那条是"同一会话内加解锁"的裸原语，调用方要自己
// 保证会话固定（如 db.Connection 回调）；这条把会话固定**做进句柄**，适合
// "锁要跨多次独立操作持有"的场景（GEO 定时任务要跑完整个任务体才解锁）。
func TryAdvisoryLockOnConn(ctx context.Context, gdb *gorm.DB, key int64) (*AdvisoryLockHandle, bool, error) {
	if gdb == nil {
		return nil, false, fmt.Errorf("db is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, false, fmt.Errorf("取 sql.DB 失败: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("取独占连接失败: %w", err)
	}
	var held bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&held); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !held {
		_ = conn.Close()
		return nil, false, nil
	}
	return &AdvisoryLockHandle{key: key, conn: conn}, true, nil
}

// TryAdvisoryLockConn 在**调用方提供的独占连接**上取会话级 advisory lock，
// 用于 `gorm.DB.Connection(func(conn *gorm.DB) error {...})` 回调内部：那条
// 回调里的 conn 整个生命周期固定在同一条连接上，加锁与解锁因此天然同会话。
//
// 与 TryAdvisoryLock（裸 GORM 句柄，易踩池形态）的区别只是**显式要求**调用方
// 已经固定了会话，语义与返回值完全一致。
func TryAdvisoryLockConn(ctx context.Context, conn *gorm.DB, key int64) (bool, error) {
	if conn == nil {
		return false, fmt.Errorf("db is nil")
	}
	var held bool
	if err := conn.WithContext(ctx).Table(advisoryLockFrom).
		Select("pg_try_advisory_lock(?)", key).Scan(&held).Error; err != nil {
		return false, err
	}
	return held, nil
}

// TryAdvisoryXactLock 尝试获取**事务级** advisory lock（pg_try_advisory_xact_lock）。
//
// 与会话级那两条的分工：事务级锁在 tx 结束（commit/rollback）时自动释放，
// 不需要也**不可以**手工 unlock —— 用在"必须在同一个事务里持锁做完读-判-写"的场景
// （例：退款入账的"锁账单 → 事务内求和 → 校验 → 插入"）。
//
// 为什么用 try 变体：阻塞版 `pg_advisory_xact_lock` 返回 void，Scan 到任何 Go 值都报
// "unsupported data type: <nil>"（实测）；`Exec` 又被 GORM-only 门禁禁在业务包外。
// try 变体返回 boolean，能走下面这条恒一行派生表的形态（陷阱见 advisoryLockFrom 注释）。
//
// 返回 acquired=false 表示锁被别人持有（不是错误），调用方决定重试还是放弃。
func TryAdvisoryXactLock(ctx context.Context, gdb *gorm.DB, key int64) (bool, error) {
	if gdb == nil {
		return false, fmt.Errorf("db is nil")
	}
	var held bool
	if err := gdb.WithContext(ctx).Table(advisoryLockFrom).
		Select("pg_try_advisory_xact_lock(?)", key).Scan(&held).Error; err != nil {
		return false, err
	}
	return held, nil
}

// AcquireAdvisoryXactLock 轮询直到拿到事务级锁，或 ctx 结束 / 超时。
//
// 阻塞语义靠轮询模拟（见 TryAdvisoryXactLock 的理由）；超时错误写清"等锁超时"，
// 调用方据此区分"锁在别人手里"与"库坏了"——两者都是 5xx，但排障方向相反。
func AcquireAdvisoryXactLock(ctx context.Context, gdb *gorm.DB, key int64, timeout time.Duration) error {
	if gdb == nil {
		return fmt.Errorf("db is nil")
	}
	deadline := time.Now().Add(timeout)
	const tick = 20 * time.Millisecond
	for {
		got, err := TryAdvisoryXactLock(ctx, gdb, key)
		if err != nil {
			return err
		}
		if got {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等事务级 advisory lock 超时（key=%d，%s）：另一个事务持有该键", key, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tick):
		}
	}
}
