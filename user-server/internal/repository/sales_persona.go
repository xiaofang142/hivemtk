package repository

import (
	"context"
	"strconv"
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

// ResponseSample 员工响应配对统计（真实样本）。
// Sample=0 表示窗口内没有「客户消息→该员工首答」的可配对样本，调用方须单独标识缺数据，
// 不得按零耗时评满分。
type ResponseSample struct {
	AvgSec float64
	Sample int64
}

// HumanResponseByStaff 统计窗口内「客户消息 → 该员工首答」的平均响应秒数与真实样本量。
//
// 归因口径（FA-42）：
//   - 会话必须属于该员工（customer_sessions.agent_id = staffID）；
//   - 客户消息 sender_type ∈ ('customer','user')，该员工回复 sender_type='agent'
//     且 sender_id = 员工ID字符串（见 customer_session_takeover.go 的 SendMessage 写法）；
//   - 按 created_at（而非插入序）排序后逐会话配对，乱序写入也能正确归因；
//   - 每条客户消息只与其后该员工的首条回复配对，其他员工/AI 回复不参与；
//   - 观察窗口 [start, end) 对消息 created_at 过滤，显式可复算。
func (r *SalesPersonaRepository) HumanResponseByStaff(ctx context.Context, staffID uint, start, end time.Time) (ResponseSample, error) {
	if r == nil || r.db == nil {
		return ResponseSample{}, nil
	}
	var sessions []string
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND created_at < ?", staffID, end).
		Pluck("session_id", &sessions).Error; err != nil {
		return ResponseSample{}, err
	}
	if len(sessions) == 0 {
		return ResponseSample{}, nil
	}

	type msgRow struct {
		SessionID  string
		SenderType string
		SenderID   string
		CreatedAt  time.Time
	}
	var msgs []msgRow
	if err := r.db.WithContext(ctx).Table("session_messages").
		Select("session_id, sender_type, sender_id, created_at").
		Where("session_id IN ? AND created_at >= ? AND created_at < ? AND is_internal = ?", sessions, start, end, false).
		Order("created_at ASC, id ASC").
		Scan(&msgs).Error; err != nil {
		return ResponseSample{}, err
	}

	staffIDStr := strconv.FormatUint(uint64(staffID), 10)
	pending := make(map[string]time.Time)
	var total float64
	var sample int64
	for _, m := range msgs {
		switch {
		case m.SenderType == "customer" || m.SenderType == "user":
			if _, ok := pending[m.SessionID]; !ok {
				pending[m.SessionID] = m.CreatedAt
			}
		case m.SenderType == "agent" && m.SenderID == staffIDStr:
			if c, ok := pending[m.SessionID]; ok {
				total += m.CreatedAt.Sub(c).Seconds()
				sample++
				delete(pending, m.SessionID)
			}
		}
	}
	if sample == 0 {
		return ResponseSample{}, nil
	}
	return ResponseSample{AvgSec: total / float64(sample), Sample: sample}, nil
}

// SessionOrderCount 统计窗口内该员工的会话数与归属订单数。
// 会话按 agent_id=staffID；订单按 account_id ∈ 该员工会话的 account_id 集合归属，
// 窗口按 order.create_time（unix 秒）过滤，避免把全租户订单算到个人头上。
func (r *SalesPersonaRepository) SessionOrderCount(ctx context.Context, staffID uint, start, end time.Time) (sessions int64, orders int64, err error) {
	if r == nil || r.db == nil {
		return 0, 0, nil
	}
	if err = r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND created_at >= ? AND created_at < ?", staffID, start, end).
		Count(&sessions).Error; err != nil {
		return 0, 0, err
	}
	var accounts []string
	if err = r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND created_at >= ? AND created_at < ?", staffID, start, end).
		Pluck("account_id", &accounts).Error; err != nil {
		return 0, 0, err
	}
	if len(accounts) == 0 {
		return sessions, 0, nil
	}
	if err = r.db.WithContext(ctx).Table("order").
		Where("account_id IN ? AND create_time >= ? AND create_time < ?", accounts, start.Unix(), end.Unix()).
		Count(&orders).Error; err != nil {
		return 0, 0, err
	}
	return sessions, orders, nil
}

func (r *SalesPersonaRepository) CountSopExecutionsByStaff(ctx context.Context, staffID uint, start, end time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("sop_executions AS se").
		Joins("JOIN customer_sessions AS cs ON se.session_id = cs.session_id").
		Where("cs.agent_id = ? AND cs.created_at >= ? AND cs.created_at < ?", staffID, start, end).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SalesPersonaRepository) CountRagQueryLogsByUser(ctx context.Context, staffID uint, start, end time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.RagSession{}).
		Where("user_id = ? AND created_at >= ? AND created_at < ?", strconv.FormatUint(uint64(staffID), 10), start, end).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SalesPersonaRepository) CountObjectionRecordsByStaff(ctx context.Context, staffID uint, start, end time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("sop_exec_events AS se").
		Joins("JOIN sop_executions AS sx ON se.execution_id = sx.id").
		Joins("JOIN customer_sessions AS cs ON sx.session_id = cs.session_id").
		Where("se.node_type = ? AND cs.agent_id = ? AND cs.created_at >= ? AND cs.created_at < ?", "objection", staffID, start, end).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

type SatisfactionStats struct {
	Avg   float64
	Count int64
}

func (r *SalesPersonaRepository) SatisfactionByStaff(ctx context.Context, staffID uint, start, end time.Time) (*SatisfactionStats, error) {
	if r == nil || r.db == nil {
		return &SatisfactionStats{}, nil
	}
	var stats SatisfactionStats
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Select("AVG(rating) as avg, COUNT(rating) as count").
		Where("agent_id = ? AND rating > 0 AND created_at >= ? AND created_at < ?", staffID, start, end).
		Scan(&stats).Error; err != nil {
		return &SatisfactionStats{}, err
	}
	return &stats, nil
}

type SessionCloseStats struct {
	Total  int64
	Closed int64
}

func (r *SalesPersonaRepository) SessionCloseByStaff(ctx context.Context, staffID uint, start, end time.Time) (*SessionCloseStats, error) {
	if r == nil || r.db == nil {
		return &SessionCloseStats{}, nil
	}
	var stats SessionCloseStats
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND created_at >= ? AND created_at < ?", staffID, start, end).
		Count(&stats.Total).Error; err != nil {
		return &SessionCloseStats{}, err
	}
	if err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("agent_id = ? AND status = ? AND created_at >= ? AND created_at < ?", staffID, "closed", start, end).
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
