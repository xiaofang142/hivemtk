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
