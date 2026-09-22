package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewSystemStatsService(t *testing.T) {
	svc := NewSystemStatsService()
	if svc == nil {
		t.Error("Expected non-nil SystemStatsService")
	}
}

func TestSystemStatsService_GetSystemInfo(t *testing.T) {
	svc := NewSystemStatsService()
	info, err := svc.GetSystemInfo()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if info == nil {
		t.Error("Expected non-nil SystemInfo")
	}
	if info.GoVersion == "" {
		t.Error("Expected non-empty GoVersion")
	}
	if info.Hostname == "" {
		t.Error("Expected non-empty Hostname")
	}
}

// TestSystemInfoCarriesPlatformEnabled 把"平台集成是否启用"透给前端，供其隐藏上架入口。
//
// 两个方向都要断言：只断言关态 false 的话，字段被写死成常量也是绿的。
// JSON 名必须是 platform_enabled——前端 Playground.vue 的 v-if 直接认这个名字。
func TestSystemInfoCarriesPlatformEnabled(t *testing.T) {
	svc := NewSystemStatsService()

	t.Setenv("PLATFORM_ENABLED", "false")
	off, err := svc.GetSystemInfo()
	if err != nil {
		t.Fatalf("GetSystemInfo: %v", err)
	}
	if off.PlatformEnabled {
		t.Fatal("关态 platform_enabled 必须为 false，否则前端会露出打不通的上架入口")
	}
	b, _ := json.Marshal(off)
	if !strings.Contains(string(b), `"platform_enabled":false`) {
		t.Fatalf("JSON 字段名必须是 platform_enabled，实际=%s", b)
	}

	t.Setenv("PLATFORM_ENABLED", "true")
	on, err := svc.GetSystemInfo()
	if err != nil {
		t.Fatalf("GetSystemInfo(开态): %v", err)
	}
	if !on.PlatformEnabled {
		t.Fatal("开态 platform_enabled 必须为 true，否则上架入口永久隐藏")
	}
}
