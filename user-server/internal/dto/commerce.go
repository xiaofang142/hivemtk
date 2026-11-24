package dto

import "time"

// CommerceTarget identifies a shop and existing mirror; tenant/actor are server context only.
type CommerceTarget struct {
	Profile string `json:"profile"`
	ShopID  string `json:"shop_id"`
	OrderID uint   `json:"order_id"`
}
type CommerceParameters struct {
	Recipient        string   `json:"recipient,omitempty"`
	Phone            string   `json:"phone,omitempty"`
	Address          string   `json:"address,omitempty"`
	Province         string   `json:"province,omitempty"`
	City             string   `json:"city,omitempty"`
	District         string   `json:"district,omitempty"`
	AmountMinor      int64    `json:"amount_minor,omitempty"`
	Currency         string   `json:"currency,omitempty"`
	Quantity         int64    `json:"quantity,omitempty"`
	LineID           string   `json:"line_id,omitempty"`
	CaseRef          string   `json:"case_ref,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	SKU              string   `json:"sku,omitempty"`
	LogisticsRef     string   `json:"logistics_ref,omitempty"`
	WarehouseRef     string   `json:"warehouse_ref,omitempty"`
	PaymentPath      string   `json:"payment_path,omitempty"`
	Acknowledgements []string `json:"acknowledgements,omitempty"`
}
type CommerceProposeRequest struct {
	CommerceTarget
	Action          string             `json:"action"`
	IntentID        string             `json:"intent_id"`
	Revision        int64              `json:"revision"`
	ExpectedVersion string             `json:"expected_version"`
	AfterSaleID     uint               `json:"after_sale_id,omitempty"`
	OrderChangeID   uint               `json:"order_change_id,omitempty"`
	ParentID        string             `json:"parent_id,omitempty"`
	Parameters      CommerceParameters `json:"parameters"`
}
type CommercePolicyRequest struct {
	Profile         string `json:"profile"`
	ShopID          string `json:"shop_id"`
	ExpectedVersion int64  `json:"expected_version"`
	WriteEnabled    bool   `json:"write_enabled"`
	WriteOwner      string `json:"write_owner"`
	ApprovalMode    string `json:"approval_mode"`
	OwnerID         uint   `json:"owner_id"`
	RecoveryEpoch   int64  `json:"recovery_epoch"`
}
type CommerceMemberRequest struct {
	Profile         string `json:"profile"`
	ShopID          string `json:"shop_id"`
	ActorID         uint   `json:"actor_id"`
	ExpectedVersion int64  `json:"expected_version"`
	CanRead         bool   `json:"can_read"`
	CanExecute      bool   `json:"can_execute"`
	CanApprove      bool   `json:"can_approve"`
	CanReview       bool   `json:"can_review"`
	LimitMinor      int64  `json:"limit_minor"`
	Currency        string `json:"currency"`
}
type CommerceTransitionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	PreflightHash   string `json:"preflight_hash,omitempty"`
}
type CommerceEvidenceRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reference       string `json:"reference"`
	NativeState     string `json:"native_state"`
	FundsState      string `json:"funds_state"`
	Outcome         string `json:"outcome"`
	EvidenceID      string `json:"evidence_id,omitempty"`
}
type CommerceActionResponse struct {
	Action         string   `json:"action"`
	Mode           string   `json:"mode"`
	Method         string   `json:"method,omitempty"`
	RequiredFields []string `json:"required_fields"`
	Obligations    []string `json:"obligations"`
	Postcondition  string   `json:"postcondition"`
	Scope          string   `json:"scope"`
}
type CommerceProfileResponse struct {
	ID                 string                   `json:"id"`
	Product            string                   `json:"product"`
	Region             string                   `json:"region"`
	Version            string                   `json:"version"`
	Mode               string                   `json:"mode"`
	Role               string                   `json:"role"`
	Steps              []string                 `json:"steps"`
	Actions            []CommerceActionResponse `json:"actions"`
	LiveStatus         string                   `json:"live_status"`
	ProviderConfigured bool                     `json:"provider_configured"`
}
type CommerceOrderResponse struct {
	CommerceTarget
	ExternalOrderID string     `json:"external_order_id"`
	Status          string     `json:"status"`
	SourceVersion   string     `json:"source_version"`
	MirrorFetchedAt time.Time  `json:"mirror_fetched_at"`
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`
	LiveFetchedAt   *time.Time `json:"live_fetched_at,omitempty"`
	Currency        string     `json:"currency,omitempty"`
	RefundableMinor int64      `json:"refundable_minor"`
	MaskedFields    []string   `json:"masked_fields"`
	Source          string     `json:"source"`
	ReadRestriction string     `json:"read_restriction,omitempty"`
}
type CommerceOperationResponse struct {
	ID string `json:"id"`
	CommerceTarget
	Action         string     `json:"action"`
	IntentID       string     `json:"intent_id"`
	Revision       int64      `json:"revision"`
	Version        int64      `json:"version"`
	State          string     `json:"state"`
	NativeState    string     `json:"native_state"`
	Mode           string     `json:"mode"`
	WriteOwner     string     `json:"write_owner"`
	ParameterHash  string     `json:"parameter_hash"`
	PreflightHash  string     `json:"preflight_hash"`
	MissingFields  []string   `json:"missing_fields"`
	Obligations    []string   `json:"obligations"`
	AmountMinor    int64      `json:"amount_minor"`
	Currency       string     `json:"currency"`
	Quantity       int64      `json:"quantity"`
	ExpiresAt      time.Time  `json:"expires_at"`
	DueAt          time.Time  `json:"due_at"`
	UnknownSince   *time.Time `json:"unknown_since,omitempty"`
	OwnerID        uint       `json:"owner_id"`
	ApproverID     uint       `json:"approver_id"`
	EvidenceLevel  string     `json:"evidence_level"`
	EvidenceRef    string     `json:"evidence_ref"`
	ExternalRef    string     `json:"external_ref"`
	FundsState     string     `json:"funds_state"`
	ManualState    string     `json:"manual_state"`
	FailureReason  string     `json:"failure_reason"`
	ActualAttempts int64      `json:"actual_attempts"`
	ResultSummary  string     `json:"result_summary"`
	LiveStatus     string     `json:"live_status"`
	AfterSaleID    uint       `json:"after_sale_id,omitempty"`
	OrderChangeID  uint       `json:"order_change_id,omitempty"`
}
type CommerceOperationListResponse struct {
	List  []CommerceOperationResponse `json:"list"`
	Total int64                       `json:"total"`
}
