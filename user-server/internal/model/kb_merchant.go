package model

import (
	"time"
)

// KnowledgeAPIToken 商户自部署场景：外部系统通过 API Token 推送文档
// 用于把 RAG 知识库开放给商户自有 CRM/ERP/Helpdesk
type KnowledgeAPIToken struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name       string     `gorm:"size:64;not null" json:"name"`
	Token      string     `gorm:"size:128;uniqueIndex;not null" json:"-"`
	TokenPlain string     `gorm:"-" json:"token_plain,omitempty"`
	Scopes     string     `gorm:"type:jsonb;default:'[\"read\",\"write\"]'" json:"scopes"`
	ProductID  string     `gorm:"size:64;index;not null;default:''" json:"product_id"`
	Enabled    int        `gorm:"default:1" json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	UseCount   int64      `gorm:"default:0" json:"use_count"`
	CreatedBy  string     `gorm:"size:64" json:"created_by"`
	CreatedAt  time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (KnowledgeAPIToken) TableName() string {
	return "knowledge_api_tokens"
}

// KnowledgeFeedback 用户对检索结果的相关性反馈（用于持续学习）
type KnowledgeFeedback struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID  string    `gorm:"size:64;index;not null;default:''" json:"product_id"`
	Query      string    `gorm:"type:text;not null" json:"query"`
	QueryHash  string    `gorm:"size:64;index" json:"query_hash"`
	DocumentID *uint64   `gorm:"index" json:"document_id"`
	ChunkID    *uint64   `gorm:"index" json:"chunk_id"`
	Rating     int       `gorm:"default:0" json:"rating"`
	Comment    string    `gorm:"type:text" json:"comment"`
	Operator   string    `gorm:"size:64" json:"operator"`
	SessionID string `gorm:"size:120" json:"session_id"`
	CreatedAt  time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (KnowledgeFeedback) TableName() string {
	return "knowledge_feedbacks"
}

// ExternalImportJob 外部系统导入任务（飞书/Notion/钉钉 异步批量）
type ExternalImportJob struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	JobNo       string     `gorm:"size:32;uniqueIndex" json:"job_no"`
	ProductID   string     `gorm:"size:64;index;not null" json:"product_id"`
	Source      string     `gorm:"size:32;not null" json:"source"`
	TotalItems  int        `gorm:"default:0" json:"total_items"`
	DoneItems   int        `gorm:"default:0" json:"done_items"`
	FailedItems int        `gorm:"default:0" json:"failed_items"`
	Status      string     `gorm:"size:16;default:'pending';index" json:"status"`
	Payload     string     `gorm:"type:jsonb" json:"payload,omitempty"`
	ErrorDetail string     `gorm:"type:text" json:"error_detail"`
	Operator    string     `gorm:"size:64" json:"operator"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (ExternalImportJob) TableName() string {
	return "external_import_jobs"
}
