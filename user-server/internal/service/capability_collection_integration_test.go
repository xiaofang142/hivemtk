package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 实际采证回调中轮换连接，随后真实仓储必须拒绝旧快照且不留下部分记录。
func TestCapabilityCollection_PersistentLateRevocation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		name := "保存后仍需独立验证源"
		if revoked {
			name = "采证期间撤权"
		}
		t.Run(name, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MerchantConnection{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
			expiry := time.Now().Add(time.Hour)
			key := capregistry.CellKey{TenantID: 61, ShopID: "shop-A", Product: "douyin_shop", Region: "CN", Action: "refund.write", Carrier: capregistry.ModeAPIAdapter}
			source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceAfterSale, Product: key.Product, ResourceID: "order-A"}
			connection := model.MerchantConnection{TenantID: key.TenantID, Provider: key.Product, ShopID: key.ShopID, Version: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A", Scopes: []string{key.Action}, ExpiresAt: &expiry}
			if err := db.Create(&connection).Error; err != nil {
				t.Fatal(err)
			}
			collector := collectionFunc(func(_ context.Context, k capregistry.CellKey, s capregistry.CapabilitySource, c model.MerchantConnection) (CapabilityObservation, error) {
				if revoked {
					if err := db.Model(&model.MerchantConnection{}).Where("id = ?", c.ID).Updates(map[string]any{"revoked": true, "version": c.Version + 1}).Error; err != nil {
						return CapabilityObservation{}, err
					}
				}
				return CapabilityObservation{Identity: capregistry.CapabilityProofIdentity{Key: k, Source: s, Provenance: "test-collector", ProofRef: "test-proof", CredentialContract: c.CredentialContract, ConnectionID: c.ID, ConnectionVersion: c.Version, CredentialVersion: c.CredentialVersion, ConsentVersion: c.ConsentVersion}, Scopes: []string{k.Action}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)}, nil
			})
			repo := repository.NewCapabilityObservationRepository(db)
			ctx := tenant.WithTenantID(context.Background(), key.TenantID)
			err := NewCapabilityCollectionService(repo, map[string]CapabilityCollector{key.Product: collector}).Collect(ctx, key, source)
			if revoked {
				if err == nil {
					t.Fatal("采证期间撤权必须拒绝旧观测")
				}
				var bindings, evidence int64
				if err := db.Model(&model.CapabilitySourceBinding{}).Count(&bindings).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&model.CapabilityEvidence{}).Count(&evidence).Error; err != nil {
					t.Fatal(err)
				}
				if bindings != 0 || evidence != 0 {
					t.Fatalf("撤权后不得写入，binding=%d evidence=%d", bindings, evidence)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				result := (capregistry.PersistentCapabilityResolver{Reader: repo}).Resolve(ctx, key, time.Now().UTC())
				if result.ProductionAllowed || result.Evidence.Runtime {
					t.Fatalf("持久证据引用不能替代独立验证源，result=%+v", result)
				}
			}
		})
	}
}
