package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LeadRuleTarget 规则解析的目标对象。account 优先级最高，channel 次之，global 兜底。
type LeadRuleTarget struct {
	ChannelAccountID uint
	ChannelID        uint
}

// ErrLeadRuleNotFound 三粒度均未配置可用规则：fail-closed，调用方不得自行生成默认规则。
var ErrLeadRuleNotFound = errors.New("未找到适用的线索发掘规则")

// LeadRuleService 三粒度规则解析（FR-011）。
//
// 与 Phase 2 的 ChannelResolver 同口径：租户上下文缺失时仓储层拒绝访问；
// 命中禁用规则继续向上回退，account → channel → global 逐级尝试，
// 全无命中返回 ErrLeadRuleNotFound，绝不返回零值规则导致静默放行。
type LeadRuleService struct {
	repo repository.LeadMiningRuleRepository
}

func NewLeadRuleService() *LeadRuleService {
	return &LeadRuleService{repo: repository.NewLeadMiningRuleRepository()}
}

func NewLeadRuleServiceWithRepo(repo repository.LeadMiningRuleRepository) *LeadRuleService {
	return &LeadRuleService{repo: repo}
}

// Resolve 按 account > channel > global 的 scope 优先级返回第一条 enabled 规则。
func (s *LeadRuleService) Resolve(ctx context.Context, target LeadRuleTarget) (*model.LeadMiningRule, error) {
	if s == nil || s.repo == nil {
		return nil, ErrLeadRuleNotFound
	}
	candidates := make([]struct {
		scope   string
		scopeID uint
	}, 0, 3)
	if target.ChannelAccountID > 0 {
		candidates = append(candidates, struct {
			scope   string
			scopeID uint
		}{"account", target.ChannelAccountID})
	}
	if target.ChannelID > 0 {
		candidates = append(candidates, struct {
			scope   string
			scopeID uint
		}{"channel", target.ChannelID})
	}
	candidates = append(candidates, struct {
		scope   string
		scopeID uint
	}{"global", 0})

	for _, c := range candidates {
		rule, err := s.repo.GetByScope(ctx, c.scope, c.scopeID)
		if err != nil {
			return nil, err
		}
		if rule != nil && rule.Enabled {
			return rule, nil
		}
	}
	return nil, ErrLeadRuleNotFound
}

// List 配置人员读取当前租户全部规则（管理端展示）。
func (s *LeadRuleService) List(ctx context.Context) ([]model.LeadMiningRule, error) {
	if s == nil || s.repo == nil {
		return nil, ErrLeadRuleNotFound
	}
	return s.repo.List(ctx)
}

// Save 新增或更新规则；scope 仅接受三粒度白名单。
func (s *LeadRuleService) Save(ctx context.Context, m *model.LeadMiningRule) error {
	if m == nil {
		return errors.New("规则为空")
	}
	m.Scope = strings.ToLower(strings.TrimSpace(m.Scope))
	switch m.Scope {
	case "global":
		m.ScopeID = 0
	case "channel", "account":
		if m.ScopeID == 0 {
			return errors.New("channel/account 粒度必须指定 scope_id")
		}
	default:
		return errors.New("scope 仅支持 global/channel/account")
	}
	if s == nil || s.repo == nil {
		return ErrLeadRuleNotFound
	}
	if m.ID > 0 {
		return s.repo.Update(ctx, m.ID, map[string]any{
			"scope": m.Scope, "scope_id": m.ScopeID,
			"keywords": m.Keywords, "semantic_keywords": m.SemanticKeywords,
			"system_description": m.SystemDescription, "enabled": m.Enabled,
		})
	}
	return s.repo.Create(ctx, m)
}

// SetEnabled 启停规则。
func (s *LeadRuleService) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	if s == nil || s.repo == nil {
		return ErrLeadRuleNotFound
	}
	return s.repo.Update(ctx, id, map[string]any{"enabled": enabled})
}

// Delete 删除规则。
func (s *LeadRuleService) Delete(ctx context.Context, id int64) error {
	if s == nil || s.repo == nil {
		return ErrLeadRuleNotFound
	}
	return s.repo.Delete(ctx, id)
}
