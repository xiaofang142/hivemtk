package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
)

// ReAct 取证工具白名单（D18）：只读、无外发、无写库。
const (
	reactToolCustomer360     = "query_customer_360"
	reactToolHistoricalLeads = "query_historical_leads"
	reactToolDictionary      = "query_dictionary"
)

// leadReactMaxRounds ReAct 最多 3 轮 Thought/Action/Observation，超限强制收敛。
const leadReactMaxRounds = 3

// LeadReactTool 是 ReAct 可调用的一个只读取证工具。
type LeadReactTool struct {
	Name        string
	Description string
	Run         func(ctx context.Context, args string) (string, error)
}

// LeadReactResolver 提供取证工具的实际数据访问；未装配的 resolver 返回明确
// 不可用错误（fail-visible），不伪造证据。
type LeadReactResolver struct {
	Customer360     func(ctx context.Context, key string) (string, error)
	HistoricalLeads func(ctx context.Context, sender string) (string, error)
	Dictionary      func(ctx context.Context, term string) (string, error)
}

func defaultLeadReactTools(resolver LeadReactResolver) []LeadReactTool {
	unavailable := errors.New("ReAct 取证工具未装配")
	return []LeadReactTool{
		{Name: reactToolCustomer360, Description: "按身份键查客户360档案，返回角色/标签/历史摘要",
			Run: func(ctx context.Context, args string) (string, error) {
				if resolver.Customer360 == nil {
					return "", unavailable
				}
				return resolver.Customer360(ctx, args)
			}},
		{Name: reactToolHistoricalLeads, Description: "查同一发送人/会话的历史线索与判定结论",
			Run: func(ctx context.Context, args string) (string, error) {
				if resolver.HistoricalLeads == nil {
					return "", unavailable
				}
				return resolver.HistoricalLeads(ctx, args)
			}},
		{Name: reactToolDictionary, Description: "查行业词库/敏感词/上下游信号词",
			Run: func(ctx context.Context, args string) (string, error) {
				if resolver.Dictionary == nil {
					return "", unavailable
				}
				return resolver.Dictionary(ctx, args)
			}},
	}
}

// reactToolDefinitions 把工具白名单转成 LLM DispatchRequest.Tools。
func reactToolDefinitions(tools []LeadReactTool) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		out = append(out, llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunctionSchema{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "查询参数"}}},
			},
		})
	}
	return out
}

// SetLeadReactHook 装配 ReAct 取证钩子（Router 装配点调用；nil 视为关闭，走既有主链）。
// 并发安全：Set 与判定读经 RWMutex 保护，避免 data race。
func SetLeadReactHook(resolver LeadReactResolver, dispatch func(context.Context, []llm.ChatMessage, []llm.ToolDefinition) (string, error)) {
	leadReactHookMu.Lock()
	defer leadReactHookMu.Unlock()
	if dispatch == nil {
		leadReactHook = nil
		return
	}
	tools := defaultLeadReactTools(resolver)
	leadReactHook = func(ctx context.Context, jctx *LeadJudgeContext) ([]string, bool) {
		if jctx == nil {
			return nil, true
		}
		collected, degraded, err := runLeadReactLoop(ctx, tools, dispatch)
		if err != nil {
			return collected, true
		}
		return collected, degraded
	}
}

// InstallLeadReactDispatcher 把 ReAct 取证钩子接到全局 LLM dispatcher（生产装配口）。
// dispatcher 未初始化时不装配（走既有主链，向后兼容）。
func InstallLeadReactDispatcher(resolver LeadReactResolver) {
	dispatcher := llm.GetGlobalDispatcher()
	if dispatcher == nil {
		SetLeadReactHook(resolver, nil)
		return
	}
	SetLeadReactHook(resolver, func(ctx context.Context, messages []llm.ChatMessage, tools []llm.ToolDefinition) (string, error) {
		result, err := dispatcher.Dispatch(ctx, llm.DispatchRequest{
			Scenario:    llm.ScenarioLowCost,
			Messages:    messages,
			Tools:       tools,
			MaxTokens:   512,
			Temperature: 0.2,
		})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	})
}

// runLeadReactLoop 执行 ReAct 取证循环：解析 Action → 调白名单工具 → 回灌 Observation。
// 工具不存在/失败/超轮数都 fail-visible：带证据返回并标记 Degraded，调用方据此
// 降级为既有三路检测主链，而不是中断发掘。
func runLeadReactLoop(ctx context.Context, tools []LeadReactTool, dispatch func(context.Context, []llm.ChatMessage, []llm.ToolDefinition) (string, error)) (collected []string, degraded bool, err error) {
	if dispatch == nil {
		return nil, true, errors.New("ReAct dispatch 未装配")
	}
	byName := make(map[string]LeadReactTool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	messages := []llm.ChatMessage{{Role: "user", Content: "如需取证请按 ReAct 协议输出 Action；证据充分请输出 Final Answer。"}}
	for round := 0; round < leadReactMaxRounds; round++ {
		content, derr := dispatch(ctx, messages, reactToolDefinitions(tools))
		if derr != nil {
			return collected, true, derr
		}
		parsed, perr := llm.ParseReActResponse(content)
		if perr != nil || parsed == nil {
			return collected, true, fmt.Errorf("ReAct 解析失败: %v", perr)
		}
		if parsed.IsFinal {
			return collected, false, nil
		}
		tool, ok := byName[parsed.Action]
		if !ok {
			collected = append(collected, fmt.Sprintf("[工具拒绝] %s 不在白名单", parsed.Action))
			return collected, true, nil
		}
		args := strings.Trim(parsed.ActionInput, `"{} `)
		obs, oerr := tool.Run(ctx, args)
		if oerr != nil {
			collected = append(collected, fmt.Sprintf("[工具失败] %s: %v", tool.Name, oerr))
			return collected, true, nil
		}
		collected = append(collected, fmt.Sprintf("[%s] %s", tool.Name, obs))
		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: content},
			llm.BuildObservationMessage(tool.Name, fmt.Sprintf("call_%d", round), obs),
		)
	}
	return collected, true, nil // 超轮数：带已取证据降级
}
