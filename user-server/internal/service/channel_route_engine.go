package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
)

// channel_route_engine.go 入站事件路由的匹配引擎（specs/024 T303）。
//
// 这是**纯逻辑**：规则由调用方注入（RouteRuleSource），这里不碰 DB、不写日志副作用。
// 好处是匹配语义可以穷举单测，而「读了哪些规则」由仓储的租户条件单独钉。
//
// 三条口径（与 LangBot 的 event filter 一致）：
//  1. 无命中 = 放行（返回 nil, nil），这是「无规则时行为与今天完全一致」的实现方式；
//  2. 非法规则（JSON 坏了、目标词不认识）跳过并计数，不抛错——一条脏数据不该让整个入站 500；
//  3. 首个命中生效，不叠加：叠加会让「哪些规则同时生效」变成不可推理的组合问题。

// RouteDecision 一次路由决策。
type RouteDecision struct {
	RuleID    uint
	Target    string
	TargetRef string
}

// RouteRuleSource 引擎的规则来源（Repository 适配；测试可注入内存实现）。
type RouteRuleSource interface {
	ListEnabled(ctx context.Context, tenantID, channel, accountID string) ([]model.ChannelRouteRule, error)
}

// ChannelRouteEngine 规则匹配引擎。
type ChannelRouteEngine struct {
	source RouteRuleSource
	// skipped 累计被跳过的非法规则数（可观测：管理端能看到「有几条规则其实没生效」）。
	skipped uint64
}

func NewChannelRouteEngine(source RouteRuleSource) *ChannelRouteEngine {
	return &ChannelRouteEngine{source: source}
}

// Resolve 解析一次入站事件的处置方式；无命中返回 (nil, nil)。
func (e *ChannelRouteEngine) Resolve(ctx context.Context, event *model.MessageEvent) (*RouteDecision, error) {
	if e == nil || e.source == nil || event == nil {
		return nil, nil
	}
	tenantID := extraString(event, "tenant_id")
	// 无租户上下文 = 读不到任何规则（fail-closed）：宁可放行回今天的行为，
	// 也不能去猜一个租户再套它的规则。
	if tenantID == "" {
		return nil, nil
	}
	accountID := extraString(event, "account_id")
	rules, err := e.source.ListEnabled(ctx, tenantID, event.Channel, accountID)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if !routeEventTypeMatch(rule.EventType, event.EventType) {
			continue
		}
		ok, valid := routeMatchConditions(rule.Match, event)
		if !valid {
			e.skipped++
			continue
		}
		if !ok {
			continue
		}
		target := rule.Target
		if target == "" {
			target = model.ChannelRouteTargetPipeline
		}
		if !model.IsKnownChannelRouteTarget(target) {
			e.skipped++
			continue
		}
		return &RouteDecision{RuleID: rule.ID, Target: target, TargetRef: rule.TargetRef}, nil
	}
	return nil, nil
}

// routeEventTypeMatch 事件类型通配匹配：`*` 全匹配、`prefix.*` 前缀匹配、否则精确比较。
// 规则留空按 `*` 处理（只按渠道/账号限定的规则是常见写法）。
func routeEventTypeMatch(pattern, actual string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(actual, strings.TrimSuffix(pattern, "*"))
	}
	// 只写 "message" 也按前缀家族处理：规则作者不会为了通配去背 `message.*` 语法。
	if !strings.Contains(pattern, ".") && !strings.Contains(pattern, "*") {
		return actual == pattern || strings.HasPrefix(actual, pattern+".")
	}
	return pattern == actual
}

// routeMatchConditions 判定 Match JSON 是否满足。第二个返回值表示规则**本身合法**：
// false 表示 JSON 坏了，调用方应跳过该规则并计数（不报错、不命中）。
func routeMatchConditions(raw string, event *model.MessageEvent) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return true, true
	}
	var conds map[string]any
	if err := json.Unmarshal([]byte(raw), &conds); err != nil {
		return false, false
	}
	for path, want := range conds {
		got, ok := routeLookup(event, path)
		if !ok {
			// 缺省字段 = 不匹配（规则写了条件就说明「必须满足」；
			// 想表达「有就该等、没有也放行」请写两条规则）。
			return false, true
		}
		if !routeValueMatches(got, want) {
			return false, true
		}
	}
	return true, true
}

// routeLookup 取嵌套路径值：支持 `extra.a.b`、`channel`、`event_type`、`sender_id` 等。
func routeLookup(event *model.MessageEvent, path string) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}
	var cur any
	switch parts[0] {
	case "extra":
		if event.Extra == nil {
			return nil, false
		}
		cur = map[string]any(event.Extra)
	case "channel":
		return event.Channel, event.Channel != ""
	case "account_id":
		v, ok := event.Extra["account_id"]
		return v, ok
	case "event_type":
		return event.EventType, event.EventType != ""
	case "sender_id":
		return event.SenderID, event.SenderID != ""
	case "sender_type":
		return event.SenderType, event.SenderType != ""
	case "conversation_id":
		return event.ConversationID, event.ConversationID != ""
	case "msg_type":
		return event.MsgType, event.MsgType != ""
	case "content":
		return event.Content, true
	default:
		// 裸键默认按 extra 处理：`{"tag":"vip"}` 比 `{"extra.tag":"vip"}` 更常被写出来。
		if event.Extra != nil {
			v, ok := event.Extra[parts[0]]
			return v, ok
		}
		return nil, false
	}
	for _, p := range parts[1:] {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// routeValueMatches 值匹配：{"$in":[...]} 集合包含，其余按等值比较。
func routeValueMatches(got, want any) bool {
	if m, ok := want.(map[string]any); ok {
		if in, has := m["$in"]; has {
			list, ok := in.([]any)
			if !ok {
				return false
			}
			for _, item := range list {
				if routeScalarEqual(got, item) {
					return true
				}
			}
			return false
		}
		// 其他操作符留给后续 spec：不认识就当作不匹配（而不是静默放行）。
		return false
	}
	return routeScalarEqual(got, want)
}

func routeScalarEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	if af, ok := routeFloat(a); ok {
		if bf, ok2 := routeFloat(b); ok2 {
			return af == bf
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func routeFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
