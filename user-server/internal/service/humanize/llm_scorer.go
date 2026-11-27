package humanize

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// LLMScorerImpl LLM 评估器实现
type LLMScorerImpl struct {
	dispatcher       LLMDispatcher
	baseline         *model.ChampionBaseline
	selfConsistencyN int
	temperature      float64
}

// NewLLMScorer 构造 LLM 评估器
func NewLLMScorer(dispatcher LLMDispatcher) *LLMScorerImpl {
	return &LLMScorerImpl{
		dispatcher:       dispatcher,
		selfConsistencyN: 3,
		temperature:      0.3,
	}
}

// WithBaseline 注入销冠基线（每次评估前注入）
func (s *LLMScorerImpl) WithBaseline(b *model.ChampionBaseline) *LLMScorerImpl {
	s.baseline = b
	return s
}

// WithSelfConsistencyN 设置 self-consistency 采样次数
func (s *LLMScorerImpl) WithSelfConsistencyN(n int) *LLMScorerImpl {
	if n > 0 {
		s.selfConsistencyN = n
	}
	return s
}

// WithTemperature 设置采样温度
func (s *LLMScorerImpl) WithTemperature(t float64) *LLMScorerImpl {
	if t > 0 && t <= 1 {
		s.temperature = t
	}
	return s
}

// Evaluate 单次 LLM 评估
func (s *LLMScorerImpl) Evaluate(ctx context.Context, input *dto.HumanizeEvalInput) (*dto.HumanizeEvalResult, error) {
	if input == nil || input.AIReply == "" {
		return nil, fmt.Errorf("%w: input or ai_reply empty", ErrInvalidInput)
	}
	if s.dispatcher == nil {
		return nil, fmt.Errorf("llm dispatcher is nil")
	}

	start := time.Now()
	results := make([]*dto.HumanizeEvalResult, 0, s.selfConsistencyN)
	lastErr := error(nil)
	callCtx := LLMCallContext{
		ExecutionScenarioID: agentcontext.ExecutionScenarioHumanizeEval,
		Step:                "evaluate",
		ContextView:         input.ContextView,
	}
	contextDispatcher, hasContextDispatcher := s.dispatcher.(LLMContextDispatcher)
	for i := 0; i < s.selfConsistencyN; i++ {
		prompt := buildHumanizeLLMPrompt(input, s.baseline)
		var content, model string
		var err error
		if hasContextDispatcher {
			content, model, err = contextDispatcher.ChatSendWithContext(ctx, prompt, callCtx)
		} else {
			content, model, err = s.dispatcher.ChatSend(ctx, prompt)
		}
		if err != nil {
			lastErr = err
			continue
		}
		parsed, perr := parseHumanizeEvalResult(content)
		if perr != nil {
			lastErr = perr
			continue
		}
		parsed.LLMModel = model
		results = append(results, parsed)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("all %d self-consistency samples failed: %w", s.selfConsistencyN, lastErr)
	}

	final := pickMedianResult(results)
	final.LLMLatencyMs = int(time.Since(start).Milliseconds())
	final.SampleStrategy = "sampled"
	final.EvaluatorType = "llm"
	final.Input = input
	final.CalculatedAt = time.Now()

	if s.baseline != nil {
		final.DistanceToChampion = weightedEuclideanDistance(final.Scores, s.baseline)
	}

	return final, nil
}

func buildHumanizeLLMPrompt(input *dto.HumanizeEvalInput, baseline *model.ChampionBaseline) string {
	var sb strings.Builder
	sb.WriteString("你是一位资深销售对话质检员，正在评估 AI 销冠回复的拟人度。\n")
	sb.WriteString("请先输出评估步骤（Chain-of-Thought），再按 5 维度打分。\n\n")
	sb.WriteString(BuildHumanizeContextBlock(input.ContextView))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("【客户消息】%s\n", input.CustomerMessage))
	sb.WriteString(fmt.Sprintf("【AI 回复】%s\n", input.AIReply))
	if input.Persona != "" {
		sb.WriteString(fmt.Sprintf("【销冠人设】%s\n", input.Persona))
	}
	if input.Industry != "" {
		sb.WriteString(fmt.Sprintf("【行业】%s\n", input.Industry))
	}
	if input.Platform != "" {
		sb.WriteString(fmt.Sprintf("【平台】%s\n", input.Platform))
	}
	if input.Intent != "" {
		sb.WriteString(fmt.Sprintf("【意图】%s\n", input.Intent))
	}
	if baseline != nil {
		sb.WriteString(fmt.Sprintf("【销冠基线分】naturalness=%.3f conciseness=%.3f empathy=%.3f professionalism=%.3f persuasiveness=%.3f\n",
			baseline.Naturalness, baseline.Conciseness, baseline.Empathy, baseline.Professionalism, baseline.Persuasiveness))
	}
	sb.WriteString("\n【维度定义】\n")
	sb.WriteString("- naturalness（权重 0.25）：口语化、无'作为 AI'等套话、句长有变化（burstiness）。0=机械感极强，1=真人感极强。\n")
	sb.WriteString("- conciseness（权重 0.15）：字数与意图匹配，不冗余。0=极冗长或极敷衍，1=恰到好处。\n")
	sb.WriteString("- empathy（权重 0.20）：是否共情客户情绪。投诉场景必须共情，否则本维直接 ≤0.4。0=冷漠，1=高度共情。\n")
	sb.WriteString("- professionalism（权重 0.20）：行业专业词密度、人设一致性。0=外行话，1=专家表达。\n")
	sb.WriteString("- persuasiveness（权重 0.20）：是否推进交易、是否给出明确建议或行动召唤。0=被动响应，1=主动推进。\n\n")
	sb.WriteString("【评估步骤】请按以下步骤输出：\n")
	sb.WriteString("1. 列出 AI 回复中的 AI 痕迹词（如有）\n")
	sb.WriteString("2. 统计字数与句长方差\n")
	sb.WriteString("3. 识别客户情绪并检查 AI 是否共情\n")
	sb.WriteString("4. 列出 AI 回复中的行业专业词\n")
	sb.WriteString("5. 判断是否有明确的行动召唤或建议\n")
	sb.WriteString("6. 对比销冠基线分，说明每维差距\n\n")
	sb.WriteString("【输出 JSON】\n")
	sb.WriteString(`{"scores":[{"dimension":"naturalness","score":0.85,"reason":"..."},{"dimension":"conciseness","score":0.9,"reason":"..."},{"dimension":"empathy","score":0.8,"reason":"..."},{"dimension":"professionalism","score":0.85,"reason":"..."},{"dimension":"persuasiveness","score":0.8,"reason":"..."}],"total_score":0.85,"distance_to_champion":0.12}`)
	return sb.String()
}

func parseHumanizeEvalResult(content string) (*dto.HumanizeEvalResult, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	if !strings.HasPrefix(content, "{") {
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start >= 0 && end > start {
			content = content[start : end+1]
		}
	}
	var raw struct {
		Scores             []dto.HumanizeDimensionScore `json:"scores"`
		TotalScore         float64                      `json:"total_score"`
		DistanceToChampion float64                      `json:"distance_to_champion"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(raw.Scores) == 0 {
		return nil, fmt.Errorf("empty scores")
	}
	for i := range raw.Scores {
		raw.Scores[i].Score = clampScore(raw.Scores[i].Score)
	}
	total := raw.TotalScore
	if total <= 0 {
		total = computeHumanizeWeightedScore(raw.Scores)
	}
	return &dto.HumanizeEvalResult{
		Scores:             raw.Scores,
		TotalScore:         total,
		DistanceToChampion: raw.DistanceToChampion,
		SampleStrategy:     "sampled",
	}, nil
}

func pickMedianResult(results []*dto.HumanizeEvalResult) *dto.HumanizeEvalResult {
	if len(results) == 1 {
		return results[0]
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalScore < results[j].TotalScore
	})
	return results[len(results)/2]
}

func weightedEuclideanDistance(scores []dto.HumanizeDimensionScore, baseline *model.ChampionBaseline) float64 {
	if baseline == nil {
		return 0
	}
	m := make(map[dto.HumanizeDimension]float64, len(scores))
	for _, s := range scores {
		m[s.Dimension] = s.Score
	}
	baselineMap := map[dto.HumanizeDimension]float64{
		dto.HumanizeDimNaturalness:     baseline.Naturalness,
		dto.HumanizeDimConciseness:     baseline.Conciseness,
		dto.HumanizeDimEmpathy:         baseline.Empathy,
		dto.HumanizeDimProfessionalism: baseline.Professionalism,
		dto.HumanizeDimPersuasiveness:  baseline.Persuasiveness,
	}
	sum := 0.0
	for _, dim := range dto.AllHumanizeDimensions {
		diff := m[dim] - baselineMap[dim]
		w := dto.HumanizeDimensionWeight[dim]
		sum += w * diff * diff
	}
	return math.Round(math.Sqrt(sum)*10000) / 10000
}

// SampleDecision 采样决策
type SampleDecision struct {
	NeedLLM  bool
	Strategy string
}

func decideLLMSample(ruleScore, threshold, boundaryLow, boundaryHigh, sampleRate float64, r *rand.Rand) SampleDecision {
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if ruleScore >= boundaryLow && ruleScore < boundaryHigh {
		return SampleDecision{NeedLLM: true, Strategy: "boundary"}
	}
	if ruleScore < threshold && ruleScore < boundaryLow {
		if r.Float64() < sampleRate {
			return SampleDecision{NeedLLM: true, Strategy: "sampled"}
		}
	}
	if ruleScore >= threshold {
		if r.Float64() < 0.01 {
			return SampleDecision{NeedLLM: true, Strategy: "sampled_monitor"}
		}
	}
	return SampleDecision{NeedLLM: false, Strategy: "full"}
}
