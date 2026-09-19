// tool_audit_repo.go 工具调用审计的 DB 读侧仓储（T-P1-08 / W-6）
//
// 写侧在 tooluse.DBAuditLogger（装饰器链上，异步批量落库），本文件只负责读。
// 为什么要有读侧：只有写入的话，"持久化"就停留在 psql 里 —— 调试端点
// GET /api/agent/tools/audit 原本读的是进程内存（重启即空、且只最近 10000 条），
// 接了 DB 之后如果端点还只看内存，运维根本无从判断"这条审计到底落库了没有"。
package repository

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// ToolAuditRepository tool_call_audits 读侧收口
type ToolAuditRepository struct {
	db *gorm.DB
}

// NewToolAuditRepository 构造
func NewToolAuditRepository(db *gorm.DB) *ToolAuditRepository {
	return &ToolAuditRepository{db: db}
}

// Available 报告仓储是否持有可用 DB 句柄（供端点回显 wired 状态，不用于吞错）。
func (r *ToolAuditRepository) Available() bool { return r != nil && r.db != nil }

// Recent 按时间倒序取最近的审计行；toolName 非空时按工具过滤。
//
// limit 必须为正：GORM 的 Limit(0) 会原样翻成 PG 的 `LIMIT 0`（恒空结果集），
// 那样"查不到"就会被读成"没有审计"。上限由调用方夹，本方法只拒绝非正数。
func (r *ToolAuditRepository) Recent(ctx context.Context, toolName string, limit int) ([]model.ToolCallAudit, error) {
	if !r.Available() {
		return nil, fmt.Errorf("tool audit repository: db handle is nil")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("tool audit repository: limit must be positive, got %d", limit)
	}
	q := r.db.WithContext(ctx).Order("executed_at DESC, id DESC").Limit(limit)
	if toolName != "" {
		q = q.Where("tool_name = ?", toolName)
	}
	var rows []model.ToolCallAudit
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// CountAll 总行数。
//
// 存在的唯一理由：本表**刻意不做自动清理**。内存版是 10000 条环形缓冲，落库后变成
// 无上限增长（每次工具调用一行），保留期该多长属合规口径而非工程默认，故交运营决定，
// 但决定之前必须看得见规模 —— 没有这个数，"要不要清理"就无从讨论。
func (r *ToolAuditRepository) CountAll(ctx context.Context) (int64, error) {
	if !r.Available() {
		return 0, fmt.Errorf("tool audit repository: db handle is nil")
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.ToolCallAudit{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// ToolAuditCostRow 按工具聚合的成本/耗时口径（列名与 tooluse.CostStats 的 JSON 对齐，
// 端点两种数据源共用一套字段，消费方不必分支）。
type ToolAuditCostRow struct {
	ToolName        string `json:"tool_name"`
	TotalCalls      int64  `json:"total_calls"`
	SuccessCalls    int64  `json:"success_calls"`
	FailedCalls     int64  `json:"failed_calls"`
	TotalDurationMs int64  `json:"total_duration_ms"`
}

// SuccessRate 成功占比（0 调用返回 0，不返回 NaN —— NaN 进 JSON 会直接序列化失败）。
func (row ToolAuditCostRow) SuccessRate() float64 {
	if row.TotalCalls <= 0 {
		return 0
	}
	return float64(row.SuccessCalls) / float64(row.TotalCalls)
}

// AvgDurationMs 平均耗时。
func (row ToolAuditCostRow) AvgDurationMs() float64 {
	if row.TotalCalls <= 0 {
		return 0
	}
	return float64(row.TotalDurationMs) / float64(row.TotalCalls)
}

// CostAggregates 从持久化审计行重算各工具调用量与耗时。
//
// 计费侧不另建表：MemoryCostTracker 记的四样（次数/成功/失败/总耗时）是本表按
// tool_name 聚合的真子集，再存一份只会造出两个事实源（重启前后各说各话）。
func (r *ToolAuditRepository) CostAggregates(ctx context.Context) ([]ToolAuditCostRow, error) {
	if !r.Available() {
		return nil, fmt.Errorf("tool audit repository: db handle is nil")
	}
	var rows []ToolAuditCostRow
	err := r.db.WithContext(ctx).Model(&model.ToolCallAudit{}).
		Select(`tool_name,
			COUNT(*) AS total_calls,
			COUNT(*) FILTER (WHERE success) AS success_calls,
			COUNT(*) FILTER (WHERE NOT success) AS failed_calls,
			COALESCE(SUM(duration_ms), 0) AS total_duration_ms`).
		Group("tool_name").
		Order("total_calls DESC, tool_name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
