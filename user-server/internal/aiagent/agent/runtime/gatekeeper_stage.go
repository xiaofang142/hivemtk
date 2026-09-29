package agent_runtime

import (
	"context"
	"strings"
	"sync"
	"time"
)

// HandoffCustomerNotice 转人工时**发给客户看**的那一句，全仓唯一一份。
//
// 为什么要有常量、而不是各处各写一句：转人工有两个产出方（本文件的危机门禁、
// service 编排器的置信度/关键词/上限/引擎缺位四个出口），文案分叉后客户会在不同渠道
// 看到不同说法，而排查"这句是谁发的"也无从下手。
// 措辞约束（改字前先看这三条，都有活的消费方）：
//   - 不许命中 scripts/simulate/ai_quality.py 的 DEGRADED_MARKERS 任一锚串
//     （如「稍后再试」「服务繁忙」）——那套锚是压测用来识别降级回复的，命中一次
//     就把一条正常的转接公告统计成 AI 降级；
//   - 不许写具体坐席名或"已接通"：转人工只保证会话进了人工队列与待办池，
//     无在线客服时没人立刻应答，所以这句只承诺"转接 + 稍等"；
//   - 不许带内部原因（「AI 置信度不足 0.42」「客户显式要求转人工」这类），
//     那些是 handoff_decisions / human_tasks 的字段，给运营看不给客户看。
const HandoffCustomerNotice = "已为您转接人工客服，请稍等～"

// HandoffQueueNotice 转人工**但当下没有可接的坐席**时发给客户的那一句。
//
// 与 HandoffCustomerNotice 分开是有原因的：分配失败时会话只是进了人工队列与待办池，
// 并没有人被叫上线，而编排器随后仍会让 AI 继续应答（客户不该因为没人接而变成没人理）。
// 这时说「已为您转接人工客服」就是把一句没发生过的承诺发给客户 —— 与本轮要消灭的
// 那句「回复「人工」」死路话术同一类。措辞约束与 HandoffCustomerNotice 一致。
const HandoffQueueNotice = "当前没有在线客服，您的诉求已记录，客服上线后会第一时间回复您；这段时间我先继续为您解答。"

// DefaultCrisisDetector 默认危机感检测器
type DefaultCrisisDetector struct {
	HighRiskKeywords   []string
	MediumRiskKeywords []string
	LowRiskKeywords    []string
	SlidingWindow      *SentimentSlidingWindow
}

// NewDefaultCrisisDetector 构造默认危机检测器
func NewDefaultCrisisDetector() *DefaultCrisisDetector {
	return &DefaultCrisisDetector{
		HighRiskKeywords: []string{
			"退款", "退货", "骗子", "骗人", "欺诈", "起诉", "曝光", "315",
			"工商", "消协", "黑猫", "12315", "律师", "报警",
			"refund", "scam", "fraud", "lawsuit", "sue", "lawyer", "police",
		},
		MediumRiskKeywords: []string{
			"投诉", "差评", "气死", "生气", "愤怒", "无语", "失望",
			"complain", "angry", "furious", "disappointed",
		},
		LowRiskKeywords: []string{
			"急", "着急", "焦虑", "担心", "麻烦",
			"urgent", "worried", "anxious",
		},
		SlidingWindow: NewSentimentSlidingWindow(20),
	}
}

// Name 阶段名
func (d *DefaultCrisisDetector) Name() string {
	return "gatekeeper"
}

// Execute 执行门禁
func (d *DefaultCrisisDetector) Execute(ctx context.Context, ic *InferenceContext) StageResult {
	start := time.Now()
	ic.Crisis = d.Detect(ctx, ic)

	action := "pass"
	if ic.Crisis.NeedsEscalation() {
		action = "escalate"
	} else if ic.Crisis.Level >= CrisisMedium {
		action = "flag"
	}

	ic.Stages = append(ic.Stages, StageDecision{
		Stage:    d.Name(),
		Action:   action,
		Reason:   "level=" + itoa(int(ic.Crisis.Level)) + " triggers=" + strings.Join(ic.Crisis.Triggers, ","),
		Duration: time.Since(start),
		Success:  true,
	})

	if ic.Crisis.NeedsEscalation() {
		ic.Decision.HandoffToHuman = true
		ic.Decision.HandoffReason = ic.Crisis.Reason
		ic.Decision.StopReason = "crisis_gate_triggered"
		ic.Decision.Reply = HandoffCustomerNotice
		ic.Decision.ReplyType = "handoff"
		return StopResult(&ic.Decision)
	}

	return ContinueResult()
}

// Detect 检测危机等级
func (d *DefaultCrisisDetector) Detect(ctx context.Context, ic *InferenceContext) CrisisSignal {
	text := strings.ToLower(ic.Payload.Content)
	triggers := []string{}
	level := CrisisNone
	reason := "no_crisis"

	if ic.Intent.Primary == IntentHandoffToHuman {
		level = CrisisHigh
		reason = "handoff_human_intent"
		triggers = append(triggers, "intent:handoff_human")
	}

	for _, kw := range d.HighRiskKeywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			triggers = append(triggers, kw)
			level = CrisisHigh
			reason = "high_risk_keyword:" + kw
		}
	}

	if level < CrisisMedium {
		for _, kw := range d.MediumRiskKeywords {
			if strings.Contains(text, strings.ToLower(kw)) {
				triggers = append(triggers, kw)
				if level < CrisisMedium {
					level = CrisisMedium
					reason = "medium_risk_keyword:" + kw
				}
			}
		}
	}

	if level == CrisisNone {
		for _, kw := range d.LowRiskKeywords {
			if strings.Contains(text, strings.ToLower(kw)) {
				triggers = append(triggers, kw)
				level = CrisisLow
				reason = "low_risk_keyword:" + kw
			}
		}
	}

	if level < CrisisMedium && ic.Sentiment.Label == SentimentAngry && ic.Sentiment.Score >= 0.7 {
		level = CrisisMedium
		reason = "angry_sentiment_high"
	}

	if d.SlidingWindow != nil {
		d.SlidingWindow.Record(ic.Sentiment.Score)
		if level < CrisisMedium && d.SlidingWindow.IsDeclining(3) {
			level = CrisisMedium
			reason = "sentiment_sliding_decline"
			triggers = append(triggers, "sliding_decline")
		}
	}

	if level < CrisisMedium && ic.Alignment.Empathy <= 2 {
		level = CrisisMedium
		reason = "low_empathy_alignment"
	}

	if level < CrisisHigh && ic.Intent.Primary == IntentHandoffToHuman {
		level = CrisisHigh
		reason = "handoff_human_intent_promoted"
	}

	return CrisisSignal{
		Level:      level,
		Triggers:   triggers,
		Reason:     reason,
		DetectedAt: time.Now(),
	}
}

// SentimentSlidingWindow 情绪滑动窗口（ring buffer 实现）
// 检测连续下降趋势：近 N 轮情绪分持续降低
type SentimentSlidingWindow struct {
	mu     sync.Mutex
	scores []float64
	idx    int
	size   int
}

// NewSentimentSlidingWindow 构造情绪滑动窗口
func NewSentimentSlidingWindow(size int) *SentimentSlidingWindow {
	if size <= 0 {
		size = 20
	}
	return &SentimentSlidingWindow{
		scores: make([]float64, size),
		size:   size,
	}
}

// Record 记录一轮情绪分
func (w *SentimentSlidingWindow) Record(score float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scores[w.idx] = score
	w.idx = (w.idx + 1) % w.size
}

// IsDeclining 检测最近连续 N 轮是否单调下降
func (w *SentimentSlidingWindow) IsDeclining(continuous int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if continuous < 2 {
		return false
	}
	for i := 0; i < continuous-1; i++ {
		curIdx := (w.idx - 1 - i + w.size) % w.size
		prevIdx := (w.idx - 2 - i + w.size) % w.size
		if w.scores[curIdx] >= w.scores[prevIdx] {
			return false
		}
	}
	return true
}
