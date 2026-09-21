package service

import "testing"

// validateWebhookURL 是营销流 webhook 动作的 SSRF 闸门：只允许 https，且解析结果不得是
// 回环/内网/链路本地地址。`MARKETING_WEBHOOK_ALLOW_INSECURE=true` 曾把整道闸门**无条件**关掉，
// 且这个开关既不在 .env-example 也不在任何文档里 ⇒ 一个无人知晓的 SSRF 旁路。
// 本组用例钉住修好后的口径：旁路只在**显式开发姿态**下生效（与 ALLOW_INSECURE_WEBHOOK 同一口径，
// 复用 config.IsDevelopmentEnv），非开发姿态设了它也不放行。
func TestValidateWebhookURL_InsecureSwitchOnlyWorksInDev(t *testing.T) {
	const loopback = "https://127.0.0.1:8202/hook"

	// 闸门本身的三条基线腿（不依赖开关）
	if err := validateWebhookURL(loopback); err == nil {
		t.Fatalf("开关未设置时回环地址必须拒绝")
	}
	if err := validateWebhookURL("http://93.184.216.34/hook"); err == nil {
		t.Fatalf("非 https 必须拒绝")
	}
	if err := validateWebhookURL("https://93.184.216.34/hook"); err != nil {
		t.Fatalf("公网 https 必须放行，实际被拒：%v", err)
	}

	// 开发姿态 + 开关：旁路仍然可用（否则本仓 sendActionWebhook 的 httptest 腿会红）
	t.Setenv("APP_ENV", "development")
	t.Setenv("MARKETING_WEBHOOK_ALLOW_INSECURE", "true")
	if err := validateWebhookURL(loopback); err != nil {
		t.Fatalf("开发姿态下的显式旁路必须放行，实际被拒：%v", err)
	}

	// 生产姿态 + 开关：必须**不**旁路。这是本卡的红因所在（旧实现返回 nil）
	for _, env := range []struct{ appEnv, ginMode string }{
		{"production", "debug"},   // APP_ENV 优先级高于 GIN_MODE
		{"production", "release"}, // 全生产
		{"", "release"},           // 未声明环境：按生产姿态收（与 webhook 护栏同口径）
	} {
		t.Setenv("APP_ENV", env.appEnv)
		t.Setenv("GIN_MODE", env.ginMode)
		if err := validateWebhookURL(loopback); err == nil {
			t.Fatalf("APP_ENV=%q GIN_MODE=%q 下开关不得旁路 SSRF 校验", env.appEnv, env.ginMode)
		}
	}
}
