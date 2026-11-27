package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

// RecognizeIntentWithContext 在精细意图入口消费轻量上下文视图；
// 旧 RecognizeIntent 走 view=nil 的兼容路径。
func (s *IntentRecognizer) RecognizeIntentWithContext(ctx context.Context, message, customerID, sessionID string, view *agentcontext.AgentConversationContext) (*IntentResult, error) {
	start := time.Now()
	if message == "" {
		return &IntentResult{Major: IntentMajorConsult, Minor: IntentMinorConsultGeneral, Confidence: 0.3, Method: "rule"}, nil
	}
	if !loadIntentEnabledFor(ctx) {
		return &IntentResult{Major: IntentMajorConsult, Minor: IntentMinorConsultGeneral, Confidence: 0.3, Method: "disabled"}, nil
	}

	signals := BuildIntentContextSignals(message, view)
	effective := signals.ResolvedText
	ruleResult := s.recognizeFineByRule(ctx, effective)
	var final *IntentResult
	if ruleResult != nil {
		final = ruleResult
		if signals.Conflict && ruleResult.Confidence >= 0.85 {
			ruleResult.Confidence = 0.75
		}
		if ruleResult.Confidence < minorLLMThreshold() && s.dispatcher != nil {
			if llmResult, err := s.recognizeFineByLLMWithContext(ctx, effective, view); err == nil && llmResult != nil {
				llmResult.Method = "hybrid"
				final = llmResult
			}
		}
	} else if s.dispatcher != nil {
		if llmResult, err := s.recognizeFineByLLMWithContext(ctx, effective, view); err == nil && llmResult != nil {
			final = llmResult
		}
	}
	if final == nil {
		final = &IntentResult{Major: IntentMajorConsult, Minor: IntentMinorConsultGeneral, Confidence: 0.3, Method: "rule"}
	}
	final.LatencyMs = int(time.Since(start).Milliseconds())
	s.saveIntentLog(ctx, customerID, sessionID, message, final)
	if customerID != "" {
		s.triggerSOPByIntent(ctx, customerID, sessionID, final.Major, final.Confidence)
	}
	return final, nil
}

func (s *IntentRecognizer) recognizeFineByLLMWithContext(ctx context.Context, text string, view *agentcontext.AgentConversationContext) (*IntentResult, error) {
	if s.dispatcher == nil {
		return nil, fmt.Errorf("dispatcher is nil")
	}
	var sb strings.Builder
	for _, mr := range fineIntentRules {
		sb.WriteString(fmt.Sprintf("- %s\n", mr.major))
	}
	contextBlock := BuildIntentContextBlock(view)
	prompt := fmt.Sprintf(`你是销售对话意图识别专家。分析以下客户消息，从给定意图列表中选择最匹配的 1 个大类。

%s
【客户消息】: %s

【意图列表】:
%s
unknown: 无法确定或消息不属于以上任何意图（这是合法答案，信息不足时必须选择它）

【输出要求】(严格按 JSON 格式输出，不要添加任何其他内容):
{
  "major": "8 大类之一或 unknown",
  "confidence": 0.0-1.0 的置信度,
  "reasoning": "简短解释为何选择此意图（不超过 50 字）"
}`, contextBlock, text, sb.String())

	sessionRef, idem := intentDispatchCallContext(view, "fine_intent", "recognize")
	result, err := s.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:              llm.ScenarioIntentRecognize,
		Prompt:                prompt,
		JSONMode:              true,
		MaxTokens:             300,
		Temperature:           0,
		ExecutionScenarioID:   "fine_intent",
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idem,
	})
	if err != nil {
		return nil, fmt.Errorf("llm dispatch: %w", err)
	}
	var parsed struct {
		Major      string  `json:"major"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm intent: %w", err)
	}
	if !isValidMajor(parsed.Major) || parsed.Confidence < 0.7 {
		parsed.Major = "unknown"
	}
	if parsed.Confidence <= 0 {
		parsed.Confidence = 0.5
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	return &IntentResult{
		Major: parsed.Major, Minor: refineMinorByKeywords(parsed.Major, text),
		Confidence: parsed.Confidence, Method: "llm", Reasoning: parsed.Reasoning,
		LLMModel: result.Model,
	}, nil
}
