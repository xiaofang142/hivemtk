package model

import "testing"

// D15：QQ / 个微 sender 必须有稳定的 OneID，不再回退随机 UUID，
// 保证同 sender 两条消息解析到同一客户。
func TestGenerateCustomerUnifiedID_QQAndPersonalWXStable(t *testing.T) {
	qq1 := &Customer{QQOpenID: "123456"}
	qq2 := &Customer{QQOpenID: "123456"}
	if a, b := GenerateCustomerUnifiedID(qq1), GenerateCustomerUnifiedID(qq2); a != b {
		t.Fatalf("同 QQ sender 的 OneID 必须稳定：%s != %s", a, b)
	}
	if got := GenerateCustomerUnifiedID(qq1); got != "qq:123456" {
		t.Fatalf("QQ OneID 前缀期望 qq:，实际 %s", got)
	}
	pw1 := &Customer{PersonalWXOpenID: "wxid_abc"}
	pw2 := &Customer{PersonalWXOpenID: "wxid_abc"}
	if a, b := GenerateCustomerUnifiedID(pw1), GenerateCustomerUnifiedID(pw2); a != b {
		t.Fatalf("同个微 sender 的 OneID 必须稳定：%s != %s", a, b)
	}
	if got := GenerateCustomerUnifiedID(pw1); got != "personal_wx:wxid_abc" {
		t.Fatalf("个微 OneID 前缀期望 personal_wx:，实际 %s", got)
	}
}
