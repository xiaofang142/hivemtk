package capregistry

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// CapabilitySource identifies a resource to look up. It conveys no authority.
type CapabilitySource struct {
	Kind, Product, ResourceID string
}

// CapabilityEvidenceReader reads tenant-scoped facts. A transaction reader locks the
// connection, then rechecks the binding under lock, then locks the evidence row.
type CapabilityEvidenceReader interface {
	FindCapabilityBinding(context.Context, uint, CapabilitySource, bool) (model.CapabilitySourceBinding, error)
	FindCapabilityConnection(context.Context, CellKey) (model.MerchantConnection, error)
	FindCapabilityEvidence(context.Context, CellKey) (model.CapabilityEvidence, error)
}

// CapabilityProofVerifier looks up independently authenticated runtime evidence and
// resource-level preflight. Implementations must be local reads, including under queue locks.
// No verifier is configured by the default production constructors in this slice.
type CapabilityProofVerifier interface {
	VerifyCapabilityProof(context.Context, CapabilityProofQuery) CapabilityProof
}

type CapabilityProofQuery struct {
	Key                CellKey
	Source             CapabilitySource
	Evidence           model.CapabilityEvidence
	At                 time.Time
	CredentialContract string
}

type CapabilityProofIdentity struct {
	Key                                                  CellKey
	Source                                               CapabilitySource
	Provenance, ProofRef, CredentialContract             string
	ConnectionID                                         uint
	ConnectionVersion, CredentialVersion, ConsentVersion int64
}

type CapabilityProof struct {
	Identity                              CapabilityProofIdentity
	RuntimeVerified                       bool
	PreflightSource                       CapabilitySource
	PreflightObservedAt, PreflightExpires time.Time
}

// PersistentCapabilityResolver evaluates fresh persistent facts for diagnostics and execution.
type PersistentCapabilityResolver struct {
	Reader   CapabilityEvidenceReader
	Verifier CapabilityProofVerifier
}

func CapabilityBridgeTarget(channel, accountID, messageType string) (CapabilitySource, string, bool) {
	action, ok := bridgeActionForMessageType(messageType)
	return CapabilitySource{Kind: SubmissionSourceBridge, Product: channel, ResourceID: accountID}, action, ok
}

func (r PersistentCapabilityResolver) Resolve(ctx context.Context, key CellKey, now time.Time) CellResolution {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || key.TenantID != id {
		return deniedCell(key, nil, now, "tenant_scope_invalid")
	}
	definition, found := FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
	if !found || !validCellKey(key) || workflowDenialReason(definition.ExecutionMode) != "" {
		return ResolveCell(key, nil, now)
	}
	if r.Reader == nil {
		return deniedCell(key, nil, now, "capability_storage_unavailable")
	}
	connection, err := r.Reader.FindCapabilityConnection(ctx, key)
	if err != nil {
		return deniedCell(key, nil, now, "capability_storage_unavailable")
	}
	return r.resolve(ctx, key, CapabilitySource{}, connection, now)
}

// CapabilityExecutionIdentity records the connection revision actually used by a transport.
type CapabilityExecutionIdentity struct {
	ConnectionID                                         uint
	ConnectionVersion, CredentialVersion, ConsentVersion int64
	CredentialContract                                   string
}

// AuthorizeExecution compares the transport's actual credential revision with this same decision's connection.
func (r PersistentCapabilityResolver) AuthorizeExecution(ctx context.Context, source CapabilitySource, key CellKey, execution CapabilityExecutionIdentity, now time.Time) error {
	id, ok := tenant.FromContext(ctx)
	if !ok || !validCellKey(key) || key.TenantID != id || key.Product != source.Product {
		return submissionDenial("tenant_scope_invalid")
	}
	return r.authorize(ctx, source, key.Action, key.Carrier, key.ShopID, key.Region, &execution, now)
}

// Authorize matches a source mapping; shop and region are optional assertions, never ownership proof.
func (r PersistentCapabilityResolver) Authorize(ctx context.Context, source CapabilitySource, action, carrier, shop, region string, now time.Time) error {
	return r.authorize(ctx, source, action, carrier, shop, region, nil, now)
}

func (r PersistentCapabilityResolver) authorize(ctx context.Context, source CapabilitySource, action, carrier, shop, region string, execution *CapabilityExecutionIdentity, now time.Time) error {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return submissionDenial("tenant_scope_invalid")
	}
	if source.Product == "" || source.ResourceID == "" || action == "" {
		return submissionDenial("capability_mapping_incomplete")
	}
	if r.Reader == nil {
		return submissionDenial("capability_storage_unavailable")
	}
	binding, err := r.Reader.FindCapabilityBinding(ctx, id, source, false)
	if err != nil {
		return submissionDenial("capability_storage_unavailable")
	}
	if binding.ID == 0 {
		return submissionDenial("capability_mapping_missing")
	}
	if binding.TenantID != id || binding.Source != source.Kind || binding.Product != source.Product || binding.ResourceID != source.ResourceID ||
		binding.ShopID == "" || binding.Region == "" || binding.ConnectionID == 0 || binding.ConsentVersion <= 0 {
		return submissionDenial("request_binding_mismatch")
	}
	if (shop != "" && shop != binding.ShopID) || (region != "" && region != binding.Region) {
		return submissionDenial("shop_binding_mismatch")
	}
	key := CellKey{TenantID: id, ShopID: binding.ShopID, Product: binding.Product, Region: binding.Region, Action: action, Carrier: carrier}
	if _, ok := FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier); !ok {
		return submissionDenial("unknown_capability")
	}
	connection, err := r.Reader.FindCapabilityConnection(ctx, key)
	if err != nil {
		return submissionDenial("capability_storage_unavailable")
	}
	current, err := r.Reader.FindCapabilityBinding(ctx, id, source, true)
	if err != nil {
		return submissionDenial("capability_storage_unavailable")
	}
	if current != binding || connection.ID != current.ConnectionID || connection.ConsentVersion != current.ConsentVersion {
		return submissionDenial("request_binding_mismatch")
	}
	if current.Revoked {
		return submissionDenial("permission_revoked")
	}
	if execution != nil && (*execution != (CapabilityExecutionIdentity{
		ConnectionID: connection.ID, ConnectionVersion: connection.Version, CredentialVersion: connection.CredentialVersion,
		ConsentVersion: connection.ConsentVersion, CredentialContract: connection.CredentialContract,
	}) || execution.ConnectionID == 0 || execution.CredentialContract == "") {
		return submissionDenial("execution_target_unbound")
	}
	result := r.resolve(ctx, key, source, connection, now)
	if !result.ProductionAllowed {
		return submissionDenial(result.ReasonCode)
	}
	return nil
}

func (r PersistentCapabilityResolver) resolve(ctx context.Context, key CellKey, source CapabilitySource, connection model.MerchantConnection, now time.Time) CellResolution {
	if connection.ID == 0 {
		return ResolveCell(key, nil, now)
	}
	if connection.TenantID != key.TenantID || connection.Provider != key.Product || connection.ShopID != key.ShopID {
		return deniedCell(key, nil, now, "shop_binding_mismatch")
	}
	evidence := CellEvidence{
		TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier,
		ConnectionState: ConnectionReady, Revoked: connection.Revoked, ExpiresAt: connection.ExpiresAt, Scopes: connection.Scopes,
	}
	switch {
	case connection.Revoked:
		return ResolveCell(key, &evidence, now)
	case connection.AttemptExpiresAt != nil && now.Before(*connection.AttemptExpiresAt):
		evidence.ConnectionState = ConnectionConnecting
		return ResolveCell(key, &evidence, now)
	case connection.AttemptExpiresAt != nil || connection.AttemptFailed || connection.RefreshUntil != nil || connection.RefreshFailed:
		evidence.ConnectionState = ConnectionReauth
		return ResolveCell(key, &evidence, now)
	case connection.CredentialVersion <= 0 || connection.ConsentVersion <= 0 || connection.Version <= 0 || connection.CredentialContract == "":
		evidence.ConnectionState = ConnectionUnconfigured
		return ResolveCell(key, &evidence, now)
	case connection.ExpiresAt == nil || !now.Before(*connection.ExpiresAt):
		return ResolveCell(key, &evidence, now)
	}
	row, err := r.Reader.FindCapabilityEvidence(ctx, key)
	if err != nil {
		return deniedCell(key, &evidence, now, "capability_storage_unavailable")
	}
	if row.ID == 0 {
		return deniedCell(key, &evidence, now, "shop_evidence_missing")
	}
	if row.TenantID != key.TenantID || row.ShopID != key.ShopID || row.Product != key.Product || row.Region != key.Region || row.Action != key.Action || row.Carrier != key.Carrier {
		return deniedCell(key, &evidence, now, "shop_binding_mismatch")
	}
	if row.ConnectionID != connection.ID || row.ConnectionVersion != connection.Version || row.CredentialVersion != connection.CredentialVersion || row.ConsentVersion != connection.ConsentVersion {
		return deniedCell(key, &evidence, now, "stale_connection_version")
	}
	evidence.Version, evidence.DocumentationVersion = row.ContractVersion, row.DocumentationVersion
	evidence.StateLimits = row.StateLimits
	evidence.Scopes = intersectCapabilityScopes(connection.Scopes, row.Scopes)
	if row.ExpiresAt.Before(*connection.ExpiresAt) {
		evidence.ExpiresAt = &row.ExpiresAt
	}
	if row.Documentation {
		evidence.Evidence = append(evidence.Evidence, EvidenceRecord{Kind: EvidenceDocumentation})
	}
	if row.Implementation {
		evidence.Evidence = append(evidence.Evidence, EvidenceRecord{Kind: EvidenceImplementation})
	}
	if r.Verifier != nil && !row.Simulated && row.Provenance != "" && row.RuntimeProofRef != "" && now.Before(*evidence.ExpiresAt) {
		proof := r.Verifier.VerifyCapabilityProof(ctx, CapabilityProofQuery{Key: key, Source: source, Evidence: row, At: now, CredentialContract: connection.CredentialContract})
		expected := CapabilityProofIdentity{Key: key, Source: source, Provenance: row.Provenance, ProofRef: row.RuntimeProofRef,
			CredentialContract: connection.CredentialContract, ConnectionID: row.ConnectionID, ConnectionVersion: row.ConnectionVersion,
			CredentialVersion: row.CredentialVersion, ConsentVersion: row.ConsentVersion}
		if proof.RuntimeVerified && proof.Identity == expected {
			evidence.Evidence = append(evidence.Evidence, EvidenceRecord{Kind: EvidenceRuntime})
			evidence.StatePreflightPassed = source.ResourceID != "" && proof.PreflightSource == source &&
				!proof.PreflightObservedAt.IsZero() && !proof.PreflightObservedAt.After(now) && now.Before(proof.PreflightExpires)
		}
	}
	result := ResolveCell(key, &evidence, now)
	if result.ReasonCode == "cell_evidence_incomplete" && row.Documentation && row.Implementation && !result.Evidence.Runtime {
		result.ReasonCode = "pending_live_validation"
	}
	return result
}

func intersectCapabilityScopes(connection, evidence []string) []string {
	current := make(map[string]bool, len(connection))
	for _, scope := range connection {
		current[scope] = true
	}
	var scopes []string
	for _, scope := range evidence {
		if current[scope] {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func deniedCell(key CellKey, evidence *CellEvidence, now time.Time, reason string) CellResolution {
	result := ResolveCell(key, evidence, now)
	result.Status, result.ReasonCode = "blocked", reason
	result.ProductionAllowed, result.PausePending = false, true
	return result
}

func submissionDenial(reason string) error { return &SubmissionDeniedError{ReasonCode: reason} }
