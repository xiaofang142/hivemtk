package main

import (
	"os"
	"path/filepath"
	"testing"
)

// probeEgoVersion 必须能读到打到 stderr 的版本（F11 实测 ego-browser 的行为），
// 并剥掉 "ego-browser" 前缀；探不到如实报 unknown。
func TestProbeEgoVersionReadsStderr(t *testing.T) {
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
