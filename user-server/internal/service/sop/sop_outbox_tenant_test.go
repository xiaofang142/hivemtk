package sop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 本文件钉住 SOP 域两条后台轮询的多租户 fail-closed 残留（TENANT_MODE=on 生产日志实证）：
//   - `[outbox] query due timers failed`（sop_timers，SOPOutboxDispatcher.processDueTimers）
//   - `[stuck] query stuck executions failed`（sop_executions，SOPStuckDetector.scanStuckExecutions）
//
// 两处都用 **裸 ctx（无租户绑定）** 直连租户表 ⇒ on 模式被隔离 callback fail-closed 拒绝。
// 修法是 tenant.Fanout（逐活跃租户注入 ctx），不是 system scope —— 点火/恢复之后都是
// 业务派发（写 sop_executions、发消息），system scope 会让下游 Create 落列默认值 1 = 串租户。
//
// 判据形状与 sop_scheduler_tenant_test.go 同源：以生产同款的 context.Background() 入口
// 调度，只有 Fanout 生效（而非 fail-closed 吞掉）才会看到"timer 被点火 / 卡死被恢复"。

// seedOnModeTenantEnv 设 on 模式 + 活跃租户枚举器（单租户 11），并保证用例间复位。
func seedOnModeTenantEnv(t *testing.T, ids []uint) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) {
		return ids, nil
	})
	t.Cleanup(func() {
		// activeTenantLoader 是进程级 atomic.Value，用例间必须复位（见 fanout.go 注释）。
		tenant.SetActiveTenantLoader(nil)
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
}

// TestSOPOutboxDispatcher_ProcessDueTimers_FansOutPerTenant 钉住 outbox 轮询：
// on 模式下裸 ctx 入口必须经 Fanout 扫到期 timer，并按行自证把 tenant_id 带进派发任务。
func TestSOPOutboxDispatcher_ProcessDueTimers_FansOutPerTenant(t *testing.T) {
	seedOnModeTenantEnv(t, []uint{11})

	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)
	ctx11 := tenant.WithTenantID(context.Background(), 11)

	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c-fanout", Status: SOPStatusRunning,
		CurrentNode: "wait_node", StartedAt: time.Now(),
	}
	if err := db.WithContext(ctx11).Create(exec).Error; err != nil {
		t.Fatalf("以租户 11 seed execution 失败: %v", err)
	}

	timer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait_node",
		WaitEvent:   WaitEventTimer,
		WaitUntil:   time.Now().Add(-1 * time.Second),
		Status:      "pending",
	}
	if err := db.WithContext(ctx11).Create(timer).Error; err != nil {
		t.Fatalf("以租户 11 seed timer 失败: %v", err)
	}

	var gotTenant uint
	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
			gotTenant = task.TenantID
		},
	}

	outbox := NewSOPOutboxDispatcher(nil)
	outbox.execDispatcher = mockDispatcher
	// 生产同款入口：loop 传的是 context.Background()（无租户）。
	outbox.processDueTimers(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 1 {
		t.Fatalf("dispatchedCount=%d want=1（若为 0，多半是 [outbox] query due timers failed 被 fail-closed 吞掉，Fanout 未生效）", dispatchedCount)
	}
	if gotTenant != 11 {
		t.Errorf("派发任务 TenantID=%d want=11（行自证 T027）", gotTenant)
	}

	var updated model.SOPTimer
	if err := db.WithContext(tenant.WithTenantID(context.Background(), 11)).
		First(&updated, timer.ID).Error; err != nil {
		t.Fatalf("读回 timer 失败（若为 [tenant] fail-closed 错误即 Fanout 未生效）: %v", err)
	}
	if updated.Status != "fired" {
		t.Errorf("timer status=%s want=fired", updated.Status)
	}
}

// TestSOPStuckDetector_Scan_FansOutPerTenant 钉住卡死扫描：on 模式下裸 ctx 入口必须经
// Fanout 扫 sop_executions，且恢复派发的 dispatchTask 必须带行上 tenant_id。
func TestSOPStuckDetector_Scan_FansOutPerTenant(t *testing.T) {
	seedOnModeTenantEnv(t, []uint{11})

	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)
	ctx11 := tenant.WithTenantID(context.Background(), 11)

	oldEvent := time.Now().Add(-2 * time.Hour)
	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c-stuck", Status: SOPStatusRunning,
		CurrentNode: "stuck_node", StartedAt: time.Now().Add(-48 * time.Hour),
		LastEventAt: &oldEvent, UpdatedAt: oldEvent,
	}
	if err := db.WithContext(ctx11).Create(exec).Error; err != nil {
		t.Fatalf("以租户 11 seed execution 失败: %v", err)
	}

	var gotTenant uint
	var dispatchedCount int32
	mockDispatcher := &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) {
			atomic.AddInt32(&dispatchedCount, 1)
			gotTenant = task.TenantID
		},
	}

	detector := NewSOPStuckDetector(nil)
	detector.execDispatcher = mockDispatcher
	detector.SetMaxIdleTime(context.Background(), 30*time.Minute)
	// 生产同款入口：loop 传的是 context.Background()（无租户）。
	detector.scanStuckExecutions(context.Background())

	if atomic.LoadInt32(&dispatchedCount) != 1 {
		t.Fatalf("dispatchedCount=%d want=1（若为 0，多半是 [stuck] query stuck executions failed 被 fail-closed 吞掉，Fanout 未生效）", dispatchedCount)
	}
	if gotTenant != 11 {
		t.Errorf("恢复派发任务 TenantID=%d want=11（行自证 T027；漏填会让 worker 的 load execution 被 fail-closed 吞掉）", gotTenant)
	}
}

// TestSOPStuckDetector_Scan_OnModeSkipsInactiveTenant 钉住"只扫活跃租户"的隔离面：
// 租户 12 不在活跃列表里 ⇒ 它的卡死行不会被本轮恢复（不产生跨租户读/派发）。
func TestSOPStuckDetector_Scan_OnModeSkipsInactiveTenant(t *testing.T) {
	seedOnModeTenantEnv(t, []uint{11})

	db := testutil.NewTestDB(t,
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
	)
	ctx12 := tenant.WithTenantID(context.Background(), 12)

	oldEvent := time.Now().Add(-2 * time.Hour)
	exec := &model.SOPExecution{
		SOPID: 1, CustomerID: "c-12", Status: SOPStatusRunning,
		CurrentNode: "stuck_node", StartedAt: time.Now().Add(-48 * time.Hour),
		LastEventAt: &oldEvent, UpdatedAt: oldEvent,
	}
	if err := db.WithContext(ctx12).Create(exec).Error; err != nil {
		t.Fatalf("以租户 12 seed execution 失败: %v", err)
	}

	var dispatchedCount int32
	detector := NewSOPStuckDetector(nil)
	detector.execDispatcher = &mockExecDispatcher{
		dispatchFn: func(task *dispatchTask) { atomic.AddInt32(&dispatchedCount, 1) },
	}
	detector.SetMaxIdleTime(context.Background(), 30*time.Minute)
	detector.scanStuckExecutions(context.Background())

	if n := atomic.LoadInt32(&dispatchedCount); n != 0 {
		t.Errorf("dispatchedCount=%d want=0（非活跃租户的行不得被扇出扫描触碰）", n)
	}
}
