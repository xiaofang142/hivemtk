package eval

// dialogue_gold.go T-P8-04：对话级 gold set 的类型、纯离线评分与运行器（LTC-15）。
//
// 为什么不用 Evaluator：它是单轮问答形状（candidate vs reference 一次打分），
// 装不下"一轮对话多回合 + 每回合期望动作"的对话级语义；
// 为什么不用 RagEvalRun：那是检索指标形状（recall/MRR/NDCG）。
// 本文件的评分只依赖 ChrFEvaluator（纯本地，无 LLM），CI 可跑（AC②）。
//
// 评价维度（turn 级，只评 assistant 回合，user 回合是题面不参评）：
//  1. ChrF：候选回复 vs 标准回复的字符/词 n-gram F 分；
//  2. 关键词覆盖：MustContain 逐个 strings.Contains，空表 = 1.0；
//  3. 动作一致：候选 ExpectedAction == 标准 ExpectedAction，标准为空 = 跳过。
// composite = 0.5*chrf + 0.3*keycov + 0.2*action；动作跳过时按 /0.8 重归一。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"hivemtk-user/internal/model"
)

// DialogueTurnRoleUser / DialogueTurnRoleAssistant 回合角色。题面与作答的区分只看它。
const (
	DialogueTurnRoleUser      = "user"
	DialogueTurnRoleAssistant = "assistant"
)

// DialogueTurn 一次对话中的一个回合。
type DialogueTurn struct {
	Role           string   `json:"role"`
	Text           string   `json:"text"`
	ExpectedAction string   `json:"expected_action,omitempty"`
	MustContain    []string `json:"must_contain,omitempty"`
}

// DialogueSample 一条 gold 对话：标准 transcript + 退化 transcript。
type DialogueSample struct {
	ID       string `json:"id"`
	Scene    string `json:"scene"`
	CaseID   string `json:"case_id,omitempty"`
	Label    string `json:"label,omitempty"`
	FixLayer string `json:"fix_layer,omitempty"`
	// Turns 标准 transcript；Degraded 退化 transcript（与 Turns 等长对齐，
	// user 回合原样，assistant 回合删关键词 + 翻动作，用于 AC③反向掉分）。
	Turns    []DialogueTurn `json:"turns"`
	Degraded []DialogueTurn `json:"degraded,omitempty"`
}

// DialogueTurnScore 单回合分数。
type DialogueTurnScore struct {
	Index         int     `json:"index"`
	Chrf          float64 `json:"chrf"`
	KeyCoverage   float64 `json:"key_coverage"`
	ActionMatch   bool    `json:"action_match"`
	ActionSkipped bool    `json:"action_skipped"`
	Composite     float64 `json:"composite"`
}

// DialogueSampleScore 单样本分数。
type DialogueSampleScore struct {
	SampleID  string              `json:"sample_id"`
	Turns     []DialogueTurnScore `json:"turns"`
	Composite float64             `json:"composite"`
}

// DialogueGoldResult 一次评测运行的汇总。
type DialogueGoldResult struct {
	SampleCount     int                   `json:"sample_count"`
	CompositeAvg    float64               `json:"composite_avg"`
	KeyCoverageAvg  float64               `json:"key_coverage_avg"`
	ActionMatchRate float64               `json:"action_match_rate"`
	ChrfAvg         float64               `json:"chrf_avg"`
	Samples         []DialogueSampleScore `json:"samples"`
}

// ScoreDialogueTurn 评一个 assistant 回合：cand 是被测，ref 是标准。
func ScoreDialogueTurn(chrf *ChrFEvaluator, cand, ref DialogueTurn) DialogueTurnScore {
	out := DialogueTurnScore{}
	out.Chrf = chrf.Score(cand.Text, ref.Text)
	out.KeyCoverage = keyCoverage(cand.Text, ref.MustContain)
	if strings.TrimSpace(ref.ExpectedAction) == "" {
		out.ActionSkipped = true
	} else {
		out.ActionMatch = strings.TrimSpace(cand.ExpectedAction) == strings.TrimSpace(ref.ExpectedAction)
	}
	actionScore := 0.0
	if out.ActionMatch {
		actionScore = 1.0
	}
	if out.ActionSkipped {
		out.Composite = (0.5*out.Chrf + 0.3*out.KeyCoverage) / 0.8
	} else {
		out.Composite = 0.5*out.Chrf + 0.3*out.KeyCoverage + 0.2*actionScore
	}
	return out
}

func keyCoverage(text string, must []string) float64 {
	if len(must) == 0 {
		return 1.0
	}
	hit := 0
	for _, k := range must {
		if k != "" && strings.Contains(text, k) {
			hit++
		}
	}
	return float64(hit) / float64(len(must))
}

// RunDialogueGold 跑一批样本。useDegraded=false 评标准 transcript（基线），
// true 评退化 transcript（反向）。Degraded 与 Turns 长度不一致直接报错，
// 不静默截断（截断会把"退化版少一轮"这种 fixture 腐烂藏起来）。
func RunDialogueGold(chrf *ChrFEvaluator, samples []DialogueSample, useDegraded bool) (DialogueGoldResult, error) {
	var out DialogueGoldResult
	actionHit, actionTotal := 0, 0
	for _, s := range samples {
		transcript := s.Turns
		if useDegraded {
			if len(s.Degraded) == 0 {
				return out, fmt.Errorf("dialogue gold: 样本 %s 没有 degraded transcript", s.ID)
			}
			if len(s.Degraded) != len(s.Turns) {
				return out, fmt.Errorf("dialogue gold: 样本 %s degraded 长度 %d 与标准 %d 不一致",
					s.ID, len(s.Degraded), len(s.Turns))
			}
			transcript = s.Degraded
		}
		ss := DialogueSampleScore{SampleID: s.ID}
		for i, ref := range s.Turns {
			if ref.Role != DialogueTurnRoleAssistant {
				continue
			}
			ts := ScoreDialogueTurn(chrf, transcript[i], ref)
			ts.Index = i
			ss.Turns = append(ss.Turns, ts)
			out.ChrfAvg += ts.Chrf
			out.KeyCoverageAvg += ts.KeyCoverage
			if !ts.ActionSkipped {
				actionTotal++
				if ts.ActionMatch {
					actionHit++
				}
			}
		}
		if len(ss.Turns) > 0 {
			sum := 0.0
			for _, ts := range ss.Turns {
				sum += ts.Composite
			}
			ss.Composite = sum / float64(len(ss.Turns))
		}
		out.CompositeAvg += ss.Composite
		out.Samples = append(out.Samples, ss)
	}
	out.SampleCount = len(samples)
	if out.SampleCount > 0 {
		out.CompositeAvg /= float64(out.SampleCount)
	}
	turns := 0
	for _, ss := range out.Samples {
		turns += len(ss.Turns)
	}
	if turns > 0 {
		out.ChrfAvg /= float64(turns)
		out.KeyCoverageAvg /= float64(turns)
	}
	if actionTotal > 0 {
		out.ActionMatchRate = float64(actionHit) / float64(actionTotal)
	} else {
		out.ActionMatchRate = 1.0
	}
	return out, nil
}

// BadCaseToDialogueSample 把一条 bad-case 导出转成 gold 样本（T-P8-03 → T-P8-04 的送料口）。
//
// bad-case 是单轮形状（一问一答），转出来是单 assistant 回合样本：
// 动作期望未知（置空 = 评分跳过动作维），关键词表为空（覆盖率恒 1.0）。
// LabelNote 是标准答案的注脚，收进 Label 供人工复核，不进自动评分。
func BadCaseToDialogueSample(b *model.BadCase) DialogueSample {
	if b == nil {
		return DialogueSample{}
	}
	return DialogueSample{
		ID:       "badcase-" + b.ID,
		Scene:    "badcase",
		CaseID:   b.ID,
		Label:    b.Label,
		FixLayer: b.FixLayer,
		Turns: []DialogueTurn{
			{Role: DialogueTurnRoleUser, Text: b.QueryText},
			{Role: DialogueTurnRoleAssistant, Text: b.AnswerText},
		},
	}
}

// LoadDialogueGold 从 JSON 文件读样本集。
func LoadDialogueGold(path string) ([]DialogueSample, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dialogue gold: 读 %s 失败：%w", path, err)
	}
	var samples []DialogueSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		return nil, fmt.Errorf("dialogue gold: 解析 %s 失败：%w", path, err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("dialogue gold: %s 为空", path)
	}
	return samples, nil
}

// ToRunRecord 把一次运行汇总转成入库行。Details 压逐样本明细，反向掉分定位到样本靠它。
func (r DialogueGoldResult) ToRunRecord(name string) model.DialogueEvalRun {
	details, _ := json.Marshal(r.Samples)
	return model.DialogueEvalRun{
		Name:            name,
		SampleCount:     r.SampleCount,
		CompositeAvg:    r.CompositeAvg,
		KeyCoverageAvg:  r.KeyCoverageAvg,
		ActionMatchRate: r.ActionMatchRate,
		ChrfAvg:         r.ChrfAvg,
		Details:         string(details),
	}
}
