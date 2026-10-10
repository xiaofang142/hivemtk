package model

import (
	"time"
)

// ScriptTemplate 话术模板
type ScriptTemplate struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CategoryID         uint      `json:"category_id"`
	Category           string    `gorm:"type:varchar(50);index" json:"category"`
	Name               string    `gorm:"type:varchar(100);not null" json:"name"`
	Title              string    `gorm:"type:varchar(100);not null" json:"title"`
	Content            string    `gorm:"type:text;not null" json:"content"`
	Variables          string    `gorm:"type:text" json:"variables"`
	Tags               string    `gorm:"type:varchar(200)" json:"tags"`
	UsageCount         int       `gorm:"default:0" json:"usage_count"`
	Rating             float64   `gorm:"type:decimal(3,2);default:0" json:"rating"`
	IsPublic           bool      `gorm:"default:false" json:"is_public"`
	IsSystem           bool      `gorm:"default:false" json:"is_system"`
	Source             string    `gorm:"type:varchar(20);default:'manual'" json:"source"`
	EffectivenessScore float64   `gorm:"type:decimal(3,2);default:0" json:"effectiveness_score"`
	TriggerKeywords    string    `gorm:"type:varchar(500)" json:"trigger_keywords"`
	JourneyStage       string    `gorm:"type:varchar(30)" json:"journey_stage"`
	ChampionDialogueID uint      `json:"champion_dialogue_id"`
	CreatedBy          uint      `json:"created_by"`
	CreatedAt          time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ScriptTemplate) TableName() string {
	return "script_templates"
}

// ScriptCategory 话术分类
type ScriptCategory struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"type:varchar(50);not null" json:"name"`
	ParentID  uint      `json:"parent_id"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (ScriptCategory) TableName() string {
	return "script_categories"
}

// ScriptRecommend 话术推荐记录
type ScriptRecommend struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID     string     `gorm:"type:varchar(120);index" json:"session_id"`
	Message       string     `gorm:"type:text" json:"message"`
	TemplateID    uint       `json:"template_id"`
	TemplateTitle string     `gorm:"type:varchar(100)" json:"template_title"`
	Confidence    float64    `gorm:"type:decimal(5,2)" json:"confidence"`
	IsUsed        bool       `gorm:"default:false" json:"is_used"`
	UsedAt        *time.Time `json:"used_at"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (ScriptRecommend) TableName() string {
	return "script_recommendations"
}
