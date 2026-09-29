package feedbackloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	textutil "hivemtk-user/internal/pkg/utils/text"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// PromptIterator Prompt 迭代器
type PromptIterator struct {
	repo       *repository.FeedbackLoopRepository
	dispatcher LLMDispatcher
	config     PromptIteratorConfig
}

// NewPromptIterator 构造迭代器
//
// 参数 db 仅为构造签名兼容保留，内部用 db 构造 repository，不存到 struct
func NewPromptIterator(db *gorm.DB, dispatcher LLMDispatcher, cfg PromptIteratorConfig) *PromptIterator {
	if cfg.MinSamplesForIteration == 0 {
		cfg.MinSamplesForIteration = 50
	}
	if cfg.NegativeRewardThreshold == 0 {
		cfg.NegativeRewardThreshold = -0.5
	}
	if cfg.CandidatesPerRun == 0 {
		cfg.CandidatesPerRun = 3
	}
	return &PromptIterator{
		repo:       repository.NewFeedbackLoopRepositoryWithDB(db),
		dispatcher: dispatcher,
		config:     cfg,
	}
}

// IterateForNode 为指定 SOP 节点迭代 Prompt
//
// 流程：
//  1. 拉取节点当前 active Prompt
//  2. 拉取最近 7 天负反馈样本
//  3. 样本数 < MinSamplesForIteration → 返回 ErrInsufficientSamples
//  4. LLM 生成 N 个候选
//  5. 入库 prompt_candidates（status=draft 或 approved）
//  6. AutoApprove=true 时自动创建 A/B 测试
//
// 返回：生成的候选列表
func (p *PromptIterator) IterateForNode(ctx context.Context, sopID uint, nodeID string) ([]model.PromptCandidate, error) {
	if p.repo == nil {
		return nil, fmt.Errorf("repo is nil")
	}
	if sopID == 0 || nodeID == "" {
		return nil, ErrInvalidInput
	}

	current, err := p.repo.GetActivePromptCandidate(ctx, sopID, nodeID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrActivePromptNotFound
		}
		return nil, fmt.Errorf("query active prompt: %w", err)
	}

	since := time.Now().Add(-7 * 24 * time.Hour)
	totalNegative, err := p.countNegativeSamples(ctx, sopID, since)
	if err != nil {
		return nil, fmt.Errorf("count negative samples: %w", err)
	}
	if int(totalNegative) < p.config.MinSamplesForIteration {
		return nil, fmt.Errorf("%w: %d < %d", ErrInsufficientSamples, totalNegative, p.config.MinSamplesForIteration)
	}
	samples, err := p.fetchNegativeSamples(ctx, sopID, since)
	if err != nil {
		return nil, fmt.Errorf("fetch negative samples: %w", err)
	}

	candidates, err := p.generateCandidates(ctx, *current, samples)
	if err != nil {
		return nil, fmt.Errorf("generate candidates: %w", err)
	}

	now := time.Now()
	for i := range candidates {
		candidates[i].ParentID = current.ID
		candidates[i].SOPID = sopID
		candidates[i].SOPNodeID = nodeID
		candidates[i].Status = model.PromptCandidateStatusDraft
		candidates[i].Alpha = 2
		candidates[i].Beta = 2
		candidates[i].GeneratedBy = "llm"
		if p.config.AutoApprove {
			candidates[i].Status = model.PromptCandidateStatusApproved
			candidates[i].ReviewedAt = &now
		}
		if err := p.repo.CreatePromptCandidate(ctx, &candidates[i]); err != nil {
			continue
		}
	}

	if p.config.AutoApprove && len(candidates) > 0 {
		p.createABTest(ctx, sopID, nodeID, *current, candidates)
	}

	return candidates, nil
}

func (p *PromptIterator) fetchNegativeSamples(ctx context.Context, sopID uint, since time.Time) ([]repository.NegativeSample, error) {
	return p.repo.FetchNegativeSamples(ctx, sopID, since, p.config.NegativeRewardThreshold)
}

func (p *PromptIterator) countNegativeSamples(ctx context.Context, sopID uint, since time.Time) (int64, error) {
	return p.repo.CountNegativeSamples(ctx, sopID, since, p.config.NegativeRewardThreshold)
}

func (p *PromptIterator) generateCandidates(ctx context.Context, current model.PromptCandidate, samples []repository.NegativeSample) ([]model.PromptCandidate, error) {
	if p.dispatcher == nil {
		return nil, ErrDispatcherNotConfig
	}
	var sampleStrs []string
	for _, s := range samples {
		sampleStrs = append(sampleStrs, fmt.Sprintf("【客户】%s\n【AI 回复】%s\n【反馈】%s（reward=%.2f）",
			s.CustomerMsg, s.AIReply, s.SignalKey, s.Reward))
	}
	prompt := fmt.Sprintf(`你是 Prompt 优化工程师。以下是一个销售 SOP 节点的当前 Prompt 和 N 条负反馈样本：

【当前 System Prompt】
%s

【当前 User Prompt 模板】
%s

【负反馈样本】
%s

请生成 %d 个改进版本，每个版本说明改进点。输出 JSON 数组：
[
  {
    "title": "版本标题",
    "system_prompt": "改进后的 system prompt",
    "user_prompt_template": "改进后的 user prompt 模板",
    "improvement_notes": "改进点说明"
  }
]`,
		current.SystemPrompt, current.UserPromptTemplate,
		strings.Join(sampleStrs, "\n\n"), p.config.CandidatesPerRun)

	content, _, err := p.dispatcher.Dispatch(ctx, "high_quality", prompt, "你是 Prompt 优化工程师，严格输出 JSON 数组。", true, 2000)
	if err != nil {
		return nil, err
	}
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON content in LLM response: %s", textutil.Truncate(content, 200))
	}
	var raw []struct {
		Title              string `json:"title"`
		SystemPrompt       string `json:"system_prompt"`
		UserPromptTemplate string `json:"user_prompt_template"`
		ImprovementNotes   string `json:"improvement_notes"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parse candidates: %w", err)
	}
	out := make([]model.PromptCandidate, 0, len(raw))
	for _, r := range raw {
		out = append(out, model.PromptCandidate{
			Scenario:           current.Scenario,
			Version:            nextVersion(current.Version),
			Title:              r.Title,
			SystemPrompt:       r.SystemPrompt,
			UserPromptTemplate: r.UserPromptTemplate,
			ImprovementNotes:   r.ImprovementNotes,
		})
	}
	return out, nil
}

func (p *PromptIterator) createABTest(ctx context.Context, sopID uint, nodeID string, current model.PromptCandidate, newCandidates []model.PromptCandidate) {
	experimentID := fmt.Sprintf("sop%d_node%s_%d", sopID, nodeID, time.Now().Unix())
	armKeys := []string{"arm_0_original"}
	for i := range newCandidates {
		armKeys = append(armKeys, fmt.Sprintf("arm_%d_%s", i+1, newCandidates[i].Version))
	}
	now := time.Now()
	abTest := &model.PromptABTest{
		ExperimentID:   experimentID,
		ExperimentType: model.BanditExperimentTypePrompt,
		SOPID:          sopID,
		SOPNodeID:      nodeID,
		Name:           fmt.Sprintf("SOP %d 节点 %s Prompt 迭代", sopID, nodeID),
		ArmKeys:        model.JSONArray(armKeysToInterface(armKeys)),
		Config:         model.JSONMap{"min_traffic": 10, "max_traffic": 60, "min_samples": 100, "posterior_threshold": 0.95},
		Status:         model.PromptABTestStatusRunning,
		StartedAt:      &now,
		AutoPromote:    true,
	}
	if err := p.repo.CreatePromptABTest(ctx, abTest); err != nil {
		return
	}

	arms := []model.BanditArm{
		{
			ExperimentID:      experimentID,
			ExperimentType:    model.BanditExperimentTypePrompt,
			ArmKey:            "arm_0_original",
			SOPID:             sopID,
			PromptCandidateID: current.ID,
			Alpha:             2,
			Beta:              2,
			Status:            model.BanditArmStatusExploring,
		},
	}
	for i, c := range newCandidates {
		arms = append(arms, model.BanditArm{
			ExperimentID:      experimentID,
			ExperimentType:    model.BanditExperimentTypePrompt,
			ArmKey:            armKeys[i+1],
			SOPID:             sopID,
			PromptCandidateID: c.ID,
			Alpha:             2,
			Beta:              2,
			Status:            model.BanditArmStatusExploring,
		})
	}
	_ = p.repo.CreateBanditArmsInBatches(ctx, arms, 100)
}

func armKeysToInterface(keys []string) []any {
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

func nextVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "v1.1"
	}
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return v + ".1"
	}
	major := parts[0]
	minor := parts[1]
	var minorInt int
	if _, err := fmt.Sscanf(minor, "%d", &minorInt); err != nil {
		return v + ".1"
	}
	minorInt++
	if minorInt >= 10 {
		var majorInt int
		if strings.HasPrefix(major, "v") {
			if _, err := fmt.Sscanf(major[1:], "%d", &majorInt); err != nil {
				return v + ".1"
			}
		} else {
			if _, err := fmt.Sscanf(major, "%d", &majorInt); err != nil {
				return v + ".1"
			}
		}
		majorInt++
		return fmt.Sprintf("v%d.0", majorInt)
	}
	return fmt.Sprintf("%s.%d", major, minorInt)
}
