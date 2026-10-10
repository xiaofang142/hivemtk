package model

import "time"

// MCPCredentialKey MCP 凭证（Client ID）名下的单把 API Key（2026-10-11 新增）。
//
// 为什么独立成表：mcp_credentials 原本是"一行=一对 (client_id, api_key_hash)"，
// 换钥匙只能整行重建（client_id 也跟着换，外部调用方全部要改配置）。
// 拆出本表后 Client ID 与 API Key 解耦为 1:N：同一身份可同时持有多把钥匙
// （灰度轮换 / 多机分发 / 按用途命名），管理端能对每把钥匙单独 启用/禁用/停用/删除。
//
// 密文双轨：
//   - KeyHash（sha256 明文 hex）：请求时校验与查找的唯一依据，恒时比较；
//   - KeyEnc（dbencrypt AES-256-GCM，enc:v1: 格式）：管理端"显示/复制"的依据。
//     与 LLM provider APIKey 同密钥同格式（MASTER_KEY）；MASTER_KEY 缺失的开发环境
//     降级明文存储（dbencrypt 永不失败语义），校验强度不受影响——查找走的是 hash。
//     旧凭证迁移行（从 mcp_credentials.api_key_hash 回填）没有明文，KeyEnc 为空，
//     管理端不可 reveal，只能"新建密钥 + 删除旧密钥"完成轮换。
type MCPCredentialKey struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// CredentialID 所属凭证（mcp_credentials.id）。凭证删除时随级联删除。
	CredentialID uint `gorm:"column:credential_id;not null;index" json:"credential_id"`
	// Name 钥匙别名（如 "jenkins-bot"、"轮换-2026Q4"），管理端识别用途
	Name string `gorm:"type:varchar(128)" json:"name"`
	// KeyHash sha256(明文) hex，全表唯一（同一把明文钥匙不可能对应两行）。
	KeyHash string `gorm:"column:key_hash;type:char(64);not null;uniqueIndex" json:"-"`
	// KeyEnc 明文密文（enc:v1: 或降级明文）；为空 = 明文从未落库（迁移行），不可 reveal
	KeyEnc string `gorm:"column:key_enc;type:text" json:"-"`
	// KeyPrefix / KeySuffix 明文首 12 位（"wgk_"+8hex）与末 4 位，列表掩码展示用。
	// 迁移行没有明文，两者为空串，前端展示为「旧密钥」。
	KeyPrefix string `gorm:"column:key_prefix;type:varchar(16)" json:"key_prefix"`
	KeySuffix string `gorm:"column:key_suffix;type:varchar(8)" json:"key_suffix"`
	// Status 生命周期：active（启用，可认证）/ disabled（禁用，可再启用）/
	// revoked（停用/吊销，终态不可恢复，仅可删除）。expired 不是存储状态——
	// 由 ExpiresAt 推导，guard 判过期优先于状态。
	Status string `gorm:"column:status;type:varchar(16);not null;default:active;index" json:"status"`
	// ExpiresAt 本钥匙独立过期时间（NULL=跟随凭证/永不过期）；签发时可指定
	ExpiresAt  *time.Time `gorm:"column:expires_at;index" json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (MCPCredentialKey) TableName() string { return "mcp_credential_keys" }

// Key 生命周期状态常量（guard / controller / 前端口径一致）。
const (
	MCPKeyStatusActive   = "active"
	MCPKeyStatusDisabled = "disabled"
	MCPKeyStatusRevoked  = "revoked"
)

// MCPKeyStatusValid 报告状态字面量是否合法（写入前防脏值）。
func MCPKeyStatusValid(s string) bool {
	return s == MCPKeyStatusActive || s == MCPKeyStatusDisabled || s == MCPKeyStatusRevoked
}

// KeyExpired 报告该钥匙是否已过独立过期时间（nil = 永不过期）。
func (k *MCPCredentialKey) KeyExpired(now time.Time) bool {
	return k.ExpiresAt != nil && now.After(*k.ExpiresAt)
}

// MaskedKey 生成掩码展示形（wgk_ab12cd34••••wxyz）；无明文痕迹的迁移行返回「旧密钥」占位。
func (k *MCPCredentialKey) MaskedKey() string {
	if k.KeyPrefix == "" && k.KeySuffix == "" {
		return "legacy-****"
	}
	return k.KeyPrefix + "••••" + k.KeySuffix
}
