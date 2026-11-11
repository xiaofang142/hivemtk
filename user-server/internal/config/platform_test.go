package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPlatform(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	// 环境隔离:LoadPlatform 的环境变量覆盖优先级高于配置文件,
	// 部署环境(.env)注入 PLATFORM_API_HOST/PLATFORM_API_URL/PLATFORM_CONFIG_PATH 时
	// 若不隔离,本测试在任何真实部署机上必然失败(断言文件值而非环境值)
	t.Setenv("PLATFORM_CONFIG_PATH", "")
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://test.example.com
secret: test_secret
log_report_interval: 30
license_sync_interval: 60
admin_username: admin
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	err = LoadPlatform(configPath)
	if err != nil {
		t.Fatalf("LoadPlatform failed: %v", err)
	}

	if PlatformCfg == nil {
		t.Fatal("PlatformCfg is nil after loading")
	}
	if PlatformCfg.APIURL != "http://test.example.com" {
		t.Errorf("Expected APIURL 'http://test.example.com', got %s", PlatformCfg.APIURL)
	}
	if PlatformCfg.Secret != "test_secret" {
		t.Errorf("Expected Secret 'test_secret', got %s", PlatformCfg.Secret)
	}
	if PlatformCfg.LogReportInterval != 30 {
		t.Errorf("Expected LogReportInterval 30, got %d", PlatformCfg.LogReportInterval)
	}
	if PlatformCfg.LicenseSyncInterval != 60 {
		t.Errorf("Expected LicenseSyncInterval 60, got %d", PlatformCfg.LicenseSyncInterval)
	}
}

func TestLoadPlatform_NonExistentFile(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	err := LoadPlatform("/non/existent/path.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadPlatform_InvalidYAML(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
invalid: yaml: content
  - broken
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	err = LoadPlatform(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

// TestLoadPlatform_MissingPassword 已删（2026-11-11 L-1 方案A）：admin_password 不再是
// 必填字段，LoadPlatform 不再校验它，原测试断言的报错分支已不存在。

// TestLoadPlatform_MissingSecret 验证：商户 API 密钥缺失时必须返回错误（合规基线 §7.2）
func TestLoadPlatform_MissingSecret(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://test.example.com
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	err = LoadPlatform(configPath)
	if err == nil {
		t.Fatal("Expected error for missing secret, got nil")
	}
}

// TestLoadPlatform_EnvExpansion 验证：${VAR} 形式的环境变量能被正确展开
func TestLoadPlatform_EnvExpansion(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	t.Setenv("TEST_PLATFORM_SECRET", "env_injected_secret")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://env.example.com
secret: "${TEST_PLATFORM_SECRET}"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	err = LoadPlatform(configPath)
	if err != nil {
		t.Fatalf("LoadPlatform failed: %v", err)
	}
	if PlatformCfg.Secret != "env_injected_secret" {
		t.Errorf("Expected Secret 'env_injected_secret', got %s", PlatformCfg.Secret)
	}
}

// TestLoadPlatform_FallsBackToExecutableDir 钉住「CWD 不是二进制目录时也能读到平台配置」。
//
// 背景（2026-10-23 双部署起服实测）：从仓库根起 user-server 时 CWD 下没有
// config/platform.yaml ⇒ 读文件失败 ⇒ PlatformCfg=nil ⇒ 商户注册/心跳/资产市场/模块下发
// 全部静默哑掉，日志只有一行 error。config/ 是随二进制一起分发的，二进制旁边那份就是
// 运维真正部署的那份，所以默认相对路径读不到时必须按二进制目录再找一次。
func TestLoadPlatform_FallsBackToExecutableDir(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_CONFIG_PATH", "")
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")

	exeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(exeDir, "config"), 0o755); err != nil {
		t.Fatalf("建 config 目录失败: %v", err)
	}
	content := `
api_url: https://exe-dir.example.com
secret: exe_dir_secret
`
	if err := os.WriteFile(filepath.Join(exeDir, "config", "platform.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("写二进制旁配置失败: %v", err)
	}
	orig := platformConfigRootDir
	platformConfigRootDir = func() string { return exeDir }
	t.Cleanup(func() { platformConfigRootDir = orig })

	// CWD 换到没有 config/ 的空目录，复现「从别处起二进制」
	t.Chdir(t.TempDir())

	if err := LoadPlatform(defaultPlatformConfigPath); err != nil {
		t.Fatalf("CWD 无 config/ 时应回落到二进制目录，实际报错: %v", err)
	}
	if PlatformCfg == nil || PlatformCfg.APIURL != "https://exe-dir.example.com" {
		t.Fatalf("回落读到的不是二进制旁那份配置：%+v", PlatformCfg)
	}
}

// TestLoadPlatform_NoFallbackWhenPathExplicit 钉住回落的边界：显式路径找不到就报错，
// 不许猜（否则"路径写错"会变成"读到了另一份配置"，比直接报错更难查）。
func TestLoadPlatform_NoFallbackWhenPathExplicit(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_CONFIG_PATH", "")

	exeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(exeDir, "config"), 0o755); err != nil {
		t.Fatalf("建 config 目录失败: %v", err)
	}
	content := `
api_url: https://exe-dir.example.com
secret: exe_dir_secret
`
	if err := os.WriteFile(filepath.Join(exeDir, "config", "platform.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("写二进制旁配置失败: %v", err)
	}
	orig := platformConfigRootDir
	platformConfigRootDir = func() string { return exeDir }
	t.Cleanup(func() { platformConfigRootDir = orig })

	if err := LoadPlatform(filepath.Join(t.TempDir(), "typo-platform.yaml")); err == nil {
		t.Fatal("显式路径不存在时必须报错，不能回落到二进制目录那份配置")
	}
}
