package service

// 契约锁：客户渠道身份有**两处**存储，而读侧必须看到并集。
//
// 这批的由来是一个真故障：LTC 验收脚本报
//   customer ltc-e2e-one-01 has no channel identity on file
// 脚本报的是"事实"，脚本错在它以为"绑了 customer_channels 就等于这个客户有渠道身份"。
// 根因是读侧（CustomerAvailableChannels / CustomerChannelIdentity）只认 customers 表上的
// 反规范化列，而 BindChannel 之外的写入口（直接 SQL、导入脚本、迁移）只落绑定表。
// 于是：客户在渠道总览里绑得好好的，报价次发与主动触达却说"没有渠道身份"，
// 而且失败形态是 502 outbound_failed —— 看起来像渠道挂了，不像数据形态问题。
//
// 三条腿分别钉住：
//  1. 只有绑定表有身份 ⇒ 可用（本次修复的正题）
//  2. 只有反规范化列有身份 ⇒ 可用（旧行为不许被改坏）
//  3. 两者都有 ⇒ 取反规范化列（BindChannel 同步过去的主字段优先，
//     列与表打架时以列为准，不能让"陈旧的一列"盖掉"刚绑的表"）
//
// 另有一条不变量：绑定行存在但 channel_user_id 为空，不算有身份
// ——外发侧拿到空收件人，与"没绑"在行为上无法区分。

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestCustomerChannelBindingsSkipsEmptyIdentity(t *testing.T) {
	rows := []model.CustomerChannel{
		{Channel: "wechat", ChannelUserID: "wx-abc"},
		{Channel: "  ", ChannelUserID: "x"},      // 渠道名为空
		{Channel: "feishu", ChannelUserID: "  "}, // 身份为空
		{Channel: "email", ChannelUserID: "a@b.c"},
	}
	got := CustomerChannelBindings(rows)

	if got["wechat"] != "wx-abc" {
		t.Fatalf("wechat 身份应为 wx-abc，实际 %q", got["wechat"])
	}
	if got["email"] != "a@b.c" {
		t.Fatalf("email 身份应为 a@b.c，实际 %q", got["email"])
	}
	if _, ok := got[" "]; ok {
		t.Fatal("渠道名为空的行不该进映射")
	}
	if _, ok := got["feishu"]; ok {
		t.Fatal("身份为空的行不该进映射：外发拿到空收件人与没绑无法区分")
	}
}

func TestCustomerChannelBindingsFirstRowWins(t *testing.T) {
	// 同一 (one_id, channel) 唯一约束下不该出现重复；真有重复时按行序取第一条，
	// 不做"最后一条覆盖"——后写的可能是脏数据，静默采纳它才是错的方向。
	rows := []model.CustomerChannel{
		{Channel: "wechat", ChannelUserID: "first"},
		{Channel: "wechat", ChannelUserID: "second"},
	}
	if got := CustomerChannelBindings(rows)["wechat"]; got != "first" {
		t.Fatalf("重复渠道应取首行 first，实际 %q", got)
	}
}

func TestCustomerIdentityAvailableCoversBindings(t *testing.T) {
	// 反规范化列全空，只有绑定表有身份——这正是 LTC 验收脚本报错的那形态。
	cust := &model.Customer{UnifiedID: "u-1"}
	bindings := map[string]string{"wechat": "wx-from-table"}

	ident := NewCustomerIdentity(cust, bindings)

	avail := ident.Available(nil)
	if len(avail) != 1 || avail[0] != "wechat" {
		t.Fatalf("绑定表里的 wechat 应出现在可用渠道，实际 %v", avail)
	}
	if !ident.Has("wechat") {
		t.Fatal("Has(wechat) 应为 true")
	}
	if got := ident.Identity("wechat"); got != "wx-from-table" {
		t.Fatalf("收件人应为绑定表里的身份，实际 %q", got)
	}
}

func TestCustomerIdentityDenormalizedStillWorks(t *testing.T) {
	// 旧行为不许被改坏：只有反规范化列有身份时同样可用。
	// 手机号同时喂 sms 与 whatsapp 两个渠道（CustomerChannelIdentity 里 whatsapp 回落 c.Phone），
	// 这条回落是既有口径，不许在本次修复里被顺手削掉——它也是"同号双渠道"能通的由来。
	cust := &model.Customer{UnifiedID: "u-2", Phone: "13800000000"}

	avail := NewCustomerIdentity(cust, nil).Available(nil)
	if len(avail) != 2 || avail[0] != "sms" || avail[1] != "whatsapp" {
		t.Fatalf("手机号应同时产出 sms 与 whatsapp，实际 %v", avail)
	}
}

func TestCustomerIdentityDenormalizedWinsOverBindings(t *testing.T) {
	cust := &model.Customer{UnifiedID: "u-3", WechatOpenID: "wx-column"}
	bindings := map[string]string{"wechat": "wx-table"}

	got := NewCustomerIdentity(cust, bindings).Identity("wechat")
	if got != "wx-column" {
		t.Fatalf("列与表冲突时应取反规范化列 wx-column，实际 %q", got)
	}
}

func TestCustomerIdentityPrefersRequestedOrderFirst(t *testing.T) {
	// 客户显式指定偏好渠道时排最前——这条口径原来在 CustomerAvailableChannels 里，
	// 提成到 CustomerIdentity.Available 后不许丢（DTO PreferredChannels 就是它的用武之地）。
	cust := &model.Customer{UnifiedID: "u-4", Phone: "13800000001", TelegramChatID: 1001}
	bindings := map[string]string{"wechat": "wx-from-table"}

	avail := NewCustomerIdentity(cust, bindings).Available([]string{"wechat"})
	if len(avail) != 4 {
		t.Fatalf("四处身份应都可用（手机号喂 sms+whatsapp），实际 %v", avail)
	}
	if avail[0] != "wechat" {
		t.Fatalf("偏好渠道应排首位，实际 %v", avail)
	}
}

func TestCustomerIdentityNilSafe(t *testing.T) {
	// controller 侧对 nil 客户也调过 Identity：nil 指针不能把预览接口打成 500。
	var ident *CustomerIdentity
	if ident.Identity("wechat") != "" || ident.Has("wechat") || len(ident.Available(nil)) != 0 {
		t.Fatal("nil CustomerIdentity 应恒返回空，不得 panic")
	}
	if NewCustomerIdentity(nil, map[string]string{"wechat": "wx"}).Has("wechat") {
		t.Fatal("nil 客户即便带绑定也不该有身份")
	}
}

func TestCustomerAvailableChannelsBackedBySameOrder(t *testing.T) {
	// 提成常量后两处必须同口径：CustomerAvailableChannels 是 CustomerIdentity.Available(nil bindings)
	// 的退化形态。判的是"旧导出函数没被改坏"，不是判实现细节。
	cust := &model.Customer{UnifiedID: "u-5", Email: "a@b.c", WechatOpenID: "wx-1", TelegramChatID: 7}
	want := CustomerAvailableChannels(cust, nil)
	got := NewCustomerIdentity(cust, nil).Available(nil)

	if len(got) != len(want) {
		t.Fatalf("长度不一致：旧导出 %v，新实现 %v", want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("第 %d 位不一致：旧导出 %q，新实现 %q", i, want[i], got[i])
		}
	}
}
