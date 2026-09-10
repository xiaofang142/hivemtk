// agent_attribution.go Agent 绩效归因仓储（五层 L5）
//
// 聚合 customer_sessions / csat_surveys 两表的统计查询，
// 供 service 层做指标拼装，禁止 service 直连 gorm。
package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AgentAttributionRepository Agent 绩效归因仓储
type AgentAttributionRepository struct {
	db *gorm.DB
}

// NewAgentAttributionRepository 构造
func NewAgentAttributionRepository() *AgentAttributionRepository {
	return &AgentAttributionRepository{db: GetDB()}
}

// NewAgentAttributionRepositoryWithDB 指定 DB 构造（测试/装配用）
func NewAgentAttributionRepositoryWithDB(db *gorm.DB) *AgentAttributionRepository {
	return &AgentAttributionRepository{db: db}
}

// SessionAggRow 会话聚合行
type SessionAggRow struct {
	AgentID       uint    `gorm:"column:agent_id"`
	AgentName     string  `gorm:"column:agent_name"`
	Total         int64   `gorm:"column:total"`
	AIResolved    int64   `gorm:"column:ai_resolved"`
	HumanTakeover int64   `gorm:"column:human_takeover"`
	AvgResolveSec float64 `gorm:"column:avg_resolve_sec"`
}

// CSATAggRow CSAT 聚合行
type CSATAggRow struct {
	AgentID   uint    `gorm:"column:agent_id"`
	AvgScore  float64 `gorm:"column:avg_score"`
	Responded int64   `gorm:"column:responded"`
}

// AggregateSessionsByAgent 按agent聚合时间窗口内的会话解决指标
func (r *AgentAttributionRepository) AggregateSessionsByAgent(ctx context.Context, agentID uint, start, end time.Time) ([]SessionAggRow, error) {
	var rows []SessionAggRow
	query := r.db.WithContext(ctx).
		Model(&model.CustomerSession{}).
		Select(`
			agent_id,
			agent_name,
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE handler_type = 'ai') as ai_resolved,
			COUNT(*) FILTER (WHERE handler_type = 'human') as human_takeover,
			AVG(EXTRACT(EPOCH FROM (COALESCE(resolved_at, updated_at) - created_at))) as avg_resolve_sec
		`).
		Where("created_at BETWEEN ? AND ? AND agent_id > 0", start, end).
		Group("agent_id, agent_name")
	if agentID > 0 {
		query = query.Where("agent_id = ?", agentID)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// AggregateCSATByAgent 按agent聚合时间窗口内的 CSAT 评分
func (r *AgentAttributionRepository) AggregateCSATByAgent(ctx context.Context, start, end time.Time) ([]CSATAggRow, error) {
	var rows []CSATAggRow
	if err := r.db.WithContext(ctx).
		Table("csat_surveys cs").
		Select(`
			cs.agent_id,
			AVG(cs.score) as avg_score,
			COUNT(*) as responded
		`).
		Joins("JOIN customer_sessions sess ON sess.session_id = cs.session_id").
		Where("cs.status = 'responded' AND cs.score > 0 AND sess.created_at BETWEEN ? AND ?", start, end).
		Group("cs.agent_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
