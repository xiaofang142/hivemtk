// agent_checkpoint_repo.go Agent Runtime 阶段级 Checkpoint 仓储（五层 L5）
package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
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

// NewAgentCheckpointRepository 构造
func NewAgentCheckpointRepository(db *gorm.DB) *AgentCheckpointRepository {
	return &AgentCheckpointRepository{db: db}
}

// Save 阶段完成即落 checkpoint（upsert，幂等）
func (r *AgentCheckpointRepository) Save(ctx context.Context, threadID, stage string, state json.RawMessage) error {
	if r.db == nil {
		return nil
	}
	if threadID == "" || stage == "" {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO agent_checkpoints (thread_id, stage, state, created_at, updated_at)
		VALUES (?, ?, ?, NOW(), NOW())
		ON CONFLICT (thread_id, stage) DO UPDATE SET state = EXCLUDED.state, updated_at = NOW()`,
		threadID, stage, string(state)).Error
}

// LoadLatest 取该 thread 最新 checkpoint（按 updated_at DESC），无记录返回 nil
func (r *AgentCheckpointRepository) LoadLatest(ctx context.Context, threadID string) (*AgentCheckpoint, error) {
	if r.db == nil || threadID == "" {
		return nil, nil
	}
	var rows []struct {
		ThreadID  string          `gorm:"column:thread_id"`
		Stage     string          `gorm:"column:stage"`
		State     json.RawMessage `gorm:"column:state"`
		UpdatedAt time.Time       `gorm:"column:updated_at"`
	}
	if err := r.db.WithContext(ctx).Table("agent_checkpoints").
		Where("thread_id = ?", threadID).
		Order("updated_at DESC").Limit(1).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &AgentCheckpoint{
		ThreadID:  rows[0].ThreadID,
		Stage:     rows[0].Stage,
		State:     rows[0].State,
		UpdatedAt: rows[0].UpdatedAt,
	}, nil
}
