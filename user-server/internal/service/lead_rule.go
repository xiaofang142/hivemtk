package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

// ---- LM-07：发掘有效配置统一 + 一次分析冻结快照 ----

// LeadMiningConfigSnapshot 一次分析冻结的有效配置。
// gate/三路/ReAct/判定/持久化必须共同消费同一个 Version，避免配置切换时混用版本。
type LeadMiningConfigSnapshot struct {
	Version         string
	Enabled         bool
	RuleScope       string
	RuleID          int64
	RuleDescription string
	Keywords        []string
	SemanticWords   []string
	Requirement     string
	MinIntentScore  int
	Model           string
	LeadAgentID     uint
	LeadAgentMode   string
	LeadAgentPrompt string
}

// Matches 判断两份快照是否为同一冻结版本（按 Version 精确比对）。
func (s *LeadMiningConfigSnapshot) Matches(other *LeadMiningConfigSnapshot) bool {
	if s == nil || other == nil {
		return false
	}
	return s.Version == other.Version && s.Version != ""
}

// Consume 返回指定消费者应使用的版本；不同消费者读同一快照即得同一版本。
// 允许 gate/detection/judge/persistence 四个消费者，未知消费者返回空串（fail-closed）。
func (s *LeadMiningConfigSnapshot) Consume(consumer string) string {
	if s == nil {
		return ""
	}
	switch consumer {
	case "gate", "detection", "judge", "persistence":
		return s.Version
	default:
		return ""
	}
}

// LeadMiningRuleResolver 规则读取口（ LeadRuleRepository 的子集，便于替身）。
type LeadMiningRuleResolver interface {
	GetByScope(ctx context.Context, scope string, scopeID uint) (*model.LeadMiningRule, error)
}

// LeadMiningConfigReader legacy 单例配置读取口。
type LeadMiningConfigReader interface {
	GetSingleton(ctx context.Context) (*model.LeadMiningConfig, error)
}

// LeadMiningAgentReader 线索 Agent 配置读取口。
type LeadMiningAgentReader interface {
	Get(ctx context.Context, id uint) (*model.AIAgent, error)
}

// LeadMiningConfigResolver 统一旧新有效配置：
//   - 三粒度规则按 account > channel > global 继承，命中禁用继续向上，全部关闭则禁用；
//   - legacy 单例配置提供阈值/模型/表单要求等默认值，Enabled=false 时不得被规则反向开启；
//   - 绑定 Agent 提供模型/提示词/状态；停用或失效时快照禁用（fail-closed）。
type LeadMiningConfigResolver struct {
	rules  LeadMiningRuleResolver
	config LeadMiningConfigReader
	agent  LeadMiningAgentReader
}

// SetAgentReader 注入线索 Agent 配置源（装配点/测试用）。
func (r *LeadMiningConfigResolver) SetAgentReader(agent LeadMiningAgentReader) {
	if r != nil {
		r.agent = agent
	}
}

func NewLeadMiningConfigResolver() *LeadMiningConfigResolver {
	return &LeadMiningConfigResolver{
		rules:  repository.NewLeadMiningRuleRepository(),
		config: repository.NewLeadMiningConfigRepository(),
		agent:  NewLeadMiningAgentServiceWithRepo(repository.NewAIAgentRepository()),
	}
}

func NewLeadMiningConfigResolverWithDeps(rules LeadMiningRuleResolver, config LeadMiningConfigReader) *LeadMiningConfigResolver {
	return &LeadMiningConfigResolver{rules: rules, config: config}
}

// Resolve 冻结一份有效配置快照。任何查询/解析错误都返回错误且不返回可放行快照。
func (r *LeadMiningConfigResolver) Resolve(ctx context.Context, target LeadRuleTarget, agentID ...uint) (*LeadMiningConfigSnapshot, error) {
	if r == nil {
		return nil, ErrLeadRuleNotFound
	}
	var cfg *model.LeadMiningConfig
	if r.config != nil {
		var err error
		cfg, err = r.config.GetSingleton(ctx)
		if err != nil {
			return nil, err
		}
	}
	snap := &LeadMiningConfigSnapshot{}
	if cfg != nil {
		snap.Enabled = cfg.Enabled
		snap.MinIntentScore = cfg.MinIntentScore
		snap.Model = cfg.Model
		snap.Requirement = cfg.Requirement
	}
	if snap.MinIntentScore <= 0 {
		snap.MinIntentScore = 50
	}

	// 规则继承：禁用继续向上；查询错误直接拒绝（不静默放大为全局启用）。
	rule, foundAny, err := resolveInheritedRule(ctx, r.rules, target)
	if err != nil {
		return nil, err
	}
	if rule != nil {
		snap.RuleScope, snap.RuleID = rule.Scope, rule.ID
		snap.RuleDescription = rule.SystemDescription
		snap.Keywords = append(snap.Keywords, []string(rule.Keywords)...)
		snap.SemanticWords = append(snap.SemanticWords, []string(rule.SemanticKeywords)...)
		// 旧配置与规则同为有效配置源：显式关闭的 legacy 不得被规则反向开启。
		snap.Enabled = rule.Enabled && snap.Enabled
	} else {
		// 所有粒度均命中禁用行：不能退回 legacy 放行（关闭语义必须生效）。
		if foundAny {
			snap.Enabled = false
		}
		if cfg != nil && len(cfg.Keywords) > 0 {
			snap.Keywords = append(snap.Keywords, []string(cfg.Keywords)...)
		}
	}

	if len(agentID) > 0 && agentID[0] > 0 && r.agent != nil {
		a, err := r.agent.Get(ctx, agentID[0])
		if err != nil {
			return nil, err
		}
		if a == nil || a.Status != 1 || !strings.EqualFold(a.AgentType, string(model.AgentTypeLeadMining)) {
			snap.Enabled = false
		} else {
			snap.LeadAgentID = a.ID
			snap.LeadAgentMode = a.AgentMode
			snap.LeadAgentPrompt = a.SystemPrompt
			if strings.TrimSpace(a.LLMModel) != "" {
				snap.Model = a.LLMModel
			}
		}
	}
	snap.Version = fingerprintLeadMiningSnapshot(snap)
	return snap, nil
}

// resolveInheritedRule 按 account > channel > global 逐级尝试，命中启用即返回；禁用继续继承。
func resolveInheritedRule(ctx context.Context, repo LeadMiningRuleResolver, target LeadRuleTarget) (*model.LeadMiningRule, bool, error) {
	if repo == nil {
		return nil, false, ErrLeadRuleNotFound
	}
	foundAny := false
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
		rule, err := repo.GetByScope(ctx, c.scope, c.scopeID)
		if err != nil {
			return nil, foundAny, err
		}
		if rule != nil {
			foundAny = true
			if rule.Enabled {
				return rule, foundAny, nil
			}
		}
	}
	return nil, foundAny, nil
}

// fingerprintLeadMiningSnapshot 对冻结快照的全部执行字段生成版本指纹。
func fingerprintLeadMiningSnapshot(s *LeadMiningConfigSnapshot) string {
	parts := []string{
		strconv.FormatBool(s.Enabled), s.RuleScope, strconv.FormatInt(s.RuleID, 10), s.RuleDescription,
		strings.Join(s.Keywords, ","), strings.Join(s.SemanticWords, ","), s.Requirement,
		strconv.Itoa(s.MinIntentScore), s.Model, strconv.FormatUint(uint64(s.LeadAgentID), 10),
		s.LeadAgentMode, s.LeadAgentPrompt,
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("lm-%s", hex.EncodeToString(h[:8]))
}
