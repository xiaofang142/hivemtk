package service

import (
	"context"
	"sync"

	"hivemtk-user/internal/aiagent/embedding"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/utils/logger"
	feedbackloop "hivemtk-user/internal/service/feedback_loop"

	"hivemtk-user/internal/repository"
)

var (
	feedbackCollectorOnce sync.Once
	feedbackCollector     *feedbackloop.FeedbackCollector
)

type embeddingAdapter struct {
	embedder *embedding.LocalEmbedding
}

func (a *embeddingAdapter) Embed(text string) []float32 {
	if a.embedder == nil {
		return make([]float32, 1024)
	}
	return a.embedder.Embed(text)
}

func (a *embeddingAdapter) Dimension() int {
	if a.embedder == nil {
		return 1024
	}
	return a.embedder.Dimension()
}

type feedbackLLMAdapter struct {
	dispatcher *llm.Dispatcher
}

func (a *feedbackLLMAdapter) Dispatch(ctx context.Context, scenario string, prompt, systemPrompt string, jsonMode bool, maxTokens int) (string, string, error) {
	if a.dispatcher == nil {
		return "", "", nil
	}
	result, err := a.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:     llm.DispatchScenario(scenario),
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		MaxTokens:    maxTokens,
	})
	if err != nil {
		return "", "", err
	}
	return result.Content, result.Model, nil
}

// InitFeedbackCollector 初始化反馈采集器（启动时调用一次）
//
// 启动后会异步消费事件队列（每 2s 刷盘），调用 Stop() 优雅关闭
// AV-04c 批次6：仓储改 Default 构造在仓储包内捕获全局句柄——连接、解析时刻与原
// `NewFeedbackLoopRepositoryWithDB(repository.GetDB())` 完全一致（全局无句柄时
// 同样得到 db=nil 的仓储，落库路径行为不变）。
func InitFeedbackCollector() *feedbackloop.FeedbackCollector {
	feedbackCollectorOnce.Do(func() {
		cfg := FeedbackCollectorConfigFromParams(context.Background())
		feedbackCollector = feedbackloop.NewFeedbackCollector(repository.NewFeedbackLoopRepositoryDefault(), cfg)
	})
	return feedbackCollector
}

// GetFeedbackCollector 获取全局采集器
func GetFeedbackCollector() *feedbackloop.FeedbackCollector {
	return feedbackCollector
}

func InitFeedbackLoopComponents(dispatcher *llm.Dispatcher, embedder *embedding.LocalEmbedding) *FeedbackLoopComponents {
	if embedder == nil {
		embedder = embedding.NewLocalEmbedding(1024, 42)
		logger.Warnf("[feedback_loop] embedder 未注入，回退本地零依赖 Embedding 引擎（1024 维随机投影）")
	}
	embAdapter := &embeddingAdapter{embedder: embedder}
	llmAdapter := &feedbackLLMAdapter{dispatcher: dispatcher}

	// 五个组件的配置全部走参数中心（feedback 组，22 条，构造期读，Restart=true）。
	// AV-04c 批次6：仓储改 Default 构造（同 InitFeedbackCollector，句柄解析下沉仓储层）。
	bg := context.Background()
	flrepo := repository.NewFeedbackLoopRepositoryDefault()
	collector := feedbackloop.NewFeedbackCollector(flrepo, FeedbackCollectorConfigFromParams(bg))
	analyzer := feedbackloop.NewChampionDialogueAnalyzer(flrepo, embAdapter, llmAdapter, ChampionAnalyzerConfigFromParams(bg))
	iterator := feedbackloop.NewPromptIterator(flrepo, llmAdapter, PromptIteratorConfigFromParams(bg))
	bandit := feedbackloop.NewBanditAllocator(flrepo, BanditConfigFromParams(bg), 42)
	optimizer := feedbackloop.NewSOPAutoOptimizer(flrepo, bandit, SOPAutoOptimizerConfigFromParams(bg))

	return &FeedbackLoopComponents{
		Collector:    collector,
		Analyzer:     analyzer,
		Iterator:     iterator,
		Bandit:       bandit,
		Optimizer:    optimizer,
		LLMAdapter:   llmAdapter,
		EmbedAdapter: embAdapter,
	}
}

// FeedbackLoopComponents 反馈学习闭环组件集合
type FeedbackLoopComponents struct {
	Collector    *feedbackloop.FeedbackCollector
	Analyzer     *feedbackloop.ChampionDialogueAnalyzer
	Iterator     *feedbackloop.PromptIterator
	Bandit       *feedbackloop.BanditAllocator
	Optimizer    *feedbackloop.SOPAutoOptimizer
	LLMAdapter   *feedbackLLMAdapter
	EmbedAdapter *embeddingAdapter
}
