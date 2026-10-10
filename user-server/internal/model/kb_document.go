package model

import (
	"time"
)

// KBDocumentStatus 知识库文档状态
type KBDocumentStatus string

const (
	KBDocumentStatusPending    KBDocumentStatus = "pending"
	KBDocumentStatusProcessing KBDocumentStatus = "processing"
	KBDocumentStatusIndexed    KBDocumentStatus = "indexed"
	KBDocumentStatusFailed     KBDocumentStatus = "failed"
)

// KBDocument 知识库文档模型
type KBDocument struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// TeamID 归属团队（多租户 P0）：文档按团队隔离，见 Customer.TeamID。
	TeamID     uint             `gorm:"column:team_id;index;default:0" json:"team_id"`
	Title      string           `gorm:"size:255;not null" json:"title"`
	Content    string           `gorm:"type:text" json:"content"`
	FilePath   string           `gorm:"size:500" json:"file_path"`
	FileSize   int64            `gorm:"default:0" json:"file_size"`
	FileType   string           `gorm:"size:20" json:"file_type"`
	Status     KBDocumentStatus `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	ErrorMsg   string           `gorm:"type:text" json:"error_msg"`
	ChunkCount int              `gorm:"default:0" json:"chunk_count"`
	CreatedAt  time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 返回表名
func (KBDocument) TableName() string {
	return "kb_documents"
}
