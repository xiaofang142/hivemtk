package capregistry

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func TestFakeCapabilityProofVerifier_AllowsMatching(t *testing.T) {
	v := NewFakeCapabilityProofVerifier("fake-verifier")
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	q := CapabilityProofQuery{
		Key:                CellKey{TenantID: 1, ShopID: "s", Product: "p", Region: "CN", Action: "a", Carrier: ModeBridge},
		Source:             CapabilitySource{Kind: SubmissionSourceBridge, Product: "p", ResourceID: "r"},
		At:                 now,
		CredentialContract: "cc",
		Evidence: model.CapabilityEvidence{
			Provenance:      "fake-verifier",
			RuntimeProofRef: "ref1",
			ExpiresAt:       expiry,
		},
	}
	p := v.VerifyCapabilityProof(context.Background(), q)
	if !p.RuntimeVerified {
		t.Fatal("expected verified")
	}
	if p.Identity.CredentialContract != "cc" {
		t.Fatalf("expected credential contract cc, got %s", p.Identity.CredentialContract)
	}
}

func TestFakeCapabilityProofVerifier_RejectsWrongProvenance(t *testing.T) {
	v := NewFakeCapabilityProofVerifier("fake-verifier")
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	q := CapabilityProofQuery{
		Key: CellKey{TenantID: 1, ShopID: "s", Product: "p", Region: "CN", Action: "a", Carrier: ModeBridge},
		At:  now,
		Evidence: model.CapabilityEvidence{
			Provenance:      "other",
			RuntimeProofRef: "ref1",
			ExpiresAt:       expiry,
		},
	}
	p := v.VerifyCapabilityProof(context.Background(), q)
	if p.RuntimeVerified {
		t.Fatal("expected not verified")
	}
}
