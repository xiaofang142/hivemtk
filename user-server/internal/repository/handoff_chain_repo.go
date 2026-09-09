// handoff_chain_repo.go 转派链仓储（五层 L5）
package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// HandoffChainRepository 转派链规则与决策记录查询收口
//
// handoff_rules KV 读写走 SystemConfigKVRepository（D12 守卫：遗留 KV 直查已收敛）
type HandoffChainRepository struct {
	db    *gorm.DB
	kv    SystemConfigKVRepository
	kvSet bool
}

// NewHandoffChainRepository 构造
func NewHandoffChainRepository(db *gorm.DB) *HandoffChainRepository {
	return &HandoffChainRepository{db: db}
}

// getKV 惰性构造 KV 仓储（复用注入的 db，全局构造则走默认句柄）
func (r *HandoffChainRepository) getKV() SystemConfigKVRepository {
	if r.kvSet {
		return r.kv
	}
	if r.db != nil {
		r.kv = &systemConfigKVRepo{db: r.db}
	} else {
		r.kv = NewSystemConfigKVRepository()
	}
	r.kvSet = true
	return r.kv
}

// GetHandoffRules 读取 handoff_rules KV 值（未配置返回空串）
func (r *HandoffChainRepository) GetHandoffRules(ctx context.Context) (string, error) {
	return r.getKV().Get(ctx, "handoff_rules")
}

// SaveHandoffRules 写入 handoff_rules KV 值
func (r *HandoffChainRepository) SaveHandoffRules(ctx context.Context, value string) (string, error) {
	return r.getKV().Upsert(ctx, "handoff_rules", value)
}

// HandoffSessionSnapshot 规则评估所需会话快照
type HandoffSessionSnapshot struct {
	ID              string     `gorm:"column:id"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ResolvedAt      *time.Time `gorm:"column:updated_at"`
	CsatScore       *int       `gorm:"column:rating"`
	AssignedAgentID *uint      `gorm:"column:agent_id"`
}

// GetHandoffSessionSnapshot 读取单会话规则评估快照
func (r *HandoffChainRepository) GetHandoffSessionSnapshot(ctx context.Context, sessionID string) (*HandoffSessionSnapshot, error) {
	if r.db == nil {
		return nil, errors.New("handoff chain repository not initialized")
	}
	var sess HandoffSessionSnapshot
	err := r.db.WithContext(ctx).
		Table("customer_sessions").
		Select("id, status, created_at, updated_at, rating, agent_id").
		Where("id = ?", sessionID).
		Scan(&sess).Error
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// ListUnresolvedSessionIDs 按创建时间升序取未解决会话 ID（cron 扫描源）
func (r *HandoffChainRepository) ListUnresolvedSessionIDs(ctx context.Context, limit int) ([]string, error) {
	if r.db == nil {
		return nil, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).
		Table("customer_sessions").
		Select("id").
		Where("status NOT IN ?", []string{"resolved", "closed"}).
		Order("created_at ASC").
		Limit(limit).
		Scan(&ids).Error
	return ids, err
}

// CreateHandoffDecisionRecord 写转派决策记录
func (r *HandoffChainRepository) CreateHandoffDecisionRecord(ctx context.Context, record *model.HandoffDecisionRecord) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(record).Error
}

// EscalateSession 会话升级：status → escalated
func (r *HandoffChainRepository) EscalateSession(ctx context.Context, sessionID string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).
		Table("customer_sessions").
		Where("id = ?", sessionID).
		Update("status", "escalated").Error
}
