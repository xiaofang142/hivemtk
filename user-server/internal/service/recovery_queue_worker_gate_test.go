package service

// T-P3-07 AC① 的第二条非工具外发路径：cron（挽回队列 worker，enforce 轮）。
//
// 这条用例刻意做成**同轮对照**（与 T-P1-07 的 DNC 用例同一写法）：同一轮里一条被闸门拦下、
// 另一条正常发出。只断"被拦的那条没发"是不够的 —— 那句话在"worker 压根没跑"
// 与"外发链路接错了"两种情况下同样成立。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// selectiveReachGate 按判定键逐条放行/拦下。
type selectiveReachGate struct {
	allowed map[string]bool
	asks    []string
}

func (g *selectiveReachGate) CheckReachPreSend(_ context.Context, s ReachSubject) (bool, string) {
	g.asks = append(g.asks, s.Key)
	if g.allowed[s.Key] {
		return true, "whitelisted"
	}
	return false, "denied_default"
}

func TestRecoveryQueueWorker_ApprovalGateDeniedYieldsZeroOutbound(t *testing.T) {
	ctx := context.Background()

	oneIDDenied := "uid-gatew-denied-" + gateNonce
	oneIDOK := "uid-gatew-ok-" + gateNonce
	phoneDenied := "131" + gateNonce
	phoneOK := "130" + gateNonce

	spy := &reachSendSpy{}
	reach, db := newSpyReachService(t, spy)
	seed := func(id, oneID, phone string) {
		t.Helper()
		if err := db.Create(&model.Customer{ID: id, UnifiedID: oneID, Phone: phone}).Error; err != nil {
			t.Fatalf("建客户失败: %v", err)
		}
	}
	seed("rc-gatew-1", oneIDDenied, phoneDenied)
	seed("rc-gatew-2", oneIDOK, phoneOK)

	gate := &selectiveReachGate{allowed: map[string]bool{oneIDOK: true}}
	reach.SetPreSendApprovalChecker(gate)

	queue := NewRecoveryQueueServiceWithRepo(repository.NewRecoveryQueueRepositoryWithDB(db))
	for _, cid := range []string{"rc-gatew-1", "rc-gatew-2"} {
		if _, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{CustomerID: cid, Content: "老客回归立减 30"}); err != nil {
			t.Fatalf("入队: %v", err)
		}
	}

	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// 同轮对照：被放行的那条必须真的发出去。
	if len(spy.sms) != 1 || spy.sms[0] != phoneOK {
		t.Fatalf("放行的那条应正常外发（否则无法证明闸门起了作用）: %v", spy.sms)
	}
	if gateHas(spy.sms, phoneDenied) {
		t.Fatalf("被拦的那条不得外发: %v", spy.sms)
	}
	if report.BlockedByApproval != 1 || report.Sent != 1 {
		t.Fatalf("处置统计异常: %+v", report)
	}
	// 判定键必须是 one_id（AC②）：cron 路径上 req.AccountID 恒空，键若是它就等于没判。
	if len(gate.asks) != 2 || !gateHas(gate.asks, oneIDDenied) || !gateHas(gate.asks, oneIDOK) {
		t.Fatalf("闸门应各问一次、键为 one_id，实际 %v", gate.asks)
	}

	var deniedRow, okRow model.RecoveryQueue
	if err := db.Where("customer_id = ?", "rc-gatew-1").First(&deniedRow).Error; err != nil {
		t.Fatalf("回读被拦项: %v", err)
	}
	// 什么都没发出去 ⇒ 不消耗尝试次数（授权补上后还能发）、不改阶段、推离队首。
	if deniedRow.Attempts != 0 {
		t.Errorf("闸门拒发不该消耗尝试次数，实际 attempts=%d", deniedRow.Attempts)
	}
	if deniedRow.Stage != model.RecoveryStageQueued {
		t.Errorf("被拦项应留在 queued 等授权，实际 %s", deniedRow.Stage)
	}
	if deniedRow.NextAttemptAt == nil {
		t.Error("被拦项应被推离队首（否则下一轮又立刻撞它）")
	}
	// last_result 刻意不写：与冷却分支同一条既有口径（T-P1-07，见 recovery_queue_worker_test.go
	// 的 "冷却不该写 last_result"）。被拦的原因走日志与闸门自己的判定计数器，不塞进外发台账。
	if deniedRow.LastResult != "" {
		t.Errorf("闸门拒发什么都没发，不该写 last_result: %q", deniedRow.LastResult)
	}
	if err := db.Where("customer_id = ?", "rc-gatew-2").First(&okRow).Error; err != nil {
		t.Fatalf("回读放行项: %v", err)
	}
	if okRow.Attempts != 1 || okRow.LastChannel != "sms" {
		t.Errorf("放行项应记一次尝试: %+v", okRow)
	}
}

func gateHas(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
