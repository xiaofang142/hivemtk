package sop

import (
	"errors"
	"fmt"
	"strings"

	contentsvc "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/model"
)

// SOPSupportedOperators SOP 支持的运算符集合
var SOPSupportedOperators = map[string]bool{
	"eq": true, "ne": true, "gt": true, "lt": true,
	"gte": true, "lte": true, "contains": true, "in": true,
}

// SOPLogicalOperators SOP 支持的逻辑运算符
const (
	SOPLogicAnd = "AND"
	SOPLogicOr  = "OR"
)

// SOPConditionResult 条件评估结果
type SOPConditionResult struct {
	Matched  bool
	NextNode string
}

// SOPParseCondition 解析单个条件表达式（包装 ParseCondition，方便统一调用）
// 返回 field / operator / value，例如 "intent_score gte 0.7" → ("intent_score", "gte", "0.7")
func SOPParseCondition(condition string) (field, operator, value string, err error) {
	return contentsvc.ParseCondition(condition)
}

// SOPEvaluateOperator 评估单个运算符（包装 EvaluateOperator）
func SOPEvaluateOperator(fieldValue any, operator, value string) (bool, error) {
	return contentsvc.EvaluateOperator(fieldValue, operator, value)
}

// SOPEvaluateSingleCondition 评估单条条件表达式
// condition 形如 "field op value"，例如 "intent_score gte 0.7"
// 字段不存在视为不匹配（返回 false, nil），不抛错
func SOPEvaluateSingleCondition(condition string, data map[string]any) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	field, operator, value, err := SOPParseCondition(condition)
	if err != nil {
		return false, fmt.Errorf("条件表达式解析失败：%w", err)
	}

	fieldValue, exists := data[field]
	if !exists {
		return false, nil
	}

	return SOPEvaluateOperator(fieldValue, operator, value)
}

// SOPEvaluateCompoundCondition 评估复合条件表达式
// 支持：
//   - 单条件："field op value"
//   - AND："cond1 AND cond2"
//   - OR："cond1 OR cond2"
//   - AND/OR 混用时必须用括号分组，括号内子表达式递归求值（evalParenthesized）
//
// 空条件视为始终匹配（与单条件行为一致）
func SOPEvaluateCompoundCondition(condition string, data map[string]any) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	upper := strings.ToUpper(condition)

	hasAnd := strings.Contains(upper, " "+SOPLogicAnd+" ")
	hasOr := strings.Contains(upper, " "+SOPLogicOr+" ")
	if hasAnd && hasOr {

		if strings.Contains(condition, "(") && strings.Contains(condition, ")") {
			return evalParenthesized(condition, data)
		}
		return false, errors.New("复合条件不允许 AND/OR 混用，请使用括号分组")
	}

	if hasAnd {
		parts := splitByOperator(condition, SOPLogicAnd)
		for _, p := range parts {
			matched, err := SOPEvaluateSingleCondition(p, data)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		return true, nil
	}

	if hasOr {
		parts := splitByOperator(condition, SOPLogicOr)
		for _, p := range parts {
			matched, err := SOPEvaluateSingleCondition(p, data)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	}

	return SOPEvaluateSingleCondition(condition, data)
}

// SOPEvaluateConditionBranches 按优先级评估 condition 节点的多个分支
// 规则：
//  1. 按 Priority 降序排序（相同 Priority 按数组顺序）
//  2. 依次评估每个分支的 Condition
//  3. 第一个匹配成功的分支胜出，返回其 Next 节点 ID
//  4. 全部不匹配时返回 Matched=false, NextNode=""（调用方决定 fallback 策略）
//
// 空分支列表视为不匹配
// 单个分支的 Condition 为空视为始终匹配（catch-all 分支）
func SOPEvaluateConditionBranches(branches []SOPConditionBranch, data map[string]any) (SOPConditionResult, error) {
	if len(branches) == 0 {
		return SOPConditionResult{Matched: false}, nil
	}

	sorted := make([]SOPConditionBranch, len(branches))
	copy(sorted, branches)

	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Priority > sorted[j-1].Priority; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	for _, br := range sorted {
		cond := strings.TrimSpace(br.Condition)
		if cond == "" {
			return SOPConditionResult{Matched: true, NextNode: br.Next}, nil
		}

		matched, err := SOPEvaluateCompoundCondition(cond, data)
		if err != nil {
			return SOPConditionResult{}, fmt.Errorf("分支 [%s] 条件评估失败：%w", br.Label, err)
		}

		if matched {
			return SOPConditionResult{Matched: true, NextNode: br.Next}, nil
		}
	}

	return SOPConditionResult{Matched: false}, nil
}

// SOPEvaluateNodeCondition 评估 SOPNode 的条件
// 适用场景：
//   - condition 节点：优先用 Conditions 字段（优先级路由），fallback 到旧 Condition 字段
//   - branch 节点：仅用旧 Condition 字段（向后兼容）
//   - 其他节点：返回始终匹配
//
// 返回 result map，与 marketing_flow.go evaluateCondition 行为兼容：
//   - "_condition_matched": bool
//   - "_next_node": string（可能不存在）
func SOPEvaluateNodeCondition(node *SOPNode, data model.JSONMap) (map[string]any, error) {
	result := make(map[string]any)
	for k, v := range data {
		result[k] = v
	}

	if node == nil {
		result["_condition_matched"] = true
		return result, nil
	}

	if node.Type == SOPNodeTypeCondition && len(node.Conditions) > 0 {
		br, err := SOPEvaluateConditionBranches(node.Conditions, data)
		if err != nil {
			return nil, err
		}
		result["_condition_matched"] = br.Matched
		if br.Matched && br.NextNode != "" {
			result["_next_node"] = br.NextNode
		}
		return result, nil
	}

	condStr := strings.TrimSpace(node.Condition)
	if condStr == "" {
		if c, ok := node.Config["condition"].(string); ok {
			condStr = strings.TrimSpace(c)
		}
	}

	if condStr == "" {
		result["_condition_matched"] = true
		if len(node.Next) > 0 {
			result["_next_node"] = node.Next[0]
		}
		return result, nil
	}

	matched, err := SOPEvaluateCompoundCondition(condStr, data)
	if err != nil {
		return nil, err
	}
	result["_condition_matched"] = matched

	if matched {
		if len(node.Next) > 0 {
			result["_next_node"] = node.Next[0]
		}
	} else {
		if len(node.Next) > 1 {
			result["_next_node"] = node.Next[1]
		} else if len(node.Next) == 1 {
			result["_next_node"] = node.Next[0]
		}
	}

	return result, nil
}

func splitByOperator(condition, op string) []string {
	upper := strings.ToUpper(condition)
	opToken := " " + op + " "
	opLen := len(opToken)

	var parts []string
	start := 0

	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(condition); i++ {
		c := condition[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
			continue
		}

		if i+opLen <= len(upper) && upper[i:i+opLen] == opToken {
			parts = append(parts, strings.TrimSpace(condition[start:i]))
			start = i + opLen
			i += opLen - 1
		}
	}
	parts = append(parts, strings.TrimSpace(condition[start:]))

	filtered := parts[:0]
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func evalParenthesized(condition string, data map[string]any) (bool, error) {
	for strings.Contains(condition, "(") {
		start := strings.LastIndex(condition, "(")
		end := strings.Index(condition[start:], ")")
		if end < 0 {
			return false, errors.New("unmatched parenthesis")
		}
		end += start
		inner := condition[start+1 : end]
		res, err := SOPEvaluateCompoundCondition(inner, data)
		if err != nil {
			return false, err
		}
		condition = condition[:start] + map[bool]string{true: "TRUE", false: "FALSE"}[res] + condition[end+1:]
	}
	condition = strings.ReplaceAll(strings.ToUpper(condition), "TRUE", "true")
	condition = strings.ReplaceAll(strings.ToUpper(condition), "FALSE", "false")
	return SOPEvaluateCompoundCondition(condition, data)
}
