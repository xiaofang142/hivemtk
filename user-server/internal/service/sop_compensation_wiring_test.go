// sop_compensation_wiring_test.go T-P1-02：Saga 补偿装配 + 失败路径真实补偿。
//
// 为什么必须在 service 包内写：入口是未导出的 failExecution（tryCompensate 的唯一生产触发点），
// 而 internal/service/sop_compensation_integration_test.go 那批"集成测试"实为
// 空跑烟测（构造 exec 但不落库 ⇒ plan 恒空、早退，随后 sleep 100ms 且**零断言**）。
// 本文件用真库补齐 AC①：失败路径确实触发 Plan→Run，确实产出 Summary，
// 确实把 pending 定时器与 LLM 产物键撤掉。
package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// compensationFixture 建一套可补偿的 SOP：start → n_llm(llm) → g1(greeting) → w1(wait) → end，
// 覆盖三类补偿语义：全可撤（llm 产物键 / wait 定时器）、部分可撤（消息节点，T-P1-03）、
// 无可撤状态（start），并预置 1 条该节点的 pending 定时器 + 2 条**不该被动**的对照定时器。
type compensationFixture struct {
	db        *gorm.DB
	disp      *SOPExecutionDispatcher
	execID    uint
	llmNodeID string
	waitNode  string
	otherNode string
}

func newCompensationFixture(t *testing.T) *compensationFixture {
	t.Helper()
	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{}, &model.SOPTimer{})
	ctx := context.Background()

	graph := model.JSONMap{
		"nodes": []any{
			map[string]any{"id": "start", "type": "start", "next": []any{"n_llm"}},
			map[string]any{"id": "n_llm", "type": "llm", "next": []any{"g1"}},
			map[string]any{"id": "g1", "type": "greeting", "next": []any{"w1"}},
			map[string]any{"id": "w1", "type": "wait", "next": []any{"end"}},
			map[string]any{"id": "end", "type": "end"},
		},
	}
	agent := &model.SOPAgent{
		Name:        "t-p1-02-compensation",
		Scenario:    "test",
		TriggerType: SOPTriggerManual,
		SOPGraph:    graph,
		IsActive:    true,
	}
	if err := db.WithContext(ctx).Create(agent).Error; err != nil {
		t.Fatalf("建 SOP 失败: %v", err)
	}

	exec := &model.SOPExecution{
		SOPID:      agent.ID,
		CustomerID: "cust-tp102",
		SessionID:  "sess-tp102",
		Status:     SOPStatusRunning,
		// 轨迹：start/llm/wait 三步都记为已完成（failed 节点由 plan 侧排除）
		ExecutedNodes: model.JSONArray{
			map[string]any{"node_id": "start", "node_type": "start", "status": "completed", "attempt": 1},
			map[string]any{"node_id": "n_llm", "node_type": "llm", "status": "completed", "attempt": 1},
			map[string]any{"node_id": "g1", "node_type": "greeting", "status": "completed", "attempt": 1},
			map[string]any{"node_id": "w1", "node_type": "wait", "status": "completed", "attempt": 1},
		},
		// g1 是消息节点：产物键可撤、已出域消息不可撤（T-P1-03 部分补偿）
		ExecutionData: model.JSONMap{
			"_llm_decision": "advance", "_llm_reason": "r",
			"_greeting_content": "您好，看到您咨询过", "_greeting_source": "prompt",
			"keep_me": 1,
		},
	}
	if err := db.WithContext(ctx).Create(exec).Error; err != nil {
		t.Fatalf("建执行失败: %v", err)
	}

	timers := []model.SOPTimer{
		// 被补偿对象：同一 wait 节点的 pending 定时器
		{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: "reply", WaitUntil: time.Now().Add(time.Hour), Status: "pending"},
		// 对照 1：同节点已 fired，pending 守卫必须让它活着
		{ExecutionID: exec.ID, NodeID: "w1", WaitEvent: "reply", WaitUntil: time.Now().Add(-time.Hour), Status: "fired"},
		// 对照 2：别的节点的 pending，节点级作用域必须让它活着
		{ExecutionID: exec.ID, NodeID: "other", WaitEvent: "reply", WaitUntil: time.Now().Add(time.Hour), Status: "pending"},
	}
	for i := range timers {
		if err := db.WithContext(ctx).Create(&timers[i]).Error; err != nil {
			t.Fatalf("建定时器失败: %v", err)
		}
	}

	disp := NewSOPExecutionDispatcher(db, NewSOPService(db, nil), NewNodeExecutorRegistry(), nil)
	return &compensationFixture{
		db: db, disp: disp, execID: exec.ID,
		llmNodeID: "n_llm", waitNode: "w1", otherNode: "other",
	}
}

func (f *compensationFixture) countTimers(t *testing.T, nodeID, status string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.SOPTimer{}).
		Where("execution_id = ? AND node_id = ? AND status = ?", f.execID, nodeID, status).
		Count(&n).Error; err != nil {
		t.Fatalf("查定时器失败: %v", err)
	}
	return n
}

func (f *compensationFixture) executionData(t *testing.T) map[string]any {
	t.Helper()
	return f.loadExecution(t).ExecutionData
}

// loadExecution 取库里的实时执行行。
//
// 必须把**这条**传给 failExecution：生产路径传入的就是 worker 手上那份带 ExecutionData
// 与 ExecutedNodes 的活对象；传桩对象会让 failExecution 内的 Save 把 execution_data
// 覆写成空，LLM 补偿随即"无键可清"而假成功（本文件初版就踩了这一点，是关旗对照组抓出来的）。
func (f *compensationFixture) loadExecution(t *testing.T) *model.SOPExecution {
	t.Helper()
	var fresh model.SOPExecution
	if err := f.db.First(&fresh, f.execID).Error; err != nil {
		t.Fatalf("重读执行失败: %v", err)
	}
	return &fresh
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("超时 %s 内未满足：%s", timeout, what)
}

// TestInitSOPCompensation_FollowsFlag AC②：装配点存在且只由开关决定是否注入。
func TestInitSOPCompensation_FollowsFlag(t *testing.T) {
	t.Setenv(compensationEnvVar, "")
	if got := CompensationEnabled(); got {
		t.Fatal("env 置空应判关")
	}
	off := NewSOPExecutionDispatcher(nil, nil, NewNodeExecutorRegistry(), nil)
	if mgr := InitSOPCompensation(off); mgr != nil {
		t.Fatal("开关关闭时不得注入管理器")
	}
	if off.compensationManager() != nil {
		t.Fatal("开关关闭时 compensationMgr 必须保持 nil（= 与接线前一致）")
	}

	t.Setenv(compensationEnvVar, "1")
	on := NewSOPExecutionDispatcher(nil, nil, NewNodeExecutorRegistry(), nil)
	mgr := InitSOPCompensation(on)
	if mgr == nil {
		t.Fatal("开关打开时必须注入管理器")
	}
	if on.compensationManager() != mgr {
		t.Fatal("注入后 dispatcher 必须持有同一实例")
	}

	if InitSOPCompensation(nil) != nil {
		t.Fatal("nil dispatcher 应直接返回 nil 而非 panic")
	}
}

// TestSOPFailPath_CompensatesAndSummarizes AC①：真实失败路径 → Plan→Run → Summary。
func TestSOPFailPath_CompensatesAndSummarizes(t *testing.T) {
	f := newCompensationFixture(t)
	t.Setenv(compensationEnvVar, "1")
	mgr := InitSOPCompensation(f.disp)
	if mgr == nil {
		t.Fatal("装配未生效")
	}
	ctx := context.Background()

	f.disp.failExecution(ctx, f.loadExecution(t), "boom: 注入的失败")

	eventually(t, 5*time.Second, "补偿计划落定", func() bool {
		p := mgr.GetPlan(f.execID)
		return p != nil && p.Status != CompensationStatusRunning
	})

	plan := mgr.GetPlan(f.execID)
	if len(plan.Records) != 4 {
		t.Fatalf("4 个已执行节点应有 4 条补偿记录, got %d (%+v)", len(plan.Records), plan.Records)
	}
	byNode := map[string]string{}
	for _, r := range plan.Records {
		byNode[r.NodeID] = r.Status
	}
	// SAGA 语义：补偿按执行的**逆序**（LIFO）下发，先撤最近的副作用
	wantOrder := []string{"w1", "g1", "n_llm", "start"}
	for i, want := range wantOrder {
		if plan.Records[i].NodeID != want {
			t.Fatalf("补偿顺序应为 %v，第 %d 条 got %s", wantOrder, i, plan.Records[i].NodeID)
		}
	}
	if byNode["w1"] != CompensationStatusCompleted {
		t.Errorf("wait 节点补偿应 completed, got %q", byNode["w1"])
	}
	if byNode["n_llm"] != CompensationStatusCompleted {
		t.Errorf("llm 节点补偿应 completed, got %q", byNode["n_llm"])
	}
	if byNode["start"] != CompensationStatusSkipped {
		t.Errorf("start 无副作用应 skipped, got %q", byNode["start"])
	}
	// T-P1-03：skipped 必须自带理由，且"无可撤状态"与"有出域残留"要能分辨
	byReason := map[string]string{}
	for _, r := range plan.Records {
		byReason[r.NodeID] = r.Reason
	}
	if byReason["start"] == "" {
		t.Error("start 的 skipped 应自述为何不撤销")
	}
	if !strings.Contains(byReason["g1"], "出域") {
		t.Errorf("消息节点的补偿记录应声明出域残留, got %q", byReason["g1"])
	}
	// 反向证据：撤销真的发生了，而不是"记了一条 completed 却没动手"
	if n := f.countTimers(t, f.waitNode, "pending"); n != 0 {
		t.Errorf("wait 节点 pending 定时器应被撤销, got %d 条", n)
	}
	if n := f.countTimers(t, f.waitNode, "fired"); n != 1 {
		t.Errorf("已 fired 定时器不得被撤销（pending 守卫）, got %d 条", n)
	}
	if n := f.countTimers(t, f.otherNode, "pending"); n != 1 {
		t.Errorf("其它节点的 pending 定时器不得被撤销（节点级作用域）, got %d 条", n)
	}
	data := f.executionData(t)
	for _, k := range []string{"_llm_decision", "_llm_reason", "_llm_" + f.llmNodeID} {
		if _, ok := data[k]; ok {
			t.Errorf("LLM 产物键 %s 应被清除", k)
		}
	}
	if _, ok := data["keep_me"]; !ok {
		t.Error("补偿不得越界清除非本节点产物键")
	}
	for _, k := range []string{"_greeting_content", "_greeting_source"} {
		if _, ok := data[k]; ok {
			t.Errorf("消息节点产物键 %s 应被清除（部分补偿的内部那一半）", k)
		}
	}

	sum := mgr.Summary()
	if sum.TotalPlans != 1 || sum.TotalNodes != 4 || sum.CompletedPlans != 1 {
		t.Errorf("Summary 口径不符: %+v", sum)
	}
	if sum.FailedPlans != 0 || sum.FailedNodes != 0 {
		t.Errorf("本用例不该有失败补偿: %+v", sum)
	}
}

// TestSOPFailPath_FlagOffKeepsPriorBehavior AC①的对照组：关旗时定时器与产物键**原样保留**。
func TestSOPFailPath_FlagOffKeepsPriorBehavior(t *testing.T) {
	f := newCompensationFixture(t)
	t.Setenv(compensationEnvVar, "")
	if mgr := InitSOPCompensation(f.disp); mgr != nil {
		t.Fatal("关旗不应装配")
	}

	f.disp.failExecution(context.Background(), f.loadExecution(t), "boom")
	time.Sleep(300 * time.Millisecond) // 给异步路径足够窗口，再断言"什么都没发生"

	if n := f.countTimers(t, f.waitNode, "pending"); n != 1 {
		t.Errorf("关旗时 pending 定时器必须原样保留, got %d", n)
	}
	if _, ok := f.executionData(t)["_llm_decision"]; !ok {
		t.Error("关旗时 LLM 产物键必须原样保留")
	}
}

// TestCompensationPlansRetentionBounded 进程级单例的保留上界（接线后才暴露的泄漏面）。
func TestCompensationPlansRetentionBounded(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxPlansKept = 4
	mgr := NewCompensationManager(cfg)
	ctx := context.Background()

	for id := uint(1); id <= 30; id++ {
		mgr.Run(ctx, id, nil,
			func(string) NodeExecutor { return nil },
			func(string) *ExecutionContext { return nil })
	}
	if got := mgr.Summary().TotalPlans; got != 4 {
		t.Fatalf("保留条数应被上界压住, got %d", got)
	}
	// 淘汰按写入顺序：最旧的 1..26 走掉，最近的还在
	if mgr.GetPlan(26) != nil {
		t.Error("第 26 条应已被淘汰")
	}
	if mgr.GetPlan(30) == nil {
		t.Error("最近一条必须仍可查（Summary/GetPlan 的观测口径）")
	}

	// 上界非法时回落默认值，而不是退化成"不保留"或"无界"
	if NewCompensationManager(CompensationConfig{MaxPlansKept: 0}).config.MaxPlansKept != 512 {
		t.Error("MaxPlansKept<=0 应回落 512")
	}
}

// TestCompensationManager_ConcurrentRunAndRead 是 -race 契约：
// 生产形态就是"dispatcher 的失败路径在后台 goroutine 里边补偿边被读"
// （main.go 退出摘要、监控侧 GetPlan），所以 Run 对已发布计划的每次改写都必须持锁、
// GetPlan 必须给快照而不是活指针。
//
// 为什么单独立一个测试而不是靠上面那几个：失败路径用例的读点在 20ms 轮询里，
// 补偿写入可能在两次轮询之间就跑完，-race 只能报**真实重叠**的访问（实测：
// 去掉 appendRecord 的锁后该用例仍然绿）。本测试用紧循环把窗口压满。
func TestCompensationManager_ConcurrentRunAndRead(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxPlansKept = 4
	mgr := NewCompensationManager(cfg)

	executor := &mockCompensableExecutor{nodeType: "llm"}
	getExecutor := func(string) NodeExecutor { return executor }
	execCtxFor := func(nodeID string) *ExecutionContext { return newTestExecCtx(nodeID, "llm") }
	planned := make([]CompensationRecord, 12)
	for i := range planned {
		planned[i] = CompensationRecord{NodeID: fmt.Sprintf("n%02d", i), NodeType: "llm"}
	}

	const executions = 4
	var writers, readers sync.WaitGroup
	stop := make(chan struct{})

	for w := 0; w < executions; w++ {
		id := uint(w + 1)
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := 0; i < 60; i++ {
				mgr.Run(context.Background(), id, planned, getExecutor, execCtxFor)
			}
		}()
	}

	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for id := uint(1); id <= executions; id++ {
					if p := mgr.GetPlan(id); p != nil {
						for _, rec := range p.Records {
							if rec.NodeID == "" {
								t.Error("快照里出现空 node_id 记录（读到半写入状态）")
								return
							}
						}
						if p.Status == "" {
							t.Error("快照 Status 为空")
							return
						}
					}
				}
				mgr.Summary()
			}
		}()
	}

	writers.Wait()
	close(stop)
	readers.Wait()

	// 收工后每个执行都应留一份**完整**计划（快照语义：不会读到半截 Records）
	for id := uint(1); id <= executions; id++ {
		p := mgr.GetPlan(id)
		if p == nil {
			t.Fatalf("execution %d 的计划读不到", id)
		}
		if len(p.Records) != len(planned) {
			t.Errorf("execution %d 记录数不完整: got %d want %d", id, len(p.Records), len(planned))
		}
		if p.Status != CompensationStatusCompleted {
			t.Errorf("execution %d 终态应为 completed, got %q", id, p.Status)
		}
	}
	if sum := mgr.Summary(); sum.TotalPlans != executions || sum.FailedPlans != 0 {
		t.Errorf("Summary 口径不符: %+v", sum)
	}

	// 返回副本而非活指针：改写快照不得污染管理器
	if p := mgr.GetPlan(1); p != nil {
		original := len(p.Records)
		p.Records = append(p.Records, CompensationRecord{NodeID: "pollution"})
		if got := len(mgr.GetPlan(1).Records); got != original {
			t.Errorf("GetPlan 返回的是共享指针（got %d want %d），调用方可污染内部状态", got, original)
		}
	}
}
