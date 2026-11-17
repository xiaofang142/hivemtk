package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
)

// inbox_conversation_metrics.go 生命周期读侧聚合（specs/024 T605）。
//
// 只读：不改任何会话状态，也不落缓存——看板数字错一次的代价远高于多查一次。
// 无首响样本（first_reply_at 为空）必须排除：把它们当 0 秒会把 p50 拉到「秒回」，
// 而真实情况是一批客户根本没人回。

// FirstReplyMetrics 首响时长聚合结果（秒）。
type FirstReplyMetrics struct {
	// SampleCount 纳入统计的会话数（有等待起点且有首响）。
	SampleCount int64   `json:"sample_count"`
	P50Seconds  float64 `json:"p50_seconds"`
	P95Seconds  float64 `json:"p95_seconds"`
}

// FirstReplyMetrics 计算首响时长 p50/p95。
//
// platform 为空表示全部渠道；from/to 为零值表示不设时间窗（按 first_reply_at 过滤）。
func (r *InboxConversationRepository) FirstReplyMetrics(ctx context.Context, platform string, from, to time.Time) (*FirstReplyMetrics, error) {
	out := &FirstReplyMetrics{}
	if r == nil || r.db == nil {
		return out, nil
	}
	tx := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("waiting_since IS NOT NULL AND first_reply_at IS NOT NULL").
		// 时钟偏移/补录可能写出 first_reply_at < waiting_since，这种样本是脏数据，
		// 计入会得到负时长（拉低均值），直接排除。
		Where("first_reply_at >= waiting_since")
	if platform != "" {
		tx = tx.Where("platform = ?", platform)
	}
	if !from.IsZero() {
		tx = tx.Where("first_reply_at >= ?", from)
	}
	if !to.IsZero() {
		tx = tx.Where("first_reply_at < ?", to)
	}

	// percentile_cont 对空集返回 NULL，用 COALESCE 落成 0，避免扫描 NULL 报错。
	var row struct {
		SampleCount int64
		P50         float64
		P95         float64
	}
	err := tx.Select(`
		COUNT(*) AS sample_count,
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (first_reply_at - waiting_since))), 0) AS p50,
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (first_reply_at - waiting_since))), 0) AS p95
	`).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	out.SampleCount = row.SampleCount
	out.P50Seconds = row.P50
	out.P95Seconds = row.P95
	return out, nil
}
