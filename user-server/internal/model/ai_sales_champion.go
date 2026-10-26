package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// MessageHub 消息中台 - 多账号聚合消息
type MessageHub struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform string `gorm:"type:varchar(30);not null;index;uniqueIndex:uni_message_hub_platform_msg_conv,priority:1" json:"platform"`
	MsgID    string `gorm:"type:varchar(100);uniqueIndex:uni_message_hub_platform_msg_conv,priority:2" json:"msg_id"`
	// Direction 参与唯一索引（D-2 方案③，2026-10-23）：ContentHashMsgID 不含方向，
	// 同会话内"客户发 X（inbound）+ AI 回复恰好也是 X（outbound）"两行三元组相同会撞
	// 唯一索引（d2_msg_id_cross_direction_test.go 实测 23505 证实）。加 direction 后
	// 跨方向可共存；同方向重复仍被索引兜底去重（入站重复去重语义不变）。
	Direction      string     `gorm:"type:varchar(10);not null;uniqueIndex:uni_message_hub_platform_msg_conv,priority:4" json:"direction"`
	AccountID      string     `gorm:"type:varchar(100);not null;index" json:"account_id"`
	Status         string     `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	MsgType        string     `gorm:"type:varchar(20);not null" json:"msg_type"`
	SenderID       string     `gorm:"type:varchar(100);index" json:"sender_id"`
	SenderName     string     `gorm:"type:varchar(200)" json:"sender_name"`
	ReceiverID     string     `gorm:"type:varchar(100)" json:"receiver_id"`
	ReceiverName   string     `gorm:"type:varchar(200)" json:"receiver_name"`
	Content        string     `gorm:"type:text" json:"content"`
	MediaURL       string     `gorm:"type:varchar(500)" json:"media_url"`
	ConversationID string     `gorm:"type:varchar(100);index;uniqueIndex:uni_message_hub_platform_msg_conv,priority:3" json:"conversation_id"`
	IsGroup        bool       `gorm:"default:false" json:"is_group"`
	GroupID        string     `gorm:"type:varchar(100)" json:"group_id"`
	IsAIReply      bool       `gorm:"default:false" json:"is_ai_reply"`
	AIAgent        string     `gorm:"type:varchar(50)" json:"ai_agent"`
	TraceID        string     `gorm:"type:varchar(64);index:idx_hub_trace" json:"trace_id"`
	DedupHash      string     `gorm:"type:varchar(64);index:idx_mh_dedup_hash" json:"dedup_hash"`
	ClaimedAt      *time.Time `gorm:"index" json:"claimed_at"`
	// PushAttempts/PushError（批20d-A3）只给出站行记账：服务端每把一行交给桥端一次计一次，
	// 到 MaxOutboundPushAttempts 即落终态 failed，PushError 写明「为什么不再是 pending」。
	// 计数留在行上而不是内存里：SW 回收/进程重启都要能接着算，否则预算每次重启都白送一轮。
	PushAttempts int        `gorm:"not null;default:0" json:"push_attempts"`
	PushError    string     `gorm:"type:varchar(200);not null;default:''" json:"push_error"`
	IsRead       bool       `gorm:"default:false" json:"is_read"`
	ReadAt       *time.Time `json:"read_at"`
	SentAt       time.Time  `gorm:"index" json:"sent_at"`
	Extra        JSONMap    `gorm:"type:text" json:"extra"`
	// PlatformMsgID D-9（2026-10-24）：平台侧消息号（发送成功后由平台返回；
	// hub 行键 msg_id 是 ContentHash/合成键，两者不是一回事）。可空：发送时
	// 平台还没返回；历史行不回填（TG/飞书可按行键格式解析，见 platformMessageID）。
	// 复合索引 (platform, account_id, platform_msg_id) 由迁移 v3.82.0 建——模型
	// **有意**不用 index 标签（同 v3.81.0 教训：链路 Down 会删掉 AutoMigrate
	// 在用的索引，判"越权删除在用索引"）。
	PlatformMsgID string `gorm:"type:varchar(100)" json:"platform_msg_id,omitempty"`
	// RecalledAt / RecallError（B2，2026-10-26）：平台撤回的结果格。
	//
	// 为什么不在 Status 上再加一个 'recalled'：Status 记的是**发送**状态机
	// （pending→sent/delivered/send_failed），描述"平台有没有把这条发出去"；
	// 撤回是"发出去之后有没有撤掉"，是另一个维度。两件事挤进一列会出现
	// "撤回了但发送状态被抹成 recalled" 这种把投递事实抹掉的写法，而且
	// UpdateDeliveryStatus 的乱序回执保护（send_failed 不回翻）也就跟着失效。
	//
	// RecalledAt 非空 = 平台已确认撤回（终态，幂等判据）；RecallError 只记最近一次
	// 失败原因，成功时清空——留着它才能回答"点过撤回但没成，为什么"。
	RecalledAt  *time.Time     `json:"recalled_at,omitempty"`
	RecallError string         `gorm:"type:varchar(200);not null;default:''" json:"recall_error,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MessageHub) TableName() string { return "message_hub" }

// IntentRecord 销售意图识别记录
type IntentRecord struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID       string         `gorm:"type:varchar(120);not null;index" json:"session_id"`
	CustomerID      string         `gorm:"type:varchar(64);index" json:"customer_id"`
	MessageID       uint           `json:"message_id"`
	RawText         string         `gorm:"type:text;not null" json:"raw_text"`
	IntentType      string         `gorm:"type:varchar(50);not null;index" json:"intent_type"`
	IntentSubtype   string         `gorm:"type:varchar(50)" json:"intent_subtype"`
	Confidence      float64        `gorm:"type:decimal(5,2);not null" json:"confidence"`
	ConfidenceLevel string         `gorm:"type:varchar(20)" json:"confidence_level"`
	Entities        JSONMap        `gorm:"type:text" json:"entities"`
	Sentiment       string         `gorm:"type:varchar(20)" json:"sentiment"`
	LLMModel        string         `gorm:"type:varchar(50)" json:"llm_model"`
	CostTokens      int            `gorm:"default:0" json:"cost_tokens"`
	LatencyMs       int            `gorm:"default:0" json:"latency_ms"`
	CreatedAt       time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// ---- 以下 6 列由精细意图链路（model.IntentLog / 迁移 v3.22.3）写入 ----
	//
	// ⚠️ 本结构体是 intent_records 的**唯一 automigrate 来源**
	// （见 internal/pkg/db/migrate.go 的 AutoMigrate 清单），因此它必须是该表的
	// **完整**列映射。缺陷：这 6 列只声明在 model.IntentLog 里，而 IntentLog
	// 被明确禁止加入 automigrate 清单 —— 于是
	//   ① 测试侧：testutil.NewTestDB 会 DropTable 后按模型重建，缺列即丢列，
	//      精细意图查询报 column "source" does not exist (SQLSTATE 42703)（8 个用例失败）；
	//   ② 生产侧：全新部署时 AutoMigrate 建出的表同样缺这 6 列，精细意图功能不可用
	//      （存量库之所以正常，只是因为 AutoMigrate 不会删列）。
	// 列宽/默认值以实时库 user_db.intent_records 为准（22 列）。
	//
	// 这些字段用 json:"-" 隐藏：controller/intent.go 直接把 []model.IntentRecord 返回给
	// 前端，补列不得改变既有响应结构（精细日志另有 IntentLog 视图与专属接口暴露）。
	TraceID   string     `gorm:"type:varchar(64);index" json:"-"`
	Timestamp *time.Time `json:"-"`
	Source    string     `gorm:"type:varchar(32);not null;default:sales;index" json:"-"`
	Method    string     `gorm:"type:varchar(16);not null;default:llm" json:"-"`
	Reasoning string     `gorm:"type:text" json:"-"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"-"`
}

func (IntentRecord) TableName() string { return "intent_records" }

// DialogueMemory 对话长期记忆
type DialogueMemory struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                   uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID            string    `gorm:"type:varchar(120);not null" json:"session_id"`
	CustomerID           string    `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	Summary              string    `gorm:"type:text" json:"summary"`
	KeyFacts             JSONMap   `gorm:"type:text" json:"key_facts"`
	CustomerName         string    `gorm:"type:varchar(100)" json:"customer_name"`
	CustomerPhone        string    `gorm:"type:varchar(20)" json:"customer_phone"`
	CustomerWechat       string    `gorm:"type:varchar(100)" json:"customer_wechat"`
	Budget               string    `gorm:"type:varchar(200)" json:"budget"`
	Demand               string    `gorm:"type:text" json:"demand"`
	Objections           JSONArray `gorm:"type:text" json:"objections"`
	PurchaseIntent       string    `gorm:"type:varchar(20);index" json:"purchase_intent"`
	IntentTrail          JSONArray `gorm:"type:text" json:"intent_trail"`
	SOPHistory           JSONArray `gorm:"type:text" json:"sop_history"`
	LastAction           string    `gorm:"type:varchar(2000)" json:"last_action"`
	NextActionSuggestion string    `gorm:"type:varchar(200)" json:"next_action_suggestion"`
	LastActiveAt         time.Time `gorm:"index" json:"last_active_at"`
	MessageCount         int       `gorm:"default:0" json:"message_count"`
	CreatedAt            time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DialogueMemory) TableName() string { return "dialogue_memories" }

// SOPAgent SOP 智能体
type SOPAgent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string         `gorm:"type:varchar(100);not null" json:"name"`
	Scenario       string         `gorm:"type:varchar(50);not null;index" json:"scenario"`
	Description    string         `gorm:"type:varchar(500)" json:"description"`
	TriggerType    string         `gorm:"type:varchar(50)" json:"trigger_type"`
	TriggerConfig  JSONMap        `gorm:"type:text" json:"trigger_config"`
	SOPGraph       JSONMap        `gorm:"type:text;not null" json:"sop_graph"`
	Version        int            `gorm:"default:1" json:"version"`
	IsActive       bool           `gorm:"default:true;index" json:"is_active"`
	Status         string         `gorm:"-" json:"status,omitempty"` // 派生字段：由 IsActive 计算，供列表/详情展示与筛选
	Priority       int            `gorm:"default:0" json:"priority"`
	ExecutionCount int            `gorm:"default:0" json:"execution_count"`
	SuccessCount   int            `gorm:"default:0" json:"success_count"`
	ABTestConfig   JSONMap        `gorm:"type:text" json:"ab_test_config"`
	UseBandit      bool           `gorm:"default:false" json:"use_bandit"`
	CreatedBy      uint           `json:"created_by"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (SOPAgent) TableName() string { return "sop_agents" }

// SOPExecution SOP 执行记录
type SOPExecution struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SOPID          uint       `gorm:"index;not null" json:"sop_id"`
	SOPName        string     `gorm:"-" json:"sop_name,omitempty"` // 派生字段：由 SOPID 关联 sop_agents 计算
	CustomerID     string     `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	SessionID      string     `gorm:"type:varchar(120)" json:"session_id"`
	CurrentNode    string     `gorm:"type:varchar(50)" json:"current_node"`
	CurrentNodeIdx int        `gorm:"default:0" json:"current_node_index"`
	Status         string     `gorm:"type:varchar(20);default:'running';index" json:"status"`
	ExecutionData  JSONMap    `gorm:"type:text" json:"execution_data"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	ErrorMessage   string     `gorm:"type:text" json:"error_message"`
	Variant        string     `gorm:"type:varchar(50);index" json:"variant"`

	LastEventAt  *time.Time `gorm:"index" json:"last_event_at"`
	AttemptCount int        `gorm:"default:0" json:"attempt_count"`
	TraceID      string     `gorm:"type:varchar(64);index" json:"trace_id"`
	WaitEvent    string     `gorm:"type:varchar(30)" json:"wait_event"`

	// ExecutedNodes 已完成节点轨迹（SAGA 补偿依据，元素结构同 service.CompensationRecord）
	// NULL/空 视为无补偿历史（向后兼容存量数据）
	ExecutedNodes JSONArray `gorm:"type:text" json:"executed_nodes"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (SOPExecution) TableName() string { return "sop_executions" }

// ReachPipeline 触达 Pipeline
type ReachPipeline struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"type:varchar(100);not null" json:"name"`
	Description  string    `gorm:"type:varchar(500)" json:"description"`
	Channel      string    `gorm:"type:varchar(30);not null;index" json:"channel"`
	Steps        JSONArray `gorm:"type:text;not null" json:"steps"`
	RetryPolicy  JSONMap   `gorm:"type:text" json:"retry_policy"`
	RateLimit    JSONMap   `gorm:"type:text" json:"rate_limit"`
	Status       string    `gorm:"type:varchar(20);default:'active';index" json:"status"`
	Version      int       `gorm:"default:1" json:"version"`
	TotalRuns    int64     `gorm:"default:0" json:"total_runs"`
	TotalSuccess int64     `gorm:"default:0" json:"total_success"`
	TotalFailure int64     `gorm:"default:0" json:"total_failure"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ReachPipeline) TableName() string { return "reach_pipelines" }

// ReachJob 触达任务
type ReachJob struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	PipelineID   uint       `gorm:"index;not null" json:"pipeline_id"`
	Channel      string     `gorm:"type:varchar(30);index" json:"channel"`
	CustomerID   string     `gorm:"type:varchar(64);index" json:"customer_id"`
	AccountID    string     `gorm:"type:varchar(100);index" json:"account_id"`
	Payload      JSONMap    `gorm:"type:text;not null" json:"payload"`
	State        string     `gorm:"type:varchar(20);default:'pending';index" json:"state"`
	CurrentStep  int        `gorm:"default:0" json:"current_step"`
	StepResults  JSONArray  `gorm:"type:text" json:"step_results"`
	RetryCount   int        `gorm:"default:0" json:"retry_count"`
	MaxRetry     int        `gorm:"default:3" json:"max_retry"`
	NextRunAt    *time.Time `gorm:"index" json:"next_run_at"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	DurationMs   int        `gorm:"default:0" json:"duration_ms"`
	CreatedAt    time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ReachJob) TableName() string { return "reach_jobs" }

// WeComAccountHealth 企微账号健康度记录
type WeComAccountHealth struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID      uint      `gorm:"index;not null" json:"account_id"`
	Platform       string    `gorm:"type:varchar(30);default:'wecom'" json:"platform"`
	HealthScore    int       `gorm:"type:int;default:0" json:"health_score"`
	RiskLevel      string    `gorm:"type:varchar(20);default:'normal'" json:"risk_level"`
	LoginState     string    `gorm:"type:varchar(20)" json:"login_state"`
	QuotaUsed      int       `gorm:"default:0" json:"quota_used"`
	QuotaTotal     int       `gorm:"default:0" json:"quota_total"`
	QuotaUsageRate float64   `gorm:"type:decimal(5,2);default:0" json:"quota_usage_rate"`
	SuccessRate    float64   `gorm:"type:decimal(5,2);default:100" json:"success_rate"`
	ErrorCount     int       `gorm:"default:0" json:"error_count"`
	LastError      string    `gorm:"type:varchar(500)" json:"last_error"`
	Metrics        JSONMap   `gorm:"type:text" json:"metrics"`
	ReportedAt     time.Time `gorm:"index" json:"reported_at"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (WeComAccountHealth) TableName() string { return "wecom_account_health" }

// SalesIntentScore 销售意向打分
type SalesIntentScore struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID        string     `gorm:"type:varchar(64);not null" json:"customer_id"`
	TotalScore        float64    `gorm:"type:decimal(5,2);not null" json:"total_score"`
	IntentLevel       string     `gorm:"type:varchar(20);not null;index" json:"intent_level"`
	Dimensions        JSONMap    `gorm:"type:text" json:"dimensions"`
	BehaviorScore     float64    `gorm:"type:decimal(5,2)" json:"behavior_score"`
	ContentScore      float64    `gorm:"type:decimal(5,2)" json:"content_score"`
	FrequencyScore    float64    `gorm:"type:decimal(5,2)" json:"frequency_score"`
	ProfileScore      float64    `gorm:"type:decimal(5,2)" json:"profile_score"`
	LastMessageAt     *time.Time `json:"last_message_at"`
	LastIntentType    string     `gorm:"type:varchar(50)" json:"last_intent_type"`
	LastScoreChange   float64    `gorm:"type:decimal(5,2)" json:"last_score_change"`
	RecommendedAction string     `gorm:"type:varchar(200)" json:"recommended_action"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (SalesIntentScore) TableName() string { return "sales_intent_scores" }

// ScriptLibrary 销冠话术库
type ScriptLibrary struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Category       string     `gorm:"type:varchar(50);not null;index" json:"category"`
	Subcategory    string     `gorm:"type:varchar(50)" json:"subcategory"`
	Title          string     `gorm:"type:varchar(200);not null" json:"title"`
	Content        string     `gorm:"type:text;not null" json:"content"`
	Scenario       string     `gorm:"type:varchar(100)" json:"scenario"`
	Tags           JSONArray  `gorm:"type:text" json:"tags"`
	UsageCount     int        `gorm:"default:0" json:"usage_count"`
	SuccessCount   int        `gorm:"default:0" json:"success_count"`
	ConversionRate float64    `gorm:"type:decimal(5,2);default:0" json:"conversion_rate"`
	IsFeatured     bool       `gorm:"default:false;index" json:"is_featured"`
	Version        int        `gorm:"default:1" json:"version"`
	Status         string     `gorm:"type:varchar(20);default:'active';index" json:"status"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedBy      uint       `json:"created_by"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ScriptLibrary) TableName() string { return "script_library" }

// ScriptVersion 话术版本历史（T-6，Langfuse 整数版本模式：历史不可变）
type ScriptVersion struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ScriptID  uint      `gorm:"not null;uniqueIndex:idx_script_version,priority:1" json:"script_id"`
	Version   int       `gorm:"not null;uniqueIndex:idx_script_version,priority:2" json:"version"`
	Title     string    `gorm:"type:varchar(200)" json:"title"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Status    string    `gorm:"type:varchar(20);default:'archived'" json:"status"`
	Note      string    `gorm:"type:varchar(500)" json:"note"`
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ScriptVersion) TableName() string { return "script_versions" }

// ScriptExposureLog 话术 AB 曝光日志（T-7：分桶+曝光+归因窗转化回写）
type ScriptExposureLog struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID         uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ScriptID   uint   `gorm:"not null;index:idx_exposure_script_ver,priority:1" json:"script_id"`
	Version    int    `gorm:"not null;default:1;index:idx_exposure_script_ver,priority:2" json:"version"`
	Bucket     string `gorm:"type:varchar(8);not null" json:"bucket"`
	CustomerID string `gorm:"column:customer_id;type:varchar(64);index" json:"customer_id"`
	// 与 customers.unified_id 对齐为 varchar(128)：手机号型 OneID 长 70 字符
	// （"phone:" + sha256 hex 64 位），varchar(64) 会写入溢出。
	OneID          string     `gorm:"type:varchar(128);index" json:"one_id"`
	ConversationID string     `gorm:"type:varchar(64);index" json:"conversation_id"`
	TraceID        string     `gorm:"type:varchar(64)" json:"trace_id"`
	ExposedAt      time.Time  `gorm:"index" json:"exposed_at"`
	Converted      bool       `gorm:"default:false" json:"converted"`
	ConvertedAt    *time.Time `json:"converted_at,omitempty"`
	Outcome        string     `gorm:"type:varchar(30)" json:"outcome"`
}

func (ScriptExposureLog) TableName() string { return "script_exposure_logs" }

// ObjectionTemplate 异议处理模板
type ObjectionTemplate struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ObjectionType    string    `gorm:"type:varchar(50);not null;index" json:"objection_type"`
	ObjectionKeyword string    `gorm:"type:varchar(200);not null" json:"objection_keyword"`
	ObjectionPattern string    `gorm:"type:varchar(500)" json:"objection_pattern"`
	ReplyTemplate    string    `gorm:"type:text;not null" json:"reply_template"`
	ReplyStrategy    string    `gorm:"type:varchar(200)" json:"reply_strategy"`
	ExampleReply     string    `gorm:"type:text" json:"example_reply"`
	UseCount         int       `gorm:"default:0" json:"use_count"`
	SuccessCount     int       `gorm:"default:0" json:"success_count"`
	IsActive         bool      `gorm:"default:true;index" json:"is_active"`
	Priority         int       `gorm:"default:0" json:"priority"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ObjectionTemplate) TableName() string { return "objection_templates" }

// ConversionFunnel 转化漏斗 —— ⚠️ **僵尸表：只有演示数据在写，全仓没有读路径**（R-4）。
//
// 实测口径：对 `model.ConversionFunnel` 的非测试引用只有 `cmd/seed/seed_stats.go`
// 一处（往里插 35 行假数据）；`GET /api/conversion-funnel` 走的是
// `internal/ops/service.ConversionFunnelService.BuildFunnel`，它对 customer_events / clues /
// intent_records / customer_sessions **实时聚合**，从不读本表。两边连阶段名都不成套：
// 演示侧是 exposure/click/consult/add_wecom/deal，真实侧是 visit/clue/intent/session
// （词表在 `internal/ops/repository.FunnelStageKey`）。因此谁按这张表算指标，得到的是一套
// 产品里并不存在的漏斗。
//
// 为什么不删（"未证伪不删"）：本表仍在 `internal/pkg/db/migrate.go allModels()` 里登记，
// 生产库可能已有存量行；删表要先证伪"仓外没有读取方"（BI/报表脚本不在本仓，本地看不到）。
// 三条同时成立时另立卡删除：① 仓外读取方确认为零；② 表内行只来自 seed；③ 已决定归档方式。
//
// **LTC 报价/商机漏斗（T-P4、T-P7-02）不得写入本表**，阶段维度走
// `ops/repository.FunnelStageKey`（`opportunity` 已作预留位定名、尚未产出）。
//
// 字段侧两条口径：`Stage` 存的是 seed 自造的演示字符串，不受上面那份词表约束；
// `Extra` 里固定带 `seed` 与 `demo_only=true` —— 清库按前者匹配，读的人按后者辨真伪。
type ConversionFunnel struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	StatDate       string    `gorm:"type:varchar(20);not null;index" json:"stat_date"`
	FunnelType     string    `gorm:"type:varchar(50);not null;index" json:"funnel_type"`
	Stage          string    `gorm:"type:varchar(50);not null" json:"stage"`
	StageOrder     int       `gorm:"not null" json:"stage_order"`
	Count          int       `gorm:"default:0" json:"count"`
	ConversionRate float64   `gorm:"type:decimal(5,2);default:0" json:"conversion_rate"`
	DropOffRate    float64   `gorm:"type:decimal(5,2);default:0" json:"drop_off_rate"`
	AvgDurationSec int       `gorm:"default:0" json:"avg_duration_seconds"`
	Extra          JSONMap   `gorm:"type:text" json:"extra"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ConversionFunnel) TableName() string { return "conversion_funnels" }

// SalesPersona 销冠能力画像
type SalesPersona struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                 uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SalesID            string     `gorm:"type:varchar(64);not null" json:"sales_id"`
	SalesName          string     `gorm:"type:varchar(100)" json:"sales_name"`
	Avatar             string     `gorm:"type:varchar(500)" json:"avatar"`
	TotalCustomers     int        `gorm:"default:0" json:"total_customers"`
	ActiveCustomers    int        `gorm:"default:0" json:"active_customers"`
	ConvertedCustomers int        `gorm:"default:0" json:"converted_customers"`
	ConversionRate     float64    `gorm:"type:decimal(5,2);default:0" json:"conversion_rate"`
	AvgResponseSec     int        `gorm:"default:0" json:"avg_response_seconds"`
	AvgDealAmount      int64      `gorm:"type:bigint;default:0" json:"avg_deal_amount"`
	TotalRevenue       int64      `gorm:"type:bigint;default:0" json:"total_revenue"`
	SkillTags          JSONArray  `gorm:"type:text" json:"skill_tags"`
	BestScenarios      JSONArray  `gorm:"type:text" json:"best_scenarios"`
	WorkDays           int        `gorm:"default:0" json:"work_days"`
	LastActiveAt       *time.Time `json:"last_active_at"`
	Level              string     `gorm:"type:varchar(20);index" json:"level"`
	LevelScore         float64    `gorm:"type:decimal(5,2);default:0" json:"level_score"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SalesPersona) TableName() string { return "sales_personas" }

// InboxConversation 统一收件箱会话
type InboxConversation struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                 uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform           string         `gorm:"type:varchar(30);not null;uniqueIndex:uk_inbox_conv_channel;index" json:"platform"`
	AccountID          string         `gorm:"type:varchar(100);not null;uniqueIndex:uk_inbox_conv_channel;index" json:"account_id"`
	CustomerID         string         `gorm:"type:varchar(64);not null;uniqueIndex:uk_inbox_conv_channel;index" json:"customer_id"`
	CustomerName       string         `gorm:"type:varchar(200)" json:"customer_name"`
	ConversationID     string         `gorm:"type:varchar(100);index" json:"conversation_id"`
	Status             string         `gorm:"type:varchar(20);default:'unread';index" json:"status"`
	AssignedTo         string         `gorm:"type:varchar(64);index" json:"assigned_to"`
	AssignedToSOP      uint           `gorm:"index" json:"assigned_to_sop"`
	AssignedAt         *time.Time     `json:"assigned_at"`
	UnreadCount        int            `gorm:"default:0" json:"unread_count"`
	TotalCount         int            `gorm:"default:0" json:"total_count"`
	LastMessageID      uint           `json:"last_message_id"`
	LastMessagePreview string         `gorm:"type:varchar(500)" json:"last_message_preview"`
	LastMessageAt      *time.Time     `gorm:"index" json:"last_message_at"`
	LastMessageFrom    string         `gorm:"type:varchar(20)" json:"last_message_from"`
	Pinned             bool           `gorm:"default:false;index" json:"pinned"`
	Starred            bool           `gorm:"default:false" json:"starred"`
	Muted              bool           `gorm:"default:false" json:"muted"`
	Tags               JSONArray      `gorm:"type:text" json:"tags"`
	Extra              JSONMap        `gorm:"type:text" json:"extra"`
	ClosedAt           *time.Time     `json:"closed_at"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (InboxConversation) TableName() string { return "inbox_conversations" }

// InboxAssignment 统一收件箱分配历史
type InboxAssignment struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ConversationID uint      `gorm:"index;not null" json:"conversation_id"`
	Platform       string    `gorm:"type:varchar(30);json" json:"platform"`
	AccountID      string    `gorm:"type:varchar(100)" json:"account_id"`
	CustomerID     string    `gorm:"type:varchar(64)" json:"customer_id"`
	Action         string    `gorm:"type:varchar(20);not null" json:"action"`
	FromType       string    `gorm:"type:varchar(20)" json:"from_type"`
	FromUserID     string    `gorm:"type:varchar(64)" json:"from_user_id"`
	ToType         string    `gorm:"type:varchar(20);index" json:"to_type"`
	ToUserID       string    `gorm:"type:varchar(64);index" json:"to_user_id"`
	ToSOPID        uint      `gorm:"index" json:"to_sop_id"`
	OperatorID     string    `gorm:"type:varchar(64);index" json:"operator_id"`
	Remark         string    `gorm:"type:varchar(500)" json:"remark"`
	CreatedAt      time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (InboxAssignment) TableName() string { return "inbox_assignments" }

// JSONMap 通用 JSON 字段类型
type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = JSONMap{}
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data = []byte("{}")
	}
	if len(data) == 0 {
		*j = JSONMap{}
		return nil
	}
	return json.Unmarshal(data, j)
}

// JSONArray 通用 JSON 数组字段
type JSONArray []any

func (j JSONArray) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	return json.Marshal(j)
}

func (j *JSONArray) Scan(value any) error {
	if value == nil {
		*j = JSONArray{}
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data = []byte("[]")
	}
	if len(data) == 0 {
		*j = JSONArray{}
		return nil
	}
	return json.Unmarshal(data, j)
}
