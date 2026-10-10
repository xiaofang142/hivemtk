package model

import "time"

// MCPCredential MCP 工具入口的 Client ID + API Key 凭证对。
//
// 与单值 MCP_TOKEN 并存：带 X-Client-Id 头的请求走凭证对校验（本表），
// 不带头的老调用方继续走单值链路——存量部署零破坏。
// API Key 只存 sha256：管理端签发时明文只出现一次。
type MCPCredential struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID    string    `gorm:"column:client_id;type:varchar(64);uniqueIndex" json:"client_id"`
	Name        string    `gorm:"type:varchar(128)" json:"name"`
	APIKeyHash  string    `gorm:"column:api_key_hash;type:char(64);not null" json:"-"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (MCPCredential) TableName() string { return "mcp_credentials" }
