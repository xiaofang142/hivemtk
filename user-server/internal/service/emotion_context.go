package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

// ClassifyEmotionWithContext 在当轮关键词之外检查近期情绪轨迹；
// 当前消息明确撤回不满时以当前消息为准，避免把历史情绪强加给客户。
func ClassifyEmotionWithContext(content string, view *agentcontext.AgentConversationContext) EmotionType {
	current := ClassifyEmotion(content)
	if current != EmotionNeutral {
		return current
	}
	if view == nil || hasEmotionDeEscalation(content) {
		return EmotionNeutral
	}
	trajectory := EmotionNeutral
	for _, msg := range visibleIntentHistory(view.Recent) {
		emotion := ClassifyEmotion(msg.Content)
		if emotion == EmotionAnger {
			return EmotionAnger
		}
		if emotion == EmotionAnxiety && trajectory != EmotionAnger {
			trajectory = EmotionAnxiety
		}
		if emotion == EmotionSatisfied && trajectory == EmotionNeutral {
			trajectory = EmotionSatisfied
		}
	}
	return trajectory
}

func hasEmotionDeEscalation(content string) bool {
	for _, marker := range []string{"算了", "不用退款了", "不用赔了", "没事了", "不用了", "撤销投诉", "不投诉了"} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// MatchTransferKeywordsWithContext 保留近期明确的转人工请求；
// 当前消息撤销请求时立即失效。
func MatchTransferKeywordsWithContext(content string, view *agentcontext.AgentConversationContext) bool {
	if hasTransferCancellation(content) {
		return false
	}
	if MatchTransferKeywords(content) {
		return true
	}
	if view == nil {
		return false
	}
	for _, msg := range visibleIntentHistory(view.Recent) {
		if MatchTransferKeywords(msg.Content) {
			return true
		}
	}
	return false
}

func hasTransferCancellation(content string) bool {
	for _, marker := range []string{"不用转人工", "不需要人工", "取消转人工", "不转人工", "不用客服"} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// objectionContextDispatchRequest 构造带上下文块与独立执行场景的异议分类请求。
func objectionContextDispatchRequest(text string, view *agentcontext.AgentConversationContext) llm.DispatchRequest {
	catList := make([]string, 0, len(objectionRules))
	for _, r := range objectionRules {
		catList = append(catList, fmt.Sprintf("%s: %s", r.Category, r.Name))
	}
	contextBlock := BuildIntentContextBlock(view)
	prompt := fmt.Sprintf(`你是销售异议分类专家。判断客户消息是否为真实异议（而非拒绝/敷衍伪装），并归类。

%s
【客户消息】: %s

【异议类别】:
%s
other: 无法归类

【输出要求】(严格 JSON):
{"category": "类别之一", "confidence": 0.0-1.0, "is_genuine": true/false}
is_genuine=false 表示"太贵了买不起"实为委婉拒绝等伪装场景。`, contextBlock, text, strings.Join(catList, "\n"))
	sessionRef, idem := intentDispatchCallContext(view, "objection", "classify")
	return llm.DispatchRequest{
		Scenario:              llm.ScenarioObjection,
		Prompt:                prompt,
		JSONMode:              true,
		MaxTokens:             200,
		Temperature:           0,
		ExecutionScenarioID:   "objection",
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idem,
	}
}

// classifyByLLMWithContext 保留旧返回值契约，仅替换请求构造。
func (s *ObjectionHandlerService) classifyByLLMWithContext(ctx context.Context, text string, view *agentcontext.AgentConversationContext) (ObjectionCategory, float64, bool, bool) {
	result, err := s.dispatcher.Dispatch(ctx, objectionContextDispatchRequest(text, view))
	if err != nil {
		return "", 0, false, false
	}
	var parsed struct {
		Category   string  `json:"category"`
		Confidence float64 `json:"confidence"`
		IsGenuine  bool    `json:"is_genuine"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return "", 0, false, false
	}
	valid := false
	for _, r := range objectionRules {
		if ObjectionCategory(parsed.Category) == r.Category {
			valid = true
			break
		}
	}
	if parsed.Category == "other" {
		valid = true
	}
	if !valid || parsed.Confidence <= 0 || parsed.Confidence > 1 {
		return "", 0, false, false
	}
	return ObjectionCategory(parsed.Category), parsed.Confidence, parsed.IsGenuine, true
}
