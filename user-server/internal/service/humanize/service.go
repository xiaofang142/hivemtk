package humanize

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// DefaultThreshold 默认达标阈值（PRD §5.2 G6：≥ 0.85）
const DefaultThreshold = 0.85

// thresholdProvider 由装配层（internal/app/confidence_params_wiring.go）注入，
// 数据源是 config_params 的 confidence.humanize_default_threshold。
// 单独一个 provider 而不是直接读库：humanize 是下层包，不能反向依赖 service
// 读参数中心（那会形成 service/humanize → service 的循环）。
var thresholdProvider = func() float64 { return DefaultThreshold }

// SetDefaultThresholdProvider 注入达标阈值读取口。
// 传 nil 视为不注入：装配顺序错时宁可用编译期默认值，也不要拿到一个空函数。
func SetDefaultThresholdProvider(fn func() float64) {
	if fn != nil {
		thresholdProvider = fn
	}
}

// EffectiveThreshold 当前生效的达标阈值。
// 非 (0,1] 的值一律回落编译期默认值——阈值取到 0 会让所有文本都判达标，
// 取到 1 以上则永远不达标，两种都是静默的行为错误。
func EffectiveThreshold() float64 {
	if t := thresholdProvider(); t > 0 && t <= 1 {
		return t
	}
	return DefaultThreshold
}

// DefaultBoundaryLow 边界样本下界（含）
const DefaultBoundaryLow = 0.70

// DefaultBoundaryHigh 边界样本上界（不含）
const DefaultBoundaryHigh = 0.85

// DefaultSampleRate 不达标样本 LLM 采样率
const DefaultSampleRate = 0.10

// DefaultMaxRetry 最大重生成次数
const DefaultMaxRetry = 3

// ---- 以下四项是阶段三 3.1 的接线：与 DefaultThreshold 同属「拟人度评估」一组旋钮 ----
//
// DefaultThreshold 已在阶段 1.2 接为 confidence.humanize_default_threshold，
// 剩下 boundary/sample_rate/max_retry 三项散在这里没人读参数中心。
// 合起来才是完整的一组：阈值决定「谁算不达标」，边界决定「哪些算边缘样本值得送 LLM」，
// 采样率决定「边缘样本里送多少」，重试次数决定「不达标时重写几轮」。
// 四项各占一个键而不是合成一张 JSON：运维在参数中心页面上是逐项调、逐项看审计的。

// boundaryLowProvider 数据源是 config_params 的 confidence.humanize_boundary_low。
var boundaryLowProvider = func() float64 { return DefaultBoundaryLow }

// SetBoundaryLowProvider 注入边界样本下界读取口。传 nil 视为不注入。
func SetBoundaryLowProvider(fn func() float64) {
	if fn != nil {
		boundaryLowProvider = fn
	}
}

// EffectiveBoundaryLow 当前生效的边界样本下界（含）。
// 非 [0,1) 的值回落编译期默认值：负下界会让所有文本都算边缘样本（全部送 LLM，
// 费用与延迟同时失控），≥1 则一个都没有（LLM 复核形同关闭）。
func EffectiveBoundaryLow() float64 {
	if v := boundaryLowProvider(); v >= 0 && v < 1 {
		return v
	}
	return DefaultBoundaryLow
}

// boundaryHighProvider 数据源是 config_params 的 confidence.humanize_boundary_high。
var boundaryHighProvider = func() float64 { return DefaultBoundaryHigh }

// SetBoundaryHighProvider 注入边界样本上界读取口。传 nil 视为不注入。
func SetBoundaryHighProvider(fn func() float64) {
	if fn != nil {
		boundaryHighProvider = fn
	}
}

// EffectiveBoundaryHigh 当前生效的边界样本上界（不含）。
// 非 (0,1] 的值回落编译期默认值：0 同样让「边缘样本」这个概念消失。
func EffectiveBoundaryHigh() float64 {
	if v := boundaryHighProvider(); v > 0 && v <= 1 {
		return v
	}
	return DefaultBoundaryHigh
}

// sampleRateProvider 数据源是 config_params 的 confidence.humanize_sample_rate。
var sampleRateProvider = func() float64 { return DefaultSampleRate }

// SetSampleRateProvider 注入 LLM 采样率读取口。传 nil 视为不注入。
func SetSampleRateProvider(fn func() float64) {
	if fn != nil {
		sampleRateProvider = fn
	}
}

// EffectiveSampleRate 当前生效的采样率，落在 [0,1]。
// 0 是合法值（语义是「一个都不送 LLM」，纯规则打分），所以下界含 0；
// 越界与负值回落默认值——负采样率在下面的按比例挑选里会算出负数个数。
func EffectiveSampleRate() float64 {
	if v := sampleRateProvider(); v >= 0 && v <= 1 {
		return v
	}
	return DefaultSampleRate
}

// maxRetryProvider 数据源是 config_params 的 confidence.humanize_max_retry。
var maxRetryProvider = func() int { return DefaultMaxRetry }

// SetMaxRetryProvider 注入最大重生成次数读取口。传 nil 视为不注入。
func SetMaxRetryProvider(fn func() int) {
	if fn != nil {
		maxRetryProvider = fn
	}
}

// EffectiveMaxRetry 当前生效的最大重生成次数。
// 非正数回落编译期默认值：0 会让「不达标就重写」这条路径一步都不走，
// 但调度侧是按次数预算 LLM 调用的，静默变 0 会让预算模型与真实行为脱节。
func EffectiveMaxRetry() int {
	if v := maxRetryProvider(); v > 0 {
		return v
	}
	return DefaultMaxRetry
}

// HumanizeEvalService 主编排服务
type HumanizeEvalService struct {
	ruleScorer      HumanizeEvaluator
	llmScorer       HumanizeEvaluator
	baselineRepo    ChampionBaselineRepository
	scoreRepo       HumanizeScoreRepository
	sampleCollector LowQualitySampleCollector
	threshold       float64
	sampleRate      float64
	boundaryLow     float64
	boundaryHigh    float64
	maxRetry        int
	regenerateFn    HumanizeRegenerateFn
	rng             *rand.Rand
}

// NewHumanizeEvalService 构造
func NewHumanizeEvalService(
	rule, llm HumanizeEvaluator,
	baselineRepo ChampionBaselineRepository,
	scoreRepo HumanizeScoreRepository,
	sampleCollector LowQualitySampleCollector,
) *HumanizeEvalService {
	return &HumanizeEvalService{
		ruleScorer:      rule,
		llmScorer:       llm,
		baselineRepo:    baselineRepo,
		scoreRepo:       scoreRepo,
		sampleCollector: sampleCollector,
		threshold:       EffectiveThreshold(),
		sampleRate:      EffectiveSampleRate(),
		boundaryLow:     EffectiveBoundaryLow(),
		boundaryHigh:    EffectiveBoundaryHigh(),
		maxRetry:        EffectiveMaxRetry(),
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WithThreshold 设置达标阈值
func (s *HumanizeEvalService) WithThreshold(ctx context.Context, t float64) *HumanizeEvalService {
	if t > 0 && t <= 1 {
		s.threshold = t
	}
	return s
}

// WithSampleRate 设置 LLM 采样率
func (s *HumanizeEvalService) WithSampleRate(ctx context.Context, r float64) *HumanizeEvalService {
	if r >= 0 && r <= 1 {
		s.sampleRate = r
	}
	return s
}

// Threshold 返回当前生效的达标阈值（可被 WithThreshold 覆盖，区别于编译期 DefaultThreshold）
//
// 供外部装配层（humanize_init.go）在构造重生成 prompt 时使用真实阈值，
// 也用于落库时写入 humanize_scores.threshold（必须等于实际判定阈值，否则数据失真）。
func (s *HumanizeEvalService) Threshold() float64 {
	return s.threshold
}

// WithBoundary 设置边界区间
func (s *HumanizeEvalService) WithBoundary(ctx context.Context, low, high float64) *HumanizeEvalService {
	if low >= 0 && high > low && high <= 1 {
		s.boundaryLow = low
		s.boundaryHigh = high
	}
	return s
}

// WithMaxRetry 设置最大重试次数
func (s *HumanizeEvalService) WithMaxRetry(ctx context.Context, n int) *HumanizeEvalService {
	if n > 0 {
		s.maxRetry = n
	}
	return s
}

// WithRegenerateFn 设置重生成回调
func (s *HumanizeEvalService) WithRegenerateFn(ctx context.Context, fn HumanizeRegenerateFn) *HumanizeEvalService {
	s.regenerateFn = fn
	return s
}

// Evaluate 主入口
//
// 被外部 SalesEngine 第 7.5 步调用
func (s *HumanizeEvalService) Evaluate(ctx context.Context, input *dto.HumanizeEvalInput) (*dto.HumanizeEvalResult, error) {
	if input == nil || input.AIReply == "" {
		return nil, fmt.Errorf("%w: input or ai_reply empty", ErrInvalidInput)
	}
	if s.ruleScorer == nil {
		return nil, ErrAggregatorNotInitialized
	}

	ruleResult, err := s.ruleScorer.Evaluate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("rule scorer: %w", err)
	}
	ruleResult.SampleStrategy = "full"

	decision := decideLLMSample(
		ruleResult.TotalScore, s.threshold,
		s.boundaryLow, s.boundaryHigh, s.sampleRate, s.rng,
	)

	if decision.NeedLLM && s.llmScorer != nil {
		if s.baselineRepo != nil && input.Persona != "" {
			baseline, berr := s.baselineRepo.FindByPersonaIndustryIntent(ctx, input.Persona, input.Industry, input.Intent)
			if berr == nil && baseline != nil {
				if ls, ok := s.llmScorer.(*LLMScorerImpl); ok {
					ls.WithBaseline(baseline)
				}
			}
		}
		llmResult, lerr := s.llmScorer.Evaluate(ctx, input)
		if lerr == nil && llmResult != nil {
			llmResult.SampleStrategy = decision.Strategy
			llmResult.EvaluatorType = "hybrid"
			s.persist(ctx, llmResult)
			if llmResult.TotalScore < s.threshold {
				return s.evaluateWithRetry(ctx, input, llmResult)
			}
			llmResult.Passed = true
			llmResult.FinalReply = input.AIReply
			return llmResult, nil
		}
	}

	s.persist(ctx, ruleResult)
	if ruleResult.TotalScore < s.threshold {
		return s.evaluateWithRetry(ctx, input, ruleResult)
	}
	ruleResult.Passed = true
	ruleResult.FinalReply = input.AIReply
	return ruleResult, nil
}

func (s *HumanizeEvalService) evaluateWithRetry(ctx context.Context, input *dto.HumanizeEvalInput, lastResult *dto.HumanizeEvalResult) (*dto.HumanizeEvalResult, error) {
	result := lastResult
	allReplies := []string{input.AIReply}
	for attempt := 1; attempt <= s.maxRetry; attempt++ {
		result.AttemptCount = attempt
		if result.TotalScore >= s.threshold {
			result.Passed = true
			result.FinalReply = input.AIReply
			result.AllReplies = allReplies
			s.persist(ctx, result)
			return result, nil
		}
		if s.regenerateFn == nil || attempt == s.maxRetry {
			break
		}
		newReply, err := s.regenerateFn(ctx, input, result)
		if err != nil || newReply == "" {
			break
		}
		input.AIReply = newReply
		allReplies = append(allReplies, newReply)
		newResult, err := s.ruleScorer.Evaluate(ctx, input)
		if err != nil {
			break
		}
		result = newResult
		result.SampleStrategy = "full"
	}
	result.Passed = false
	result.FinalReply = input.AIReply
	result.AllReplies = allReplies
	result.AttemptCount = s.maxRetry
	if s.sampleCollector != nil {
		sampleType := s.decideLowQualitySampleType(ctx, result)
		sample := s.buildLowQualitySample(ctx, input, result, sampleType)
		if err := s.sampleCollector.Collect(ctx, sample); err != nil {
			logger.Errorf("[HumanizeEval] collect low quality sample: %v", err)
		}
	}
	s.persist(ctx, result)
	return result, nil
}

func (s *HumanizeEvalService) decideLowQualitySampleType(ctx context.Context, result *dto.HumanizeEvalResult) string {
	if result == nil || len(result.Scores) == 0 {
		return "retry_exhausted"
	}
	minScore := 1.0
	minDim := ""
	for _, sc := range result.Scores {
		if sc.Score < minScore {
			minScore = sc.Score
			minDim = string(sc.Dimension)
		}
	}
	switch minDim {
	case "naturalness":
		return "naturalness_low"
	case "persuasiveness":
		return "persuasiveness_low"
	default:
		return "retry_exhausted"
	}
}

func (s *HumanizeEvalService) persist(ctx context.Context, r *dto.HumanizeEvalResult) {
	if s.scoreRepo == nil || r == nil {
		return
	}
	score, dimensions := s.buildScoreFromResult(ctx, r)
	if err := s.scoreRepo.Save(ctx, score, dimensions); err != nil {
		logger.Errorf("[HumanizeEval] persist score: %v", err)
	}
}

func (s *HumanizeEvalService) buildScoreFromResult(ctx context.Context, r *dto.HumanizeEvalResult) (*model.HumanizeScore, []model.HumanizeDimensionRecord) {
	score := &model.HumanizeScore{
		TotalScore:         r.TotalScore,
		Threshold:          s.threshold,
		DistanceToChampion: r.DistanceToChampion,
		Passed:             r.Passed,
		AttemptCount:       r.AttemptCount,
		LLMModel:           r.LLMModel,
		LLMLatencyMs:       r.LLMLatencyMs,
		EvaluatorType:      model.HumanizeEvaluatorType(r.EvaluatorType),
		SampleStrategy:     model.HumanizeSampleStrategy(r.SampleStrategy),
		FinalReply:         r.FinalReply,
	}
	if r.Input != nil {
		score.SessionID = r.Input.SessionID
		score.CustomerID = r.Input.CustomerID
		score.MessageID = r.Input.MessageID
		score.Persona = r.Input.Persona
		score.Industry = r.Input.Industry
		score.Platform = r.Input.Platform
		score.Intent = r.Input.Intent
		score.CustomerMessage = r.Input.CustomerMessage
		score.AIReply = r.Input.AIReply
	}

	var dimensions []model.HumanizeDimensionRecord
	reasonMap := make(map[string]string, len(r.Scores))
	for _, sc := range r.Scores {
		reasonMap[string(sc.Dimension)] = sc.Reason
		switch sc.Dimension {
		case dto.HumanizeDimNaturalness:
			score.Naturalness = sc.Score
		case dto.HumanizeDimConciseness:
			score.Conciseness = sc.Score
		case dto.HumanizeDimEmpathy:
			score.Empathy = sc.Score
		case dto.HumanizeDimProfessionalism:
			score.Professionalism = sc.Score
		case dto.HumanizeDimPersuasiveness:
			score.Persuasiveness = sc.Score
		}
	}
	if len(r.Scores) > 0 {
		dimensions = make([]model.HumanizeDimensionRecord, 0, len(r.Scores))
		for _, sc := range r.Scores {
			dimensions = append(dimensions, model.HumanizeDimensionRecord{
				Dimension: string(sc.Dimension),
				Score:     sc.Score,
				Weight:    dto.HumanizeDimensionWeight[sc.Dimension],
				Reason:    sc.Reason,
			})
		}
	}
	reasonJSON, _ := json.Marshal(reasonMap)
	score.ReasonJSON = string(reasonJSON)
	return score, dimensions
}

func (s *HumanizeEvalService) buildLowQualitySample(ctx context.Context, input *dto.HumanizeEvalInput, result *dto.HumanizeEvalResult, sampleType string) *model.LowQualitySample {
	if input == nil || result == nil {
		return nil
	}
	validTypes := map[string]bool{
		"persona": true, "compliance": true, "naturalness": true, "relevance": true,
		"manual_review": true, "retry_exhausted": true,
		"naturalness_low": true, "persuasiveness_low": true,
		"champion_distance": true, "ab_test_loser": true,
	}
	if !validTypes[sampleType] {
		sampleType = "retry_exhausted"
	}
	scoresMap := make(map[string]float64, len(result.Scores))
	for _, sc := range result.Scores {
		scoresMap[string(sc.Dimension)] = sc.Score
	}
	scoresJSON, _ := json.Marshal(scoresMap)
	repliesJSON, _ := json.Marshal(result.AllReplies)
	return &model.LowQualitySample{
		CustomerID:       input.CustomerID,
		SessionID:        input.SessionID,
		SampleType:       model.LowQualitySampleType(sampleType),
		CustomerMessage:  input.CustomerMessage,
		AIReply:          result.FinalReply,
		Persona:          input.Persona,
		Industry:         input.Industry,
		Platform:         input.Platform,
		Intent:           input.Intent,
		DimensionScores:  string(scoresJSON),
		TotalScore:       result.TotalScore,
		Threshold:        s.threshold,
		AttemptCount:     result.AttemptCount,
		CandidateReplies: string(repliesJSON),
	}
}

var (
	_ HumanizeEvaluator = (*RuleScorerImpl)(nil)
	_ HumanizeEvaluator = (*LLMScorerImpl)(nil)
)
