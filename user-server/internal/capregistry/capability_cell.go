package capregistry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// CellKey is the exact lookup key for one tenant/shop/product/region/action/carrier cell.
type CellKey struct {
	TenantID uint   `json:"tenant_id"`
	ShopID   string `json:"shop_id"`
	Product  string `json:"product"`
	Region   string `json:"region"`
	Action   string `json:"action"`
	Carrier  string `json:"carrier"`
}

// CellDefinition extends an existing capregistry entry with a statically declared cell.
// This is a default/contract only; it is never authorization on its own.
type CellDefinition struct {
	Product              string   `json:"product"`
	Region               string   `json:"region"`
	Action               string   `json:"action"`
	Carrier              string   `json:"carrier"`
	ExecutionMode        string   `json:"execution_mode"`
	ContractVersion      string   `json:"contract_version"`
	RequiredScopes       []string `json:"required_scopes"`
	StateLimits          []string `json:"state_limits"`
	DocumentationVersion string   `json:"documentation_version"`
	Experimental         bool     `json:"experimental"`
}

const (
	ModeBridge          = "bridge"
	ModeAPIAdapter      = "api_adapter"
	ModeOfficialUI      = "official_ui"
	ModePlatformManaged = "platform_managed"
)

type ConnectionState string

const (
	ConnectionUnconfigured ConnectionState = "unconfigured"
	ConnectionConnecting   ConnectionState = "connecting"
	ConnectionReady        ConnectionState = "ready"
	ConnectionReauth       ConnectionState = "reauth"
	ConnectionDisabled     ConnectionState = "disabled"
)

type EvidenceKind string

const (
	EvidenceDocumentation  EvidenceKind = "D"
	EvidenceImplementation EvidenceKind = "I"
	EvidenceRuntime        EvidenceKind = "R"
)

// EvidenceRecord is intentionally reference-free so internal URLs, credentials, and raw
// evidence cannot accidentally be copied into a capability summary.
type EvidenceRecord struct {
	Kind      EvidenceKind `json:"kind"`
	Simulated bool         `json:"simulated,omitempty"`
}

// CellEvidence is trusted server-side shop evidence. HTTP request bodies must never be
// mapped directly to this type; the current resolver API has no evidence-write endpoint.
type CellEvidence struct {
	TenantID             uint             `json:"tenant_id"`
	ShopID               string           `json:"shop_id"`
	Product              string           `json:"product"`
	Region               string           `json:"region"`
	Action               string           `json:"action"`
	Carrier              string           `json:"carrier"`
	ConnectionState      ConnectionState  `json:"connection_state"`
	Revoked              bool             `json:"revoked"`
	Scopes               []string         `json:"scopes"`
	Version              string           `json:"version"`
	DocumentationVersion string           `json:"documentation_version"`
	ExpiresAt            *time.Time       `json:"expires_at,omitempty"`
	StateLimits          []string         `json:"state_limits"`
	StatePreflightPassed bool             `json:"state_preflight_passed"`
	Evidence             []EvidenceRecord `json:"evidence"`
}

type EvidenceCoverage struct {
	Documentation  bool `json:"documentation"`
	Implementation bool `json:"implementation"`
	Runtime        bool `json:"runtime"`
}

// CellResolution is a safe, secret-free status view for one exact capability cell.
type CellResolution struct {
	TenantID             uint             `json:"tenant_id"`
	ShopID               string           `json:"shop_id"`
	Product              string           `json:"product"`
	Region               string           `json:"region"`
	Action               string           `json:"action"`
	Carrier              string           `json:"carrier"`
	ExecutionMode        string           `json:"execution_mode"`
	ConnectionState      ConnectionState  `json:"connection_state"`
	Status               string           `json:"status"`
	Scopes               []string         `json:"scopes"`
	Version              string           `json:"version,omitempty"`
	DocumentationVersion string           `json:"documentation_version,omitempty"`
	ExpiresAt            *time.Time       `json:"expires_at,omitempty"`
	StateLimits          []string         `json:"state_limits"`
	StatePreflightPassed bool             `json:"state_preflight_passed"`
	Evidence             EvidenceCoverage `json:"evidence"`
	ReasonCode           string           `json:"reason_code"`
	Experimental         bool             `json:"experimental"`
	SimulationAllowed    bool             `json:"simulation_allowed"`
	ProductionAllowed    bool             `json:"production_allowed"`
	PausePending         bool             `json:"pause_pending"`
}

type evidenceContextKey struct{}
type submissionBindingContextKey struct{}

const (
	SubmissionSourceBridge    = "bridge"
	SubmissionSourceAfterSale = "aftersale"
)

// SubmissionBinding is populated only by trusted server-side identity resolution.
// Never decode it (or CellEvidence) from an HTTP request body. Bridge AccountID and
// after-sale Platform are source identifiers, not shop IDs; a resolver must explicitly
// bind them to the exact CellKey before a submit can be authorized.
type SubmissionBinding struct {
	Source     string
	Platform   string
	ResourceID string
	Key        CellKey
}

// SubmissionDeniedError is a machine-readable fail-closed decision.
type SubmissionDeniedError struct{ ReasonCode string }

func (e *SubmissionDeniedError) Error() string {
	if e == nil {
		return "capability_submit_denied"
	}
	return "capability_submit_denied: " + e.ReasonCode
}

func (e *SubmissionDeniedError) CapabilityReasonCode() string {
	if e == nil {
		return ""
	}
	return e.ReasonCode
}

func IsSubmissionDenied(err error) bool {
	var denied *SubmissionDeniedError
	return errors.As(err, &denied)
}

// WithSubmissionBinding attaches an identity binding produced by trusted server-side
// resolution. It is intentionally not exposed by any HTTP route.
func WithSubmissionBinding(ctx context.Context, binding SubmissionBinding) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, submissionBindingContextKey{}, binding)
}

func submissionBindingFromContext(ctx context.Context) (SubmissionBinding, bool) {
	if ctx == nil {
		return SubmissionBinding{}, false
	}
	binding, ok := ctx.Value(submissionBindingContextKey{}).(SubmissionBinding)
	return binding, ok
}

// WithCellEvidence attaches trusted, already-loaded evidence for server-side execution.
func WithCellEvidence(ctx context.Context, evidence CellEvidence) context.Context {
	return context.WithValue(ctx, evidenceContextKey{}, evidence)
}

// CellEvidenceFromContext returns evidence loaded by a trusted service, if any.
func CellEvidenceFromContext(ctx context.Context) (CellEvidence, bool) {
	if ctx == nil {
		return CellEvidence{}, false
	}
	evidence, ok := ctx.Value(evidenceContextKey{}).(CellEvidence)
	return evidence, ok
}

// AuthorizeBridgeSubmission is the common exact-cell gate for bridge outbox submit paths.
func AuthorizeBridgeSubmission(ctx context.Context, channel, accountID, msgType string, now time.Time) error {
	action, ok := bridgeActionForMessageType(msgType)
	if !ok {
		return &SubmissionDeniedError{ReasonCode: "unknown_capability"}
	}
	return authorizeSubmission(ctx, SubmissionSourceBridge, channel, accountID, action, ModeBridge, now)
}

// AuthorizeAfterSaleSubmission is the common exact-cell gate for external after-sale writes.
func AuthorizeAfterSaleSubmission(ctx context.Context, platform, orderID, action string, now time.Time) error {
	if action != "aftersale.write" && action != "refund.write" {
		return &SubmissionDeniedError{ReasonCode: "unknown_capability"}
	}
	return authorizeSubmission(ctx, SubmissionSourceAfterSale, platform, orderID, action, ModeAPIAdapter, now)
}

func authorizeSubmission(ctx context.Context, source, platform, resourceID, action, carrier string, now time.Time) error {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		return &SubmissionDeniedError{ReasonCode: "tenant_context_missing"}
	}
	if tenant.IsSystemScope(ctx) {
		return &SubmissionDeniedError{ReasonCode: "tenant_scope_invalid"}
	}
	if strings.TrimSpace(resourceID) == "" {
		return &SubmissionDeniedError{ReasonCode: "capability_mapping_incomplete"}
	}
	binding, ok := submissionBindingFromContext(ctx)
	if !ok {
		return &SubmissionDeniedError{ReasonCode: "capability_mapping_missing"}
	}
	if binding.Source != source || binding.Platform != platform || binding.ResourceID != resourceID {
		return &SubmissionDeniedError{ReasonCode: "request_binding_mismatch"}
	}
	key := binding.Key
	if key.TenantID != tenantID {
		return &SubmissionDeniedError{ReasonCode: "tenant_binding_mismatch"}
	}
	if !validCellKey(key) {
		return &SubmissionDeniedError{ReasonCode: "capability_mapping_incomplete"}
	}
	if key.Product != platform {
		return &SubmissionDeniedError{ReasonCode: "product_binding_mismatch"}
	}
	if key.Action != action || key.Carrier != carrier {
		return &SubmissionDeniedError{ReasonCode: "request_action_mismatch"}
	}
	if _, found := FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier); !found {
		return &SubmissionDeniedError{ReasonCode: "unknown_capability"}
	}
	evidence, ok := CellEvidenceFromContext(ctx)
	if !ok {
		return &SubmissionDeniedError{ReasonCode: "capability_evidence_missing"}
	}
	resolution := ResolveCell(key, &evidence, now.UTC())
	if !resolution.ProductionAllowed {
		reason := resolution.ReasonCode
		if reason == "" {
			reason = "capability_not_production_ready"
		}
		return &SubmissionDeniedError{ReasonCode: reason}
	}
	return nil
}

func bridgeActionForMessageType(msgType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(msgType)) {
	case "", "text":
		return "message.text.send", true
	case "image":
		return "message.image.send", true
	default:
		return "", false
	}
}

// CellCacheKey includes tenant, shop, and evidence version so no shop or version can share
// a cached decision. The current implementation deliberately does not cache decisions.
func CellCacheKey(key CellKey, version string) string {
	return fmt.Sprintf("%d|%d:%s|%d:%s|%d:%s|%d:%s|%d:%s|%d:%s",
		key.TenantID,
		len(key.ShopID), key.ShopID,
		len(key.Product), key.Product,
		len(key.Region), key.Region,
		len(key.Action), key.Action,
		len(key.Carrier), key.Carrier,
		len(version), version,
	)
}

// CellDefinitions returns action-level declarations for runtime and merchant workflows.
func CellDefinitions() []CellDefinition {
	out := make([]CellDefinition, 0, len(merchantWorkflowCells))
	for _, cell := range merchantWorkflowCells {
		out = append(out, cloneCellDefinition(cell))
	}
	for _, entry := range All() {
		for _, cell := range entry.Cells {
			out = append(out, cloneCellDefinition(cell))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := cellDefinitionKey(out[i])
		right := cellDefinitionKey(out[j])
		return left < right
	})
	return out
}

// FindCellDefinition looks up an exact static product/region/action/carrier declaration.
func FindCellDefinition(product, region, action, carrier string) (CellDefinition, bool) {
	for _, cell := range CellDefinitions() {
		if cell.Product == product && cell.Region == region && cell.Action == action && cell.Carrier == carrier {
			return cell, true
		}
	}
	return CellDefinition{}, false
}

// ResolveCell evaluates exact shop evidence against the existing static capability registry.
// Static/Experimental declarations never grant access without matching per-cell evidence.
func ResolveCell(key CellKey, evidence *CellEvidence, now time.Time) CellResolution {
	out := CellResolution{
		TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region,
		Action: key.Action, Carrier: key.Carrier, ConnectionState: ConnectionUnconfigured,
		Status: "blocked", Scopes: []string{}, StateLimits: []string{},
	}
	if !validCellKey(key) {
		out.ReasonCode = "unknown_capability"
		out.PausePending = true
		return out
	}
	definition, found := FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
	if !found {
		out.ReasonCode = "unknown_capability"
		out.PausePending = true
		return out
	}
	out.ExecutionMode = definition.ExecutionMode
	out.Experimental = definition.Experimental
	out.StateLimits = sortedUnique(definition.StateLimits)
	if evidence != nil && !evidenceMatches(key, *evidence) {
		out.ReasonCode = "shop_binding_mismatch"
		out.PausePending = true
		return out
	}
	workflowReason := workflowDenialReason(definition.ExecutionMode)
	if evidence == nil {
		out.ReasonCode = workflowReason
		if out.ReasonCode == "" {
			out.ReasonCode = "shop_evidence_missing"
		}
		out.PausePending = true
		return out
	}
	out.ConnectionState = evidence.ConnectionState
	out.Scopes = sortedUnique(evidence.Scopes)
	out.StateLimits = sortedUnique(append(out.StateLimits, evidence.StateLimits...))
	out.StatePreflightPassed = evidence.StatePreflightPassed
	out.Version = evidence.Version
	out.DocumentationVersion = evidence.DocumentationVersion
	out.ExpiresAt = cloneTime(evidence.ExpiresAt)
	out.Evidence = evidenceCoverage(evidence.Evidence)
	if evidence.Revoked {
		out.ConnectionState = ConnectionReauth
		out.ReasonCode = "permission_revoked"
		out.PausePending = true
		return out
	}
	if !validConnectionState(evidence.ConnectionState) {
		out.ConnectionState = ConnectionDisabled
		out.ReasonCode = "invalid_connection_state"
		out.PausePending = true
		return out
	}
	if workflowReason != "" {
		out.ReasonCode = workflowReason
		out.PausePending = true
		return out
	}
	if evidence.ConnectionState != ConnectionReady {
		out.ReasonCode = "connection_" + string(evidence.ConnectionState)
		out.PausePending = true
		return out
	}
	if evidence.ExpiresAt == nil {
		out.ConnectionState = ConnectionReauth
		out.ReasonCode = "evidence_expiry_missing"
		out.PausePending = true
		return out
	}
	if !now.Before(*evidence.ExpiresAt) {
		out.ConnectionState = ConnectionReauth
		out.ReasonCode = "evidence_expired"
		out.PausePending = true
		return out
	}
	if evidence.DocumentationVersion == "" || evidence.DocumentationVersion != definition.DocumentationVersion {
		out.ConnectionState = ConnectionReauth
		out.ReasonCode = "stale_documentation_version"
		out.PausePending = true
		return out
	}
	if evidence.Version == "" || evidence.Version != definition.ContractVersion {
		out.ConnectionState = ConnectionReauth
		out.ReasonCode = "stale_contract_version"
		out.PausePending = true
		return out
	}
	if !containsAll(evidence.Scopes, definition.RequiredScopes) {
		out.ReasonCode = "required_scope_missing"
		out.PausePending = true
		return out
	}
	if len(out.StateLimits) > 0 && !evidence.StatePreflightPassed {
		out.ReasonCode = "state_preflight_required"
		out.PausePending = true
		return out
	}
	if !out.Evidence.Documentation || !out.Evidence.Implementation || !out.Evidence.Runtime {
		out.ReasonCode = "cell_evidence_incomplete"
		out.PausePending = true
		return out
	}
	for _, record := range evidence.Evidence {
		if record.Simulated {
			out.Status = "simulated"
			out.SimulationAllowed = true
			out.ReasonCode = "pending_live_validation"
			out.PausePending = true
			return out
		}
	}
	out.Status = "ready"
	out.ProductionAllowed = true
	return out
}

func workflowDenialReason(mode string) string {
	switch mode {
	case ModeOfficialUI:
		return "official_workbench_required"
	case ModePlatformManaged:
		return "platform_managed_action"
	default:
		return ""
	}
}

func validCellKey(key CellKey) bool {
	return key.TenantID > 0 && strings.TrimSpace(key.ShopID) != "" && strings.TrimSpace(key.Product) != "" &&
		strings.TrimSpace(key.Region) != "" && strings.TrimSpace(key.Action) != "" && strings.TrimSpace(key.Carrier) != ""
}

func evidenceMatches(key CellKey, evidence CellEvidence) bool {
	return key.TenantID == evidence.TenantID && key.ShopID == evidence.ShopID && key.Product == evidence.Product &&
		key.Region == evidence.Region && key.Action == evidence.Action && key.Carrier == evidence.Carrier
}

func validConnectionState(state ConnectionState) bool {
	switch state {
	case ConnectionUnconfigured, ConnectionConnecting, ConnectionReady, ConnectionReauth, ConnectionDisabled:
		return true
	default:
		return false
	}
}

func evidenceCoverage(records []EvidenceRecord) EvidenceCoverage {
	var coverage EvidenceCoverage
	for _, record := range records {
		switch record.Kind {
		case EvidenceDocumentation:
			coverage.Documentation = true
		case EvidenceImplementation:
			coverage.Implementation = true
		case EvidenceRuntime:
			coverage.Runtime = true
		}
	}
	return coverage
}

func containsAll(actual, required []string) bool {
	set := make(map[string]struct{}, len(actual))
	for _, value := range actual {
		set[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cellDefinitionKey(cell CellDefinition) string {
	return strings.Join([]string{cell.Product, cell.Region, cell.Action, cell.Carrier}, "/")
}

func cloneCellDefinition(cell CellDefinition) CellDefinition {
	cell.RequiredScopes = append([]string(nil), cell.RequiredScopes...)
	cell.StateLimits = append([]string(nil), cell.StateLimits...)
	return cell
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
