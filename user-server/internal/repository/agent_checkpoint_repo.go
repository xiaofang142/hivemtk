// agent_checkpoint_repo.go Agent Runtime 阶段级 Checkpoint 仓储（五层 L5）
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AgentCheckpoint 单条记录
type AgentCheckpoint struct {
	ThreadID  string
	Stage     string
	State     json.RawMessage
	UpdatedAt time.Time
}

// AgentCheckpointRepository agent_checkpoints 存取（五层 L5：gorm 收敛在仓储）
type AgentCheckpointRepository struct {
	db *gorm.DB
}

func NewAgentCheckpointRepository(db *gorm.DB) *AgentCheckpointRepository {
	return &AgentCheckpointRepository{db: db}
}

// Save 阶段完成即落 checkpoint（upsert，幂等）
func (r *AgentCheckpointRepository) Save(ctx context.Context, threadID, stage string, state json.RawMessage) error {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenantID == 0 {
		return errors.New("checkpoint_tenant_required")
	}
	if r == nil || r.db == nil {
		return errors.New("checkpoint_store_unavailable")
	}
	if threadID == "" || stage == "" {
		return errors.New("checkpoint_identity_unavailable")
	}
	now := time.Now()
	return r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Table("agent_checkpoints").
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "thread_id"}, {Name: "stage"}},
			DoUpdates: clause.Assignments(map[string]any{
				"state":      string(state),
				"updated_at": now,
			}),
		}).
		Create(map[string]any{
			"tenant_id":  tenantID,
			"thread_id":  threadID,
			"stage":      stage,
			"state":      string(state),
			"created_at": now,
			"updated_at": now,
		}).Error
}

// LoadAllByThread 取该 thread 的全部阶段快照（单线程上限 = 阶段数，量级 5）。
//
// 不在 SQL 里按 updated_at 排序取"最新"：阶段可重跑（如 planner 失败后整阶段重来），
// 重跑会把早期阶段的 updated_at 顶到最后，按时间取就会把恢复游标倒退。
// "最新"的权威定义是阶段序号最大（AgentStageNames 顺序），该顺序表在 service 层，
// 故仓储只负责取全量、由 service 判定游标。
func (r *AgentCheckpointRepository) LoadAllByThread(ctx context.Context, threadID string) ([]AgentCheckpoint, error) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, errors.New("checkpoint_tenant_required")
	}
	if r == nil || r.db == nil {
		return nil, errors.New("checkpoint_store_unavailable")
	}
	if threadID == "" {
		return nil, errors.New("checkpoint_identity_unavailable")
	}
	var rows []struct {
		ThreadID  string          `gorm:"column:thread_id"`
		Stage     string          `gorm:"column:stage"`
		State     json.RawMessage `gorm:"column:state"`
		UpdatedAt time.Time       `gorm:"column:updated_at"`
	}
	if err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Table("agent_checkpoints").
		Where("tenant_id = ? AND thread_id = ?", tenantID, threadID).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AgentCheckpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentCheckpoint{
			ThreadID:  row.ThreadID,
			Stage:     row.Stage,
			State:     row.State,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}
