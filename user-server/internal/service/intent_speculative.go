package service

import (
	"context"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils"
)

// RecognizeSpeculative 投机识别 (并行化优化)
//
// 返回:
//   - 同步结果 *dto.RecognizeResult: 规则命中即返回, 未命中返回低置信度 placeholder
//   - 异步 channel: LLM 完成后通过 channel 投递最终结果 (缓冲 1)
//   - error: 严重错误 (ctx canceled, dispatcher nil 等)
//
// 调用方建议:
//  1. 立即用同步结果继续 Phase 1 (SOP/RAG/LLM 生成候选)
//  2. Phase 2 异步收割 channel: select + 10ms timeout
//  3. 如果 LLM 结果置信度更高,可选择性升级 (用于下一轮 cache)
func (s *IntentRecognizer) RecognizeSpeculative(
	ctx context.Context, sessionID, customerID, text string,
) (*dto.RecognizeResult, <-chan *dto.RecognizeResult, error) {
	if text == "" {
		empty := &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0, Method: "rule"}
		ch := make(chan *dto.RecognizeResult, 1)
		ch <- empty
		close(ch)
		return empty, ch, nil
	}

	if !loadIntentEnabled() {
		placeholder := &dto.RecognizeResult{
			IntentType:      IntentUnknown,
			IntentName:      "未知",
			Confidence:      0.3,
			ConfidenceLevel: "low",
			Sentiment:       "neutral",
			Method:          "disabled",
		}
		ch := make(chan *dto.RecognizeResult, 1)
		ch <- placeholder
		close(ch)
		return placeholder, ch, nil
	}

	if r := s.recognizeByRule(ctx, text); r != nil {
		s.saveRecord(ctx, sessionID, customerID, text, r, "", 0, 0)
		ch := make(chan *dto.RecognizeResult, 1)
		if s.dispatcher != nil {
			go s.runLLMAsync(sessionID, customerID, text, ch, true)
		} else {
			ch <- r
			close(ch)
		}
		return r, ch, nil
	}

	placeholder := &dto.RecognizeResult{
		IntentType:      IntentUnknown,
		IntentName:      "未知",
		Confidence:      0.3,
		ConfidenceLevel: "low",
		Sentiment:       "neutral",
		Method:          "rule_placeholder",
	}
	ch := make(chan *dto.RecognizeResult, 1)
	if s.dispatcher != nil {
		go s.runLLMAsync(sessionID, customerID, text, ch, false)
	} else {
		ch <- placeholder
		close(ch)
	}
	return placeholder, ch, nil
}

func (s *IntentRecognizer) runLLMAsync(
	sessionID, customerID, text string,
	ch chan<- *dto.RecognizeResult,
	ruleHit bool,
) {
	defer close(ch)
	bgCtx, cancel := context.WithTimeout(context.Background(), utils.LongTimeout)
	defer cancel()

	llmR, err := s.recognizeByLLM(bgCtx, text)
	if err != nil || llmR == nil {
		return
	}
	s.saveRecord(bgCtx, sessionID, customerID, text, llmR, llmR.LLMModel, llmR.CostTokens, llmR.LatencyMs)
	if customerID != "" {
		s.triggerSOPByIntent(bgCtx, customerID, sessionID, llmR.IntentType, llmR.Confidence)
	}
	if ruleHit {
		select {
		case ch <- llmR:
		default:
		}
	} else {
		ch <- llmR
	}
}
