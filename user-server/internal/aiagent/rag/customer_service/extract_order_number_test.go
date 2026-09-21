package ragcustomerservice

import "testing"

// 订单号只能来自消息本身：抽取器一旦返回与消息无关的值，下游会拿假单号去查不存在的订单。
func TestExtractOrderNumberReturnsRealToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"订单号 12345 有货吗", "12345"},
		{"订单号：a0099 已发货", "a0099"},
		{"订单号 8899，创建于 2026 年", "8899"},
		{"订单号在哪里看", ""},
		{"号", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := extractOrderNumber(tc.in); got != tc.want {
			t.Errorf("extractOrderNumber(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractParametersUsesMessageOrderNumber(t *testing.T) {
	got := extractParameters("订单号 123 商品 裙子 到货")
	if got["product_name"] != "裙子" {
		t.Fatalf("夹具前置不成立：商品名未抽出，%v", got)
	}
	if got["order_number"] != "123" {
		t.Errorf("order_number = %q, 期望消息里那个 123 而不是任何与消息无关的值", got["order_number"])
	}
}
