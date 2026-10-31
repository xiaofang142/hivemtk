package app

import (
	"context"
	"encoding/json"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// ToolExecutorAdapter 适配器实现
type ToolExecutorAdapter struct {
	executor *tooluse.ToolExecutor
}

// NewToolExecutorAdapter 创建适配器
func NewToolExecutorAdapter(executor *tooluse.ToolExecutor) *ToolExecutorAdapter {
	return &ToolExecutorAdapter{executor: executor}
}

// ListTools 实现 service.AgentToolExecutor.ListTools
// 将 tooluse.LLMFunction 转为 service.AgentToolDef；并入当前请求租户自己的
// custom_tools / mcp_servers 工具（多租户隔离，见 tooluse/tenant_registry.go）。
func (a *ToolExecutorAdapter) ListTools(ctx context.Context) []service.AgentToolDef {
	if a.executor == nil {
		return nil
	}
	fns := a.executor.ListAvailableLLMFunctionsForTenant(ctx)
	out := make([]service.AgentToolDef, 0, len(fns))
	for _, fn := range fns {
		paramsMap, err := structToMap(fn.Parameters)
		if err != nil {
			logger.Errorf("[ToolExecutorAdapter] 跳过工具 %s：参数序列化失败 err=%v", fn.Name, err)
			continue
		}
		out = append(out, service.AgentToolDef{
			Name:        fn.Name,
			Description: fn.Description,
			Parameters:  paramsMap,
		})
	}
	return out
}

// DispatchToolCalls 实现 service.AgentToolExecutor.DispatchToolCalls
// 将 service.AgentToolCall 转为 tooluse.LLMToolCall，调用 executor 并发执行，返回结果
func (a *ToolExecutorAdapter) DispatchToolCalls(ctx context.Context, calls []service.AgentToolCall, toolCtx service.AgentToolContext) []service.AgentToolResult {
	if a.executor == nil || len(calls) == 0 {
		return nil
	}
	llmCalls := make([]tooluse.LLMToolCall, 0, len(calls))
	for _, c := range calls {
		llmCalls = append(llmCalls, tooluse.LLMToolCall{
			ID: c.ID,
			Function: tooluse.LLMToolFunction{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		})
	}
	tooluseCtx := &tooluse.ToolContext{
		AgentID:    toolCtx.AgentID,
		SessionID:  toolCtx.SessionID,
		CustomerID: toolCtx.CustomerID,
		Source:     toolCtx.Source,
	}
	results := a.executor.DispatchByLLMToolCall(ctx, llmCalls, tooluseCtx)
	out := make([]service.AgentToolResult, 0, len(results))
	for _, r := range results {
		out = append(out, service.AgentToolResult{
			ToolCallID: r.ToolCallID,
			Content:    r.Content,
			Success:    r.Success,
			Card:       r.Card,
		})
	}
	return out
}

func structToMap(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{"type": "object"}, nil
	}
	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(bytes, &m); err != nil {
		return nil, err
	}
	return m, nil
}
