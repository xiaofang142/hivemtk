package dto

import (
	"time"
)

// LLMProviderConfig LLM 提供商配置（镜像 model.LLMProviderConfig，去 gorm 标签）
type LLMProviderConfig struct {
	APIKey         string `json:"api_key"`
	BaseURL        string `json:"base_url"`
	APIType        string `json:"api_type"`
	Model          string `json:"model"`
	MaxRetries     int    `json:"max_retries"`
	RequestTimeout int    `json:"request_timeout"`
}

// DialogueMemory 对话长期记忆视图（镜像 model.DialogueMemory，去 gorm 标签）
type DialogueMemory struct {
	ID                   uint           `json:"id"`
	SessionID            string         `json:"session_id"`
	CustomerID           string         `json:"customer_id"`
	Summary              string         `json:"summary"`
	KeyFacts             map[string]any `json:"key_facts"`
	CustomerName         string         `json:"customer_name"`
	CustomerPhone        string         `json:"customer_phone"`
	CustomerWechat       string         `json:"customer_wechat"`
	Budget               string         `json:"budget"`
	Demand               string         `json:"demand"`
	Objections           []any          `json:"objections"`
	PurchaseIntent       string         `json:"purchase_intent"`
	IntentTrail          []any          `json:"intent_trail"`
	SOPHistory           []any          `json:"sop_history"`
	LastAction           string         `json:"last_action"`
	NextActionSuggestion string         `json:"next_action_suggestion"`
	LastActiveAt         time.Time      `json:"last_active_at"`
	MessageCount         int            `json:"message_count"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// SOPAgent SOP 智能体视图（镜像 model.SOPAgent，去 gorm 标签）
type SOPAgent struct {
	ID             uint           `json:"id"`
	Name           string         `json:"name"`
	Scenario       string         `json:"scenario"`
	Description    string         `json:"description"`
	TriggerType    string         `json:"trigger_type"`
	TriggerConfig  map[string]any `json:"trigger_config"`
	SOPGraph       map[string]any `json:"sop_graph"`
	Version        int            `json:"version"`
	IsActive       bool           `json:"is_active"`
	Priority       int            `json:"priority"`
	ExecutionCount int            `json:"execution_count"`
	SuccessCount   int            `json:"success_count"`
	ABTestConfig   map[string]any `json:"ab_test_config"`
	UseBandit      bool           `json:"use_bandit"`
	CreatedBy      uint           `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// RichCardType 卡片类型
type RichCardType string

// CardButton 卡片动作按钮
type CardButton struct {
	Text   string `json:"text"`
	URL    string `json:"url,omitempty"`
	Action string `json:"action,omitempty"`
}

// RichCard 会话内结构化富卡片（镜像 model.RichCard）
type RichCard struct {
	Type        RichCardType      `json:"type"`
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle,omitempty"`
	Description string            `json:"description,omitempty"`
	ImageURL    string            `json:"image_url,omitempty"`
	ThumbURL    string            `json:"thumb_url,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
	Buttons     []CardButton      `json:"buttons,omitempty"`
}

// SalesStepLog 销售链路步骤日志
type SalesStepLog struct {
	Step      string `json:"step"`
	Status    string `json:"status"`
	LatencyMs int    `json:"latency_ms"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
	Extra     any    `json:"extra,omitempty"`
}

// RAGChunk RAG 召回片段
type RAGChunk struct {
	Content string  `json:"content"`
	Source  string  `json:"source"`
	Score   float64 `json:"score"`
	DocID   string  `json:"doc_id"`
	ChunkID string  `json:"chunk_id"`
	Weight  float64 `json:"weight"`
}

// ScriptTemplate 话术模板
type ScriptTemplate struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Scenario  string   `json:"scenario"`
	Tags      []string `json:"tags"`
	MatchRate float64  `json:"match_rate"`
}

// Industry 行业
type Industry string

const (
	IndustryMedicalBeauty Industry = "medical_beauty"
	IndustryEducation     Industry = "education"
	IndustryEcommerce     Industry = "ecommerce"
	IndustryRealEstate    Industry = "real_estate"
	IndustryAuto          Industry = "auto"
	IndustryFinance       Industry = "finance"
	IndustryB2B           Industry = "b2b"
)

// ObjectionType 异议类型（销冠话术库专用，与 objection_handler_service.go 的 ObjectionCategory 区分）
type ObjectionType string

const (
	PlayObjectionPrice       ObjectionType = "price"
	PlayObjectionTime        ObjectionType = "time"
	PlayObjectionTrust       ObjectionType = "trust"
	PlayObjectionCompetition ObjectionType = "competition"
	PlayObjectionNeed        ObjectionType = "need"
	PlayObjectionAuthority   ObjectionType = "authority"
	PlayObjectionStall       ObjectionType = "stall"
)

// PlaybookEntry 话术条目
type PlaybookEntry struct {
	ID           string        `json:"id"`
	Industry     Industry      `json:"industry"`
	ProductID    string        `json:"product_id"`
	Stage        JourneyStage  `json:"stage"`
	Objection    ObjectionType `json:"objection"`
	Title        string        `json:"title"`
	Content      string        `json:"content"`
	Tips         string        `json:"tips"`
	Tags         []string      `json:"tags"`
	UseCount     int           `json:"use_count"`
	SuccessCount int           `json:"success_count"`
	CreatedBy    string        `json:"created_by"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// JourneyStage 客户旅程阶段
type JourneyStage string

const (
	StageStranger   JourneyStage = "stranger"
	StageLead       JourneyStage = "lead"
	StageContact    JourneyStage = "contact"
	StageInterested JourneyStage = "interested"
	StageQuoted     JourneyStage = "quoted"
	StageWon        JourneyStage = "won"
	StageAfterSale  JourneyStage = "after_sale"
	StageRepurchase JourneyStage = "repurchase"
	StageSleeping   JourneyStage = "sleeping"
	StageLost       JourneyStage = "lost"
)

// AllStages 所有阶段
var AllStages = []JourneyStage{
	StageStranger, StageLead, StageContact, StageInterested,
	StageQuoted, StageWon, StageAfterSale, StageRepurchase, StageSleeping, StageLost,
}

// JourneyTransitionRequest 客户旅程阶段迁移请求
type JourneyTransitionRequest struct {
	CustomerID string       `json:"customer_id" binding:"required"`
	ToStage    JourneyStage `json:"to_stage" binding:"required"`
	Source     string       `json:"source"`
	Reason     string       `json:"reason"`
}

// SalesEngineConfig 销售引擎配置
type SalesEngineConfig struct {
	EnableRAG            bool    `json:"enable_rag"`
	EnableScriptMatch    bool    `json:"enable_script_match"`
	EnableHumanizePolish bool    `json:"enable_humanize_polish"`
	EnableContentAudit   bool    `json:"enable_content_audit"`
	RAGTopK              int     `json:"rag_top_k"`
	Temperature          float64 `json:"temperature"`
	MaxTokens            int     `json:"max_tokens"`
	Persona              string  `json:"persona"`

	CustomerLevel string `json:"customer_level,omitempty"`

	Tools []string `json:"tools,omitempty"`
}

// DefaultSalesEngineConfig 默认配置
func DefaultSalesEngineConfig() *SalesEngineConfig {
	return &SalesEngineConfig{
		EnableRAG:            true,
		EnableScriptMatch:    true,
		EnableHumanizePolish: true,
		EnableContentAudit:   false,
		RAGTopK:              3,
		Temperature:          0.7,
		MaxTokens:            800,
		Persona:              "你是一位资深销售专家，擅长用温和、专业的语气帮助客户解决问题。回复简洁、亲切、不超过 80 字。",
	}
}

// AgentContext 智能体执行上下文
// 由 AIAgentService.LoadContext 加载，传给 SalesEngine.HandleWithAgent
type AgentContext struct {
	AgentID              uint              `json:"agent_id"`
	AgentCode            string            `json:"agent_code"`
	Name                 string            `json:"name"`
	AgentType            string            `json:"agent_type"`
	AgentMode            string            `json:"agent_mode"`
	Persona              string            `json:"persona"`
	SystemPrompt         string            `json:"system_prompt"`
	Greeting             string            `json:"greeting"`
	RagProductIDs        []string          `json:"rag_product_ids"`
	FAQEntryIDs          []string          `json:"faq_entry_ids"`
	SOPTemplateIDs       []string          `json:"sop_template_ids"`
	SOPIDs               []string          `json:"sop_ids"`
	ScriptLibraryIDs     []string          `json:"script_library_ids"`
	LLMModel             string            `json:"llm_model"`
	LLMProviderConfig    LLMProviderConfig `json:"llm_provider_config"`
	Temperature          float64           `json:"temperature"`
	MaxTokens            int               `json:"max_tokens"`
	TopP                 float64           `json:"top_p"`
	FrequencyPenalty     float64           `json:"frequency_penalty"`
	PresencePenalty      float64           `json:"presence_penalty"`
	EnableRAG            bool              `json:"enable_rag"`
	EnableScriptMatch    bool              `json:"enable_script_match"`
	EnableHumanizePolish bool              `json:"enable_humanize_polish"`
	EnableContentAudit   bool              `json:"enable_content_audit"`
	EnablePlaybook       bool              `json:"enable_playbook"`
	RAGTopK              int               `json:"rag_top_k"`
	ConfidenceThreshold  float64           `json:"confidence_threshold"`
	MaxAIConsecutive     int               `json:"max_ai_consecutive"`

	ABExperimentIDs []string `json:"ab_experiment_ids"`

	AssetBundleID string `json:"asset_bundle_id,omitempty"`

	Tools []string `json:"tools,omitempty"`

	Version int `json:"version"`
}

// AgentContextToSalesEngineConfig 将 AgentContext 转换为 SalesEngineConfig（包级函数，type alias 不能定义方法）
func AgentContextToSalesEngineConfig(a *AgentContext) *SalesEngineConfig {
	if a == nil {
		return DefaultSalesEngineConfig()
	}
	return &SalesEngineConfig{
		EnableRAG:            a.EnableRAG,
		EnableScriptMatch:    a.EnableScriptMatch,
		EnableHumanizePolish: a.EnableHumanizePolish,
		EnableContentAudit:   a.EnableContentAudit,
		RAGTopK:              a.RAGTopK,
		Temperature:          a.Temperature,
		MaxTokens:            a.MaxTokens,
		Persona:              a.Persona,
		Tools:                a.Tools,
	}
}

// SalesRequest 销售请求
type SalesRequest struct {
	SessionID   string             `json:"session_id"`
	CustomerID  string             `json:"customer_id"`
	OneID       string             `json:"one_id"`
	UserMessage string             `json:"user_message"`
	Platform    string             `json:"platform"`
	Config      *SalesEngineConfig `json:"config,omitempty"`

	AgentContext *AgentContext `json:"agent_context,omitempty"`

	// IsFirstTurn 标记是否为会话首条消息（用于行为层拟人 thinking pause 判定），
	// 由上游 chat_visitor / web_chat 等入口根据 session 消息历史判断后填入
	IsFirstTurn bool `json:"is_first_turn,omitempty"`

	// EmotionHint 情感分层策略提示（焦虑=进度可视化 / 满意=裂变引导），空=无情绪干预；
	// 由 SmartCSOrchestrator 按 ClassifyEmotion 结果注入，buildPrompt 消费。
	EmotionHint string `json:"emotion_hint,omitempty"`

	// NoFastpath 禁用 FAQ fastpath 直答（2026-11-14 生产实证）：同会话近期已投递过
	// 相同标准答案时（客户在追问、答案未解决问题），由编排器置位重跑完整 LLM 链，
	// 结合对话历史生成针对追问的新回答——否则固定文案撞已投递占位，回复静默丢失。
	NoFastpath bool `json:"no_fastpath,omitempty"`
}

// SalesResponse 销售回复
type SalesResponse struct {
	Reply      string           `json:"reply"`
	Intent     *RecognizeResult `json:"intent"`
	Memory     *DialogueMemory  `json:"memory,omitempty"`
	MatchedSOP *SOPAgent        `json:"matched_sop,omitempty"`
	RAGChunks  []RAGChunk       `json:"rag_chunks,omitempty"`
	// Citations 答案引用溯源：FAQ 级/chunk 级引用，纯旁路字段，
	// 不参与置信度判定（组装发生在 extractConfidenceWithTopK 之后）。
	Citations           []Citation       `json:"citations,omitempty"`
	ScriptTemplate      *ScriptTemplate  `json:"script_template,omitempty"`
	PlaybookSuggestions []*PlaybookEntry `json:"playbook_suggestions,omitempty"`
	LLMProvider         string           `json:"llm_provider,omitempty"`
	LLMModel            string           `json:"llm_model,omitempty"`
	CostTokens          int              `json:"cost_tokens"`
	LatencyMs           int              `json:"latency_ms"`
	Polished            bool             `json:"polished"`
	Audited             bool             `json:"audited"`
	AuditIssues         []string         `json:"audit_issues,omitempty"`
	TransferredToHuman  bool             `json:"transferred_to_human"`

	// DefiniteAnswer 标记本条回复来自确定答案（FAQ 标准答案 / 人工标注答案），
	// 而不是模型现场生成。编排器据此跳过置信度判据——确定答案就是标准答案本身，
	// 不需要再判"这个回答可不可信"。
	DefiniteAnswer bool `json:"definite_answer,omitempty"`

	// Degraded 标记本条回复是引擎的降级兜底文案（模型空输出 → emptyReplyFallback），
	// 不是模型生成的答案。编排器透传到出站行 extra.degraded，并把意图置信度从出站行
	// extra.confidence 摘掉——降级轮没有模型答案，不得声称任何置信度，否则应答质量
	// 指标把降级轮当正常 AI 回答计数（CS-02 口径修复；放行语义不变）。
	Degraded       bool           `json:"degraded,omitempty"`
	TransferReason string         `json:"transfer_reason,omitempty"`
	Cards          []RichCard     `json:"cards,omitempty"`
	Steps          []SalesStepLog `json:"steps"`

	Confidence *ConfidenceDecision `json:"confidence,omitempty"`

	HumanizeScore   float64 `json:"humanize_score,omitempty"`
	HumanizePassed  bool    `json:"humanize_passed,omitempty"`
	HumanizeAttempt int     `json:"humanize_attempt,omitempty"`

	// SendPlan 行为层拟人发送计划（打字延迟 + 分条发送）。
	// 为 nil 表示未启用行为层拟人，前端应直接用 Reply
	SendPlan *SendPlanDTO `json:"send_plan,omitempty"`
}

// SendPlanDTO 发送计划（DTO 层）
type SendPlanDTO struct {
	Messages   []string  `json:"messages"`
	Intervals  []float64 `json:"intervals,omitempty"`
	TotalDelay float64   `json:"total_delay"`
}

// SalesRequestHasAgentContext 判断 SalesRequest 是否携带智能体上下文（包级函数，type alias 不能定义方法）
func SalesRequestHasAgentContext(r *SalesRequest) bool {
	return r != nil && r.AgentContext != nil
}

// SalesRequestRagProductIDs 获取当前请求应检索的知识库产品 ID 列表
// 优先使用 agentCtx.RagProductIDs，回退空（由 RAG 服务默认处理）
func SalesRequestRagProductIDs(r *SalesRequest) []string {
	if SalesRequestHasAgentContext(r) && len(r.AgentContext.RagProductIDs) > 0 {
		return r.AgentContext.RagProductIDs
	}
	return nil
}

// SalesRequestSOPIDs 获取当前请求应匹配的 SOP ID 列表
func SalesRequestSOPIDs(r *SalesRequest) []string {
	if SalesRequestHasAgentContext(r) && len(r.AgentContext.SOPIDs) > 0 {
		return r.AgentContext.SOPIDs
	}
	return nil
}

// SalesRequestAgentPersona 获取当前请求的智能体人设
// 供 LLM 生成步骤注入系统提示
func SalesRequestAgentPersona(r *SalesRequest) string {
	if SalesRequestHasAgentContext(r) {
		if r.AgentContext.SystemPrompt != "" {
			return r.AgentContext.SystemPrompt
		}
		if r.AgentContext.Persona != "" {
			return r.AgentContext.Persona
		}
	}
	if r.Config != nil && r.Config.Persona != "" {
		return r.Config.Persona
	}
	return ""
}

// SalesRequestAgentLLMModel 获取当前请求应使用的 LLM 模型
// 留待 LLM 调度层使用（当前 dispatcher 默认模型）
func SalesRequestAgentLLMModel(r *SalesRequest) string {
	if SalesRequestHasAgentContext(r) && r.AgentContext.LLMModel != "" {
		return r.AgentContext.LLMModel
	}
	return ""
}
