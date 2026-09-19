package pagination

import "testing"

// TestCursorOrderByAllowlist 钉死 CursorQuery 的 order_by 结构白名单：
// 仅"列名 ASC/DESC 逗号串"可通过，SQL 注释/分号/子查询/函数调用一律拒绝。
// 反向测试：orderClauseRe 若被放宽为 Contains 式判据，恶意样例将不再被拒 ⇒ 本测试红。
func TestCursorOrderByAllowlist(t *testing.T) {
	allow := []string{
		"created_at desc",
		"id asc",
		"created_at desc, id desc",
		"sent_at asc,created_at desc", // 逗号后无空格亦合法
	}
	deny := []string{
		"created_at DESC; DROP TABLE users",
		"(select password from admins)",
		"created_at DESC --",
		"1=1",
		"random()",
		"col ASC, (SELECT 1)",
		"pg_sleep(1)",
		"created_at UPS",
		"",
	}
	for _, s := range allow {
		if !orderClauseRe.MatchString(s) {
			t.Errorf("合法 order 被拒: %q", s)
		}
	}
	for _, s := range deny {
		if orderClauseRe.MatchString(s) {
			t.Errorf("恶意 order 被放行: %q", s)
		}
	}
}
