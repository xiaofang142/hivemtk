// capability_runtime_test.go D01（能力运行视图）测试。
// 验收口径（docs/assessment/functionality-2026-10-27/开发顺序与验收.md D01）：
//   - 每个登记能力都找得到实际执行路径（声明支持但 Adapter 缺失 = 契约测试失败）；
//   - 实现存在但未配置返回 configured=false 而非 implemented=false；
//   - reason_code 机器可读，前端可区分"平台不支持"与"没配账号"。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 契约：capregistry 里登记的每一条都必须在本二进制有真实执行路径。
// 有人登记了能力却没写实现时，这条测试先红——而不是让用户在运行时撞 404/空转发。
func TestCapabilityRuntime_ContractAllRegisteredHaveRuntimePath(t *testing.T) {
	for _, e := range capregistry.All() {
		if !runtimeImplemented(e) {
			t.Errorf("capregistry 登记了 %s/%s (via=%s) 但 runtimeImplemented=false：先补执行路径再登记",
				e.Platform, e.Cap, e.Via)
		}
	}
}

// 反向契约：未登记的组合不得冒充已实现。
func TestCapabilityRuntime_UnregisteredCombinationNotImplemented(t *testing.T) {
	if runtimeImplemented(capregistry.Entry{Platform: "weixin", Cap: capregistry.CapDMSend, Via: "api"}) {
		t.Error("未登记平台冒充已实现会掩盖能力缺口")
	}
	if runtimeImplemented(capregistry.Entry{Platform: "douyin", Cap: capregistry.CapDMSend, Via: "carrier-pigeon"}) {
		t.Error("未知 via 不得视为已实现")
	}
}

// configured 语义：新测试库无账号 → configured=false / reason=not_configured_no_account；
// 播种账号后 configured=true / ready=true。
func TestCapabilityRuntime_ConfiguredSemantics(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.ExternalKBConnector{},
		&model.TelegramAccount{},
	)
	_ = database
	svc := NewCapabilityRuntimeService(
		repository.NewChannelOverviewRepository(database),
		repository.NewExternalKBConnectorRepo(),
	)
	ctx := context.Background()

	view := svc.Describe(ctx)
	if view.Total != len(capregistry.All()) {
		t.Fatalf("视图条数 %d 与登记数 %d 不一致", view.Total, len(capregistry.All()))
	}

	byKey := map[string]dto.CapabilityRuntimeView{}
	for _, c := range view.Capabilities {
		byKey[c.Platform+"/"+c.Capability] = c
	}

	// 未配置：实现存在但 configured=false（而不是 implemented=false）
	telegramSend := byKey["telegram/dm.send"]
	if _, ok := byKey["telegram/dm.send"]; !ok {
		t.Fatal("缺 telegram/dm.send 视图")
	}
	if !telegramSend.Implemented {
		t.Error("telegram/dm.send 应为 implemented")
	}
	if telegramSend.Configured || telegramSend.Ready {
		t.Errorf("空库下 telegram/dm.send 不应 configured/ready: %+v", telegramSend)
	}
	if telegramSend.ReasonCode != "not_configured_no_account" {
		t.Errorf("reason_code = %q, want not_configured_no_account", telegramSend.ReasonCode)
	}

	// sms 同理
	if sms, ok := byKey["sms/sms"]; ok && (sms.Configured || sms.Ready) {
		t.Errorf("空库下 sms 不应 configured/ready: %+v", sms)
	}

	// 播种一条 telegram 账号后 ready
	if err := database.Exec("INSERT INTO telegram_accounts (tenant_id, account_name, bot_token, status) VALUES (1, 't1', 'tok', 1)").Error; err != nil {
		t.Fatalf("seed telegram account: %v", err)
	}
	view2 := svc.Describe(ctx)
	for _, c := range view2.Capabilities {
		if c.Platform == "telegram" && c.Capability == "dm.send" {
			if !c.Configured || !c.Ready {
				t.Errorf("播种账号后 telegram/dm.send 应 ready: %+v", c)
			}
			if c.ReasonCode != "" {
				t.Errorf("ready 项不应带 reason_code，got %q", c.ReasonCode)
			}
		}
	}
}
