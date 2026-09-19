package repository

import "testing"

// TestSanitizeOrder 覆盖泛型仓库 orderBy 结构白名单（fail-closed 回退）。
func TestSanitizeOrder(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "created_at DESC"},
		{"id ASC", "id asc"},
		{"created_at desc, id DESC", "created_at desc, id desc"},
		{"  name asc  ", "name asc"},
		{"name ILIKE 'x'", "created_at DESC"},      // 注入式 order 回退
		{"1; DROP TABLE users", "created_at DESC"}, // 分号串
		{"(select 1)", "created_at DESC"},          // 子查询
	}
	for _, c := range cases {
		if got := sanitizeOrder(c.in, "created_at DESC"); got != c.want {
			t.Errorf("sanitizeOrder(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIdentRe 覆盖条件列名标识符白名单。
func TestIdentRe(t *testing.T) {
	for _, ok := range []string{"status", "account_id", "created1"} {
		if !identRe.MatchString(ok) {
			t.Errorf("合法列名被拒: %q", ok)
		}
	}
	for _, bad := range []string{"1x", "a;b", "a b", "(a)", "a=", "id) = 1 OR ("} {
		if identRe.MatchString(bad) {
			t.Errorf("非法列名被放行: %q", bad)
		}
	}
}
