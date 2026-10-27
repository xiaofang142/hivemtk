package egolite

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 执行域状态机测试（真 PG；testutil 按 CI 纪律不可达即 Fatal/Skip 同族行为）。

func newExecTestEnv(t *testing.T) (EgoExecutionRepository, EgoEventRepository, EgoFlowRepository, EgoAssetRepository, *EgoRunnerRegistry, *EgoExecutionService) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.EgoliteExecution{}, &model.EgoEventLog{}, &model.EgoliteFlow{}, &model.EgoliteWriteClaim{},
		&model.EgoliteAsset{},
	)
	if db == nil {
		t.Fatal("测试库不可用")
	}
	execRepo := NewEgoExecutionRepositoryWithDB(db)
	evRepo := NewEgoEventRepositoryWithDB(db)
	flowRepo := NewEgoFlowRepositoryWithDB(db)
	assetRepo := NewEgoAssetRepositoryWithDB(db)
	registry := NewEgoRunnerRegistry()
	svc := NewEgoExecutionService(execRepo, evRepo, flowRepo, assetRepo, registry)
	return execRepo, evRepo, flowRepo, assetRepo, registry, svc
}

func TestExecutionStateTransitionAtomic(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, _ := newExecTestEnv(t)

	e := &model.EgoliteExecution{UserID: 1, Origin: "draft", State: model.EgoExecCreated}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}

	// 合法迁移：created → dispatching
	if err := execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecCreated}, model.EgoExecDispatching,
		map[string]any{"dispatched_at": time.Now()}); err != nil {
		t.Fatalf("合法迁移失败: %v", err)
	}

	// 非法迁移：created 态已不在 from 集合 → ErrRecordNotFound
	err := execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecCreated}, model.EgoExecRunning, nil)
	if err == nil {
		t.Fatal("非法迁移必须失败（单语句乐观迁移语义）")
	}

	// 终态收敛：dispatching → timeout
	if err := execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecTimeout,
		map[string]any{"ended_at": time.Now(), "fail_reason": "硬超时"}); err != nil {
		t.Fatalf("终态迁移失败: %v", err)
	}
	got, _ := execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecTimeout {
		t.Fatalf("终态不符: %s", got.State)
	}
}

func TestConcurrencyGate(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, registry, svc := newExecTestEnv(t)

	// 模拟两个在途
	for i := 0; i < 2; i++ {
		e := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecRunning}
		if err := execRepo.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// runner 不在线 → Start 落 created（不走并发闸之后的 dispatch 失败路径，
	// 但并发闸先查：2 个在途 ≥ 上限 2 → 拒绝）
	_, err := svc.Start(ctx, 7, StartRequest{Origin: "draft", FlowID: 1})
	if err != ErrConcurrencyGate {
		t.Fatalf("并发闸应拒绝（在途=2），got %v", err)
	}
	_ = registry
}

func TestStartValidation(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, svc := newExecTestEnv(t)

	cases := []struct {
		name string
		req  StartRequest
	}{
		{"draft 缺 flow_id", StartRequest{Origin: "draft"}},
		{"market 缺 entry", StartRequest{Origin: "market", AssetID: "x"}},
		{"非法 origin", StartRequest{Origin: "evil", FlowID: 1}},
		{"非法 confirm_policy", StartRequest{Origin: "draft", FlowID: 1, ConfirmPolicy: "maybe"}},
	}
	for _, tc := range cases {
		if _, err := svc.Start(ctx, 1, tc.req); err == nil {
			t.Errorf("[%s] 应拒绝", tc.name)
		}
	}
}

func TestIngestEventAdvancesStateMachine(t *testing.T) {
	ctx := context.Background()
	execRepo, evRepo, flowRepo, _, registry, svc := newExecTestEnv(t)

	// flow + execution 落库
	f := &model.EgoliteFlow{UserID: 3, Name: "t", DomainsJSON: []byte(`["github.com"]`), Script: "console.log(1)", Status: model.EgoFlowDraft}
	if err := flowRepo.Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	e := &model.EgoliteExecution{UserID: 3, FlowID: f.ID, Origin: "draft", State: model.EgoExecDispatching, TimeoutSec: 1800}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}

	// action 事件 → running
	if err := svc.IngestEvent(ctx, 3, RunnerEvent{
		Seq: 1, Type: "action", ExecutionID: EgoExecKey(e.ID),
		Payload: []byte(`{"action":"goto","ok":true}`),
	}); err != nil {
		t.Fatalf("ingest action 失败: %v", err)
	}
	got, _ := execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecRunning {
		t.Fatalf("action 事件后应 running，got %s", got.State)
	}

	// confirm 事件 → waiting_confirm
	if err := svc.IngestEvent(ctx, 3, RunnerEvent{
		Seq: 2, Type: "confirm", ExecutionID: EgoExecKey(e.ID),
		Payload: []byte(`{"action":"post_comment"}`),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecWaitingConfirm {
		t.Fatalf("confirm 事件后应 waiting_confirm，got %s", got.State)
	}

	// confirm TTL 内 allow → running
	if _, err := svc.Confirm(ctx, 3, e.ID, true); err != nil {
		t.Fatalf("confirm 失败: %v", err)
	}
	got, _ = execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecRunning {
		t.Fatalf("allow 后应 running，got %s", got.State)
	}

	// done(exit 0) → completed；事件落库含 Ok 三态
	if err := svc.IngestEvent(ctx, 3, RunnerEvent{
		Seq: 3, Type: "done", ExecutionID: EgoExecKey(e.ID), ExitCode: 0,
		Payload: []byte(`{"summary":"ok"}`),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecCompleted {
		t.Fatalf("done 后应 completed，got %s", got.State)
	}
	events, _ := evRepo.ListByExecution(ctx, e.ID, 100)
	if len(events) != 3 {
		t.Fatalf("事件应落 3 条，got %d", len(events))
	}
	if events[0].Ok == nil || !*events[0].Ok {
		t.Fatalf("action 事件的 ok 三态应继承 payload（true）")
	}
	_ = registry
}

func TestIngestEventOwnershipCheck(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)
	e := &model.EgoliteExecution{UserID: 5, Origin: "draft", State: model.EgoExecRunning}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	// 别的用户的事件帧 → 拒绝（归属校验，防 runner 越权注入）
	err := svc.IngestEvent(ctx, 99, RunnerEvent{
		Seq: 1, Type: "done", ExecutionID: EgoExecKey(e.ID), ExitCode: 0,
	})
	if err != ErrExecutionNotFound {
		t.Fatalf("越权事件帧应 ErrExecutionNotFound，got %v", err)
	}
	// 非法 execKey 同响应
	err = svc.IngestEvent(ctx, 5, RunnerEvent{Seq: 1, Type: "done", ExecutionID: "evil-1"})
	if err != ErrExecutionNotFound {
		t.Fatalf("非法 execKey 应 ErrExecutionNotFound，got %v", err)
	}
}

func TestAbortAndDenyFlow(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)

	e := &model.EgoliteExecution{UserID: 9, Origin: "draft", State: model.EgoExecWaitingConfirm}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	// deny → stopped 终态
	if _, err := svc.Confirm(ctx, 9, e.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := execRepo.GetByID(ctx, e.ID)
	if got.State != model.EgoExecStopped {
		t.Fatalf("deny 后应 stopped，got %s", got.State)
	}
	// 对终态再 abort → 冲突
	if _, err := svc.Abort(ctx, 9, e.ID, "again"); err != ErrExecStateConflict {
		t.Fatalf("终态 abort 应冲突，got %v", err)
	}
}

func TestWatchdogSweepsConfirmTTLAndTimeout(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)

	// waiting_confirm 且 updated_at 已老于 TTL（ConfirmTTLSec=1s，updated_at 由 Create 写 now——
	// 直接手工把 updated_at 拨老）
	eWait := &model.EgoliteExecution{UserID: 11, Origin: "draft", State: model.EgoExecWaitingConfirm, ConfirmTTLSec: 1}
	if err := execRepo.Create(ctx, eWait); err != nil {
		t.Fatal(err)
	}
	execRepo.(*egoExecutionRepo).db.Exec(
		`UPDATE egolite_executions SET updated_at = now() - interval '10 seconds' WHERE id = ?`, eWait.ID)

	// running 且 dispatched_at 老
	eRun := &model.EgoliteExecution{UserID: 11, Origin: "draft", State: model.EgoExecRunning, TimeoutSec: 1800}
	if err := execRepo.Create(ctx, eRun); err != nil {
		t.Fatal(err)
	}
	execRepo.(*egoExecutionRepo).db.Exec(
		`UPDATE egolite_executions SET dispatched_at = now() - interval '2000 seconds' WHERE id = ?`, eRun.ID)

	swept, err := svc.WatchdogSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 2 {
		t.Fatalf("看门狗应收口 2 条（confirm TTL + 硬超时），got %d", swept)
	}
	got1, _ := execRepo.GetByID(ctx, eWait.ID)
	if got1.State != model.EgoExecStopped {
		t.Fatalf("confirm 超时应 stopped（fail-close），got %s", got1.State)
	}
	got2, _ := execRepo.GetByID(ctx, eRun.ID)
	if got2.State != model.EgoExecTimeout {
		t.Fatalf("硬超时应 timeout，got %s", got2.State)
	}
}

func TestRunnerDisconnectFailsActive(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)
	for _, st := range []string{model.EgoExecRunning, model.EgoExecDispatching, model.EgoExecCompleted} {
		e := &model.EgoliteExecution{UserID: 13, Origin: "draft", State: st}
		if err := execRepo.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	svc.OnRunnerDisconnect(ctx, 13)
	got, _, _ := execRepo.ListByUser(ctx, 13, 1, 10)
	states := map[string]int{}
	for _, e := range got {
		states[e.State]++
	}
	if states[model.EgoExecFailed] != 2 {
		t.Fatalf("断连应把 running/dispatching 判 failed，got %v", states)
	}
	if states[model.EgoExecCompleted] != 1 {
		t.Fatalf("终态不得被断连改写，got %v", states)
	}
}

func TestWriteClaimLifecycle(t *testing.T) {
	ctx := context.Background()
	_ = testutil.NewTestDB(t, &model.EgoliteWriteClaim{})
	// 直接建 repo
	db := testutil.NewTestDB(t, &model.EgoliteWriteClaim{})
	repo := NewEgoWriteClaimRepositoryWithDB(db)

	c := &model.EgoliteWriteClaim{ExecutionID: 1, UserID: 1, Action: "post_comment"}
	ok, err := repo.Claim(ctx, c)
	if err != nil || !ok {
		t.Fatalf("首占应成功: ok=%v err=%v", ok, err)
	}
	ok2, _ := repo.Claim(ctx, &model.EgoliteWriteClaim{ExecutionID: 1, UserID: 1, Action: "post_comment"})
	if ok2 {
		t.Fatal("重复占坑必须失败（双发闸语义）")
	}
	// 不同 action 是不同的坑
	ok3, _ := repo.Claim(ctx, &model.EgoliteWriteClaim{ExecutionID: 1, UserID: 1, Action: "send_msg"})
	if !ok3 {
		t.Fatal("不同 action 应各自占坑")
	}
	// 状态机：claimed → sent → verified
	list, _ := repo.ListByExecution(ctx, 1)
	var target uint
	for _, w := range list {
		if w.Action == "post_comment" {
			target = w.ID
		}
	}
	if err := repo.MarkSent(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkVerified(ctx, target); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListByExecution(ctx, 1)
	for _, w := range list {
		if w.ID == target && w.ClaimState != model.EgoWriteVerified {
			t.Fatalf("verified 迁移失败: %s", w.ClaimState)
		}
	}
}
