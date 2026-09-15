// Package service 的测试统一入口（TestMain）
//
// 职责：
//  1. 在所有 service 包测试运行前设置环境变量，避免任何子测试意外发起真实网络出站
//  2. IS_TEST_MODE=1   —— 与 cmd/api/main.go 行为一致（test 模式）
//  3. WECOM_DISABLE_OUTBOUND=1 —— 强制禁用企微真实出站
//  4. HTTP_BASE_URL_TEST=...   —— 允许单独测试覆盖（不常用）
//  5. 退出时调用 cache.ShutdownAll() 关闭所有 MemoryCache cleanup goroutine，防止 goroutine leak 导致测试超时
//  6. 注入访客 token 的 HMAC 密钥（见下），否则所有访客会话用例都会因"secret 不能为空"失败
//
// 任何 service 子包的新增测试都会自动应用此设置，不需要每个 _test.go 重复声明。
package service

import (
	"os"
	"testing"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/config"
)

// visitorTokenTestSecret 测试用访客 token HMAC 密钥（仅测试，非生产密钥）
const visitorTokenTestSecret = "hivemtk-test-visitor-token-secret-0123456789"

// TestMain service 包测试统一入口
func TestMain(m *testing.M) {
	if os.Getenv("IS_TEST_MODE") == "" {
		_ = os.Setenv("IS_TEST_MODE", "1")
	}
	if os.Getenv("WECOM_DISABLE_OUTBOUND") == "" {
		_ = os.Setenv("WECOM_DISABLE_OUTBOUND", "1")
	}

	// 访客 token 的 HMAC 密钥来自 config.Security.VisitorTokenSecret，
	// 而测试环境通常没有 config.yaml，该字段为空 —— 于是
	// VisitorChatService.OpenSession 会报 "生成 visitor_token 失败: secret 不能为空"，
	// 连带 web_chat E2E、chat_card_channel 等一批用例集体失败。
	// SetAppConfig 是 config 包明确提供的"测试场景注入"入口。
	if cfg := config.GetAppConfig(); cfg.Security.VisitorTokenSecret == "" {
		cfg.Security.VisitorTokenSecret = visitorTokenTestSecret
		config.SetAppConfig(&cfg)
	}

	code := m.Run()
	cache.ShutdownAll()
	os.Exit(code)
}
