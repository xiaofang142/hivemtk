package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// AssignmentStrategy 分配策略
type AssignmentStrategy string

const (
	StrategyRoundRobin AssignmentStrategy = "round_robin"
	StrategyLeastBusy  AssignmentStrategy = "least_busy"
	StrategySkillMatch AssignmentStrategy = "skill_match"
	StrategyOwnerRoute AssignmentStrategy = "owner_route"
	StrategyManual     AssignmentStrategy = "manual"
)

var (
	// ErrManualAssignNotAllowed manual 策略不走自动分配——明确指引调用方使用指定坐席接口
	ErrManualAssignNotAllowed = errors.New("手动分配请用指定坐席接口（action=assign + to_user_id）")
	// ErrUnknownAssignStrategy 非法策略名
	ErrUnknownAssignStrategy = errors.New("不支持的分配策略")
)

// SupportedAutoAssignStrategies 支持的自动分配策略集合（含空串=默认 least_busy）
var SupportedAutoAssignStrategies = map[AssignmentStrategy]struct{}{
	"":                 {},
	StrategyLeastBusy:  {},
	StrategySkillMatch: {},
	StrategyOwnerRoute: {},
	StrategyRoundRobin: {},
}

// ResolveAutoAssignMode 校验自动分配模式（M5）：
//   - round_robin → 返回 StrategyRoundRobin
//   - ""/least_busy/skill_match/owner_route → 返回 StrategyLeastBusy（默认自动算法）
//   - manual → 返回 ErrManualAssignNotAllowed（调用方应转 HTTP 400）
//   - 其他 → 返回 wrapped ErrUnknownAssignStrategy（调用方应转 HTTP 400）
func ResolveAutoAssignMode(mode string) (AssignmentStrategy, error) {
	s := AssignmentStrategy(strings.TrimSpace(mode))
	if s == StrategyManual {
		return "", ErrManualAssignNotAllowed
	}
	if _, ok := SupportedAutoAssignStrategies[s]; !ok {
		return "", fmt.Errorf("%w: %q（支持 auto/least_busy/skill_match/owner_route/round_robin）", ErrUnknownAssignStrategy, mode)
	}
	if s == StrategyRoundRobin {
		return s, nil
	}
	return StrategyLeastBusy, nil
}

type AssignmentService struct {
	customerRepo repository.CustomerRepository
}

func NewAssignmentService(db *gorm.DB) *AssignmentService {
	return &AssignmentService{customerRepo: repository.NewCustomerRepository()}
}

// AgentInfo 坐席信息
type AgentInfo struct {
	AgentID    uint
	AgentName  string
	Skills     []string
	Online     bool
	ActiveSess int
	Capacity   int
	LastAssign time.Time
}

// AssignmentDecision 分配决策
type AssignmentDecision struct {
	AgentID   uint    `json:"agent_id"`
	AgentName string  `json:"agent_name"`
	Strategy  string  `json:"strategy"`
	RuleID    uint    `json:"rule_id"`
	Reason    string  `json:"reason"`
	Score     float64 `json:"score"`
}

func (s *AssignmentService) Assign(ctx context.Context, sess *model.CustomerSession, candidates []AgentInfo) (*AssignmentDecision, error) {
	ownerID := s.resolveOwnerAgentID(ctx, sess)
	return s.AssignWithOwner(ctx, sess, candidates, ownerID)
}

// AssignWithOwner 带专属坐席的分配入口（ownerID<=0 时行为与原算法完全一致）。
// 独立导出便于单测注入 owner 而无需 DB。
func (s *AssignmentService) AssignWithOwner(ctx context.Context, sess *model.CustomerSession, candidates []AgentInfo, ownerID uint) (*AssignmentDecision, error) {
	if len(candidates) == 0 {
		return nil, errors.New("无可用坐席")
	}

	available := make([]AgentInfo, 0, len(candidates))
	for _, a := range candidates {
		if !a.Online {
			continue
		}
		if a.Capacity > 0 && a.ActiveSess >= a.Capacity {
			continue
		}
		available = append(available, a)
	}
	if len(available) == 0 {
		return nil, errors.New("所有坐席均已满载或离线")
	}

	if ownerID > 0 {
		for _, a := range available {
			if a.AgentID == ownerID {
				return &AssignmentDecision{
					AgentID:   a.AgentID,
					AgentName: a.AgentName,
					Strategy:  string(StrategyOwnerRoute),
					Reason:    fmt.Sprintf("owner_agent: %d/%d", a.ActiveSess, a.Capacity),
					Score:     1,
				}, nil
			}
		}

	}

	needSkills := extractSessionSkills(sess)
	if len(needSkills) > 0 {
		skillMatched := filterBySkills(available, needSkills)
		if len(skillMatched) > 0 {
			available = skillMatched
		}
	}

	sort.SliceStable(available, func(i, j int) bool {

		if available[i].ActiveSess != available[j].ActiveSess {
			return available[i].ActiveSess < available[j].ActiveSess
		}

		return available[i].LastAssign.Before(available[j].LastAssign)
	})

	best := available[0]
	strategy := StrategyLeastBusy
	reason := "least_busy"
	if len(needSkills) > 0 {
		strategy = StrategySkillMatch
		reason = "skill_match"
	}

	return &AssignmentDecision{
		AgentID:   best.AgentID,
		AgentName: best.AgentName,
		Strategy:  string(strategy),
		Reason:    fmt.Sprintf("%s: %d/%d", reason, best.ActiveSess, best.Capacity),
		Score:     float64(best.Capacity-best.ActiveSess) / float64(best.Capacity+1),
	}, nil
}

// AssignWithStrategy 按显式策略分配坐席（M5：策略不再静默忽略）
//
//   - round_robin：按 LastAssign 最早优先轮转
//   - least_busy/skill_match/owner_route/""：走默认算法（owner 定向 → 技能匹配 → 最闲优先）
//   - manual：返回 ErrManualAssignNotAllowed
//   - 其他非法策略名：返回 wrapped ErrUnknownAssignStrategy（调用方转 400）
func (s *AssignmentService) AssignWithStrategy(ctx context.Context, sess *model.CustomerSession, candidates []AgentInfo, strategy string) (*AssignmentDecision, error) {
	resolved, err := ResolveAutoAssignMode(strategy)
	if err != nil {
		return nil, err
	}
	if resolved == StrategyRoundRobin {
		return s.assignRoundRobin(sess, candidates)
	}
	return s.Assign(ctx, sess, candidates)
}

func (s *AssignmentService) assignRoundRobin(sess *model.CustomerSession, candidates []AgentInfo) (*AssignmentDecision, error) {
	available := make([]AgentInfo, 0, len(candidates))
	for _, a := range candidates {
		if !a.Online {
			continue
		}
		if a.Capacity > 0 && a.ActiveSess >= a.Capacity {
			continue
		}
		available = append(available, a)
	}
	if len(available) == 0 {
		return nil, errors.New("所有坐席均已满载或离线")
	}

	sort.SliceStable(available, func(i, j int) bool {
		return available[i].LastAssign.Before(available[j].LastAssign)
	})
	best := available[0]
	return &AssignmentDecision{
		AgentID:   best.AgentID,
		AgentName: best.AgentName,
		Strategy:  string(StrategyRoundRobin),
		Reason:    fmt.Sprintf("round_robin: last_assign=%s", best.LastAssign.Format(time.RFC3339)),
		Score:     1,
	}, nil
}

func extractSessionSkills(sess *model.CustomerSession) []string {

	var skills []string
	switch sess.Platform {
	case "wechat", "wecom":
		skills = append(skills, "wechat")
	case "douyin", "kuaishou", "tiktok", "xiaohongshu", "xianyu":
		skills = append(skills, "social_media")
	}
	if sess.Priority >= 3 {
		skills = append(skills, "vip")
	}
	return skills
}

func (s *AssignmentService) resolveOwnerAgentID(ctx context.Context, sess *model.CustomerSession) uint {
	if s == nil || s.customerRepo == nil || sess == nil {
		return 0
	}
	identity := sess.OneID
	if identity == "" {
		identity = sess.UserID
	}
	if identity == "" {
		return 0
	}
	cust, err := s.customerRepo.GetByUnifiedID(ctx, identity)
	if err != nil || cust == nil || cust.OwnerAgentID == nil || *cust.OwnerAgentID == 0 {
		return 0
	}
	return *cust.OwnerAgentID
}

func filterBySkills(agents []AgentInfo, required []string) []AgentInfo {
	reqSet := make(map[string]struct{}, len(required))
	for _, s := range required {
		reqSet[s] = struct{}{}
	}
	out := make([]AgentInfo, 0, len(agents))
	for _, a := range agents {
		hasAll := true
		for r := range reqSet {
			found := false
			for _, s := range a.Skills {
				if s == r {
					found = true
					break
				}
			}
			if !found {
				hasAll = false
				break
			}
		}
		if hasAll {
			out = append(out, a)
		}
	}
	return out
}
