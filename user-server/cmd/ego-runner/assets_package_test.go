package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeZipEntry(t *testing.T) {
	for _, p := range []string{"a/b.js", "a/manifest.json", "x/y/z.js"} {
		if !safeZipEntry(p) {
			t.Errorf("%q 应合法", p)
		}
	}
	for _, p := range []string{"", "../a", "a/../../b", "/abs", `a\b`, "a//b", "./a"} {
		if safeZipEntry(p) {
			t.Errorf("%q 应判非法", p)
		}
	}
}

func TestInstallFromZip_RejectsZipSlip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../evil/x.js")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()

	r := &runner{conf: &runnerConf{WorkspaceDir: t.TempDir()}}
	if _, _, err := r.installFromZip(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("zip-slip 条目未被拒")
	}
	if _, err := os.Stat(filepath.Join(r.conf.WorkspaceDir, "evil")); err == nil {
		t.Fatal("非法条目被写到了磁盘")
	}
}

func TestInstallFromZip_InstallsAndIsIdempotent(t *testing.T) {
	manifest := `{"id":"alpha","hivemtk":{"script_hash":"abc123"}}`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"alpha/manifest.json": manifest, "alpha/tools/a.js": "console.log(1)"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()

	r := &runner{conf: &runnerConf{WorkspaceDir: t.TempDir()}}
	installed, skipped, err := r.installFromZip(bytes.NewReader(buf.Bytes()))
	if err != nil || installed != 1 || skipped != 0 {
		t.Fatalf("首次安装 = (%d,%d,%v)，期望 (1,0,nil)", installed, skipped, err)
	}
	dir := filepath.Join(learningsDir(r.conf.WorkspaceDir), "alpha")
	if b, err := os.ReadFile(filepath.Join(dir, "tools", "a.js")); err != nil || !strings.Contains(string(b), "console.log") {
		t.Fatalf("入口脚本未落盘: %v", err)
	}
	// 再跑一次：hash 一致 ⇒ 跳过（幂等）
	installed, skipped, err = r.installFromZip(bytes.NewReader(buf.Bytes()))
	if err != nil || installed != 0 || skipped != 1 {
		t.Fatalf("重复安装 = (%d,%d,%v)，期望 (0,1,nil)", installed, skipped, err)
	}
}
