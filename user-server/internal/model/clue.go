package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Clue struct {
	ID string `gorm:"type:varchar(36);primary_key" json:"id"`
	// TeamID 归属团队（多租户 P0）：线索按团队隔离，见 Customer.TeamID。
	TeamID         uint   `gorm:"column:team_id;index;default:0" json:"team_id"`
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
	Level      string         `gorm:"column:level;type:varchar(16);default:''" json:"level"`
	CreateTime int64          `gorm:"autoCreateTime" json:"create_time"`
	UpdatedAt  int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *Clue) TableName() string {
	return "clues"
}

func (m *Clue) BeforeCreate(tx *gorm.DB) error {
	m.ID = uuid.New().String()
	return nil
}
