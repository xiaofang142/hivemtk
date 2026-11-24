package model

import (
	"encoding/json"
	"time"
)

// AIInsightDraft AI 画像/分层/商机/建议草稿（PR-7/R-06）。默认草稿，需人工确认。
type AIInsightDraft struct {
	TenantID           uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID                 int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	DraftID            string     `gorm:"type:varchar(32);not null;index" json:"draft_id"`
	EntityType         string     `gorm:"type:varchar(32);not null;default:'customer';index" json:"entity_type"`
	EntityID           string     `gorm:"type:varchar(64);not null;default:'';index" json:"entity_id"`
	InsightType        string     `gorm:"type:varchar(32);not null;default:'profile';index" json:"insight_type"` // profile/segment/opportunity/suggestion
	Title              string     `gorm:"type:varchar(255);not null;default:''" json:"title"`
	ContentJSON        json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"content"`
	EvidenceJSON       json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"evidence"`
	SourceRefsJSON     json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"source_refs"`
	ProposedActionJSON json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"proposed_action"`
	AppliedActionJSON  json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"applied_action"`
	Confidence         float64    `gorm:"type:numeric(5,4);not null;default:0" json:"confidence"`
	Status             string     `gorm:"type:varchar(32);not null;default:'draft';index" json:"status"` // draft/confirmed/rejected/applied
	GeneratedBy        string     `gorm:"type:varchar(64);not null;default:''" json:"generated_by"`
	ModelName          string     `gorm:"type:varchar(64);not null;default:''" json:"model_name"`
	PromptVersion      string     `gorm:"type:varchar(64);not null;default:''" json:"prompt_version"`
	ReviewNote         string     `gorm:"type:text;not null;default:''" json:"review_note"`
	ConfirmedBy        *uint      `json:"confirmed_by,omitempty"`
	ConfirmedAt        *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (AIInsightDraft) TableName() string { return "ai_insight_drafts" }
