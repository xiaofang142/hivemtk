package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// 注册表明细由 channelgw.Default 维护；service 侧 fixture 只投影名字，
// 不在 service 包反向 import channelgw（会形成 import cycle）。
var leadWiringRegistryFixture = []struct {
	name string
	mine bool
}{
	{"douyin", true}, {"xiaohongshu", true}, {"tiktok", true}, {"kuaishou", true},
	{"xianyu", true}, {"personal_wx", true}, {"telegram", true}, {"feishu", true},
	{"wecom", false},
}

// LM-06 契约 fixture：当前注册表中每个 MineMode != none 渠道都必须在发掘资格投影里，
// 且接线契约明确区分「落库→资格→双模块独立分发」三段；不要求真实渠道账号。
func TestLeadChannelWiringMatrix_RegistryProjection(t *testing.T) {
	// service 包测试不 import channelgw（会形成 import cycle），其 init 不执行；
	// 这里按注册表 fixture 显式投影，验证 service 侧消费契约。
	fixtureNames := make([]string, 0, len(leadWiringRegistryFixture))
	for _, spec := range leadWiringRegistryFixture {
		if spec.mine {
			fixtureNames = append(fixtureNames, spec.name)
		}
	}
	SetLeadMiningChannels(fixtureNames)
	projected := map[string]bool{}
	for _, name := range LeadMiningChannels() {
		projected[name] = true
	}
	if len(projected) == 0 {
		t.Fatal("发掘资格投影为空：注册渠道未接入发掘链")
	}
	for _, spec := range leadWiringRegistryFixture {
		if !spec.mine {
			continue
		}
		if !projected[spec.name] {
			t.Errorf("渠道 %s 在注册表声明发掘能力，但未投影到资格集合，接线断裂", spec.name)
		}
	}
}

// LM-06 正/反例：契约描述一条入站只触发一次发掘，AI 回声/系统通知/重复事件不误触发；
// 自动回复与发掘两个开关独立，互不阻断。
func TestLeadChannelWiringContract_DispatchCases(t *testing.T) {
	cases := []struct {
		name       string
		direction  string
		isAI       bool
		senderType string
		duplicate  bool
		wantMine   bool
	}{
		{"客户真实入站", "inbound", false, "customer", false, true},
		{"AI 回声", "outbound", true, "agent", false, false},
		{"系统通知", "inbound", false, "system", false, false},
		{"重复事件", "inbound", false, "customer", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub := &model.MessageHub{Direction: tc.direction, IsAIReply: tc.isAI, SenderName: tc.senderType}
			got := leadChannelWiringAllowsMining(hub, tc.senderType, tc.duplicate)
			if got != tc.wantMine {
				t.Fatalf("%s: 期望发掘触发=%v，实际=%v", tc.name, tc.wantMine, got)
			}
		})
	}
}
