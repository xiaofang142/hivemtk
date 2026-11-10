package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// IntentRouteDailyRepository 意图路由日聚合仓储（TB-11 观测面数据面）。
//
// 租户作用域：写侧显式 stamps TenantID（FromContext；单租户/无租户上下文
// 回落 DefaultTenantID，与 tenant.ContextWithDefault 语义一致）；读侧不拼
// tenant_id，由 GORM 租户隔离 callback 盖章（CS-05 口径，同观测看板仓储）。
type IntentRouteDailyRepository interface {
	// UpsertDiff 增量累加一行：delta 的计数列当作「本轮增量」
	// （ON CONFLICT 五列 PK 累加，先例 msg_hourly_summary）；
	// DB 失败原样返回，调用方 fail-open（不阻断客户回复）。
	UpsertDiff(ctx context.Context, delta *model.IntentRouteDaily) error
	// GetRow 按五列 PK 读单行；未命中返回 (nil, nil)。
	GetRow(ctx context.Context, date, intentType, chainName, toolName string) (*model.IntentRouteDaily, error)
}

type intentRouteDailyRepo struct {
	db *gorm.DB
}

// NewIntentRouteDailyRepositoryWithDB 显式句柄构造（装配层传 db，不回落全局 GetDB）。
func NewIntentRouteDailyRepositoryWithDB(db *gorm.DB) IntentRouteDailyRepository {
	return &intentRouteDailyRepo{db: db}
}

// tenantScope 从 ctx 解析租户；拿不到租户上下文按 DefaultTenantID 收敛
// （fail-closed：后台路径只看默认租户数据，不放大可见面）。
func (r *intentRouteDailyRepo) tenantScope(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

var errIntentRouteDailyDBNotReady = errors.New("intent_route_daily repository: db not ready")

// UpsertDiff 增量累加一行（链级/工具级/纠错事件行共用）。
//
// delta 的计数列语义为「本轮增量」；五列 PK 定位行，存在则计数累加
// （EXCLUDED 增量），不存在则插入。写侧显式 stamps TenantID，
// 不依赖 ctx 租户回调（回调只是第二道防线）。
func (r *intentRouteDailyRepo) UpsertDiff(ctx context.Context, delta *model.IntentRouteDaily) error {
	if r.db == nil {
		return errIntentRouteDailyDBNotReady
	}
	if delta == nil {
		return errors.New("intent_route_daily repository: nil delta")
	}
	now := time.Now()
	return r.db.WithContext(ctx).Table("intent_route_daily").
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "tenant_id"}, {Name: "date"}, {Name: "intent_type"},
				{Name: "chain_name"}, {Name: "tool_name"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"chain_hits":   gorm.Expr("intent_route_daily.chain_hits + EXCLUDED.chain_hits"),
				"chain_misses": gorm.Expr("intent_route_daily.chain_misses + EXCLUDED.chain_misses"),
				"tool_hits":    gorm.Expr("intent_route_daily.tool_hits + EXCLUDED.tool_hits"),
				"tool_skips":   gorm.Expr("intent_route_daily.tool_skips + EXCLUDED.tool_skips"),
				"tool_calls":   gorm.Expr("intent_route_daily.tool_calls + EXCLUDED.tool_calls"),
				"tool_success": gorm.Expr("intent_route_daily.tool_success + EXCLUDED.tool_success"),
				"tool_fail":    gorm.Expr("intent_route_daily.tool_fail + EXCLUDED.tool_fail"),
				"updated_at":   now,
			}),
		}).
		Create(map[string]any{
			"tenant_id":    r.tenantScope(ctx),
			"date":         delta.Date,
			"intent_type":  delta.IntentType,
			"chain_name":   delta.ChainName,
			"tool_name":    delta.ToolName,
			"chain_hits":   delta.ChainHits,
			"chain_misses": delta.ChainMisses,
			"tool_hits":    delta.ToolHits,
			"tool_skips":   delta.ToolSkips,
			"tool_calls":   delta.ToolCalls,
			"tool_success": delta.ToolSuccess,
			"tool_fail":    delta.ToolFail,
			"created_at":   now,
			"updated_at":   now,
		}).Error
}

// GetRow 按五列 PK 读单行；未命中返回 (nil, nil)。
func (r *intentRouteDailyRepo) GetRow(ctx context.Context, date, intentType, chainName, toolName string) (*model.IntentRouteDaily, error) {
	if r.db == nil {
		return nil, errIntentRouteDailyDBNotReady
	}
	var row model.IntentRouteDaily
	err := r.db.WithContext(ctx).Table("intent_route_daily").
		Where("date = ? AND intent_type = ? AND chain_name = ? AND tool_name = ?",
			date, intentType, chainName, toolName).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
