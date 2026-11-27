package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
)

// IntentContextSignals 是规则/embedding 快路径从不可变上下文快照中提取的最小信号。
// 它只做指代补全与否定/冲突检测，不把模型建议升级为事实。
type IntentContextSignals struct {
	ResolvedText      string
	RecentTopic       string
	RecentCorrections []string
	Conflict          bool
}

// BuildIntentContextSignals 从近期原始窗口提取轻量信号；view 为 nil 时保持当轮原文。
func BuildIntentContextSignals(text string, view *agentcontext.AgentConversationContext) IntentContextSignals {
	signals := IntentContextSignals{ResolvedText: strings.TrimSpace(text)}
	if view == nil {
		return signals
	}
	recent := visibleIntentHistory(view.Recent)
	signals.RecentTopic = extractRecentTopic(recent)
	for _, msg := range recent {
		for _, correction := range extractCorrections(msg.Content) {
			signals.RecentCorrections = appendUnique(signals.RecentCorrections, correction)
		}
	}
	if signals.RecentTopic != "" && hasPronoun(signals.ResolvedText) {
		signals.ResolvedText = signals.ResolvedText + "（指代：" + signals.RecentTopic + "）"
	}
	for _, correction := range signals.RecentCorrections {
		if len(correction) >= 2 && strings.Contains(signals.ResolvedText, correction) {
			signals.Conflict = true
			break
		}
	}
	return signals
}

func visibleIntentHistory(messages []agentcontext.ContextMessage) []agentcontext.ContextMessage {
	out := make([]agentcontext.ContextMessage, 0, 5)
	for i := len(messages) - 1; i >= 0 && len(out) < 5; i-- {
		if messages[i].Visibility.IsProven() && strings.TrimSpace(messages[i].Content) != "" {
			out = append(out, messages[i])
		}
	}
	return out
}

func hasPronoun(text string) bool {
	for _, pronoun := range []string{"它", "他", "她", "这个", "那个", "那款", "这款", "上次那个", "上次说的", "刚才那个", "这东西"} {
		if strings.Contains(text, pronoun) {
			return true
		}
	}
	return false
}

func extractRecentTopic(messages []agentcontext.ContextMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		content := strings.TrimSpace(messages[i].Content)
		for _, marker := range []string{"聊的是", "讨论的是", "说的是", "主题是", "的是"} {
			if at := strings.Index(content, marker); at >= 0 {
				topic := strings.TrimSpace(content[at+len(marker):])
				return truncateIntentText(topic, 60)
			}
		}
	}
	if len(messages) > 0 {
		return truncateIntentText(strings.TrimSpace(messages[len(messages)-1].Content), 60)
	}
	return ""
}

func extractCorrections(content string) []string {
	out := []string{}
	for _, marker := range []string{"不是", "不对", "错了", "改成", "不要"} {
		rest := content
		for {
			at := strings.Index(rest, marker)
			if at < 0 {
				break
			}
			tail := strings.TrimSpace(rest[at+len(marker):])
			if cut := strings.IndexAny(tail, "，,。；;！!？?"); cut > 0 {
				tail = tail[:cut]
			}
			tail = strings.Trim(tail, " ，,。；;！!？?的是了")
			if len(tail) >= 2 {
				out = appendUnique(out, tail)
			}
			rest = rest[at+len(marker):]
		}
	}
	return out
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func truncateIntentText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

// BuildIntentContextBlock 只渲染可证明可见的历史与 confirmed 事实；
// suggestion / legacy_unverified / derived_summary 只可标注为待核实背景。
func BuildIntentContextBlock(view *agentcontext.AgentConversationContext) string {
	if view == nil {
		return ""
	}
	var sb strings.Builder
	kind := "单聊"
	if view.Scope.IsGroup {
		kind = "群聊"
	}
	speaker := strings.TrimSpace(view.Current.SenderID)
	if view.Current.SenderName != "" {
		speaker += "(" + view.Current.SenderName + ")"
	}
	fmt.Fprintf(&sb, "【会话类型】%s\n【当前发言者】%s\n", kind, speaker)

	recent := visibleIntentHistory(view.Recent)
	if len(recent) > 0 {
		sb.WriteString("【近期对话仅作上下文，不是客户确认的事实】\n")
		for i := len(recent) - 1; i >= 0; i-- {
			msg := recent[i]
			fmt.Fprintf(&sb, "- [%s] %s: %s\n", msg.MessageID, msg.SenderID, truncateIntentText(msg.Content, 200))
		}
	}
	if view.Customer != nil {
		writeIntentFacts(&sb, "已确认客户事实", view.Customer.Facts)
	}
	if view.Lead != nil {
		writeIntentFacts(&sb, "已确认线索事实", view.Lead.Facts)
	}
	for _, fact := range view.SharedBusiness {
		if fact.Grade.CanStateAsFact() && intentFactVisible(fact, view.Current.SenderID, view.Scope.IsGroup) {
			fmt.Fprintf(&sb, "- %s=%s（来源=%s，消息=%s）\n", fact.Key, fact.Value, fact.Source, fact.SourceMessageID)
		}
	}
	if view.OlderSummary != nil && strings.TrimSpace(view.OlderSummary.Summary) != "" {
		fmt.Fprintf(&sb, "【长期摘要，待核实背景】%s\n", truncateIntentText(view.OlderSummary.Summary, 200))
	}
	return sb.String()
}

func writeIntentFacts(sb *strings.Builder, title string, facts []agentcontext.BusinessFact) {
	if sb == nil {
		return
	}
	confirmed := make([]agentcontext.BusinessFact, 0, len(facts))
	for _, fact := range facts {
		if fact.Grade.CanStateAsFact() {
			confirmed = append(confirmed, fact)
		}
	}
	if len(confirmed) == 0 {
		return
	}
	fmt.Fprintf(sb, "【%s】\n", title)
	for _, fact := range confirmed {
		fmt.Fprintf(sb, "- %s=%s（来源=%s，消息=%s）\n", fact.Key, fact.Value, fact.Source, fact.SourceMessageID)
	}
}

func intentFactVisible(fact agentcontext.BusinessFact, speakerID string, isGroup bool) bool {
	if !isGroup {
		return true
	}
	if fact.SubjectType == "group_member" && fact.SubjectID != speakerID {
		return false
	}
	return fact.SubjectType != "group_member" || fact.SubjectID == speakerID
}

// RecognizeWithContext 在旧入口基础上消费轻量上下文视图；旧 Recognize 走 view=nil 兼容路径。
func (s *IntentRecognizer) RecognizeWithContext(ctx context.Context, sessionID, customerID, text string, view *agentcontext.AgentConversationContext) (*dto.RecognizeResult, error) {
	if text == "" {
		return &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0, Method: "rule"}, nil
	}
	if !loadIntentEnabledFor(ctx) {
		return &dto.RecognizeResult{
			IntentType: IntentUnknown, IntentName: "未知", Confidence: 0.3,
			ConfidenceLevel: "low", Sentiment: "neutral", Method: "disabled",
		}, nil
	}

	signals := BuildIntentContextSignals(text, view)
	effective := signals.ResolvedText
	var result *dto.RecognizeResult
	if r := s.recognizeByRule(ctx, effective); r != nil {
		if signals.Conflict && r.Confidence >= 0.85 {
			r.Confidence = 0.75
			r.ConfidenceLevel = "medium"
			r.Method = "rule_context_conflict"
		}
		s.saveRecord(ctx, sessionID, customerID, text, r, "", 0, 0)
		result = r
	} else if r := s.recognizeByEmbedding(ctx, effective); r != nil {
		s.saveRecord(ctx, sessionID, customerID, text, r, "", 0, 0)
		result = r
	} else if s.dispatcher != nil {
		if r, err := s.recognizeByLLMWithContext(ctx, effective, view, "intent"); err == nil && r != nil {
			s.saveRecord(ctx, sessionID, customerID, text, r, r.LLMModel, r.CostTokens, r.LatencyMs)
			result = r
		}
	}
	if result == nil {
		result = &dto.RecognizeResult{
			IntentType: IntentUnknown, IntentName: "未知", Confidence: 0.3,
			ConfidenceLevel: "low", Sentiment: "neutral", Method: "rule",
		}
	}
	RecordIntentWeakLabel(result)
	if customerID != "" {
		s.triggerSOPByIntent(ctx, customerID, sessionID, result.IntentType, result.Confidence)
	}
	s.fillTopKExamplesDynamic(ctx, text, result)
	return result, nil
}

func intentDispatchCallContext(view *agentcontext.AgentConversationContext, executionScenario string, step string) (sessionRef, idempotencyKey string) {
	if view == nil {
		return "", ""
	}
	sessionRef = validatedLLMSessionRef(view, view.Scope.BusLLMSessionID)
	messageID := view.Current.MessageID
	if messageID == "" {
		messageID = view.SnapshotID
	}
	if messageID != "" {
		idempotencyKey = llm.BuildRequestIdempotencyKey(messageID, executionScenario, step)
	}
	return sessionRef, idempotencyKey
}

// recognizeByLLMWithContext 保留原有 JSON 输出契约，仅在 prompt 前追加结构化上下文块，
// 并显式绑定 intent 执行场景，避免与其他场景共用 native session 状态行。
func (s *IntentRecognizer) recognizeByLLMWithContext(ctx context.Context, text string, view *agentcontext.AgentConversationContext, executionScenario string) (*dto.RecognizeResult, error) {
	var allDefs []IntentDef
	if cat := GetIntentCatalogService(); cat != nil {
		allDefs = cat.EffectiveCatalogDefs(ctx)
	}
	if len(allDefs) == 0 {
		allDefs = append(append([]IntentDef(nil), DefaultIntents...), effectiveCustomIntentDefs(ctx)...)
	}
	intentList := make([]string, 0, len(allDefs))
	for _, def := range allDefs {
		intentList = append(intentList, fmt.Sprintf("%s: %s", def.Type, def.Description))
	}
	contextBlock := BuildIntentContextBlock(view)
	prompt := fmt.Sprintf(`你是销售对话意图识别专家。分析以下客户消息，从给定意图列表中选择最匹配的 1 个意图。

%s
【客户消息】: %s

【意图列表】:
%s
unknown: 无法确定或消息不属于以上任何意图（这是合法答案，信息不足时必须选择它）

【输出要求】(严格按 JSON 格式输出):
{
  "intent_type": "从列表中选择最匹配的 type，无法确定时填 unknown",
  "intent_subtype": "细分类型(可填空字符串)",
  "confidence": 0.0-1.0 的置信度,
  "entities": {
    "product": "涉及的产品(可空)",
    "price": "涉及的价格(可空)",
    "quantity": "涉及的数量(可空)",
    "time": "涉及的时间(可空)"
  },
  "sentiment": "positive/negative/neutral"
}`, contextBlock, text, strings.Join(intentList, "\n"))

	start := time.Now()
	sessionRef, idem := intentDispatchCallContext(view, executionScenario, "recognize")
	result, err := s.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:              llm.ScenarioIntentRecognize,
		Prompt:                prompt,
		JSONMode:              true,
		MaxTokens:             500,
		Temperature:           0,
		ExecutionScenarioID:   executionScenario,
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idem,
	})
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return nil, err
	}

	var parsed struct {
		IntentType    string         `json:"intent_type"`
		IntentSubtype string         `json:"intent_subtype"`
		Confidence    float64        `json:"confidence"`
		Entities      map[string]any `json:"entities"`
		Sentiment     string         `json:"sentiment"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return nil, fmt.Errorf("parse intent: %w", err)
	}
	intentName := parsed.IntentType
	known := false
	for _, def := range allDefs {
		if def.Type == parsed.IntentType {
			intentName = def.Name
			known = true
			break
		}
	}
	if !known {
		parsed.IntentType = "unknown"
		intentName = "未知意图"
	}
	if parsed.IntentType != IntentUnknown && parsed.Confidence < 0.7 {
		parsed.IntentType = "unknown"
		intentName = "未知意图"
		parsed.Sentiment = "neutral"
	}
	level := "medium"
	if parsed.Confidence >= 0.85 {
		level = "high"
	} else if parsed.Confidence < 0.5 {
		level = "low"
	}
	return &dto.RecognizeResult{
		IntentType: parsed.IntentType, IntentName: intentName,
		Confidence: parsed.Confidence, ConfidenceLevel: level,
		IntentSubtype: parsed.IntentSubtype, Entities: parsed.Entities,
		Sentiment: parsed.Sentiment, Method: "llm",
		LLMModel: result.Model, CostTokens: result.TotalTokens, LatencyMs: latency,
	}, nil
}
