package platform

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// withMerchantKey 隔离 sync.go 的进程级 merchantKey，用例结束后还原。
func withMerchantKey(t *testing.T) {
	t.Helper()
	old := merchantKey
	t.Cleanup(func() { merchantKey = old })
}

// TestInitSyncKeepsMerchantKeyAcrossBoots 两次「重启」必须得到同一个 merchant key，
// 且每次都把这个身份如实上报给平台。
//
// key 是贡献者账号（mtk_<key> + sha256(key|secret)）和平台侧商户记录的唯一锚点：
// 每次启动重掷一次就等于每装一次多一个商户、每次重启换一个人。
// 注册请求走真 httptest：InitSync 必须能在 PlatformCfg 已就位时把 key 发出去
// （main.go 早先的 InitSync-before-LoadPlatform 顺序让这一步从未成功过）。
func TestInitSyncKeepsMerchantKeyAcrossBoots(t *testing.T) {
	t.Setenv("MERCHANT_STATE_DIR", t.TempDir())
	withMerchantKey(t)

	type registration struct{ path, merchantKey string }
	registrations := make(chan registration, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case registrations <- registration{path: r.URL.Path, merchantKey: r.Header.Get("X-Merchant-Key")}:
		default:
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	// 先注册 → LIFO 后执行：srv 活着的时间必须覆盖下面等待请求的全过程
	t.Cleanup(srv.Close)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3cr3t"})

	if err := InitSync(); err != nil {
		t.Fatalf("首次 InitSync: %v", err)
	}
	first := GetMerchantKey()
	if first == "" {
		t.Fatal("首次 InitSync 后 merchantKey 为空")
	}
	if err := InitSync(); err != nil { // 模拟重启
		t.Fatalf("重启后 InitSync: %v", err)
	}
	if second := GetMerchantKey(); second != first {
		t.Errorf("merchantKey 重启后漂移: first=%q second=%q", first, second)
	}

	// 收齐两次注册再收尾：既证明身份真上报了，也让读 PlatformCfg 的协程先于配置还原结束
	// （否则 -race 会把它和 withPlatformConfig 的 cleanup 判成数据竞争）。
	for i := 0; i < 2; i++ {
		select {
		case reg := <-registrations:
			if reg.path != "/merchant-api/merchant/register" {
				t.Errorf("注册请求路径=%q", reg.path)
			}
			if reg.merchantKey != first {
				t.Errorf("第 %d 次上报的 X-Merchant-Key=%q，应为落盘身份 %q", i+1, reg.merchantKey, first)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("只等到 %d 次商户注册请求", i)
		}
	}
}

// TestLoadOrInitMerchantKeyWritesDurableFile 首启落盘的文件必须就是身份本身：
// 内容与返回值一致，且权限收到 0600（同目录还放着每商户签名密钥）。
func TestLoadOrInitMerchantKeyWritesDurableFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MERCHANT_STATE_DIR", dir)

	key, err := loadOrInitMerchantKey()
	if err != nil {
		t.Fatalf("loadOrInitMerchantKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key 长度=%d want 32", len(key))
	}
	b, err := os.ReadFile(filepath.Join(dir, ".merchant_key"))
	if err != nil {
		t.Fatalf("读取落盘文件: %v", err)
	}
	if string(b) != key {
		t.Errorf("落盘内容=%q 与返回值不符", b)
	}
	if fi, err := os.Stat(filepath.Join(dir, ".merchant_key")); err != nil {
		t.Fatalf("stat: %v", err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("key 文件权限=%o want 600", perm)
	}

	// 目录不同的两台实例必须是两个身份，否则共享状态盘会把它们并成一个商户
	t.Setenv("MERCHANT_STATE_DIR", t.TempDir())
	other, err := loadOrInitMerchantKey()
	if err != nil {
		t.Fatalf("第二个状态目录: %v", err)
	}
	if other == key {
		t.Error("不同状态目录不应得到同一个 merchant key")
	}
}

// TestLoadOrInitMerchantKeyFailClosed 状态文件坏掉时宁可没身份，也不静默另起一个身份：
// 一旦重新生成，原 key 就永久丢在平台侧，且两次读取之间还会各生成一个新号。
func TestLoadOrInitMerchantKeyFailClosed(t *testing.T) {
	// 1) 文件存在但内容为空（写中断的产物）
	empty := t.TempDir()
	p := filepath.Join(empty, ".merchant_key")
	if err := os.WriteFile(p, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("预置空文件: %v", err)
	}
	t.Setenv("MERCHANT_STATE_DIR", empty)
	if key, err := loadOrInitMerchantKey(); err == nil || key != "" {
		t.Errorf("空文件应 fail-closed, got key=%q err=%v", key, err)
	}

	// 2) 状态目录压根不是目录：MkdirAll 失败，首启也无法落盘
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("预置占位文件: %v", err)
	}
	t.Setenv("MERCHANT_STATE_DIR", filepath.Join(blocker, "nested"))
	if key, err := loadOrInitMerchantKey(); err == nil || key != "" {
		t.Errorf("状态目录不可用时应 fail-closed, got key=%q err=%v", key, err)
	}
}

// TestLoadOrInitMerchantKeyRegeneratesWhenAbsent 状态文件被清掉后重新生成一个身份并落盘：
// 这是「重装/迁移」的预期路径，与上面 fail-closed 的区别在于文件从来不存在。
func TestLoadOrInitMerchantKeyRegeneratesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MERCHANT_STATE_DIR", dir)

	first, err := loadOrInitMerchantKey()
	if err != nil {
		t.Fatalf("loadOrInitMerchantKey: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, ".merchant_key")); err != nil {
		t.Fatalf("删除 key 文件: %v", err)
	}
	second, err := loadOrInitMerchantKey()
	if err != nil {
		t.Fatalf("重新生成: %v", err)
	}
	if second == first {
		t.Error("重新生成应得到新 key（否则等于没落盘）")
	}
	if third, err := loadOrInitMerchantKey(); err != nil || third != second {
		t.Errorf("新生成的 key 未落盘: third=%q err=%v", third, err)
	}
}
