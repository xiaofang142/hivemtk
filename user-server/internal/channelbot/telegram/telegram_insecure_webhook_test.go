package telegram

import "testing"

// TestVerifyWebhook_InsecureSwitchOnlyWorksInDev 钉住 ALLOW_INSECURE_TELEGRAM_WEBHOOK
// 的双条件口径：显式开关 + 开发姿态必须同时满足。
//
// 2026-10-10 之前该旁路只看开关、没有任何环境护栏 —— 生产环境误设该变量会让
// Telegram 回调验签被静默跳过（仅一条 Warnf），而 service 包的
// guardInsecureWebhookAtStartup 只覆盖 ALLOW_INSECURE_WEBHOOK 一个键，启动也不报错。
// 这是三处验签旁路（ALLOW_INSECURE_WEBHOOK / ALLOW_INSECURE_TELEGRAM_WEBHOOK /
// MARKETING_WEBHOOK_ALLOW_INSECURE）里唯一没有护栏的一处。
//
// 与 marketing 那处（internal/content/service/marketing_flow_ssrf_guard_test.go）同一组判据。
func TestVerifyWebhook_InsecureSwitchOnlyWorksInDev(t *testing.T) {
	// 基线：不设开关、不设密钥 ⇒ 一律拒签（fail-closed）。
	if VerifyWebhook("", "x-telegram-secret") {
		t.Fatal("开关未设置时必须拒签")
	}

	// 配了密钥 ⇒ 走真实比对，开关无从介入。
	if !VerifyWebhook("s3cret", "s3cret") {
		t.Fatal("密钥匹配必须放行")
	}
	if VerifyWebhook("s3cret", "wrong") {
		t.Fatal("密钥不匹配必须拒签，开关不得介入已配密钥的账号")
	}

	// 开发姿态 + 开关 ⇒ 旁路可用（仓内 telegram_account / e2e 用例依赖这条腿）。
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	if !VerifyWebhook("", "anything") {
		t.Fatal("开发姿态下的显式旁路必须放行")
	}

	// 生产姿态 + 开关 ⇒ 必须**不**旁路。这是本组用例的红因所在（旧实现返回 true）。
	for _, env := range []struct{ appEnv, ginMode string }{
		{"production", "debug"},   // APP_ENV 优先级高于 GIN_MODE
		{"production", "release"}, // 全生产
		{"", "release"},           // 未声明环境：按生产姿态收（与 webhook 护栏同口径）
	} {
		t.Setenv("APP_ENV", env.appEnv)
		t.Setenv("GIN_MODE", env.ginMode)
		if VerifyWebhook("", "anything") {
			t.Fatalf("APP_ENV=%q GIN_MODE=%q 下开关不得跳过验签", env.appEnv, env.ginMode)
		}
	}

	// 开关值本身也只认字面量 "true"：大小写/前后空格等形态一律按未开启处理。
	for _, v := range []string{"", "false", "TRUE", "1", "true "} {
		t.Setenv("APP_ENV", "development")
		t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", v)
		if VerifyWebhook("", "anything") {
			t.Fatalf("开关值 %q 不得启用旁路", v)
		}
	}
}
