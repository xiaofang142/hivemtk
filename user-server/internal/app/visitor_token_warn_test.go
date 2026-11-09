package app

import (
	"testing"

	"hivemtk-user/internal/config"
)

// TestWarnIfVisitorTokenSecretMissing_DoesNotPanic 冒烟：密钥缺失/已配两态都要
// 能安全走一遍（日志内容无法在单测里断言，钉"不 panic + 不改写配置值"）。
// 对齐 WarnIfCloudURLIsBuiltinDefault_DoesNotPanic 的先例。
func TestWarnIfVisitorTokenSecretMissing_DoesNotPanic(t *testing.T) {
	// nil 配置（未注入）：懒加载路径也不得 panic
	config.SetAppConfig(nil)
	WarnIfVisitorTokenSecretMissing()

	// 已配置：函数应静默返回，且不得改写配置值
	cfg := config.GetAppConfig()
	cfg.Security.VisitorTokenSecret = "unit-test-secret-0123456789abcdef"
	config.SetAppConfig(&cfg)
	WarnIfVisitorTokenSecretMissing()
	if got := config.GetAppConfig().Security.VisitorTokenSecret; got != "unit-test-secret-0123456789abcdef" {
		t.Fatalf("提示函数不得改写配置值，实际 %q", got)
	}

	config.SetAppConfig(nil)
}
