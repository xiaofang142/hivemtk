package service

import (
	"context"
	"fmt"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
)

// filterLeadJudgeMessages 按 ContextView 的 audience/visibility 过滤判定消息。
// 无 view 时保持存量行为（调用方已做 scope 校验），有 view 时一律 fail-closed。
func filterLeadJudgeMessages(msgs []DetectionMessage, view *agentcontext.AgentConversationContext) []DetectionMessage {
	if view == nil {
		out := make([]DetectionMessage, 0, len(msgs))
		return append(out, msgs...)
	}
	allowed := leadDetectionAllowedMessages(DetectionInput{ContextView: view})
	out := make([]DetectionMessage, 0, len(msgs))
	for _, msg := range msgs {
		if !allowed[msg.MessageID] {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// messageHubsToDetectionMessages 把真实 MessageHub 行转成带真实证据 ID 的判定消息。
func messageHubsToDetectionMessages(rows []model.MessageHub, isGroup bool) []DetectionMessage {
	out := make([]DetectionMessage, 0, len(rows))
	for _, h := range rows {
		content := h.Content
		if content == "" {
			continue
		}
		role := "user"
		if h.Direction == "outbound" {
			role = "assistant"
		}
		if isGroup {
			speaker := h.SenderName
			if speaker == "" {
				speaker = h.SenderID
			}
			if speaker != "" {
				content = fmt.Sprintf("[%s] %s", speaker, content)
			}
			role = "user"
		}
		visibility := agentcontext.Visibility(h.Visibility)
		if !visibility.IsProven() {
			visibility = agentcontext.VisibilityUnknown
		}
		out = append(out, DetectionMessage{
			MessageID:  fmt.Sprintf("hub:%d", h.ID),
			Content:    content,
			Role:       role,
			SenderID:   h.SenderID,
			SenderName: h.SenderName,
			Visibility: visibility,
		})
	}
	return out
}

// fetchHistoryDetectionMessages 从授权范围内的 MessageHub 行生成真实证据锚点。
func (s *Service) fetchHistoryDetectionMessages(ctx context.Context, hub *model.MessageHub) []DetectionMessage {
	return messageHubsToDetectionMessages(s.loadHistoryHubs(ctx, hub), hub.IsGroup)
}

// leadJudgeMessagesWithEvidence 为判定请求保留真实 MessageID/发送者。
func leadJudgeMessagesWithEvidence(jctx *LeadJudgeContext) []llm.ChatMessage {
	msgs := filterLeadJudgeMessages(jctx.Messages, jctx.ContextView)
	out := make([]llm.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" || m.Role == "self" {
			role = "assistant"
		}
		content := fmt.Sprintf("[msg id=%s|sender=%s] %s", m.MessageID, m.SenderID, m.Content)
		out = append(out, llm.ChatMessage{Role: role, Content: content})
	}
	return out
}

// leadJudgeDispatchRequest 绑定 lead_judge 执行场景与 canonical session。
func leadJudgeDispatchRequest(jctx *LeadJudgeContext, messages []llm.ChatMessage) llm.DispatchRequest {
	sessionRef, idempotencyKey := intentDispatchCallContext(jctx.ContextView, string(agentcontext.ExecutionScenarioLeadJudge), "judge")
	return llm.DispatchRequest{
		Scenario:              llm.ScenarioLowCost,
		SystemPrompt:          buildLeadJudgeSystemPrompt(jctx),
		Messages:              messages,
		MaxTokens:             1024,
		Temperature:           0.2,
		JSONMode:              true,
		ExecutionScenarioID:   string(agentcontext.ExecutionScenarioLeadJudge),
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idempotencyKey,
		StrictOutput:          true,
	}
}
