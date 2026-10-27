// trace_learning_repo.go 追踪自学习仓储（评估日志/批次候选/权重排行）
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"

	"gorm.io/gorm"
)

// TraceLearningRepository trace_learning 域数据访问收口
type TraceLearningRepository struct {
	db *gorm.DB
}

// NewTraceLearningRepository 构造
func NewTraceLearningRepository(db *gorm.DB) *TraceLearningRepository {
	return &TraceLearningRepository{db: db}
}

// GetDB 暴露底层连接（仅供 trace_learning 聚合/权重调整等只读协作方复用同一连接/事务）
func (r *TraceLearningRepository) GetDB() *gorm.DB {
	if r == nil {
		return nil
	}
	return r.db
}

// DBReady 就绪探针：句柄是否已装配（不暴露 *gorm.DB 给 service）。
//
// 调用方只想知道"能不能读库"，与 repository.DBReady() 同一语义，区别是这里
// 看的是本仓储绑定的那份连接（测试可注入独立库，不被全局句柄状态误导）。
func (r *TraceLearningRepository) DBReady() bool {
	return r != nil && r.db != nil
}

// UpsertAttemptedEvalLog 幂等写评估审计（trace_id 命中即跳过）
func (r *TraceLearningRepository) UpsertAttemptedEvalLog(ctx context.Context, log *model.TraceEvalLog) error {
	return r.db.WithContext(ctx).Where("trace_id = ?", log.TraceID).Assign(*log).FirstOrCreate(log).Error
}

// ListPendingTraceIDs 批次候选：窗口内 AI dispatch 节点带 reply 的未评估 trace（id 升序）
func (r *TraceLearningRepository) ListPendingTraceIDs(ctx context.Context, sinceHours, limit int) ([]string, error) {
	sub := r.db.WithContext(ctx).Table("message_trace").
		Select("trace_id").
		Where("node = ?", tracing.NodeAIDispatch).
		Where("output::text LIKE ?", `%"`+`reply`+`"%`)
	if sinceHours > 0 {
		sub = sub.Where("created_at >= now() - make_interval(hours => ?)", sinceHours)
	}
	var traceIDs []string
	err := r.db.WithContext(ctx).
		Table("message_trace").
		Select("trace_id").
		Where("trace_id IN (?)", sub).
		Where("trace_id NOT IN (SELECT trace_id FROM trace_eval_log)").
		Group("trace_id").
		Order("MAX(id) ASC").
		Limit(limit).
		Pluck("trace_id", &traceIDs).Error
	return traceIDs, err
}

// SaveInsight 幂等写一条学习洞察（source_trace_id 命中即覆盖文本，不重复插行）
func (r *TraceLearningRepository) SaveInsight(ctx context.Context, industry, text, sourceTraceID string) error {
	row := model.LearningInsight{
		Industry:      industry,
		InsightText:   text,
		SourceTraceID: sourceTraceID,
	}
	return r.db.WithContext(ctx).
		Where("source_trace_id = ?", sourceTraceID).
		Assign(row).
		FirstOrCreate(&row).Error
}

// ListRecentEvalLogs 最近评估记录（created_at 倒序）
func (r *TraceLearningRepository) ListRecentEvalLogs(ctx context.Context, limit int) ([]model.TraceEvalLog, error) {
	var logs []model.TraceEvalLog
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// ListTopWeightDeviations 权重偏离 1.0 最大的知识 chunk
func (r *TraceLearningRepository) ListTopWeightDeviations(ctx context.Context, limit int) ([]map[string]any, error) {
	var rows []map[string]any
	err := r.db.WithContext(ctx).
		Table("knowledge_chunks").
		Select("id, content, weight, hit_count").
		Where("weight <> 1").
		Order("abs(weight - 1) DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// ---- 下沉自 service/trace_learning（2026-10-25 W-2 续：五层收口）----
// 原则：service 侧不再出现 *gorm.DB 类型字面量；SQL 与事务体在本包，
// 业务纯计算（权重曲线）由调用方以 TLWeightFn 注入。

// TLWeightChange 单条 chunk 权重调整结果。
type TLWeightChange struct {
	ID  uint64
	Old float64
	New float64
}

// TLWeightFn 纯计算回调：旧权重 → (新权重, 是否有变化)。由 service 注入权重曲线。
type TLWeightFn func(oldW float64) (newW float64, changed bool)

// UpdateChunkWeightsWithinTx 在**给定事务**上逐行调整 knowledge_chunks.weight
// （trace_learning 评估事务内调权用：与评估日志同事务，原子）。
func (r *TraceLearningRepository) UpdateChunkWeightsWithinTx(ctx context.Context, tx *gorm.DB, ids []uint64, compute TLWeightFn) ([]TLWeightChange, error) {
	adjusted := make([]TLWeightChange, 0, len(ids))
	for _, id := range ids {
		var oldW float64
		if e := tx.WithContext(ctx).Table("knowledge_chunks").Select("weight").Where("id = ?", id).Scan(&oldW).Error; e != nil {
			return nil, e
		}
		if oldW <= 0 {
			oldW = 1.0
		}
		newW, changed := compute(oldW)
		if !changed {
			continue
		}
		if e := tx.WithContext(ctx).Table("knowledge_chunks").Where("id = ?", id).Update("weight", newW).Error; e != nil {
			return nil, e
		}
		adjusted = append(adjusted, TLWeightChange{ID: id, Old: oldW, New: newW})
	}
	return adjusted, nil
}

// AdjustChunkWeights 自带事务版权重调整（独立调用场景；评估事务内请用 UpdateChunkWeightsWithinTx）。
func (r *TraceLearningRepository) AdjustChunkWeights(ctx context.Context, ids []uint64, compute TLWeightFn) ([]TLWeightChange, error) {
	db := r.GetDB()
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var out []TLWeightChange
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		changes, e := r.UpdateChunkWeightsWithinTx(ctx, tx, ids, compute)
		if e != nil {
			return e
		}
		out = changes
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PreviewChunkWeights 只读预览：按 compute 计算「将如何调权」，不写库。
func (r *TraceLearningRepository) PreviewChunkWeights(ctx context.Context, ids []uint64, compute TLWeightFn) ([]TLWeightChange, error) {
	db := r.GetDB()
	if db == nil {
		return nil, nil
	}
	type wrow struct {
		ID     uint64
		Weight float64
	}
	var rows []wrow
	if err := db.WithContext(ctx).Table("knowledge_chunks").Select("id, weight").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TLWeightChange, 0, len(rows))
	for _, row := range rows {
		oldW := row.Weight
		if oldW <= 0 {
			oldW = 1.0
		}
		newW, changed := compute(oldW)
		if !changed {
			continue
		}
		out = append(out, TLWeightChange{ID: row.ID, Old: oldW, New: newW})
	}
	return out, nil
}

// ListTraceSpans 按 node_order 取一条 trace 的全部 span。
func (r *TraceLearningRepository) ListTraceSpans(ctx context.Context, traceID string) ([]model.MessageTrace, error) {
	db := r.GetDB()
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var spans []model.MessageTrace
	if err := db.WithContext(ctx).Where("trace_id = ?", traceID).Order("node_order, id").Find(&spans).Error; err != nil {
		return nil, err
	}
	return spans, nil
}

// RecentHubContent message_hub 兜底：按会话取最近一条指定方向的 content。
func (r *TraceLearningRepository) RecentHubContent(ctx context.Context, conversationID, direction string) (string, error) {
	db := r.GetDB()
	if db == nil {
		return "", gorm.ErrInvalidDB
	}
	var content string
	err := db.WithContext(ctx).Table("message_hub").
		Where("conversation_id = ? AND direction = ?", conversationID, direction).
		Order("id DESC").Limit(1).Pluck("content", &content).Error
	return content, err
}

// UpsertEvalLogAtomic 评估结果落库的事务体（原 service 侧整体下沉）：
// 事务内 advisory lock 防同 trace 并发重评 → 查重 →（可选）事务内调权 → upsert 评估日志。
// 调权与日志同事务：AdjustedChunks 与权重写入要么同成要么同败。
// 返回 (落库后的日志, 实际调整列表)。alreadyEvaluated（已有日志）时 changes 为空。
func (r *TraceLearningRepository) UpsertEvalLogAtomic(
	ctx context.Context,
	traceID string,
	lockKey int64,
	log model.TraceEvalLog,
	recallIDs []string,
	skipAdjust bool,
	compute TLWeightFn,
) (*model.TraceEvalLog, []TLWeightChange, error) {
	db := r.GetDB()
	if db == nil {
		return nil, nil, gorm.ErrInvalidDB
	}
	var resultLog *model.TraceEvalLog
	var changes []TLWeightChange
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockHeld bool
		if e := tx.Select("pg_advisory_xact_lock(?)", lockKey).Scan(&lockHeld).Error; e != nil {
			return e
		}
		var existing model.TraceEvalLog
		if e := tx.Where("trace_id = ?", traceID).First(&existing).Error; e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		alreadyEvaluated := existing.ID != 0

		if len(recallIDs) > 0 && !alreadyEvaluated && !skipAdjust {
			// 原实现语义：调权失败只记日志不阻断评估日志落库（AdjustedChunks 置空）。
			changes, _ = r.UpdateChunkWeightsWithinTx(ctx, tx, parseUint64IDs(recallIDs), compute)
		}
		adjJSON, _ := json.Marshal(changes)
		log.AdjustedChunks = string(adjJSON)
		if e := tx.Where("trace_id = ?", traceID).Assign(log).FirstOrCreate(&log).Error; e != nil {
			return e
		}
		resultLog = &log
		return nil
	})
	if txErr != nil {
		return nil, nil, txErr
	}
	return resultLog, changes, nil
}

// parseUint64IDs 容错解析字符串 ID 列表（与 service 侧 dedupeParseIDs 同规则：非法跳过）。
func parseUint64IDs(ids []string) []uint64 {
	out := make([]uint64, 0, len(ids))
	for _, s := range ids {
		if v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}
