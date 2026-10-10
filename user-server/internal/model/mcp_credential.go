package model

import "time"

// MCPCredential MCP 工具入口的 Client ID + API Key 凭证对。
//
// 与单值 MCP_TOKEN 并存：带 X-Client-Id 头的请求走凭证对校验（本表），
// 不带头的老调用方继续走单值链路——存量部署零破坏。
// API Key 只存 sha256：管理端签发时明文只出现一次。
type MCPCredential struct {
	ID         uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID   string `gorm:"column:client_id;type:varchar(64);uniqueIndex" json:"client_id"`
	Name       string `gorm:"type:varchar(128)" json:"name"`
	APIKeyHash string `gorm:"column:api_key_hash;type:char(64);not null" json:"-"`
	Enabled    bool   `gorm:"default:true" json:"enabled"`
	// OwnerUserID 凭证绑定的系统账户（system_users.id，NULL=未绑定）。
	//
	// 未绑定 = 老行为（谁拿到这对密钥都能用）；绑定后该凭证的"身份"归账户所有，
	// 角色/权限/功能/业务/付费语义从账户侧继承（RBAC P0 起 team_id 从账户侧注入）。
	// 删除绑定账户时随账户一起失效的前提是 Migrate 保留本列（不建外键，防循环迁移）。
	OwnerUserID *uint `gorm:"column:owner_user_id;index" json:"owner_user_id"`
	// ExpiresAt 凭证过期时间（NULL=永不过期，兼容存量行）。
	//
	// 签发默认给（默认有效期天数见 service 层签发常量），也可在签发时手动指定；
	// 到期后 MCPGuard 直接 401，不进入密钥比对（过期即死的语义最省事，
	// 免得"密钥对但已过期"和"密钥错"混在一条日志里）。
	ExpiresAt *time.Time `gorm:"column:expires_at;index" json:"expires_at"`
	// AllowedTools 该凭证可调用的工具白名单（逗号分隔）。
	// 空串 = 不限制（存量凭证行为不变：这一列是 2026-10-10 新增的可空列，
	// AutoMigrate 只加不删，老行留空即"未曾设限"）。
	// 非空时由 MCPGuard 写入请求上下文，MCP 入口按它过滤 tools/list 并拒绝越权 tools/call。
	AllowedTools string     `gorm:"column:allowed_tools;type:text" json:"allowed_tools"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (MCPCredential) TableName() string { return "mcp_credentials" }

// CredentialExpired 报告该凭证是否已过期（nil ExpiresAt 永不视为过期）。
func (c *MCPCredential) CredentialExpired(now time.Time) bool {
	return c.ExpiresAt != nil && now.After(*c.ExpiresAt)
}
