package model

import "time"

// ExternalKBConnector 第三方知识库连接器（通用框架）。
//
// 设计：类型注册制（type → provider），v1 内置 ima（腾讯 IMA 知识库）与
// webhttp（通用 HTTP 清单）。同步模型为「拉取落库」：把第三方文件下载后
// 走本地知识库既有导入管线（分块+向量化+pgvector），检索体验与本地文档完全一致。
//
// 局限说明：IMA OpenAPI 对「笔记类」内容不提供原文导出（仅文件/网页可取），
// 笔记会被跳过并记录在 last_error 摘要里。
type ExternalKBConnector struct {
	ID              uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Type            string     `gorm:"type:varchar(32);not null" json:"type"` // ima | webhttp
	Name            string     `gorm:"type:varchar(128)" json:"name"`
	Config          string     `gorm:"type:text" json:"config"`           // provider 各自的 JSON 配置（凭证等，脱敏返回）
	IntervalMinutes int        `gorm:"default:0" json:"interval_minutes"` // >0 时由调度器周期同步；0=仅手动
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	LastStatus      string     `gorm:"type:varchar(32)" json:"last_status"` // idle|running|success|failed
	LastSummary     string     `gorm:"type:varchar(512)" json:"last_summary"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (ExternalKBConnector) TableName() string { return "external_kb_connectors" }

// ExternalKBSyncItem 同步幂等记录：同一文档内容不重复入库。
type ExternalKBSyncItem struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ConnectorID uint      `gorm:"index:idx_connector_doc,unique;not null" json:"connector_id"`
	DocKey      string    `gorm:"column:doc_key;type:varchar(191);index:idx_connector_doc,unique;not null" json:"doc_key"`
	Title       string    `gorm:"type:varchar(255)" json:"title"`
	ContentHash string    `gorm:"type:char(64)" json:"content_hash"`
	Status      string    `gorm:"type:varchar(16)" json:"status"` // success | failed | skipped
	Detail      string    `gorm:"type:varchar(512)" json:"detail"`
	DocumentID  uint      `json:"document_id"`
	SyncedAt    time.Time `json:"synced_at"`
}

func (ExternalKBSyncItem) TableName() string { return "external_kb_sync_items" }
