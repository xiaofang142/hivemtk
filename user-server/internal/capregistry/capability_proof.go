package capregistry

import (
	"context"
	"maps"
	"time"
)

// CapabilityProofSource 只读取独立认证后的本地观测，不得在队列持锁期间请求外部平台。
// 实现必须按当前提供方配置重新给出契约，不能从查询中的证据引用推导认证结果。
type CapabilityProofSource interface {
	CurrentCapabilityContract(context.Context, CellKey) (string, error)
	FindCapabilityProof(context.Context, CapabilityProofQuery) (CapabilityVerifiedObservation, error)
}

type CapabilityProofSourceKey struct{ Product, Provenance string }

type CapabilityVerifiedObservation struct {
	Proof                 CapabilityProof
	ObservedAt, ExpiresAt time.Time
	Simulated             bool
}

type LocalCapabilityProofVerifier struct {
	sources map[CapabilityProofSourceKey]CapabilityProofSource
}

func NewLocalCapabilityProofVerifier(sources map[CapabilityProofSourceKey]CapabilityProofSource) *LocalCapabilityProofVerifier {
	return &LocalCapabilityProofVerifier{sources: maps.Clone(sources)}
}

// VerifyCapabilityProof 不信任持久引用本身；只有精确匹配的本地验证源能提供运行证明。
func (v *LocalCapabilityProofVerifier) VerifyCapabilityProof(ctx context.Context, query CapabilityProofQuery) CapabilityProof {
	if v == nil || ctx.Err() != nil || query.At.IsZero() || query.Evidence.Simulated || query.Evidence.Provenance == "" || query.Evidence.RuntimeProofRef == "" || !query.At.Before(query.Evidence.ExpiresAt) {
		return CapabilityProof{}
	}
	source := v.sources[CapabilityProofSourceKey{Product: query.Key.Product, Provenance: query.Evidence.Provenance}]
	if source == nil {
		return CapabilityProof{}
	}
	contract, err := source.CurrentCapabilityContract(ctx, query.Key)
	if err != nil || contract == "" {
		return CapabilityProof{}
	}
	observation, err := source.FindCapabilityProof(ctx, query)
	if err != nil || ctx.Err() != nil || observation.Simulated || !observation.Proof.RuntimeVerified || observation.ObservedAt.IsZero() || observation.ObservedAt.After(query.At) || !query.At.Before(observation.ExpiresAt) {
		return CapabilityProof{}
	}
	expected := CapabilityProofIdentity{Key: query.Key, Source: query.Source, Provenance: query.Evidence.Provenance, ProofRef: query.Evidence.RuntimeProofRef, CredentialContract: contract,
		ConnectionID: query.Evidence.ConnectionID, ConnectionVersion: query.Evidence.ConnectionVersion, CredentialVersion: query.Evidence.CredentialVersion, ConsentVersion: query.Evidence.ConsentVersion}
	if observation.Proof.Identity != expected {
		return CapabilityProof{}
	}
	current, err := source.CurrentCapabilityContract(ctx, query.Key)
	if err != nil || current != contract || ctx.Err() != nil {
		return CapabilityProof{}
	}
	return observation.Proof
}
