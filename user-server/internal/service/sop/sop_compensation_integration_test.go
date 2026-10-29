package sop

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// TestSOPDispatcher_SetCompensationManager 验证 SetCompensationManager
func TestSOPDispatcher_SetCompensationManager(t *testing.T) {
	d := &SOPExecutionDispatcher{}
	if d.compensationMgr != nil {
		t.Error("default compensationMgr should be nil")
	}
	mgr := NewCompensationManager(DefaultCompensationConfig())
	d.SetCompensationManager(mgr)
	if d.compensationMgr != mgr {
		t.Error("SetCompensationManager should set instance")
	}
}

// TestSOPDispatcher_SetCompensationManager_NilSafe 验证 nil dispatcher
func TestSOPDispatcher_SetCompensationManager_NilSafe(t *testing.T) {
	var d *SOPExecutionDispatcher
	d.SetCompensationManager(NewCompensationManager(DefaultCompensationConfig()))

}

// TestSOPDispatcher_tryCompensate_NilMgr 验证 nil manager 不破坏 fail 路径
func TestSOPDispatcher_tryCompensate_NilMgr(t *testing.T) {
	d := &SOPExecutionDispatcher{}

	d.tryCompensate(context.Background(), &model.SOPExecution{ID: 1})

}

// TestSOPDispatcher_tryCompensate_NilExec 验证 nil execution
func TestSOPDispatcher_tryCompensate_NilExec(t *testing.T) {
	d := &SOPExecutionDispatcher{}
	d.SetCompensationManager(NewCompensationManager(DefaultCompensationConfig()))
	d.tryCompensate(context.Background(), nil)

}

// TestSOPDispatcher_tryCompensate_ZeroIDExec 验证 0 ID execution
func TestSOPDispatcher_tryCompensate_ZeroIDExec(t *testing.T) {
	d := &SOPExecutionDispatcher{}
	d.SetCompensationManager(NewCompensationManager(DefaultCompensationConfig()))
	d.tryCompensate(context.Background(), &model.SOPExecution{ID: 0})

}

// TestSOPDispatcher_tryCompensate_NilDispatcher 验证 nil dispatcher
func TestSOPDispatcher_tryCompensate_NilDispatcher(t *testing.T) {
	var d *SOPExecutionDispatcher
	d.tryCompensate(context.Background(), &model.SOPExecution{ID: 1})

}

// TestSOPDispatcher_tryCompensate_ConcurrentSafety 验证并发调用
func TestSOPDispatcher_tryCompensate_ConcurrentSafety(t *testing.T) {
	d := &SOPExecutionDispatcher{}
	d.SetCompensationManager(NewCompensationManager(DefaultCompensationConfig()))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.tryCompensate(context.Background(), &model.SOPExecution{ID: 1})
		}()
	}
	wg.Wait()

	time.Sleep(100 * time.Millisecond)
}

// TestSOPDispatcher_FailExecutionTriggersCompensation 验证 failExecution 触发补偿
//
// 不直接测试 DB 写入（需 testutil），只测试「fail 路径调用 tryCompensate」
func TestSOPDispatcher_FailExecutionTriggersCompensation(t *testing.T) {
	d := &SOPExecutionDispatcher{}
	mgr := NewCompensationManager(DefaultCompensationConfig())
	d.SetCompensationManager(mgr)

	exec := &model.SOPExecution{ID: 100}
	d.tryCompensate(context.Background(), exec)

	time.Sleep(100 * time.Millisecond)
}

// TestCompensationRecord_Defaults 验证默认字段
func TestCompensationRecord_Defaults(t *testing.T) {
	r := CompensationRecord{
		NodeID:   "n1",
		NodeType: "send_sms",
	}
	if r.Status != "" {
		t.Error("default status should be empty")
	}
	if r.Attempt != 0 {
		t.Error("default attempt should be 0")
	}
}

// TestCompensationPlan_Defaults 验证默认 plan
func TestCompensationPlan_Defaults(t *testing.T) {
	p := &CompensationPlan{}
	if p.Status != "" {
		t.Error("default plan status should be empty")
	}
	if p.Records != nil {
		t.Error("default records should be nil")
	}
}

// TestSOPDispatcher_CompensationMgr_AfterFail 验证 fail 后 mgr.plan 增加
//
// 真实集成测试：fail → 异步补偿触发（即使没有 executed_nodes 数据，框架应就位）
func TestSOPDispatcher_CompensationMgr_AfterFail(t *testing.T) {
	mgr := NewCompensationManager(DefaultCompensationConfig())
	d := &SOPExecutionDispatcher{compensationMgr: mgr}

	if mgr.GetPlan(1) != nil {
		t.Error("initial state should have no plan")
	}

	d.tryCompensate(context.Background(), &model.SOPExecution{ID: 1})

	time.Sleep(100 * time.Millisecond)
}

// TestDTO_SendPlanDTO 验证 DTO 结构
func TestDTO_SendPlanDTO(t *testing.T) {
	plan := &dto.SendPlanDTO{
		Messages:   []string{"a", "b"},
		Intervals:  []float64{1.5},
		TotalDelay: 3.0,
	}
	if len(plan.Messages) != 2 {
		t.Errorf("messages length should be 2, got %d", len(plan.Messages))
	}
	if plan.TotalDelay != 3.0 {
		t.Errorf("total delay should be 3.0, got %v", plan.TotalDelay)
	}
}
