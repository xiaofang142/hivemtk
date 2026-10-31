package service

import "testing"

// 帮助中心正文来自知识库 chunk，行业演示种子含 {hours} 这类模板占位符。
// 展示层必须把模板语法变成可读文本，否则终端用户会看到 "{hours}小时"。
func TestSanitizeDemoPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"已知占位符填值", "充满约{hours}小时，可抽{puffs}口", "充满约2小时，可抽500口"},
		{"区间占位符", "建议在{temp_range}环境使用", "建议在10–25℃环境使用"},
		{"未知占位符去括号", "厚度约{thickness_mm}毫米", "厚度约thickness_mm毫米"},
		{"无占位符原样返回", "正常正文，没有模板", "正常正文，没有模板"},
		{"不误伤代码块", "调用 { 和 } 之外的普通括号（中文）", "调用 { 和 } 之外的普通括号（中文）"},
	}
	for _, c := range cases {
		if got := sanitizeDemoPlaceholders(c.in); got != c.want {
			t.Fatalf("%s: sanitizeDemoPlaceholders(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
