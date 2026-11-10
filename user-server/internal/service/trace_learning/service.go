package trace_learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

var adjustMu sync.Mutex

func traceLockKey(traceID string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(traceID))
	return int64(h.Sum32())
}

// Service 追踪自学习服务：聚合 trace → LLM 打分 → 调整知识库权重 → 记录审计。
type Service struct {
	repo       *repository.TraceLearningRepository
	dispatcher *llm.Dispatcher
	cfg        Config
}

// New 构造服务；若评估场景无路由，则兜底复用 friendly_chat 路由，确保评估有 provider。
func New(repo *repository.TraceLearningRepository, dispatcher *llm.Dispatcher, cfg Config) *Service {
	if dispatcher != nil && dispatcher.GetRoute(cfg.Scenario) == nil {
		if r := dispatcher.GetRoute(llm.ScenarioFriendlyChat); r != nil {
			dispatcher.SetRoute(llm.ScenarioRoute{
				Scenario:   cfg.Scenario,
				Provider:   r.Provider,
				Fallbacks:  r.Fallbacks,
				MaxLatency: r.MaxLatency,
			})
		}
	}
	if repo == nil {
		// AV-04c 批次6：兜底装配改 Default 构造（句柄解析下沉仓储层，同连接同时刻）。
		repo = repository.NewTraceLearningRepositoryDefault()
	}
	return &Service{repo: repo, dispatcher: dispatcher, cfg: cfg}
}

func (s *Service) evaluateTraceOn(ctx context.Context, traceID string, dryRun bool) (*model.TraceEvalLog, error) {
	ctx = ensureCtx(ctx)
	agg, err := AggregateTrace(ctx, s.repo, traceID)
	if err != nil {
		return nil, err
	}
	if agg == nil {
		return nil, fmt.Errorf("trace 不存在: %s", traceID)
	}
	res, err := Evaluate(ctx, s.dispatcher, s.cfg, agg)
	skipAdjust := false
	if err != nil {
		if errors.Is(err, ErrNoEvaluableContent) {
			if dryRun {
				return nil, nil
			}
			skipAdjust = true
			res = &EvalResult{
				Score:      0,
				Bad:        false,
				Reason:     "跳过：" + err.Error(),
				Dimensions: map[string]float64{},
			}
		} else {
			if !dryRun {
				if werr := s.persistAttemptedLog(ctx, traceID, agg, "评估出错: "+err.Error()); werr != nil {
					logger.Warnf("[trace_learning] 写错误尝试审计失败 trace=%s: %v", traceID, werr)
				}
			}
			return nil, err
		}
	}
	dimJSON, _ := json.Marshal(res.Dimensions)

	if dryRun {

		var adjusted []AdjustedChunk
		if len(agg.RecalledChunkIDs) > 0 {
			adjusted, _ = PreviewAdjustments(ctx, s.repo, agg.RecalledChunkIDs, *res, s.cfg)
		}
		adjJSON, _ := json.Marshal(adjusted)
		return &model.TraceEvalLog{
			TraceID:        traceID,
			ConversationID: agg.ConversationID,
			Channel:        agg.Channel,
			Score:          res.Score,
			DimensionsJSON: string(dimJSON),
			Reason:         res.Reason,
			Bad:            res.Bad,
			AdjustedChunks: string(adjJSON),
		}, nil
	}

	safety, safetyPresent := res.Dimensions["safety"]
	action := weightAction(res.Score, res.Bad, safety, safetyPresent, s.cfg)
	adjustMu.Lock()
	resultLog, _, txErr := s.repo.UpsertEvalLogAtomic(ctx, traceID, traceLockKey(traceID), model.TraceEvalLog{
		TraceID:        traceID,
		ConversationID: agg.ConversationID,
		Channel:        agg.Channel,
		Score:          res.Score,
		DimensionsJSON: string(dimJSON),
		Reason:         res.Reason,
		Bad:            res.Bad,
	}, agg.RecalledChunkIDs, skipAdjust, weightFnFor(action, s.cfg))
	adjustMu.Unlock()
	if txErr != nil {
		return nil, txErr
	}

	s.distillInsightForTrace(ctx, agg, res)
	return resultLog, nil
}

func (s *Service) persistAttemptedLog(ctx context.Context, traceID string, agg *AggregatedTrace, reason string) error {
	log := model.TraceEvalLog{
		TraceID:        traceID,
		ConversationID: agg.ConversationID,
		Channel:        agg.Channel,
		Score:          0,
		DimensionsJSON: "{}",
		Reason:         reason,
		Bad:            false,
		AdjustedChunks: "[]",
	}
	return s.repo.UpsertAttemptedEvalLog(ctx, &log)
}

const runBatchLockKey int64 = 9173001

func (s *Service) RunBatch(ctx context.Context, sinceHours, batchSize int, dryRun bool) (*BatchResult, error) {
	ctx = ensureCtx(ctx)
	// AV-04c 批次6：句柄判断改 DBReady() 就绪探针（nil 接收者安全，语义同原
	// `s.repo == nil || s.repo.GetDB() == nil`），错误文案保持「db nil」。
	if !s.repo.DBReady() {
		return nil, fmt.Errorf("db nil")
	}
	if batchSize <= 0 || batchSize > 500 {
		batchSize = s.cfg.BatchSize
	}
	conc := s.cfg.Concurrency
	if conc < 1 {
		conc = 4
	}

	result := &BatchResult{}
	var previewMu sync.Mutex
	var previewBuf []*model.TraceEvalLog

	// AV-03 钉住的合法 Connection 接缝（advisory lock）：回调体逐字保留，仅把
	// 「取句柄再开独占连接」换成仓储同名 Connection 接缝（WithContext→Connection
	// 链路等价，锁与锁内扫描仍同一条连接）。
	connErr := s.repo.Connection(ctx, func(conn *gorm.DB) error {
		// 会话固定：这条 conn 由 GORM Connection 扣住，加锁与 defer 解锁都在
		// 同一后端会话上 —— 这是 advisory lock 唯一正确的用法。
		held, e := dbutil.TryAdvisoryLockConn(ctx, conn, runBatchLockKey)
		if e != nil {
			return e
		}
		if !held {
			logger.Infof("[trace_learning] RunBatch 跳过：已有实例在运行")
			return nil
		}
		defer func() {
			_ = dbutil.ReleaseAdvisoryLock(ctx, conn, runBatchLockKey)
		}()

		stalled := 0
		for {
			traceIDs, err := repository.NewTraceLearningRepository(conn).ListPendingTraceIDs(ctx, sinceHours, batchSize)
			if err != nil {
				return err
			}
			if len(traceIDs) == 0 {
				break
			}
			sem := make(chan struct{}, conc)
			var wg sync.WaitGroup
			var processed int32
			for _, tid := range traceIDs {
				wg.Add(1)
				// tid 为 go 1.22+ 逐轮局部量，直接捕获与原传参等价；
				// SafeGo：批量评估跑 LLM 链，单条 panic 不能击穿进程
				utils.SafeGo(ctx, "trace_learning.evaluate_trace", func(_ context.Context) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					log, e2 := s.evaluateTraceOn(ctx, tid, dryRun)
					if e2 != nil {
						logger.Warnf("[trace_learning] 评估失败 trace=%s: %v", tid, e2)
						return
					}
					atomic.AddInt32(&processed, 1)
					if dryRun {
						previewMu.Lock()
						previewBuf = append(previewBuf, log)
						previewMu.Unlock()
					}
				})
			}
			wg.Wait()
			result.Processed += int(atomic.LoadInt32(&processed))
			if dryRun {
				previewMu.Lock()
				result.Previews = append(result.Previews, previewBuf...)
				previewBuf = previewBuf[:0]
				previewMu.Unlock()
			}
			if processed == 0 && len(traceIDs) >= batchSize {
				stalled++
				if stalled >= 2 {
					logger.Warnf("[trace_learning] RunBatch 连续 %d 轮满批无成功评估，提前退出避免死循环", stalled)
					break
				}
			} else {
				stalled = 0
			}
			if len(traceIDs) < batchSize {
				break
			}
		}
		return nil
	})
	if connErr != nil {
		return result, connErr
	}
	return result, nil
}

// BatchResult RunBatch 处理结果
type BatchResult struct {
	Processed int
	Previews  []*model.TraceEvalLog
}

// Logs 查询最近打分记录（供前端展示）。
func (s *Service) Logs(ctx context.Context, limit int) ([]model.TraceEvalLog, error) {
	ctx = ensureCtx(ctx)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.ListRecentEvalLogs(ctx, limit)
}

// TopWeights 知识库权重排行（取权重偏离 1.0 最大的 chunk，供前端展示「自学习影响」）。
func (s *Service) TopWeights(ctx context.Context, limit int) ([]map[string]any, error) {
	ctx = ensureCtx(ctx)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListTopWeightDeviations(ctx, limit)
}

var _ = marshalJSON
