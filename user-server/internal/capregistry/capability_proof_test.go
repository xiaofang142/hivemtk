package capregistry

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type localProofSource struct {
	contract    string
	observation CapabilityVerifiedObservation
	err         error
	rotate      bool
	reads       int
}

func (s *localProofSource) CurrentCapabilityContract(context.Context, CellKey) (string, error) {
	s.reads++
	if s.rotate && s.reads > 1 {
		return "rotated", nil
	}
	return s.contract, s.err
}
func (s *localProofSource) FindCapabilityProof(context.Context, CapabilityProofQuery) (CapabilityVerifiedObservation, error) {
	return s.observation, s.err
}

// 独立来源、版本、时效和模拟标记缺一不可，持久引用自身不能取得运行证明。
func TestLocalCapabilityProofVerifier_ResolverFences(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	key := CellKey{TenantID: 71, ShopID: "shop-A", Product: "douyin_shop", Region: "CN", Action: "message.text.send", Carrier: ModeBridge}
	source := CapabilitySource{Kind: SubmissionSourceBridge, Product: key.Product, ResourceID: "account-A"}
	definition, ok := FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
	if !ok {
		t.Fatal("测试动作必须已登记")
	}
	for _, name := range []string{"精确来源", "无验证源", "伪来源", "模拟观测", "持久模拟标记", "观测过期", "未来观测", "错租户", "错来源", "错动作", "错版本", "错误契约", "契约轮换", "无运行证明", "来源故障", "上下文取消"} {
		t.Run(name, func(t *testing.T) {
			evidence := model.CapabilityEvidence{ID: 3, TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier, ConnectionID: 9, ConnectionVersion: 2, CredentialVersion: 3, ConsentVersion: 4, ContractVersion: definition.ContractVersion, DocumentationVersion: definition.DocumentationVersion, Scopes: definition.RequiredScopes, ExpiresAt: expiry, Documentation: true, Implementation: true, Provenance: "independent-test-source", RuntimeProofRef: "proof-A"}
			reader := capabilityTestReader{connection: model.MerchantConnection{ID: 9, TenantID: key.TenantID, Provider: key.Product, ShopID: key.ShopID, Version: 2, CredentialVersion: 3, ConsentVersion: 4, CredentialContract: "contract-A", ExpiresAt: &expiry, Scopes: definition.RequiredScopes}, evidence: evidence,
				binding: model.CapabilitySourceBinding{ID: 1, TenantID: key.TenantID, Source: source.Kind, Product: key.Product, ResourceID: source.ResourceID, ShopID: key.ShopID, Region: key.Region, ConnectionID: 9, ConsentVersion: 4}}
			proofSource := &localProofSource{contract: "contract-A", observation: CapabilityVerifiedObservation{ObservedAt: now.Add(-time.Second), ExpiresAt: expiry, Proof: CapabilityProof{RuntimeVerified: true, Identity: CapabilityProofIdentity{Key: key, Source: source, Provenance: evidence.Provenance, ProofRef: evidence.RuntimeProofRef, CredentialContract: "contract-A", ConnectionID: 9, ConnectionVersion: 2, CredentialVersion: 3, ConsentVersion: 4}}}}
			ctx, cancel := context.WithCancel(tenant.WithTenantID(context.Background(), key.TenantID))
			defer cancel()
			switch name {
			case "伪来源":
				reader.evidence.Provenance = "unregistered"
			case "模拟观测":
				proofSource.observation.Simulated = true
			case "持久模拟标记":
				reader.evidence.Simulated = true
			case "观测过期":
				proofSource.observation.ExpiresAt = now
			case "未来观测":
				proofSource.observation.ObservedAt = now.Add(time.Minute)
			case "错租户":
				proofSource.observation.Proof.Identity.Key.TenantID++
			case "错来源":
				proofSource.observation.Proof.Identity.Source.ResourceID = "account-B"
			case "错动作":
				proofSource.observation.Proof.Identity.Key.Action = "message.image.send"
			case "错版本":
				proofSource.observation.Proof.Identity.CredentialVersion++
			case "错误契约":
				proofSource.contract = "current-other-contract"
			case "契约轮换":
				proofSource.rotate = true
			case "无运行证明":
				proofSource.observation.Proof.RuntimeVerified = false
			case "来源故障":
				proofSource.err = errors.New("store unavailable")
			case "上下文取消":
				cancel()
			}
			sources := map[CapabilityProofSourceKey]CapabilityProofSource{{Product: key.Product, Provenance: evidence.Provenance}: proofSource}
			if name == "无验证源" {
				sources = nil
			}
			verifier := NewLocalCapabilityProofVerifier(sources)
			delete(sources, CapabilityProofSourceKey{Product: key.Product, Provenance: evidence.Provenance})
			err := (PersistentCapabilityResolver{Reader: reader, Verifier: verifier}).Authorize(ctx, source, key.Action, key.Carrier, key.ShopID, key.Region, now)
			if name == "精确来源" {
				if err != nil {
					t.Fatalf("精确本地测试证明应通过，错误=%v", err)
				}
			} else if err == nil {
				t.Fatal("无效证明不得通过裁决")
			}
		})
	}
}
