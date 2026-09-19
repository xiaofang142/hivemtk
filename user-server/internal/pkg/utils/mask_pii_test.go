package utils

import "testing"

func TestMaskPhone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"13812345678", "138****5678"},         // 标准 11 位
		{"+86 138-1234-5678", "861******5678"}, // 13 位数字：国际前缀计入，首3尾4
		{"1391234", "*******"},                 // 7 位：全打码（防"假打码"）
		{"123456", "******"},                   // ≤7 位全打码
		{"12345", "*****"},
		{"", ""},
		{"abc-+/()", ""},                       // 无数字
		{"138123456789012", "138********9012"}, // 15 位：首3尾4、中间 8 星
	}
	for _, c := range cases {
		if got := MaskPhone(c.in); got != c.want {
			t.Errorf("MaskPhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"zhangsan@qq.com", "z*******@qq.com"},
		{"a@x.cn", "*@x.cn"},
		{"ab@x.cn", "a*@x.cn"},
		{"name@sub.domain.io", "n***@sub.domain.io"},
		{"no-at-sign", "**********"},
		{"@nolocal.com", "************"},
		{"trail@", "******"},
		{"", ""},
	}
	for _, c := range cases {
		if got := MaskEmail(c.in); got != c.want {
			t.Errorf("MaskEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 关键不变量：打码结果不得含完整原号码/原本地用户名（防"假打码"回归）。
func TestMask_NoFullLeak(t *testing.T) {
	phone := "13812345678"
	if got := MaskPhone(phone); got == phone || len(got) != len(phone) {
		t.Errorf("手机号未被打码或长度漂移: %q", got)
	}
	email := "zhangsan@example.com"
	got := MaskEmail(email)
	for _, leak := range []string{"zhangsan", "hangsan@e", "13812345678"} {
		if containsStr(got, leak) {
			t.Errorf("打码结果包含原文片段 %q: %q", leak, got)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(sub) > 0 && stringsIndex(s, sub) >= 0
}

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
