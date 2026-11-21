package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 两个租户可以有同名来源，读取只能看见当前租户的准确店铺、连接和动作。
func TestCapabilityEvidenceRepository_ExactTenantAndAction(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MerchantConnection{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
	const product = "douyin_shop"
	source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceBridge, Product: product, ResourceID: "same-account"}
	for _, seed := range []struct {
		tenantID uint
		shop     string
	}{{21, "shop-A"}, {22, "shop-B"}} {
		expires := time.Now().Add(time.Hour)
		connection := model.MerchantConnection{TenantID: seed.tenantID, Provider: product, ShopID: seed.shop, CredentialCiphertext: "never-project", ExpiresAt: &expires}
		if err := db.Create(&connection).Error; err != nil {
			t.Fatal(err)
		}
		binding := model.CapabilitySourceBinding{TenantID: seed.tenantID, Source: source.Kind, Product: product, ResourceID: source.ResourceID, ShopID: seed.shop, Region: "CN", ConnectionID: connection.ID}
		if err := db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
		evidence := model.CapabilityEvidence{TenantID: seed.tenantID, ShopID: seed.shop, Product: product, Region: "CN", Action: "message.text.send", Carrier: capregistry.ModeBridge, ConnectionID: connection.ID, ExpiresAt: expires}
		if err := db.Create(&evidence).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := NewCapabilityEvidenceRepository(db)
	ctx := tenant.WithTenantID(context.Background(), 21)
	binding, err := r.FindCapabilityBinding(ctx, 21, source, false)
	if err != nil || binding.ShopID != "shop-A" {
		t.Fatalf("当前租户绑定错误 %+v %v", binding, err)
	}
	if _, err = r.FindCapabilityBinding(ctx, 22, source, false); err == nil {
		t.Fatal("拒绝跨租户读取")
	}
	if _, err = r.FindCapabilityBinding(tenant.WithSystemScope(ctx), 21, source, false); err == nil {
		t.Fatal("拒绝系统范围")
	}
	key := capregistry.CellKey{TenantID: 21, ShopID: "shop-A", Product: product, Region: "CN", Action: "message.text.send", Carrier: capregistry.ModeBridge}
	c, err := r.FindCapabilityConnection(ctx, key)
	if err != nil || c.ID != binding.ConnectionID || c.CredentialCiphertext != "" {
		t.Fatalf("连接投影错误，id=%d err=%v", c.ID, err)
	}
	e, err := r.FindCapabilityEvidence(ctx, key)
	if err != nil || e.ID == 0 || !e.Simulated {
		t.Fatalf("证据状态错误 %+v %v", e, err)
	}
	key.Action = "message.image.send"
	e, err = r.FindCapabilityEvidence(ctx, key)
	if err != nil || e.ID != 0 {
		t.Fatalf("不能继承其他动作证据 %+v %v", e, err)
	}
	key.ShopID = "shop-B"
	c, err = r.FindCapabilityConnection(ctx, key)
	if err != nil || c.ID != 0 {
		t.Fatalf("不能读取外租户连接，id=%d err=%v", c.ID, err)
	}
	if _, err = NewCapabilityEvidenceRepository(nil).FindCapabilityEvidence(ctx, key); err == nil {
		t.Fatal("缺数据库时必须拒绝")
	}
}
