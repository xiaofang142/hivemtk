// workflow_run_test.go 工作流执行记录的落库语义。
//
// 执行记录是排障唯一凭据：无论成功、失败，还是请求方中途断开，终态都必须落库。
package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/repository"

	"gorm.io/gorm"
)

func TestWorkflowService_RunPersistsTerminalStatus(t *testing.T) {
	db := geoTestDB(t)
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepositoryWithDB(db),
		repository.NewGeoContentTaskRepositoryWithDB(db),
		nil,
	)
	ctx := context.Background()

	okWF, err := svc.Create(ctx, &dto.SaveWorkflowRequest{
		Name:    "成功工作流",
		Steps:   []dto.WorkflowStep{{Name: "火", Type: "trigger"}, {Name: "收尾", Type: "end"}},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建成功工作流失败: %v", err)
	}
	resp, err := svc.Run(ctx, okWF.ID)
	if err != nil {
		t.Fatalf("Run 应成功，实际: %v", err)
	}
	if resp.Status != "success" || resp.CompletedAt == nil {
		t.Fatalf("执行结果不对: status=%q completed=%v", resp.Status, resp.CompletedAt)
	}
	execs, err := svc.ListExecutions(ctx, okWF.ID)
	if err != nil {
		t.Fatalf("查执行记录失败: %v", err)
	}
	if len(execs) != 1 || execs[0].ID != resp.ID {
		t.Fatalf("执行记录未落库或对不上: %+v", execs)
	}
	if execs[0].Status != "success" || execs[0].CompletedAt == nil || execs[0].Result == "" {
		t.Fatalf("执行记录终态未落库: %+v", execs[0])
	}

	// 无执行器的步骤类型：Run 返回 result + err（控制器按 result.status 渲染），
	// 执行记录必须以 failed + error 落库。
	badWF, err := svc.Create(ctx, &dto.SaveWorkflowRequest{
		Name:    "未知步骤工作流",
		Steps:   []dto.WorkflowStep{{Name: "未知", Type: "no_such_executor"}},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建未知步骤工作流失败: %v", err)
	}
	resp, runErr := svc.Run(ctx, badWF.ID)
	if runErr == nil {
		t.Fatal("未知步骤类型应报错")
	}
	if resp == nil || resp.Status != "failed" {
		t.Fatalf("失败执行也须返回结果: %+v", resp)
	}
	execs, err = svc.ListExecutions(ctx, badWF.ID)
	if err != nil {
		t.Fatalf("查失败执行记录失败: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("失败执行记录条数 = %d，期望 1", len(execs))
	}
	if execs[0].Status != "failed" || execs[0].Error == "" || execs[0].CompletedAt == nil {
		t.Fatalf("失败终态未落库: %+v", execs[0])
	}

	// 不存在的 workflow：不建执行记录、直接报不存在。
	if resp, err := svc.Run(ctx, "no-such-workflow"); resp != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不存在的 workflow 应返回 nil + ErrRecordNotFound: resp=%v err=%v", resp, err)
	}
}

// TestWorkflowService_Run_请求方断开仍落终态：请求 ctx 在执行过程中被取消时，
// 延迟的终态更新必须仍然写进去（否则执行记录永远停在 running）。
func TestWorkflowService_Run_请求方断开仍落终态(t *testing.T) {
	db := geoTestDB(t)
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepositoryWithDB(db),
		repository.NewGeoContentTaskRepositoryWithDB(db),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.RegisterExecutor("cancel_ctx", func(_ context.Context, _ map[string]interface{}) (string, error) {
		cancel() // 模拟客户端在步骤执行期间断开
		return "partial", nil
	})

	wf, err := svc.Create(context.Background(), &dto.SaveWorkflowRequest{
		Name:    "断连工作流",
		Steps:   []dto.WorkflowStep{{Name: "断连", Type: "cancel_ctx"}},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建工作流失败: %v", err)
	}

	if _, err := svc.Run(ctx, wf.ID); err != nil {
		t.Fatalf("Run 不应因 ctx 取消失败: %v", err)
	}

	execs, err := svc.ListExecutions(context.Background(), wf.ID)
	if err != nil {
		t.Fatalf("查执行记录失败: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("执行记录条数 = %d，期望 1", len(execs))
	}
	if execs[0].Status == "running" {
		t.Fatal("请求方断开后执行记录停在 running：终态更新被取消信号带走了")
	}
}
