package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/platform"
)

// TestReportUsageBestEffortDisabledSendsNothing 是"无授权/无平台也照常跑"的那条锁：
// 平台集成关闭时，资产运行时命中后的使用上报必须一个请求都不发、一个协程都不起。
//
// 计数靶开在关态两侧都能连通，所以"0 次"不是靶子坏了造成的假绿——
// 同一个靶在开启态必须打到 1 次（正向对照，见下）。
func TestReportUsageBestEffortDisabledSendsNothing(t *testing.T) {
	var got int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&got, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	defer target.Close()

	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: target.URL, Secret: "s", AdminPassword: "p"}
	t.Setenv("MERCHANT_API_SECRET", "s")
	t.Setenv("PLATFORM_MERCHANT_KEY", "mk-test")

	// 关态：ReportUsageBestEffort 必须在发请求之前就走掉，即使 PlatformCfg 里
	// 明明装着一个可达地址（真实关态下它是 nil，这里刻意给一个，
	// 才能证明"早退"靠的是开关而不是碰巧没配置）。
	t.Setenv("PLATFORM_ENABLED", "false")
	ReportUsageBestEffort("asset-x")
	if n := atomic.LoadInt64(&got); n != 0 {
		t.Fatalf("关态必须零出站请求，实际打到 %d 次", n)
	}

	// 正向对照：同一个靶、同一份配置，开启态必须真的打出去 1 次。
	t.Setenv("PLATFORM_ENABLED", "true")
	ReportUsageBestEffort("asset-x")
	if n := atomic.LoadInt64(&got); n != 1 {
		t.Fatalf("开态应真的发出 1 次上报（否则说明靶子或配置没接通，上面的 0 次是假绿），实际 %d 次", n)
	}
}

// TestSubmitToPlatformDisabledEarlyReturns 关态下上架链路必须在构造 contributor
// 客户端之前就返回哨兵，绝不去登录平台、也不把 bundle 内容往外发。
func TestSubmitToPlatformDisabledEarlyReturns(t *testing.T) {
	var got int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&got, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	defer target.Close()

	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: target.URL, Secret: "s", AdminPassword: "p"}
	t.Setenv("PLATFORM_ENABLED", "false")

	svc := NewAssetBundleService(nil, nil)
	// 仓储刻意传 nil：开关判定必须发生在查库之前，否则这里就是 nil 解引用 panic，
	// 正好把"早退没早退"暴露成一条响亮的红。
	_, err := svc.SubmitToPlatform(context.Background(), "asset-that-does-not-matter")
	if err == nil {
		t.Fatal("关态 SubmitToPlatform 必须报错，不得静默成功")
	}
	if !errors.Is(err, platform.ErrPlatformNotConfigured) {
		t.Fatalf("关态 SubmitToPlatform 必须返回平台未启用哨兵，实际=%v", err)
	}
	if n := atomic.LoadInt64(&got); n != 0 {
		t.Fatalf("关态上架必须零出站请求，实际打到 %d 次", n)
	}
}
