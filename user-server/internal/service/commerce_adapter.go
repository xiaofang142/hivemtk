package service

import (
	"context"
	"hivemtk-user/internal/dto"
	"time"
)

// CommerceSnapshot is supplied only by an approved injected provider, never by an HTTP DTO.
type CommerceSnapshot struct {
	Version           string     `json:"version"`
	NativeState       string     `json:"native_state"`
	FetchedAt         time.Time  `json:"fetched_at"`
	Currency          string     `json:"currency"`
	RefundableMinor   int64      `json:"refundable_minor"`
	AvailableQuantity int64      `json:"available_quantity"`
	AddressDigest     string     `json:"address_digest"`
	CustomerReview    bool       `json:"customer_review"`
	AllowedActions    []string   `json:"allowed_actions"`
	Obligations       []string   `json:"obligations"`
	CaseRef           string     `json:"case_ref"`
	PaymentRef        string     `json:"payment_ref"`
	Source            string     `json:"source"`
	Deadline          *time.Time `json:"deadline,omitempty"`
}
type CommerceAdapterRequest struct {
	TenantID                                                                                             uint
	Profile, Product, Region, ShopID, ExternalOrderID, Action, OperationID, BusinessKey, ExpectedVersion string
	ExternalRef                                                                                          string
	CredentialRef                                                                                        uint
	CredentialVersion                                                                                    int64
	WriteOwner                                                                                           string
	Parameters                                                                                           dto.CommerceParameters
}
type CommerceQuote struct {
	Snapshot      CommerceSnapshot
	Obligations   []string
	FeesKnown     bool
	PolicyVersion string
}
type CommerceAdapterResult struct {
	State, NativeState, ExternalRef, EvidenceRef, EvidenceSource, FundsState, Currency, Action, AddressDigest string
	AmountMinor                                                                                               int64
	CustomerReview                                                                                            bool
	Postcondition                                                                                             bool
	Authoritative                                                                                             bool
	DefinitelyNotDispatched                                                                                   bool
	Reason                                                                                                    string
	RetryAfter                                                                                                time.Duration
}

// CommerceAdapter separates readonly preflight and observation from an approved business submit.
// Providers must enforce current contract, business errors, identity, request timeout and no PII logging.
type CommerceAdapter interface {
	Read(context.Context, CommerceAdapterRequest) (CommerceSnapshot, error)
	Preflight(context.Context, CommerceAdapterRequest) (CommerceQuote, error)
	Submit(context.Context, CommerceAdapterRequest) (CommerceAdapterResult, error)
	Observe(context.Context, CommerceAdapterRequest) (CommerceAdapterResult, error)
}

// ApprovedCommerceTransport is a configured connector boundary with an already approved wire contract.
// It deliberately accepts semantic requests instead of inventing vendor URLs or signature layouts.
type ApprovedCommerceTransport interface {
	CommerceAdapter
	ContractVersion() string
	Product() string
	Region() string
}
type connectorCommerceAdapter struct {
	transport ApprovedCommerceTransport
	profile   dto.CommerceProfileResponse
}

func NewConnectorCommerceAdapter(profile dto.CommerceProfileResponse, transport ApprovedCommerceTransport) (CommerceAdapter, error) {
	if transport == nil || transport.ContractVersion() != profile.Version || transport.Product() != profile.Product || transport.Region() != profile.Region {
		return nil, ErrCommerceProvider
	}
	return &connectorCommerceAdapter{transport: transport, profile: profile}, nil
}
func (a *connectorCommerceAdapter) Read(ctx context.Context, r CommerceAdapterRequest) (CommerceSnapshot, error) {
	if !a.matches(r) {
		return CommerceSnapshot{}, ErrCommerceProvider
	}
	return a.transport.Read(ctx, r)
}
func (a *connectorCommerceAdapter) Preflight(ctx context.Context, r CommerceAdapterRequest) (CommerceQuote, error) {
	if !a.matches(r) {
		return CommerceQuote{}, ErrCommerceProvider
	}
	return a.transport.Preflight(ctx, r)
}
func (a *connectorCommerceAdapter) Submit(ctx context.Context, r CommerceAdapterRequest) (CommerceAdapterResult, error) {
	if !a.matches(r) {
		return CommerceAdapterResult{}, ErrCommerceProvider
	}
	return a.transport.Submit(ctx, r)
}
func (a *connectorCommerceAdapter) Observe(ctx context.Context, r CommerceAdapterRequest) (CommerceAdapterResult, error) {
	if !a.matches(r) {
		return CommerceAdapterResult{}, ErrCommerceProvider
	}
	return a.transport.Observe(ctx, r)
}
func (a *connectorCommerceAdapter) matches(r CommerceAdapterRequest) bool {
	return r.Profile == a.profile.ID && r.Product == a.profile.Product && r.Region == a.profile.Region && r.CredentialRef != 0 && r.CredentialVersion > 0
}
