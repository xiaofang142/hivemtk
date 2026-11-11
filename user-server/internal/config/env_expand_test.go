package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnvWithDefault(t *testing.T) {
	oldVars := map[string]string{
		"LLM_BASE_URL":        os.Getenv("LLM_BASE_URL"),
		"EMBEDDING_BASE_URL":  os.Getenv("EMBEDDING_BASE_URL"),
		"TEST_PLAIN_VAR":      os.Getenv("TEST_PLAIN_VAR"),
		"TEST_VAR_WITH_COLON": os.Getenv("TEST_VAR_WITH_COLON"),
	}
	_ = os.Unsetenv("LLM_BASE_URL")
	_ = os.Unsetenv("EMBEDDING_BASE_URL")
	_ = os.Unsetenv("TEST_PLAIN_VAR")
	_ = os.Unsetenv("TEST_VAR_WITH_COLON")
	defer func() {
		for k, v := range oldVars {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	cases := []struct {
		name string
		in   string
		want string
		pre  func()
	}{
		{
			name: "no var placeholder, pass through",
			in:   "https://api.example.com/v1",
			want: "https://api.example.com/v1",
		},
		{
			name: "env var unset → use default (bash style)",
			in:   "${LLM_BASE_URL:http://127.0.0.1:8207/v1}",
			want: "http://127.0.0.1:8207/v1",
		},
		{
			name: "env var set → override default",
			in:   "${LLM_BASE_URL:http://127.0.0.1:8207/v1}",
			want: "https://my-llm.example.com/v1",
			pre:  func() { _ = os.Setenv("LLM_BASE_URL", "https://my-llm.example.com/v1") },
		},
		{
			name: "plain ${VAR} (no default) - unset returns empty",
			in:   "${TEST_PLAIN_VAR}",
			want: "",
		},
		{
			name: "plain ${VAR} - set returns value",
			in:   "${TEST_PLAIN_VAR}",
			want: "hello",
			pre:  func() { _ = os.Setenv("TEST_PLAIN_VAR", "hello") },
		},
		{
			name: "default contains port+colon, must not break regex",
			in:   "${EMBEDDING_BASE_URL:http://127.0.0.1:8208/v1}",
			want: "http://127.0.0.1:8208/v1",
		},
		{
			name: "default contains path with slash",
			in:   "${API_BASE:http://x:8000/api/v1}",
			want: "http://x:8000/api/v1",
		},
		{
			name: "yaml multi-line: multiple vars",
			in: `base_url: "${A:http://a:1}"
host: "${B:localhost}"`,
			want: `base_url: "http://a:1"
host: "localhost"`,
		},
		{
			name: "default is empty",
			in:   "${MAYBE:}",
			want: "",
		},
		{
			name: "literal $ not in placeholder",
			in:   "price is $10",
			want: "price is $10",
		},
		{
			name: "literal $ followed by space (not a var)",
			in:   "amount: $ 100",
			want: "amount: $ 100",
		},
		{
			name: "non env var sequence ${1abc} ignored (must start with letter/underscore)",
			in:   "${1invalid}",
			want: "${1invalid}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pre != nil {
				tc.pre()
			}
			got := expandEnvWithDefault(tc.in)
			if got != tc.want {
				t.Errorf("expandEnvWithDefault(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// GetServerBaseURL 读 config.yaml 时必须先展开 ${VAR:default}：
// 未展开时跟踪/退订链接会带上字面占位符。
func TestGetServerBaseURLExpandsEnvPlaceholder(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := "platform_api:\n  api_url: \"${PLATFORM_API_URL:http://127.0.0.1:8205}\"\n"
	if err := os.WriteFile("config.yaml", []byte(cfg), 0o600); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}

	old, had := os.LookupEnv("PLATFORM_API_URL")
	if err := os.Unsetenv("PLATFORM_API_URL"); err != nil {
		t.Fatalf("unset PLATFORM_API_URL 失败: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("PLATFORM_API_URL", old)
		} else {
			_ = os.Unsetenv("PLATFORM_API_URL")
		}
	})
	if got := GetServerBaseURL(); got != "http://127.0.0.1:8205" {
		t.Fatalf("未设置 PLATFORM_API_URL 时 GetServerBaseURL() = %q，期望展开后的默认值", got)
	}

	t.Setenv("PLATFORM_API_URL", "https://platform.example.com")
	if got := GetServerBaseURL(); got != "https://platform.example.com" {
		t.Fatalf("设置 PLATFORM_API_URL 后 GetServerBaseURL() = %q，期望环境变量值", got)
	}
}

// CF-05（2026-11-12 §3.17 配置审计批）：CONFIG_PATH 显式指向配置文件时插搜索链头——
// 从任意工作目录启动都能命中；未设置时搜索链与历史逐字节一致（行为零变更由既有用例守护）。
// 断言用 logging.component：AppConfig 不含 platform_api 段（那是 platformAPIYAML 专属解析）。
func TestLoadAppConfigHonorsConfigPathEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "custom-config.yaml")
	body := "logging:\n  component: via-config-path\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("写入自定义配置失败: %v", err)
	}
	t.Setenv("CONFIG_PATH", cfgPath)
	t.Chdir(t.TempDir()) // CWD 与父目录都没有 config.yaml，逼出 CONFIG_PATH 生效

	cfg := loadAppConfigOnce()
	if cfg.Logging.Component != "via-config-path" {
		t.Fatalf("CONFIG_PATH 指向的配置未生效：Logging.Component = %q", cfg.Logging.Component)
	}
}
