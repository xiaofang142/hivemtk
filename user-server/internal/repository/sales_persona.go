package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// SalesPersonaRepository 销冠能力画像聚合查询仓储。
// 查询为 PostgreSQL 原生 SQL（含窗口函数/EXTRACT EPOCH），仅 PG 上结果正确。
type SalesPersonaRepository struct {
	db *gorm.DB
}

func NewSalesPersonaRepository(db *gorm.DB) *SalesPersonaRepository {
	return &SalesPersonaRepository{db: db}
}

// NewSalesPersonaRepositoryDefault 默认构造（全局句柄；未装配时返回 nil，调用方按
// 「repo 未装配」处理——NewFAQRepositoryDefault 同款语义）。
//
// AV-04c 批次6：service/sales_persona.go 的构造期装配与 ensureReposFromDB 懒构造
// 原先都自己取 repository.GetDB()（service 直取 DB 句柄的宽口径），收口为本构造。
// 全局无句柄时返回 nil：懒构造侧保持「本轮回退、下轮再试」的原语义；全部 8 个
// 查询方法既有 r == nil || r.db == nil 守卫（fail-open 记零分），无库输出不变。
func NewSalesPersonaRepositoryDefault() *SalesPersonaRepository {
	if db := GetDB(); db != nil {
		return NewSalesPersonaRepository(db)
	}
	return nil
}

// AvgHumanResponseSec 统计近 30 天人工座席的平均响应时长（秒），
// 按 session_id 分区取相邻消息时间差。
func (r *SalesPersonaRepository) AvgHumanResponseSec(ctx context.Context) (float64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var avg float64
	sub := r.db.WithContext(ctx).Table("session_messages").
		Select("created_at - LAG(created_at) OVER (PARTITION BY session_id ORDER BY created_at) AS diff").
		Where("sender_type = ?", "human").
		Where("created_at >= ?", time.Now().AddDate(0, 0, -30))
	err := r.db.WithContext(ctx).Table("(?) AS t", sub).
		Select("COALESCE(AVG(EXTRACT(EPOCH FROM diff)), 0)").
		Scan(&avg).Error
	if err != nil {
		return 0, err
	}
	return avg, nil
}

func (r *SalesPersonaRepository) SessionOrderCount(ctx context.Context, staffID uint) (sessions int64, orders int64, err error) {
	if r == nil || r.db == nil {
		return 0, 0, nil
	}
	if err = r.db.WithContext(ctx).Table("customer_sessions").
		Select("COUNT(*) as sessions").
		Where("agent_id = ? AND created_at >= ?", staffID, time.Now().AddDate(0, 0, -30)).
		Scan(&sessions).Error; err != nil {
		return 0, 0, err
	}
	if err = r.db.WithContext(ctx).Table("order").
		Select("COUNT(*) as orders").
		Where("create_time >= ?", time.Now().AddDate(0, 0, -30).Unix()).
		Scan(&orders).Error; err != nil {
		return 0, 0, err
	}
	return sessions, orders, nil
}

func (r *SalesPersonaRepository) CountSopExecutionsByStaff(ctx context.Context, staffID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("sop_executions AS se").
		Joins("JOIN customer_sessions AS cs ON se.session_id = cs.session_id").
		Where("cs.agent_id = ?", staffID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SalesPersonaRepository) CountRagQueryLogsByUser(ctx context.Context, staffID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("rag_sessions").
		Where("user_id = ?", staffID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SalesPersonaRepository) CountObjectionRecordsByStaff(ctx context.Context, staffID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("sop_exec_events AS se").
		Joins("JOIN sop_executions AS sx ON se.execution_id = sx.id").
		Joins("JOIN customer_sessions AS cs ON sx.session_id = cs.session_id").
		Where("se.node_type = ?", "objection").
		Where("cs.agent_id = ?", staffID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

type SatisfactionStats struct {
	Avg   float64
	Count int64
}

func (r *SalesPersonaRepository) SatisfactionByStaff(ctx context.Context, staffID uint) (*SatisfactionStats, error) {
	if r == nil || r.db == nil {
		return &SatisfactionStats{}, nil
	}
	var stats SatisfactionStats
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Select("AVG(rating) as avg, COUNT(rating) as count").
		Where("agent_id = ? AND rating > 0", staffID).
		Scan(&stats).Error; err != nil {
		return &SatisfactionStats{}, err
	}
	return &stats, nil
}

type SessionCloseStats struct {
	Total  int64
	Closed int64
}

func (r *SalesPersonaRepository) SessionCloseByStaff(ctx context.Context, staffID uint) (*SessionCloseStats, error) {
	if r == nil || r.db == nil {
		return &SessionCloseStats{}, nil
	}
	var stats SessionCloseStats
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ?", staffID).
		Count(&stats.Total).Error; err != nil {
		return &SessionCloseStats{}, err
	}
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND status = ?", staffID, "closed").
		Count(&stats.Closed).Error; err != nil {
		return &SessionCloseStats{}, err
	}
	return &stats, nil
}

type StaffSessionCount struct {
	AgentID uint  `gorm:"column:agent_id"`
	Count   int64 `gorm:"column:count"`
}

func (r *SalesPersonaRepository) ListTopStaffsBySession(ctx context.Context) ([]StaffSessionCount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var rows []StaffSessionCount
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Select("agent_id, COUNT(*) as count").
		Where("agent_id > 0").
		Group("agent_id").
		Order("count DESC").
		Limit(50).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
