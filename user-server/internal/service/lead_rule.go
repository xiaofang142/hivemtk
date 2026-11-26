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

// LeadRuleTarget 规则解析的目标对象。account 优先级最高，channel 次之；
// 没有全局兜底——两者均无启用规则即 fail-closed（ErrLeadRuleNotFound）。
type LeadRuleTarget struct {
	ChannelAccountID uint
	ChannelID        uint
}

// ErrLeadRuleNotFound 账号/渠道两级均无可用启用规则：fail-closed，
// 调用方不得自行生成默认规则，也不得回退 legacy 单例配置或全局规则。
var ErrLeadRuleNotFound = errors.New("未找到适用的线索发掘规则")

// LeadRuleService 两粒度规则解析（FR-011）。
//
// 与 Phase 2 的 ChannelResolver 同口径：租户上下文缺失时仓储层拒绝访问；
// 命中禁用规则继续向上回退，account → channel 逐级尝试，
// 全无命中返回 ErrLeadRuleNotFound，绝不返回零值规则或全局兜底导致静默放行。
type LeadRuleService struct {
	repo repository.LeadMiningRuleRepository
}

func NewLeadRuleService() *LeadRuleService {
	return &LeadRuleService{repo: repository.NewLeadMiningRuleRepository()}
}

func NewLeadRuleServiceWithRepo(repo repository.LeadMiningRuleRepository) *LeadRuleService {
	return &LeadRuleService{repo: repo}
}

// Resolve 按 account > channel 的 scope 优先级返回第一条 enabled 规则；
// 两级都没有启用规则时返回 ErrLeadRuleNotFound（不再回退 global）。
func (s *LeadRuleService) Resolve(ctx context.Context, target LeadRuleTarget) (*model.LeadMiningRule, error) {
	if s == nil || s.repo == nil {
		return nil, ErrLeadRuleNotFound
	}
	return resolveRuleForTarget(ctx, s.repo, target)
}

// List 配置人员读取当前租户全部规则（管理端展示）。
func (s *LeadRuleService) List(ctx context.Context) ([]model.LeadMiningRule, error) {
	if s == nil || s.repo == nil {
		return nil, ErrLeadRuleNotFound
	}
	return s.repo.List(ctx)
}

// Save 新增或更新规则；scope 仅接受 channel/account 两粒度白名单。
// global 已随「去掉全局默认兜底」下线：写入直接拒绝，库里存量 global 行仅作历史数据保留、不参与解析。
func (s *LeadRuleService) Save(ctx context.Context, m *model.LeadMiningRule) error {
	if m == nil {
		return errors.New("规则为空")
	}
	m.Scope = strings.ToLower(strings.TrimSpace(m.Scope))
	switch m.Scope {
	case "channel", "account":
		if m.ScopeID == 0 {
			return errors.New("channel/account 粒度必须指定 scope_id")
		}
	default:
		return errors.New("scope 仅支持 channel/account；全局兜底已移除")
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
	LeadAgentID      uint
	LeadAgentMode    string
	LeadAgentPersona string
	LeadAgentPrompt  string
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

// LeadMiningConfigResolver 统一有效配置：
//   - account > channel 两级规则是发掘开关/关键词/描述的唯一权威源，命中禁用继续向上，
//     两级均无启用规则即 ErrLeadRuleNotFound（fail-closed，不回退 global，也不拿 legacy 关键词兜底）；
//   - legacy 单例配置只提供阈值/模型/表单要求等参数默认值（非规则兜底，属租户/安全语义保留项）；
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
		// 只读参数默认值；cfg.Enabled 与 cfg.Keywords 不再是规则/开关兜底。
		snap.MinIntentScore = cfg.MinIntentScore
		snap.Model = cfg.Model
		snap.Requirement = cfg.Requirement
	}
	if snap.MinIntentScore <= 0 {
		snap.MinIntentScore = 50
	}

	// 规则继承：account > channel，命中禁用继续向上；无启用规则直接 fail-closed。
	// 查询错误同样直接拒绝，不静默放大为全局启用。
	rule, err := resolveRuleForTarget(ctx, r.rules, target)
	if err != nil {
		return nil, err
	}
	snap.RuleScope, snap.RuleID = rule.Scope, rule.ID
	snap.RuleDescription = rule.SystemDescription
	snap.Keywords = append(snap.Keywords, []string(rule.Keywords)...)
	snap.SemanticWords = append(snap.SemanticWords, []string(rule.SemanticKeywords)...)
	// 规则是唯一开关源：legacy cfg.Enabled 不得再与规则做 AND（无规则时上面已拒绝）。
	snap.Enabled = rule.Enabled

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
			snap.LeadAgentPersona = a.Persona
			snap.LeadAgentPrompt = a.SystemPrompt
			if strings.TrimSpace(a.LLMModel) != "" {
				snap.Model = a.LLMModel
			}
		}
	}
	snap.Version = fingerprintLeadMiningSnapshot(snap)
	return snap, nil
}

// resolveRuleForTarget 是 LeadRuleService.Resolve 与冻结快照解析器共用的唯一规则读取口：
// 按 account > channel 逐级尝试，命中启用即返回；禁用继续向上继承；
// 两级都没有启用规则返回 ErrLeadRuleNotFound，不存在 global 或 legacy 关键词兜底。
func resolveRuleForTarget(ctx context.Context, repo LeadMiningRuleResolver, target LeadRuleTarget) (*model.LeadMiningRule, error) {
	if repo == nil {
		return nil, ErrLeadRuleNotFound
	}
	candidates := make([]struct {
		scope   string
		scopeID uint
	}, 0, 2)
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
	for _, c := range candidates {
		rule, err := repo.GetByScope(ctx, c.scope, c.scopeID)
		if err != nil {
			return nil, err
		}
		if rule != nil && rule.Enabled {
			return rule, nil
		}
	}
	return nil, ErrLeadRuleNotFound
}

// fingerprintLeadMiningSnapshot 对冻结快照的全部执行字段生成版本指纹。
func fingerprintLeadMiningSnapshot(s *LeadMiningConfigSnapshot) string {
	parts := []string{
		strconv.FormatBool(s.Enabled), s.RuleScope, strconv.FormatInt(s.RuleID, 10), s.RuleDescription,
		strings.Join(s.Keywords, ","), strings.Join(s.SemanticWords, ","), s.Requirement,
		strconv.Itoa(s.MinIntentScore), s.Model, strconv.FormatUint(uint64(s.LeadAgentID), 10),
		s.LeadAgentMode, s.LeadAgentPersona, s.LeadAgentPrompt,
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("lm-%s", hex.EncodeToString(h[:8]))
}
