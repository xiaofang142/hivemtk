package humanize

import (
	"context"
	"errors"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// HumanizeEvaluator 单次评估器接口（规则与 LLM 共同实现）
type HumanizeEvaluator interface {
	Evaluate(ctx context.Context, input *dto.HumanizeEvalInput) (*dto.HumanizeEvalResult, error)
}

// HumanizeRegenerateFn 重生成回调
//
// 调用方提供：根据上次评估结果重新生成回复
type HumanizeRegenerateFn func(ctx context.Context, input *dto.HumanizeEvalInput, feedback *dto.HumanizeEvalResult) (string, error)

// HumanizeScoreRepository 评分持久化仓储接口
//
// service/humanize 包只依赖 Save 方法；查询方法在 repository 包中独立提供，
// 避免 service 层反向依赖 model 类型
type HumanizeScoreRepository interface {
	Save(ctx context.Context, score *model.HumanizeScore, dimensions []model.HumanizeDimensionRecord) error
}

// ChampionBaselineRepository 销冠基线仓储接口
type ChampionBaselineRepository interface {
	FindByPersonaIndustryIntent(ctx context.Context, persona, industry, intent string) (*model.ChampionBaseline, error)
	ListEnabled(ctx context.Context) ([]model.ChampionBaseline, error)
}

// LowQualitySampleCollector 低质样本收集器接口
//
// 实现由 repository.NewHumanizeLowQualitySampleCollector 提供（humanize_init.go 装配），
// HumanizeEvalService 通过此抽象接口调用
type LowQualitySampleCollector interface {
	Collect(ctx context.Context, sample *model.LowQualitySample) error
}

// LLMDispatcher LLM 调度器接口（抽象 llm.Dispatcher，便于测试）
type LLMDispatcher interface {
	ChatSend(ctx context.Context, prompt string) (content, model string, err error)
}

// LLMCallContext 是一次拟人 LLM 调用的隔离标识。
// ExecutionScenarioID 区分评估与重生，Step 参与幂等键，ContextView 携带
// 可见性与事实分级后的会话上下文。
type LLMCallContext struct {
	ExecutionScenarioID agentcontext.ExecutionScenarioID
	Step                string
	ContextView         *agentcontext.AgentConversationContext
}

// LLMContextDispatcher 是支持会话上下文与独立执行场景的可选能力。
// 老测试/老装配只实现 LLMDispatcher 时仍走原路径。
type LLMContextDispatcher interface {
	ChatSendWithContext(ctx context.Context, prompt string, call LLMCallContext) (content, model string, err error)
}

// ErrInvalidInput 输入非法
var ErrInvalidInput = errors.New("humanize: invalid input")

// ErrAggregatorNotInitialized 聚合器未初始化
var ErrAggregatorNotInitialized = errors.New("humanize: aggregator not initialized")

func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}

func computeHumanizeWeightedScore(scores []dto.HumanizeDimensionScore) float64 {
	if len(scores) == 0 {
		return 0
	}
	m := make(map[dto.HumanizeDimension]float64, len(scores))
	for _, s := range scores {
		m[s.Dimension] = s.Score
	}
	total := 0.0
	for _, dim := range dto.AllHumanizeDimensions {
		total += dto.HumanizeDimensionWeight[dim] * m[dim]
	}
	return total
}
