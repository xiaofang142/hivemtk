package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var ErrCapabilityCollectionUnavailable = errors.New("capability collection unavailable")
var ErrCapabilityCollectionInvalid = errors.New("capability observation identity invalid")

// CapabilityCollector 仅由可信服务端适配器实现，不能由请求正文注册或替换。
type CapabilityCollector interface {
	CurrentCapabilityContract(context.Context, capregistry.CellKey) (string, error)
	CollectCapability(context.Context, capregistry.CellKey, capregistry.CapabilitySource, model.MerchantConnection) (CapabilityObservation, error)
}

type CapabilityObservation struct {
	Identity              capregistry.CapabilityProofIdentity
	ObservedAt, ExpiresAt time.Time
	Scopes                []string
	Simulated             bool
}

type CapabilityCollectionService interface {
	Collect(context.Context, capregistry.CellKey, capregistry.CapabilitySource) error
}

type capabilityCollectionService struct {
	repo       repository.CapabilityObservationRepository
	collectors map[string]CapabilityCollector
}

func NewCapabilityCollectionService(repo repository.CapabilityObservationRepository, collectors map[string]CapabilityCollector) CapabilityCollectionService {
	return &capabilityCollectionService{repo: repo, collectors: cloneCapabilityCollectors(collectors)}
}

func cloneCapabilityCollectors(input map[string]CapabilityCollector) map[string]CapabilityCollector {
	out := make(map[string]CapabilityCollector, len(input))
	for product, collector := range input {
		out[product] = collector
	}
	return out
}

func validCapabilityCollectionSource(carrier, source string) bool {
	return carrier == capregistry.ModeBridge && source == capregistry.SubmissionSourceBridge ||
		carrier == capregistry.ModeAPIAdapter && source == capregistry.SubmissionSourceAfterSale
}

// Collect 在事务外采证，持久化时仓储重新核对连接，避免慢响应恢复过期授权。
func (s *capabilityCollectionService) Collect(ctx context.Context, key capregistry.CellKey, source capregistry.CapabilitySource) error {
	id, ok := tenant.FromContext(ctx)
	definition, found := capregistry.FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
	if !ok || tenant.IsSystemScope(ctx) || id != key.TenantID || key.ShopID == "" || !found || source.Product != key.Product || source.ResourceID == "" ||
		!validCapabilityCollectionSource(key.Carrier, source.Kind) || definition.ExecutionMode != key.Carrier {
		return ErrCapabilityCollectionInvalid
	}
	collector := s.collectors[key.Product]
	if s.repo == nil || collector == nil {
		return ErrCapabilityCollectionUnavailable
	}
	connection, err := s.repo.FindCapabilityConnection(ctx, key)
	if err != nil {
		return err
	}
	started := time.Now().UTC()
	if connection.ID == 0 || connection.TenantID != id || connection.Provider != key.Product || connection.ShopID != key.ShopID || connection.Revoked ||
		connection.Version <= 0 || connection.CredentialVersion <= 0 || connection.ConsentVersion <= 0 || connection.CredentialContract == "" || connection.ExpiresAt == nil || !started.Before(*connection.ExpiresAt) ||
		connection.AttemptExpiresAt != nil || connection.AttemptFailed || connection.RefreshUntil != nil || connection.RefreshFailed {
		return ErrCapabilityCollectionInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	contract, err := collector.CurrentCapabilityContract(bounded, key)
	if err != nil || contract == "" || contract != connection.CredentialContract {
		return ErrCapabilityCollectionInvalid
	}
	collectorConnection := connection
	collectorConnection.Scopes = slices.Clone(connection.Scopes)
	collectorExpiry := *connection.ExpiresAt
	collectorConnection.ExpiresAt = &collectorExpiry
	collectorConnection.CredentialCiphertext = ""
	collectorConnection.VerifierCiphertext = ""
	observation, err := collector.CollectCapability(bounded, key, source, collectorConnection)
	if err != nil {
		return err
	}
	currentContract, err := collector.CurrentCapabilityContract(bounded, key)
	if err != nil || currentContract != contract {
		return ErrCapabilityCollectionInvalid
	}
	if err := bounded.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	expected := capregistry.CapabilityProofIdentity{Key: key, Source: source, Provenance: observation.Identity.Provenance, ProofRef: observation.Identity.ProofRef,
		CredentialContract: connection.CredentialContract, ConnectionID: connection.ID, ConnectionVersion: connection.Version, CredentialVersion: connection.CredentialVersion, ConsentVersion: connection.ConsentVersion}
	if observation.Identity != expected || expected.Provenance == "" || len(expected.Provenance) > 64 || expected.ProofRef == "" || len(expected.ProofRef) > 160 ||
		observation.ObservedAt.Before(started) || observation.ObservedAt.After(now) || !now.Before(observation.ExpiresAt) || observation.ExpiresAt.After(*connection.ExpiresAt) {
		return ErrCapabilityCollectionInvalid
	}
	for _, scope := range observation.Scopes {
		if !slices.Contains(connection.Scopes, scope) {
			return ErrCapabilityCollectionInvalid
		}
	}
	for _, scope := range definition.RequiredScopes {
		if !slices.Contains(observation.Scopes, scope) {
			return ErrCapabilityCollectionInvalid
		}
	}
	binding := model.CapabilitySourceBinding{TenantID: id, Source: source.Kind, Product: key.Product, ResourceID: source.ResourceID, ShopID: key.ShopID, Region: key.Region, ConnectionID: connection.ID, ConsentVersion: connection.ConsentVersion}
	evidence := model.CapabilityEvidence{TenantID: id, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier,
		ConnectionID: connection.ID, ConnectionVersion: connection.Version, CredentialVersion: connection.CredentialVersion, ConsentVersion: connection.ConsentVersion,
		ContractVersion: definition.ContractVersion, DocumentationVersion: definition.DocumentationVersion, Scopes: slices.Clone(observation.Scopes), StateLimits: slices.Clone(definition.StateLimits), ExpiresAt: observation.ExpiresAt,
		Documentation: true, Implementation: true, Provenance: expected.Provenance, RuntimeProofRef: expected.ProofRef, Simulated: observation.Simulated}
	return s.repo.StoreCapabilityObservation(bounded, binding, evidence, connection.CredentialContract)
}
