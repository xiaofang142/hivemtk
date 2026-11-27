package service

import (
	"fmt"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

// salesGenerateDispatchCallContext 返回销售生成场景的 canonical 续接键与幂等键。
// 同一个总线 session 下 sales_generate 拥有独立状态行，不与其他执行场景串链。
func salesGenerateDispatchCallContext(req *SalesRequest, step string) (sessionRef, idempotencyKey string) {
	if req == nil {
		return "", ""
	}
	sessionRef = strings.TrimSpace(req.BusLLMSessionID)
	messageID := strings.TrimSpace(req.MessageID)
	if view := req.ContextView; view != nil {
		if key := validatedLLMSessionRef(view, view.Scope.BusLLMSessionID); key != "" {
			sessionRef = key
		}
		if id := strings.TrimSpace(view.Current.MessageID); id != "" {
			messageID = id
		}
	}
	if messageID == "" {
		messageID = strings.TrimSpace(req.SessionID)
	}
	if messageID == "" {
		return sessionRef, ""
	}
	return sessionRef, llm.BuildRequestIdempotencyKey(messageID, string(agentcontext.ExecutionScenarioSalesGenerate), step)
}

// salesGenerateHistoryMessages 把统一上下文中的可见历史渲染成模型消息。
// 真实消息 ID 与发送者进入内容，供证据引用与群聊归属判断；不可证明可见的历史一律丢弃。
func salesGenerateHistoryMessages(view *agentcontext.AgentConversationContext) []llm.ChatMessage {
	if view == nil || len(view.Recent) == 0 {
		return nil
	}
	out := make([]llm.ChatMessage, 0, len(view.Recent))
	for _, msg := range view.Recent {
		if !msg.Visibility.IsProven() {
			continue
		}
		if msg.Visibility == agentcontext.VisibilityPrivate && msg.SenderID != view.Current.SenderID {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "assistant", "ai", "agent", "bot":
			role = "assistant"
		case "system":
			continue
		default:
			role = "user"
		}
		var sb strings.Builder
		if msg.MessageID != "" {
			sb.WriteString("[")
			sb.WriteString(msg.MessageID)
			sb.WriteString("] ")
		}
		if msg.SenderID != "" {
			sb.WriteString(msg.SenderID)
			sb.WriteString("：")
		}
		sb.WriteString(msg.Content)
		out = append(out, llm.ChatMessage{Role: role, Content: sb.String()})
	}
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return out
}

// salesGeneratePlainDispatchRequest 构造无工具销售回复请求。
// 有个性化上下文/总线 session 时禁止走最终答案缓存，只复用输入前缀缓存能力。
// allowRawSessionHistory 决定是否允许回退到未经过统一上下文过滤的 SessionMessage 历史。
// 只有完全没有 ContextView 的存量调用才允许；群聊或已装配统一上下文时一律禁止，
// 防止绕过 audience/visibility 过滤把其他成员私有消息送进模型。
func allowRawSessionHistory(req *SalesRequest) bool {
	return req != nil && req.ContextView == nil
}

// renderContextHistoryForPrompt 把已过滤的统一上下文历史渲染成提示词文本。
// 真实 MessageID 与发送者一并写入，便于证据归属；不引入任何未证明可见的消息。
func renderContextHistoryForPrompt(view *agentcontext.AgentConversationContext) string {
	messages := salesGenerateHistoryMessages(view)
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n【对话历史（按时间顺序，共 %d 条）】\n", len(messages)))
	for _, msg := range messages {
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func salesGeneratePlainDispatchRequest(req *SalesRequest, scenario llm.DispatchScenario, prompt, systemPrompt string) llm.DispatchRequest {
	result := llm.DispatchRequest{
		Scenario:     scenario,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
	}
	if req.Config != nil {
		result.MaxTokens = req.Config.MaxTokens
		result.Temperature = req.Config.Temperature
	}
	if req.ContextView == nil && strings.TrimSpace(req.BusLLMSessionID) == "" {
		result.CacheKey = llm.CacheKeyWithSystem(scenario, systemPrompt, prompt)
		result.CacheTTL = 3600
		return result
	}
	sessionRef, idem := salesGenerateDispatchCallContext(req, "generate")
	result.ExecutionScenarioID = string(agentcontext.ExecutionScenarioSalesGenerate)
	result.LLMSessionRef = sessionRef
	result.RequestIdempotencyKey = idem
	result.StrictOutput = true
	return result
}

// salesGenerateRetryBudgetOnTruncation 只在 finish_reason=length 且输出被截断时
// 加倍输出预算重试一次，避免把半截回复当最终答案。
func salesGenerateRetryMaxTokens(current int, finishReason, content string, retried bool) (int, bool) {
	if retried || finishReason != "length" || strings.TrimSpace(content) == "" {
		return current, true
	}
	next := current * 2
	if next <= current {
		return current, true
	}
	return next, false
}
