package agent_runtime

import (
	"context"
	"strings"
	"time"
)

// NewDefaultReviewer 构造默认 Reviewer
func NewDefaultReviewer() *DefaultReviewer {
	return &DefaultReviewer{}
}

type DefaultReviewer struct {
	MinReplyLength  int
	MinConfidence   float64
	MinAlignmentAvg float64
	SensitiveWords  []string
}

// NewDefaultReviewerWithConfig 自定义阈值
func NewDefaultReviewerWithConfig(minLen int, minConf, minAlign float64, words []string) *DefaultReviewer {
	return &DefaultReviewer{
		MinReplyLength:  minLen,
		MinConfidence:   minConf,
		MinAlignmentAvg: minAlign,
		SensitiveWords:  words,
	}
}

// Name 阶段名
func (r *DefaultReviewer) Name() string {
	return "reviewer"
}

// Execute 执行复审（InferenceStage 兼容）
func (r *DefaultReviewer) Execute(ctx context.Context, ic *InferenceContext) StageResult {
	start := time.Now()
	result, err := r.Review(ctx, ic)
	if err != nil {
		ic.Stages = append(ic.Stages, StageDecision{
			Stage:    r.Name(),
			Action:   "review_failed",
			Reason:   err.Error(),
			Duration: time.Since(start),
			Success:  false,
			Error:    err.Error(),
		})
		return FailResult(err)
	}

	if result != nil {
		ic.Decision.Review = result
		if result.AdjustedReply != "" && result.AdjustReason != "" {
			ic.Decision.Reply = result.AdjustedReply
		}
	}

	ic.Stages = append(ic.Stages, StageDecision{
		Stage:    r.Name(),
		Action:   "review",
		Reason:   "score=" + floatStr64(result.OverallScore) + " passed=" + boolStr(result.Passed),
		Duration: time.Since(start),
		Success:  true,
	})
	return ContinueResult()
}

// Review 执行复审，返回 ReviewResult
func (r *DefaultReviewer) Review(_ context.Context, ic *InferenceContext) (*ReviewResult, error) {
	result := &ReviewResult{
		Passed:     true,
		ReviewedAt: time.Now(),
	}

	reply := ic.Decision.Reply
	if reply == "" && ic.Plan != nil {
		reply = ic.Plan.ReplyHint
	}

	minLen := r.MinReplyLength
	if minLen == 0 {
		minLen = 4
	}
	r.checkLength(reply, minLen, result)

	minConf := r.MinConfidence
	if minConf == 0 {
		minConf = 0.5
	}
	confScore := ic.Decision.Confidence
	if ic.Plan != nil && ic.Plan.Confidence > confScore {
		confScore = ic.Plan.Confidence
	}
	r.checkConfidence(confScore, minConf, result)

	minAlign := r.MinAlignmentAvg
	if minAlign == 0 {
		minAlign = 2.5
	}
	r.checkAlignment(ic.Alignment.Total(), minAlign, result)

	r.checkSensitive(reply, result)

	if len(result.Checks) > 0 {
		total := 0.0
		passed := 0
		for _, c := range result.Checks {
			total += c.Score
			if c.Passed {
				passed++
			}
		}
		result.OverallScore = total / float64(len(result.Checks))
		if passed < len(result.Checks) {
			result.Passed = false
		}
	} else {
		result.OverallScore = 1.0
	}

	if !result.Passed && reply != "" {
		adjusted := r.adjustReply(reply, result)
		if adjusted != reply {
			result.AdjustedReply = adjusted
			result.AdjustReason = "review_adjusted"
		}
	}

	return result, nil
}

func (r *DefaultReviewer) checkLength(reply string, min int, result *ReviewResult) {
	passed := len([]rune(reply)) >= min
	score := 1.0
	if !passed {
		score = 0.2
	}
	result.Checks = append(result.Checks, ReviewCheck{
		Name:   "length",
		Passed: passed,
		Score:  score,
		Reason: "长度=" + itoa(len([]rune(reply))) + " 下限=" + itoa(min),
	})
	if !passed {
		result.Passed = false
	}
}

func (r *DefaultReviewer) checkConfidence(conf, min float64, result *ReviewResult) {
	passed := conf >= min
	score := conf
	if score > 1 {
		score = 1
	}
	if !passed {
		score = conf / min
		if score > 1 {
			score = 1
		}
	}
	result.Checks = append(result.Checks, ReviewCheck{
		Name:   "confidence",
		Passed: passed,
		Score:  score,
		Reason: "置信度=" + floatStr64(conf) + " 下限=" + floatStr64(min),
	})
	if !passed {
		result.Passed = false
	}
}

func (r *DefaultReviewer) checkAlignment(alignTotal, min float64, result *ReviewResult) {
	passed := alignTotal >= min
	score := alignTotal / 5.0
	if score > 1 {
		score = 1
	}
	if !passed {
		score = alignTotal / min
		if score > 1 {
			score = 1
		}
	}
	result.Checks = append(result.Checks, ReviewCheck{
		Name:   "alignment",
		Passed: passed,
		Score:  score,
		Reason: "6维总分=" + floatStr64(alignTotal) + " 下限=" + floatStr64(min),
	})
	if !passed {
		result.Passed = false
	}
}

func (r *DefaultReviewer) checkSensitive(reply string, result *ReviewResult) {
	words := r.SensitiveWords
	if len(words) == 0 {
		words = defaultSensitiveWords
	}
	hit := []string{}
	lower := strings.ToLower(reply)
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(w)) {
			hit = append(hit, w)
		}
	}
	passed := len(hit) == 0
	score := 1.0
	if !passed {
		score = 0.0
		result.Passed = false
	}
	reason := "ok"
	if !passed {
		reason = "命中敏感词=" + strings.Join(hit, ",")
	}
	result.Checks = append(result.Checks, ReviewCheck{
		Name:   "sensitive",
		Passed: passed,
		Score:  score,
		Reason: reason,
	})
}

func (r *DefaultReviewer) adjustReply(reply string, result *ReviewResult) string {
	prefix := ""
	suffix := ""
	for _, c := range result.Checks {
		switch c.Name {
		case "length":
			if !c.Passed {
				prefix = "您好，关于您的问题，我了解到："
			}
		case "sensitive":
			if !c.Passed {
				return "抱歉，这部分内容我暂时无法回复，请稍后联系人工客服。"
			}
		}
	}
	_ = suffix
	return prefix + reply
}

func floatStr64(f float64) string {
	return itoa(int(f * 100))
}

var defaultSensitiveWords = []string{
	"政治敏感",
	"违法犯罪",
	"个人信息泄露",
}
