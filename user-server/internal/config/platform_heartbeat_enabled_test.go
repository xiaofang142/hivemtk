package config

import "testing"

// TestResolvePlatformHeartbeatEnabledDefaultOnAndUnknownSafe 钉死心跳细粒度开关（任务四）的三态语义。
//
// 与 PLATFORM_ENABLED 的方向刻意相反：
//   - 空值默认**开启**——心跳是运营侧唯一的装机/在线观测来源，"意外关闭"比"意外开启"危害大；
//   - 未知值（拼错）保守保持**开启**，但 known=false 让启动日志能说清"你拼错了"；
//   - 明确关闭（false/off/0/no）⇒ (false, true)。
//
// 该开关只在 PLATFORM_ENABLED=true 时有意义：关掉心跳后商户注册/资产市场/
// 模块下发照常（心跳在平台侧是纯统计口，不参与业务闸）。
func TestResolvePlatformHeartbeatEnabledDefaultOnAndUnknownSafe(t *testing.T) {
	offCases := []string{"false", "FALSE", " False ", "0", "off", "OFF", "no", "NO"}
	for _, v := range offCases {
		enabled, known := ResolvePlatformHeartbeatEnabled(v)
		if enabled {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 应判为关闭", v)
		}
		if !known {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 是已知关闭值，known 应为 true", v)
		}
	}

	onCases := []string{"", "  ", "true", "TRUE", " True ", "1", "on", "ON", "yes", "YES"}
	for _, v := range onCases {
		enabled, known := ResolvePlatformHeartbeatEnabled(v)
		if !enabled {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 应判为开启（空值默认开）", v)
		}
		if !known {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 是已知开启值（含未设置），known 应为 true", v)
		}
	}

	// 拼错：保守保持开启（与 PLATFORM_ENABLED 的"拼错朝关"方向相反），
	// 但 known=false 供调用方打 Warn 说清真相
	typoCases := []string{"tru", "ture", "TRUEE", "1x", "enable", "enabled", "yes please", "on1"}
	for _, v := range typoCases {
		enabled, known := ResolvePlatformHeartbeatEnabled(v)
		if !enabled {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 拼错时保守保持开启（心跳意外关闭危害更大）", v)
		}
		if known {
			t.Errorf("PLATFORM_HEARTBEAT_ENABLED=%q 是拼错值，known 应为 false", v)
		}
	}
}

// TestPlatformHeartbeatEnabledMatchesResolve 保证布尔入口与三态入口同判真结果。
func TestPlatformHeartbeatEnabledMatchesResolve(t *testing.T) {
	t.Setenv("PLATFORM_HEARTBEAT_ENABLED", "")
	if !PlatformHeartbeatEnabled() {
		t.Fatal("未设置时 PlatformHeartbeatEnabled 必须为 true（默认开启）")
	}
	t.Setenv("PLATFORM_HEARTBEAT_ENABLED", "false")
	if PlatformHeartbeatEnabled() {
		t.Fatal("显式 false 时 PlatformHeartbeatEnabled 必须为 false")
	}
	t.Setenv("PLATFORM_HEARTBEAT_ENABLED", "TRUE")
	if !PlatformHeartbeatEnabled() {
		t.Fatal("TRUE 时 PlatformHeartbeatEnabled 必须为 true")
	}
}
