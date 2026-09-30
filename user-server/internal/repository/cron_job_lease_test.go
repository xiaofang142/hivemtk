package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 租约的全部判据都在那一条 UPDATE 的 WHERE 里，所以逐条腿都要现证：
// 空行可抢 / 自持可续 / 他人活着不可抢 / 他人死了可接管 / 释放只清自己。
func TestCronJobLease_HoldAndRelease(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CronJobLease{})
	repo := NewCronJobLeaseRepositoryWithDB(db)
	ctx := context.Background()

	// 行还不存在：第一次 Hold 建行并由 A 持有
	held, err := repo.Hold(ctx, "tg_gate_sweeper", "hostA:1")
	if err != nil || !held {
		t.Fatalf("首次 Hold 应建行并由 A 持有, 实际 held=%v err=%v", held, err)
	}
	owner, hb, found, err := repo.Holder(ctx, "tg_gate_sweeper")
	if err != nil || !found || owner != "hostA:1" || hb == nil {
		t.Fatalf("持有者应读回 hostA:1 与心跳, 实际 owner=%q found=%v hb=%v err=%v", owner, found, hb, err)
	}
	firstHB := *hb

	// B 在 A 还活着时抢不到
	if held, err = repo.Hold(ctx, "tg_gate_sweeper", "hostB:2"); err != nil || held {
		t.Fatalf("A 心跳未陈旧时 B 不该抢到, 实际 held=%v err=%v", held, err)
	}
	if owner, _, _, _ = mustHolder(t, repo, ctx, "tg_gate_sweeper"); owner != "hostA:1" {
		t.Fatalf("抢失败的进程不该把 owner 改掉, 实际=%q", owner)
	}

	// A 自己再 Hold 是续约：仍然 true，且心跳被推后
	time.Sleep(2 * time.Millisecond)
	if held, err = repo.Hold(ctx, "tg_gate_sweeper", "hostA:1"); err != nil || !held {
		t.Fatalf("自持者续约应成功, 实际 held=%v err=%v", held, err)
	}
	if _, hb, _, _ = mustHolder(t, repo, ctx, "tg_gate_sweeper"); hb == nil || !hb.After(firstHB) {
		t.Fatalf("续约应把心跳推后, 实际 first=%v now=%v", firstHB, hb)
	}

	// 释放只认自己：B 释放不动 A 的租约
	if err := repo.Release(ctx, "tg_gate_sweeper", "hostB:2"); err != nil {
		t.Fatalf("非持有者 Release 不该报错: %v", err)
	}
	if owner, _, _, _ = mustHolder(t, repo, ctx, "tg_gate_sweeper"); owner != "hostA:1" {
		t.Fatalf("非持有者把租约清了, 实际 owner=%q", owner)
	}

	// A 心跳陈旧（进程已死的形态）→ B 接管
	stale := time.Now().Add(-cronLeaseStaleAfter - time.Minute)
	if err := db.Model(&model.CronJobLease{}).Where("job_name = ?", "tg_gate_sweeper").
		Update("heartbeat_at", stale).Error; err != nil {
		t.Fatalf("制造陈旧心跳失败: %v", err)
	}
	if held, err = repo.Hold(ctx, "tg_gate_sweeper", "hostB:2"); err != nil || !held {
		t.Fatalf("心跳陈旧后 B 应能接管僵尸租约, 实际 held=%v err=%v", held, err)
	}

	// 正常交回后立刻可被别人拿走（不等陈旧窗）
	if err := repo.Release(ctx, "tg_gate_sweeper", "hostB:2"); err != nil {
		t.Fatalf("持有者 Release 失败: %v", err)
	}
	if held, err = repo.Hold(ctx, "tg_gate_sweeper", "hostC:3"); err != nil || !held {
		t.Fatalf("释放后应立即可抢, 实际 held=%v err=%v", held, err)
	}
}

// 两个空参哨兵要在动库之前拦住：任务名为空会写出一行主键为空的租约，
// 持有者名为空则任何进程都能"续"上这条租约，互斥直接失效。
func TestCronJobLease_RejectsEmptyArgs(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CronJobLease{})
	repo := NewCronJobLeaseRepositoryWithDB(db)
	ctx := context.Background()

	for _, c := range []struct{ job, worker string }{{"", "hostA:1"}, {"tg_gate_sweeper", ""}} {
		if held, err := repo.Hold(ctx, c.job, c.worker); err != ErrInvalidCronLeaseArgs || held {
			t.Fatalf("空参应被拦住, 实际 job=%q worker=%q held=%v err=%v", c.job, c.worker, held, err)
		}
	}
	if _, _, found, err := repo.Holder(ctx, ""); err != nil || found {
		t.Fatalf("空任务名不该有租约行, 实际 found=%v err=%v", found, err)
	}
}

func mustHolder(t *testing.T, repo *CronJobLeaseRepository, ctx context.Context, job string) (string, *time.Time, bool, error) {
	t.Helper()
	owner, hb, found, err := repo.Holder(ctx, job)
	if err != nil || !found {
		t.Fatalf("读回租约 %s 失败: found=%v err=%v", job, found, err)
	}
	return owner, hb, found, err
}
