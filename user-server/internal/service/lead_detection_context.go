package service

import (
	"fmt"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

// LeadDetectionContextSignals 从不可变上下文快照提取线索检测的轻量信号。
// 规则/embedding 快路径也必须消费它，不能以“高置信当轮”为由跳过历史。
func LeadDetectionContextSignals(in DetectionInput) IntentContextSignals {
	text := ""
	if in.ContextView != nil {
		text = in.ContextView.Current.Content
	}
	if strings.TrimSpace(text) == "" && len(in.Messages) > 0 {
		text = in.Messages[len(in.Messages)-1].Content
	}
	return BuildIntentContextSignals(text, in.ContextView)
}

// leadDetectionResolvedContent 返回规则快路径实际用于匹配的文本。
// 只有当前消息使用补全文本，避免把历史主题重复当成多条证据。
func leadDetectionResolvedContent(in DetectionInput, msg DetectionMessage) string {
	if in.ContextView == nil || msg.MessageID != in.ContextView.Current.MessageID {
		return msg.Content
	}
	resolved := LeadDetectionContextSignals(in).ResolvedText
	if strings.TrimSpace(resolved) == "" {
		return msg.Content
	}
	return resolved
}

// BuildLeadDetectionMessages 把检测输入转成模型消息。
// 每条消息带真实 MessageID 锚点；默认不可证明可见或跨成员私有的消息一律不进模型。
func BuildLeadDetectionMessages(in DetectionInput) []llm.ChatMessage {
	visible := leadDetectionVisibleMessages(in)
	out := make([]llm.ChatMessage, 0, len(visible))
	for _, msg := range visible {
		role := "user"
		if msg.Role == "assistant" || msg.Role == "self" {
			role = "assistant"
		}
		content := leadDetectionResolvedContent(in, msg)
		header := fmt.Sprintf("[msg id=%s|sender=%s|role=%s] ", msg.MessageID, msg.SenderID, msg.Role)
		out = append(out, llm.ChatMessage{Role: role, Content: header + content})
	}
	return out
}

func leadDetectionVisibleMessages(in DetectionInput) []DetectionMessage {
	allowed := leadDetectionAllowedMessages(in)
	out := make([]DetectionMessage, 0, len(in.Messages))
	for _, msg := range in.Messages {
		if !allowed[msg.MessageID] {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func leadDetectionAllowedMessages(in DetectionInput) map[string]bool {
	out := make(map[string]bool)
	if in.ContextView == nil {
		for _, msg := range in.Messages {
			out[msg.MessageID] = true
		}
		return out
	}
	check := func(msg agentcontext.ContextMessage) {
		if !msg.Visibility.IsProven() || strings.TrimSpace(msg.Content) == "" {
			return
		}
		if msg.Visibility == agentcontext.VisibilityPrivate {
			if msg.SenderID != in.ContextView.Current.SenderID {
				return
			}
		}
		out[msg.MessageID] = true
	}
	check(in.ContextView.Current)
	for _, msg := range in.ContextView.Recent {
		check(msg)
	}
	return out
}

// leadDetectionDispatchRequest 绑定 lead_detect 执行场景与 canonical session。
func leadDetectionDispatchRequest(in DetectionInput, system string, messages []llm.ChatMessage) llm.DispatchRequest {
	sessionRef, idempotencyKey := intentDispatchCallContext(in.ContextView, string(agentcontext.ExecutionScenarioLeadDetect), "detect")
	contextBlock := BuildIntentContextBlock(in.ContextView)
	if contextBlock != "" {
		system = strings.TrimSpace(system) + "\n" + contextBlock
	}
	return llm.DispatchRequest{
		Scenario:              llm.ScenarioLowCost,
		SystemPrompt:          system,
		Messages:              messages,
		MaxTokens:             4096,
		Temperature:           0.2,
		JSONMode:              true,
		ExecutionScenarioID:   string(agentcontext.ExecutionScenarioLeadDetect),
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idempotencyKey,
		StrictOutput:          true,
	}
}

// validateDetectionEvidenceIDs 拒绝模型幻觉出的未授权 MessageID。
func validateDetectionEvidenceIDs(ids []string, allowed map[string]struct{}) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if _, ok := allowed[id]; !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
