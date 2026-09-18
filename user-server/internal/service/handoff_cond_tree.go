// Package service 转人工条件树（D20，借鉴 Chatwoot Captain AudienceMatcher schema）。
//
// 条件树 JSON schema（深度 ≤3，算子白名单）：
//
//	{"op":"and"|"or", "conditions":[
//	  {"attr":"platform","operator":"eq","value":"wecom"},        // leaf
//	  {"op":"or","conditions":[...]}                              // group（嵌套 ≤3 层）
//	]}
//
// 硬约束（DNC/紧急词/AI 连续上限）保持代码级，不进条件树——可编排≠LLM/配置决定一切，
// 合规底线不可被运营关闭（D20 设计红线）。
package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	condTreeMaxDepth = 3
)

var conditionOperators = map[string]func(actual, value any) bool{
	"eq":  func(a, v any) bool { return fmt.Sprint(a) == fmt.Sprint(v) },
	"neq": func(a, v any) bool { return fmt.Sprint(a) != fmt.Sprint(v) },
	"in": func(a, v any) bool {
		list, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range list {
			if fmt.Sprint(item) == fmt.Sprint(a) {
				return true
			}
		}
		return false
	},
	"contains": func(a, v any) bool { return strings.Contains(fmt.Sprint(a), fmt.Sprint(v)) },
	"gt":       condCompare,
	"lt":       func(a, v any) bool { return !condCompare(a, v) && fmt.Sprint(a) != fmt.Sprint(v) },
}

func condCompare(a, v any) bool {
	af, aok := condToFloat(a)
	vf, vok := condToFloat(v)
	if aok && vok {
		return af > vf
	}
	return fmt.Sprint(a) > fmt.Sprint(v)
}

func condToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(n), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// CondNode 条件树节点（group 或 leaf 二选一）
type CondNode struct {
	Op         string      `json:"op,omitempty"`
	Conditions []*CondNode `json:"conditions,omitempty"`
	Attr       string      `json:"attr,omitempty"`
	Operator   string      `json:"operator,omitempty"`
	Value      any         `json:"value,omitempty"`
}

// ParseCondTree 解析并校验条件树 JSON（深度/算子白名单）
func ParseCondTree(raw string) (*CondNode, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var root CondNode
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("cond tree parse: %w", err)
	}
	if err := validateCondNode(&root, 1); err != nil {
		return nil, err
	}
	return &root, nil
}

func validateCondNode(n *CondNode, depth int) error {
	if depth > condTreeMaxDepth {
		return fmt.Errorf("cond tree depth %d exceeds max %d", depth, condTreeMaxDepth)
	}
	if n == nil {
		return fmt.Errorf("cond node nil")
	}
	if n.Op != "" {
		if n.Op != "and" && n.Op != "or" {
			return fmt.Errorf("invalid group op: %s", n.Op)
		}
		if len(n.Conditions) == 0 {
			return fmt.Errorf("group node requires conditions")
		}
		for _, c := range n.Conditions {
			if err := validateCondNode(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if n.Attr == "" {
		return fmt.Errorf("leaf node requires attr")
	}
	if _, ok := conditionOperators[n.Operator]; !ok {
		return fmt.Errorf("invalid operator: %s", n.Operator)
	}
	return nil
}

// Evaluate 求值：attrs 为会话/客户字段快照（缺 attr = false，宽松安全）
func (n *CondNode) Evaluate(attrs map[string]any) bool {
	if n == nil {
		return false
	}
	if n.Op != "" {
		if n.Op == "and" {
			for _, c := range n.Conditions {
				if !c.Evaluate(attrs) {
					return false
				}
			}
			return true
		}
		for _, c := range n.Conditions {
			if c.Evaluate(attrs) {
				return true
			}
		}
		return false
	}

	fn, ok := conditionOperators[n.Operator]
	if !ok {
		return false
	}
	actual, exists := attrs[n.Attr]
	if !exists {
		return false
	}
	return fn(actual, n.Value)
}
