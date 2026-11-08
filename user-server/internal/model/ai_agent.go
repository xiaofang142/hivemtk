package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/lib/pq"
)

// AgentType 智能体类型（仅作为智能体内部子类型，平台层统一称「智能体」）
type AgentType string

const (
	AgentTypeSales           AgentType = "sales"
	AgentTypeCustomerService AgentType = "customer_service"
	AgentTypeHybrid          AgentType = "hybrid"
)

// AgentMode 智能体工作模式（平台层两大运行范式）
type AgentMode string

const (
	AgentModePassive AgentMode = "passive"
	AgentModeActive  AgentMode = "active"
)

// AIAgent AI 智能体主表
//
// 一个 AIAgent = 一套完整的 智能体配置（人设 + 知识库 + LLM + SOP + 话术）
// 可被多渠道账号绑定，也可被多客服座席挂载
type AIAgent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentCode   string `gorm:"type:varchar(64);uniqueIndex;not null" json:"agent_code"`
	Name        string `gorm:"type:varchar(128);not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Avatar      string `gorm:"type:varchar(500)" json:"avatar"`
	AgentType   string `gorm:"type:varchar(32);not null;default:'sales';index" json:"agent_type"`
	AgentMode   string `gorm:"type:varchar(32);not null;default:'passive';index" json:"agent_mode"`

	Persona      string `gorm:"type:text;not null" json:"persona"`
	SystemPrompt string `gorm:"type:text" json:"system_prompt"`
	Greeting     string `gorm:"type:text" json:"greeting"`

	RagProductIDs pq.StringArray `gorm:"type:text[];column:rag_product_ids" json:"rag_product_ids"`

	FAQEntryIDs pq.StringArray `gorm:"type:text[];column:faq_entry_ids" json:"faq_entry_ids"`

	SOPTemplateIDs pq.StringArray `gorm:"type:text[];column:sop_template_ids" json:"sop_template_ids"`

	SOPIDs pq.StringArray `gorm:"type:text[];column:sop_ids" json:"sop_ids"`

	ScriptLibraryIDs pq.StringArray `gorm:"type:text[];column:script_library_ids" json:"script_library_ids"`

	ABExperimentIDs pq.StringArray `gorm:"type:text[];column:ab_experiment_ids" json:"ab_experiment_ids"`

	AssetBundleID string `gorm:"type:varchar(128);column:asset_bundle_id;default:''" json:"asset_bundle_id"`

	LLMModel          string            `gorm:"type:varchar(100);default:'Qwen2.5-3B-Instruct'" json:"llm_model"`
	LLMProviderConfig LLMProviderConfig `gorm:"embedded;embeddedPrefix:llm_" json:"llm_provider_config"`
	Temperature       float64           `gorm:"default:0.7" json:"temperature"`
	MaxTokens         int               `gorm:"default:800" json:"max_tokens"`
	TopP              float64           `gorm:"default:0.9" json:"top_p"`
	FrequencyPenalty  float64           `gorm:"default:0.5" json:"frequency_penalty"`
	PresencePenalty   float64           `gorm:"default:0.5" json:"presence_penalty"`

	InternalLanguage string `gorm:"type:varchar(8);default:'zh'" json:"internal_language"`
	TargetLanguage   string `gorm:"type:varchar(8);default:''" json:"target_language"`

	// 四个「默认开启」的能力开关必须是 *bool：bool + `default:true` 时 GORM 会把
	// 显式 false 当成零值，写入 DefaultValueInterface(true) 并回填结构体 —— 创建时
	// 关不掉能力，且内存值与落库值不一致。指针语义：nil=未传 → 落 DB 默认 true，
	// 显式 false → 真写 false。读取一律走 RAGEnabled() 等取值方法，别直接解引用。
	EnableRAG            *bool `gorm:"default:true" json:"enable_rag"`
	EnableScriptMatch    *bool `gorm:"default:true" json:"enable_script_match"`
	EnableHumanizePolish *bool `gorm:"default:true" json:"enable_humanize_polish"`
	EnableContentAudit   bool  `gorm:"default:false" json:"enable_content_audit"`
	EnablePlaybook       *bool `gorm:"default:true" json:"enable_playbook"`
	RAGTopK              int   `gorm:"default:3" json:"rag_top_k"`

	ConfidenceThreshold float64 `gorm:"default:0.7" json:"confidence_threshold"`
	MaxAIConsecutive    int     `gorm:"default:0" json:"max_ai_consecutive"`

	Status  int `gorm:"default:1;index" json:"status"`
	Version int `gorm:"default:1" json:"version"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (AIAgent) TableName() string {
	return "ai_agents"
}

// ChannelType 渠道类型枚举
type ChannelType string

const (
	ChannelTypeTelegram    ChannelType = "telegram"
	ChannelTypeQQ          ChannelType = "qq"
	ChannelTypeWeCom       ChannelType = "wecom"
	ChannelTypeFeishu      ChannelType = "feishu"
	ChannelTypeWhatsApp    ChannelType = "whatsapp"
	ChannelTypeDingTalk    ChannelType = "dingtalk"
	ChannelTypeDouyin      ChannelType = "douyin"
	ChannelTypeXiaohongshu ChannelType = "xiaohongshu"
	ChannelTypeKuaishou    ChannelType = "kuaishou"
	ChannelTypeXianyu      ChannelType = "xianyu"
	ChannelTypeTikTok      ChannelType = "tiktok"
	ChannelTypeJD          ChannelType = "jd"
	ChannelTypeBilibili    ChannelType = "bilibili"
	ChannelTypeWeibo       ChannelType = "weibo"
	ChannelTypePersonalWX  ChannelType = "personal_wx"
	// CH-16 首切片（2026-11-09）：拼多多商家网页渠道，与 model.ChannelPDD 同名。
	ChannelTypePDD      ChannelType = "pdd"
	ChannelTypeWeb      ChannelType = "web"
	ChannelTypeWebEmbed ChannelType = "web_embed"
)

// ChannelAgentBinding 渠道账号 ↔ 智能体绑定
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

// CustomerServiceAgent 客服座席 ↔ 智能体挂载
//
// 同一座席可挂载多个智能体，但只有一个 is_primary=true
// SmartCSOrchestrator 在处理消息时按 agent_status_id 查询主挂载智能体
type CustomerServiceAgent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentStatusID uint `gorm:"not null;index;uniqueIndex:idx_cs_agent_unique" json:"agent_status_id"`
	AIAgentID     uint `gorm:"not null;index;uniqueIndex:idx_cs_agent_unique" json:"ai_agent_id"`
	// 指针语义同 ChannelAgentBinding：显式 false 必须能落库，否则「辅助挂载」会
	// 变成主挂载、停用的挂载会在 Create 时被默认值拉回启用。
	IsPrimary *bool `gorm:"default:true" json:"is_primary"`
	Enabled   *bool `gorm:"default:true" json:"enabled"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (CustomerServiceAgent) TableName() string {
	return "customer_service_agents"
}
