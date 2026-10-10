package model

import "time"

// MCPCallAudit MCP 工具入口的调用审计（2026-10-10 新增）。
//
// 为什么要有它：MCP 入口能直接触发服务端工具（发消息、写客户数据、跑触达管线），
// 但此前只有「签发了哪些凭证」，没有「谁、用哪把凭证、调了什么工具、成没成」的记录 ——
// 出事后无法回答"这条消息是谁发出去的"。本表只记**参数摘要（sha256 前 16 字节）**，
// 不落参数原文：MCP 参数里经常带手机号/邮箱/消息正文，原文入库等于把敏感数据复制一份。
type MCPCallAudit struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ClientID string `gorm:"column:client_id;type:varchar(64);index" json:"client_id"`
	// CredentialID 签发侧稳定键（mcp_credentials.id，可空=单值链路不落审计）。
	// client_id 可被人工修改，审计行要能对回"当初签发的那一行"，故额外带 id。
	CredentialID *uint `gorm:"column:credential_id;index" json:"credential_id"`
	// KeyID 命中的钥匙（mcp_credential_keys.id，可空=单值链路/迁移前老数据）。
	// 1:N 之后同一凭证可以有多把钥匙，"哪把钥匙干的"只有这里能回答。
	KeyID *uint `gorm:"column:key_id;index" json:"key_id"`
	// Event 事件类型：call（tools/call，默认）/ list（tools/list）/ auth（鉴权失败）。
	// 鉴权失败此前只在 HTTP 401 里一闪而过，出事无法回答"谁在什么时候拿错钥匙撞门"；
	// 2026-10-11 起 guard 层把凭证对链路的失败也落进本表。
	Event string `gorm:"column:event;type:varchar(16);not null;default:call;index" json:"event"`
	// Method 取 tools/list / tools/call（也记其它 JSON-RPC 方法，便于看客户端行为）
	Method string `gorm:"type:varchar(40);index" json:"method"`
	// Tool 仅在 tools/call 时非空
	Tool string `gorm:"type:varchar(128);index" json:"tool"`
	// Allowed=false 表示被凭证的工具授权范围（scope）拒绝，未执行
	Allowed   bool      `gorm:"not null;default:true;index" json:"allowed"`
	OK        bool      `gorm:"not null;default:false;index" json:"ok"`
	ErrorMsg  string    `gorm:"type:text" json:"error_msg"`
	ArgsHash  string    `gorm:"column:args_hash;type:varchar(64)" json:"args_hash"`
	LatencyMS int64     `gorm:"not null;default:0" json:"latency_ms"`
	IP        string    `gorm:"type:varchar(64)" json:"ip"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (MCPCallAudit) TableName() string { return "mcp_call_audits" }

// MCPSkill MCP Skill 注册表（2026-10-10 新增）。
//
// 一个 Skill = 一份给外部 AI Agent 用的能力说明（SKILL.md）+ 一把绑定凭证 + 可用工具集。
// 此前 skills/hivemtk/ 只是仓库里的一个静态目录：平台侧不知道有哪些 Skill 在用、
// 各自能用哪些工具，也就无从审计与吊销。本表把这件事收敛成可管理的记录。
type MCPSkill struct {
	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
	// Description 供人和 Agent 识别用途（对应 SKILL.md 的 description 前置元数据）
	Description string `gorm:"type:text" json:"description"`
	// CredentialID 绑定签发的凭证；解绑后 Skill 无法调用 MCP 入口
	CredentialID *uint `gorm:"column:credential_id;index" json:"credential_id"`
	// AllowedTools 该 Skill 期望的工具集（逗号分隔）。签发/保存时同步写入绑定凭证的 scope。
	AllowedTools string `gorm:"column:allowed_tools;type:text" json:"allowed_tools"`
	// DocURL SKILL.md 的可访问位置（仓库路径或文档地址），仅作登记
	DocURL string `gorm:"column:doc_url;type:varchar(512)" json:"doc_url"`
	// Guardrails 红线提示（如"reach.* 外发前必须人工确认"），回显给管理端与接入片段
	Guardrails string    `gorm:"type:text" json:"guardrails"`
	Enabled    bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (MCPSkill) TableName() string { return "mcp_skills" }
