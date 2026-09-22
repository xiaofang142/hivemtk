package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/system/install"
)

// 心跳读的是 install.lock。这份文件一旦坏着（写到一半被杀／盘上残留），
// 旧实现 Load 拿到大错误就直接 return：这台实例从此一帧心跳都不发，而且没有任何日志——
// 平台侧只看到"这台机器掉了"。修好之后：能自愈的必须自愈并照常上报。
func TestSendHeartbeatHealsCorruptLockAndStillReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.lock")
	// 半截 JSON：install_id 那一行是完整的，尾巴被砍在 admin_username 中间。
	half := `{"install_id":"ins-0123456789abcdef0123456789abcdef","install_time":"2026-01-01T00:00:00Z","admin_username":"ad`
	if err := os.WriteFile(path, []byte(half), 0o644); err != nil {
		t.Fatalf("铺损坏 install.lock 失败：%v", err)
	}
	t.Setenv("INSTALL_LOCK_PATH", path)
	install.SetAdminProbe(func(context.Context) (string, error) { return "admin", nil })
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	got := make(chan *ReportHeartbeatReq, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/heartbeat" {
			t.Errorf("心跳打到了 %q，want /api/platform/heartbeat", r.URL.Path)
		}
		var req ReportHeartbeatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("心跳体解不开：%v", err)
		}
		got <- &req
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	sendHeartbeat()

	select {
	case req := <-got:
		if req.InstallID != "ins-0123456789abcdef0123456789abcdef" {
			t.Errorf("损坏文件里的 install_id 必须保着报上来，实得 %q", req.InstallID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("install.lock 只是坏着，不许从此一帧心跳都不发")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("回读 install.lock 失败：%v", err)
	}
	var healed map[string]any
	if err := json.Unmarshal(raw, &healed); err != nil {
		t.Fatalf("心跳这条腿也要把坏文件修回去，盘上仍是坏 JSON：%v", err)
	}
	if healed["install_id"] != "ins-0123456789abcdef0123456789abcdef" {
		t.Errorf("回填后的 install_id 变了：实得 %v", healed["install_id"])
	}
}

// 全新未初始化（连 install.lock 都没有）时不发心跳——这是设计意图，不是被错误吞掉。
// 钉住它，免得"自愈"顺手把未初始化实例也往平台上报了个空身份。
func TestSendHeartbeatStaysSilentBeforeInstallIDExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.lock")
	t.Setenv("INSTALL_LOCK_PATH", path)
	install.SetAdminProbe(func(context.Context) (string, error) { return "", nil })
	t.Cleanup(func() { install.SetAdminProbe(nil) })

	var hits int32
	hitCh := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		hitCh <- struct{}{}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	sendHeartbeat()
	// Close 会等在线处理完的 handler，所以它返回后 hitCh 的条数就是确定值（不靠 sleep 猜）。
	srv.Close()

	select {
	case <-hitCh:
		t.Errorf("未初始化不该往平台发心跳，实发 %d 次", hits)
	default:
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("未初始化这条路上不该凭空造出 install.lock：%v", err)
	}
}
