package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFilesMissingDir dir 不存在时返回 (0, nil)，不报错。
func TestLoadFilesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-exists")
	n, err := LoadFiles(dir)
	if err != nil {
		t.Fatalf("期望缺失目录不报错, got err=%v", err)
	}
	if n != 0 {
		t.Fatalf("期望注入 0 个, got %d", n)
	}
}

// TestLoadFilesTxtAndEnv 验证 *.txt 单值与 *.env 键值两种风格均可注入。
func TestLoadFilesTxtAndEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "LOADER_TEST_MASTER.txt"), []byte(strings.Repeat("m", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.env"), []byte("# 注释行\nLOADER_TEST_A=alpha\n\nexport LOADER_TEST_B=\"beta quoted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.md"), []byte("X=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOADER_TEST_A", "")
	os.Unsetenv("LOADER_TEST_A")
	os.Unsetenv("LOADER_TEST_B")
	os.Unsetenv("LOADER_TEST_MASTER")
	t.Cleanup(func() {
		os.Unsetenv("LOADER_TEST_MASTER")
		os.Unsetenv("LOADER_TEST_A")
		os.Unsetenv("LOADER_TEST_B")
	})

	n, err := LoadFiles(dir)
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	if n != 3 {
		t.Fatalf("期望注入 3 个, got %d", n)
	}
	if got := os.Getenv("LOADER_TEST_MASTER"); got != strings.Repeat("m", 40) {
		t.Fatalf("txt 注入错误: %q", got)
	}
	if got := os.Getenv("LOADER_TEST_A"); got != "alpha" {
		t.Fatalf("env 注入错误: %q", got)
	}
	if got := os.Getenv("LOADER_TEST_B"); got != "beta quoted" {
		t.Fatalf("env 引号剥离错误: %q", got)
	}
	if v, ok := os.LookupEnv("X"); ok && v == "1" {
		t.Fatal("非 .txt/.env 扩展名文件不应被解析")
	}
}

// TestLoadFilesNoOverwrite 已显式设置的 env 优先，加载器不覆盖。
func TestLoadFilesNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "LOADER_TEST_KEY.txt"), []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOADER_TEST_KEY", "from-env")

	n, err := LoadFiles(dir)
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	if n != 0 {
		t.Fatalf("已有 env 时不应注入, got %d", n)
	}
	if got := os.Getenv("LOADER_TEST_KEY"); got != "from-env" {
		t.Fatalf("env 被覆盖: %q", got)
	}
}
