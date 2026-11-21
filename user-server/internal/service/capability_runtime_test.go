// capability_runtime_test.go D01（能力运行视图）测试。
// 验收口径（docs/assessment/functionality-2026-10-27/开发顺序与验收.md D01）：
//   - 每个登记能力都找得到实际执行路径（声明支持但 Adapter 缺失 = 契约测试失败）；
//   - 实现存在但未配置返回 configured=false 而非 implemented=false；
//   - reason_code 机器可读，前端可区分"平台不支持"与"没配账号"。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

// 账号计数只证明配置。平台级列表没有精确店铺身份，不能用请求上下文里的证据补齐这一缺口。
func TestCapabilityRuntime_BridgeConfiguredDoesNotMeanSendReady(t *testing.T) {
	// DryRun 走正式仓储计数路径，通过回调提供计数，无需数据库连接或凭证。
	database, err := gorm.Open(postgres.Open("host=127.0.0.1 port=1 sslmode=disable"), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Callback().Query().After("gorm:query").Register("test:capability_account_counts", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			return
		}
		switch tx.Statement.Table {
		case "bridge_accounts", "telegram_accounts":
			*count = 1
			tx.RowsAffected = 1
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewCapabilityRuntimeService(repository.NewChannelOverviewRepository(database), nil)
	ctx := tenant.WithTenantID(context.Background(), 501)
	binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "configured-account", "message.text.send", capregistry.ModeBridge)
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "无证据", ctx: ctx},
		{name: "单店上下文证据", ctx: capregistry.WithCellEvidence(capregistry.WithSubmissionBinding(ctx, binding), evidence)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := svc.Describe(tc.ctx)
			byKey := make(map[string]dto.CapabilityRuntimeView)
			wantReady := 0
			for _, row := range view.Capabilities {
				byKey[row.Platform+"/"+row.Capability] = row
				if row.Via == capregistry.ModeBridge && row.Capability == string(capregistry.CapDMSend) {
					if !row.Implemented || !row.Configured || row.Ready || row.ReasonCode != "capability_mapping_missing" {
						t.Errorf("桥接发送应保留已配置并标明缺店铺映射，实际 %+v", row)
					}
					continue
				}
				if row.Ready {
					wantReady++
				}
			}
			if view.ReadyCount != wantReady {
				t.Errorf("汇总应排除未授权的桥接发送，期望 %d，实际 %d", wantReady, view.ReadyCount)
			}
			for _, key := range []string{"douyin_shop/dm.receive", "telegram/dm.send"} {
				if row := byKey[key]; !row.Implemented || !row.Configured || !row.Ready || row.ReasonCode != "" {
					t.Errorf("%s 应保留原就绪逻辑，实际 %+v", key, row)
				}
			}
			if row := byKey["sms/sms"]; !row.Implemented || row.Configured || row.Ready || row.ReasonCode != "not_configured_no_account" {
				t.Errorf("未配置短信应保留原原因，实际 %+v", row)
			}
		})
	}
	for _, row := range NewCapabilityRuntimeService(nil, nil).Describe(ctx).Capabilities {
		if row.Platform == "douyin_shop" && row.Capability == string(capregistry.CapDMSend) &&
			(row.Configured || row.Ready || row.ReasonCode != "not_configured_no_account") {
			t.Errorf("无桥接账号应返回未配置原因，实际 %+v", row)
		}
	}
}

// 精确只读查询重新裁决店铺、产品、地区、动作和载体。模拟证据可以被诊断，但不能产生生产就绪。
func TestCapabilityRuntime_ResolveCellRejectsUnusableBridgeEvidence(t *testing.T) {
	svc := NewCapabilityRuntimeService(nil, nil)
	binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "configured-account", "message.text.send", capregistry.ModeBridge)
	key := binding.Key
	ctx := tenant.WithTenantID(context.Background(), key.TenantID)
	if got, err := svc.ResolveCell(ctx, key.ShopID, key.Product, key.Region, key.Action, key.Carrier); err != nil ||
		got.ProductionAllowed || !got.PausePending || got.ReasonCode != "shop_evidence_missing" {
		t.Fatalf("缺少店铺实证时应阻塞，实际 %+v，错误 %v", got, err)
	}
	for _, tc := range []struct {
		name   string
		edit   func(*capregistry.CellEvidence)
		reason string
	}{
		{name: "模拟实证", edit: func(e *capregistry.CellEvidence) { e.Evidence[2].Simulated = true }, reason: "pending_live_validation"},
		{name: "另一租户", edit: func(e *capregistry.CellEvidence) { e.TenantID++ }, reason: "shop_binding_mismatch"},
		{name: "另一店铺", edit: func(e *capregistry.CellEvidence) { e.ShopID = "another-shop" }, reason: "shop_binding_mismatch"},
		{name: "另一产品", edit: func(e *capregistry.CellEvidence) { e.Product = "douyin" }, reason: "shop_binding_mismatch"},
		{name: "另一地区", edit: func(e *capregistry.CellEvidence) { e.Region = "US" }, reason: "shop_binding_mismatch"},
		{name: "另一动作", edit: func(e *capregistry.CellEvidence) { e.Action = "message.image.send" }, reason: "shop_binding_mismatch"},
		{name: "另一载体", edit: func(e *capregistry.CellEvidence) { e.Carrier = capregistry.ModeAPIAdapter }, reason: "shop_binding_mismatch"},
		{name: "撤权", edit: func(e *capregistry.CellEvidence) { e.Revoked = true }, reason: "permission_revoked"},
		{name: "已到期", edit: func(e *capregistry.CellEvidence) { expired := time.Now().Add(-time.Hour); e.ExpiresAt = &expired }, reason: "evidence_expired"},
		{name: "旧版本", edit: func(e *capregistry.CellEvidence) { e.Version = "old" }, reason: "stale_contract_version"},
		{name: "缺权限", edit: func(e *capregistry.CellEvidence) { e.Scopes = nil }, reason: "required_scope_missing"},
		{name: "已停用", edit: func(e *capregistry.CellEvidence) { e.ConnectionState = capregistry.ConnectionDisabled }, reason: "connection_disabled"},
		{name: "缺运行实证", edit: func(e *capregistry.CellEvidence) { e.Evidence = e.Evidence[:2] }, reason: "cell_evidence_incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := evidence
			fixture.Evidence = append([]capregistry.EvidenceRecord(nil), evidence.Evidence...)
			tc.edit(&fixture)
			decision := capregistry.ResolveCell(key, &fixture, time.Now())
			if decision.ProductionAllowed || !decision.PausePending || decision.ReasonCode != tc.reason {
				t.Fatalf("纯裁决预期不就绪并返回 %q，实际 %+v", tc.reason, decision)
			}
			got, err := svc.ResolveCell(capregistry.WithCellEvidence(ctx, fixture), key.ShopID, key.Product, key.Region, key.Action, key.Carrier)
			if err != nil || got.ProductionAllowed || !got.PausePending || got.ReasonCode != "shop_evidence_missing" {
				t.Fatalf("诊断服务不可装载旧上下文，实际 %+v，错误 %v", got, err)
			}
		})
	}
	system := tenant.WithSystemScope(capregistry.WithCellEvidence(ctx, evidence))
	if got, err := svc.ResolveCell(system, key.ShopID, key.Product, key.Region, key.Action, key.Carrier); !errors.Is(err, ErrCapabilityTenantContextMissing) || got.ProductionAllowed {
		t.Fatalf("系统范围没有店铺授权，实际 %+v，错误 %v", got, err)
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

func TestCapabilityRuntime_ResolveCellRequiresTenantAndReturnsBlockedWithoutEvidence(t *testing.T) {
	svc := NewCapabilityRuntimeService(nil, nil)
	_, err := svc.ResolveCell(context.Background(), "shop-a", "douyin_shop", "CN", "order.address.change", "api_adapter")
	if err == nil {
		t.Fatal("缺少租户上下文时能力解析必须 fail-closed")
	}

	ctx := tenant.WithTenantID(context.Background(), 17)
	got, err := svc.ResolveCell(ctx, "shop-a", "douyin_shop", "CN", "order.address.change", "api_adapter")
	if err != nil {
		t.Fatalf("解析阻塞格失败: %v", err)
	}
	if got.TenantID != 17 || got.ShopID != "shop-a" || got.Product != "douyin_shop" ||
		got.Action != "order.address.change" || got.ProductionAllowed || got.ReasonCode != "shop_evidence_missing" {
		t.Fatalf("未加载权威店铺证据时必须展示精确阻塞原因，实际 %+v", got)
	}
}
