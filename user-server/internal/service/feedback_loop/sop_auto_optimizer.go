package feedbackloop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// SOPAutoOptimizer SOP 自动优化器
type SOPAutoOptimizer struct {
	db      *gorm.DB
	repo    *repository.FeedbackLoopRepository
	bandit  BanditAllocatorInterface
	gateLLM gateLLM
	config  SOPAutoOptimizerConfig

	ledger *FailureLedger
}

// NewSOPAutoOptimizer 构造优化器
func NewSOPAutoOptimizer(db *gorm.DB, bandit BanditAllocatorInterface, cfg SOPAutoOptimizerConfig) *SOPAutoOptimizer {
	if cfg.AutoApplyPriority == 0 {
		cfg.AutoApplyPriority = 2
	}
	if cfg.RollbackDropThreshold == 0 {
		cfg.RollbackDropThreshold = 0.20
	}
	if cfg.RollbackComplaintRatio == 0 {
		cfg.RollbackComplaintRatio = 0.50
	}
	if cfg.ABTestDuration == 0 {
		cfg.ABTestDuration = 7 * 24 * time.Hour
	}
	return &SOPAutoOptimizer{
		db:     db,
		repo:   repository.NewFeedbackLoopRepositoryWithDB(db),
		bandit: bandit,
		config: cfg,
		ledger: NewFailureLedger(),
	}
}

func (o *SOPAutoOptimizer) getRepo() *repository.FeedbackLoopRepository {
	if o.repo == nil && o.db != nil {
		o.repo = repository.NewFeedbackLoopRepositoryWithDB(o.db)
	}
	return o.repo
}

// ProcessPendingSuggestions 处理待审核建议
//
// 1. 高 priority 自动应用 + 创建 A/B 测试
// 2. 中低 priority 留待人工审核
// 3. 检查进行中的 A/B 测试是否需要回滚
// 4. 检查进行中的 A/B 测试是否可以收敛选优
func (o *SOPAutoOptimizer) ProcessPendingSuggestions(ctx context.Context) (*dto.OptimizationReport, error) {
	report := &dto.OptimizationReport{
		RunAt:  time.Now(),
		Errors: make([]string, 0),
	}
	repo := o.getRepo()
	if repo == nil {
		return report, fmt.Errorf("repo is nil")
	}

	autoApply, err := repo.ListPendingSuggestions(ctx, o.config.AutoApplyPriority)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("query pending suggestions: %v", err))
		return report, err
	}
	report.PendingCount = len(autoApply)

	for i := range autoApply {

		if o.recentlyGateChecked(&autoApply[i]) {
			continue
		}
		gateRes := o.passGate(ctx, &autoApply[i])
		o.recordGateResult(ctx, autoApply[i].ID, gateRes)
		if !gateRes.Passed {
			logger.Warnf("[sop_gate] 建议 %d 未过验证门，转人工审核: %s", autoApply[i].ID, gateRes.Reason)

			o.ledger.RecordFailure(FailureAttribution{
				LineageID:   fmt.Sprintf("sop%d_sug%d", autoApply[i].SOPID, autoApply[i].ID),
				SampleScore: autoApply[i].CurrentScore,
				JudgeReason: gateRes.Reason,
				Decision:    "gate_failed",
			})
			continue
		}
		if err := o.autoApply(ctx, &autoApply[i]); err != nil {
			report.FailedCount++
			report.Errors = append(report.Errors, fmt.Sprintf("apply suggestion %d: %v", autoApply[i].ID, err))
			continue
		}
		if err := repo.MarkSuggestionApplied(ctx, autoApply[i].ID, time.Now()); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("mark suggestion %d applied: %v", autoApply[i].ID, err))
		}
		report.AppliedCount++
	}

	o.checkAndRollback(ctx, report)

	o.checkAndPromote(ctx, report)

	return report, nil
}

func (o *SOPAutoOptimizer) autoApply(ctx context.Context, sug *model.OptimizationSuggestion) error {
	switch sug.SuggestionType {
	case model.SuggestionTypePromptRewrite:
		return o.applyPromptRewrite(ctx, sug)
	case model.SuggestionTypeBranchPrune:
		return o.applyBranchPrune(ctx, sug)
	case model.SuggestionTypeNodeMerge:
		return o.applyNodeMerge(ctx, sug)
	case model.SuggestionTypeAddObjection:
		return o.applyAddObjection(ctx, sug)
	case model.SuggestionTypeAddEmpathy:
		return o.applyAddEmpathy(ctx, sug)
	case model.SuggestionTypeTimingAdjust:
		return o.applyTimingAdjust(ctx, sug)
	}
	return fmt.Errorf("unknown suggestion type: %s", sug.SuggestionType)
}

func (o *SOPAutoOptimizer) applyBranchPrune(ctx context.Context, sug *model.OptimizationSuggestion) error {
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-剪枝]", "branch_prune", SOPGraphMutatorForExperiment("branch_prune"))
}

func (o *SOPAutoOptimizer) applyNodeMerge(ctx context.Context, sug *model.OptimizationSuggestion) error {
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-节点合并]", "node_merge", SOPGraphMutatorForExperiment("node_merge"))
}

func (o *SOPAutoOptimizer) applyAddObjection(ctx context.Context, sug *model.OptimizationSuggestion) error {
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-异议处理]", "add_objection", SOPGraphMutatorForExperiment("add_objection"))
}

func (o *SOPAutoOptimizer) applyAddEmpathy(ctx context.Context, sug *model.OptimizationSuggestion) error {
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-共情补充]", "add_empathy", SOPGraphMutatorForExperiment("add_empathy"))
}

func (o *SOPAutoOptimizer) applyTimingAdjust(ctx context.Context, sug *model.OptimizationSuggestion) error {
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-时机调整]", "timing_adjust", SOPGraphMutatorForExperiment("timing_adjust"))
}

func (o *SOPAutoOptimizer) applyPromptRewrite(ctx context.Context, sug *model.OptimizationSuggestion) error {
	if o.gateLLM == nil {
		return fmt.Errorf("prompt_rewrite 需要 LLM 能力，当前未注入 dispatcher")
	}
	newPrompt, err := o.rewriteNodePrompt(ctx, sug)
	if err != nil {
		return fmt.Errorf("rewrite prompt: %w", err)
	}
	mutator := SOPGraphMutatorForNodePrompt(sug.NodeID, newPrompt)
	return o.getRepo().CloneSOPAndCreateABTestMutated(ctx, sug.SOPID, " [优化-Prompt重写]", "prompt_rewrite", mutator)
}

func (o *SOPAutoOptimizer) rewriteNodePrompt(ctx context.Context, sug *model.OptimizationSuggestion) (string, error) {
	graph, err := o.getRepo().GetSOPGraph(ctx, sug.SOPID)
	if err != nil {
		return "", fmt.Errorf("fetch sop graph: %w", err)
	}
	currentPrompt, ok := ExtractNodePrompt(graph, sug.NodeID)
	if !ok {
		return "", fmt.Errorf("sop %d 图中未找到节点 %q 的可用 prompt，拒绝以建议文本顶替重写基线", sug.SOPID, sug.NodeID)
	}
	systemPrompt := `你是销售 SOP 优化专家。给定一个 LLM 节点的当前 prompt 和优化建议，输出改进后的完整 prompt。
要求：保留原有业务意图与合规边界；按建议落实改进；输出仅含新 prompt 正文，不要解释。`
	userPrompt := fmt.Sprintf("当前 prompt：\n%s\n\n优化建议：\n%s\n\n输出改进后的 prompt：", currentPrompt, sug.SuggestionText)
	res, err := o.gateLLM.Dispatch(ctx, llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxTokens:    1024,
		Temperature:  0.3,
	})
	if err != nil {
		return "", err
	}
	out := ""
	if res != nil {
		out = res.Content
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("LLM 返回空 prompt")
	}
	return out, nil
}

func (o *SOPAutoOptimizer) checkAndRollback(ctx context.Context, report *dto.OptimizationReport) {
	tests, err := o.getRepo().ListRunningABTestsByType(ctx, model.BanditExperimentTypeSOPVariant)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("list running ab tests: %v", err))
		return
	}
	for _, t := range tests {
		controlRate, experimentRate := o.fetchConversionRates(ctx, t.ID)
		if controlRate > 0 && (controlRate-experimentRate)/controlRate > o.config.RollbackDropThreshold {
			if err := o.rollbackTest(ctx, t.ID, "conversion_drop"); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("rollback test %d (conversion_drop): %v", t.ID, err))
				continue
			}

			o.ledger.RecordFailure(FailureAttribution{
				LineageID:   t.ExperimentID,
				JudgeReason: "conversion_drop",
				Decision:    "rolled_back",
			})
			report.RolledBackCount++
			continue
		}
		controlComplaint, expComplaint := o.fetchComplaintRates(ctx, t.ID)
		if controlComplaint > 0 && (expComplaint-controlComplaint)/controlComplaint > o.config.RollbackComplaintRatio {
			if err := o.rollbackTest(ctx, t.ID, "complaint_spike"); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("rollback test %d (complaint_spike): %v", t.ID, err))
				continue
			}
			o.ledger.RecordFailure(FailureAttribution{
				LineageID:   t.ExperimentID,
				JudgeReason: "complaint_spike",
				Decision:    "rolled_back",
			})
			report.RolledBackCount++
		}
	}
}

func (o *SOPAutoOptimizer) checkAndPromote(ctx context.Context, report *dto.OptimizationReport) {
	if o.bandit == nil {
		return
	}
	tests, err := o.getRepo().ListRunningABTests(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("list running ab tests for promote: %v", err))
		return
	}
	for _, t := range tests {
		winner, ok := o.bandit.CheckConvergence(ctx, t.ExperimentID)
		if !ok {
			continue
		}
		if err := o.bandit.PromoteArm(ctx, t.ExperimentID, winner); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("promote arm (test=%d): %v", t.ID, err))
			continue
		}
		now := time.Now()
		if err := o.getRepo().UpdateABTestFields(ctx, t.ID, map[string]any{
			"status":         model.PromptABTestStatusCompleted,
			"winner_arm_key": winner,
			"ended_at":       now,
		}); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("update ab test %d to completed: %v", t.ID, err))
		}
		report.PromotedCount++
	}
}

func (o *SOPAutoOptimizer) fetchConversionRates(ctx context.Context, testID uint) (controlRate, experimentRate float64) {
	repo := o.getRepo()
	test, err := repo.GetPromptABTest(ctx, testID)
	if err != nil || test == nil {
		return 0, 0
	}
	controlTotal, _ := repo.CountFeedbackSignalsByVariant(ctx, test.SOPID, "A")
	controlSuccess, _ := repo.CountFeedbackSignalsByVariantAndOutcome(ctx, test.SOPID, "A", model.FeedbackSignalOutcomeSuccess)
	if controlTotal > 0 {
		controlRate = float64(controlSuccess) / float64(controlTotal)
	}
	expTotal, _ := repo.CountFeedbackSignalsByVariant(ctx, test.SOPID, "B")
	expSuccess, _ := repo.CountFeedbackSignalsByVariantAndOutcome(ctx, test.SOPID, "B", model.FeedbackSignalOutcomeSuccess)
	if expTotal > 0 {
		experimentRate = float64(expSuccess) / float64(expTotal)
	}
	return controlRate, experimentRate
}

func (o *SOPAutoOptimizer) fetchComplaintRates(ctx context.Context, testID uint) (controlRate, experimentRate float64) {
	repo := o.getRepo()
	test, err := repo.GetPromptABTest(ctx, testID)
	if err != nil || test == nil {
		return 0, 0
	}
	controlTotal, _ := repo.CountFeedbackEventsByVariant(ctx, test.SOPID, "A")
	controlComplaint, _ := repo.CountFeedbackEventsByVariantAndSignalKey(ctx, test.SOPID, "A", model.FeedbackSignalComplaint)
	if controlTotal > 0 {
		controlRate = float64(controlComplaint) / float64(controlTotal)
	}
	expTotal, _ := repo.CountFeedbackEventsByVariant(ctx, test.SOPID, "B")
	expComplaint, _ := repo.CountFeedbackEventsByVariantAndSignalKey(ctx, test.SOPID, "B", model.FeedbackSignalComplaint)
	if expTotal > 0 {
		experimentRate = float64(expComplaint) / float64(expTotal)
	}
	return controlRate, experimentRate
}

func (o *SOPAutoOptimizer) rollbackTest(ctx context.Context, testID uint, reason string) error {
	return o.getRepo().RollbackABTest(ctx, testID)
}
