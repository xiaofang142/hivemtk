package router

import (
	"testing"
)

// A-15 复检钉子（任务二 F-2 优化后）：isLocalEnv 空值语义收口。
//
// 原实现：APP_ENV/MODE 均空 → 视为本地 → 生产忘设环境变量时 /__debug__/routes 被注册。
// 该路由挂 JWT+AdminAuth 双守卫，泄露面=超管可见路由表，但"未配置即放行"的默认方向
// 与安全默认值原则相反。优化后：空值=非本地，显式写 dev/development/debug/test/local
// 才算本地；本地开发者在 .env 里写一行即可（.env-example 已补文档）。
// 注意：本包测试经 TestMain 初始化 config，.env 若被 config 加载器读入会污染单键 Setenv
// 隔离 —— 所以每个用例把 APP_ENV/MODE/GIN_MODE 三键全部显式置值/置空后再断言。
func TestIsLocalEnv_EmptyMeansNotLocal(t *testing.T) {
	for _, k := range []string{"APP_ENV", "MODE", "GIN_MODE"} {
		t.Setenv(k, "")
	}
	if isLocalEnv() {
		t.Fatal("APP_ENV/MODE 均空应视为非本地（安全默认值），got true")
	}
}

func TestIsLocalEnv_ExplicitLocalValues(t *testing.T) {
	cases := map[string]bool{
		"dev":         true,
		"development": true,
		"debug":       true,
		"test":        true,
		"local":       true,
		" DEV ":       true, // 大小写与空白容忍保持
		"production":  false,
		"prod":        false,
		"release":     false,
	}
	for val, want := range cases {
		t.Setenv("APP_ENV", val)
		t.Setenv("MODE", "")
		t.Setenv("GIN_MODE", "")
		if got := isLocalEnv(); got != want {
			t.Errorf("APP_ENV=%q → isLocalEnv=%v, want %v", val, got, want)
		}
	}
}

func TestIsLocalEnv_ModeFallback(t *testing.T) {
	// APP_ENV 未设、MODE=dev → 本地（双键回退语义保持）
	t.Setenv("APP_ENV", "")
	t.Setenv("MODE", "dev")
	t.Setenv("GIN_MODE", "")
	if !isLocalEnv() {
		t.Fatal("MODE=dev 应视为本地")
	}
	// MODE 非本地值覆盖 APP_ENV 缺省
	t.Setenv("MODE", "production")
	if isLocalEnv() {
		t.Fatal("MODE=production 应视为非本地")
	}
}

func TestIsLocalEnv_GinModeDebugNotConsulted(t *testing.T) {
	// isLocalEnv 与 config.IsDevelopmentEnv 的职责分界：前者不看 GIN_MODE
	// （GIN_MODE=debug 是 gin 框架的日志档，不是部署环境声明）。
	t.Setenv("APP_ENV", "")
	t.Setenv("MODE", "")
	t.Setenv("GIN_MODE", "debug")
	if isLocalEnv() {
		t.Fatal("仅 GIN_MODE=debug 不应判为本地（isLocalEnv 不读 GIN_MODE）")
	}
}
