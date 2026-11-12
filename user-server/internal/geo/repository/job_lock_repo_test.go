// job_lock_repo_test.go GEO 任务分布式锁仓储契约（PG 定向，8217）。
//
// 本文件钉的是**会话固定**语义：会话级 advisory lock 的加锁与解锁必须落在
// 同一条后端连接上。2026-11-10 实证缺陷 —— 旧的 TryLock/Unlock 两个方法各自
// `db.WithContext(ctx)` 从池里取连接，解锁落在别的会话上时 pg_advisory_unlock
// 返回 false 且不报错 ⇒ 锁永久滞留，`negative_monitor` 与 llm_usage_daily 聚合
// 此后每轮都被判成"已被其他实例锁定"而静默跳过。
//
// 因此这里不只测"能拿到"，还要测：
//  1. 拿到后，**同一池的另一个句柄**拿不到（互斥真的生效）；
//  2. Release 之后能重新拿到（锁真的还回去了，不是泄漏）；
//  3. 加锁/解锁在同一后端会话上（否则 pg_advisory_unlock 会返回 false）。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

func TestGeoJobLockRepo_AcquireAndRelease(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达：锁仓储契约判不了（不 Skip，跳过等于没锁）")
	}
	repo := NewGeoJobLockRepositoryWithDB(db)
	other := NewGeoJobLockRepositoryWithDB(db)
	ctx := context.Background()
	const key = int64(9173101)

	handle, held, err := repo.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	if !held || handle == nil {
		t.Fatal("锁没拿到：acquired=false 会被调用方读成「已有实例在运行」⇒ 实际从未加锁")
	}

	// 另一条连接必须拿不到：这是多实例保护的唯一意义。
	otherHandle, held2, err := other.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("第二次 Acquire 失败: %v", err)
	}
	if held2 || otherHandle != nil {
		if otherHandle != nil {
			_ = otherHandle.Release(ctx)
		}
		t.Fatal("同一锁键被两条会话同时持有：互斥失效")
	}

	// 释放后必须能重新拿到 —— 锁没还回去 = 下轮任务被永久挡住。
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("Release 失败（会话级锁必须由加锁的同一会话解锁）: %v", err)
	}
	handle3, held3, err := other.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("释放后再取失败: %v", err)
	}
	if !held3 || handle3 == nil {
		t.Fatal("Release 后必须能重新拿到锁（锁没还回去 = 下轮任务被永久挡住）")
	}
	if err := handle3.Release(ctx); err != nil {
		t.Fatalf("二次 Release 失败: %v", err)
	}
}

// TestGeoJobLockRepo_ReleaseIdempotent 重复 Release 不得报错、不得 panic
// （JobManager 的 defer 与异常路径可能各调一次）。
func TestGeoJobLockRepo_ReleaseIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewGeoJobLockRepositoryWithDB(db)
	ctx := context.Background()
	handle, held, err := repo.Acquire(ctx, int64(9173102))
	if err != nil || !held || handle == nil {
		t.Fatalf("Acquire 失败: held=%v err=%v", held, err)
	}
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("首次 Release 失败: %v", err)
	}
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("重复 Release 应幂等无错，实际: %v", err)
	}
}

func TestGeoJobLockRepo_NilDB(t *testing.T) {
	repo := NewGeoJobLockRepositoryWithDB(nil)
	if _, _, err := repo.Acquire(context.Background(), 1); err == nil {
		t.Fatal("nil 连接应返回 error（与改前 dbutil.TryAdvisoryLock(nil) 同形）")
	}
}
