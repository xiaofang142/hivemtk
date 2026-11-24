package repository

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
)

// 不同连接同时声明同一来源时，只允许一个店铺建立归属。
func TestCapabilityObservation_ConcurrentSourceOwnership(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MerchantConnection{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
	expires := time.Now().Add(time.Hour)
	connections := []model.MerchantConnection{
		{TenantID: 51, Provider: "douyin_shop", ShopID: "shop-A", Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "contract-A", ExpiresAt: &expires},
		{TenantID: 51, Provider: "douyin_shop", ShopID: "shop-B", Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "contract-B", ExpiresAt: &expires},
	}
	for i := range connections {
		if err := db.Create(&connections[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(tenant.WithTenantID(context.Background(), 51), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, connection := range connections {
		wg.Add(1)
		utils.SafeGo(ctx, "capability_source_test", func(ctx context.Context) {
			defer wg.Done()
			<-start
			binding := model.CapabilitySourceBinding{TenantID: 51, Source: capregistry.SubmissionSourceAfterSale, Product: connection.Provider, ResourceID: "shared-order", ShopID: connection.ShopID, Region: "CN", ConnectionID: connection.ID, ConsentVersion: 1}
			evidence := model.CapabilityEvidence{TenantID: 51, Product: connection.Provider, ShopID: connection.ShopID, Region: "CN", Action: "refund.write", Carrier: capregistry.ModeAPIAdapter, ConnectionID: connection.ID, ConnectionVersion: 1, CredentialVersion: 1, ConsentVersion: 1, ExpiresAt: time.Now().Add(time.Minute), Simulated: true}
			results <- NewCapabilityObservationRepository(db).StoreCapabilityObservation(ctx, binding, evidence, connection.CredentialContract)
		})
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	var bindings, evidence int64
	if err := db.Model(&model.CapabilitySourceBinding{}).Count(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CapabilityEvidence{}).Count(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if successes != 1 || bindings != 1 || evidence != 1 {
		t.Fatalf("同来源只能归属一家店铺，成功=%d 映射=%d 证据=%d", successes, bindings, evidence)
	}
}

// 采集后授权发生变化或来源已归属其他店铺时，映射和证据均不能部分写入。
func TestCapabilityObservation_AtomicVersionFence(t *testing.T) {
	for _, name := range []string{"保存及更新", "已撤权", "凭证轮换", "连接版本变化", "同意版本变化", "契约变化", "刷新中", "过期观测", "跨租户", "来源换店", "来源已撤销", "证据写入失败", "连接到期", "授权进行中", "空凭证版本"} {
		t.Run(name, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MerchantConnection{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
			expires := time.Now().Add(time.Hour)
			c := model.MerchantConnection{TenantID: 41, Provider: "douyin_shop", ShopID: "shop-A", Version: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A", ExpiresAt: &expires}
			if err := db.Create(&c).Error; err != nil {
				t.Fatal(err)
			}
			b := model.CapabilitySourceBinding{TenantID: 41, Source: capregistry.SubmissionSourceAfterSale, Product: c.Provider, ResourceID: "order-A", ShopID: c.ShopID, Region: "CN", ConnectionID: c.ID, ConsentVersion: c.ConsentVersion}
			e := model.CapabilityEvidence{TenantID: 41, Product: c.Provider, ShopID: c.ShopID, Region: "CN", Action: "refund.write", Carrier: capregistry.ModeAPIAdapter, ConnectionID: c.ID, ConnectionVersion: c.Version, CredentialVersion: c.CredentialVersion, ConsentVersion: c.ConsentVersion, ExpiresAt: time.Now().Add(time.Minute), Provenance: "test", RuntimeProofRef: "proof-A", Simulated: true}
			ctx := tenant.WithTenantID(context.Background(), 41)
			updates := map[string]any{}
			switch name {
			case "已撤权":
				updates["revoked"] = true
			case "凭证轮换":
				updates["credential_version"] = 4
			case "连接版本变化":
				updates["version"] = 3
			case "同意版本变化":
				updates["consent_version"] = 5
			case "契约变化":
				updates["credential_contract"] = "contract-B"
			case "连接到期":
				updates["expires_at"] = time.Now().Add(-time.Minute)
			case "授权进行中":
				updates["attempt_expires_at"] = time.Now().Add(time.Minute)
			case "空凭证版本":
				updates["credential_version"] = 0
				e.CredentialVersion = 0
			case "刷新中":
				updates["refresh_until"] = time.Now().Add(time.Minute)
			case "过期观测":
				e.ExpiresAt = time.Now().Add(-time.Minute)
			case "证据写入失败":
				e.Provenance = strings.Repeat("x", 65)
			case "跨租户":
				ctx = tenant.WithTenantID(context.Background(), 42)
			case "来源换店", "来源已撤销":
				existing := b
				if name == "来源换店" {
					existing.ShopID = "shop-B"
				} else {
					existing.Revoked = true
				}
				if err := db.Create(&existing).Error; err != nil {
					t.Fatal(err)
				}
			}
			if len(updates) > 0 {
				if err := db.Model(&model.MerchantConnection{}).Where("id = ?", c.ID).Updates(updates).Error; err != nil {
					t.Fatal(err)
				}
			}
			repo := NewCapabilityObservationRepository(db)
			err := repo.StoreCapabilityObservation(ctx, b, e, c.CredentialContract)
			var count int64
			if countErr := db.Model(&model.CapabilityEvidence{}).Count(&count).Error; countErr != nil {
				t.Fatal(countErr)
			}
			if name == "保存及更新" {
				if err != nil || count != 1 {
					t.Fatalf("应保存一份证据，count=%d err=%v", count, err)
				}
				e.RuntimeProofRef = "proof-B"
				if err := repo.StoreCapabilityObservation(ctx, b, e, c.CredentialContract); err != nil {
					t.Fatal(err)
				}
				var stored model.CapabilityEvidence
				if err := db.Take(&stored).Error; err != nil || stored.RuntimeProofRef != "proof-B" {
					t.Fatalf("应原位更新证据，err=%v", err)
				}
				e.Simulated = false
				if err := repo.StoreCapabilityObservation(ctx, b, e, c.CredentialContract); err != nil {
					t.Fatal(err)
				}
				stored = model.CapabilityEvidence{}
				if err := db.Take(&stored).Error; err != nil || stored.Simulated {
					t.Fatalf("显式非模拟标记不得被默认值替换，err=%v", err)
				}
			} else {
				if err == nil || count != 0 {
					t.Fatalf("失效观测必须零证据写入，count=%d err=%v", count, err)
				}
				var bindings int64
				if err := db.Model(&model.CapabilitySourceBinding{}).Count(&bindings).Error; err != nil {
					t.Fatal(err)
				}
				expected := int64(0)
				if name == "来源换店" || name == "来源已撤销" {
					expected = 1
				}
				if bindings != expected {
					t.Fatalf("失败不能留下部分映射，期望%d，实际%d", expected, bindings)
				}
			}
		})
	}
}
