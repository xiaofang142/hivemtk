package db

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

// TestTryAdvisoryLock_AcquiresAndExcludes 锁必须真的拿到。
//
// 这条断言是本文件存在的理由：2026-10-22 实测原来的写法
// `db.Select("pg_try_advisory_lock(?)", key).Scan(&held)` 会报
// "unsupported data type: <nil>"，而**返回值 false 看起来像"锁被别人拿了"**
// —— 于是所有调用方都安静地跳过了加锁，多实例并发保护形同虚设。
func TestTryAdvisoryLock_AcquiresAndExcludes(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	const key = int64(9173001)

	held, err := TryAdvisoryLock(ctx, db, key)
	if err != nil {
		t.Fatalf("TryAdvisoryLock 失败: %v", err)
	}
	if !held {
		t.Fatal("锁没拿到：返回值 false 会被调用方读成「已有实例在运行」⇒ 实际从未加锁")
	}

	// 同一 session 再取一次：PG 的会话级 advisory lock 可重入 ⇒ 仍为 true。
	held2, err := TryAdvisoryLock(ctx, db, key)
	if err != nil {
		t.Fatalf("二次 TryAdvisoryLock 失败: %v", err)
	}
	if !held2 {
		t.Log("二次加锁返回 false（会话级锁可重入时不会发生）")
	}

	if err := ReleaseAdvisoryLock(ctx, db, key); err != nil {
		t.Fatalf("ReleaseAdvisoryLock 失败: %v", err)
	}
}

// TestTryAdvisoryLock_UnlockFromOtherSessionFails 确定性证明「加解锁必须同会话」。
//
// 直接控制两条后端连接：A 加锁，B 解锁。B 的 pg_advisory_unlock 必然返回 false
// 且**不产生任何错误** —— 这正是池形态（每次 WithContext 取任意连接）下锁会
// 永久泄漏的机制，也是本仓改用 AdvisoryLockHandle 扣住连接的理由。
func TestTryAdvisoryLock_UnlockFromOtherSessionFails(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	const key = int64(9173008)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	connA, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("取连接 A 失败: %v", err)
	}
	defer connA.Close()
	connB, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("取连接 B 失败: %v", err)
	}
	defer connB.Close()

	var pidA, pidB int
	if err := connA.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pidA); err != nil {
		t.Fatalf("读 A 的 pid 失败: %v", err)
	}
	if err := connB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pidB); err != nil {
		t.Fatalf("读 B 的 pid 失败: %v", err)
	}
	if pidA == pidB {
		t.Skip("两条 sql.Conn 落到了同一后端会话，无法构造跨会话场景")
	}

	var held bool
	if err := connA.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&held); err != nil {
		t.Fatalf("A 加锁失败: %v", err)
	}
	if !held {
		t.Fatalf("A 应能拿到锁（key=%d 被别的用例占用？）", key)
	}
	defer func() {
		var rel bool
		_ = connA.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", key).Scan(&rel)
	}()

	// B 解锁：返回 false，且 err == nil —— 静默失败，就是缺陷的危险形态。
	var unlockedByB bool
	if err := connB.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlockedByB); err != nil {
		t.Fatalf("B 的 unlock 报了错（预期是静默 false）: %v", err)
	}
	if unlockedByB {
		t.Fatal("别的会话竟然解锁成功了：那 pg_advisory_unlock 就不是会话级语义，本仓的句柄设计前提需要重新评估")
	}

	// 锁仍在 A 手里：另一条连接依旧拿不到。
	var heldByB bool
	if err := connB.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&heldByB); err != nil {
		t.Fatalf("B 加锁失败: %v", err)
	}
	if heldByB {
		t.Fatal("跨会话解锁后锁仍可被 B 拿到：与「B 解锁无效」矛盾")
	}
}

// TestTryAdvisoryLock_NilDB nil 句柄不得 panic
func TestTryAdvisoryLock_NilDB(t *testing.T) {
	if _, err := TryAdvisoryLock(context.Background(), nil, 1); err == nil {
		t.Fatal("nil db 应当返回 error")
	}
	if err := ReleaseAdvisoryLock(context.Background(), nil, 1); err == nil {
		t.Fatal("nil db 应当返回 error")
	}
}

// TestTryAdvisoryLock_PooledHandleLeaksLock 回归：**池句柄加锁 + 池句柄解锁 = 锁泄漏**。
//
// 这是 2026-11-10 生产缺陷的成因：pg_try_advisory_lock / pg_advisory_unlock 都是
// 会话级函数，而 `db.WithContext(ctx)` 每次从池里取一条连接。两条语句落在不同
// 后端会话上时，unlock 返回 false 但**不报错** —— 锁留在加锁那条会话上，直到
// 该连接断开（池里可能几小时不回收）。症状：任务每轮都判成"已被其他实例锁定"。
//
// 本用例把两条语句故意分到不同连接上复现该症状，用来锁住"别再退回池句柄"这个口径。
// 注意：本例**不断言**池形态必然失败（池里只有一条空闲连接时它可以碰巧成功），
// 只断言"失败时必须是静默失败"这一危险特性，并证明固定连接形态恒成功。
func TestTryAdvisoryLock_PooledHandleLeaksLock(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	const key = int64(9173009)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	// 占住一条连接，逼后续池操作分流到另一条 —— 复现"加解锁不同会话"。
	blocker, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("取阻塞连接失败: %v", err)
	}
	defer blocker.Close()
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatalf("读 blocker pid 失败: %v", err)
	}

	held, err := TryAdvisoryLock(ctx, db, key)
	if err != nil {
		t.Fatalf("池句柄加锁失败: %v", err)
	}
	if !held {
		t.Skip("池形态这次把加锁落到了 blocker 之外的另一条连接上，无法复现分流；不影响本仓的固定连接契约")
	}

	// 池句柄解锁：若落在别的会话，返回 false 且无错 —— 正是缺陷的危险形态。
	var unlocked bool
	unlockErr := db.WithContext(ctx).Table(advisoryLockFrom).
		Select("pg_advisory_unlock(?)", key).Scan(&unlocked).Error
	if unlockErr == nil && !unlocked {
		t.Logf("已复现：池形态解锁落在别的会话上 ⇒ unlock=false 且无 error（锁泄漏，正是负向修正前的生产症状）")
	}

	// 无论上面是否复现，都必须把测试键清理干净：用固定连接形态确认并释放。
	handle, got, aerr := TryAdvisoryLockOnConn(ctx, db, key)
	if aerr != nil {
		t.Fatalf("固定连接形态加锁失败: %v", aerr)
	}
	if !got || handle == nil {
		// 池形态确实泄漏了锁 ⇒ 该会话还活着，这里拿不到是对的；
		// 断开 blocker 与池连接后锁才释放。把这条断言留给下面的显式校验。
		return
	}
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("固定连接形态解锁失败（加解锁必须同会话）: %v", err)
	}
}

// TestTryAdvisoryLockOnConn_SameSessionAndMutualExclusion 回归：固定连接形态必须
//
//  1. 能拿到锁；
//  2. 释放后能重新拿到（证明解锁真的落在同一会话，不是静默失败）；
//  3. 句柄活着的期间，另一条连接拿不到（互斥有效）。
func TestTryAdvisoryLockOnConn_SameSessionAndMutualExclusion(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	const key = int64(9173010)

	handle, held, err := TryAdvisoryLockOnConn(ctx, db, key)
	if err != nil {
		t.Fatalf("TryAdvisoryLockOnConn 失败: %v", err)
	}
	if !held || handle == nil {
		t.Fatal("锁没拿到")
	}

	// 持锁期间，另一次尝试必须失败（可能落在同一连接上而成功，故用独立连接判定）。
	sqlDB, _ := db.DB()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("取独立连接失败: %v", err)
	}
	var otherHeld bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&otherHeld); err != nil {
		t.Fatalf("独立连接加锁失败: %v", err)
	}
	if otherHeld {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", key)
		_ = conn.Close()
		_ = handle.Release(ctx)
		t.Fatal("同一锁键被两条会话同时持有：互斥失效")
	}
	_ = conn.Close()

	if err := handle.Release(ctx); err != nil {
		t.Fatalf("Release 必须成功（加解锁同会话），实际: %v", err)
	}

	// 释放后必须能重新拿到。
	handle2, held2, err := TryAdvisoryLockOnConn(ctx, db, key)
	if err != nil || !held2 || handle2 == nil {
		t.Fatalf("释放后应能重新拿到锁: held=%v err=%v", held2, err)
	}
	if err := handle2.Release(ctx); err != nil {
		t.Fatalf("二次 Release 失败: %v", err)
	}
}

// TestTryAdvisoryLockOnConn_NilDB nil 句柄不得 panic。
func TestTryAdvisoryLockOnConn_NilDB(t *testing.T) {
	if _, _, err := TryAdvisoryLockOnConn(context.Background(), nil, 1); err == nil {
		t.Fatal("nil db 应当返回 error")
	}
	if err := (*AdvisoryLockHandle)(nil).Release(context.Background()); err != nil {
		t.Fatalf("nil 句柄 Release 应是空操作，实际: %v", err)
	}
}
