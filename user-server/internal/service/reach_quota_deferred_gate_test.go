package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// FA-08 判据测试：延迟触达不得提前消耗不可退还的配额。
//
//	① 入队→取消：reserved 释放返额（含跨日计数行归属）；
//	② 入队→重启→实际发送：同一 intent 只 commit 一次；
//	③ committed 后取消：不返额（已发生动作不得当未发生）。
type quotaDeferredFixture struct {
	q     *PersistentQuietHoursQueue
	drepo *repository.ReachDeferredOutboundRepository
	m     *ReachQuotaManager
	qrepo *repository.ReachQuotaRepository
}

func setupQuotaDeferred(t *testing.T) *quotaDeferredFixture {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.ReachDeferredOutbound{},
		&model.ReachQuotaReservation{},
		&model.ReachQuotaCounter{},
	)
	drepo := repository.NewReachDeferredOutboundRepository(database)
	qrepo := repository.NewReachQuotaRepository(database)
	m := NewReachQuotaManager(qrepo)
	q := NewPersistentQuietHoursQueue(drepo)
	q.SetQuotaReleaser(m)
	return &quotaDeferredFixture{q: q, drepo: drepo, m: m, qrepo: qrepo}
}

func quotaScopes() []QuotaScopeDef {
	return []QuotaScopeDef{{Key: "customer:cust-1", Limit: 3}}
}

func deferredReqWithIntent(intent string) *ReachSendRequest {
	req := deferredTestReq()
	req.Metadata = map[string]string{"action_intent_id": intent}
	return req
}

func counterUsed(t *testing.T, f *quotaDeferredFixture, day string) int {
	t.Helper()
	used, err := f.qrepo.GetCounterUsed(context.Background(), "customer:cust-1", day)
	if err != nil {
		t.Fatalf("读计数行失败: %v", err)
	}
	return used
}

// ① 入队→取消：释放返额；计数行回到 0。
func TestDeferredQueue_CancelReleasesReservedQuota(t *testing.T) {
	f := setupQuotaDeferred(t)
	ctx := context.Background()
	day := time.Now().In(cstZone).Format("2006-01-02")

	if _, _, err := f.m.ReserveIntent(ctx, "intent-cancel", "h", quotaScopes()); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if used := counterUsed(t, f, day); used != 1 {
		t.Fatalf("预占后计数应为 1，得到 %d", used)
	}
	if err := f.q.Defer(ctx, deferredReqWithIntent("intent-cancel"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	rows, total, _ := f.drepo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if total != 1 {
		t.Fatalf("应 1 条 pending，得到 %d", total)
	}
	if err := f.q.CancelDeferredOutbound(ctx, rows[0].ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	rec, _ := f.qrepo.GetReservation(ctx, "intent-cancel#customer:cust-1")
	if rec == nil || rec.State != model.QuotaStateReleased {
		t.Fatalf("取消后预占必须 released，得到 %+v", rec)
	}
	if used := counterUsed(t, f, day); used != 0 {
		t.Fatalf("取消后计数必须返还为 0，得到 %d", used)
	}
}

// ② 入队→重启→实际发送：同一 intent 经管线重发成功恰好 commit 一次。
func TestDeferredQueue_ReplayCommitsQuotaExactlyOnce(t *testing.T) {
	f := setupQuotaDeferred(t)
	ctx := context.Background()
	day := time.Now().In(cstZone).Format("2006-01-02")

	if err := f.q.Defer(ctx, deferredReqWithIntent("intent-replay"), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	rows, total, _ := f.drepo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if total != 1 {
		t.Fatalf("应 1 条 pending，得到 %d", total)
	}
	rec := rows[0]

	// 重发走原管线：快照带同 intent → 预占复用 → 发送成功 → commit
	pipeline := NewSendPipeline(SendPipelineConfig{
		Adapter:       &successCountingAdapter{},
		QuotaReserver: f.m,
		QuotaScopesFn: func(context.Context, *ReachSendRequest) []QuotaScopeDef { return quotaScopes() },
	})
	f.q.replayOne(ctx, pipeline, &rec)
	// 模拟调度器重复派发同一行（租约已转 sending，第二次应被状态机挡住或幂等）
	f.q.replayOne(ctx, pipeline, &rec)

	res, _ := f.qrepo.GetReservation(ctx, "intent-replay#customer:cust-1")
	if res == nil || res.State != model.QuotaStateCommitted {
		t.Fatalf("实际发送后必须 committed，得到 %+v", res)
	}
	if used := counterUsed(t, f, day); used != 1 {
		t.Fatalf("同一 intent 只允许消耗 1 次配额，得到 %d", used)
	}
}

// ③ committed 后再取消：预占不返额（已发生动作不得当未发生）。
func TestDeferredQueue_CancelAfterCommitKeepsQuota(t *testing.T) {
	f := setupQuotaDeferred(t)
	ctx := context.Background()
	day := time.Now().In(cstZone).Format("2006-01-02")

	if _, _, err := f.m.ReserveIntent(ctx, "intent-committed", "h", quotaScopes()); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := f.m.CommitIntent(ctx, "intent-committed", mustToken(t, f.m, "intent-committed")); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := f.q.Defer(ctx, deferredReqWithIntent("intent-committed"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	rows, total, _ := f.drepo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if total != 1 {
		t.Fatalf("应 1 条 pending，得到 %d", total)
	}
	_ = f.q.CancelDeferredOutbound(ctx, rows[0].ID)
	res, _ := f.qrepo.GetReservation(ctx, "intent-committed#customer:cust-1")
	if res == nil || res.State != model.QuotaStateCommitted {
		t.Fatalf("committed 预占不得被取消路径释放，得到 %+v", res)
	}
	if used := counterUsed(t, f, day); used != 1 {
		t.Fatalf("已发送的配额不得返还，得到 %d", used)
	}
}

func mustToken(t *testing.T, m *ReachQuotaManager, intent string) string {
	t.Helper()
	tok, _, err := m.ReserveIntent(context.Background(), intent, "h", quotaScopes())
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	return tok
}

// successCountingAdapter 恒成功适配器。
type successCountingAdapter struct{ calls int }

func (a *successCountingAdapter) Send(context.Context, *ReachSendRequest) (string, error) {
	a.calls++
	return "mh-gate", nil
}
