// recovery_worker_wiring_test.go T-P1-07：挽回队列消费 worker 的装配层实跑。
//
// 装配层单独设测的理由与 T-P0-07 同一教训：判定 A 的病灶是"service 侧单测全绿、
// 生产装配点却根本没人调用"。只测 RecoveryQueueWorker 自身的逻辑测不到这一层——
// 必须有人真的调 InitRecoveryWorker 并断言它启动/不启动。
package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// db 为 nil 时不装配（与 InitAgentCheckpointStore 同一口径：宁可没有消费者，
// 也不装一个会在半路上抛 nil 指针的 worker）。
func TestInitRecoveryWorker_NilDBReturnsNil(t *testing.T) {
	if got := InitRecoveryWorker(nil); got != nil {
		t.Fatalf("db=nil 不应装配，got %v", got.Mode())
	}
}

// AC③：开关 off（默认）时 worker 不启动——Running 为 false，且 Stop 可重入。
func TestInitRecoveryWorker_OffModeDoesNotStart(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv(service.RecoveryWorkerFlagEnv, "off")
	w := InitRecoveryWorker(db)
	if w == nil {
		t.Fatal("db 非 nil 时应返回 worker（off 也要返回，供 Mode() 观测）")
	}
	defer w.Stop(context.Background())
	if w.Running() {
		t.Error("off 模式下 worker 不应运行")
	}
	if w.Mode() != service.RecoveryWorkerModeOff {
		t.Errorf("Mode=%s，期望 off", w.Mode())
	}
}

// shadow/enforce 才真的起 goroutine；Stop 必须幂等，否则优雅关停会卡或 panic。
func TestInitRecoveryWorker_ShadowModeStartsAndStops(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv(service.RecoveryWorkerFlagEnv, "shadow")
	t.Setenv(service.RecoveryWorkerIntervalEnv, "1h")
	w := InitRecoveryWorker(db)
	if w == nil {
		t.Fatal("db 非 nil 时应返回 worker")
	}
	if !w.Running() {
		t.Error("shadow 模式下 worker 应已启动（shadow 无外发副作用，只跑 DryRun）")
	}
	w.Stop(context.Background())
	w.Stop(context.Background())
	if w.Running() {
		t.Error("Stop 后不应仍在运行")
	}
}

// ---- MD-02 生命周期行为断言：缺必需审批依赖拒启，不静默裸发 ----

// TestInitRecoveryWorker_RefusesWhenApprovalDependencyUnmet FF_LTC_REACH_GATE 开着但
// W-1 裁决来源没接线时拒启：返回 nil、不起协程、闸门不装（AttachedServices 不涨）。
// 这一格把"配置关闭"与"依赖缺失"分开：off 档照常装配（见上用例），只有旗子开着而
// checker 没接才拒启 —— 拒启前的那一行若漏掉，挽回队列每轮都会走裸外发路径。
func TestInitRecoveryWorker_RefusesWhenApprovalDependencyUnmet(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "off") // W-1 没接线：applyApprovalGate 的 off 分支把 checker 清零
	t.Setenv(ReachGateFlagEnv, "shadow")
	t.Setenv(service.RecoveryWorkerFlagEnv, "shadow") // 门若误装就会真起协程
	t.Setenv(service.RecoveryWorkerIntervalEnv, "1h")

	db := testutil.NewTestDB(t)
	before, _ := GetReachGateSnapshot()
	w := InitRecoveryWorker(db)
	if w != nil {
		w.Stop(context.Background())
		t.Fatalf("审批依赖缺失应拒启（返回 nil，不静默裸发），got mode=%s", w.Mode())
	}
	after, _ := GetReachGateSnapshot()
	if after.AttachedServices != before.AttachedServices {
		t.Errorf("拒启不应装上外发闸门：AttachedServices %d→%d", before.AttachedServices, after.AttachedServices)
	}
	if !after.DependencyUnmet {
		t.Error("前置不成立：快照未报 DependencyUnmet，拒启分支根本没被走到")
	}
}

// TestInitRecoveryWorker_DepsCompleteStartsPerFlag 依赖齐全时按旗子原档启动，
// Stop 幂等（优雅关停会调两次），重复 Init 后新实例照常起。
func TestInitRecoveryWorker_DepsCompleteStartsPerFlag(t *testing.T) {
	restoreApprovalState(t)
	restoreReachGateState(t)
	wireToolGate(t, "shadow")
	t.Setenv(ReachGateFlagEnv, "shadow")
	t.Setenv(service.RecoveryWorkerFlagEnv, "shadow")
	t.Setenv(service.RecoveryWorkerIntervalEnv, "1h")

	db := testutil.NewTestDB(t)
	w := InitRecoveryWorker(db)
	if w == nil {
		t.Fatal("依赖齐全 + shadow 档应装配并启动")
	}
	if !w.Running() {
		t.Error("shadow 档应已起协程（首轮在间隔后才跑，不产生外发副作用）")
	}
	if w.Mode() != service.RecoveryWorkerModeShadow {
		t.Errorf("Mode=%s，期望 shadow", w.Mode())
	}
	w.Stop(context.Background())
	w.Stop(context.Background())
	if w.Running() {
		t.Error("Stop 应幂等：第二次 Stop 后不应仍在运行")
	}

	// 重复初始化：装配层无全局登记，调用方持有句柄各自 Stop；这里验新实例照常起。
	w2 := InitRecoveryWorker(db)
	if w2 == nil || !w2.Running() {
		t.Fatal("重复 Init 应装配出第二份实例并启动")
	}
	w2.Stop(context.Background())
}
