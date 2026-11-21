package model

import "time"

// CapabilitySourceBinding ties a server-verified account or order to one connection and shop.
type CapabilitySourceBinding struct {
	ID             uint   `gorm:"primaryKey"`
	TenantID       uint   `gorm:"not null;uniqueIndex:uq_cap_source,priority:1"`
	Source         string `gorm:"not null;size:32;uniqueIndex:uq_cap_source,priority:2"`
	Product        string `gorm:"not null;size:64;uniqueIndex:uq_cap_source,priority:3"`
	ResourceID     string `gorm:"not null;size:160;uniqueIndex:uq_cap_source,priority:4"`
	ShopID         string `gorm:"not null;size:160"`
	Region         string `gorm:"not null;size:32"`
	ConnectionID   uint   `gorm:"not null"`
	ConsentVersion int64  `gorm:"not null"`
	Revoked        bool   `gorm:"not null;default:false"`
}

func (CapabilitySourceBinding) TableName() string { return "capability_source_bindings" }

// CapabilityEvidence stores the current contract and a reference to independently verified evidence.
// A reference is not itself proof of a real runtime observation or a resource preflight.
type CapabilityEvidence struct {
	ID                   uint      `gorm:"primaryKey"`
	TenantID             uint      `gorm:"not null;uniqueIndex:uq_cap_evidence,priority:1"`
	ShopID               string    `gorm:"not null;size:160;uniqueIndex:uq_cap_evidence,priority:2"`
	Product              string    `gorm:"not null;size:64;uniqueIndex:uq_cap_evidence,priority:3"`
	Region               string    `gorm:"not null;size:32;uniqueIndex:uq_cap_evidence,priority:4"`
	Action               string    `gorm:"not null;size:96;uniqueIndex:uq_cap_evidence,priority:5"`
	Carrier              string    `gorm:"not null;size:32;uniqueIndex:uq_cap_evidence,priority:6"`
	ConnectionID         uint      `gorm:"not null"`
	ConnectionVersion    int64     `gorm:"not null"`
	CredentialVersion    int64     `gorm:"not null"`
	ConsentVersion       int64     `gorm:"not null"`
	ContractVersion      string    `gorm:"not null;size:64"`
	DocumentationVersion string    `gorm:"not null;size:64"`
	Scopes               []string  `gorm:"serializer:json;type:text"`
	StateLimits          []string  `gorm:"serializer:json;type:text"`
	ExpiresAt            time.Time `gorm:"not null"`
	Documentation        bool      `gorm:"not null;default:false"`
	Implementation       bool      `gorm:"not null;default:false"`
	Provenance           string    `gorm:"not null;size:64"`
	RuntimeProofRef      string    `gorm:"not null;size:160"`
	Simulated            bool      `gorm:"not null;default:true"`
}

func (CapabilityEvidence) TableName() string { return "capability_evidences" }
