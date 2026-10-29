package router

// egolite_workflow_node.go —— S-10：workflow 的 egolite_run 节点装配。
//
// 这里是「第四种触发面」的接线点：手跑（controller）、cron（ego_flow_cron）、
// AI 工具（tooluse.SetEgoLiteRunner）、workflow 节点（本文件）。
// 四条腿共用同一个判据：EnsureOnline 判 runner 在线 + 走 execSvc.Start 同一入口
// ——触发方式可以不同，能不能跑、跑谁的，只由执行域说了算。
//
// 为什么放 router 而不是 service：根 service 不能 import egolite
// （egolite → pkg/cron → email → service 成环），service 侧只留
// WorkflowEgoLiteRunner 最小面，转换由装配层适配器完成。

import (
	"context"
	"fmt"

	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/service"
)

// workflowEgoLiteAdapter 把 egolite 执行域收敛成 workflow 节点要的最小面。
type workflowEgoLiteAdapter struct {
	execSvc  *egolite.EgoExecutionService
	registry *egolite.EgoRunnerRegistry
}

// 契约钉在编译期：service 侧接口漂移（加字段/改签名）时这里先红。
var _ service.WorkflowEgoLiteRunner = workflowEgoLiteAdapter{}

// Start 下发一次执行（节点侧已做参数校验，这里只补「谁在跑」的前置）。
func (a workflowEgoLiteAdapter) Start(ctx context.Context, userID uint, req service.WorkflowEgoLiteStartRequest) (*service.WorkflowEgoLiteExecution, error) {
	if userID == 0 {
		return nil, fmt.Errorf("egolite_run 节点缺少 user_id")
	}
	// runner 不在线时 Start 会创建 created 态执行但不下发（M1 不做自动补投），
	// 对 workflow 来说那等于「节点显示完成、实际没跑」——必须在起点拦住。
	if err := a.registry.EnsureOnline(userID); err != nil {
		return nil, err
	}
	e, err := a.execSvc.Start(ctx, userID, egolite.StartRequest{
		Origin:        req.Origin,
		FlowID:        req.FlowID,
		AssetID:       req.AssetID,
		AssetVersion:  req.AssetVersion,
		Entry:         req.Entry,
		Params:        req.Params,
		SpaceName:     req.SpaceName,
		ConfirmPolicy: req.ConfirmPolicy,
		ConfirmTTLSec: req.ConfirmTTLSec,
		TimeoutSec:    req.TimeoutSec,
	})
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("egolite 执行服务返回空执行")
	}
	return &service.WorkflowEgoLiteExecution{
		ID:      e.ID,
		Status:  e.State,
		ExecKey: egolite.EgoExecKey(e.ID),
	}, nil
}
