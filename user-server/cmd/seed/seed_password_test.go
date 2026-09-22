package main

import (
	"strings"
	"testing"
)

// TestResolveSeedPassword 覆盖入口的取值优先级：SEED_PASSWORD > ADMIN_PASSWORD > 公开默认值。
// 空串与纯空白都必须落回默认值——否则一次误设 SEED_PASSWORD="" 就会把空口令账号写进库。
func TestResolveSeedPassword(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		expect string
	}{
		{"都不设置", map[string]string{}, "Seed@123456"},
		{"只设 ADMIN_PASSWORD", map[string]string{"ADMIN_PASSWORD": "FromAdmin"}, "FromAdmin"},
		{"两个都设时 SEED 优先", map[string]string{"SEED_PASSWORD": "FromSeed", "ADMIN_PASSWORD": "FromAdmin"}, "FromSeed"},
		{"空白值视同未设置", map[string]string{"SEED_PASSWORD": "   ", "ADMIN_PASSWORD": "\t"}, "Seed@123456"},
		{"空 SEED 时回落 ADMIN", map[string]string{"SEED_PASSWORD": "", "ADMIN_PASSWORD": "FromAdmin"}, "FromAdmin"},
	}
	for _, tc := range cases {
		getenv := func(k string) string { return tc.env[k] }
		if got := resolveSeedPassword(getenv); got != tc.expect {
			t.Errorf("%s：resolveSeedPassword = %q，期望 %q", tc.name, got, tc.expect)
		}
	}
}

// TestSeedPasswordForLog 部署方自设的口令不许进日志；公开默认值才照常回显。
func TestSeedPasswordForLog(t *testing.T) {
	restore := seedPassword
	defer func() { seedPassword = restore }()

	seedPassword = "OperatorChosenSecret"
	line := seedPasswordForLog()
	if !strings.Contains(line, "********") {
		t.Fatalf("自定义口令必须掩码，实得 %q", line)
	}
	if strings.Contains(line, "OperatorChosenSecret") {
		t.Fatalf("日志行泄露了自定义口令：%s", line)
	}

	seedPassword = seedPasswordDefault
	if !strings.Contains(seedPasswordForLog(), seedPasswordDefault) {
		t.Fatalf("默认口令应照常回显（它本就是公开值），实得 %q", seedPasswordForLog())
	}
}
