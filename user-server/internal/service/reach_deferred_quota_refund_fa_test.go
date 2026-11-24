// reach_deferred_quota_refund_fa_test.go FA-REACH-03：
// 取消延迟触达后，配额返额必须可恢复、可对账、幂等；部分 scope 释放失败
// 不得伪装成「取消且返额完成」，重试/进程重启后按 reservation 状态收敛。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// faScopeRefundQuota 双 scope 返额器。按 reservation 状态逐 scope 幂等释放
// （与 ReachQuotaManager.ReleaseReservedIntent 同语义），第二笔可注入失败，
// 模拟「第一笔已释放、第二笔仍 reserved」的部分失败窗口。
type faScopeRefundQuota struct {
	failSecond bool
	calls      int
	states     map[string]string // intentID#scope -> state
	counter    int               // 剩余额度占用（每次成功释放 -1）
}

func newFAScopeRefundQuota(failSecond bool) *faScopeRefundQuota {
	return &faScopeRefundQuota{
		failSecond: failSecond,
		states: map[string]string{
			"intent-refund#s1": model.QuotaStateReserved,
			"intent-refund#s2": model.QuotaStateReserved,
		},
		counter: 2,
	}
}

func (f *faScopeRefundQuota) ReleaseReservedIntent(_ context.Context, intentID string) error {
	f.calls++
	for i, scope := range []string{"s1", "s2"} {
		key := intentID + "#" + scope
		if f.states[key] == model.QuotaStateReleased {
			continue // 已释放：幂等跳过
		}
		if i == 1 && f.failSecond {
			return errors.New("第二个 scope 释放失败（注入）")
		}
		f.states[key] = model.QuotaStateReleased
		f.counter--
	}
	return nil
}

func faRefundSetup(t *testing.T, failSecond bool) (*PersistentQuietHoursQueue, *repository.ReachDeferredOutboundRepository, *faScopeRefundQuota) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ReachDeferredOutbound{})
	repo := repository.NewReachDeferredOutboundRepository(database)
	q := NewPersistentQuietHoursQueue(repo)
	quota := newFAScopeRefundQuota(failSecond)
	q.SetQuotaReleaser(quota)
	return q, repo, quota
}

func faRefundDefer(t *testing.T, q *PersistentQuietHoursQueue) uint {
	t.Helper()
	if err := q.Defer(context.Background(), deferredReqWithIntent("intent-refund"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	rows, total, err := q.repo.ListPending(context.Background(), "", model.DeferredReachStatusPending, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("应 1 条 pending，得到 total=%d err=%v", total, err)
	}
	return rows[0].ID
}

func faRefundRow(t *testing.T, repo *repository.ReachDeferredOutboundRepository, id uint) model.ReachDeferredOutbound {
	t.Helper()
	rec, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	return rec
}

// ① 双 scope 第二笔释放失败：取消已提交，但返额保留 pending，返回明确错误；
// 第一笔已释放、第二笔仍 reserved，剩余额度只返还 1 笔，绝不假成功。
func TestFAReach03_DualScopeSecondFailureKeepsRefundPending(t *testing.T) {
	q, repo, quota := faRefundSetup(t, true)
	ctx := context.Background()
	id := faRefundDefer(t, q)

	err := q.CancelDeferredOutbound(ctx, id)
	if !errors.Is(err, ErrDeferredQuotaRefundPending) {
		t.Fatalf("取消返额失败应返回 ErrDeferredQuotaRefundPending，得到 %v", err)
	}
	rec := faRefundRow(t, repo, id)
	if rec.Status != model.DeferredReachStatusCancelled {
		t.Fatalf("取消状态应为 cancelled，得到 %s", rec.Status)
	}
	if rec.QuotaReleaseStatus != model.DeferredQuotaReleasePending {
		t.Fatalf("返额状态应保留 pending，得到 %q", rec.QuotaReleaseStatus)
	}
	if quota.states["intent-refund#s1"] != model.QuotaStateReleased {
		t.Fatalf("第一笔 scope 应已释放，得到 %q", quota.states["intent-refund#s1"])
	}
	if quota.states["intent-refund#s2"] != model.QuotaStateReserved {
		t.Fatalf("第二笔 scope 应仍 reserved，得到 %q", quota.states["intent-refund#s2"])
	}
	if quota.counter != 1 {
		t.Fatalf("剩余额度应只返还 1 笔，得到 %d", quota.counter)
	}
}

// ② 故障消失后对账：第二笔 scope 释放、计数归零、行标记 released。
func TestFAReach03_ReconcileReleasesRemainingScope(t *testing.T) {
	q, repo, quota := faRefundSetup(t, true)
	ctx := context.Background()
	id := faRefundDefer(t, q)

	if err := q.CancelDeferredOutbound(ctx, id); !errors.Is(err, ErrDeferredQuotaRefundPending) {
		t.Fatalf("首次取消应失败：%v", err)
	}
	quota.failSecond = false // 注入故障消失
	ok, failed, ferr := q.ReconcileCancelledQuotaRefunds(ctx, 10)
	if ok != 1 || failed != 0 || ferr != nil {
		t.Fatalf("对账应成功 1 失败 0，得到 ok=%d failed=%d err=%v", ok, failed, ferr)
	}
	rec := faRefundRow(t, repo, id)
	if rec.QuotaReleaseStatus != model.DeferredQuotaReleaseDone {
		t.Fatalf("对账后返额状态应为 released，得到 %q", rec.QuotaReleaseStatus)
	}
	if quota.states["intent-refund#s2"] != model.QuotaStateReleased {
		t.Fatalf("第二笔 scope 应对账释放，得到 %q", quota.states["intent-refund#s2"])
	}
	if quota.counter != 0 {
		t.Fatalf("对账后剩余额度应归零，得到 %d", quota.counter)
	}
}

// ③ 进程重启（新 queue 实例共享 DB）：重驱返额一致，且已结清行不再重复返额。
func TestFAReach03_RestartReplaysRefundIdempotently(t *testing.T) {
	q1, repo, quota := faRefundSetup(t, true)
	ctx := context.Background()
	id := faRefundDefer(t, q1)
	_ = q1.CancelDeferredOutbound(ctx, id) // 第二笔失败，留 pending
	quota.failSecond = false

	q2 := NewPersistentQuietHoursQueue(repo) // 模拟重启
	q2.SetQuotaReleaser(quota)
	ok, failed, _ := q2.ReconcileCancelledQuotaRefunds(ctx, 10)
	if ok != 1 || failed != 0 {
		t.Fatalf("重启对账应成功 1，得到 ok=%d failed=%d", ok, failed)
	}
	rec := faRefundRow(t, repo, id)
	if rec.Status != model.DeferredReachStatusCancelled || rec.QuotaReleaseStatus != model.DeferredQuotaReleaseDone {
		t.Fatalf("重启后应为 cancelled/released，得到 %s/%q", rec.Status, rec.QuotaReleaseStatus)
	}
	if quota.counter != 0 {
		t.Fatalf("重启对账后剩余额度应归零，得到 %d", quota.counter)
	}
	// 再次对账：已结清行不应再出现，也不得重复返额。
	callsBefore := quota.calls
	ok2, failed2, _ := q2.ReconcileCancelledQuotaRefunds(ctx, 10)
	if ok2 != 0 || failed2 != 0 {
		t.Fatalf("已结清行不应再被对账，得到 ok=%d failed=%d", ok2, failed2)
	}
	if quota.calls != callsBefore {
		t.Fatalf("已结清行不得重复调用返额器，calls %d→%d", callsBefore, quota.calls)
	}
}

// ④ repeated cancel 幂等：released 行重复取消不再触发返额。
func TestFAReach03_RepeatedCancelNoDoubleRefund(t *testing.T) {
	q, repo, quota := faRefundSetup(t, false)
	ctx := context.Background()
	id := faRefundDefer(t, q)

	if err := q.CancelDeferredOutbound(ctx, id); err != nil {
		t.Fatalf("首次取消应成功：%v", err)
	}
	if quota.counter != 0 || quota.calls != 1 {
		t.Fatalf("首次取消应返额 2 笔、调用 1 次，得到 counter=%d calls=%d", quota.counter, quota.calls)
	}
	if err := q.CancelDeferredOutbound(ctx, id); err != nil {
		t.Fatalf("重复取消应幂等成功：%v", err)
	}
	if quota.calls != 1 || quota.counter != 0 {
		t.Fatalf("重复取消不得重复返额，calls=%d counter=%d", quota.calls, quota.counter)
	}
	if rec := faRefundRow(t, repo, id); rec.QuotaReleaseStatus != model.DeferredQuotaReleaseDone {
		t.Fatalf("返额状态应为 released，得到 %q", rec.QuotaReleaseStatus)
	}
}

// ⑤ CAS 原语：CancelWithQuotaRefund 仅 pending 命中一次，并发/重复调用只有一笔成功。
func TestFAReach03_CancelWithQuotaRefundCAS(t *testing.T) {
	_, repo, _ := faRefundSetup(t, false)
	ctx := context.Background()
	rec := &model.ReachDeferredOutbound{
		Channel: "wecom", AccountID: "acc-1", RecipientID: "cust-1",
		Status: model.DeferredReachStatusPending, SendAt: time.Now().Add(time.Hour),
		RequestJSON: model.JSONMap{"metadata": map[string]any{"action_intent_id": "intent-refund"}},
	}
	if err := repo.CreatePending(ctx, rec); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	rows, err := repo.CancelWithQuotaRefund(ctx, rec.ID)
	if err != nil || rows != 1 {
		t.Fatalf("首次 CAS 应命中 1 行，得到 rows=%d err=%v", rows, err)
	}
	rows, err = repo.CancelWithQuotaRefund(ctx, rec.ID)
	if err != nil || rows != 0 {
		t.Fatalf("重复 CAS 应命中 0 行，得到 rows=%d err=%v", rows, err)
	}
	if got := faRefundRow(t, repo, rec.ID); got.QuotaReleaseStatus != model.DeferredQuotaReleasePending {
		t.Fatalf("CAS 后返额状态应为 pending，得到 %q", got.QuotaReleaseStatus)
	}
}
