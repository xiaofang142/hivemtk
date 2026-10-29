package service

// workflow_egolite_node.go —— S-10：workflow 的 egolite_run 节点。
//
// 定位：把"跑一次 ego 资产/流程"接进 workflow 编排（cron 触发与 AI 工具已有，
// 这是第三种触发面）。节点本身**不做执行**：它只把参数收敛成 egolite.StartRequest
// 交给执行服务，并在产物里留下执行号/执行键，供下游 condition 与人工回查。
//
// 三条纪律（与 SOP 的 reach_send 节点同口径）：
//  1. 未装配 ⇒ fail-closed（不能"看起来跑过了"）；
//  2. 幂等：产物里带 `egolite_run:<execID>` 副作用键，重跑同一节点不会重复起执行；
//  3. 超时/在途上限这类"现在跑不了"的失败标 retryable，配置类失败不重试。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"hivemtk-user/internal/model"
)

// WorkflowEgoLiteStartRequest workflow 侧的执行入参（与 egolite.StartRequest 同形，
// 由装配层适配器转换——根 service 不能 import egolite：egolite→pkg/cron→email→service 成环）。
type WorkflowEgoLiteStartRequest struct {
	Origin        string
	FlowID        uint
	AssetID       string
	AssetVersion  string
	Entry         string
	Params        []byte
	SpaceName     string
	ConfirmPolicy string
	ConfirmTTLSec int
	TimeoutSec    int
}

// WorkflowEgoLiteExecution 执行服务的返回面（只留 workflow 需要的三列）。
type WorkflowEgoLiteExecution struct {
	ID      uint
	Status  string
	ExecKey string
}

// WorkflowEgoLiteRunner 执行服务对 workflow 暴露的最小面（装配层适配器实现）。
type WorkflowEgoLiteRunner interface {
	Start(ctx context.Context, userID uint, req WorkflowEgoLiteStartRequest) (*WorkflowEgoLiteExecution, error)
}

var (
	workflowEgoLiteMu     sync.RWMutex
	workflowEgoLiteRunner WorkflowEgoLiteRunner
)

// SetWorkflowEgoLiteRunner 装配 egolite 执行服务（router/app 装配期调用；nil 撤装）。
func SetWorkflowEgoLiteRunner(r WorkflowEgoLiteRunner) {
	workflowEgoLiteMu.Lock()
	workflowEgoLiteRunner = r
	workflowEgoLiteMu.Unlock()
}

// GetWorkflowEgoLiteRunner 读当前装配（未装配返回 nil）。
func GetWorkflowEgoLiteRunner() WorkflowEgoLiteRunner {
	workflowEgoLiteMu.RLock()
	defer workflowEgoLiteMu.RUnlock()
	return workflowEgoLiteRunner
}

// EgoLiteRunNodeExecutor workflow 的 egolite_run 节点。
type EgoLiteRunNodeExecutor struct{}

// NodeType 节点类型名（流程图与前端同词表）。
func (e *EgoLiteRunNodeExecutor) NodeType() string { return "egolite_run" }

// IsAsync 节点下发即返回（执行在 runner 侧异步完成，状态由执行服务跟踪）。
func (e *EgoLiteRunNodeExecutor) IsAsync() bool { return false }

// Execute 组装 StartRequest 并下发。
//
// 节点配置（NodeConfig）：
//
//	origin          draft | market（必填）
//	flow_id         draft 必填
//	asset_id        market 必填（slug）
//	asset_version   market 可选
//	entry           market 必填
//	params          对象，透传（服务端会按资产 params_schema 校验）
//	space_name      可选
//	confirm_policy  none | require（默认 require——外发类动作不默认放行）
//	confirm_ttl_sec 可选
//	timeout_sec     可选
func (e *EgoLiteRunNodeExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	if wctx == nil || wctx.Execution == nil {
		return nil, fmt.Errorf("egolite_run: 执行上下文不完整")
	}
	runner := GetWorkflowEgoLiteRunner()
	if runner == nil {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: "egolite_run: 执行服务未装配（装配点未接 egolite.EgoExecutionService）",
		}, nil
	}
	cfg := wctx.NodeConfig
	req := WorkflowEgoLiteStartRequest{
		Origin:        cfgString(cfg, "origin"),
		AssetID:       cfgString(cfg, "asset_id"),
		AssetVersion:  cfgString(cfg, "asset_version"),
		Entry:         cfgString(cfg, "entry"),
		SpaceName:     cfgString(cfg, "space_name"),
		ConfirmPolicy: cfgString(cfg, "confirm_policy"),
	}
	if id, ok := cfgInt(cfg, "flow_id"); ok {
		req.FlowID = uint(id)
	}
	if v, ok := cfgInt(cfg, "confirm_ttl_sec"); ok {
		req.ConfirmTTLSec = int(v)
	}
	if v, ok := cfgInt(cfg, "timeout_sec"); ok {
		req.TimeoutSec = int(v)
	}
	if req.Origin != "draft" && req.Origin != "market" {
		return egoliteRunFailure(fmt.Sprintf("egolite_run: origin 必须是 draft 或 market（实得 %q）", req.Origin)), nil
	}
	if req.Origin == "draft" && req.FlowID == 0 {
		return egoliteRunFailure("egolite_run: origin=draft 时 flow_id 必填"), nil
	}
	if req.Origin == "market" && (req.AssetID == "" || req.Entry == "") {
		return egoliteRunFailure("egolite_run: origin=market 时 asset_id 与 entry 必填"), nil
	}
	if req.ConfirmPolicy == "" {
		req.ConfirmPolicy = "require"
	}
	if req.ConfirmPolicy != "none" && req.ConfirmPolicy != "require" {
		return egoliteRunFailure(fmt.Sprintf("egolite_run: confirm_policy 非法（%q）", req.ConfirmPolicy)), nil
	}
	if raw, ok := cfg["params"]; ok && raw != nil {
		b, err := json.Marshal(raw)
		if err != nil {
			return egoliteRunFailure(fmt.Sprintf("egolite_run: params 序列化失败: %v", err)), nil
		}
		req.Params = b
	}

	// 执行归属人：节点配置优先，其次触发载荷，再次已合并的运行上下文
	//（browser_session / browser_task 动作只认 NodeConfig["user_id"]，这里多两级回落
	// 是因为定时触发与 Webhook 触发没有「当前登录用户」，只能由载荷携带）。
	// 三者都空 ⇒ fail-closed：执行必须挂在某个真实用户名下，猜一个人等于越权。
	userID := egoliteRunUserID(cfg, wctx)
	if userID == 0 {
		return egoliteRunFailure("egolite_run: 缺少 user_id（节点配置 / 触发载荷 / 运行上下文均未提供）"), nil
	}

	// 幂等：本节点已起过执行 ⇒ 复用，不再起第二次。
	// 判据取副作用账（workflow_node_executor.go 的 wf_<execID>_<nodeID>_<action> 键，节点级），
	// 而不是 ctx 里的 egolite_execution_id——后者是把所有节点输出合并后的全局键，
	// 一条流程里放两个 egolite_run 节点时会被上一个节点覆盖，用它判重等于让第二个节点静默不跑。
	if id := egoliteRunExistingID(wctx); id != "" {
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"egolite_execution_id": id, "egolite_reused": true},
			SideEffects: []string{egoliteRunSideEffectKey(wctx, id)},
		}, nil
	}

	exec, err := runner.Start(ctx, userID, req)
	if err != nil {
		// 在途上限/超时属于"现在跑不了"，留重试；参数类失败不重试。
		retryable := isTransientEgoLiteErr(err)
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("egolite_run: %v", err),
			Retryable:    retryable,
		}, nil
	}
	if exec == nil {
		return egoliteRunFailure("egolite_run: 执行服务返回空执行号（不能当作已跑）"), nil
	}
	execKey := exec.ExecKey
	if execKey == "" {
		execKey = fmt.Sprintf("ego-%d", exec.ID)
	}
	return &WorkflowNodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{
			"egolite_execution_id": strconv.FormatUint(uint64(exec.ID), 10),
			"egolite_exec_key":     execKey,
			"egolite_status":       exec.Status,
		},
		SideEffects: []string{egoliteRunSideEffectKey(wctx, strconv.FormatUint(uint64(exec.ID), 10))},
	}, nil
}

func egoliteRunFailure(msg string) *WorkflowNodeExecResult {
	return &WorkflowNodeExecResult{Status: NodeStatusFailed, ErrorMessage: msg}
}

// egoliteRunUserID 执行归属人：NodeConfig["user_id"] → TriggerPayload["user_id"] → ctx["user_id"]。
// 三处都不给就返回 0，由调用方 fail-closed（宁可节点报配置错，也不替任何人起执行）。
func egoliteRunUserID(cfg model.JSONMap, wctx *WorkflowExecContext) uint {
	if v, ok := cfgInt(cfg, "user_id"); ok && v > 0 {
		return uint(v)
	}
	for _, m := range []model.JSONMap{wctx.Input, wctx.Context} {
		if m == nil {
			continue
		}
		if v, ok := cfgInt(m, "user_id"); ok && v > 0 {
			return uint(v)
		}
		if s, ok := m["user_id"].(string); ok {
			if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil && n > 0 {
				return uint(n)
			}
		}
	}
	return 0
}

// egoliteRunSideEffectPrefix 本节点副作用键前缀：wf_<execID>_<nodeID>_egolite_run:
// （沿用 WorkflowSideEffectKey 的节点级键，见 workflow_node_executor.go）
func egoliteRunSideEffectPrefix(wctx *WorkflowExecContext) string {
	return WorkflowSideEffectKey(wctx, "egolite_run") + ":"
}

// egoliteRunSideEffectKey 把执行号挂在节点级副作用键上——它既是"已跑过"的证据，
// 也是复用路径回填执行号的唯一来源（键里带 nodeID，节点之间互不串味）。
func egoliteRunSideEffectKey(wctx *WorkflowExecContext, execID string) string {
	return egoliteRunSideEffectPrefix(wctx) + execID
}

// egoliteRunExistingID 本节点已落账的执行号。
//
// 只认副作用账，不认 ctx["egolite_execution_id"]：后者是 dispatcher 合并所有节点输出后的
// 全局键，一条流程里两个 egolite_run 节点会互相覆盖，用它判重会让第二个节点静默不跑。
func egoliteRunExistingID(wctx *WorkflowExecContext) string {
	data := wctx.Context
	if data == nil && wctx.Execution != nil {
		data = wctx.Execution.Context
	}
	prefix := egoliteRunSideEffectPrefix(wctx)
	for _, e := range extractWorkflowSideEffects(data) {
		if !strings.HasPrefix(e, prefix) {
			continue
		}
		if id := strings.TrimPrefix(e, prefix); id != "" && id != "0" {
			return id
		}
	}
	return ""
}

// isTransientEgoLiteErr 只有"资源/时序"类失败可重试；配置与权限类不重试。
// 「ego-runner 未连接 / 在途上限 / 超时」都是等一会儿就能好的——留重试；
// 参数非法、资产不可用这类改配置才能好的，重试只会把同一错误刷满重试预算。
func isTransientEgoLiteErr(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"在途", "上限", "超时", "timeout", "runner 不在线", "未连接", "busy"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func cfgString(m model.JSONMap, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func cfgInt(m model.JSONMap, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	}
	return 0, false
}
