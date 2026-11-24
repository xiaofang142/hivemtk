package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/repository"
)

// FA-WF-12：关键字搜索必须走后端跨分页过滤（name/workflow_id LIKE），
// 不能以"已加载当前页"的本地过滤替代。
func TestFAWF12_KeywordSearchServerSidePagination(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	// 造数据：Alpha 报表(wf-alpha)、Beta 订单(wf-beta)、Alpha 明细(wf-alpha-2)，
	// 再造 12 条 Gamma 命中项用于跨页。
	mk := func(name, wfID string) {
		if _, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
			WorkflowID: wfID, Name: name, Definition: workflowTestDefinition(),
		}); err != nil {
			t.Fatalf("CreateVersion(%s,%s): %v", name, wfID, err)
		}
	}
	mk("Alpha 报表", "wf-alpha")
	mk("Beta 订单", "wf-beta")
	mk("Alpha 明细", "wf-alpha-2")
	for i := 0; i < 12; i++ {
		mk("Gamma 任务", "wf-gamma")
	}

	// 按 name 关键字：只命中 2 条 Alpha，total 与返回数一致。
	list, total, err := svc.ListAll(ctx, "", "", "Alpha", 1, 10)
	if err != nil {
		t.Fatalf("ListAll(Alpha): %v", err)
	}
	if total != 2 || int64(len(list)) != 2 {
		t.Fatalf("name=Alpha 应命中 2 条, total=%d len=%d", total, len(list))
	}
	for _, v := range list {
		if v.Name != "Alpha 报表" && v.Name != "Alpha 明细" {
			t.Fatalf("name=Alpha 串入了非 Alpha 版本: %s", v.Name)
		}
	}

	// 按 workflow_id 关键字：命中 wf-beta 1 条。
	list, total, err = svc.ListAll(ctx, "", "", "wf-beta", 1, 10)
	if err != nil {
		t.Fatalf("ListAll(wf-beta): %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Name != "Beta 订单" {
		t.Fatalf("workflow_id=wf-beta 应命中 Beta 订单 1 条, total=%d list=%+v", total, list)
	}

	// 跨页：Gamma 关键字命中 12 条，page1=10 条、page2=2 条，total=12。
	p1, total, err := svc.ListAll(ctx, "", "", "Gamma", 1, 10)
	if err != nil {
		t.Fatalf("ListAll(Gamma p1): %v", err)
	}
	if total != 12 || len(p1) != 10 {
		t.Fatalf("Gamma page1 应 10 条 total=12, 实际 len=%d total=%d", len(p1), total)
	}
	p2, total2, err := svc.ListAll(ctx, "", "", "Gamma", 2, 10)
	if err != nil {
		t.Fatalf("ListAll(Gamma p2): %v", err)
	}
	if total2 != 12 || len(p2) != 2 {
		t.Fatalf("Gamma page2 应 2 条 total=12, 实际 len=%d total=%d", len(p2), total2)
	}

	// 空 keyword 返回全部（15 条）。
	_, total, err = svc.ListAll(ctx, "", "", "", 1, 100)
	if err != nil {
		t.Fatalf("ListAll(empty): %v", err)
	}
	if total != 15 {
		t.Fatalf("空 keyword 应返回全部 15 条, 实际 total=%d", total)
	}
}
