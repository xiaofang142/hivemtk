package service

import (
	"strings"
	"testing"
)

func TestGroupOutputPrivacyGuardAllowsPublicFacts(t *testing.T) {
	guard := NewGroupOutputPrivacyGuard()
	decision := guard.Check("这款产品支持批量导出和团队权限管理。", GroupOutputPolicy{
		AllowedFacts: []string{"批量导出", "团队权限"},
	})
	if !decision.Allowed || len(decision.Reasons) != 0 {
		t.Fatalf("公开事实应放行：%+v", decision)
	}
}

func TestGroupOutputPrivacyGuardBlocksMemberPII(t *testing.T) {
	guard := NewGroupOutputPrivacyGuard()
	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "phone", text: "请联系 13800138000 处理", want: PrivacyReasonPhone},
		{name: "email", text: "发到 zhang.example@example.com", want: PrivacyReasonEmail},
		{name: "order", text: "订单号 ORD-20261127-0001 已处理", want: PrivacyReasonOrderID},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			decision := guard.Check(tt.text, GroupOutputPolicy{})
			if decision.Allowed {
				t.Fatalf("含敏感信息不得放行：%+v", decision)
			}
			found := false
			for _, reason := range decision.Reasons {
				if reason == tt.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("缺少原因 %q：%+v", tt.want, decision)
			}
			if strings.Contains(strings.Join(decision.Reasons, "|"), tt.text) {
				t.Fatal("拒绝原因不得回显原文")
			}
		})
	}
}

func TestGroupOutputPrivacyGuardBlocksPrivateMemberFacts(t *testing.T) {
	guard := NewGroupOutputPrivacyGuard()
	decision := guard.Check("客户私下说预算 50 万，先别在群里讲。", GroupOutputPolicy{
		PrivateFacts: []string{"预算 50 万"},
	})
	if decision.Allowed {
		t.Fatalf("群成员私有事实不得进群回复：%+v", decision)
	}
	found := false
	for _, reason := range decision.Reasons {
		if reason == PrivacyReasonPrivateFact {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少私有事实原因：%+v", decision)
	}
}

func TestGroupOutputPrivacyGuardAllowsExplicitPublicOrderID(t *testing.T) {
	guard := NewGroupOutputPrivacyGuard()
	decision := guard.Check("订单 ORD-20261127-0001 的公开状态已更新。", GroupOutputPolicy{
		AllowedOrderIDs: []string{"ORD-20261127-0001"},
	})
	if !decision.Allowed {
		t.Fatalf("显式授权的公开订单号应放行：%+v", decision)
	}
}

func TestGroupOutputPrivacyGuardIsDeterministic(t *testing.T) {
	guard := NewGroupOutputPrivacyGuard()
	policy := GroupOutputPolicy{PrivateFacts: []string{"预算 50 万"}}
	text := "预算 50 万，联系 13800138000"
	first := guard.Check(text, policy)
	for range 10 {
		next := guard.Check(text, policy)
		if next.Allowed != first.Allowed || strings.Join(next.Reasons, ",") != strings.Join(first.Reasons, ",") {
			t.Fatalf("隐私守卫必须确定性：%+v vs %+v", first, next)
		}
	}
}
