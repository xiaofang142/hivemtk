package hostagent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseConf 钉住两个宿主进程共用的 key=value 语义：
// 空行/注释跳过、键值各去一次首尾空白、无等号行跳过、重复键后者胜。
func TestParseConf(t *testing.T) {
	data := []byte(`# 注释行
server_url = ws://127.0.0.1:8204
token=eg_1_abc

loopback_only = true
no-equals-line
dup=1
dup=2
`)
	got := ParseConf(data)
	want := map[string]string{
		"server_url":    "ws://127.0.0.1:8204",
		"token":         "eg_1_abc",
		"loopback_only": "true",
		"dup":           "2",
	}
	if len(got) != len(want) {
		t.Fatalf("键数 = %d，期望 %d（got=%v）", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q，期望 %q", k, got[k], v)
		}
	}
}

// TestReadConfFileMissing 文件不存在 ⇒ 空 map + nil error（缺项由调用方决定兜底）。
func TestReadConfFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "not-exist.conf")
	conf, err := ReadConfFile(p)
	if err != nil {
		t.Fatalf("不存在应返回 nil error，实际 %v", err)
	}
	if len(conf) != 0 {
		t.Fatalf("期望空 map，实际 %v", conf)
	}
}

// TestReadConfFile 真实文件读回；DefaultConfPath 在能拿到 home 时以 ~/.hivemtk 结尾。
func TestReadConfFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.conf")
	if err := os.WriteFile(p, []byte("a=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conf, err := ReadConfFile(p)
	if err != nil || conf["a"] != "1" {
		t.Fatalf("ReadConfFile = (%v,%v)，期望 a=1", conf, err)
	}
	if got := DefaultConfPath("nm_host.conf"); got != "" && filepath.Base(got) != "nm_host.conf" {
		t.Fatalf("DefaultConfPath 形状不对: %s", got)
	}
}
