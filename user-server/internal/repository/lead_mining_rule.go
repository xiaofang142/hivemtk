package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrLeadTenantRequired 线索域仓储缺租户上下文：多租户下 fail-closed，拒绝访问。
var ErrLeadTenantRequired = errors.New("线索域访问缺少已确认的租户上下文")

// LeadMiningRuleRepository 三粒度线索规则仓储（每租户多行，主键 id）。
type LeadMiningRuleRepository interface {
	List(ctx context.Context) ([]model.LeadMiningRule, error)
	GetByScope(ctx context.Context, scope string, scopeID uint) (*model.LeadMiningRule, error)
	Create(ctx context.Context, m *model.LeadMiningRule) error
	Update(ctx context.Context, id int64, updates map[string]any) error
	Delete(ctx context.Context, id int64) error
}

type leadMiningRuleRepo struct {
	db *gorm.DB
}

func NewLeadMiningRuleRepository() LeadMiningRuleRepository {
	return &leadMiningRuleRepo{db: _db.GetDB()}
}

func NewLeadMiningRuleRepositoryWithDB(db *gorm.DB) LeadMiningRuleRepository {
	return &leadMiningRuleRepo{db: db}
}

// scoped 返回已绑定租户的查询句柄。缺租户且多租户开启时直接报错（fail-closed），
// 单租户部署回退默认租户，与 lead_mining_config 的 singletonID 口径一致。
func (r *leadMiningRuleRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	if r == nil || r.db == nil {
		return nil, 0, ErrChannelStoreUnavailable
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, 0, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", tid), tid, nil
}

func (r *leadMiningRuleRepo) List(ctx context.Context) ([]model.LeadMiningRule, error) {
	q, _, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var items []model.LeadMiningRule
	err = q.Order("scope ASC, scope_id ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *leadMiningRuleRepo) GetByScope(ctx context.Context, scope string, scopeID uint) (*model.LeadMiningRule, error) {
	q, tid, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var m model.LeadMiningRule
	err = q.Where("scope = ? AND scope_id = ?", scope, scopeID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	_ = tid
	return &m, nil
}

func (r *leadMiningRuleRepo) Create(ctx context.Context, m *model.LeadMiningRule) error {
	_, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	if m == nil {
		return errors.New("规则为空")
	}
	m.TenantID = tid
	// Create 用不带 Where 的句柄：部分 GORM 版本会把作用域条件带进 INSERT。
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *leadMiningRuleRepo) Update(ctx context.Context, id int64, updates map[string]any) error {
	q, _, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	res := q.Model(&model.LeadMiningRule{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *leadMiningRuleRepo) Delete(ctx context.Context, id int64) error {
	q, _, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	return q.Where("id = ?", id).Delete(&model.LeadMiningRule{}).Error
}
