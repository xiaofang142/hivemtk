package model

import "time"

// CommerceOperation is the durable execution ledger; external_orders remains the order mirror.
type CommerceOperation struct {
	ID                string `gorm:"primaryKey;size:36"`
	TenantID          uint   `gorm:"not null;uniqueIndex:uq_commerce_intent,priority:1;index:idx_commerce_queue,priority:1"`
	ShopID            string `gorm:"not null;size:160;uniqueIndex:uq_commerce_intent,priority:2"`
	Profile           string `gorm:"not null;size:64;uniqueIndex:uq_commerce_intent,priority:3"`
	Product           string `gorm:"not null;size:64"`
	Action            string `gorm:"not null;size:64;uniqueIndex:uq_commerce_intent,priority:4"`
	IntentID          string `gorm:"not null;size:120;uniqueIndex:uq_commerce_intent,priority:5"`
	Revision          int64  `gorm:"not null;uniqueIndex:uq_commerce_intent,priority:6"`
	OrderID           uint   `gorm:"not null;index"`
	ExternalOrderID   string `gorm:"not null;size:160"`
	AfterSaleID       uint
	OrderChangeID     uint
	ParentID          string `gorm:"size:36"`
	ParameterHash     string `gorm:"not null;size:64"`
	Parameters        string `gorm:"type:text;not null"`
	Snapshot          string `gorm:"type:text;not null"`
	PreflightHash     string `gorm:"size:64"`
	State             string `gorm:"size:32;not null;index:idx_commerce_queue,priority:2"`
	NativeState       string `gorm:"size:100"`
	Mode              string `gorm:"size:32;not null"`
	WriteOwner        string `gorm:"size:80;not null"`
	ActorID           uint
	OwnerID           uint
	ApprovalID        string `gorm:"size:36"`
	ConnectionID      uint
	ConnectionVersion int64
	CredentialVersion int64
	PolicyVersion     int64
	MemberVersion     int64
	RecoveryEpoch     int64
	Version           int64  `gorm:"not null;default:1"`
	LeaseOwner        string `gorm:"size:36"`
	LeaseUntil        *time.Time
	Fence             int64
	DispatchedAt      *time.Time
	DueAt             time.Time `gorm:"index:idx_commerce_queue,priority:3"`
	ExpiresAt         time.Time
	UnknownSince      *time.Time
	ExternalRef       string `gorm:"size:160"`
	EvidenceLevel     string `gorm:"size:32"`
	EvidenceRef       string `gorm:"size:160"`
	ManualState       string `gorm:"size:32"`
	FundsState        string `gorm:"size:32"`
	FailureReason     string `gorm:"size:64"`
	AmountMinor       int64  `gorm:"not null;default:0;check:commerce_amount_nonnegative,amount_minor >= 0"`
	Currency          string `gorm:"size:3"`
	Quantity          int64  `gorm:"not null;default:0;check:commerce_quantity_nonnegative,quantity >= 0"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (CommerceOperation) TableName() string { return "commerce_operations" }

type CommerceApproval struct {
	ID            string `gorm:"primaryKey;size:36"`
	TenantID      uint   `gorm:"not null;index"`
	OperationID   string `gorm:"not null;size:36;index"`
	ActorID       uint
	Digest        string `gorm:"not null;size:64"`
	PolicyVersion int64
	MemberVersion int64
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

func (CommerceApproval) TableName() string { return "commerce_approvals" }

type CommerceAttempt struct {
	ID                string `gorm:"primaryKey;size:36"`
	TenantID          uint   `gorm:"not null;index"`
	OperationID       string `gorm:"not null;size:36;index"`
	Kind              string `gorm:"size:20"`
	Fence             int64
	DispatchCommitted bool
	State             string `gorm:"size:32"`
	ExternalRef       string `gorm:"size:160"`
	Reason            string `gorm:"size:64"`
	CreatedAt         time.Time
	FinishedAt        *time.Time
}

func (CommerceAttempt) TableName() string { return "commerce_attempts" }

// CommerceResource retains an order guard independently of short worker/network leases.
type CommerceResource struct {
	ID              string `gorm:"primaryKey;size:64"`
	TenantID        uint   `gorm:"not null;index"`
	ShopID          string `gorm:"size:160;not null"`
	Profile         string `gorm:"size:64;not null"`
	OrderID         uint   `gorm:"not null"`
	ActiveOperation string `gorm:"size:36"`
	Revision        int64
	AmountMinor     int64  `gorm:"not null;default:0;check:commerce_reserved_nonnegative,amount_minor >= 0"`
	Currency        string `gorm:"size:3"`
	Quantity        int64  `gorm:"not null;default:0"`
	UpdatedAt       time.Time
}

func (CommerceResource) TableName() string { return "commerce_resources" }

// CommerceOutbox distinguishes business events and commerce commands by schema and kind.
type CommerceOutbox struct {
	ID            string    `gorm:"primaryKey;size:36"`
	TenantID      uint      `gorm:"not null;index:idx_commerce_outbox,priority:1"`
	OperationID   string    `gorm:"not null;size:36;uniqueIndex:uq_commerce_event,priority:1"`
	Version       int64     `gorm:"not null;uniqueIndex:uq_commerce_event,priority:2"`
	Kind          string    `gorm:"size:32;not null;uniqueIndex:uq_commerce_event,priority:3"`
	SchemaVersion int       `gorm:"not null;default:1"`
	TraceID       string    `gorm:"size:100"`
	State         string    `gorm:"size:20;index:idx_commerce_outbox,priority:2"`
	DueAt         time.Time `gorm:"index:idx_commerce_outbox,priority:3"`
	CreatedAt     time.Time
}

func (CommerceOutbox) TableName() string { return "commerce_outbox" }

// CommercePolicy is set by a current tenant administrator; empty configuration grants nothing.
type CommercePolicy struct {
	ID            string `gorm:"primaryKey;size:64"`
	TenantID      uint   `gorm:"not null;uniqueIndex:uq_commerce_policy,priority:1"`
	ShopID        string `gorm:"not null;size:160;uniqueIndex:uq_commerce_policy,priority:2"`
	Profile       string `gorm:"not null;size:64;uniqueIndex:uq_commerce_policy,priority:3"`
	Version       int64
	WriteEnabled  bool
	WriteOwner    string `gorm:"size:80"`
	ApprovalMode  string `gorm:"size:32"`
	OwnerID       uint
	RecoveryEpoch int64
	NetworkLease  string `gorm:"size:36"`
	NetworkUntil  *time.Time
	UpdatedAt     time.Time
}

func (CommercePolicy) TableName() string { return "commerce_policies" }

type CommerceMember struct {
	ID         string `gorm:"primaryKey;size:64"`
	TenantID   uint   `gorm:"not null;uniqueIndex:uq_commerce_member,priority:1"`
	ShopID     string `gorm:"not null;size:160;uniqueIndex:uq_commerce_member,priority:2"`
	Profile    string `gorm:"not null;size:64;uniqueIndex:uq_commerce_member,priority:3"`
	ActorID    uint   `gorm:"not null;uniqueIndex:uq_commerce_member,priority:4"`
	Version    int64
	CanRead    bool
	CanExecute bool
	CanApprove bool
	CanReview  bool
	LimitMinor int64  `gorm:"not null;default:0;check:commerce_limit_nonnegative,limit_minor >= 0"`
	Currency   string `gorm:"size:3"`
	UpdatedAt  time.Time
}

func (CommerceMember) TableName() string { return "commerce_members" }

type CommerceEvidence struct {
	ID          string `gorm:"primaryKey;size:36"`
	TenantID    uint   `gorm:"not null;index"`
	OperationID string `gorm:"not null;size:36;index"`
	ActorID     uint
	ReviewerID  uint
	Source      string `gorm:"size:32"`
	Reference   string `gorm:"size:160"`
	NativeState string `gorm:"size:100"`
	FundsState  string `gorm:"size:32"`
	Outcome     string `gorm:"size:32"`
	CreatedAt   time.Time
	ReviewedAt  *time.Time
}

func (CommerceEvidence) TableName() string { return "commerce_evidences" }
