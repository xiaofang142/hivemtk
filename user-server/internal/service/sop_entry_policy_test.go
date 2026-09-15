package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestParseSOPEntryPolicy_DefaultOnce(t *testing.T) {
	p := ParseSOPEntryPolicy(nil)
	if p.Mode != SOPEntryModeOnce {
		t.Errorf("缺省 mode 应为 once，实际 %q", p.Mode)
	}
	p2 := ParseSOPEntryPolicy(model.JSONMap{})
	if p2.Mode != SOPEntryModeOnce {
		t.Errorf("空 trigger_config 缺省应为 once，实际 %q", p2.Mode)
	}
}

func TestParseSOPEntryPolicy_FullConfig(t *testing.T) {
	p := ParseSOPEntryPolicy(model.JSONMap{
		"entry_policy": map[string]any{
			"mode":          "cooldown",
			"cooldown_days": float64(7),
			"goal_exit":     "deal_closed eq true",
		},
	})
	if p.Mode != SOPEntryModeCooldown || p.CooldownDays != 7 || p.GoalExit != "deal_closed eq true" {
		t.Errorf("解析错误: %+v", p)
	}
}

func TestParseSOPEntryPolicy_InvalidModeFallsBackToOnce(t *testing.T) {
	p := ParseSOPEntryPolicy(model.JSONMap{
		"entry_policy": map[string]any{"mode": "bogus"},
	})
	if p.Mode != SOPEntryModeOnce {
		t.Errorf("非法 mode 应回退 once，实际 %q", p.Mode)
	}
}

func TestValidateSOPEntryPolicy(t *testing.T) {
	if err := ValidateSOPEntryPolicy(SOPEntryPolicy{Mode: SOPEntryModeCooldown}); err == nil {
		t.Error("cooldown 且 cooldown_days=0 应报错")
	}
	if err := ValidateSOPEntryPolicy(SOPEntryPolicy{Mode: "nope"}); err == nil {
		t.Error("非法 mode 应报错")
	}
	for _, m := range []string{SOPEntryModeOnce, SOPEntryModeAlways} {
		if err := ValidateSOPEntryPolicy(SOPEntryPolicy{Mode: m}); err != nil {
			t.Errorf("mode=%s 不应报错: %v", m, err)
		}
	}
}

func TestGoalExitAchieved(t *testing.T) {
	data := model.JSONMap{"deal_closed": true, "intent_score": 0.9}
	if !goalExitAchieved("deal_closed eq true", data) {
		t.Error("goal_exit 应达成")
	}
	if goalExitAchieved("intent_score lt 0.5", data) {
		t.Error("goal_exit 不应达成")
	}
	if goalExitAchieved("", data) {
		t.Error("空 goal_exit 不应视为达成")
	}
}

func newS1TestService(t *testing.T) *SOPService {
	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{})
	return NewSOPService(db, nil)
}

func createS1Agent(t *testing.T, svc *SOPService, trigger model.JSONMap) uint {
	t.Helper()
	graph := model.JSONMap{
		"nodes": []any{
			map[string]any{"id": "start", "type": "start", "next": []any{"end"}},
			map[string]any{"id": "end", "type": "end"},
		},
	}
	agent := &model.SOPAgent{
		Name:          "s1-test",
		Scenario:      "test",
		TriggerType:   SOPTriggerManual,
		TriggerConfig: trigger,
		SOPGraph:      graph,
		IsActive:      true,
	}
	if err := svc.agentRepo.Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return agent.ID
}

func TestS1_1_EntryPolicy_Modes(t *testing.T) {
	cases := []struct {
		name    string
		trigger model.JSONMap
		blocked bool
	}{
		{"once_缺省第二次拦截", nil, true},
		{"once_显式第二次拦截", model.JSONMap{"entry_policy": map[string]any{"mode": "once"}}, true},
		{"always_允许重入", model.JSONMap{"entry_policy": map[string]any{"mode": "always"}}, false},
		{"cooldown_窗口内第二次拦截", model.JSONMap{"entry_policy": map[string]any{"mode": "cooldown", "cooldown_days": 7}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newS1TestService(t)
			ctx := context.Background()
			sopID := createS1Agent(t, svc, tc.trigger)

			first, err := svc.Execute(ctx, &dto.ExecuteRequest{SOPID: sopID, CustomerID: "cust_a"})
			if err != nil {
				t.Fatalf("首次进入应放行: %v", err)
			}

			if err := svc.execRepo.UpdateStatus(ctx, first.ID, SOPStatusSuccess); err != nil {
				t.Fatalf("update status: %v", err)
			}
			_, err = svc.Execute(ctx, &dto.ExecuteRequest{SOPID: sopID, CustomerID: "cust_a"})
			if tc.blocked && err == nil {
				t.Error("第二次进入应被 entry_policy 拦截")
			}
			if !tc.blocked && err != nil {
				t.Errorf("重入应放行: %v", err)
			}
		})
	}
}

// TestS1_1_CooldownBoundary cooldown 边界：恰好 N 天放行（>= 窗口），N 天内拦截
func TestS1_1_CooldownBoundary(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{})
	svc := NewSOPService(db, nil)
	ctx := context.Background()
	sopID := createS1Agent(t, svc, model.JSONMap{"entry_policy": map[string]any{"mode": "cooldown", "cooldown_days": 7}})
	policy := ParseSOPEntryPolicy(model.JSONMap{
		"entry_policy": map[string]any{"mode": "cooldown", "cooldown_days": 7},
	})

	now := time.Now()
	cases := []struct {
		name    string
		ageDays float64
		allowed bool
	}{
		{"冷却期内_6天_拦截", 6, false},
		{"恰好7天边界_放行", 7, true},
		{"超过窗口_8天_放行", 8, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			last := &model.SOPExecution{
				SOPID:      sopID,
				CustomerID: "cust_boundary",
				Status:     SOPStatusSuccess,
				CreatedAt:  now.Add(-time.Duration(tc.ageDays * float64(24*time.Hour))),
			}
			if err := db.WithContext(ctx).Create(last).Error; err != nil {
				t.Fatalf("seed execution: %v", err)
			}
			got := svc.entryAllowedByPolicy(ctx, sopID, "cust_boundary", policy)
			if got != tc.allowed {
				t.Errorf("age=%.0fd allowed=%v want=%v", tc.ageDays, got, tc.allowed)
			}
			db.WithContext(ctx).Where("sop_id = ? AND customer_id = ?", sopID, "cust_boundary").
				Delete(&model.SOPExecution{})
		})
	}
}

type s1TaskRecorder struct{ tasks []*dispatchTask }

func (r *s1TaskRecorder) DispatchOrLog(task *dispatchTask) { r.tasks = append(r.tasks, task) }

func TestS1_2_MaxWaitExceeded_TimerSkipped(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{}, &model.SOPExecEvent{}, &model.SOPTimer{})

	ctx := context.Background()

	exec := &model.SOPExecution{SOPID: 1, CustomerID: "c1", Status: SOPStatusRunning, CurrentNode: "wait_node"}
	if err := db.Create(exec).Error; err != nil {
		t.Fatal(err)
	}
	timer := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait_node",
		WaitEvent:   WaitEventCustomerReply,
		WaitUntil:   time.Now().Add(10 * time.Hour),
		Status:      "pending",
		Payload: model.JSONMap{
			"expires_at":  time.Now().Add(10 * time.Hour).Format(time.RFC3339),
			"max_wait_at": time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
		},
	}
	if err := db.Create(timer).Error; err != nil {
		t.Fatal(err)
	}

	recorder := &s1TaskRecorder{}
	outbox := &SOPOutboxDispatcher{
		timerRepo:      repository.NewSOPTimerRepository(db),
		eventRepo:      repository.NewSOPExecEventRepository(db),
		execDispatcher: recorder,
		batchSize:      100,
	}
	outbox.processDueTimers(ctx)

	var updated model.SOPTimer
	if err := db.First(&updated, timer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != sopTimerStatusSkipped {
		t.Errorf("timer status 应为 skipped，实际 %q", updated.Status)
	}

	var events []model.SOPExecEvent
	db.Where("execution_id = ? AND event_type = ?", exec.ID, NodeEventSkipped).Find(&events)
	if len(events) == 0 {
		t.Error("应写入 skipped 事件记录")
	}

	if len(recorder.tasks) != 1 {
		t.Fatalf("应派发 1 个 SkipWait 任务，实际 %d", len(recorder.tasks))
	}
	task := recorder.tasks[0]
	if !task.SkipWait || task.NodeID != "wait_node" || task.ExecutionID != exec.ID {
		t.Errorf("SkipWait 任务字段错误: %+v", task)
	}
}

// TestWaitExecutor_WritesExpiresAndMaxWait S1-2：timer 创建时写双字段快照
func TestWaitExecutor_WritesExpiresAndMaxWait(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPTimer{})

	e := NewWaitExecutor(&SOPNodeExecutorDeps{DB: db})
	start := time.Now()
	result, err := e.Execute(context.Background(), &ExecutionContext{
		Execution:  &model.SOPExecution{ID: 9},
		Node:       &dto.SOPNode{ID: "w", Type: SOPNodeTypeWait, Config: map[string]any{"max_wait_seconds": float64(60)}},
		CustomerID: "c",
		TraceID:    "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != NodeStatusWaiting {
		t.Fatalf("status 应 waiting，实际 %q", result.Status)
	}

	var tm model.SOPTimer
	if err := db.Where("execution_id = ?", 9).First(&tm).Error; err != nil {
		t.Fatal(err)
	}
	expiresAt := parseSOPTimePayload(tm.Payload, "expires_at")
	maxWaitAt := parseSOPTimePayload(tm.Payload, "max_wait_at")
	if expiresAt.IsZero() || maxWaitAt.IsZero() {
		t.Fatalf("payload 缺少 expires_at/max_wait_at: %v", tm.Payload)
	}

	if maxWaitAt.Sub(start) < 30*time.Second || maxWaitAt.Sub(start) > 2*time.Minute {
		t.Errorf("max_wait_at 应≈now+60s，实际 delta=%v", maxWaitAt.Sub(start))
	}
	if !expiresAt.After(maxWaitAt) {
		t.Errorf("本例 expires_at 应晚于 max_wait_at")
	}
}

func TestS1_5_DeadLetter_ClaimThreshold(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{}, &model.SOPExecEvent{}, &model.SOPTimer{})

	ctx := context.Background()

	mkTimer := func(claims int) *model.SOPTimer {
		tm := &model.SOPTimer{
			ExecutionID: 1,
			NodeID:      "n1",
			WaitEvent:   WaitEventTimer,
			WaitUntil:   time.Now().Add(10 * time.Hour),
			Status:      "pending",
			Payload:     model.JSONMap{},
		}
		if claims > 0 {
			tm.Payload["claim_count"] = claims
		}
		if err := db.Create(tm).Error; err != nil {
			t.Fatal(err)
		}
		return tm
	}

	below := mkTimer(sopTimerMaxClaims - 1)
	atLimit := mkTimer(sopTimerMaxClaims)

	outbox := &SOPOutboxDispatcher{
		timerRepo:      repository.NewSOPTimerRepository(db),
		eventRepo:      repository.NewSOPExecEventRepository(db),
		execDispatcher: &s1TaskRecorder{},
		batchSize:      100,
	}
	outbox.processDueTimers(ctx)

	var gotBelow, gotDead model.SOPTimer
	db.First(&gotBelow, below.ID)
	db.First(&gotDead, atLimit.ID)
	if gotBelow.Status != sopTimerStatusPending {
		t.Errorf("claim_count=4 不应迁移，实际 %q", gotBelow.Status)
	}
	if gotDead.Status != sopTimerStatusDeadLetter {
		t.Errorf("claim_count=5 应转 dead_letter，实际 %q", gotDead.Status)
	}
}

// TestS1_3_AppendExecutedNode_ErrorClass S1-3：失败节点轨迹携带 error_class
func TestS1_3_AppendExecutedNode_ErrorClass(t *testing.T) {
	exec := &model.SOPExecution{}
	node := &dto.SOPNode{ID: "n1", Type: "llm"}

	appendExecutedNodeWithStatus(exec, node, 2, "failed", "boom", SOPErrorClassPermanent)
	appendExecutedNodeWithStatus(exec, node, 3, "failed", "boom again", SOPErrorClassTransient)

	raw, _ := json.Marshal(exec.ExecutedNodes)
	var entries []compensationTraceEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("应有 2 条轨迹，实际 %d", len(entries))
	}
	if entries[0].ErrorClass != SOPErrorClassPermanent || entries[0].Status != "failed" {
		t.Errorf("第一条应为 failed/permanent: %+v", entries[0])
	}
	if entries[1].ErrorClass != SOPErrorClassTransient {
		t.Errorf("第二条应为 transient: %+v", entries[1])
	}
}
