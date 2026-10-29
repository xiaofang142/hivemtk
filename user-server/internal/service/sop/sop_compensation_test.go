package sop

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

type mockCompensableExecutor struct {
	nodeType     string
	executeFn    func(ctx context.Context, execCtx *ExecutionContext) (*NodeExecResult, error)
	compensateFn func(ctx context.Context, execCtx *ExecutionContext) error
}

func (m *mockCompensableExecutor) NodeType() string { return m.nodeType }
func (m *mockCompensableExecutor) IsAsync() bool    { return false }
func (m *mockCompensableExecutor) Execute(ctx context.Context, execCtx *ExecutionContext) (*NodeExecResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, execCtx)
	}
	return &NodeExecResult{Status: NodeStatusCompleted, Output: model.JSONMap{}}, nil
}
func (m *mockCompensableExecutor) Compensate(ctx context.Context, execCtx *ExecutionContext) error {
	if m.compensateFn != nil {
		return m.compensateFn(ctx, execCtx)
	}
	return nil
}

type mockNonCompensableExecutor struct {
	nodeType string
}

func (m *mockNonCompensableExecutor) NodeType() string { return m.nodeType }
func (m *mockNonCompensableExecutor) IsAsync() bool    { return false }
func (m *mockNonCompensableExecutor) Execute(_ context.Context, _ *ExecutionContext) (*NodeExecResult, error) {
	return &NodeExecResult{Status: NodeStatusCompleted}, nil
}

func newTestExecCtx(nodeID, nodeType string) *ExecutionContext {
	return &ExecutionContext{
		Node:    &dto.SOPNode{ID: nodeID, Type: nodeType},
		TraceID: "test-trace",
	}
}

// TestCompensationManager_Plan_ReverseOrder 验证补偿计划为反向
//
// 业界 SAGA 原则：补偿必须按执行**相反**顺序（Garcia-Molina 1987 §3）。
func TestCompensationManager_Plan_ReverseOrder(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	executed := []CompensationRecord{
		{NodeID: "step1"}, {NodeID: "step2"}, {NodeID: "step3"}, {NodeID: "step4"},
	}
	plan := m.Plan(executed)
	if len(plan) != 4 {
		t.Fatalf("expected 4 records, got %d", len(plan))
	}
	if plan[0].NodeID != "step4" || plan[3].NodeID != "step1" {
		t.Errorf("expected reverse order, got %v", []string{plan[0].NodeID, plan[3].NodeID})
	}
}

// TestCompensationManager_Plan_EmptyInput 验证空输入
func TestCompensationManager_Plan_EmptyInput(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	plan := m.Plan(nil)
	if len(plan) != 0 {
		t.Errorf("empty input should yield empty plan, got %d", len(plan))
	}
}

// TestCompensationManager_CompensateNode_Success 验证成功补偿
func TestCompensationManager_CompensateNode_Success(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	executor := &mockCompensableExecutor{
		nodeType: "send_sms",
		compensateFn: func(_ context.Context, _ *ExecutionContext) error {
			return nil
		},
	}
	rec := m.CompensateNode(context.Background(), newTestExecCtx("n1", "send_sms"), executor)
	if rec.Status != CompensationStatusCompleted {
		t.Errorf("expected completed, got %s (err=%s)", rec.Status, rec.Error)
	}
	if rec.Attempt != 1 {
		t.Errorf("expected attempt 1, got %d", rec.Attempt)
	}
}

// TestCompensationManager_CompensateNode_NotCompensable 验证不可补偿节点被跳过
func TestCompensationManager_CompensateNode_NotCompensable(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	executor := &mockNonCompensableExecutor{nodeType: "log"}
	rec := m.CompensateNode(context.Background(), newTestExecCtx("n1", "log"), executor)
	if rec.Status != CompensationStatusSkipped {
		t.Errorf("expected skipped, got %s", rec.Status)
	}
}

// TestCompensationManager_CompensateNode_RetriesUntilSuccess 验证重试直到成功
func TestCompensationManager_CompensateNode_RetriesUntilSuccess(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 3
	cfg.PerCompensationTTL = 1 * time.Second
	m := NewCompensationManager(cfg)
	attempts := 0
	executor := &mockCompensableExecutor{
		nodeType: "send_sms",
		compensateFn: func(_ context.Context, _ *ExecutionContext) error {
			attempts++
			if attempts < 2 {
				return errors.New("transient error")
			}
			return nil
		},
	}
	rec := m.CompensateNode(context.Background(), newTestExecCtx("n1", "send_sms"), executor)
	if rec.Status != CompensationStatusCompleted {
		t.Errorf("expected completed after retry, got %s", rec.Status)
	}
	if rec.Attempt != 2 {
		t.Errorf("expected attempt 2, got %d", rec.Attempt)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

// TestCompensationManager_CompensateNode_AllAttemptsFail 验证所有尝试都失败
func TestCompensationManager_CompensateNode_AllAttemptsFail(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 3
	cfg.PerCompensationTTL = 100 * time.Millisecond
	m := NewCompensationManager(cfg)
	executor := &mockCompensableExecutor{
		nodeType: "send_sms",
		compensateFn: func(_ context.Context, _ *ExecutionContext) error {
			return errors.New("permanent error")
		},
	}
	rec := m.CompensateNode(context.Background(), newTestExecCtx("n1", "send_sms"), executor)
	if rec.Status != CompensationStatusFailed {
		t.Errorf("expected failed, got %s", rec.Status)
	}
	if rec.Attempt != 3 {
		t.Errorf("expected 3 attempts, got %d", rec.Attempt)
	}
	if rec.Error == "" {
		t.Error("error message should be set")
	}
}

// TestCompensationManager_Run_FullSaga 验证完整 SAGA 流程
func TestCompensationManager_Run_FullSaga(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 1
	cfg.PerCompensationTTL = 1 * time.Second
	m := NewCompensationManager(cfg)

	plan := []CompensationRecord{
		{NodeID: "step1", NodeType: "send_sms"},
		{NodeID: "step2", NodeType: "create_order"},
		{NodeID: "step3", NodeType: "log"},
	}

	getExecutor := func(nodeType string) NodeExecutor {
		switch nodeType {
		case "send_sms":
			return &mockCompensableExecutor{
				nodeType:     "send_sms",
				compensateFn: func(_ context.Context, _ *ExecutionContext) error { return nil },
			}
		case "create_order":
			return &mockCompensableExecutor{
				nodeType:     "create_order",
				compensateFn: func(_ context.Context, _ *ExecutionContext) error { return nil },
			}
		case "log":
			return &mockNonCompensableExecutor{nodeType: "log"}
		}
		return nil
	}
	execCtxFor := func(nodeID string) *ExecutionContext {
		return newTestExecCtx(nodeID, "")
	}

	result := m.Run(context.Background(), 100, plan, getExecutor, execCtxFor)
	if result.Status != CompensationStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.Records) != 3 {
		t.Errorf("expected 3 records, got %d", len(result.Records))
	}

	if result.Records[0].Status != CompensationStatusCompleted {
		t.Errorf("[0] should be completed, got %s", result.Records[0].Status)
	}
	if result.Records[2].Status != CompensationStatusSkipped {
		t.Errorf("[2] (log) should be skipped, got %s", result.Records[2].Status)
	}
}

// TestCompensationManager_Run_PartialFailure 验证部分节点失败
//
// 业界 SAGA 特性：单节点失败不阻断其他补偿。
//   - 业务实践：失败的节点可人工介入处理
//   - 状态标记：partial → 让运维知道要跟进
func TestCompensationManager_Run_PartialFailure(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 1
	cfg.PerCompensationTTL = 100 * time.Millisecond
	m := NewCompensationManager(cfg)

	plan := []CompensationRecord{
		{NodeID: "step1", NodeType: "send_sms"},
		{NodeID: "step2", NodeType: "create_order"},
	}

	getExecutor := func(nodeType string) NodeExecutor {
		if nodeType == "send_sms" {
			return &mockCompensableExecutor{
				nodeType: nodeType,
				compensateFn: func(_ context.Context, _ *ExecutionContext) error {
					return errors.New("external API down")
				},
			}
		}
		return &mockCompensableExecutor{
			nodeType:     nodeType,
			compensateFn: func(_ context.Context, _ *ExecutionContext) error { return nil },
		}
	}
	execCtxFor := func(nodeID string) *ExecutionContext {
		return newTestExecCtx(nodeID, "")
	}

	result := m.Run(context.Background(), 100, plan, getExecutor, execCtxFor)
	if result.Status != "partial" {
		t.Errorf("expected partial, got %s", result.Status)
	}
	if result.Records[0].Status != CompensationStatusFailed {
		t.Errorf("[0] should be failed")
	}
	if result.Records[1].Status != CompensationStatusCompleted {
		t.Errorf("[1] should be completed")
	}
}

// TestCompensationManager_Run_AllFailed 验证全失败
func TestCompensationManager_Run_AllFailed(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 1
	m := NewCompensationManager(cfg)

	plan := []CompensationRecord{
		{NodeID: "step1", NodeType: "send_sms"},
		{NodeID: "step2", NodeType: "create_order"},
	}

	getExecutor := func(_ string) NodeExecutor {
		return &mockCompensableExecutor{
			compensateFn: func(_ context.Context, _ *ExecutionContext) error {
				return errors.New("fail")
			},
		}
	}
	execCtxFor := func(nodeID string) *ExecutionContext {
		return newTestExecCtx(nodeID, "")
	}

	result := m.Run(context.Background(), 100, plan, getExecutor, execCtxFor)
	if result.Status != CompensationStatusFailed {
		t.Errorf("expected failed, got %s", result.Status)
	}
}

// TestCompensationManager_Run_ContextCancellation 验证 ctx 取消
func TestCompensationManager_Run_ContextCancellation(t *testing.T) {
	cfg := DefaultCompensationConfig()
	cfg.MaxAttempts = 1
	cfg.PerCompensationTTL = 50 * time.Millisecond
	m := NewCompensationManager(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := []CompensationRecord{{NodeID: "step1", NodeType: "send_sms"}}
	getExecutor := func(_ string) NodeExecutor {
		return &mockCompensableExecutor{
			compensateFn: func(ctx context.Context, _ *ExecutionContext) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return nil
			},
		}
	}
	execCtxFor := func(nodeID string) *ExecutionContext {
		return newTestExecCtx(nodeID, "")
	}

	result := m.Run(ctx, 100, plan, getExecutor, execCtxFor)

	if len(result.Records) == 0 {
		t.Fatal("expected at least 1 record (aborted), got 0")
	}
	if result.Records[0].Status != CompensationStatusSkipped {
		t.Errorf("cancelled ctx should cause skipped status, got %s", result.Records[0].Status)
	}
	if result.Records[0].Error == "" {
		t.Error("aborted record should explain why")
	}
}

// TestCompensationManager_Run_MissingExecutor 验证 executor 找不到
func TestCompensationManager_Run_MissingExecutor(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	plan := []CompensationRecord{{NodeID: "step1", NodeType: "unknown_type"}}
	getExecutor := func(_ string) NodeExecutor { return nil }
	execCtxFor := func(nodeID string) *ExecutionContext { return newTestExecCtx(nodeID, "") }

	result := m.Run(context.Background(), 100, plan, getExecutor, execCtxFor)
	if result.Records[0].Status != CompensationStatusSkipped {
		t.Errorf("missing executor should be skipped, got %s", result.Records[0].Status)
	}
}

// TestCompensationManager_Run_MissingExecCtx 验证 execCtx 找不到
func TestCompensationManager_Run_MissingExecCtx(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	plan := []CompensationRecord{{NodeID: "step1", NodeType: "send_sms"}}
	getExecutor := func(_ string) NodeExecutor {
		return &mockCompensableExecutor{}
	}
	execCtxFor := func(_ string) *ExecutionContext { return nil }

	result := m.Run(context.Background(), 100, plan, getExecutor, execCtxFor)
	if result.Records[0].Status != CompensationStatusFailed {
		t.Errorf("missing execCtx should be failed, got %s", result.Records[0].Status)
	}
	if result.Records[0].Error == "" {
		t.Error("error message should explain missing context")
	}
}

// TestCompensationManager_GetPlan 验证查询补偿计划
func TestCompensationManager_GetPlan(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	if m.GetPlan(999) != nil {
		t.Error("non-existent plan should be nil")
	}
	plan := []CompensationRecord{{NodeID: "step1", NodeType: "send_sms"}}
	getExecutor := func(_ string) NodeExecutor { return &mockNonCompensableExecutor{} }
	execCtxFor := func(nodeID string) *ExecutionContext { return newTestExecCtx(nodeID, "") }
	m.Run(context.Background(), 42, plan, getExecutor, execCtxFor)

	got := m.GetPlan(42)
	if got == nil {
		t.Fatal("plan should be retrievable")
	}
	if got.ExecutionID != 42 {
		t.Errorf("expected exec id 42, got %d", got.ExecutionID)
	}
}

// TestCompensationManager_Summary 验证全局摘要
func TestCompensationManager_Summary(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	summary := m.Summary()
	if summary.TotalPlans != 0 {
		t.Errorf("empty manager should have 0 plans, got %d", summary.TotalPlans)
	}

	plan1 := []CompensationRecord{
		{NodeID: "n1", NodeType: "send_sms"},
		{NodeID: "n2", NodeType: "create_order"},
	}
	m.Run(context.Background(), 1, plan1,
		func(_ string) NodeExecutor { return &mockCompensableExecutor{} },
		func(id string) *ExecutionContext { return newTestExecCtx(id, "") })

	plan2 := []CompensationRecord{{NodeID: "n3", NodeType: "send_sms"}}
	m.Run(context.Background(), 2, plan2,
		func(_ string) NodeExecutor {
			return &mockCompensableExecutor{
				compensateFn: func(_ context.Context, _ *ExecutionContext) error {
					return errors.New("fail")
				},
			}
		},
		func(id string) *ExecutionContext { return newTestExecCtx(id, "") })

	summary = m.Summary()
	if summary.TotalPlans != 2 {
		t.Errorf("expected 2 plans, got %d", summary.TotalPlans)
	}
	if summary.TotalNodes != 3 {
		t.Errorf("expected 3 nodes, got %d", summary.TotalNodes)
	}
	if summary.FailedNodes < 1 {
		t.Errorf("expected at least 1 failed node, got %d", summary.FailedNodes)
	}
}

// TestCompensationManager_DefaultConfig 验证默认配置归一化
func TestCompensationManager_DefaultConfig(t *testing.T) {
	m := NewCompensationManager(CompensationConfig{})
	if m.config.MaxAttempts != 3 {
		t.Errorf("MaxAttempts should default to 3, got %d", m.config.MaxAttempts)
	}
	if m.config.PerCompensationTTL != 30*time.Second {
		t.Errorf("PerCompensationTTL should default to 30s, got %v", m.config.PerCompensationTTL)
	}
	if m.config.TotalTimeout != 5*time.Minute {
		t.Errorf("TotalTimeout should default to 5min, got %v", m.config.TotalTimeout)
	}
}

// TestCompensableInterface_Assertion 验证接口设计
//
// 业界 pattern：能力探测用 interface assertion，避免类型强耦合。
func TestCompensableInterface_Assertion(t *testing.T) {
	var _ NodeExecutor = (*mockCompensableExecutor)(nil)
	var _ Compensable = (*mockCompensableExecutor)(nil)

	var _ NodeExecutor = (*mockNonCompensableExecutor)(nil)
}
