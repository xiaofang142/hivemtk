package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/pkg/testutil"
)

// 任务三：设备信标服务口径测试。
// SaaS 形态收快照（心跳 client_machine 的数据源）；离线单机形态静默拒收
// （部署机=服务器机，host_info 已是商户机器信息，两套口径不并存）。

func TestClientDeviceServiceReportsInSaaSMode(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev); platform.ResetClientDeviceSnapshotForTest() })
	// TA-06：归属按登录租户反查 tenant_code（需库）——夹具建 tenant 84=t84xxxx
	// + 白名单含该码（新契约默认 fail-closed）。
	testutil.NewTestDB(t, &model.Tenant{})
	if err := db.GetDB().Create(&model.Tenant{Code: "t84xxxx", Name: "TA-06 夹具", ID: 84}).Error; err != nil {
		t.Fatalf("建租户夹具失败: %v", err)
	}
	t.Setenv("CLIENT_DEVICE_TENANT_CODES", "t84xxxx")

	svc := NewClientDeviceService()
	recorded, err := svc.Report(tenant.WithTenantID(context.Background(), 84), &ClientDeviceReport{
		Platform: "Win32",
		Screen:   "2560x1440",
		Timezone: "Asia/Shanghai",
		Language: "zh-CN",
		UA:       "Mozilla/5.0 test",
		IP:       "198.51.100.23",
	})
	if err != nil {
		t.Fatalf("SaaS 形态下信标上报不应报错：%v", err)
	}
	if !recorded {
		t.Fatal("SaaS 形态下信标必须被收下")
	}
	snap := platform.FreshClientDeviceSnapshot()
	if snap == nil {
		t.Fatal("快照必须已落缓存")
	}
	if snap.IP != "198.51.100.23" || snap.Platform != "Win32" || snap.Screen != "2560x1440" {
		t.Errorf("快照内容不符：ip=%q platform=%q screen=%q", snap.IP, snap.Platform, snap.Screen)
	}
	if snap.UA != "Mozilla/5.0 test" {
		t.Errorf("UA（controller 侧采集）不符：%q", snap.UA)
	}
}

func TestClientDeviceServiceSkipsInOfflineMode(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(prev); platform.ResetClientDeviceSnapshotForTest() })

	svc := NewClientDeviceService()
	recorded, err := svc.Report(tenant.WithTenantID(context.Background(), 84), &ClientDeviceReport{IP: "198.51.100.24", Platform: "MacIntel"})
	if err != nil {
		t.Fatalf("离线形态拒收是正常口径，不应报错：%v", err)
	}
	if recorded {
		t.Fatal("离线形态不应收快照")
	}
	if platform.FreshClientDeviceSnapshot() != nil {
		t.Fatal("离线形态不得落任何快照")
	}
}
