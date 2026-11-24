package model

import (
	"time"

	"gorm.io/gorm"
)

// ChannelAgentBinding 渠道账号 ↔ 智能体绑定
//
// T005 注记：该类型原定义在 ai_agent.go（本仓历史位置）。渠道域地基落地时把它
// 原位搬迁到本文件，**字段、表名、GORM 标签一律不变**，避免重复声明导致编译冲突。
// data-model.md 约定的「移除智能体管理中绑定渠道账号的逻辑、绑定收口到
// channel_accounts 的 auto_reply_agent_id / lead_mining_agent_id」属于 US3（T053），
// 本阶段不动该表语义。
//
// 强 1对1 改造 (Task 21):
//   - 同一 (channel_type, account_id) 只能有 1 条 is_primary=true 记录
//   - 数据库层通过唯一索引保证: idx_channel_binding_unique
//     (tenant_id, channel_type, account_id, COALESCE(chat_id, ”))
//   - 历史 is_primary=false 的辅助记录不再被业务使用, 保留为审计/回滚用途
//   - 业务层在 Replace / Bind 时, 必须先 ClearPrimaryByChannelAccount 再 Create
//
// 表: channel_agent_bindings
// 索引:
//   - INDEX(channel_type, account_id)       按渠道账号查询
//   - INDEX(agent_id)                       反查智能体被哪些渠道使用
//   - idx_channel_binding_unique (v3.70.0) 唯一键，带 chat_id 维度
//
// **唯一键为什么带 chat_id**：chat_id 是「群组/会话 ID」（Telegram chat_id /
// Slack channel_id），NULL=account 级默认。telegram 群组功能靠它区分「同一账号下的
// 多个群」——不含 chat_id 的唯一键会让同一账号的第二个群根本建不出绑定行。
// 而 chat_id 为 NULL 与空串必须视为同一个键（PG 唯一索引里 NULL 互不相等，
// 不归一的话 account 级默认可以无限多条），所以用 COALESCE(chat_id, ”)。
//
// **为什么由版本化迁移建而不是模型标签**：GORM 的 uniqueIndex 标签表达不了
// COALESCE 表达式索引。该索引由 v3.70.0 迁移建（见
// internal/migration/migrations/v3_70_0_channel_binding_chat_id_unique_key_migration.go），
// 而 v3.15 迁移建的那个不含 chat_id 的旧索引由同一迁移替换掉。
// 注：更早的注释说唯一键是 `uq_channel_account_primary (channel_type, account_id)
// WHERE is_primary = true`（迁移 035 的形状）——该索引在开发库里并不存在，
// 且那个形状也不含 tenant_id，多租户下会让一条渠道账号只能属于一个租户。
type ChannelAgentBinding struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelType string `gorm:"type:varchar(32);not null;index" json:"channel_type"`
	AccountID   string `gorm:"type:varchar(64);not null;index" json:"account_id"`
	ChatID      string `gorm:"type:varchar(64);index" json:"chat_id,omitempty"` // 群组/会话 ID（Telegram chat_id / Slack channel_id），NULL=account 级默认
	ChatType    string `gorm:"type:varchar(16)" json:"chat_type,omitempty"`     // group | dm | thread
	Priority    int    `gorm:"default:0" json:"priority"`                       // 手动优先级，高优先
	AgentID     uint   `gorm:"not null;index" json:"agent_id"`
	// IsPrimary/Enabled 用 *bool 的原因同 AIAgent 能力开关：bool + `default:true`
	// 会把显式 false 吞成 true。这里吞掉的后果是硬伤 —— 要求建「辅助绑定」
	// (is_primary=false) 的请求会落成第二条主绑定（ClearPrimary 没被调用），
	// 要求 enabled=false 的绑定会直接上线接流量。
	IsPrimary *bool `gorm:"default:true" json:"is_primary"`
	Enabled   *bool `gorm:"default:true" json:"enabled"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (ChannelAgentBinding) TableName() string {
	return "channel_agent_bindings"
}
