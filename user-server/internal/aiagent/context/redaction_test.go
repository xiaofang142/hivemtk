package context

import (
	"strings"
	"testing"
)

func TestRedactForLogRemovesPIIAndSensitiveFragments(t *testing.T) {
	input := "客户 13800138000 邮箱 buyer@example.com，订单 ORD-2026-11，备注客户预算 50 万"
	got := RedactForLog(input, "客户预算 50 万")
	for _, raw := range []string{"13800138000", "buyer@example.com", "ORD-2026-11", "客户预算 50 万"} {
		if strings.Contains(got, raw) {
			t.Fatalf("日志仍包含明文 %q：%s", raw, got)
		}
	}
	for _, marker := range []string{"[REDACTED:phone:", "[REDACTED:email:", "[REDACTED:order:", "[REDACTED:sensitive:"} {
		if !strings.Contains(got, marker) {
			t.Fatalf("缺少脱敏标记 %q：%s", marker, got)
		}
	}
}

func TestRedactForLogIsDeterministic(t *testing.T) {
	a := RedactForLog("13800138000")
	b := RedactForLog("13800138000")
	if a != b {
		t.Fatalf("同一值脱敏必须稳定：%q vs %q", a, b)
	}
	if RedactForLog("13800138000") == RedactForLog("13800138001") {
		t.Fatal("不同值不得产生相同标记")
	}
}

func TestRedactForLogKeepsNonSensitiveText(t *testing.T) {
	got := RedactForLog("客户询问交期，答复三天内发货")
	if got != "客户询问交期，答复三天内发货" {
		t.Fatalf("非敏感文本不应被改写：%q", got)
	}
}
