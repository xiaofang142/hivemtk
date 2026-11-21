package capregistry

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type capabilityTestReader struct {
	binding    model.CapabilitySourceBinding
	connection model.MerchantConnection
	evidence   model.CapabilityEvidence
	err        error
}

func (r capabilityTestReader) FindCapabilityBinding(context.Context, uint, CapabilitySource, bool) (model.CapabilitySourceBinding, error) {
	return r.binding, r.err
}
func (r capabilityTestReader) FindCapabilityConnection(context.Context, CellKey) (model.MerchantConnection, error) {
	return r.connection, r.err
}
func (r capabilityTestReader) FindCapabilityEvidence(context.Context, CellKey) (model.CapabilityEvidence, error) {
	return r.evidence, r.err
}

type proofFunc func(CapabilityProofQuery) CapabilityProof

func (f proofFunc) VerifyCapabilityProof(_ context.Context, q CapabilityProofQuery) CapabilityProof {
	return f(q)
}

// 假证明仅通过测试 reader/verifier 注入，裸上下文和持久引用不能提升生产等级。
func TestPersistentCapabilityResolver_Fences(t *testing.T) {
	now := time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	ctx := tenant.WithTenantID(context.Background(), 17)
	source := CapabilitySource{Kind: SubmissionSourceAfterSale, Product: "douyin_shop", ResourceID: "order-A"}
	key := CellKey{TenantID: 17, ShopID: "shop-A", Product: source.Product, Region: "CN", Action: "refund.write", Carrier: ModeAPIAdapter}
	fixture := capabilityTestReader{
		binding:    model.CapabilitySourceBinding{ID: 1, TenantID: 17, Source: source.Kind, Product: source.Product, ResourceID: source.ResourceID, ShopID: key.ShopID, Region: key.Region, ConnectionID: 4, ConsentVersion: 2},
		connection: model.MerchantConnection{ID: 4, TenantID: 17, Provider: key.Product, ShopID: key.ShopID, Version: 3, CredentialVersion: 3, ConsentVersion: 2, CredentialContract: "checked-connection", Scopes: []string{key.Action}, ExpiresAt: &future},
		evidence:   model.CapabilityEvidence{ID: 7, TenantID: 17, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier, ConnectionID: 4, ConnectionVersion: 3, CredentialVersion: 3, ConsentVersion: 2, ContractVersion: "api-v1", DocumentationVersion: "design-cell-v1", Scopes: []string{key.Action}, ExpiresAt: future, Documentation: true, Implementation: true, Provenance: "test-verifier", RuntimeProofRef: "test-proof"},
	}
	for _, tc := range []struct {
		name, reason string
		alter        func(*capabilityTestReader, *CapabilityProof, *CapabilitySource, *string)
	}{
		{name: "同资源正确实时证据"},
		{name: "跨租户", reason: "request_binding_mismatch", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.binding.TenantID++
		}},
		{name: "错店", reason: "shop_binding_mismatch", alter: func(_ *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, shop *string) {
			*shop = "other-shop"
		}},
		{name: "过期", reason: "evidence_expired", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.evidence.ExpiresAt = now
		}},
		{name: "撤权", reason: "permission_revoked", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.connection.Revoked = true
		}},
		{name: "连接版本变更", reason: "stale_connection_version", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.connection.CredentialVersion++
		}},
		{name: "伪R", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.RuntimeVerified = false
		}},
		{name: "证明动作错配", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.Identity.Key.Action = "aftersale.write"
		}},
		{name: "证明载体错配", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.Identity.Key.Carrier = ModeBridge
		}},
		{name: "证明版本错配", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.Identity.CredentialVersion--
		}},
		{name: "状态证据尚未生效", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.PreflightObservedAt = now.Add(time.Minute)
		}},
		{name: "模拟R", reason: "state_preflight_required", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.evidence.Simulated = true
		}},
		{name: "状态证据过期", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.PreflightExpires = now
		}},
		{name: "状态证据属于其他订单", reason: "state_preflight_required", alter: func(_ *capabilityTestReader, p *CapabilityProof, _ *CapabilitySource, _ *string) {
			p.PreflightSource.ResourceID = "order-B"
		}},
		{name: "数据库故障", reason: "capability_storage_unavailable", alter: func(r *capabilityTestReader, _ *CapabilityProof, _ *CapabilitySource, _ *string) {
			r.err = errors.New("closed")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, src, shop := fixture, source, key.ShopID
			proof := CapabilityProof{RuntimeVerified: true, PreflightSource: source, PreflightObservedAt: now, PreflightExpires: future,
				Identity: CapabilityProofIdentity{Key: key, Source: source, Provenance: fixture.evidence.Provenance, ProofRef: fixture.evidence.RuntimeProofRef,
					ConnectionID: 4, ConnectionVersion: 3, CredentialVersion: 3, ConsentVersion: 2, CredentialContract: fixture.connection.CredentialContract}}
			if tc.alter != nil {
				tc.alter(&r, &proof, &src, &shop)
			}
			resolver := PersistentCapabilityResolver{Reader: r, Verifier: proofFunc(func(CapabilityProofQuery) CapabilityProof { return proof })}
			err := resolver.Authorize(ctx, src, key.Action, key.Carrier, shop, key.Region, now)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var denied *SubmissionDeniedError
			if !errors.As(err, &denied) || denied.ReasonCode != tc.reason {
				t.Fatalf("期望 %s，实际 %v", tc.reason, err)
			}
		})
	}
}
