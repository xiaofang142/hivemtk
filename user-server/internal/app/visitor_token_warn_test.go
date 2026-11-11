package app

import (
	"testing"

	"hivemtk-user/internal/config"
)

// TestAuditVisitorTokenSecretAtStartup 三态审计（F-6-5①）：
//   - nil/空配置：Error 告警 + 弱密钥计数 +1（不 panic，不改写配置值）；
//   - 弱密钥（非空 <32 字符）：Error 告警 + 计数 +1；
//   - 强密钥（≥32 字符）：静默，计数不增。
//
// 日志内容无法在单测里断言（zap 全局），以 metrics 计数差值钉行为；
// 对齐 TestWarnIfCloudURLIsBuiltinDefault_DoesNotPanic 的先例。
func TestAuditVisitorTokenSecretAtStartup(t *testing.T) {
	readCounter := func() uint64 {
		return visitorTokenWeakSecretTotal.WithLabel().Value()
	}

	// nil 配置（未注入）：懒加载路径也不得 panic，且按缺失计数
	config.SetAppConfig(nil)
	before := readCounter()
	AuditVisitorTokenSecretAtStartup()
	if got := readCounter(); got != before+1 {
		t.Fatalf("nil 配置应按缺失计弱密钥 +1，before=%d after=%d", before, got)
	}

	// 弱密钥（非空 <32）：计数 +1，且不得改写配置值
	cfg := config.GetAppConfig()
	cfg.Security.VisitorTokenSecret = "short-secret"
	config.SetAppConfig(&cfg)
	before = readCounter()
	AuditVisitorTokenSecretAtStartup()
	if got := readCounter(); got != before+1 {
		t.Fatalf("弱密钥（%d 字符）应计 +1，before=%d after=%d",
			len("short-secret"), before, got)
	}
	if got := config.GetAppConfig().Security.VisitorTokenSecret; got != "short-secret" {
		t.Fatalf("审计函数不得改写配置值，实际 %q", got)
	}

	// 强密钥（≥32）：静默，计数不增
	cfg = config.GetAppConfig()
	cfg.Security.VisitorTokenSecret = "unit-test-secret-0123456789abcdef" // 37 字符
	config.SetAppConfig(&cfg)
	before = readCounter()
	AuditVisitorTokenSecretAtStartup()
	if got := readCounter(); got != before {
		t.Fatalf("强密钥应静默（计数不增），before=%d after=%d", before, got)
	}

	config.SetAppConfig(nil)
}
