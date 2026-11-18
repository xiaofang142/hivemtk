package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/system/install"
)

// 任务二/任务三验收测试：
//   1. 心跳必须携带 deploy_mode（saas|offline），平台侧据此区分两类部署形态；
//   2. SaaS 形态下心跳附带 client_machine（商户正在使用的电脑快照，浏览器信标采集）；
//   3. 离线单机形态下不附带 client_machine（部署机=服务器机，host_info 已是商户机器信息）；
//   4. 过期快照（超过 clientDeviceTTL）不再附带，防止把几天前的访客电脑当"正在使用"。

func withInstallLock(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install.lock")
	lock := `{"install_id":"ins-deploymode0123456789abcdef012345","install_time":"2026-01-01T00:00:00Z","admin_username":"admin"}`
	if err := os.WriteFile(path, []byte(lock), 0o644); err != nil {
		t.Fatalf("铺 install.lock 失败：%v", err)
	}
	t.Setenv("INSTALL_LOCK_PATH", path)
	install.SetAdminProbe(func(context.Context) (string, error) { return "admin", nil })
	t.Cleanup(func() { install.SetAdminProbe(nil) })
}

func captureHeartbeat(t *testing.T) chan *ReportHeartbeatReq {
	t.Helper()
	got := make(chan *ReportHeartbeatReq, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ReportHeartbeatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("心跳体解不开：%v", err)
		}
		got <- &req
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})
	return got
}

func withTenantMode(t *testing.T, m tenant.Mode) {
	t.Helper()
	prev := tenant.CurrentMode()
	tenant.SetMode(m)
	t.Cleanup(func() { tenant.SetMode(prev) })
}

func waitHeartbeat(t *testing.T, got chan *ReportHeartbeatReq) *ReportHeartbeatReq {
	t.Helper()
	select {
	case req := <-got:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("等心跳超时")
		return nil
	}
}

// SaaS 形态：deploy_mode=saas 且携带商户电脑快照（ip/platform 来自信标）。
func TestHeartbeatSaaSCarriesDeployModeAndClientMachine(t *testing.T) {
	withInstallLock(t)
	withTenantMode(t, tenant.ModeOn)
	SetClientDeviceSnapshot(&ClientDeviceSnapshot{
		IP:         "203.0.113.7",
		UA:         "Mozilla/5.0 test",
		Platform:   "Win32",
		Screen:     "1920x1080",
		Timezone:   "Asia/Shanghai",
		Language:   "zh-CN",
		CapturedAt: time.Now(),
	})
	t.Cleanup(func() { clientDeviceLatest = nil })

	got := captureHeartbeat(t)
	sendHeartbeat()
	req := waitHeartbeat(t, got)

	if req.DeployMode != "saas" {
		t.Errorf("deploy_mode = %q, want saas", req.DeployMode)
	}
	if req.ClientMachine == nil {
		t.Fatal("SaaS 心跳必须携带 client_machine（商户电脑快照）")
	}
	var cm ClientDeviceSnapshot
	if err := json.Unmarshal(req.ClientMachine, &cm); err != nil {
		t.Fatalf("client_machine 解不开：%v", err)
	}
	if cm.IP != "203.0.113.7" || cm.Platform != "Win32" {
		t.Errorf("client_machine 内容不符：ip=%q platform=%q", cm.IP, cm.Platform)
	}
}

// 离线单机形态：deploy_mode=offline，且即使有快照也不带 client_machine。
func TestHeartbeatOfflineCarriesDeployModeWithoutClientMachine(t *testing.T) {
	withInstallLock(t)
	withTenantMode(t, tenant.ModeOff)
	SetClientDeviceSnapshot(&ClientDeviceSnapshot{
		IP:         "203.0.113.9",
		Platform:   "MacIntel",
		CapturedAt: time.Now(),
	})
	t.Cleanup(func() { clientDeviceLatest = nil })

	got := captureHeartbeat(t)
	sendHeartbeat()
	req := waitHeartbeat(t, got)

	if req.DeployMode != "offline" {
		t.Errorf("deploy_mode = %q, want offline", req.DeployMode)
	}
	if req.ClientMachine != nil {
		t.Errorf("离线单机部署不应携带 client_machine（部署机=服务器机），实得 %s", string(req.ClientMachine))
	}
}

// 过期快照不随心跳上报：商户几天没开页面时，不能把旧快照当"正在使用的电脑"。
func TestHeartbeatSkipsStaleClientMachine(t *testing.T) {
	withInstallLock(t)
	withTenantMode(t, tenant.ModeOn)
	SetClientDeviceSnapshot(&ClientDeviceSnapshot{
		IP:         "203.0.113.8",
		Platform:   "Win32",
		CapturedAt: time.Now().Add(-clientDeviceTTL - time.Minute),
	})
	t.Cleanup(func() { clientDeviceLatest = nil })

	if FreshClientDeviceSnapshot() != nil {
		t.Fatal("超过 TTL 的快照必须视为过期")
	}

	got := captureHeartbeat(t)
	sendHeartbeat()
	req := waitHeartbeat(t, got)

	if req.DeployMode != "saas" {
		t.Errorf("deploy_mode = %q, want saas", req.DeployMode)
	}
	if req.ClientMachine != nil {
		t.Errorf("过期快照不应随心跳上报，实得 %s", string(req.ClientMachine))
	}
}

// client_machine 字段必须真的出现在 JSON body 里（omitempty 口径验证）：
// 离线心跳的 wire format 不应出现 client_machine 键，平台侧老版本兼容不靠字段存在性，
// 但显式验证可防止未来有人把 omitempty 改丢、造成平台端空对象覆盖。
func TestHeartbeatWireFormatOmitsClientMachineWhenAbsent(t *testing.T) {
	withInstallLock(t)
	withTenantMode(t, tenant.ModeOff)
	t.Cleanup(func() { clientDeviceLatest = nil })

	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(rawBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	sendHeartbeat()
	if strings.Contains(string(rawBody), "client_machine") {
		t.Errorf("无快照时 wire format 不应出现 client_machine 键：%s", string(rawBody))
	}
	if !strings.Contains(string(rawBody), `"deploy_mode":"offline"`) {
		t.Errorf("wire format 应包含 deploy_mode=offline：%s", string(rawBody))
	}
}
