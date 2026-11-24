package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type collectionRepo struct {
	connection model.MerchantConnection
	stores     int
	evidence   model.CapabilityEvidence
}

func (r *collectionRepo) FindCapabilityBinding(context.Context, uint, capregistry.CapabilitySource, bool) (model.CapabilitySourceBinding, error) {
	return model.CapabilitySourceBinding{}, nil
}
func (r *collectionRepo) FindCapabilityConnection(context.Context, capregistry.CellKey) (model.MerchantConnection, error) {
	return r.connection, nil
}
func (r *collectionRepo) FindCapabilityEvidence(context.Context, capregistry.CellKey) (model.CapabilityEvidence, error) {
	return r.evidence, nil
}
func (r *collectionRepo) StoreCapabilityObservation(_ context.Context, _ model.CapabilitySourceBinding, e model.CapabilityEvidence, _ string) error {
	r.stores++
	r.evidence = e
	return nil
}

type collectionFunc func(context.Context, capregistry.CellKey, capregistry.CapabilitySource, model.MerchantConnection) (CapabilityObservation, error)

func (f collectionFunc) CurrentCapabilityContract(context.Context, capregistry.CellKey) (string, error) {
	return "contract-A", nil
}

func (f collectionFunc) CollectCapability(ctx context.Context, k capregistry.CellKey, s capregistry.CapabilitySource, c model.MerchantConnection) (CapabilityObservation, error) {
	return f(ctx, k, s, c)
}

type rotatingCollection struct {
	collectionFunc
	reads int
}

func (c *rotatingCollection) CurrentCapabilityContract(context.Context, capregistry.CellKey) (string, error) {
	c.reads++
	if c.reads > 1 {
		return "rotated-contract", nil
	}
	return "contract-A", nil
}

// 采集器只能提供与当前动作和连接完全一致的观测；错误证据不调用持久化。
func TestCapabilityCollection_IdentityAndFailures(t *testing.T) {
	for _, name := range []string{"有效观测", "未配置采集器", "跨租户请求", "跨店证据", "错动作", "凭证版本变化", "错误契约", "扩大scope", "缺少scope", "未来观测", "旧观测", "过期证据", "超过凭证有效期", "采集失败", "上下文取消", "撤权", "模拟证据", "篡改scope快照", "篡改到期快照", "来源载体错配", "当前契约变化", "采证期间契约变化"} {
		t.Run(name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour)
			key := capregistry.CellKey{TenantID: 31, ShopID: "shop-A", Product: "douyin_shop", Region: "CN", Action: "refund.write", Carrier: capregistry.ModeAPIAdapter}
			source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceAfterSale, Product: key.Product, ResourceID: "order-A"}
			repo := &collectionRepo{connection: model.MerchantConnection{ID: 7, TenantID: 31, Provider: key.Product, ShopID: key.ShopID, Version: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A", Scopes: []string{key.Action}, ExpiresAt: &expires}}
			if name == "来源载体错配" {
				source.Kind = capregistry.SubmissionSourceBridge
			}
			if name == "当前契约变化" {
				repo.connection.CredentialContract = "old-contract"
			}
			if name == "撤权" {
				repo.connection.Revoked = true
			}
			ctx, cancel := context.WithCancel(tenant.WithTenantID(context.Background(), 31))
			defer cancel()
			collector := collectionFunc(func(_ context.Context, k capregistry.CellKey, s capregistry.CapabilitySource, c model.MerchantConnection) (CapabilityObservation, error) {
				o := CapabilityObservation{Identity: capregistry.CapabilityProofIdentity{Key: k, Source: s, Provenance: "collector-test", ProofRef: "observation-A", CredentialContract: c.CredentialContract, ConnectionID: c.ID, ConnectionVersion: c.Version, CredentialVersion: c.CredentialVersion, ConsentVersion: c.ConsentVersion}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute), Scopes: []string{k.Action}}
				switch name {
				case "跨店证据":
					o.Identity.Key.ShopID = "shop-B"
				case "错动作":
					o.Identity.Key.Action = "aftersale.write"
				case "凭证版本变化":
					o.Identity.CredentialVersion++
				case "错误契约":
					o.Identity.CredentialContract = "other"
				case "扩大scope":
					o.Scopes = append(o.Scopes, "order.write")
				case "缺少scope":
					o.Scopes = nil
				case "未来观测":
					o.ObservedAt = time.Now().Add(time.Hour)
				case "旧观测":
					o.ObservedAt = time.Now().Add(-time.Hour)
				case "过期证据":
					o.ExpiresAt = time.Now().Add(-time.Minute)
				case "超过凭证有效期":
					o.ExpiresAt = expires.Add(time.Minute)
				case "采集失败":
					return CapabilityObservation{}, errors.New("provider unavailable")
				case "上下文取消":
					cancel()
				case "篡改scope快照":
					c.Scopes[0] = "order.write"
					o.Scopes = []string{"order.write"}
				case "篡改到期快照":
					*c.ExpiresAt = expires.Add(time.Hour)
					o.ExpiresAt = expires.Add(time.Minute)
				case "模拟证据":
					o.Simulated = true
				}
				return o, nil
			})
			collectors := map[string]CapabilityCollector{key.Product: collector}
			if name == "采证期间契约变化" {
				collectors[key.Product] = &rotatingCollection{collectionFunc: collector}
			}
			if name == "未配置采集器" {
				collectors = nil
			}
			if name == "跨租户请求" {
				key.TenantID++
			}
			svc := NewCapabilityCollectionService(repo, collectors)
			delete(collectors, key.Product) // 构造器必须保留独立注册表。
			err := svc.Collect(ctx, key, source)
			if name == "有效观测" || name == "模拟证据" {
				if err != nil || repo.stores != 1 || repo.evidence.Simulated != (name == "模拟证据") {
					t.Fatalf("有效观测应精确保存一次，次数=%d 错误=%v", repo.stores, err)
				}
			} else if err == nil || repo.stores != 0 {
				t.Fatalf("无效观测不得保存，次数=%d 错误=%v", repo.stores, err)
			}
		})
	}
}
