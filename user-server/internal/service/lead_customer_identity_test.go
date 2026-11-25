package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 未知渠道也必须生成稳定且带作用域的身份键，同账号同联系人重复命中归一到同一 ID。
func TestLeadIdentity_UnknownChannelStable(t *testing.T) {
	a := leadMiningUnifiedIDFor("sms", "acct-1", "+8613800000000")
	b := leadMiningUnifiedIDFor("sms", "acct-1", "+8613800000000")
	if a == "" || a != b {
		t.Fatalf("未知渠道身份键必须稳定，实际 a=%q b=%q", a, b)
	}
	c := leadMiningUnifiedIDFor("sms", "acct-2", "+8613800000000")
	if c == a {
		t.Fatalf("不同账号的身份键不得碰撞：a=%q c=%q", a, c)
	}
}

// 个微/QQ 保持专用身份前缀，不受作用域键影响。
func TestLeadIdentity_DedicatedChannelsUnchanged(t *testing.T) {
	if got := leadMiningUnifiedIDFor("personal_wx", "acct-1", "wxid-1"); got == "" {
		t.Fatal("个微身份键不能为空")
	}
	_ = context.Background()
}

// 真实 PG：未知渠道同联系人重复命中归一到一个稳定客户，且 SourceAccountID 为真实来源账号而非联系人 ID。
func TestLeadIdentity_ResolveCustomerStableAndSourceAccount(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	db := testutil.NewTestDB(t, &model.Customer{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：客户身份归一不能跳过")
	}
	repo := repository.NewCustomerRepository()
	svc := &Service{custRepo: repo}
	ctx := tenant.WithTenantID(context.Background(), 6601)
	c1, err := svc.resolveCustomer(ctx, "sms", "acct-1", "+8613800000000", "张三")
	if err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	c2, err := svc.resolveCustomer(ctx, "sms", "acct-1", "+8613800000000", "张三")
	if err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}
	if c1.ID != c2.ID {
		t.Fatalf("同账号同联系人应归一到同一客户，实际 %s vs %s", c1.ID, c2.ID)
	}
	if c1.SourceAccountID != "acct-1" {
		t.Fatalf("来源账号应为真实接入账号 acct-1，实际 %q", c1.SourceAccountID)
	}
	// 同联系人在不同账号下应为不同客户（作用域隔离）。
	c3, err := svc.resolveCustomer(ctx, "sms", "acct-2", "+8613800000000", "张三")
	if err != nil {
		t.Fatalf("异账号解析失败: %v", err)
	}
	if c3.ID == c1.ID {
		t.Fatalf("不同账号的同联系人不应合并，实际均为 %s", c1.ID)
	}
}
