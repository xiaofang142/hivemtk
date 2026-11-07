package feedbackloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"hivemtk-user/internal/repository"
)

type fakeGateLLM struct {
	content string
	err     error
	calls   int
}

func (f *fakeGateLLM) Dispatch(ctx context.Context, req llm.DispatchRequest) (*llm.DispatchResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &llm.DispatchResult{Content: f.content}, nil
}

func setupGateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.OptimizationSuggestion{},
		&model.TraceEvalLog{},
		&model.MessageTrace{},
		&model.SOPAgent{},
		&model.PromptABTest{},
		&model.BanditArm{},
	)
}

func seedGoldenCases(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		tid := fmt.Sprintf("golden-%02d", i)
		require.NoError(t, db.Create(&model.TraceEvalLog{
			TraceID: tid, Score: 95 - i, Bad: false, Reason: "回复准确",
		}).Error)
		require.NoError(t, db.Create(&model.MessageTrace{
			TraceID: tid, Node: "ingest", NodeOrder: 1,
			Input: `{"content":"这个产品多少钱"}`,
		}).Error)
		out, _ := json.Marshal(map[string]any{"reply": "您好，具体报价会按需定制", "recalled_chunk_ids": []string{}})
		require.NoError(t, db.Create(&model.MessageTrace{
			TraceID: tid, Node: "ai_dispatch", NodeOrder: 2,
			Output: string(out),
		}).Error)
	}
}

func seedPendingSuggestion(t *testing.T, db *gorm.DB) model.OptimizationSuggestion {
	t.Helper()
	sop := model.SOPAgent{Name: "销售SOP", Scenario: "sales", SOPGraph: model.JSONMap{"nodes": []any{}}}
	require.NoError(t, db.Create(&sop).Error)
	sug := model.OptimizationSuggestion{
		SOPID:          sop.ID,
		SuggestionType: model.SuggestionTypeTimingAdjust,
		SuggestionText: "将 wait 节点时长从 24h 调整为 12h",
		Priority:       2,
		Status:         model.SuggestionStatusPending,
	}
	require.NoError(t, db.Create(&sug).Error)
	return sug
}

func reloadSuggestion(t *testing.T, db *gorm.DB, id uint) model.OptimizationSuggestion {
	t.Helper()
	var sug model.OptimizationSuggestion
	require.NoError(t, db.First(&sug, id).Error)
	return sug
}

func gateVerdict(pass bool, reason string) string {
	b, _ := json.Marshal(map[string]any{"pass": pass, "reason": reason})
	return string(b)
}

// TestGate_InsufficientGolden_FailClosed golden 不足 → fail-closed 不自动应用，保持 pending 可人工审
func TestGate_InsufficientGolden_FailClosed(t *testing.T) {
	db := setupGateTestDB(t)
	seedGoldenCases(t, db, minGoldenCases-1)

	fake := &fakeGateLLM{content: gateVerdict(true, "ok")}
	o := NewSOPAutoOptimizer(repository.NewFeedbackLoopRepositoryWithDB(db), nil, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(fake)

	sug := seedPendingSuggestion(t, db)
	report, err := o.ProcessPendingSuggestions(context.Background())
	require.NoError(t, err)
	assert.Zero(t, report.AppliedCount)

	reloaded := reloadSuggestion(t, db, sug.ID)
	assert.Equal(t, model.SuggestionStatusPending, reloaded.Status, "未过门不应应用")
	assert.False(t, reloaded.EvidenceData["gate"] == nil, "门结果应写入审计字段")

	assert.Zero(t, fake.calls)
}

// TestGate_Pass_Applied 全门通过 → 自动应用（克隆 SOP + AB 测试）
func TestGate_Pass_Applied(t *testing.T) {
	db := setupGateTestDB(t)
	seedGoldenCases(t, db, minGoldenCases+2)

	fake := &fakeGateLLM{content: gateVerdict(true, "无损害")}
	o := NewSOPAutoOptimizer(repository.NewFeedbackLoopRepositoryWithDB(db), nil, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(fake)

	sug := seedPendingSuggestion(t, db)
	report, err := o.ProcessPendingSuggestions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, report.AppliedCount)

	reloaded := reloadSuggestion(t, db, sug.ID)
	assert.Equal(t, model.SuggestionStatusApplied, reloaded.Status)

	var abTests int64
	require.NoError(t, db.Model(&model.PromptABTest{}).Count(&abTests).Error)
	assert.Equal(t, int64(1), abTests, "通过后应创建 A/B 测试")
}

// TestGate_LLMJudgeReject_NotApplied LLM 评审未过 → 不应用
func TestGate_LLMJudgeReject_NotApplied(t *testing.T) {
	db := setupGateTestDB(t)
	seedGoldenCases(t, db, minGoldenCases+2)

	fake := &fakeGateLLM{content: gateVerdict(false, "存在过度承诺风险")}
	o := NewSOPAutoOptimizer(repository.NewFeedbackLoopRepositoryWithDB(db), nil, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(fake)

	sug := seedPendingSuggestion(t, db)
	report, err := o.ProcessPendingSuggestions(context.Background())
	require.NoError(t, err)
	assert.Zero(t, report.AppliedCount)

	reloaded := reloadSuggestion(t, db, sug.ID)
	assert.Equal(t, model.SuggestionStatusPending, reloaded.Status)
	assert.Equal(t, 1, fake.calls)
}

// TestGate_RecheckSkip 近期已检查且未过的建议跳过，不重复烧 LLM
func TestGate_RecheckSkip(t *testing.T) {
	db := setupGateTestDB(t)
	seedGoldenCases(t, db, minGoldenCases+2)

	fake := &fakeGateLLM{content: gateVerdict(false, "未过")}
	o := NewSOPAutoOptimizer(repository.NewFeedbackLoopRepositoryWithDB(db), nil, DefaultSOPAutoOptimizerConfig())
	o.SetGateLLM(fake)

	seedPendingSuggestion(t, db)
	_, err := o.ProcessPendingSuggestions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fake.calls)

	_, err = o.ProcessPendingSuggestions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fake.calls, "24h 内不应重复评审")
}

// TestGate_StructuralChecks 结构门与合规门
func TestGate_StructuralChecks(t *testing.T) {
	db := setupGateTestDB(t)
	o := NewSOPAutoOptimizer(repository.NewFeedbackLoopRepositoryWithDB(db), nil, DefaultSOPAutoOptimizerConfig())

	empty := &model.OptimizationSuggestion{SuggestionText: "  "}
	res := o.passGate(context.Background(), empty)
	assert.False(t, res.Passed)
	assert.Contains(t, res.Reason, "结构门")

	huge := &model.OptimizationSuggestion{SuggestionText: strings.Repeat("a", suggestionMaxTextLen+1)}
	res = o.passGate(context.Background(), huge)
	assert.False(t, res.Passed)
	assert.Contains(t, res.Reason, "超上限")

	banned := &model.OptimizationSuggestion{SuggestionText: "我们的产品是全网第一的选择"}
	res = o.passGate(context.Background(), banned)
	assert.False(t, res.Passed)
	assert.Contains(t, res.Reason, "合规门")
}

// TestGate_TraceLearningRepo_SharesInjectedDB AV-04c 批次4：gate 的 golden 聚合
// 改走 repository.NewTraceLearningRepositoryShared(flRepo)（原 GetDB() 直拼
// TraceLearning 仓储的写法已删，FeedbackLoopRepository.GetDB 方法一并移除）。
//
// 注入库构造的反馈闭环仓储，Shared 构造出的聚合仓储必须共享**同一条连接**：
// MessageTrace 行只落在注入库上，聚合仓储能读到即证明连接同源（而不是全局句柄
// ——SetTestDB 之前的库），单测注入语义与改前一致。
func TestGate_TraceLearningRepo_SharesInjectedDB(t *testing.T) {
	db := setupGateTestDB(t)
	out, _ := json.Marshal(map[string]any{"reply": "您好，具体报价会按需定制"})
	require.NoError(t, db.Create(&model.MessageTrace{
		TraceID: "trace-shared", Node: "ai_dispatch", NodeOrder: 2, Output: string(out),
	}).Error)

	flRepo := repository.NewFeedbackLoopRepositoryWithDB(db)
	tlRepo := repository.NewTraceLearningRepositoryShared(flRepo)
	require.NotNil(t, tlRepo, "非 nil 反馈仓储必须给出聚合仓储")

	spans, err := tlRepo.ListTraceSpans(context.Background(), "trace-shared")
	require.NoError(t, err)
	require.Len(t, spans, 1, "Shared 聚合仓储应读到注入库上的 message_trace 行")
	assert.Equal(t, "ai_dispatch", spans[0].Node)

	// nil 防御沿原 GetDB() 语义：nil 反馈仓储返回 nil，不 panic。
	assert.Nil(t, repository.NewTraceLearningRepositoryShared(nil))
}
