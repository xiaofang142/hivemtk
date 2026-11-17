package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

type fakeRouteSource struct {
	rules []model.ChannelRouteRule
	err   error
	// 记录调用参数，钉「引擎确实把 channel/account 透传给仓储」
	lastChannel   string
	lastAccountID string
}

func (f *fakeRouteSource) ListEnabled(_ context.Context, tenantID, channel, accountID string) ([]model.ChannelRouteRule, error) {
	f.lastChannel, f.lastAccountID = channel, accountID
	if f.err != nil {
		return nil, f.err
	}
	return f.rules, nil
}

func routeEvent(typ string, extra map[string]any) *model.MessageEvent {
	e := &model.MessageEvent{
		EventID:   "e-1",
		Channel:   model.ChannelTelegram,
		EventType: typ,
		SenderID:  "u-1",
		Extra:     map[string]any{"tenant_id": "t1", "account_id": "acc-1"},
	}
	for k, v := range extra {
		e.Extra[k] = v
	}
	return e
}

func TestRouteEngine_Wildcard(t *testing.T) {
	cases := []struct {
		pattern string
		actual  string
		want    bool
	}{
		{"", model.EventTypeMessageReceived, true},
		{"*", "anything.at.all", true},
		{"message.*", model.EventTypeMessageEdited, true},
		{"message.*", model.EventTypeGroupMemberJoined, false},
		{"message", model.EventTypeMessageDeleted, true},
		{model.EventTypeInteractionCallback, model.EventTypeInteractionCallback, true},
		{model.EventTypeInteractionCallback, model.EventTypeMessageReceived, false},
	}
	for _, c := range cases {
		if got := routeEventTypeMatch(c.pattern, c.actual); got != c.want {
			t.Fatalf("pattern=%q actual=%q 期望 %v，实际 %v", c.pattern, c.actual, c.want, got)
		}
	}
}

func TestRouteEngine_NestedMatch(t *testing.T) {
	src := &fakeRouteSource{rules: []model.ChannelRouteRule{{
		ID: 7, EventType: "*", Target: model.ChannelRouteTargetAgent,
		Match: `{"extra.tag":"vip","extra.level":{"$in":["A","B"]}}`,
	}}}
	eng := NewChannelRouteEngine(src)

	d, err := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, map[string]any{"tag": "vip", "level": "B"}))
	if err != nil || d == nil || d.RuleID != 7 {
		t.Fatalf("嵌套条件应命中：d=%+v err=%v", d, err)
	}
	if src.lastChannel != model.ChannelTelegram || src.lastAccountID != "acc-1" {
		t.Fatalf("引擎应把渠道/账号透传给来源：channel=%q account=%q", src.lastChannel, src.lastAccountID)
	}

	if d, _ := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, map[string]any{"tag": "vip", "level": "C"})); d != nil {
		t.Fatalf("$in 不含 C，不应命中：%+v", d)
	}
	if d, _ := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, map[string]any{"tag": "vip"})); d != nil {
		t.Fatalf("缺 extra.level 时条件不成立，不应命中：%+v", d)
	}
}

func TestRouteEngine_PriorityOrder(t *testing.T) {
	src := &fakeRouteSource{rules: []model.ChannelRouteRule{
		{ID: 1, EventType: "*", Target: model.ChannelRouteTargetPipeline, Priority: 10},
		{ID: 2, EventType: "*", Target: model.ChannelRouteTargetAgent, Priority: 5},
	}}
	eng := NewChannelRouteEngine(src)
	d, err := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// 仓储已按 priority 升序返回；引擎「首个命中生效」，所以这里应命中 ID=1。
	// 换序后命中应跟着换——钉的是「信仓储顺序」这条约定，而不是自己再排一遍。
	if d == nil || d.RuleID != 1 {
		t.Fatalf("应按来源顺序取首个命中，实际 %+v", d)
	}
	src.rules[0], src.rules[1] = src.rules[1], src.rules[0]
	d, _ = eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, nil))
	if d == nil || d.RuleID != 2 {
		t.Fatalf("顺序变化后应命中 ID=2，实际 %+v", d)
	}
}

func TestRouteEngine_NoMatchReturnsNil(t *testing.T) {
	src := &fakeRouteSource{}
	eng := NewChannelRouteEngine(src)
	d, err := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if d != nil {
		t.Fatalf("无规则必须返回 nil（放行），实际 %+v", d)
	}

	// 无租户上下文：一条规则都不该读（fail-closed，不猜租户）。
	src.rules = []model.ChannelRouteRule{{ID: 9, EventType: "*", Target: model.ChannelRouteTargetDiscard}}
	noTenant := routeEvent(model.EventTypeMessageReceived, nil)
	delete(noTenant.Extra, "tenant_id")
	if d, _ := eng.Resolve(context.Background(), noTenant); d != nil {
		t.Fatalf("无租户不应命中任何规则：%+v", d)
	}
}

func TestRouteEngine_InvalidJSONRuleSkipped(t *testing.T) {
	src := &fakeRouteSource{rules: []model.ChannelRouteRule{
		{ID: 1, EventType: "*", Target: model.ChannelRouteTargetDiscard, Match: `{"broken":`},
		{ID: 2, EventType: "*", Target: "not_a_target"},
		{ID: 3, EventType: "*", Target: model.ChannelRouteTargetAgent},
	}}
	eng := NewChannelRouteEngine(src)
	d, err := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, nil))
	if err != nil {
		t.Fatalf("脏规则不应让解析报错: %v", err)
	}
	if d == nil || d.RuleID != 3 {
		t.Fatalf("应跳过非法规则并命中下一条，实际 %+v", d)
	}
	if eng.skipped != 2 {
		t.Fatalf("跳过计数应为 2（坏 JSON + 未知目标），实际 %d", eng.skipped)
	}
}

func TestRouteEngine_SourceErrorPropagates(t *testing.T) {
	eng := NewChannelRouteEngine(&fakeRouteSource{err: context.DeadlineExceeded})
	if _, err := eng.Resolve(context.Background(), routeEvent(model.EventTypeMessageReceived, nil)); err == nil {
		t.Fatal("来源报错必须向上传递（调用方据此决定放行并打日志）")
	}
}
