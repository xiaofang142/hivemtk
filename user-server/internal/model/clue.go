package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Clue struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             string `gorm:"type:varchar(36);primary_key" json:"id"`
	SourceID       string `gorm:"source_id" json:"source_id"`
	Account        string `gorm:"account" json:"account"`
	Type           int64  `gorm:"column:type;index" json:"type"`
	IsVerify       int64  `gorm:"is_verify" json:"is_verify"`
	Name           string `gorm:"name" json:"name"`
	City           string `gorm:"city" json:"city"`
	Address        string `gorm:"address" json:"address"`
	Desc           string `gorm:"desc" json:"desc"`
	IntentScore    int64  `gorm:"column:intent_score;default:0" json:"intent_score"`
	IsOpportunity  int64  `gorm:"column:is_opportunity;default:0" json:"is_opportunity"`
	MessageID      string `gorm:"column:message_id;type:varchar(100)" json:"message_id"`
	ConversationID string `gorm:"column:conversation_id;type:varchar(100);index" json:"conversation_id"`
	OneID          string `gorm:"column:one_id;type:varchar(100);index" json:"one_id"`
	OwnerAccount   string `gorm:"column:owner_account;type:varchar(255);index" json:"owner_account"`
	IsGroup        bool   `gorm:"column:is_group;default:false" json:"is_group"`
	GroupID        string `gorm:"column:group_id;type:varchar(100)" json:"group_id"`
	GroupName      string `gorm:"column:group_name;type:varchar(255)" json:"group_name"`
	// Level 线索温度等级（P-9 动态化）：由 ClueScoreService 按 clue_score 写回
	// hot(>=70) / warm(40-69) / cold(<40)；空串表示尚未评分，读取侧兜底 warm。
	Level              string `gorm:"column:level;type:varchar(16);default:''" json:"level"`
	LeadRole           string `gorm:"column:lead_role;type:varchar(16);default:'unknown';index" json:"lead_role"` // demand/supply/resource/broker/unknown
	Status             string `gorm:"column:status;type:varchar(32);default:'new';index" json:"status"`
	EvidenceJSON       string `gorm:"column:evidence_json;type:jsonb;default:'[]'" json:"evidence_json,omitempty"`
	Summary            string `gorm:"column:summary;type:text" json:"summary,omitempty"`
	FollowupSuggestion string `gorm:"column:followup_suggestion;type:text" json:"followup_suggestion,omitempty"`
	LastJudgedAt       *int64 `gorm:"column:last_judged_at" json:"last_judged_at,omitempty"`
	SourceChannel      string `gorm:"column:source_channel;type:varchar(32);default:''" json:"source_channel,omitempty"`
	SourceAccountID    string `gorm:"column:source_account_id;type:varchar(128);default:''" json:"source_account_id,omitempty"`
	ConversationType   string `gorm:"column:conversation_type;type:varchar(16);default:''" json:"conversation_type,omitempty"`       // private/group
	FeedbackStatus     string `gorm:"column:feedback_status;type:varchar(16);default:'none';index" json:"feedback_status,omitempty"` // none/false_positive/valid
	FeedbackReason     string `gorm:"column:feedback_reason;type:text;default:''" json:"feedback_reason,omitempty"`
	FeedbackBy         string `gorm:"column:feedback_by;type:varchar(64);default:''" json:"feedback_by,omitempty"`
	FeedbackAt         *int64 `gorm:"column:feedback_at" json:"feedback_at,omitempty"`
	// SourceEvidence 固定来源事件和许可证据引用；公开账号或自述 opt-in 不是营销许可。
	SourceEvidence string `gorm:"type:text" json:"source_evidence"`
	// T033 US2 归档扩展：客户关联、线索用户画像、三路证据引用、归档时间。
	// Role 语义复用现有 LeadRole（demand/supply/resource/broker/unknown），不另建列。
	CustomerID   string         `gorm:"column:customer_id;type:varchar(64);default:'';index" json:"customer_id,omitempty"`
	UserInfo     string         `gorm:"column:user_info;type:jsonb;default:'{}'" json:"user_info,omitempty"`
	SourceHitIDs JSONStrings    `gorm:"column:source_hit_ids;type:jsonb;default:'[]'" json:"source_hit_ids,omitempty"`
	ArchivedAt   *int64         `gorm:"column:archived_at" json:"archived_at,omitempty"`
	CreateTime   int64          `gorm:"autoCreateTime" json:"create_time"`
	UpdatedAt    int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *Clue) TableName() string {
	return "clues"
}

func (m *Clue) BeforeCreate(tx *gorm.DB) error {
	m.ID = uuid.New().String()
	return nil
}
