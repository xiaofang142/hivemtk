package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// probeEgoVersion 必须能读到打到 stderr 的版本（F11 实测 ego-browser 的行为），
// 并剥掉 "ego-browser" 前缀；探不到如实报 unknown。
func TestProbeEgoVersionReadsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		// A-6：#!/bin/sh 脚本在 Windows CreateProcess 下无 shebang 语义，
		// 子进程起不来 → probeEgoVersion 恒 "unknown"——测的是 OS 而非代码。
		// 生产逻辑本身有优雅降级（unknown 即降级路径），POSIX-only 断言跳过。
		t.Skip("shebang 脚本仅在 POSIX 可执行；Windows 覆盖见 broken-ego 分支同语义")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ego")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'ego-browser 9.9.9' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := probeEgoVersion(script); got != "9.9.9" {
		t.Fatalf("stderr 版本应被读到并剥前缀, got %q", got)
	}

	broken := filepath.Join(dir, "broken-ego")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := probeEgoVersion(broken); got != "unknown" {
		t.Fatalf("探测失败应报 unknown, got %q", got)
	}
}

// TestProbeEgoVersionBrokenReportsUnknown 覆盖跨平台都成立的失败分支：
// 探测一个必然不存在的可执行文件，CombinedOutput 必报错 → unknown。
func TestProbeEgoVersionBrokenReportsUnknown(t *testing.T) {
	if got := probeEgoVersion(filepath.Join(t.TempDir(), "no-such-ego-bin")); got != "unknown" {
		t.Fatalf("不可执行目标应报 unknown, got %q", got)
	}
}
