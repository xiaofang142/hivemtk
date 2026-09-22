package main

import (
	"testing"
)

func TestResolvePassword(t *testing.T) {
	cases := []struct {
		name   string
		argv   []string
		env    map[string]string
		expect string
	}{
		{"无参无环境变量则用公开默认值", nil, map[string]string{}, "Seed@123456"},
		{"命令行参数优先", []string{"FromArg"}, map[string]string{"SEED_PASSWORD": "FromSeed"}, "FromArg"},
		{"SEED 优先于 ADMIN", nil, map[string]string{"SEED_PASSWORD": "FromSeed", "ADMIN_PASSWORD": "FromAdmin"}, "FromSeed"},
		{"env 首尾空白被裁掉（与 cmd/seed 同口径）", nil, map[string]string{"SEED_PASSWORD": "  Padded  "}, "Padded"},
		{"空 env 落回默认值", nil, map[string]string{"SEED_PASSWORD": "", "ADMIN_PASSWORD": "   "}, "Seed@123456"},
	}
	for _, tc := range cases {
		getenv := func(k string) string { return tc.env[k] }
		if got := resolvePassword(tc.argv, getenv); got != tc.expect {
			t.Errorf("%s：resolvePassword = %q，期望 %q", tc.name, got, tc.expect)
		}
	}
}
