// job_lock_repo_test.go GEO 任务分布式锁仓储契约（PG 定向，8217）。
//
// AV-04c 批次3 自 geo/service/job_manager.go 下沉 advisory lock 时，唯一要钉的
// 就是「锁必须真的拿到」：返回值 false 会被调用方读成"已有实例在运行"并跳过本轮
// ——锁从未生效的多实例保护是静默失效。SQL 载体（恒一行派生表）的陷阱论证在
// internal/pkg/db/advisory_lock.go，本仓储只做转发，不重复测 PG 语义。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

func TestGeoJobLockRepo_TryLockAcquireAndRelease(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达：锁仓储契约判不了（不 Skip，跳过等于没锁）")
	}
	repo := NewGeoJobLockRepositoryWithDB(db)
	ctx := context.Background()
	const key = int64(9173101)

	held, err := repo.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock 失败: %v", err)
	}
	if !held {
		t.Fatal("锁没拿到：返回值 false 会被调用方读成「已有实例在运行」⇒ 实际从未加锁")
	}

	repo.Unlock(ctx, key)

	held2, err := repo.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("释放后再取失败: %v", err)
	}
	if !held2 {
		t.Fatal("Unlock 后必须能重新拿到锁（锁没还回去 = 下轮任务被永久挡住）")
	}
	repo.Unlock(ctx, key)
}

func TestGeoJobLockRepo_NilDB(t *testing.T) {
	repo := NewGeoJobLockRepositoryWithDB(nil)
	if _, err := repo.TryLock(context.Background(), 1); err == nil {
		t.Fatal("nil 连接应返回 error（与改前 dbutil.TryAdvisoryLock(nil) 同形）")
	}
	repo.Unlock(context.Background(), 1) // 吞错不 panic，与改前 `_ =` 同形
}
