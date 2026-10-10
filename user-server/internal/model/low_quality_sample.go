package model

import (
	"time"
)

// LowQualitySampleType 低质样本类型
type LowQualitySampleType string

type LowQualitySample struct {
	ID               uint64               `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID       string               `gorm:"type:varchar(64);index" json:"customer_id"`
	SessionID string `gorm:"type:varchar(120);index" json:"session_id"`
	SampleType       LowQualitySampleType `gorm:"type:varchar(32);not null;index" json:"sample_type"`
	CustomerMessage  string               `gorm:"type:text" json:"customer_message"`
	AIReply          string               `gorm:"type:text;not null" json:"ai_reply"`
	Persona          string               `gorm:"type:varchar(128)" json:"persona"`
	Industry         string               `gorm:"type:varchar(64)" json:"industry"`
	Platform         string               `gorm:"type:varchar(32)" json:"platform"`
	Intent           string               `gorm:"type:varchar(32)" json:"intent"`
	DimensionScores  string               `gorm:"type:jsonb;default:'{}'" json:"dimension_scores"`
	TotalScore       float64              `gorm:"default:0" json:"total_score"`
	Threshold        float64              `gorm:"default:0.85" json:"threshold"`
	AttemptCount     int                  `gorm:"default:1" json:"attempt_count"`
	CandidateReplies string               `gorm:"type:jsonb;default:'[]'" json:"candidate_replies"`
	Handled          bool                 `gorm:"default:false;index" json:"handled"`
	HandledBy        string               `gorm:"type:varchar(64)" json:"handled_by"`
	HandledAt        *time.Time           `gorm:"index" json:"handled_at"`
	HandledNote      string               `gorm:"type:text" json:"handled_note"`
	CreatedAt        time.Time            `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (LowQualitySample) TableName() string { return "low_quality_samples" }
