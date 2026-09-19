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
