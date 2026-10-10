package model

import "time"

// MCPCredentialAuditLog MCP/Skill 管理面的操作审计（2026-10-11 新增）。
//
// 为什么不用 operation_logs 兜底：全局 AuditMiddleware 只拦 POST/PUT/DELETE/PATCH 且要求
// JWT user_id —— ① key.reveal 是 GET（敏感读，漏网）；② guard 层的鉴权失败不是管理操作；
// ③ mcp_call_audits 只记"调用"，记不了"谁在什么时候把哪把钥匙禁用了"。
// 本表把 MCP/Skill 域的管理动作（凭证/钥匙/Skill 的增删改、状态流转、明文查看）
// 收敛成一张可分页、可按对象过滤的专用账本，与 mcp_call_audits（使用面）互补。
//
// Detail 是结构化 JSON 摘要（状态前后值、TTL、掩码形），**永不落明文 API Key** ——
// reveal 审计也只记"谁看了哪把钥匙"，不记看到的内容。
type MCPCredentialAuditLog struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Action 动作名（点分：对象.动作）：
	//   credential.create / credential.update / credential.delete
	//   key.create / key.enable / key.disable / key.revoke / key.delete / key.reveal
	//   skill.create / skill.update / skill.delete
	Action string `gorm:"type:varchar(32);not null;index" json:"action"`
	// TargetType 操作对象类型：credential | key | skill
	TargetType string `gorm:"column:target_type;type:varchar(16);not null;index" json:"target_type"`
	// TargetID 对象自身 id（key 记钥匙 id，credential 记凭证 id，skill 记 skill id）
	TargetID uint `gorm:"column:target_id;not null" json:"target_id"`
	// CredentialID 动作最终归属的凭证（mcp_credentials.id）：key/skill 操作也能对回凭证
	CredentialID *uint `gorm:"column:credential_id;index" json:"credential_id"`
	// TargetLabel 人类可读标签（client_id / 钥匙名 / skill 名）；client_id 可改，
	// 对账以 CredentialID+TargetID 为准，label 只是省一次 join
	TargetLabel string `gorm:"column:target_label;type:varchar(128)" json:"target_label"`
	// Detail JSON 摘要（status 前后值 / ttl / 掩码形 / 变更字段名），固定字段拼装，无明文钥匙
	Detail string `gorm:"type:text" json:"detail"`
	// OperatorUserID 操作人（JWT user_id；system 管理员路径可能缺失 → NULL）
	OperatorUserID   *uint     `gorm:"column:operator_user_id;index" json:"operator_user_id"`
	OperatorUsername string    `gorm:"column:operator_username;type:varchar(64)" json:"operator_username"`
	IP               string    `gorm:"type:varchar(64)" json:"ip"`
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}

func (MCPCredentialAuditLog) TableName() string { return "mcp_credential_audit_logs" }

// 管理/使用审计的动作字面量（controller 写入与前端筛选共用一套口径）。
const (
	MCPAuditActionCredentialCreate = "credential.create"
	MCPAuditActionCredentialUpdate = "credential.update"
	MCPAuditActionCredentialDelete = "credential.delete"
	MCPAuditActionKeyCreate        = "key.create"
	MCPAuditActionKeyEnable        = "key.enable"
	MCPAuditActionKeyDisable       = "key.disable"
	MCPAuditActionKeySuspend       = "key.suspend"
	MCPAuditActionKeyRevoke        = "key.revoke"
	MCPAuditActionKeyDelete        = "key.delete"
	MCPAuditActionKeyReveal        = "key.reveal"
	MCPAuditActionSkillCreate      = "skill.create"
	MCPAuditActionSkillUpdate      = "skill.update"
	MCPAuditActionSkillDelete      = "skill.delete"
)
