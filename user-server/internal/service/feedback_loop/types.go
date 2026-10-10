package feedbackloop

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/dto"
)

// LLMDispatcher LLM 调度器接口
//
// 抽象 llm.Dispatcher，使 feedback_loop 包不直接依赖 llm 包
// 测试时可注入 stub 实现
type LLMDispatcher interface {
	Dispatch(ctx context.Context, scenario string, prompt, systemPrompt string, jsonMode bool, maxTokens int) (content, model string, err error)
}

// Embedder 文本向量化接口
//
// 抽象 embedding.LocalEmbedding，使 feedback_loop 包不直接依赖 embedding 包
type Embedder interface {
	Embed(text string) []float32
	Dimension() int
}

// BanditAllocatorInterface Bandit 分配器接口（供 SOPAutoOptimizer 依赖）
//
// 实现方为 BanditAllocator，抽象接口便于测试 SOPAutoOptimizer 时注入 mock
type BanditAllocatorInterface interface {
	CheckConvergence(ctx context.Context, experimentID string) (string, bool)
	PromoteArm(ctx context.Context, experimentID, winnerKey string) error
}

// SignalWeightMap 信号权重映射（可由配置覆盖）
//
// 权重设计依据 docs/核心链路优化.md §17.2.1 信号权重设计表：
//
//	客户点赞 +1.0  客户点踩 -1.5  评分 ≥4 +0.8  评分 ≤2 -1.0  投诉 -2.0
//	转化成功 +2.0   回复率>0.7 +0.5  会话>5min +0.3  转人工 -0.5
//	销冠标记 +1.5   话术采用 +0.6
//	回款金额 log10 缩放（weight=0.5 作系数，1 万 ≈ +2.0 与转化同量级，封顶 +3.0）
//	丢单 flat -1.0
//
// 回款两键不进 reflux（无 arm 上下文）：入库供训练/离线消费，见 bandit_reward_reflux 四键过滤。
type SignalWeightMap map[dto.FeedbackSignalKey]float64

// DefaultSignalWeights 默认信号权重
var DefaultSignalWeights = SignalWeightMap{
	dto.FBSignalLike:         1.0,
	dto.FBSignalDislike:      -1.5,
	dto.FBSignalRating:       0.8,
	dto.FBSignalComplaint:    -2.0,
	dto.FBSignalConversion:   2.0,
	dto.FBSignalReplyRate:    0.5,
	dto.FBSignalDuration:     0.3,
	dto.FBSignalTransfer:     -0.5,
	dto.FBSignalChampionMark: 1.5,
	dto.FBSignalScriptAdopt:  0.6,

	dto.FBSignalToolCall:    0.3,
	dto.FBSignalIntentMatch: 0.5,

	// collection 的 weight 是 log10 缩放系数（见 computeReward collection 分支）；
	// collection_lost 走 computeReward default 分支，weight 即 flat reward。
	dto.FBSignalCollection:     0.5,
	dto.FBSignalCollectionLost: -1.0,
}

// BanditConfig Bandit 分配器配置
type BanditConfig struct {
	MinSamplesForExploit int
	ExplorationFloor     float64
	TrafficCeiling       float64
	ConvergenceThreshold float64
	MinSamplesForPromote int
	PosteriorSamples     int
}

// DefaultBanditConfig 默认 Bandit 配置
func DefaultBanditConfig() BanditConfig {
	return BanditConfig{
		MinSamplesForExploit: 30,
		ExplorationFloor:     0.10,
		TrafficCeiling:       0.60,
		ConvergenceThreshold: 0.95,
		MinSamplesForPromote: 100,
		PosteriorSamples:     1000,
	}
}

// FeedbackCollectorConfig 反馈采集器配置
type FeedbackCollectorConfig struct {
	QueueSize     int
	FlushInterval time.Duration
	BatchSize     int
	Weights       SignalWeightMap
}

// DefaultFeedbackCollectorConfig 默认采集器配置
func DefaultFeedbackCollectorConfig() FeedbackCollectorConfig {
	return FeedbackCollectorConfig{
		QueueSize:     1000,
		FlushInterval: 2 * time.Second,
		BatchSize:     50,
		Weights:       DefaultSignalWeights,
	}
}

// ChampionAnalyzerConfig 销冠对话分析器配置
type ChampionAnalyzerConfig struct {
	MinReward           float64
	ClusterSimThreshold float64
	MinClusterSize      int
	TopKPerCluster      int
	MaxDialoguesPerRun  int
}

// DefaultChampionAnalyzerConfig 默认分析器配置
func DefaultChampionAnalyzerConfig() ChampionAnalyzerConfig {
	return ChampionAnalyzerConfig{
		MinReward:           1.0,
		ClusterSimThreshold: 0.85,
		MinClusterSize:      3,
		TopKPerCluster:      3,
		MaxDialoguesPerRun:  500,
	}
}

// PromptIteratorConfig Prompt 迭代器配置
type PromptIteratorConfig struct {
	MinSamplesForIteration  int
	NegativeRewardThreshold float64
	CandidatesPerRun        int
	AutoApprove             bool
}

// DefaultPromptIteratorConfig 默认迭代器配置
func DefaultPromptIteratorConfig() PromptIteratorConfig {
	return PromptIteratorConfig{
		MinSamplesForIteration:  50,
		NegativeRewardThreshold: -0.5,
		CandidatesPerRun:        3,
		AutoApprove:             false,
	}
}

// SOPAutoOptimizerConfig SOP 自动优化器配置
type SOPAutoOptimizerConfig struct {
	AutoApplyPriority      int
	RollbackDropThreshold  float64
	RollbackComplaintRatio float64
	ABTestDuration         time.Duration
}

// DefaultSOPAutoOptimizerConfig 默认优化器配置
func DefaultSOPAutoOptimizerConfig() SOPAutoOptimizerConfig {
	return SOPAutoOptimizerConfig{
		AutoApplyPriority:      2,
		RollbackDropThreshold:  0.20,
		RollbackComplaintRatio: 0.50,
		ABTestDuration:         7 * 24 * time.Hour,
	}
}

var (
	ErrInvalidInput         = errors.New("feedback_loop: invalid input")
	ErrNoArms               = errors.New("feedback_loop: no arms for experiment")
	ErrInsufficientSamples  = errors.New("feedback_loop: insufficient samples for iteration")
	ErrActivePromptNotFound = errors.New("feedback_loop: no active prompt for sop node")
	ErrDispatcherNotConfig  = errors.New("feedback_loop: llm dispatcher not configured")
	ErrEmbedderNotConfig    = errors.New("feedback_loop: embedder not configured")
	ErrQueueFull            = errors.New("feedback_loop: feedback queue full, event dropped")
)
