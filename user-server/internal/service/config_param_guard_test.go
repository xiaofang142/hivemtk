package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// D12 守卫：禁止新增对两套遗留 KV 的直查（新配置一律走 ConfigParamService）。
// 白名单 = 既有合法引用（model/repo 定义、遗留 seed/读取、迁移、测试文件）。
func TestD12_NoNewLegacyKVDirectQuery(t *testing.T) {

	whitelist := map[string]bool{
		// 经 SystemConfigKVRepository.Get 读回调密钥，无裸 SQL：与同类入站守卫
		// bridge_ingress_guard.go 同规格。本守卫拦的是新写的裸 KV 直查。
		"webhook_signature.go":       true,
		"provider_failover.go":       true,
		"config_param.go":            true,
		"intent.go":                  true,
		"bridge_ingress_guard.go":    true,
		"aftersale.go":               true,
		"agent_co_pilot.go":          true,
		"agent_settings_config.go":   true,
		"asset_bundle.go":            true,
		"bridge_token.go":            true,
		"handoff_chain.go":           true,
		"intent_recognition.go":      true,
		"kb_connectors.go":           true,
		"password_policy.go":         true,
		"reach_pipeline.go":          true,
		"sales_engine.go":            true,
		"script_ab.go":               true,
		"tool_integration_config.go": true,
	}
	skipDirs := map[string]bool{
		"migration": true,
	}
	root := "../"
	var violations []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		base := filepath.Base(path)
		if whitelist[base] || strings.Contains(path, "system_config_kv") || strings.Contains(path, "system_kv_config") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		content := string(data)
		if strings.Contains(content, "FROM system_kv_config") || strings.Contains(content, "system_config_kv") {
			if strings.Contains(content, "禁止新增") || strings.Contains(content, "D12") {
				return nil
			}
			violations = append(violations, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Errorf("发现新增遗留 KV 直查（新配置请走 ConfigParamService）: %v", violations)
	}
}

// D12: TTL 过期回源 + 失败回滚 loaded 修复
func TestD12_TTLExpiryReloads(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Skip("无测试库")
	}
	svc := NewConfigParamService(db)
	now := time.Now()
	svc.nowFn = func() time.Time { return now }

	if err := SeedConfigParams(context.Background(), db); err != nil {
		t.Skipf("seed 不可用（schema 环境）: %v", err)
	}

	group, key := "d12test", "ttlkey"
	putParam := func(v string) {
		if err := db.Exec(`DELETE FROM config_params WHERE param_group = ? AND key = ?`, group, key).Error; err != nil {
			t.Fatalf("del param: %v", err)
		}
		if err := db.Exec(`INSERT INTO config_params (param_group, key, param_value, value_type, name, description, default_value, min, max, created_at, updated_at)
			VALUES (?, ?, ?, 'string', 'D12', 'D12 test', ?, '', '', NOW(), NOW())`,
			group, key, v, v).Error; err != nil {
			t.Fatalf("put param: %v", err)
		}
	}
	putParam("v1")
	if got := svc.GetString(context.Background(), group, key, "fallback"); got != "v1" {
		t.Fatalf("首读应 v1, got %s", got)
	}

	putParam("v2")
	if got := svc.GetString(context.Background(), group, key, "fallback"); got != "v1" {
		t.Fatalf("未过期应读缓存 v1, got %s", got)
	}

	now = now.Add(61 * time.Second)
	if got := svc.GetString(context.Background(), group, key, "fallback"); got != "v2" {
		t.Fatalf("过期后应回源 v2, got %s", got)
	}
}
