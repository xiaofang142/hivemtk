package sop

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// FA-45：容量 1 的假队列实现 SOPDispatchSender + 可靠 Dispatch(ctx,task) error。
// 满载/停机返回错误，模拟队列拒收；dispatchReliable 会走带 error 的分支。
type fa45FakeSender struct {
	ch      chan *dispatchTask
	stopped bool
}

func (f *fa45FakeSender) DispatchOrLog(task *dispatchTask) {
	select {
	case f.ch <- task:
	default:
	}
}

func (f *fa45FakeSender) Dispatch(_ context.Context, task *dispatchTask) error {
	if f.stopped {
		return errors.New("dispatcher stopped")
	}
	select {
	case f.ch <- task:
		return nil
	default:
		return errors.New("queue full")
	}
}

func fa45Seed(t *testing.T) (*gorm.DB, *model.SOPTimer, *repository.SOPDispatchClaimRepository) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.SOPExecution{}, &model.SOPExecEvent{},
		&model.SOPTimer{}, &model.SOPDispatchClaim{},
	)
	ctx := context.Background()
	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "fa45", Status: SOPStatusRunning,
		CurrentNode: "wait_node", StartedAt: time.Now(),
	}
	if err := db.WithContext(ctx).Create(exec).Error; err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	timer := &model.SOPTimer{
		ExecutionID: exec.ID, NodeID: "wait_node", WaitEvent: WaitEventTimer,
		WaitUntil: time.Now().Add(-time.Second), Status: sopTimerStatusPending,
		Payload: model.JSONMap{},
	}
	if err := db.WithContext(ctx).Create(timer).Error; err != nil {
		t.Fatalf("seed timer: %v", err)
	}
	return db, timer, repository.NewSOPDispatchClaimRepository(db)
}

func fa45ReadTimer(t *testing.T, db *gorm.DB, id uint) model.SOPTimer {
	t.Helper()
	var row model.SOPTimer
	if err := db.First(&row, id).Error; err != nil {
		t.Fatalf("read timer: %v", err)
	}
	return row
}

// 队列满载时不得先标 fired：claim 留 pending、timer 仍 pending；排空后下一轮恢复派发+确认。
func TestFA45_QueueFullKeepsPendingThenRecovers(t *testing.T) {
	db, timer, claimRepo := fa45Seed(t)
	ctx := context.Background()
	sender := &fa45FakeSender{ch: make(chan *dispatchTask, 1)}
	sender.ch <- &dispatchTask{} // 占满容量
	outbox := NewSOPOutboxDispatcher(sender)

	outbox.processDueTimers(ctx)
	claim, err := claimRepo.GetByTimerID(ctx, timer.ID)
	if err != nil || claim == nil || claim.Status != model.SOPDispatchClaimPending {
		t.Fatalf("满载后 claim 应 pending: %+v err=%v", claim, err)
	}
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != sopTimerStatusPending {
		t.Fatalf("满载后 timer 仍应 pending，实际 %s", got)
	}
	if len(sender.ch) != 1 {
		t.Fatalf("满载不应新增入队, len=%d", len(sender.ch))
	}

	<-sender.ch // 排空
	outbox.processDueTimers(ctx)
	claim, _ = claimRepo.GetByTimerID(ctx, timer.ID)
	if claim == nil || claim.Status != model.SOPDispatchClaimConfirmed {
		t.Fatalf("恢复后 claim 应 confirmed: %+v", claim)
	}
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != "fired" {
		t.Fatalf("恢复后 timer 应 fired，实际 %s", got)
	}
	if len(sender.ch) != 1 {
		t.Fatalf("恢复后应恰好入队一次, len=%d", len(sender.ch))
	}
}

// 停机（Dispatch 返回 stopped）同样不先标 fired；恢复后重派。
func TestFA45_StoppedDispatcherRecovers(t *testing.T) {
	db, timer, claimRepo := fa45Seed(t)
	ctx := context.Background()
	sender := &fa45FakeSender{ch: make(chan *dispatchTask, 1), stopped: true}
	outbox := NewSOPOutboxDispatcher(sender)

	outbox.processDueTimers(ctx)
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != sopTimerStatusPending {
		t.Fatalf("停机时 timer 仍应 pending，实际 %s", got)
	}
	claim, _ := claimRepo.GetByTimerID(ctx, timer.ID)
	if claim == nil || claim.Status != model.SOPDispatchClaimPending {
		t.Fatalf("停机时 claim 应 pending: %+v", claim)
	}

	sender.stopped = false
	outbox.processDueTimers(ctx)
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != "fired" {
		t.Fatalf("恢复后 timer 应 fired，实际 %s", got)
	}
	if len(sender.ch) != 1 {
		t.Fatalf("恢复后应恰好入队一次, len=%d", len(sender.ch))
	}
}

// confirm 后、改 timer 状态前崩溃：新实例只见 confirmed claim，补状态不重派。
func TestFA45_ConfirmedClaimCrashRecoveryDoesNotRedispatch(t *testing.T) {
	db, timer, claimRepo := fa45Seed(t)
	ctx := context.Background()
	claim := &model.SOPDispatchClaim{
		TimerID: timer.ID, ExecutionID: timer.ExecutionID, NodeID: timer.NodeID,
		Kind: model.SOPDispatchKindWake, Status: model.SOPDispatchClaimConfirmed,
		LeaseUntil: time.Now().Add(time.Hour), TenantID: 1,
	}
	if _, err := claimRepo.Acquire(ctx, claim); err != nil {
		t.Fatalf("seed confirmed claim: %v", err)
	}

	sender := &fa45FakeSender{ch: make(chan *dispatchTask, 1)}
	outbox := NewSOPOutboxDispatcher(sender) // 模拟新实例
	outbox.processDueTimers(ctx)
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != "fired" {
		t.Fatalf("崩溃恢复应补 fired，实际 %s", got)
	}
	if len(sender.ch) != 0 {
		t.Fatalf("confirmed claim 不得重派, len=%d", len(sender.ch))
	}
}

// 幂等：多轮轮询同一 timer 只成功入队一次。
func TestFA45_IdempotentAcrossTicks(t *testing.T) {
	db, timer, _ := fa45Seed(t)
	ctx := context.Background()
	sender := &fa45FakeSender{ch: make(chan *dispatchTask, 2)}
	outbox := NewSOPOutboxDispatcher(sender)
	for i := 0; i < 4; i++ {
		outbox.processDueTimers(ctx)
	}
	if got := fa45ReadTimer(t, db, timer.ID).Status; got != "fired" {
		t.Fatalf("timer 应 fired，实际 %s", got)
	}
	if len(sender.ch) != 1 {
		t.Fatalf("同一 timer 只应入队一次, len=%d", len(sender.ch))
	}
}

// wait 超时跳过路径：先认领可靠投递，成功才转 skipped（不先改状态）。
func TestFA45_MaxWaitSkipUsesDurableClaim(t *testing.T) {
	db, timer, claimRepo := fa45Seed(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	if err := db.Model(&model.SOPTimer{}).Where("id = ?", timer.ID).
		Update("max_wait_at", past).Error; err != nil {
		t.Fatalf("set max_wait_at: %v", err)
	}
	sender := &fa45FakeSender{ch: make(chan *dispatchTask, 1)}
	outbox := NewSOPOutboxDispatcher(sender)
	outbox.processDueTimers(ctx)

	if got := fa45ReadTimer(t, db, timer.ID).Status; got != sopTimerStatusSkipped {
		t.Fatalf("超时 timer 应 skipped，实际 %s", got)
	}
	claim, _ := claimRepo.GetByTimerID(ctx, timer.ID)
	if claim == nil || claim.Status != model.SOPDispatchClaimConfirmed || claim.Kind != model.SOPDispatchKindSkip {
		t.Fatalf("skip claim 应 confirmed/kind=skip: %+v", claim)
	}
	if len(sender.ch) != 1 {
		t.Fatalf("skip 任务应入队一次, len=%d", len(sender.ch))
	}
	if task := <-sender.ch; !task.SkipWait {
		t.Fatalf("skip 任务应带 SkipWait=true: %+v", task)
	}
}
