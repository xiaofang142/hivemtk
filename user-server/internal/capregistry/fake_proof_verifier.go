package capregistry

import (
	"context"
)

// FakeCapabilityProofVerifier is a dev/test-only verifier that approves proofs when
// evidence.Provenance equals ProvenanceAllow and RuntimeProofRef is non-empty and
// not simulated. It is NOT for production use.
type FakeCapabilityProofVerifier struct {
	ProvenanceAllow string
}

func NewFakeCapabilityProofVerifier(provenanceAllow string) *FakeCapabilityProofVerifier {
	if provenanceAllow == "" {
		provenanceAllow = "fake-verifier"
	}
	return &FakeCapabilityProofVerifier{ProvenanceAllow: provenanceAllow}
}

func (v *FakeCapabilityProofVerifier) VerifyCapabilityProof(ctx context.Context, q CapabilityProofQuery) CapabilityProof {
	if v == nil || ctx.Err() != nil {
		return CapabilityProof{}
	}
	if q.Evidence.Simulated {
		return CapabilityProof{}
	}
	if q.Evidence.Provenance != v.ProvenanceAllow || q.Evidence.RuntimeProofRef == "" || q.At.IsZero() || q.At.After(q.Evidence.ExpiresAt) {
		return CapabilityProof{}
	}
	// For fake verifier, we trust the stored evidence’s contract/version fields are consistent
	// with what was checked upstream? The resolver already validated contract/doc versions.
	return CapabilityProof{
		Identity: CapabilityProofIdentity{
			Key:                q.Key,
			Source:             q.Source,
			Provenance:         q.Evidence.Provenance,
			ProofRef:           q.Evidence.RuntimeProofRef,
			CredentialContract: q.CredentialContract,
			ConnectionID:       q.Evidence.ConnectionID,
			ConnectionVersion:  q.Evidence.ConnectionVersion,
			CredentialVersion:  q.Evidence.CredentialVersion,
			ConsentVersion:     q.Evidence.ConsentVersion,
		},
		RuntimeVerified:     true,
		PreflightSource:     q.Source,
		PreflightObservedAt: q.At,
		PreflightExpires:    q.Evidence.ExpiresAt,
	}
}
