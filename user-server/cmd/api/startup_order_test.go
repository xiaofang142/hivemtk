package main

import (
	"os"
	"strings"
	"testing"
)

// TestPlatformConfigLoadedBeforeInitSync 启动顺序：config.LoadPlatform 必须先于 platform.InitSync。
//
// InitSync 会用 SafeGo 拉起商户注册协程，注册请求要读 config.PlatformCfg；
// 先跑 InitSync 等于每次启动都让首个注册撞上「平台配置未初始化」，
// 而且协程读该全局变量与 LoadPlatform 写它构成数据竞争。
func TestPlatformConfigLoadedBeforeInitSync(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	s := string(src)
	load := strings.Index(s, "config.LoadPlatform(")
	initSync := strings.Index(s, "platform.InitSync()")
	if load < 0 || initSync < 0 {
		t.Fatalf("启动序列口径已失效: LoadPlatform offset=%d, InitSync offset=%d", load, initSync)
	}
	if load > initSync {
		t.Errorf("InitSync(偏移 %d) 早于 LoadPlatform(偏移 %d)，商户注册永远拿不到平台配置", initSync, load)
	}
}

// TestPlatformAssemblyBehindEnabledGuard 平台集成的整块装配必须收在
// config.PlatformEnabled() 开关后面（2026-09-21 起平台端是可选本地组件，默认关闭）。
//
// 判据是偏移量先后：开关判定必须出现在 LoadPlatform / InitSync / StartHeartbeat
// 之前。若有人把这三步挪回开关外（或删掉开关），默认关态下就会重新出现
// "启动即朝平台发请求 + 每 3 分钟刷一条 Error 日志"。
// 本仓既有的启动顺序门就是这个写法，沿用不另起炉灶。
func TestPlatformAssemblyBehindEnabledGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	s := string(src)
	guard := strings.Index(s, "config.PlatformEnabled()")
	if guard < 0 {
		t.Fatal("main.go 里已找不到 config.PlatformEnabled() 判定：平台集成开关被摘掉了")
	}
	for _, step := range []struct {
		name  string
		probe string
	}{
		{"config.LoadPlatform", "config.LoadPlatform("},
		{"platform.InitSync", "platform.InitSync()"},
		{"platform.StartHeartbeat", "platform.StartHeartbeat("},
	} {
		at := strings.Index(s, step.probe)
		if at < 0 {
			t.Errorf("%s 已从 main.go 消失（%s）", step.name, step.probe)
			continue
		}
		if at < guard {
			t.Errorf("%s(偏移 %d) 早于开关判定(偏移 %d)，关态下它仍会执行", step.name, at, guard)
		}
	}
}

// TestInstallStatusInitOutsidePlatformGuard 安装态检查器必须在开关**之外**无条件装配。
//
// 这条比"先后顺序"更要紧：InitGuard 中间件拿的是全局单例，
// nil 时的行为是 c.Next() 直接放行（见 init_guard.go）。也就是说，一旦有人把
// InitInstallStatus 顺手塞进 else 分支，默认关态下整个系统就变成
// "未初始化也照样放行所有 API"——把最该拦的那道门拆了，且不会有任何报错。
// 判据用花括号配对算出 if/else 整条语句的结束位置，而不是"晚于 StartHeartbeat"：
// 放在 else 分支里 StartHeartbeat 之后，偏移同样更大，却正是本条要拦的形态。
func TestInstallStatusInitOutsidePlatformGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	s := string(src)

	init := strings.Index(s, "middleware.InitInstallStatus()")
	if init < 0 {
		t.Fatal("main.go 里没有 middleware.InitInstallStatus()：安装态检查器未装配")
	}
	if n := strings.Count(s, "middleware.InitInstallStatus("); n != 1 {
		t.Fatalf("InitInstallStatus 应只装配一次，实际 %d 次", n)
	}
	if strings.Contains(s, "InitLicenseChecker") {
		t.Error("main.go 仍引用 InitLicenseChecker：授权时代的检查器应已改名")
	}

	guard := strings.Index(s, "if !config.PlatformEnabled() {")
	if guard < 0 {
		t.Fatal("main.go 里已找不到平台开关的 if 语句，结束位置无从计算")
	}
	depth := 0
	end := -1
	for i := guard + len("if !config.PlatformEnabled()"); i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth > 0 {
				continue
			}
			// depth 归零有两种：`if {…} else {…}` 的中途，和整条语句的结尾。
			// 只有紧跟 else 时才是前者，必须继续往下配对。
			if rest := strings.TrimLeft(s[i+1:], " \t\n\r"); strings.HasPrefix(rest, "else") {
				continue
			}
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatal("平台开关的 if/else 花括号未配对，判据本身失效")
	}
	if init < end {
		t.Errorf("InitInstallStatus(偏移 %d) 落在平台开关语句内部(结束于 %d)：关态下安装态检查器会是 nil，InitGuard 会放行全部 API", init, end)
	}
}

// TestCollectionJobMountedAfterRouterSetup 催收腿必须装进启动路径，而且必须排在
// router.Setup **之后**。
//
// 两件事各有各的红法，所以同一格里都判：
//   - 没装配（第一处 Fatal）：`CollectionJob` 的全部判据只在 service 包的假件上跑过，
//     生产里一条逾期单都不会被催。这一种破坏编译照过、全套用例照绿 —— 判定 A 的原文形状，
//     所以启动路径上有没有这一行必须单独成为一条断言，而不是靠台账（台账也是这一族，
//     但台账跑在 CI 的另一步，用例这一层也得有一份）。
//   - 装配但排在 router.Setup 之前：不 panic、不报错，只留一条"依赖未成立 ⇒ 不装门"的告警。
//     于是催收提醒这条**对客户外发**的路径静默地不受审批约束，而账单页与日志都一切正常。
//     顺序在这里是功能的一部分，不是可挪动的细节（与挽回 worker 那一条同一学费）。
func TestCollectionJobMountedAfterRouterSetup(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go: %v", err)
	}
	s := string(src)

	const mount = "app.InitCollectionRuntime("
	init := strings.Index(s, mount)
	if init < 0 {
		t.Fatal("main.go 里没有 app.InitCollectionRuntime ⇒ 催收腿不在启动路径上：" +
			"逾期应收不会被提醒、不会升级人工，而这件事在编译与用例面上全都看不出来")
	}
	if n := strings.Count(s, mount); n != 1 {
		t.Fatalf("催收腿装配应只发生一次，实际 %d 次（多装一台就多一台在扫同一批逾期单）", n)
	}
	setup := strings.Index(s, "router.Setup(r")
	if setup < 0 {
		t.Fatal("main.go 里已找不到 router.Setup(r：本条判据的参照物消失了")
	}
	if init < setup {
		t.Errorf("催收腿装配(偏移 %d)早于 router.Setup(偏移 %d) ⇒ AttachReachGate 拿不到 W-1 的 checker，"+
			"这条外发路径不受审批约束，而日志里只有一行告警", init, setup)
	}

	// 返回的那一台必须被 Stop：off 档下 InitCollectionRuntime 也会返回实例，
	// defer 只在非 nil 时挂，所以"没挂 Stop"是一种独立的漏法（关停时协程还在发）。
	if n := strings.Count(s, "collectionJob.Stop(context.Background())"); n != 1 {
		t.Errorf("collectionJob.Stop 出现 %d 次，期望 1 ⇒ 优雅关停时这一台还在跑下一轮", n)
	}
}
