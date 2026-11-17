package service

import (
	"context"
	"os"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// TestResolveTelegramWebhookURL 覆盖 ResolveTelegramWebhookURL 的所有分支
func TestResolveTelegramWebhookURL(t *testing.T) {
	oldEnv := os.Getenv("PUBLIC_BASE_URL")
	defer func() {
		if oldEnv == "" {
			_ = os.Unsetenv("PUBLIC_BASE_URL")
		} else {
			_ = os.Setenv("PUBLIC_BASE_URL", oldEnv)
		}
	}()
	oldCfg := config.GetAppConfig()
	defer config.SetAppConfig(&oldCfg)

	t.Run("nil account", func(t *testing.T) {
		url, has := ResolveTelegramWebhookURL(nil)
		if url != "" || has {
			t.Errorf("nil account: got url=%q has=%v, want both empty/false", url, has)
		}
	})

	t.Run("explicit webhook_url wins regardless of public_base", func(t *testing.T) {
		_ = os.Unsetenv("PUBLIC_BASE_URL")
		config.SetAppConfig(&config.AppConfig{External: config.ExternalConfig{PublicBaseURL: "https://env.example.com"}})
		acc := &model.TelegramAccount{ID: 42, WebhookURL: "https://custom.example.com/hook"}
		url, has := ResolveTelegramWebhookURL(acc)
		if url != "https://custom.example.com/hook" {
			t.Errorf("got %q, want explicit URL to win", url)
		}
		if !has {
			t.Errorf("has should be true when explicit URL provided")
		}
	})

	t.Run("falls back to public_base_url env when explicit empty", func(t *testing.T) {
		_ = os.Setenv("PUBLIC_BASE_URL", "https://env.example.com")
		config.SetAppConfig(&config.AppConfig{})
		acc := &model.TelegramAccount{ID: 7, WebhookURL: ""}
		url, has := ResolveTelegramWebhookURL(acc)
		if url != "https://env.example.com/api/webhook/telegram/7" {
			t.Errorf("got %q, want env-based URL", url)
		}
		if !has {
			t.Errorf("has should be true when public base URL hits")
		}
	})

	t.Run("falls back to config.external.public_base_url when env empty", func(t *testing.T) {
		_ = os.Unsetenv("PUBLIC_BASE_URL")
		config.SetAppConfig(&config.AppConfig{External: config.ExternalConfig{PublicBaseURL: "https://cfg.example.com/"}})
		acc := &model.TelegramAccount{ID: 9, WebhookURL: ""}
		url, has := ResolveTelegramWebhookURL(acc)
		if url != "https://cfg.example.com/api/webhook/telegram/9" {
			t.Errorf("got %q, want config-based URL with trailing slash stripped", url)
		}
		if !has {
			t.Errorf("has should be true when config hits")
		}
	})

	t.Run("returns empty when no explicit and no public base", func(t *testing.T) {
		_ = os.Unsetenv("PUBLIC_BASE_URL")
		config.SetAppConfig(&config.AppConfig{})
		acc := &model.TelegramAccount{ID: 11, WebhookURL: ""}
		url, has := ResolveTelegramWebhookURL(acc)
		if url != "" {
			t.Errorf("got %q, want empty when no source", url)
		}
		if has {
			t.Errorf("has should be false when no source")
		}
	})
}

// TestResolveTelegramWebhookURLForTenantBase R5-3：公网基址按 ctx 所属租户解析。
//
// A 配了自己的回调域名、B 没配时，B 的 bot 绝不能注册到 A 的域名上——
// 那等于把 B 的 Telegram 推送整条送到 A 的服务里（跨租户数据泄露）。
func TestResolveTelegramWebhookURLForTenantBase(t *testing.T) {
	oldEnv := os.Getenv("PUBLIC_BASE_URL")
	defer func() {
		if oldEnv == "" {
			_ = os.Unsetenv("PUBLIC_BASE_URL")
		} else {
			_ = os.Setenv("PUBLIC_BASE_URL", oldEnv)
		}
	}()
	_ = os.Unsetenv("PUBLIC_BASE_URL")
	oldCfg := config.GetAppConfig()
	defer config.SetAppConfig(&oldCfg)
	config.SetAppConfig(&config.AppConfig{})

	bases := map[uint]string{71: "https://t71.example.com", 72: "https://t72.example.com", 99: "https://t99.example.com/"}
	config.SetPublicBaseURLProvider(func(ctx context.Context) string {
		if tid, ok := tenant.FromContext(ctx); ok {
			return bases[tid]
		}
		return "https://platform.example.com"
	})
	// SetPublicBaseURLProvider(nil) 是空操作而不是复位，只能显式装一枚空 stub 归还全局状态。
	defer config.SetPublicBaseURLProvider(func(context.Context) string { return "" })

	acc := &model.TelegramAccount{ID: 7}
	ctxA := tenant.WithTenantID(context.Background(), 71)
	ctxB := tenant.WithTenantID(context.Background(), 72)

	urlA, hasA := ResolveTelegramWebhookURLFor(ctxA, acc)
	if !hasA || urlA != "https://t71.example.com/api/webhook/telegram/7" {
		t.Errorf("A 租户: got url=%q has=%v, want https://t71.example.com/api/webhook/telegram/7 true", urlA, hasA)
	}
	urlB, _ := ResolveTelegramWebhookURLFor(ctxB, acc)
	if urlB == urlA {
		t.Errorf("B 复用了 A 的基址: both=%q", urlB)
	}
	if urlB != "https://t72.example.com/api/webhook/telegram/7" {
		t.Errorf("B 租户: got %q, want https://t72.example.com/api/webhook/telegram/7", urlB)
	}

	// 基址自带结尾斜杠时不能拼出 //api
	urlTrailing, _ := ResolveTelegramWebhookURLFor(tenant.WithTenantID(context.Background(), 99), acc)
	if urlTrailing != "https://t99.example.com/api/webhook/telegram/7" {
		t.Errorf("结尾斜杠未裁: got %q, want 单斜杠拼接", urlTrailing)
	}

	// 无 ctx 形态读平台默认，不借用任何租户的覆盖值
	if got, _ := ResolveTelegramWebhookURL(acc); got != "https://platform.example.com/api/webhook/telegram/7" {
		t.Errorf("无租户 ctx: got %q, want 平台默认基址", got)
	}

	// B18 翻转（2026-11-17 SaaS 配置面改造）：参数中心（租户覆盖）优先于部署 env——
	// A 配了自己的回调域名后，部署 env 的全局单值不得再把它顶掉；env 只在
	// 租户没配（provider 返回空）时兜底。
	_ = os.Setenv("PUBLIC_BASE_URL", "https://env.example.com")
	if got, _ := ResolveTelegramWebhookURLFor(ctxA, acc); got != "https://t71.example.com/api/webhook/telegram/7" {
		t.Errorf("租户覆盖应压过 env（B18）: got %q, want https://t71.example.com/api/webhook/telegram/7", got)
	}
	// provider 未配置的租户（ctxB 有 72 基址；换一个 provider 未覆盖的租户）回落 env。
	ctxEmpty := tenant.WithTenantID(context.Background(), 777)
	if got, _ := ResolveTelegramWebhookURLFor(ctxEmpty, acc); got != "https://env.example.com/api/webhook/telegram/7" {
		t.Errorf("租户未配时应回落 env: got %q, want https://env.example.com/api/webhook/telegram/7", got)
	}
}

// TestIsTelegramPollingEnabled 覆盖 polling 自动启用的判定逻辑
func TestIsTelegramPollingEnabled(t *testing.T) {
	oldEnv := os.Getenv("TELEGRAM_POLLING_ENABLED")
	oldPB := os.Getenv("PUBLIC_BASE_URL")
	defer func() {
		_ = os.Setenv("TELEGRAM_POLLING_ENABLED", oldEnv)
		_ = os.Setenv("PUBLIC_BASE_URL", oldPB)
	}()
	oldCfg := config.GetAppConfig()
	defer config.SetAppConfig(&oldCfg)

	t.Run("env=1 forces on even with public_base set", func(t *testing.T) {
		_ = os.Setenv("TELEGRAM_POLLING_ENABLED", "1")
		_ = os.Setenv("PUBLIC_BASE_URL", "https://example.com")
		config.SetAppConfig(&config.AppConfig{})
		if !IsTelegramPollingEnabled() {
			t.Error("env=1 should force polling on")
		}
	})

	t.Run("env=0 forces off even without public_base", func(t *testing.T) {
		_ = os.Setenv("TELEGRAM_POLLING_ENABLED", "0")
		_ = os.Unsetenv("PUBLIC_BASE_URL")
		config.SetAppConfig(&config.AppConfig{})
		if IsTelegramPollingEnabled() {
			t.Error("env=0 should force polling off")
		}
	})

	t.Run("env unset + public_base set → polling off (webhook path)", func(t *testing.T) {
		_ = os.Unsetenv("TELEGRAM_POLLING_ENABLED")
		_ = os.Setenv("PUBLIC_BASE_URL", "https://example.com")
		config.SetAppConfig(&config.AppConfig{})
		if IsTelegramPollingEnabled() {
			t.Error("with public base URL, polling should be disabled to use webhook")
		}
	})

	t.Run("env unset + no public_base → polling on (fallback)", func(t *testing.T) {
		_ = os.Unsetenv("TELEGRAM_POLLING_ENABLED")
		_ = os.Unsetenv("PUBLIC_BASE_URL")
		config.SetAppConfig(&config.AppConfig{})
		if !IsTelegramPollingEnabled() {
			t.Error("without public base URL, polling should auto-enable as fallback")
		}
	})

	t.Run("env=true/on/yes parsed correctly", func(t *testing.T) {
		for _, v := range []string{"true", "TRUE", "yes", "YES", "on", "ON"} {
			_ = os.Setenv("TELEGRAM_POLLING_ENABLED", v)
			if !IsTelegramPollingEnabled() {
				t.Errorf("env=%q should enable polling", v)
			}
		}
	})
}

// TestIsTelegramConflictError 覆盖 409 Conflict 错误识别
func TestIsTelegramConflictError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errString("random error"), false},
		{errString("409"), true},
		{errString("Conflict: terminated by other getUpdates request"), true},
		{errString("HTTP 409 conflict"), true},
	}
	for _, c := range cases {
		got := isTelegramConflictError(c.err)
		if got != c.want {
			t.Errorf("isTelegramConflictError(%q) = %v, want %v", errMsg(c.err), got, c.want)
		}
	}
}

// TestValidateTelegramWebhookURL 覆盖 S3-5 修复：URL 格式校验
//
// 校验规则：
//  1. 空 → 拒
//  2. scheme != https → 拒
//  3. host 缺失 → 拒
//  4. path 前缀不是 /api/webhook/telegram/ → 拒
//  5. 其余合法 https URL → 通过
func TestValidateTelegramWebhookURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "missing scheme", in: "example.com/api/webhook/telegram/1", wantErr: true},
		{name: "http scheme rejected", in: "http://chat.example.com/api/webhook/telegram/1", wantErr: true},
		{name: "HTTP uppercase rejected", in: "HTTP://chat.example.com/api/webhook/telegram/1", wantErr: true},
		{name: "missing host", in: "https:///api/webhook/telegram/1", wantErr: true},
		{name: "wrong path prefix", in: "https://chat.example.com/webhook/tg/1", wantErr: true},
		{name: "wrong path api only", in: "https://chat.example.com/api/hook/1", wantErr: true},
		{name: "wrong prefix telegram only", in: "https://chat.example.com/telegram/1", wantErr: true},
		{name: "valid https with path", in: "https://chat.example.com/api/webhook/telegram/1", wantErr: false},
		{name: "valid https with port", in: "https://chat.example.com:8443/api/webhook/telegram/42", wantErr: false},
		{name: "valid https subdomain", in: "https://bot.example.com/api/webhook/telegram/7", wantErr: false},
		{name: "valid https with trailing slash and id", in: "https://chat.example.com/api/webhook/telegram/100", wantErr: false},
		{name: "malformed url parse error", in: "ht!tp://[invalid", wantErr: true},
		{name: "valid https with extra path segment allowed", in: "https://chat.example.com/api/webhook/telegram/3/extra", wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := ValidateTelegramWebhookURL(tc.in)
			if tc.wantErr && gotErr == nil {
				t.Errorf("ValidateTelegramWebhookURL(%q) = nil, want error", tc.in)
			}
			if !tc.wantErr && gotErr != nil {
				t.Errorf("ValidateTelegramWebhookURL(%q) = %v, want nil", tc.in, gotErr)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }
func errMsg(e error) string {
	if e == nil {
		return "<nil>"
	}
	return e.Error()
}
