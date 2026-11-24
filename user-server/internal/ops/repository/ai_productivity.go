package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// LLMUsageStat LLM 用量统计结果。
//
// FA-AN-01：唯一权威用量事实 = llm_routing_logs（每次 Dispatch 落一条，自带 Key 与
// 平台垫资都在此记 cost）。旧实现读的 llm_usage_records 全仓只有迁移与读口、无写入方
// ⇒ 恒为 0；而平台账本 platform_llm_usage_records 与 routing_logs 成本口径恒一致，
// 两者相加会双计。故产能看板只取 routing_logs 一套，不叠加平台账本。
type LLMUsageStat struct {
	Tokens int64
	Cost   float64
	Calls  int64
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

// Normalized sender identity (FA-AN-01): all inbound channels are one class,
// all human reply entrypoints are one class. inbox_reply.go writes SenderType="agent",
// the legacy takeover path writes "human"/"staff"; only counting "human" silently
// dropped real human replies. AI replies stay "ai".
var (
	ProductivityInboundSenders = []string{"customer", "user"}
	ProductivityAISenders      = []string{"ai"}
	ProductivityHumanSenders   = []string{"agent", "staff", "human"}
)

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

// CountSessionMessagesBySenderTypes 统计时间范围内多个发送者类型的消息数（规范身份集合）。
func (r *AIProductivityRepository) CountSessionMessagesBySenderTypes(ctx context.Context, startTime, endTime time.Time, senderTypes []string) (int64, error) {
	if len(senderTypes) == 0 {
		return 0, nil
	}
	var count int64
	err := r.db.WithContext(ctx).
		Table("session_messages").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Where("sender_type IN ?", senderTypes).
		Count(&count).Error
	return count, err
}

// CountSessionMessagesBySenderTypesAndDayRange 统计某一天内多个发送者类型的消息数（左闭右开 [day, dayEnd)）。
func (r *AIProductivityRepository) CountSessionMessagesBySenderTypesAndDayRange(ctx context.Context, day, dayEnd time.Time, senderTypes []string) (int64, error) {
	if len(senderTypes) == 0 {
		return 0, nil
	}
	var count int64
	err := r.db.WithContext(ctx).
		Table("session_messages").
		Where("created_at >= ? AND created_at < ?", day, dayEnd).
		Where("sender_type IN ?", senderTypes).
		Count(&count).Error
	return count, err
}

// CountInboundMessages / CountInboundMessagesByDayRange 规范入站口径（customer + user）。
func (r *AIProductivityRepository) CountInboundMessages(ctx context.Context, start, end time.Time) (int64, error) {
	return r.CountSessionMessagesBySenderTypes(ctx, start, end, ProductivityInboundSenders)
}
func (r *AIProductivityRepository) CountInboundMessagesByDayRange(ctx context.Context, day, dayEnd time.Time) (int64, error) {
	return r.CountSessionMessagesBySenderTypesAndDayRange(ctx, day, dayEnd, ProductivityInboundSenders)
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
//
// 子查询必须用 GORM 子查询（Table("(?)", sub)）构造，禁止 `Table("(SELECT … FROM
// session_messages …)")` 字面量派生表：派生表外层解析不出表名 ⇒ 隔离 callback
// 不注入；字面量里的 FROM 同样在 callback 的 JOIN/预构建 SQL 扫描面之外 ⇒
// on 模式下会把**全部租户**的 session_messages 聚进同一个平均响应时长
// （/api/analytics/ai-productivity 是租户身份端点，实测命中，specs/001 S-18）。
// *gorm.DB 子查询由 GORM 经 callbacks.Query().Execute(DryRun) 展开，
// 租户隔离 callback 对子查询照常注入 WHERE tenant_id = ?。
func (r *AIProductivityRepository) GetAvgResponseTime(ctx context.Context, startTime, endTime time.Time) (float64, error) {
	type rtRow struct {
		Avg float64
	}
	var rt rtRow
	lagged := r.db.WithContext(ctx).
		Table("session_messages").
		Select("created_at, sender_type, "+
			"LAG(created_at) OVER (PARTITION BY session_id ORDER BY created_at) AS prev_at, "+
			"LAG(sender_type) OVER (PARTITION BY session_id ORDER BY created_at) AS prev_sender").
		Where("created_at BETWEEN ? AND ?", startTime, endTime)
	err := r.db.WithContext(ctx).
		Table("(?) t", lagged).
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

// GetLLMUsageStats 统计时间范围内的 LLM Token 用量与成本（权威事实 = llm_routing_logs）。
func (r *AIProductivityRepository) GetLLMUsageStats(ctx context.Context, startTime, endTime time.Time) (LLMUsageStat, error) {
	var ur LLMUsageStat
	err := r.db.WithContext(ctx).
		Table("llm_routing_logs").
		Select("COALESCE(SUM(total_tokens), 0) as tokens, COALESCE(SUM(cost), 0) as cost, COUNT(*) as calls").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Scan(&ur).Error
	return ur, err
}

// GetLLMUsageCostSum 统计某一天内的 LLM 成本之和（左闭右开 [day, dayEnd)，权威事实 = llm_routing_logs）。
func (r *AIProductivityRepository) GetLLMUsageCostSum(ctx context.Context, day, dayEnd time.Time) (float64, error) {
	var result struct{ Cost float64 }
	err := r.db.WithContext(ctx).
		Table("llm_routing_logs").
		Select("COALESCE(SUM(cost), 0) as cost").
		Where("created_at >= ? AND created_at < ?", day, dayEnd).
		Scan(&result).Error
	return result.Cost, err
}
