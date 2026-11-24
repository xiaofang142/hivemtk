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
	// AgentTypeLeadMining 线索发现智能体（FR-026：「线索发现」改名为「线索发现智能体」）。
	// 复用 ai_agents 表，仅以 agent_type 子类型区分，不新建表。
	AgentTypeLeadMining AgentType = "lead_mining"
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
	ChannelTypePDD ChannelType = "pdd"
	// 2026-11-18 电商店铺五渠道（CH-15/18/19/28/35，二次审核补齐）：与
	// model.Channel* 同名；ENUM 值由 v3.98.0 迁移补入 channel_type_enum。
	ChannelTypeTaobao       ChannelType = "taobao"
	ChannelTypeDouyinShop   ChannelType = "douyin_shop"
	ChannelTypeKuaishouShop ChannelType = "kuaishou_shop"
	ChannelTypeShopee       ChannelType = "shopee"
	ChannelTypeWalmartUS    ChannelType = "walmart_us"
	ChannelTypeWeb          ChannelType = "web"
	ChannelTypeWebEmbed     ChannelType = "web_embed"
	// 2026-11 specs/025 新平台（与 model.Channel* 同名）。
	ChannelTypeWeComCS   ChannelType = "wecomcs"
	ChannelTypeOneBot    ChannelType = "onebot"
	ChannelTypeSlack     ChannelType = "slack"
	ChannelTypeWeCombBot ChannelType = "wecombot"
)

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
