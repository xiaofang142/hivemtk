package mcp

// rediscover_test.go P1-3 回归：关→开一次之后 tool_count 不能掉成 0。
//
// API 冒烟抓到的真缺陷：DiscoverAndRegister 把「工具已注册」一律当失败跳过，
// 于是重复发现（重新启用 / 启动期再次接入）返回 0，前端读到「0 个工具」而工具其实可用。
// 判据：对同一 server 连续发现两次，第二次仍应报告可用工具数 = 上游 tools/list 条数。

import (
	"context"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

func TestDiscoverAndRegister_SecondDiscoveryStillCounts(t *testing.T) {
	withTestTransport(t)
	upstream := newUpstreamMCPServer(t)
	defer upstream.Close()

	reg := tooluse.NewToolRegistry()
	cfg := ClientConfig{Name: "fs", Endpoint: upstream.URL, Timeout: 5e9}

	first, err := DiscoverAndRegister(context.Background(), cfg, reg)
	if err != nil {
		t.Fatalf("首次发现失败: %v", err)
	}
	if first != 1 {
		t.Fatalf("首次发现工具数 = %d want 1", first)
	}
	second, err := DiscoverAndRegister(context.Background(), cfg, reg)
	if err != nil {
		t.Fatalf("二次发现失败: %v", err)
	}
	if second != first {
		t.Errorf("二次发现工具数 = %d want %d（重复注册被当成失败 ⇒ 前端显示 0）", second, first)
	}
	if _, err := reg.Get("mcp_fs_echo"); err != nil {
		t.Errorf("工具应仍可用: %v", err)
	}
}
