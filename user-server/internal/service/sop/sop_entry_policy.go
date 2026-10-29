package sop

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

const (
	SOPEntryModeOnce     = "once"
	SOPEntryModeCooldown = "cooldown"
	SOPEntryModeAlways   = "always"
)

// S1-3 错误分类（executed_nodes JSONB 元素 error_class 字段）
const (
	SOPErrorClassTransient = "transient"
	SOPErrorClassPermanent = "permanent"
)

// 事件类型：goal_exit 达成退出 / max_wait 兜底跳过
const (
	NodeEventGoalAchieved = "goal_achieved"
	NodeEventSkipped      = "skipped"
)

// ErrSOPEntrySuppressed entry_policy 拦截重复进入
var ErrSOPEntrySuppressed = errors.New("sop entry suppressed by entry_policy")

// SOPEntryPolicy SOP 重入控制策略
type SOPEntryPolicy struct {
	Mode         string `json:"mode"`
	CooldownDays int    `json:"cooldown_days,omitempty"`
	GoalExit     string `json:"goal_exit,omitempty"`
}

// DefaultSOPEntryPolicy 缺省策略：once（与 RT-2 契约一致）
func DefaultSOPEntryPolicy() SOPEntryPolicy {
	return SOPEntryPolicy{Mode: SOPEntryModeOnce}
}

// ParseSOPEntryPolicy 从 trigger_config.entry_policy 解析策略。
// 接受 map 或 JSON 字符串两种形态；缺省/非法 mode 回退 once。
func ParseSOPEntryPolicy(cfg model.JSONMap) SOPEntryPolicy {
	policy := DefaultSOPEntryPolicy()
	if cfg == nil {
		return policy
	}
	raw, ok := cfg["entry_policy"]
	if !ok || raw == nil {
		return policy
	}
	var m map[string]any
	switch v := raw.(type) {
	case map[string]any:
		m = v
	case string:
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return policy
		}
	default:
		return policy
	}
	if s, ok := m["mode"].(string); ok && s != "" {
		switch s {
		case SOPEntryModeOnce, SOPEntryModeCooldown, SOPEntryModeAlways:
			policy.Mode = s
		default:
			policy.Mode = SOPEntryModeOnce
		}
	}
	if f, ok := m["cooldown_days"].(float64); ok && f > 0 {
		policy.CooldownDays = int(f)
	} else if i, ok := m["cooldown_days"].(int); ok && i > 0 {
		policy.CooldownDays = i
	}
	if s, ok := m["goal_exit"].(string); ok {
		policy.GoalExit = strings.TrimSpace(s)
	}
	return policy
}

// ValidateSOPEntryPolicy 校验策略合法性（Create/Update 时调用）
func ValidateSOPEntryPolicy(p SOPEntryPolicy) error {
	switch p.Mode {
	case SOPEntryModeOnce:
	case SOPEntryModeCooldown:
		if p.CooldownDays <= 0 {
			return fmt.Errorf("entry_policy.mode=cooldown 时 cooldown_days 必须为正整数")
		}
	case SOPEntryModeAlways:
	default:
		return fmt.Errorf("entry_policy.mode 非法：%q（支持 once|cooldown|always）", p.Mode)
	}
	return nil
}

// ValidateSOPTriggerConfigEntryPolicy 校验 trigger_config 中内嵌的 entry_policy
func ValidateSOPTriggerConfigEntryPolicy(cfg model.JSONMap) error {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["entry_policy"]
	if !ok || raw == nil {
		return nil
	}
	return ValidateSOPEntryPolicy(ParseSOPEntryPolicy(cfg))
}

func goalExitAchieved(expr string, data model.JSONMap) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	matched, err := SOPEvaluateCompoundCondition(expr, data)
	if err != nil {
		return false
	}
	return matched
}

func (p SOPEntryPolicy) cooldownWindow() time.Duration {
	if p.CooldownDays <= 0 {
		return 0
	}
	return time.Duration(p.CooldownDays) * 24 * time.Hour
}
