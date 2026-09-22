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
admin_password: test_password
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
	if PlatformCfg.AdminPassword != "test_password" {
		t.Errorf("Expected AdminPassword 'test_password', got %s", PlatformCfg.AdminPassword)
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

// TestLoadPlatform_MissingPassword 验证：管理员密码缺失时必须返回错误（合规基线 §7.2）
func TestLoadPlatform_MissingPassword(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://test.example.com
secret: test_secret
admin_username: admin
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	err = LoadPlatform(configPath)
	if err == nil {
		t.Fatal("Expected error for missing admin_password, got nil")
	}
}

// TestLoadPlatform_MissingSecret 验证：商户 API 密钥缺失时必须返回错误（合规基线 §7.2）
func TestLoadPlatform_MissingSecret(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true") // 本组用例验的是"开启态"的加载与必填校验；默认关态会在 LoadPlatform 早退
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://test.example.com
admin_password: test_password
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
	t.Setenv("TEST_PLATFORM_ADMIN_PW", "env_injected_password")
	t.Setenv("TEST_PLATFORM_SECRET", "env_injected_secret")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "platform.yaml")

	configContent := `
api_url: http://env.example.com
secret: "${TEST_PLATFORM_SECRET}"
admin_password: "${TEST_PLATFORM_ADMIN_PW}"
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
	if PlatformCfg.AdminPassword != "env_injected_password" {
		t.Errorf("Expected AdminPassword 'env_injected_password', got %s", PlatformCfg.AdminPassword)
	}
}
