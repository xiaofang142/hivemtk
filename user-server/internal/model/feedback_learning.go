package model

import (
	"gorm.io/gorm"
	"time"
)

// SalesChampionDimension 销冠能力维度标识
type SalesChampionDimension string

const (
	DimensionObjectionHandling   SalesChampionDimension = "objection_handling"
	DimensionClosingInvitation   SalesChampionDimension = "closing_invitation"
	DimensionFollowupActivation  SalesChampionDimension = "followup_activation"
	DimensionNurturingConversion SalesChampionDimension = "nurturing_conversion"
	DimensionRepurchaseOperation SalesChampionDimension = "repurchase_operation"
)

// AllSalesChampionDimensions 全部 5 维度
var AllSalesChampionDimensions = []SalesChampionDimension{
	DimensionObjectionHandling,
	DimensionClosingInvitation,
	DimensionFollowupActivation,
	DimensionNurturingConversion,
	DimensionRepurchaseOperation,
}

// SalesChampionProfileSnapshot 销冠画像快照
// 每次从对话记录提取能力维度后，生成一份快照持久化
type SalesChampionProfileSnapshot struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	StaffID       uint      `gorm:"index" json:"staff_id"`
	StaffName     string    `gorm:"type:varchar(100)" json:"staff_name"`
	Scenario      string    `gorm:"type:varchar(50);index" json:"scenario"`
	Dimension     string    `gorm:"type:varchar(50);not null;index" json:"dimension"`
	Score         float64   `gorm:"type:decimal(5,2);not null" json:"score"`
	SampleCount   int       `gorm:"default:0" json:"sample_count"`
	PositiveCount int       `gorm:"default:0" json:"positive_count"`
	NegativeCount int       `gorm:"default:0" json:"negative_count"`
	EvidenceTags  JSONArray `gorm:"type:text" json:"evidence_tags"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
	GeneratedAt   time.Time `gorm:"autoCreateTime" json:"generated_at"`
}

// TableName 表名
func (SalesChampionProfileSnapshot) TableName() string { return "sales_champion_profile_snapshots" }

// SOPNodeTransition SOP 节点流转记录
// 记录每个 SOP 执行中节点之间的流转情况，用于计算节点级转化率
type SOPNodeTransition struct {
	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SOPID           uint           `gorm:"index;not null" json:"sop_id"`
	ExecutionID     uint           `gorm:"index;not null" json:"execution_id"`
	CustomerID      string         `gorm:"type:varchar(64);index" json:"customer_id"`
	SessionID       string         `gorm:"type:varchar(120)" json:"session_id"`
	Variant         string         `gorm:"type:varchar(50);index" json:"variant"`
	FromNode        string         `gorm:"type:varchar(50);index" json:"from_node"`
	ToNode          string         `gorm:"type:varchar(50);index" json:"to_node"`
	NodeType        string         `gorm:"type:varchar(30)" json:"node_type"`
	Outcome         string         `gorm:"type:varchar(30);index" json:"outcome"`
	DurationMs      int            `gorm:"default:0" json:"duration_ms"`
	MessageSnippets JSONArray      `gorm:"type:text" json:"message_snippets"`
	CreatedAt       time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (SOPNodeTransition) TableName() string { return "sop_node_transitions" }

// NodeOutcome 节点结果常量
const (
	NodeOutcomeSuccess   = "success"
	NodeOutcomeAbandoned = "abandoned"
	NodeOutcomeFailed    = "failed"
	NodeOutcomePending   = "pending"
)

// OptimizationSuggestion 优化建议
// 对低转化节点自动生成的优化建议，供运营/产品审核
type OptimizationSuggestion struct {
	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SOPID          uint           `gorm:"index;not null" json:"sop_id"`
	SOPName        string         `gorm:"type:varchar(100)" json:"sop_name"`
	NodeID         string         `gorm:"type:varchar(50);index" json:"node_id"`
	NodeName       string         `gorm:"type:varchar(100)" json:"node_name"`
	NodeType       string         `gorm:"type:varchar(30)" json:"node_type"`
	CurrentScore   float64        `gorm:"type:decimal(5,2)" json:"current_score"`
	Threshold      float64        `gorm:"type:decimal(5,2)" json:"threshold"`
	SuggestionType string         `gorm:"type:varchar(50);index" json:"suggestion_type"`
	SuggestionText string         `gorm:"type:text;not null" json:"suggestion_text"`
	ExpectedImpact string         `gorm:"type:varchar(200)" json:"expected_impact"`
	Priority       int            `gorm:"default:0" json:"priority"`
	Status         string         `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	SampleCount    int            `gorm:"default:0" json:"sample_count"`
	EvidenceData   JSONMap        `gorm:"type:text" json:"evidence_data"`
	GeneratedAt    time.Time      `gorm:"autoCreateTime" json:"generated_at"`
	ReviewedAt     *time.Time     `json:"reviewed_at"`
	ReviewedBy     uint           `json:"reviewed_by"`
	AppliedAt      *time.Time     `json:"applied_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (OptimizationSuggestion) TableName() string { return "optimization_suggestions" }

// SuggestionStatus 建议状态常量
const (
	SuggestionStatusPending  = "pending"
	SuggestionStatusApproved = "approved"
	SuggestionStatusApplied  = "applied"
	SuggestionStatusRejected = "rejected"
)

// SuggestionType 建议类型常量
const (
	SuggestionTypePromptRewrite = "prompt_rewrite"
	SuggestionTypeBranchPrune   = "branch_prune"
	SuggestionTypeNodeMerge     = "node_merge"
	SuggestionTypeAddObjection  = "add_objection"
	SuggestionTypeAddEmpathy    = "add_empathy"
	SuggestionTypeTimingAdjust  = "timing_adjust"
)
