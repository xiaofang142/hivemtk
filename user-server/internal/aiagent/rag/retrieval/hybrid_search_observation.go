package ragretrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

type retrievalObservationWriter interface {
	CreateObservation(context.Context, *model.KnowledgeSearchLog) error
}

type retrievalPhase struct {
	started, completed time.Time
	failed             bool
}

func (p *retrievalPhase) elapsed() *int64 {
	if p == nil {
		return nil
	}
	duration := p.completed.Sub(p.started).Milliseconds()
	return &duration
}

type retrievalOperation struct {
	started, completed                              time.Time
	embedding, vector, keyword, rrf, rerank, parent *retrievalPhase
}

// RetrievalObservationStats 显示观测覆盖损失，不把日志队列当业务可靠队列。
type RetrievalObservationStats struct {
	Persisted, Failed, Dropped, InFlight uint64
}

type retrievalObservationCounters struct {
	persisted, failed, dropped, inFlight atomic.Uint64
}

func (h *HybridSearcher) ObservationStats() RetrievalObservationStats {
	return RetrievalObservationStats{
		Persisted: h.observationCounts.persisted.Load(), Failed: h.observationCounts.failed.Load(),
		Dropped: h.observationCounts.dropped.Load(), InFlight: h.observationCounts.inFlight.Load(),
	}
}

func retrievalObservationContext(ctx context.Context) (context.Context, bool) {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, false
	}
	id := tenant.WriteTenantID(ctx)
	if id == 0 {
		return nil, false
	}
	c := tracing.CarrierFromContext(ctx).Child()
	if c.TenantID != 0 && c.TenantID != id {
		return nil, false
	}
	c.TenantID = id
	// 缺少会话边界时不借用父载体的轮次；知识查询本身仍可按租户记录。
	if c.Channel == "" || c.AccountID == "" || c.ConversationID == "" {
		c.TurnID = ""
		c.AnswerAttempt = 0
	}
	return tracing.WithCarrier(tenant.OnlyTenant(context.Background(), id), c), true
}

func (h *HybridSearcher) observeSearch(ctx context.Context, query, productID string, topK, vectorCount, keywordCount, fusedCount, rerankCount int, operation retrievalOperation) {
	observedCtx, ok := retrievalObservationContext(ctx)
	if !ok {
		return
	}
	c := tracing.CarrierFromContext(observedCtx)
	hash := sha256.Sum256([]byte(query))
	row := &model.KnowledgeSearchLog{
		TenantID: c.TenantID, ProductID: productID, QueryHash: hex.EncodeToString(hash[:]), QueryChars: utf8.RuneCountInString(query),
		TraceID: c.TraceID, TurnID: c.TurnID, Channel: c.Channel, AccountID: c.AccountID, ConversationID: c.ConversationID, AnswerAttempt: c.AnswerAttempt,
		TimingVersion: tracing.TimingVersion, DurationMeasured: true, StartedAt: &operation.started, CompletedAt: &operation.completed,
		LatencyMs: int(operation.completed.Sub(operation.started).Milliseconds()), TopK: topK, ResultCount: fusedCount, Source: "hybrid",
		EmbeddingLatencyMs: operation.embedding.elapsed(), VectorLatencyMs: operation.vector.elapsed(), BM25LatencyMs: operation.keyword.elapsed(),
		RRFLatencyMs: operation.rrf.elapsed(), RerankLatencyMs: operation.rerank.elapsed(), ParentLatencyMs: operation.parent.elapsed(),
		VectorCount: vectorCount, BM25Count: keywordCount, FusedCount: fusedCount, RerankCount: rerankCount,
		EmbeddingFailed: phaseFailed(operation.embedding), VectorFailed: phaseFailed(operation.vector), BM25Failed: phaseFailed(operation.keyword), RerankFailed: phaseFailed(operation.rerank),
	}
	if fusedCount > 0 {
		row.Hit = 1
	}
	if h.observationWriter == nil && c.TraceID == "" {
		return
	}
	h.observationOnce.Do(func() { h.observationSlots = make(chan struct{}, 4) })
	select {
	case h.observationSlots <- struct{}{}:
	default:
		h.observationCounts.dropped.Add(1)
		logger.Ctx(observedCtx).Warn().Msg("retrieval observation capacity exhausted; coverage reduced")
		return
	}
	h.observationCounts.inFlight.Add(1)
	utils.SafeGo(observedCtx, "retrieval.observe", func(parent context.Context) {
		defer func() {
			<-h.observationSlots
			h.observationCounts.inFlight.Add(^uint64(0))
			if recover() != nil {
				h.observationCounts.failed.Add(1)
				logger.Ctx(parent).Warn().Msg("retrieval observation write panicked; coverage reduced")
			}
		}()
		writeCtx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		// 包括未初始化追踪sink时的兼容直写，也不能阻塞检索调用方。
		if err := recordRetrievalPhases(writeCtx, row, operation); err != nil {
			h.observationCounts.failed.Add(1)
			logger.Ctx(parent).Warn().Msg("retrieval phase observation failed; coverage reduced")
			return
		}
		if h.observationWriter == nil {
			return
		}
		if err := h.observationWriter.CreateObservation(writeCtx, row); err != nil {
			h.observationCounts.failed.Add(1)
			logger.Ctx(parent).Warn().Msg("retrieval observation write failed; coverage reduced")
			return
		}
		h.observationCounts.persisted.Add(1)
	})
}

func recordRetrievalPhases(ctx context.Context, row *model.KnowledgeSearchLog, operation retrievalOperation) error {
	if row.TraceID == "" || row.Channel == "" || row.AccountID == "" || row.ConversationID == "" {
		return nil
	}
	for _, step := range []struct {
		name  string
		phase *retrievalPhase
	}{
		{name: "embedding", phase: operation.embedding}, {name: "vector", phase: operation.vector},
		{name: "keyword", phase: operation.keyword}, {name: "rrf", phase: operation.rrf},
		{name: "rerank", phase: operation.rerank}, {name: "parent", phase: operation.parent},
	} {
		if step.phase == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := tracing.StatusOk
		if step.phase.failed {
			status = tracing.StatusAbnormal
		}
		if err := tracing.RecordNodeContext(ctx, tracing.NodeSpan{
			Node: "retrieval_" + step.name, NodeOrder: 2,
			StartedAt: &step.phase.started, CompletedAt: &step.phase.completed,
			DurationMeasured: true, DurationMs: *step.phase.elapsed(), Status: status,
			Input:  map[string]any{"query_hash": row.QueryHash, "product_id": row.ProductID},
			Output: map[string]any{"failed": step.phase.failed},
		}); err != nil {
			return err
		}
	}
	return nil
}

func phaseFailed(phase *retrievalPhase) bool { return phase != nil && phase.failed }
