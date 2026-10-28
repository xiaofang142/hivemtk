package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// LLMUsageStat LLM 用量统计结果
type LLMUsageStat struct {
	Tokens int64
	Cost   float64
}

// AIProductivityRepository AI 产能分析仓储
type AIProductivityRepository struct {
	db *gorm.DB
}

// NewAIProductivityRepository 创建 AI 产能仓储实例
func NewAIProductivityRepository() *AIProductivityRepository {
	return &AIProductivityRepository{db: _db.GetDB()}
}

// CountCustomerSessionsByTimeRange 统计时间范围内的会话数
func (r *AIProductivityRepository) CountCustomerSessionsByTimeRange(ctx context.Context, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.CustomerSession{}).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Count(&count).Error
	return count, err
}

// CountCustomerSessionsByDayRange 统计某一天内的会话数（左闭右开 [day, dayEnd)）
func (r *AIProductivityRepository) CountCustomerSessionsByDayRange(ctx context.Context, day, dayEnd time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.CustomerSession{}).
		Where("created_at >= ? AND created_at < ?", day, dayEnd).
		Count(&count).Error
	return count, err
}

// CountSessionMessagesBySenderType 统计时间范围内某发送者类型的消息数
func (r *AIProductivityRepository) CountSessionMessagesBySenderType(ctx context.Context, startTime, endTime time.Time, senderType string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("session_messages").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Where("sender_type = ?", senderType).
		Count(&count).Error
	return count, err
}

// CountSessionMessagesBySenderTypeAndDayRange 统计某一天内某发送者类型的消息数（左闭右开 [day, dayEnd)）
func (r *AIProductivityRepository) CountSessionMessagesBySenderTypeAndDayRange(ctx context.Context, day, dayEnd time.Time, senderType string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("session_messages").
		Where("created_at >= ? AND created_at < ?", day, dayEnd).
		Where("sender_type = ?", senderType).
		Count(&count).Error
	return count, err
}

// GetAvgResponseTime 平均响应时长（秒）
//
// 口径：只统计「用户问 → 紧接着的首条回复（ai/agent/staff）」的间隔。
// 旧实现取的是**任意相邻两条消息**的差（AI 连发两条、用户连发两条、跨天再回来都算），
// 实测把间隔均值抬到 3220 秒（53 分钟），页面「平均响应时长」因此失真。
// 多轮里只有首条回复计入，避免 AI 连发被重复计为多次响应。
//
// PostgreSQL 不允许在聚合函数(AVG)中直接嵌套窗口函数(LAG)，
// 必须用子查询先算出相邻消息与相邻发送者，再在外层聚合。
func (r *AIProductivityRepository) GetAvgResponseTime(ctx context.Context, startTime, endTime time.Time) (float64, error) {
	type rtRow struct {
		Avg float64
	}
	var rt rtRow
	err := r.db.WithContext(ctx).
		Table("(SELECT created_at, sender_type, "+
			"LAG(created_at) OVER (PARTITION BY session_id ORDER BY created_at) AS prev_at, "+
			"LAG(sender_type) OVER (PARTITION BY session_id ORDER BY created_at) AS prev_sender "+
			"FROM session_messages WHERE created_at BETWEEN ? AND ?) t", startTime, endTime).
		Select("COALESCE(AVG(EXTRACT(EPOCH FROM (created_at - prev_at))), 0) AS avg").
		Where("prev_sender = ? AND sender_type IN ?", "user", []string{"ai", "agent", "staff"}).
		Scan(&rt).Error
	return rt.Avg, err
}

// CountOrdersByUnixTimeRangeAndStatus 统计时间范围内某状态的订单数
func (r *AIProductivityRepository) CountOrdersByUnixTimeRangeAndStatus(ctx context.Context, startTime, endTime time.Time, status int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Order{}).
		Where("create_time BETWEEN ? AND ?", startTime.Unix(), endTime.Unix()).
		Where("status = ?", status).
		Count(&count).Error
	return count, err
}

// CountOrdersByUnixDayRangeAndStatus 统计某一天内某状态的订单数（左闭右开 [day, dayEnd)）
func (r *AIProductivityRepository) CountOrdersByUnixDayRangeAndStatus(ctx context.Context, day, dayEnd time.Time, status int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Order{}).
		Where("create_time >= ? AND create_time < ?", day.Unix(), dayEnd.Unix()).
		Where("status = ?", status).
		Count(&count).Error
	return count, err
}

// GetLLMUsageStats 统计时间范围内的 LLM Token 用量与成本
func (r *AIProductivityRepository) GetLLMUsageStats(ctx context.Context, startTime, endTime time.Time) (LLMUsageStat, error) {
	var ur LLMUsageStat
	err := r.db.WithContext(ctx).
		Table("llm_usage_records").
		Select("COALESCE(SUM(total_tokens), 0) as tokens, COALESCE(SUM(cost), 0) as cost").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Scan(&ur).Error
	return ur, err
}

// GetLLMUsageCostSum 统计某一天内的 LLM 成本之和（左闭右开 [day, dayEnd)）
func (r *AIProductivityRepository) GetLLMUsageCostSum(ctx context.Context, day, dayEnd time.Time) (float64, error) {
	var costs []float64
	err := r.db.WithContext(ctx).
		Table("llm_usage_records").
		Select("COALESCE(SUM(cost), 0)").
		Where("created_at >= ? AND created_at < ?", day, dayEnd).
		Scan(&costs).Error
	if err != nil {
		return 0, err
	}
	if len(costs) > 0 {
		return costs[0], nil
	}
	return 0, nil
}
