// handoff_chain_repo.go 转派链仓储（五层 L5）
package repository

import (
	"context"
	"errors"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrHandoffTenantContextMissing on 模式下拿不到租户归属时拒绝扫/写 customer_sessions。
var ErrHandoffTenantContextMissing = errors.New("handoff: 表 customer_sessions 需要租户上下文但 ctx 未绑定")

// HandoffChainRepository 转派链规则与决策记录查询收口
//
// handoff_rules KV 读写走 SystemConfigKVRepository（D12 守卫：遗留 KV 直查已收敛）
type HandoffChainRepository struct {
	db    *gorm.DB
	kv    SystemConfigKVRepository
	kvSet bool
}

// NewHandoffChainRepository 构造
// NewHandoffChainRepository 构造（无参，内部取全局 DB；对齐 customer_session.go 先例）
func NewHandoffChainRepository() *HandoffChainRepository {
	return NewHandoffChainRepositoryWithDB(_db.GetDB())
}

// NewHandoffChainRepositoryWithDB 显式注入 DB（测试/装配用）
func NewHandoffChainRepositoryWithDB(db *gorm.DB) *HandoffChainRepository {
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

// sessionsFor 构造带显式租户条件的 customer_sessions 语句。
//
// 隔离 callback 只给"认得出目标表"的语句注入 tenant_id，而 Table("customer_sessions")
// 这种 schema-less 形态在 callback 眼里既不会被拒、也不会被过滤。cron 经 Fanout 逐租户
// 执行时，这就是"每个租户扫到同一批会话、并把别的租户的会话升级掉"（线上
// 日志验证：一轮 tick 打印两行完全相同的 sessions=1）。
//
// 口径对齐 kvTenantIDFor 与 config_param_tenant.go 的显式条件写法：有绑定按绑定过滤；
// 系统作用域放行（跨租户运维的唯一入口）；off 模式不过滤（与 callback 在 off 模式直通
// 一致，直连仓储的既有调用方与测试行为不变）；on 模式无绑定拒绝，绝不静默扫全表。
func (r *HandoffChainRepository) sessionsFor(ctx context.Context) (*gorm.DB, error) {
	tx := r.db.WithContext(ctx).Table("customer_sessions")
	if tenant.IsSystemScope(ctx) {
		return tx, nil
	}
	if id, ok := tenant.FromContext(ctx); ok {
		return tx.Where("tenant_id = ?", id), nil
	}
	if !tenant.IsMultiTenant() {
		return tx, nil
	}
	return nil, ErrHandoffTenantContextMissing
}

// SaveHandoffRules 写入 handoff_rules KV 值
func (r *HandoffChainRepository) SaveHandoffRules(ctx context.Context, value string) (string, error) {
	return r.getKV().Upsert(ctx, "handoff_rules", value)
}

// HandoffSessionSnapshot 规则评估所需会话快照
//
// 两处口径以"规则能不能真触发"为准：
//   - 身份用业务会话号 session_id（不是自增主键 id）：handoff_decisions.session_id 的
//     另一位生产者（置信度决策 service）写的就是业务号，按主键登记会让同一张表里两种
//     口径并存，按会话查决策记录的那条路永远读不到 cron 这批。
//   - "是否已解决"读 resolved_at（解决时由 customer_session.go 写入），不是 updated_at：
//     updated_at 由 autoUpdateTime 维护、任何一行都非空，而两条默认规则都要求"解决时刻为空"
//     才触发 ⇒ 映射到 updated_at 等于把整条升级链钉死（线上实测 triggered 恒为 0）。
type HandoffSessionSnapshot struct {
	SessionID       string     `gorm:"column:session_id"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ResolvedAt      *time.Time `gorm:"column:resolved_at"`
	CsatScore       *int       `gorm:"column:rating"`
	AssignedAgentID *uint      `gorm:"column:agent_id"`
}

// GetHandoffSessionSnapshot 读取单会话规则评估快照
func (r *HandoffChainRepository) GetHandoffSessionSnapshot(ctx context.Context, sessionID string) (*HandoffSessionSnapshot, error) {
	if r.db == nil {
		return nil, errors.New("handoff chain repository not initialized")
	}
	tx, err := r.sessionsFor(ctx)
	if err != nil {
		return nil, err
	}
	var sess HandoffSessionSnapshot
	err = tx.
		Select("session_id, status, created_at, resolved_at, rating, agent_id").
		Where("session_id = ?", sessionID).
		Scan(&sess).Error
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// ListUnresolvedSessionIDs 按创建时间升序取本租户未解决会话的业务号（cron 扫描源）
func (r *HandoffChainRepository) ListUnresolvedSessionIDs(ctx context.Context, limit int) ([]string, error) {
	if r.db == nil {
		return nil, nil
	}
	tx, err := r.sessionsFor(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	err = tx.
		Select("session_id").
		Where("status NOT IN ?", []string{"resolved", "closed"}).
		Order("created_at ASC").
		Limit(limit).
		Scan(&ids).Error
	return ids, err
}

// CreateHandoffDecisionRecord 写转派决策记录（幂等）。
// DecisionID = ruleID_sessionID 是确定性业务键：同一规则对同一会话的重复命中
// 属于 cron 正常路径，ON CONFLICT DO NOTHING 跳过；created=false 交由调用方
// 跳过 escalate 等副作用，避免每分钟重复写库撞唯一约束。
func (r *HandoffChainRepository) CreateHandoffDecisionRecord(ctx context.Context, record *model.HandoffDecisionRecord) (created bool, err error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(record)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// EscalateSession 会话升级：本租户内 status → escalated
func (r *HandoffChainRepository) EscalateSession(ctx context.Context, sessionID string) error {
	if r.db == nil {
		return nil
	}
	tx, err := r.sessionsFor(ctx)
	if err != nil {
		return err
	}
	return tx.
		Where("session_id = ?", sessionID).
		Update("status", "escalated").Error
}
