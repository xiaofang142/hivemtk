package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSOPRetryPolicy_Backoff(t *testing.T) {
	p := DefaultSOPRetryPolicy()
	if d := p.Backoff(context.Background(), 1); d != 1*time.Second {
		t.Errorf("Backoff(1)=%v want=1s", d)
	}
	if d := p.Backoff(context.Background(), 2); d != 2*time.Second {
		t.Errorf("Backoff(2)=%v want=2s", d)
	}
	if d := p.Backoff(context.Background(), 3); d != 4*time.Second {
		t.Errorf("Backoff(3)=%v want=4s", d)
	}
}

func TestSOPRetryPolicy_BackoffCappedAtMax(t *testing.T) {
	p := &SOPRetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		Multiplier:     2.0,
	}
	if d := p.Backoff(context.Background(), 10); d != 5*time.Second {
		t.Errorf("Backoff(10)=%v want=5s (capped)", d)
	}
}

func TestSOPRetryPolicy_BackoffAttemptZeroOrNegative(t *testing.T) {
	p := DefaultSOPRetryPolicy()
	if d := p.Backoff(context.Background(), 0); d != 1*time.Second {
		t.Errorf("Backoff(0)=%v want=1s", d)
	}
	if d := p.Backoff(context.Background(), -1); d != 1*time.Second {
		t.Errorf("Backoff(-1)=%v want=1s", d)
	}
}

func TestSOPExecutionDispatcher_DispatchQueued(t *testing.T) {
	d := &SOPExecutionDispatcher{
		dispatchQueue: make(chan *dispatchTask, 10),
		stopCh:        make(chan struct{}),
		retryPolicy:   DefaultSOPRetryPolicy(),
	}
	task := &dispatchTask{ExecutionID: 1, NodeID: "n1", Attempt: 0, TraceID: "t1"}
	if err := d.Dispatch(context.Background(), task); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if len(d.dispatchQueue) != 1 {
		t.Errorf("queue len=%d want=1", len(d.dispatchQueue))
	}
}

func TestSOPExecutionDispatcher_DispatchQueueFull(t *testing.T) {
	d := &SOPExecutionDispatcher{
		dispatchQueue: make(chan *dispatchTask, 1),
		stopCh:        make(chan struct{}),
		retryPolicy:   DefaultSOPRetryPolicy(),
	}
	d.dispatchQueue <- &dispatchTask{ExecutionID: 1, NodeID: "n1"}
	err := d.Dispatch(context.Background(), &dispatchTask{ExecutionID: 2, NodeID: "n2"})
	if err == nil {
		t.Error("expected error when queue full")
	}
}

func TestSOPExecutionDispatcher_DispatchAfterStop(t *testing.T) {
	d := &SOPExecutionDispatcher{
		dispatchQueue: make(chan *dispatchTask, 10),
		stopCh:        make(chan struct{}),
		retryPolicy:   DefaultSOPRetryPolicy(),
	}
	close(d.stopCh)
	err := d.Dispatch(context.Background(), &dispatchTask{ExecutionID: 1, NodeID: "n1"})
	if err == nil {
		t.Error("expected error after stop")
	}
}

func TestSOPExecutionDispatcher_DispatchOrLogNoPanic(t *testing.T) {
	d := &SOPExecutionDispatcher{
		dispatchQueue: make(chan *dispatchTask),
		stopCh:        make(chan struct{}),
		retryPolicy:   DefaultSOPRetryPolicy(),
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("DispatchOrLog panicked: %v", r)
		}
	}()
	d.DispatchOrLog(&dispatchTask{ExecutionID: 1, NodeID: "n1"})
}

func TestSOPOutboxDispatcher_ProcessDueTimers_FiresPendingTimers(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
		&model.SOPOutbox{},
	)

	exec := &model.SOPExecution{
		SOPID:       1,
		CustomerID:  "cust_001",
		Status:      SOPStatusRunning,
		CurrentNode: "wait_node",
		StartedAt:   time.Now(),
	}
	if err := db.Create(exec).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}

	timer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait_node",
		WaitEvent:   WaitEventTimer,
		WaitUntil:   time.Now().Add(-1 * time.Second),
		Status:      "pending",
	}
	if err := db.Create(timer).Error; err != nil {
		t.Fatalf("create timer: %v", err)
	}

	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
			if task.ExecutionID != exec.ID {
				t.Errorf("task.ExecutionID=%d want=%d", task.ExecutionID, exec.ID)
			}
			if task.NodeID != "wait_node" {
				t.Errorf("task.NodeID=%s want=wait_node", task.NodeID)
			}
		},
	}

	outbox := NewSOPOutboxDispatcher(db, nil)
	outbox.execDispatcher = mockDispatcher
	outbox.processDueTimers(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 1 {
		t.Errorf("dispatchedCount=%d want=1", dispatchedCount)
	}

	var updated model.SOPTimer
	db.First(&updated, timer.ID)
	if updated.Status != "fired" {
		t.Errorf("timer status=%s want=fired", updated.Status)
	}
	if updated.FiredAt == nil {
		t.Error("timer FiredAt should not be nil")
	}
}

func TestSOPOutboxDispatcher_ProcessDueTimers_SkipsFutureTimers(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPTimer{},
	)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning,
		CurrentNode: "wait", StartedAt: time.Now(),
	}
	db.Create(exec)

	futureTimer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait",
		WaitEvent:   WaitEventTimer,
		WaitUntil:   time.Now().Add(1 * time.Hour),
		Status:      "pending",
	}
	db.Create(futureTimer)

	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
		},
	}
	outbox := NewSOPOutboxDispatcher(db, nil)
	outbox.execDispatcher = mockDispatcher
	outbox.processDueTimers(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 0 {
		t.Errorf("dispatchedCount=%d want=0 (future timer should not fire)", dispatchedCount)
	}

	var updated model.SOPTimer
	db.First(&updated, futureTimer.ID)
	if updated.Status != "pending" {
		t.Errorf("future timer status=%s want=pending", updated.Status)
	}
}

func TestSOPOutboxDispatcher_ProcessDueTimers_MultiInstanceIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPTimer{},
	)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning,
		CurrentNode: "wait", StartedAt: time.Now(),
	}
	db.Create(exec)

	timer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait",
		WaitEvent:   WaitEventTimer,
		WaitUntil:   time.Now().Add(-1 * time.Second),
		Status:      "pending",
	}
	db.Create(timer)

	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o := NewSOPOutboxDispatcher(db, nil)
			o.execDispatcher = mockDispatcher
			o.processDueTimers(context.Background())
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&dispatchedCount) != 1 {
		t.Errorf("dispatchedCount=%d want=1 (multi-instance idempotent)", dispatchedCount)
	}
}

func TestSOPStuckDetector_ScanSkipsRunningExecution(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)

	now := time.Now()
	recentEvent := now.Add(-1 * time.Minute)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning,
		CurrentNode: "n1", StartedAt: now.Add(-1 * time.Hour),
		LastEventAt: &recentEvent,
	}
	db.Create(exec)

	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
		},
	}

	detector := NewSOPStuckDetector(db, nil)
	detector.execDispatcher = mockDispatcher
	detector.SetMaxIdleTime(context.Background(), 30*time.Minute)
	detector.scanStuckExecutions(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 0 {
		t.Errorf("dispatchedCount=%d want=0 (recent event, not stuck)", dispatchedCount)
	}
}

func TestSOPStuckDetector_ScanSkipsWaitNodeWithPendingTimer(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)

	now := time.Now()
	oldEvent := now.Add(-2 * time.Hour)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning,
		CurrentNode: "wait", StartedAt: now.Add(-48 * time.Hour),
		LastEventAt: &oldEvent,
		WaitEvent:   WaitEventCustomerReply,
	}
	db.Create(exec)

	timer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait",
		WaitEvent:   WaitEventCustomerReply,
		WaitUntil:   now.Add(20 * time.Hour),
		Status:      "pending",
	}
	db.Create(timer)

	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
		},
	}

	detector := NewSOPStuckDetector(db, nil)
	detector.execDispatcher = mockDispatcher
	detector.SetMaxIdleTime(context.Background(), 30*time.Minute)
	detector.scanStuckExecutions(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 0 {
		t.Errorf("dispatchedCount=%d want=0 (wait node with pending timer not stuck)", dispatchedCount)
	}
}

func TestSOPStuckDetector_ScanRecoversTrulyStuckExecution(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)

	now := time.Now()
	oldEvent := now.Add(-2 * time.Hour)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning,
		CurrentNode: "stuck_node", StartedAt: now.Add(-48 * time.Hour),
		LastEventAt: &oldEvent,
		UpdatedAt:   oldEvent,
	}
	db.Create(exec)

	var dispatchedCount int32
	var dispatchedNodeID string
	var dispatchedExecID uint
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
			dispatchedNodeID = task.NodeID
			dispatchedExecID = task.ExecutionID
		},
	}

	detector := NewSOPStuckDetector(db, nil)
	detector.execDispatcher = mockDispatcher
	detector.SetMaxIdleTime(context.Background(), 30*time.Minute)
	detector.scanStuckExecutions(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 1 {
		t.Fatalf("dispatchedCount=%d want=1 (stuck execution should be recovered)", dispatchedCount)
	}
	if dispatchedNodeID != "stuck_node" {
		t.Errorf("dispatched NodeID=%s want=stuck_node", dispatchedNodeID)
	}
	if dispatchedExecID != exec.ID {
		t.Errorf("dispatched ExecutionID=%d want=%d", dispatchedExecID, exec.ID)
	}
}

type mockExecDispatcher struct {
	dispatchFn func(task *dispatchTask)
}

func (m *mockExecDispatcher) DispatchOrLog(task *dispatchTask) {
	if m.dispatchFn != nil {
		m.dispatchFn(task)
	}
}

var _ = context.Background
