package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// OneIDMergeRuleService OneID 合并规则服务
//
// 负责：规则的 CRUD、按优先级排序、与 CustomerIdentity 协同执行合并
type OneIDMergeRuleService struct {
	mu    sync.RWMutex
	rules *MergeRuleSet
}

var oneIDMergeRuleSingleton = &OneIDMergeRuleService{rules: defaultRuleSet()}

// NewOneIDMergeRuleService 返回包级单例（进程内共享同一规则集；
// 规则设计为内存态配置，重启恢复默认，GET/SAVE/DELETE 必须操作同一实例）
func NewOneIDMergeRuleService() *OneIDMergeRuleService {
	return oneIDMergeRuleSingleton
}

// MergeRule 合并规则
type MergeRule struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Field       string `json:"field"`
	Op          string `json:"op"`
	Value       string `json:"value"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
	BuiltIn     bool   `json:"built_in"`
	UpdatedAt   string `json:"updated_at"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// MergeStrategy 合并策略
type MergeStrategy struct {
	PrimaryRule      string   `json:"primary_rule"`
	ConflictBehavior string   `json:"conflict_behavior"`
	WindowStart      string   `json:"window_start"`
	WindowEnd        string   `json:"window_end"`
	PostMergeActions []string `json:"post_merge_actions"`
}

// MergeRuleSet 完整规则集
type MergeRuleSet struct {
	BuiltIn   []MergeRule   `json:"built_in"`
	Custom    []MergeRule   `json:"custom"`
	Strategy  MergeStrategy `json:"strategy"`
	UpdatedAt string        `json:"updated_at"`
}

// GetRules godoc
// @Summary      获取 OneID 合并规则集
// @Description  返回预置规则 + 自定义规则 + 合并策略
// @Tags         OneID
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  MergeRuleSet
// @Router       /api/oneid/merge-rules [get]
func (s *OneIDMergeRuleService) GetRules(ctx context.Context) (*MergeRuleSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := *s.rules
	return &out, nil
}

// SaveRules godoc
// @Summary      保存 OneID 合并规则集
// @Description  全量替换当前规则集；预置规则只允许切换 enabled / 调整 priority
// @Tags         OneID
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  MergeRuleSet  true  "规则集"
// @Success      200   {object}  MergeRuleSet
// @Failure      400   {object}  response.Response
// @Router       /api/oneid/merge-rules [post]
func (s *OneIDMergeRuleService) SaveRules(ctx context.Context, set *MergeRuleSet) (*MergeRuleSet, error) {
	if set == nil {
		return nil, errors.New("规则集不能为空")
	}
	if err := validateRuleSet(set); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.rules = set
	return set, nil
}

// DeleteRule 按 ID 删除规则；内置规则不允许删除，自定义规则从集合中移除
func (s *OneIDMergeRuleService) DeleteRule(ctx context.Context, id int64) (*MergeRuleSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rules.BuiltIn {
		if r.ID == id {
			return nil, fmt.Errorf("内置规则不允许删除，仅可停用")
		}
	}
	found := false
	custom := make([]MergeRule, 0, len(s.rules.Custom))
	for _, r := range s.rules.Custom {
		if r.ID == id {
			found = true
			continue
		}
		custom = append(custom, r)
	}
	if !found {
		return nil, fmt.Errorf("规则 %d 不存在", id)
	}
	s.rules.Custom = custom
	out := *s.rules
	return &out, nil
}

// ApplyRules 应用规则到一对候选客户（供 CustomerIdentity.MergeCustomers 调用）
//
// 返回 true 表示应该合并，false 表示跳过
func (s *OneIDMergeRuleService) ApplyRules(ctx context.Context, a, b map[string]string) (bool, string, error) {
	s.mu.RLock()
	rules := s.rules
	s.mu.RUnlock()

	for _, r := range rules.BuiltIn {
		if !r.Enabled {
			continue
		}
		va, oka := a[r.Field]
		vb, okb := b[r.Field]
		if !oka || !okb || va == "" || vb == "" {
			continue
		}
		if matchRule(va, vb, r.Op) {
			return true, r.Name, nil
		}
	}

	for _, r := range rules.Custom {
		if !r.Enabled {
			continue
		}

		va, oka := a[r.Field]
		vb, okb := b[r.Field]
		if !oka || !okb {
			continue
		}
		if matchRule(va, vb, r.Op) {
			return true, r.Name, nil
		}
	}

	return false, "", nil
}

func matchRule(a, b, op string) bool {
	switch op {
	case "eq":
		return a == b
	case "prefix":
		plen := 7
		if len(a) < plen || len(b) < plen {
			return false
		}
		return a[:plen] == b[:plen]
	case "like":

		return containsSubstr(a, b) || containsSubstr(b, a)
	}
	return false
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func validateRuleSet(set *MergeRuleSet) error {
	if len(set.BuiltIn) == 0 && len(set.Custom) == 0 {
		return errors.New("至少需要 1 条规则")
	}
	for i, r := range set.Custom {
		if r.Name == "" {
			return errors.New("custom rule #" + strconv.Itoa(i) + " 缺 name")
		}
		if r.Field == "" {
			return errors.New("custom rule " + r.Name + " 缺 field")
		}
	}
	switch set.Strategy.PrimaryRule {
	case "latest_active", "most_orders", "manual":
	default:
		return errors.New("primary_rule 取值非法")
	}
	switch set.Strategy.ConflictBehavior {
	case "auto_merge", "queue_review", "skip":
	default:
		return errors.New("conflict_behavior 取值非法")
	}
	return nil
}

func itoaMerge(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func defaultRuleSet() *MergeRuleSet {
	now := time.Now().UTC().Format(time.RFC3339)
	return &MergeRuleSet{
		BuiltIn: []MergeRule{
			{ID: 1, Name: "同手机号合并", Field: "phone", Op: "eq", Priority: 100, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "相同手机号必合并", Example: "13800001111 == 13800001111"},
			{ID: 2, Name: "同 UnionID 合并", Field: "unionid", Op: "eq", Priority: 95, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "微信开放平台 UnionID", Example: "o6_bm123 == o6_bm123"},
			{ID: 3, Name: "同邮箱合并", Field: "email", Op: "eq", Priority: 90, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "相同邮箱必合并", Example: "a@x.com == a@x.com"},
			{ID: 4, Name: "同 OpenID 合并", Field: "openid", Op: "eq", Priority: 80, Enabled: false, BuiltIn: true, UpdatedAt: now, Description: "单应用内 OpenID", Example: "同一公众号 OpenID"},
			{ID: 5, Name: "同外部 ID 合并", Field: "external_id", Op: "eq", Priority: 70, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "CRM 外部系统 ID", Example: "salesforce_001 == sfdc_001"},
			{ID: 6, Name: "手机号前 7 位合并", Field: "phone", Op: "prefix", Priority: 50, Enabled: false, BuiltIn: true, UpdatedAt: now, Description: "易换号场景兜底", Example: "1380000**** == 1380000****"},
		},
		Custom: []MergeRule{},
		Strategy: MergeStrategy{
			PrimaryRule:      "latest_active",
			ConflictBehavior: "queue_review",
			WindowStart:      "02:00",
			WindowEnd:        "05:00",
			PostMergeActions: []string{"unify_tag", "write_audit"},
		},
		UpdatedAt: now,
	}
}
