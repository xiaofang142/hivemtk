package event

import "time"

// 主题定义
// 命名规范：{模块}.{动作}，如 operation.log、health.report
const (
	TopicOperationLog = "operation.log"

	TopicCustomerMessageReceived = "customer.message.received"

	TopicKnowledgeDocumentChanged = "knowledge.document.changed"

	TopicDialogueStarted = "dialogue.started"

	TopicDialogueEnded = "dialogue.ended"

	TopicAssetDegraded = "asset.degraded"

	TopicAssetDegradeWarning = "asset.degrade.warning"

	TopicRagCorpusUpdated = "rag.corpus.updated"
)

// OperationLogPayload 操作日志事件载荷
//
// 对应 model.OperationLog 字段，由 OperationLogSubscriber 转换并写入 DB
type OperationLogPayload struct {
	UserID     uint   `json:"user_id"`
	Username   string `json:"username"`
	Action     string `json:"action"`
	Module     string `json:"module"`
	Resource   string `json:"resource"`
	ResourceID string `json:"resource_id"`
	OldValue   any    `json:"old_value"`
	NewValue   any    `json:"new_value"`
	IP         string `json:"ip"`
}

// CustomerMessagePayload 客户消息事件载荷 — 新增(§2.2)
//
// 由 Channel Adapter 在 webhook 处理完成后 publish
// agent_runtime.EventSubscriber.Handle 消费
//
// 关联主题:TopicCustomerMessageReceived
type CustomerMessagePayload struct {
	ChannelType string         `json:"channel_type"`
	AccountID   string         `json:"account_id"`
	CustomerID  string         `json:"customer_id"`
	SessionID   string         `json:"session_id"`
	Content     string         `json:"content"`
	MessageType string         `json:"message_type"`
	Timestamp   time.Time      `json:"timestamp"`
	TraceID     string         `json:"trace_id"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// KnowledgeDocumentChangePayload 知识库文档变更事件载荷 — 新增(§2.5 子项 2)
//
// 关联主题:TopicKnowledgeDocumentChanged
// 触发时机:KnowledgeDocumentService.Create/Update/Delete 后 publish
type KnowledgeDocumentChangePayload struct {
	WorkspaceID string `json:"workspace_id"`
	DocumentID  uint   `json:"document_id"`
	ChangeType  string `json:"change_type"`
	ContentHash string `json:"content_hash"`
	OperatorID  uint   `json:"operator_id"`
	TraceID     string `json:"trace_id"`
}

// DialogueStartedPayload 对话开始事件载荷
//
// 关联主题:TopicDialogueStarted
// 触发时机:
//   - VisitorChatService.OpenSession 成功后（WS 连接建立）
//   - WebhookService 首条访客消息到达时
//
// 订阅者:SelfLearningOrchestrator.onDialogueStarted → RAGSelfCorrector.Warmup
type DialogueStartedPayload struct {
	SessionID    string    `json:"session_id"`
	VisitorID    string    `json:"visitor_id"`
	CustomerID   string    `json:"customer_id"`
	ChannelType  string    `json:"channel_type"`
	Scenario     string    `json:"scenario"`
	FirstMessage string    `json:"first_message"`
	StartedAt    time.Time `json:"started_at"`
	TraceID      string    `json:"trace_id"`
}

// DialogueEndedPayload 对话结束事件载荷
//
// 关联主题:TopicDialogueEnded
// 触发时机:CustomerSessionService.UpdateSessionStatus(status=closed/resolved) 成功后
//
// 订阅者:
//   - Orchestrator.onDialogueEnded
//   - → RAGSelfCorrector.Reflect（基于 reward 矫正语料质量）
//   - → AssetBundleLearner.GenerateCandidate（基于销冠对话生成资产包候选）
//   - → RAGSelfSupervisor.CollectMetrics / AssetBundleSelfSupervisor.CollectMetrics
type DialogueEndedPayload struct {
	SessionID        string         `json:"session_id"`
	VisitorID        string         `json:"visitor_id"`
	CustomerID       string         `json:"customer_id"`
	DurationSec      int64          `json:"duration_sec"`
	Outcome          string         `json:"outcome"`
	AggregatedReward float64        `json:"aggregated_reward"`
	SignalBreakdown  map[string]any `json:"signal_breakdown,omitempty"`
	UsedCorpusIDs    []string       `json:"used_corpus_ids,omitempty"`
	UsedAssetIDs     []string       `json:"used_asset_ids,omitempty"`
	LastCustomerMsg  string         `json:"last_customer_msg,omitempty"`
	LastAIReply      string         `json:"last_ai_reply,omitempty"`
	TraceID          string         `json:"trace_id"`
	EndedAt          time.Time      `json:"ended_at"`
}

// AssetDegradedPayload 资产包降级事件载荷
//
// 关联主题:TopicAssetDegraded
// 触发时机:AssetBundleLearner.DegradeInactiveAssets 检测到资产包连续 30 天 use_count=0
// 订阅者:Orchestrator.onAssetDegraded（记录日志，候选生成由 cron 触发）
type AssetDegradedPayload struct {
	AssetID      string    `json:"asset_id"`
	AssetTitle   string    `json:"asset_title"`
	Reason       string    `json:"reason"`
	LastUseCount int64     `json:"last_use_count"`
	LastRating   float64   `json:"last_rating"`
	Scenario     string    `json:"scenario"`
	TraceID      string    `json:"trace_id"`
	DegradedAt   time.Time `json:"degraded_at"`
}

// AssetDegradeWarningPayload 资产包降级预警事件载荷
//
// 关联主题:TopicAssetDegradeWarning
// 触发时机:autonomous 模式下，资产包即将降级前 24h 预警
// 订阅者:看板告警订阅
type AssetDegradeWarningPayload struct {
	AssetID      string    `json:"asset_id"`
	AssetTitle   string    `json:"asset_title"`
	Reason       string    `json:"reason"`
	LastUseCount int64     `json:"last_use_count"`
	LastRating   float64   `json:"last_rating"`
	Scenario     string    `json:"scenario"`
	TraceID      string    `json:"trace_id"`
	WarnedAt     time.Time `json:"warned_at"`
}

// RagCorpusUpdatedPayload RAG 语料变更事件载荷
//
// 关联主题:TopicRagCorpusUpdated
// 触发时机:RAGSelfCorrector 销冠补录 / 低质归档 / 降权
// 订阅者:RAG 缓存失效订阅
type RagCorpusUpdatedPayload struct {
	CorpusID        string    `json:"corpus_id"`
	Action          string    `json:"action"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	NewQualityLabel string    `json:"new_quality_label,omitempty"`
	TraceID         string    `json:"trace_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}
