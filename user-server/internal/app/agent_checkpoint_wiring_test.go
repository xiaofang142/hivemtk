// agent_checkpoint_wiring_test.go T-P1-01：装配层适配器对 service 三个入口的真实委托。
//
// 用真库（testutil 槽位库）跑，不走内存桩：本卡要锁死的正是"检查点真的落到
// agent_checkpoints 表、恢复游标真的由 service 判定"这条链路。
package app

import (
	"context"
	"encoding/json"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

func newCheckpointStoreForTest(t *testing.T) (*agentCheckpointStore, *gorm.DB, func()) {
	t.Helper()
	db := testutil.NewTestDB(t)
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS agent_checkpoints (
		id BIGSERIAL PRIMARY KEY,
		thread_id VARCHAR(120) NOT NULL,
		stage VARCHAR(40) NOT NULL,
		state JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_agent_ckpt UNIQUE (thread_id, stage)
	)`).Error; err != nil {
		t.Skipf("建表失败（环境限制）: %v", err)
	}
	return &agentCheckpointStore{repo: service.NewAgentCheckpointRepository(db)}, db, func() {
		db.Exec("DELETE FROM agent_checkpoints")
	}
}

func checkpointTestState(t *testing.T, stage string) json.RawMessage {
	t.Helper()
	blob, err := json.Marshal(map[string]any{"v": 1, "stage": stage})
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// LoadResume 必须按阶段序号（而非写入时间）给续跑点，且把游标行的 state 原样带回
func TestAgentCheckpointStore_LoadResumeReturnsNextStage(t *testing.T) {
	store, _, cleanup := newCheckpointStoreForTest(t)
	defer cleanup()
	ctx := context.Background()
	const thread = "thr-app-wiring-1"

	for _, stage := range []string{"perception", "alignment", "gatekeeper"} {
		if err := store.Save(ctx, thread, stage, checkpointTestState(t, stage)); err != nil {
			t.Fatalf("Save(%s) failed: %v", stage, err)
		}
	}
	// 重跑 perception：updated_at 最新，但游标不得倒退
	if err := store.Save(ctx, thread, "perception", checkpointTestState(t, "perception-rerun")); err != nil {
		t.Fatal(err)
	}

	resume, state, err := store.LoadResume(ctx, thread)
	if err != nil {
		t.Fatalf("LoadResume failed: %v", err)
	}
	if resume != "planner" {
		t.Errorf("gatekeeper 之后的续跑点应是 planner，got %q", resume)
	}
	var got map[string]any
	if err := json.Unmarshal(state, &got); err != nil {
		t.Fatalf("state 不可解析: %v", err)
	}
	if got["stage"] != "gatekeeper" {
		t.Errorf("应返回游标行（gatekeeper）的 state，got %v", got["stage"])
	}

	// 反向：无记录 → 空续跑点（运行侧据此整体重跑）
	if resume, state, err := store.LoadResume(ctx, "thr-never-saved"); err != nil || resume != "" || state != nil {
		t.Errorf("无记录应返回 (\"\", nil, nil)，got (%q, %v, %v)", resume, string(state), err)
	}
}

// Enabled 走 FF_LTC_CHECKPOINT，且默认关
func TestAgentCheckpointStore_EnabledFollowsEnv(t *testing.T) {
	store, _, cleanup := newCheckpointStoreForTest(t)
	defer cleanup()

	t.Setenv("FF_LTC_CHECKPOINT", "")
	if store.Enabled() {
		t.Error("默认必须关闭")
	}
	t.Setenv("FF_LTC_CHECKPOINT", "1")
	if !store.Enabled() {
		t.Error("FF_LTC_CHECKPOINT=1 应开启")
	}
}

// InitAgentCheckpointStore 幂等；db 为 nil 时不装配（GetAgentCheckpointStore 保持 nil）
func TestInitAgentCheckpointStore_NilDBIsNoop(t *testing.T) {
	InitAgentCheckpointStore(nil)
	if got := GetAgentCheckpointStore(); got != nil {
		t.Fatalf("db=nil 不应装配，got %T", got)
	}
}

// 生产装配路径实跑：router.Setup 里的那个 InitInferenceOrchestrator 必须真的把
// store 注进推理闭环。只测 runtime 包内的 SetCheckpointStore 调用是测不到装配缺失的
// （同 T-P0-07 的教训：静态 grep 说已接线、运行时装配点漏掉）。
func TestInitInferenceOrchestrator_MountsCheckpointStoreOnRealDB(t *testing.T) {
	store, db, cleanup := newCheckpointStoreForTest(t)
	defer cleanup()

	globalAgentCheckpointStoreOnce.Do(func() { globalAgentCheckpointStore = store })
	if globalAgentCheckpointStore == nil {
		t.Fatal("store 未装配")
	}
	InitInferenceOrchestrator()
	orch := GetInferenceOrchestrator()
	if orch == nil {
		t.Fatal("orchestrator 未装配")
	}

	payload := agent_runtime.CustomerMessagePayload{
		ChannelType: "xiaohongshu",
		CustomerID:  "cust-app-e2e",
		SessionID:   "sess-app-e2e",
		Content:     "你们支持开发票吗",
		MessageType: "text",
		TraceID:     "trace-app-e2e",
	}
	thread := agent_runtime.CheckpointThreadID(payload)

	// 开关关闭：装配到位也不得写库（AC②：flag off 行为与今天完全一致）
	t.Setenv("FF_LTC_CHECKPOINT", "")
	if _, err := orch.Process(context.Background(), payload, nil); err != nil {
		t.Fatalf("process (flag off) failed: %v", err)
	}
	var off int64
	if err := db.Table("agent_checkpoints").Where("thread_id = ?", thread).Count(&off).Error; err != nil {
		t.Fatal(err)
	}
	if off != 0 {
		t.Fatalf("FF_LTC_CHECKPOINT 关闭时不得写库，got %d 行", off)
	}

	// 开关打开：同一装配、同一条链路必须真实落点
	t.Setenv("FF_LTC_CHECKPOINT", "1")
	if _, err := orch.Process(context.Background(), payload, nil); err != nil {
		t.Fatalf("process (flag on) failed: %v", err)
	}
	var on int64
	if err := db.Table("agent_checkpoints").Where("thread_id = ?", thread).Count(&on).Error; err != nil {
		t.Fatal(err)
	}
	if on == 0 {
		t.Fatal("FF_LTC_CHECKPOINT=1 时生产装配路径应写出阶段检查点，got 0 行")
	}
	t.Logf("装配路径实跑：thread=%s 落点 %d 个阶段边界", thread, on)
}
