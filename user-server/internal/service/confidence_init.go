package service

import (
	"context"
	"sync"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/embedding"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	confidencesvc "hivemtk-user/internal/service/confidence"
)

func init() {
	confidencesvc.SetExplicitKeywordMatcher(func(content string) bool {
		return MatchTransferKeywords(content) || MatchExplicitKeywords(content)
	})
}

var (
	confidenceAggregatorOnce sync.Once
	confidenceAggregator     *confidencesvc.ConfidenceAggregator
)

// localEmbedder 把无依赖的本地投影 Embedding 适配为 confidence.Embedder 接口。
type localEmbedder struct {
	inner *embedding.LocalEmbedding
}

func (l *localEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return l.inner.Embed(text), nil
}

// NewLocalConfidenceEmbedder 构造本地零依赖 Embedding（1024 维随机投影）。
//
// 供装配层显式注入 InitConfidenceAggregator，使 CtxRelev 信号可计算；
// 与 InitConfidenceAggregator 内部 nil 回退等价，区别是不产生降级告警。
func NewLocalConfidenceEmbedder() confidencesvc.Embedder {
	return &localEmbedder{inner: embedding.NewLocalEmbedding(1024, 42)}
}

// InitConfidenceAggregator 初始化置信度聚合器（启动时调用一次）
//
// 依赖：
//   - db:       GORM DB
//   - embedder: 文本向量化器（用于上下文相关性 CtxRelev）；
//     传 nil 时回退本地零依赖投影 Embedding，避免 CtxRelev 恒为中性值 0.5
//
// 返回全局单例，可多次调用但仅第一次生效
func InitConfidenceAggregator(db *gorm.DB, embedder confidencesvc.Embedder) *confidencesvc.ConfidenceAggregator {
	confidenceAggregatorOnce.Do(func() {
		confidenceAggregator = buildConfidenceAggregator(db, embedder)
	})
	return confidenceAggregator
}

func buildConfidenceAggregator(db *gorm.DB, embedder confidencesvc.Embedder) *confidencesvc.ConfidenceAggregator {
	if embedder == nil {
		embedder = NewLocalConfidenceEmbedder()
		logger.Warnf("[confidence] embedder 未注入，回退本地零依赖 Embedding 引擎（1024 维随机投影）")
	}
	collector := confidencesvc.NewSignalCollector(embedder)

	calibrator := confidencesvc.NewCalibrator(repository.NewConfidenceCalibrationRepository())

	aggregator := confidencesvc.NewDefaultWeightedAggregator()

	vetoChain := confidencesvc.NewVetoChain()

	policyEngine := confidencesvc.NewThresholdPolicyEngine(repository.NewThresholdPolicyRepository())
	calc := confidencesvc.NewDynamicThresholdCalculator(policyEngine)

	signalRepo := repository.NewConfidenceSignalRepository()

	agg := confidencesvc.NewConfidenceAggregator(collector, calibrator, aggregator, vetoChain, calc, signalRepo)

	confCalib := confidencesvc.NewConformalCalibrator(1000, 60)
	agg.SetConformal(confCalib.Predictor())

	return agg
}

// GetConfidenceAggregator 获取全局聚合器（可能为 nil，未初始化时）
func GetConfidenceAggregator() *confidencesvc.ConfidenceAggregator {
	return confidenceAggregator
}
