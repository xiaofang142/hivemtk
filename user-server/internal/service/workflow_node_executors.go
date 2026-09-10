package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const MaxSubflowDepth = 5

// TriggerNodeExecutor 触发器节点执行器
type TriggerNodeExecutor struct{}

func (e *TriggerNodeExecutor) NodeType() string { return WorkflowNodeTypeTrigger }

func (e *TriggerNodeExecutor) IsAsync() bool { return false }

func (e *TriggerNodeExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	triggeredAt := time.Now().Format(time.RFC3339)
	output := model.JSONMap{
		"_triggered_at": triggeredAt,
	}
	if wctx.Input != nil {
		for k, v := range wctx.Input {
			output[k] = v
		}
	}
	if wctx.Execution != nil && wctx.Execution.TriggerPayload != nil {
		for k, v := range wctx.Execution.TriggerPayload {
			if _, exists := output[k]; !exists {
				output[k] = v
			}
		}
	}
	return &WorkflowNodeExecResult{
		Status:      NodeStatusCompleted,
		Output:      output,
		NextNodeID:  "",
		SideEffects: []string{WorkflowSideEffectKey(wctx, "triggered")},
	}, nil
}

// ActionNodeExecutor 动作节点执行器
type ActionNodeExecutor struct{}

func (e *ActionNodeExecutor) NodeType() string { return WorkflowNodeTypeAction }

func (e *ActionNodeExecutor) IsAsync() bool { return false }

func (e *ActionNodeExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	actionType := ""
	if wctx.NodeConfig != nil {
		if v, ok := wctx.NodeConfig["action_type"].(string); ok {
			actionType = v
		}
	}

	sideEffectKey := WorkflowSideEffectKey(wctx, "action_"+actionType)
	if HasWorkflowSideEffect(wctx, sideEffectKey) {
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"_already_executed": true},
			NextNodeID:  "",
			SideEffects: []string{sideEffectKey},
		}, nil
	}

	switch actionType {
	case "log":
		message := ""
		if wctx.NodeConfig != nil {
			if v, ok := wctx.NodeConfig["message"].(string); ok {
				message = v
			}
		}
		logger.Ctx(ctx).Info().Str("workflow_action", "log").Str("message", message).Msg("ActionNodeExecutor: log")
		AppendWorkflowSideEffect(wctx, sideEffectKey)
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"message": message, "_action_type": "log"},
			NextNodeID:  "",
			SideEffects: []string{sideEffectKey},
		}, nil

	case "http":
		url := ""
		if wctx.NodeConfig != nil {
			if v, ok := wctx.NodeConfig["url"].(string); ok {
				url = v
			}
		}
		if url == "" {
			url = "http://placeholder"
		}
		logger.Ctx(ctx).Info().Str("workflow_action", "http").Str("url", url).Msg("ActionNodeExecutor: http (mock)")
		AppendWorkflowSideEffect(wctx, sideEffectKey)
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"url": url, "_action_type": "http", "_mock": true},
			NextNodeID:  "",
			SideEffects: []string{sideEffectKey, "http_called:" + url},
		}, nil

	case "delay":
		durationMs := 0
		if wctx.NodeConfig != nil {
			switch v := wctx.NodeConfig["duration_ms"].(type) {
			case float64:
				durationMs = int(v)
			case int:
				durationMs = v
			case string:
				fmt.Sscanf(v, "%d", &durationMs)
			}
		}
		if durationMs > 5000 {
			durationMs = 5000
		}
		if durationMs > 0 {
			select {
			case <-time.After(time.Duration(durationMs) * time.Millisecond):
			case <-ctx.Done():
				return &WorkflowNodeExecResult{
					Status:       NodeStatusFailed,
					ErrorMessage: "delay cancelled",
					Retryable:    false,
				}, nil
			}
		}
		waitUntil := time.Now().Add(time.Duration(durationMs) * time.Millisecond)
		AppendWorkflowSideEffect(wctx, sideEffectKey)
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"duration_ms": durationMs, "_action_type": "delay"},
			NextNodeID:  "",
			WaitUntil:   &waitUntil,
			WaitEvent:   WaitEventTimer,
			SideEffects: []string{sideEffectKey},
		}, nil

	case "browser_task":
		taskID, userID := 0, 0
		if wctx.NodeConfig != nil {
			switch v := wctx.NodeConfig["task_id"].(type) {
			case float64:
				taskID = int(v)
			case int:
				taskID = v
			case string:
				fmt.Sscanf(v, "%d", &taskID)
			}
			switch v := wctx.NodeConfig["user_id"].(type) {
			case float64:
				userID = int(v)
			case int:
				userID = v
			case string:
				fmt.Sscanf(v, "%d", &userID)
			}
		}
		if taskID <= 0 {
			return &WorkflowNodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: "browser_task action requires task_id in node config",
				Retryable:    false,
			}, nil
		}
		if workflowBrowserTaskRunner == nil {
			return &WorkflowNodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: "browser task runner not configured",
				Retryable:    false,
			}, nil
		}
		if err := workflowBrowserTaskRunner(ctx, uint(taskID), uint(userID), 0); err != nil {
			return &WorkflowNodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: fmt.Sprintf("browser task %d failed: %v", taskID, err),
				Retryable:    true,
			}, nil
		}
		AppendWorkflowSideEffect(wctx, sideEffectKey)
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"task_id": taskID, "user_id": userID, "_action_type": "browser_task"},
			NextNodeID:  "",
			SideEffects: []string{sideEffectKey},
		}, nil

	default:
		logger.Ctx(ctx).Warn().Str("action_type", actionType).Msg("ActionNodeExecutor: unknown action_type, using noop")
		noop := &WorkflowNoopExecutor{nodeType: WorkflowNodeTypeAction}
		return noop.Execute(ctx, wctx)
	}
}

// workflowBrowserTaskRunner browser_task 动作执行注入点
// （由 router 装配层注入 TaskService.RunTaskWithRetry，避免 service → browser_automation/service 循环依赖）
var workflowBrowserTaskRunner func(ctx context.Context, taskID, userID uint, retryCount int) error

// SetWorkflowBrowserTaskRunner 注入 browser_task 动作执行函数
func SetWorkflowBrowserTaskRunner(fn func(ctx context.Context, taskID, userID uint, retryCount int) error) {
	workflowBrowserTaskRunner = fn
}

// ConditionNodeExecutor 条件节点执行器
type ConditionNodeExecutor struct{}

func (e *ConditionNodeExecutor) NodeType() string { return WorkflowNodeTypeCondition }

func (e *ConditionNodeExecutor) IsAsync() bool { return false }

func (e *ConditionNodeExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	rules := []model.JSONMap{}
	if wctx.NodeConfig != nil {
		if v, ok := wctx.NodeConfig["rules"].([]any); ok {
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					rules = append(rules, m)
				}
			}
		}
	}

	sideEffectKey := WorkflowSideEffectKey(wctx, "condition")
	if HasWorkflowSideEffect(wctx, sideEffectKey) {
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"_already_executed": true},
			NextNodeID:  "",
			SideEffects: []string{sideEffectKey},
		}, nil
	}

	for _, rule := range rules {
		field := getString(rule, "field")
		op := getString(rule, "op")
		value := rule["value"]
		branch := getString(rule, "branch")

		if field == "" || op == "" {
			continue
		}

		ctxVal := wctx.Context[field]
		matched := evaluateCondition(ctxVal, op, value)
		if matched {
			nextNode := NextWorkflowNode(wctx.Graph, wctx.NodeID, branch)
			if nextNode == nil {
				logger.Ctx(ctx).Warn().
					Str("branch", branch).
					Str("node_id", wctx.NodeID).
					Msg("ConditionNodeExecutor: branch target not found in graph, falling back to default")
			} else {
				AppendWorkflowSideEffect(wctx, sideEffectKey)
				return &WorkflowNodeExecResult{
					Status:      NodeStatusCompleted,
					Output:      model.JSONMap{"matched_rule": rule, "_condition_branch": branch},
					NextNodeID:  nextNode.ID,
					SideEffects: []string{sideEffectKey},
				}, nil
			}
		}
	}

	defaultNode := NextWorkflowNode(wctx.Graph, wctx.NodeID, "")
	AppendWorkflowSideEffect(wctx, sideEffectKey)
	if defaultNode == nil {
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"matched": false, "_condition_branch": "end"},
			NextNodeID:  "_end_",
			SideEffects: []string{sideEffectKey},
		}, nil
	}
	return &WorkflowNodeExecResult{
		Status:      NodeStatusCompleted,
		Output:      model.JSONMap{"matched": false, "_condition_branch": "default"},
		NextNodeID:  defaultNode.ID,
		SideEffects: []string{sideEffectKey},
	}, nil
}

func wfGetString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func evaluateCondition(ctxVal any, op string, target any) bool {
	if ctxVal == nil {
		return false
	}
	switch op {
	case "eq":
		return fmt.Sprintf("%v", ctxVal) == fmt.Sprintf("%v", target)
	case "ne":
		return fmt.Sprintf("%v", ctxVal) != fmt.Sprintf("%v", target)
	case "gt":
		return compareNumber(ctxVal, target) > 0
	case "lt":
		return compareNumber(ctxVal, target) < 0
	case "contains":
		ctxStr := fmt.Sprintf("%v", ctxVal)
		targetStr := fmt.Sprintf("%v", target)
		return strings.Contains(ctxStr, targetStr)
	default:
		return false
	}
}

func compareNumber(a, b any) int {
	fa, ok1 := toFloat64(a)
	fb, ok2 := toFloat64(b)
	if !ok1 || !ok2 {
		return 0
	}
	if fa < fb {
		return -1
	}
	if fa > fb {
		return 1
	}
	return 0
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

// SubflowNodeExecutor 子流程节点执行器
type SubflowNodeExecutor struct {
	orchestrator *WorkflowOrchestratorService
}

func NewSubflowNodeExecutor(orch *WorkflowOrchestratorService) *SubflowNodeExecutor {
	return &SubflowNodeExecutor{orchestrator: orch}
}

func (e *SubflowNodeExecutor) NodeType() string { return WorkflowNodeTypeSubflow }

func (e *SubflowNodeExecutor) IsAsync() bool { return false }

func (e *SubflowNodeExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	subWorkflowID := ""
	if wctx.NodeConfig != nil {
		if v, ok := wctx.NodeConfig["sub_workflow_id"].(string); ok {
			subWorkflowID = v
		}
	}
	if subWorkflowID == "" {
		return &WorkflowNodeExecResult{Status: NodeStatusFailed, Output: model.JSONMap{"error": "sub_workflow_id required"}}, nil
	}
	depth := 0
	if wctx.Execution != nil {
		if d, ok := wctx.Execution.TriggerPayload["_subflow_depth"].(int); ok {
			depth = d
		}
	}
	if depth >= MaxSubflowDepth {
		return &WorkflowNodeExecResult{Status: NodeStatusFailed, Output: model.JSONMap{"error": fmt.Sprintf("subflow max depth %d exceeded", MaxSubflowDepth)}}, nil
	}
	sideEffectKey := WorkflowSideEffectKey(wctx, "subflow:"+subWorkflowID)
	if HasWorkflowSideEffect(wctx, sideEffectKey) {
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"_already_executed": true, "sub_workflow_id": subWorkflowID},
			SideEffects: []string{sideEffectKey},
		}, nil
	}
	AppendWorkflowSideEffect(wctx, sideEffectKey)
	if e.orchestrator != nil {
		payload := model.JSONMap{}
		if wctx.Execution != nil && wctx.Execution.TriggerPayload != nil {
			payload = wctx.Execution.TriggerPayload
		}
		payload["_subflow_depth"] = depth + 1
		exec, err := e.orchestrator.Execute(ctx, &dto.WorkflowExecuteRequest{WorkflowID: subWorkflowID, TriggerPayload: payload})
		if err != nil {
			return &WorkflowNodeExecResult{Status: NodeStatusFailed, Output: model.JSONMap{"error": err.Error(), "sub_workflow_id": subWorkflowID}}, nil
		}
		return &WorkflowNodeExecResult{
			Status:      NodeStatusCompleted,
			Output:      model.JSONMap{"sub_workflow_id": subWorkflowID, "execution_id": exec.ID, "status": exec.Status},
			SideEffects: []string{sideEffectKey},
		}, nil
	}
	logger.Ctx(ctx).Warn().Str("sub_workflow_id", subWorkflowID).Msg("SubflowNodeExecutor: orchestrator not injected, skipping")
	return &WorkflowNodeExecResult{
		Status:      NodeStatusCompleted,
		Output:      model.JSONMap{"sub_workflow_id": subWorkflowID, "_subflow_invoked": subWorkflowID, "_skipped_reason": "orchestrator not injected"},
		SideEffects: []string{sideEffectKey},
	}, nil
}
