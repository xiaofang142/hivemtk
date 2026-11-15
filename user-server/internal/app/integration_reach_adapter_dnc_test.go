package app

// reach 外发勿扰前置检查回归钉（架构师审计轮）。
//
// 背景：IntegrationReachAdapter 的 9 个 Send 此前完全绕过全局退订标志位
// （DNC）——对话回复引擎有前置、AI 工具层有 decorator_dnc，唯独营销外发
//（reach pipeline → 本适配器 → 真实渠道）照发已退订用户。合规高危。
//
// 本钉锁住行为：Block(oneID) 后 SendSMS/SendWhatsApp 必须 fail-closed 拒发，
// 且错误可识别（dnc: 前缀）；未退订标识放行到渠道服务层（用 nil 服务短路）。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	dncsvc "hivemtk-user/internal/service/dnc"
)

func setupDNCTest(t *testing.T) (*IntegrationReachAdapter, *dncsvc.DoNotContactService, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.Tenant{}, &model.Customer{}, &model.CustomerDoNotContact{},
		&model.SmsConfig{}, &model.SmsRecord{},
	)
	ctx := context.Background()
	adapter := NewIntegrationReachAdapterFromDB(db)
	dnc := dncsvc.NewDoNotContactService(nil)
	adapter.SetDoNotContactService(dnc)
	return adapter, dnc, ctx
}

// 未退订标识：guard 放行（错误不来自 dnc）——落到渠道服务层（本钉用 nil 短路验证不误拦）。
func TestReachDNC_NotBlockedPassesThrough(t *testing.T) {
	adapter, _, ctx := setupDNCTest(t)

	_, err := adapter.SendSMS(ctx, "13800001111", "营销内容", "", nil)
	if err == nil {
		t.Fatal("nil sms service 应返回 NotConfigured（说明 guard 未拦）")
	}
	if strings.Contains(err.Error(), "dnc:") {
		t.Fatalf("未退订标识不应被 DNC 拦截, got: %v", err)
	}
}

// 已退订（phone 降级键）：SendSMS 必须 fail-closed 拒发。
func TestReachDNC_SMSBlockedByPhoneFallbackKey(t *testing.T) {
	adapter, dnc, ctx := setupDNCTest(t)

	// 陌生号码（customer 表无该手机号）→ ResolveOneID 降级 "phone:"+id → Block 落此键
	phone := "13900002222"
	if err := dnc.Block(ctx, "phone:"+phone, "sms", "test:unsubscribe"); err != nil {
		t.Fatalf("Block: %v", err)
	}

	_, err := adapter.SendSMS(ctx, phone, "营销内容", "", nil)
	if err == nil || !strings.Contains(err.Error(), "dnc:") {
		t.Fatalf("已退订号码的营销外发必须被拒发, got: %v", err)
	}
}

// 已退订（真实 oneID 反查命中）：phone 关联到客户后以 oneID 为准。
func TestReachDNC_SMSBlockedByResolvedOneID(t *testing.T) {
	adapter, dnc, ctx := setupDNCTest(t)

	phone := "13900003333"
	oneID := "one-dnc-3333"
	if err := dnc.Block(ctx, oneID, "", "test:unsubscribe"); err != nil {
		t.Fatalf("Block 全渠道: %v", err)
	}

	// phone 关联到客户（ResolveOneID 反查真实 oneID 命中全渠道退订）
	if err := adapter.db.Create(&model.Customer{
		Phone:     phone,
		UnifiedID: oneID,
	}).Error; err != nil {
		t.Fatalf("建客户: %v", err)
	}

	_, err := adapter.SendSMS(ctx, phone, "营销内容", "", nil)
	if err == nil || !strings.Contains(err.Error(), "dnc:") {
		t.Fatalf("反查 oneID 命中全渠道退订必须拒发, got: %v", err)
	}
}
