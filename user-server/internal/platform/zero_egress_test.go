package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/system/install"
)

// recordingTransport 是挂在 http.DefaultTransport 上的“海关记录仪”。
// 本包所有出站客户端都是 &http.Client{Timeout:...}（Transport=nil → 求值时取
// http.DefaultTransport），所以任何平台请求要出网都必须先过这里；
// 记账后立即回错——证明期间不放行、也不依赖真实网络。
type recordingTransport struct {
	mu   sync.Mutex
	reqs []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req.Method+" "+req.URL.String())
	r.mu.Unlock()
	return nil, fmt.Errorf("零出域违规：关态拦到出站 %s %s", req.Method, req.URL)
}

func (r *recordingTransport) hits() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.reqs))
	copy(out, r.reqs)
	return out
}

// TestPlatformDisabledZeroEgress 是「数据零出域」承诺的读侧证明：
// PLATFORM_ENABLED 未开启（默认纯本地）时，平台链路上每一个出站入口逐个驱动一遍，
// 一帧 HTTP 都不许发出——不是“请求失败了”，而是“请求根本没被构造出来”。
//
// 覆盖入口与装配门（cmd/api/main.go:213 的 else 分支）一一对应：
//   - 行为指标心跳 sendHeartbeat（有合法 install.lock 身份仍不发）
//   - 安装/心跳两个公开统计上报口 ReportXxxDefault
//   - 商户上报签名路径 doRetry（Client.Do）
//   - InitSync 内层 registerMerchantAs（防装配门次序回归后仍零出站）
//   - 连通性探测 CheckConnection（app_config/健康检查生产入口）
//   - 资产市场工厂读写（关态 disabledClient）
//   - 贡献者上架身份派生（出网前拒绝）
//
// 末尾用开态 dummy 地址正向触发一次，证明记录仪本身在岗——
// 否则“0 命中”可能只是没拦着，而不是真的没发。
func TestPlatformDisabledZeroEgress(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	t.Setenv("CONTRIBUTOR_DEV", "") // 置空环境里的 dev 逃生舱，否则身份派生会绕过 PlatformCfg 检查
	withPlatformConfig(t, nil)

	rec := &recordingTransport{}
	origTransport := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	// 心跳的提前 return 条件（InstallID 为空会静默 return）必须先排除，
	// 否则“没发请求”是空转，不构成零出域证明。
	path := filepath.Join(t.TempDir(), "install.lock")
	lock := `{"install_id":"ins-0123456789abcdef0123456789abcdef","install_time":"2026-01-01T00:00:00Z","admin_username":"admin","initialized":true,"version":"v1.0.0"}`
	if err := os.WriteFile(path, []byte(lock), 0o644); err != nil {
		t.Fatalf("铺 install.lock 失败：%v", err)
	}
	t.Setenv("INSTALL_LOCK_PATH", path)
	if got := install.GetStatus().InstallID; got == "" {
		t.Fatal("install.lock 有效却取不到 InstallID：sendHeartbeat 会提前静默 return，本证明将空转")
	}

	// 1) 行为指标心跳：身份齐备 + 关态，仍必须一帧不发。
	sendHeartbeat()

	// 2) 两个公开统计上报口：PlatformCfg=nil 必须命中哨兵（不是网络错误）。
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "ins-0123456789abcdef0123456789abcdef"}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("heartbeat 关态应命中 ErrPlatformNotConfigured, got %v", err)
	}
	if err := ReportInstallDefault(&ReportInstallReq{InstallID: "ins-0123456789abcdef0123456789abcdef"}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("install 关态应命中 ErrPlatformNotConfigured, got %v", err)
	}

	// 3) 商户上报签名路径（doRetry）。
	if err := NewPlatformClient("mk").Do("GET", "/merchant-api/ping", nil, nil); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("商户上报关态应命中哨兵, got %v", err)
	}

	// 4) InitSync 内层的商户注册（装配门若回归也必须在传输层之前被哨兵拦住）。
	if err := RegisterMerchant(RegisterMerchantReq{Name: "zero-egress"}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("商户注册关态应命中哨兵, got %v", err)
	}

	// 5) 连通性探测（SyncWithPlatform / HealthCheck 生产入口）。
	if err := CheckConnection(); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("CheckConnection 关态应命中哨兵, got %v", err)
	}
	if got := DegradeReason(checkConnErr(t)); got != "not_configured" {
		t.Errorf("DegradeReason=%q, want not_configured（关态是常态不是故障）", got)
	}

	// 6) 资产市场：关态工厂回 disabledClient，读空+nil、写哨兵。
	ctx := context.Background()
	api := NewPlatformAPIClient()
	list, total, err := api.ListAssets(ctx, "agent_persona", "", 1, 10)
	if err != nil || len(list) != 0 || total != 0 {
		t.Errorf("关态市场读应为空+nil, got list=%v total=%d err=%v", list, total, err)
	}
	if err := api.Purchase(ctx, "a1"); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("关态市场写应命中哨兵, got %v", err)
	}

	// 7) 贡献者上架：拿不到 platform.secret 时身份派生必须在出网前拒绝。
	withContributorGlobals(t, "mk-egress")
	if _, err := NewContributorClient().CreateAsset(CreateAssetPayload{
		AssetType: "bundle",
		Name:      "n",
		Version:   "1.0.0",
		Data:      []byte(`[]`),
	}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("关态贡献者上架应命中哨兵, got %v", err)
	}

	if hits := rec.hits(); len(hits) != 0 {
		t.Fatalf("PLATFORM_ENABLED 关态发生了 %d 次出站，零出域承诺被破：%v", len(hits), hits)
	}

	// 正向对照：开态下同一入口必须真的把请求交到传输层。
	// （withPlatformConfig 再包一层：Cleanup 后进先出，先还原 dummy 再还原最初的 nil。）
	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://platform-zero-egress.invalid", Secret: "s"})
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "x"}); err == nil {
		t.Error("开态对不可达地址必须报错（否则证明请求没进传输层，上面的 0 命中是假阴性）")
	}
	if hits := rec.hits(); len(hits) != 1 {
		t.Fatalf("正向对照应恰有 1 次出站记录, got %d: %v", len(hits), hits)
	}
}

// checkConnErr 单独取一次 CheckConnection 的错误，避免断言表达式里带副作用调用。
func checkConnErr(t *testing.T) error {
	t.Helper()
	return CheckConnection()
}
