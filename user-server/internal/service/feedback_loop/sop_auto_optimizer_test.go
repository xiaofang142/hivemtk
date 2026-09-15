package feedbackloop

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestSOPAutoOptimizer_ProcessPending_NoSuggestions 无建议
func TestSOPAutoOptimizer_ProcessPending_NoSuggestions(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	bandit := newStubBanditAllocator()
	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())

	report, err := o.ProcessPendingSuggestions(context.Background())
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.PendingCount != 0 {
		t.Errorf("PendingCount = %d want 0", report.PendingCount)
	}
	if report.AppliedCount != 0 {
		t.Errorf("AppliedCount = %d want 0", report.AppliedCount)
	}
}

// TestSOPAutoOptimizer_ProcessPending_BranchPrune 高 priority 自动应用
//
// 准备：1 个 SOP + 1 个 pending + priority=2 建议（branch_prune）
// L-1 验证门（批次①）：自动应用前须过黄金回归门 → 补种 golden 案例 + 注入门 LLM
// 验证：
//   - AppliedCount = 1
//   - 创建 variant SOP
//   - 创建 A/B 测试 + 2 个 bandit arms
//   - suggestion 状态 → applied
func TestSOPAutoOptimizer_ProcessPending_BranchPrune(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	testutil.NewTestDB(t, &model.TraceEvalLog{}, &model.MessageTrace{})
	seedGoldenCases(t, db, minGoldenCases+2)

	sop := model.SOPAgent{
		Name: "test-sop", Scenario: "test", IsActive: true,
		SOPGraph: model.JSONMap{"nodes": []any{}},
	}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatalf("seed sop: %v", err)
	}
	sug := model.OptimizationSuggestion{
		SOPID: sop.ID, SOPName: "test-sop",
		NodeID: "node_1", NodeName: "test-node",
		SuggestionType: model.SuggestionTypeBranchPrune,
		SuggestionText: "建议剪枝低效分支",
		Priority:       2, Status: model.SuggestionStatusPending,
	}
	if err := db.Create(&sug).Error; err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}

	bandit := newStubBanditAllocator()
	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(&fakeGateLLM{content: gateVerdict(true, "无损害")})
	report, err := o.ProcessPendingSuggestions(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.AppliedCount != 1 {
		t.Errorf("AppliedCount = %d want 1", report.AppliedCount)
	}
	if report.FailedCount != 0 {
		t.Errorf("FailedCount = %d want 0", report.FailedCount)
	}

	var sopCount int64
	db.Model(&model.SOPAgent{}).Where("name LIKE ?", "%[优化-剪枝]%").Count(&sopCount)
	if sopCount != 1 {
		t.Errorf("variant SOP count = %d want 1", sopCount)
	}

	var abTestCount int64
	db.Model(&model.PromptABTest{}).Where("sop_id = ? AND experiment_type = ?", sop.ID, model.BanditExperimentTypeSOPVariant).Count(&abTestCount)
	if abTestCount != 1 {
		t.Errorf("A/B test count = %d want 1", abTestCount)
	}

	var abTest model.PromptABTest
	_ = db.Where("sop_id = ? AND experiment_type = ?", sop.ID, model.BanditExperimentTypeSOPVariant).First(&abTest).Error
	var arms []model.BanditArm
	db.Where("experiment_id = ?", abTest.ExperimentID).Find(&arms)
	if len(arms) != 2 {
		t.Errorf("bandit arms = %d want 2", len(arms))
	}
	hasArmA, hasArmB := false, false
	for _, a := range arms {
		if a.ArmKey == "arm_a_original" {
			hasArmA = true
		}
		if a.ArmKey == "arm_b_variant" {
			hasArmB = true
		}
	}
	if !hasArmA || !hasArmB {
		t.Errorf("应包含 arm_a_original 和 arm_b_variant, got hasA=%v hasB=%v", hasArmA, hasArmB)
	}

	var updated model.OptimizationSuggestion
	_ = db.First(&updated, sug.ID).Error
	if updated.Status != model.SuggestionStatusApplied {
		t.Errorf("suggestion status = %q want applied", updated.Status)
	}
	if updated.AppliedAt == nil {
		t.Errorf("AppliedAt 应非空")
	}
}

// TestSOPAutoOptimizer_ProcessPending_LowPriorityNotApplied 低 priority 不自动应用
//
// priority=1 < AutoApplyPriority=2，不应自动应用
func TestSOPAutoOptimizer_ProcessPending_LowPriorityNotApplied(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	sop := model.SOPAgent{
		Name: "test-sop-low", Scenario: "test", IsActive: true,
		SOPGraph: model.JSONMap{"nodes": []any{}},
	}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatalf("seed sop: %v", err)
	}
	sug := model.OptimizationSuggestion{
		SOPID: sop.ID, SOPName: "test-sop-low",
		SuggestionType: model.SuggestionTypeBranchPrune,
		SuggestionText: "低 priority 建议",
		Priority:       1,
		Status:         model.SuggestionStatusPending,
	}
	if err := db.Create(&sug).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	bandit := newStubBanditAllocator()
	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())
	report, err := o.ProcessPendingSuggestions(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.PendingCount != 0 {
		t.Errorf("priority=1 不应被查询到 (AutoApplyPriority=2), PendingCount = %d want 0", report.PendingCount)
	}
	if report.AppliedCount != 0 {
		t.Errorf("AppliedCount = %d want 0 (低 priority 不应用)", report.AppliedCount)
	}
}

// TestSOPAutoOptimizer_ProcessPending_UnknownSuggestionType 未知建议类型失败
//
// L-1 验证门通过后（补种 golden + 门 LLM），autoApply 对未知类型报错 → FailedCount=1
func TestSOPAutoOptimizer_ProcessPending_UnknownSuggestionType(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	testutil.NewTestDB(t, &model.TraceEvalLog{}, &model.MessageTrace{})
	seedGoldenCases(t, db, minGoldenCases+2)

	sop := model.SOPAgent{
		Name: "test-sop-unknown", Scenario: "test", IsActive: true,
		SOPGraph: model.JSONMap{"nodes": []any{}},
	}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	sug := model.OptimizationSuggestion{
		SOPID: sop.ID, SOPName: "test",
		SuggestionType: "unknown_type",
		SuggestionText: "未知类型建议",
		Priority:       3,
		Status:         model.SuggestionStatusPending,
	}
	if err := db.Create(&sug).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	bandit := newStubBanditAllocator()
	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(&fakeGateLLM{content: gateVerdict(true, "无损害")})
	report, err := o.ProcessPendingSuggestions(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.FailedCount != 1 {
		t.Errorf("FailedCount = %d want 1", report.FailedCount)
	}
	if len(report.Errors) == 0 {
		t.Errorf("Errors 应记录失败")
	}
}

// TestSOPAutoOptimizer_CheckAndPromote_Converged 收敛触发选优
//
// 准备：1 个 running A/B 测试 + bandit stub 返回已收敛
// 验证：
//   - PromoteCount = 1
//   - A/B 测试状态 → completed
//   - bandit.PromoteArm 被调用
func TestSOPAutoOptimizer_CheckAndPromote_Converged(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	expID := "test_promote_converged"
	now := time.Now()
	abTest := model.PromptABTest{
		ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant,
		SOPID: 1, Name: "test",
		ArmKeys:   model.JSONArray([]any{"arm_a", "arm_b"}),
		Config:    model.JSONMap{},
		Status:    model.PromptABTestStatusRunning,
		StartedAt: &now,
	}
	if err := db.Create(&abTest).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	bandit := newStubBanditAllocator()
	bandit.SetConverged(expID, "arm_a")

	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())
	report, err := o.ProcessPendingSuggestions(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.PromotedCount != 1 {
		t.Errorf("PromotedCount = %d want 1", report.PromotedCount)
	}

	var updated model.PromptABTest
	_ = db.First(&updated, abTest.ID).Error
	if updated.Status != model.PromptABTestStatusCompleted {
		t.Errorf("abTest status = %q want completed", updated.Status)
	}
	if updated.WinnerArmKey != "arm_a" {
		t.Errorf("WinnerArmKey = %q want arm_a", updated.WinnerArmKey)
	}

	promotes := bandit.PromoteCalls()
	if len(promotes) != 1 {
		t.Errorf("PromoteArm calls = %d want 1", len(promotes))
	}
	if len(promotes) > 0 {
		if promotes[0].ExperimentID != expID || promotes[0].WinnerKey != "arm_a" {
			t.Errorf("PromoteArm call = %+v want expID=%s winner=arm_a", promotes[0], expID)
		}
	}
}

// TestSOPAutoOptimizer_CheckAndRollback_ConversionDrop 转化率下降触发回滚
//
// 准备：1 个 running SOP variant A/B 测试
//   - variant A：10 条 signal，5 条 success → 转化率 50%
//   - variant B：10 条 signal，1 条 success → 转化率 10%
//   - 下降比例 = (50-10)/50 = 80% > 20% 阈值
//
// 验证：
//   - RolledBackCount = 1
//   - A/B 测试状态 → rolled_back
//   - bandit arms 状态 → retired
func TestSOPAutoOptimizer_CheckAndRollback_ConversionDrop(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	sopID := uint(100)
	expID := "test_rollback_conversion"
	now := time.Now()
	abTest := model.PromptABTest{
		ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant,
		SOPID: sopID, Name: "rollback test",
		ArmKeys:   model.JSONArray([]any{"arm_a_original", "arm_b_variant"}),
		Config:    model.JSONMap{},
		Status:    model.PromptABTestStatusRunning,
		StartedAt: &now,
	}
	if err := db.Create(&abTest).Error; err != nil {
		t.Fatalf("seed abTest: %v", err)
	}

	arms := []model.BanditArm{
		{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "arm_a_original", SOPID: sopID, Variant: "A", Status: model.BanditArmStatusExploring},
		{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "arm_b_variant", SOPID: sopID, Variant: "B", Status: model.BanditArmStatusExploring},
	}
	if err := db.Create(&arms).Error; err != nil {
		t.Fatalf("seed arms: %v", err)
	}

	for i := 0; i < 10; i++ {
		outcome := model.FeedbackSignalOutcomePending
		if i < 5 {
			outcome = model.FeedbackSignalOutcomeSuccess
		}
		signal := model.FeedbackSignal{
			SessionID:  fmt.Sprintf("sess-a-%d", i),
			CustomerID: "cust-1", SOPID: sopID, Variant: "A",
			Outcome: outcome, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&signal).Error; err != nil {
			t.Fatalf("seed signal A %d: %v", i, err)
		}
	}
	for i := 0; i < 10; i++ {
		outcome := model.FeedbackSignalOutcomePending
		if i < 1 {
			outcome = model.FeedbackSignalOutcomeSuccess
		}
		signal := model.FeedbackSignal{
			SessionID:  fmt.Sprintf("sess-b-%d", i),
			CustomerID: "cust-1", SOPID: sopID, Variant: "B",
			Outcome: outcome, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&signal).Error; err != nil {
			t.Fatalf("seed signal B %d: %v", i, err)
		}
	}

	bandit := newStubBanditAllocator()
	o := NewSOPAutoOptimizer(db, bandit, DefaultSOPAutoOptimizerConfig())
	report, err := o.ProcessPendingSuggestions(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSuggestions: %v", err)
	}
	if report.RolledBackCount != 1 {
		t.Errorf("RolledBackCount = %d want 1 (转化率下降 80%% > 20%%)", report.RolledBackCount)
	}

	var updated model.PromptABTest
	_ = db.First(&updated, abTest.ID).Error
	if updated.Status != model.PromptABTestStatusRolledBack {
		t.Errorf("abTest status = %q want rolled_back", updated.Status)
	}

	var retiredArmsCount int64
	db.Model(&model.BanditArm{}).Where("experiment_id = ? AND status = ?", expID, model.BanditArmStatusRetired).Count(&retiredArmsCount)
	if retiredArmsCount != 2 {
		t.Errorf("retired arms = %d want 2", retiredArmsCount)
	}
}

// TestSOPAutoOptimizer_FetchConversionRates 拉取转化率
func TestSOPAutoOptimizer_FetchConversionRates(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	sopID := uint(200)
	now := time.Now()
	abTest := model.PromptABTest{
		ExperimentID: "test_fetch_conv", ExperimentType: model.BanditExperimentTypeSOPVariant,
		SOPID: sopID, Name: "test",
		ArmKeys: model.JSONArray([]any{"a", "b"}),
		Config:  model.JSONMap{}, Status: model.PromptABTestStatusRunning, StartedAt: &now,
	}
	if err := db.Create(&abTest).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 4; i++ {
		outcome := model.FeedbackSignalOutcomePending
		if i < 2 {
			outcome = model.FeedbackSignalOutcomeSuccess
		}
		_ = db.Create(&model.FeedbackSignal{
			SessionID: fmt.Sprintf("sess-conv-a-%d", i), CustomerID: "c", SOPID: sopID, Variant: "A",
			Outcome: outcome, CreatedAt: now, UpdatedAt: now,
		}).Error
	}
	for i := 0; i < 5; i++ {
		outcome := model.FeedbackSignalOutcomePending
		if i < 1 {
			outcome = model.FeedbackSignalOutcomeSuccess
		}
		_ = db.Create(&model.FeedbackSignal{
			SessionID: fmt.Sprintf("sess-conv-b-%d", i), CustomerID: "c", SOPID: sopID, Variant: "B",
			Outcome: outcome, CreatedAt: now, UpdatedAt: now,
		}).Error
	}

	o := &SOPAutoOptimizer{repo: repository.NewFeedbackLoopRepositoryWithDB(db)}
	control, experiment := o.fetchConversionRates(ctx, abTest.ID)
	if !approxEqualF64(control, 0.5) {
		t.Errorf("control rate = %v want 0.5", control)
	}
	if !approxEqualF64(experiment, 0.2) {
		t.Errorf("experiment rate = %v want 0.2", experiment)
	}
}

// TestSOPAutoOptimizer_FetchComplaintRates 拉取投诉率
func TestSOPAutoOptimizer_FetchComplaintRates(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	sopID := uint(300)
	now := time.Now()
	abTest := model.PromptABTest{
		ExperimentID: "test_fetch_complaint", ExperimentType: model.BanditExperimentTypeSOPVariant,
		SOPID: sopID, Name: "test",
		ArmKeys: model.JSONArray([]any{"a", "b"}),
		Config:  model.JSONMap{}, Status: model.PromptABTestStatusRunning, StartedAt: &now,
	}
	if err := db.Create(&abTest).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 10; i++ {
		key := "like"
		if i < 1 {
			key = model.FeedbackSignalComplaint
		}
		_ = db.Create(&model.FeedbackEvent{
			EventID:   fmt.Sprintf("evt-comp-a-%d-%d", i, now.UnixNano()),
			SessionID: fmt.Sprintf("sess-comp-a-%d", i), CustomerID: "c",
			SOPID: sopID, Variant: "A",
			EventType: "explicit", SignalKey: key,
			SignalValue: model.JSONMap{"v": true}, Weight: 1, Reward: 1,
			CreatedAt: now,
		}).Error
	}
	for i := 0; i < 10; i++ {
		key := "like"
		if i < 5 {
			key = model.FeedbackSignalComplaint
		}
		_ = db.Create(&model.FeedbackEvent{
			EventID:   fmt.Sprintf("evt-comp-b-%d-%d", i, now.UnixNano()),
			SessionID: fmt.Sprintf("sess-comp-b-%d", i), CustomerID: "c",
			SOPID: sopID, Variant: "B",
			EventType: "explicit", SignalKey: key,
			SignalValue: model.JSONMap{"v": true}, Weight: 1, Reward: 1,
			CreatedAt: now,
		}).Error
	}

	o := &SOPAutoOptimizer{repo: repository.NewFeedbackLoopRepositoryWithDB(db)}
	control, experiment := o.fetchComplaintRates(ctx, abTest.ID)
	if !approxEqualF64(control, 0.1) {
		t.Errorf("control complaint rate = %v want 0.1", control)
	}
	if !approxEqualF64(experiment, 0.5) {
		t.Errorf("experiment complaint rate = %v want 0.5", experiment)
	}
}

// TestSOPAutoOptimizer_RollbackTest 回滚 A/B 测试
func TestSOPAutoOptimizer_RollbackTest(t *testing.T) {
	db := setupFeedbackLoopTestDB(t)
	ctx := context.Background()

	expID := "test_rollback_explicit"
	now := time.Now()
	abTest := model.PromptABTest{
		ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant,
		SOPID: 1, Name: "test", ArmKeys: model.JSONArray([]any{"a"}),
		Config: model.JSONMap{}, Status: model.PromptABTestStatusRunning, StartedAt: &now,
	}
	if err := db.Create(&abTest).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	arms := []model.BanditArm{
		{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "a", Status: model.BanditArmStatusExploring},
		{ExperimentID: expID, ExperimentType: model.BanditExperimentTypeSOPVariant, ArmKey: "b", Status: model.BanditArmStatusExploring},
	}
	if err := db.Create(&arms).Error; err != nil {
		t.Fatalf("seed arms: %v", err)
	}

	o := &SOPAutoOptimizer{repo: repository.NewFeedbackLoopRepositoryWithDB(db)}
	if err := o.rollbackTest(ctx, abTest.ID, "test_reason"); err != nil {
		t.Fatalf("rollbackTest: %v", err)
	}

	var updated model.PromptABTest
	_ = db.First(&updated, abTest.ID).Error
	if updated.Status != model.PromptABTestStatusRolledBack {
		t.Errorf("abTest status = %q want rolled_back", updated.Status)
	}
	if updated.EndedAt == nil {
		t.Errorf("EndedAt 应非空")
	}

	var retiredCount int64
	db.Model(&model.BanditArm{}).Where("experiment_id = ? AND status = ?", expID, model.BanditArmStatusRetired).Count(&retiredCount)
	if retiredCount != 2 {
		t.Errorf("retired arms = %d want 2", retiredCount)
	}
}
