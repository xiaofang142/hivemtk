// Package agent_runtime 提供 AI 智能体运行时隔离层。
//
// 设计依据：docs/architecture/adr/-agent-runtime-isolation.md
//
// 核心职责：
//  1. 加载智能体上下文（从 ai_agents 表 + channel_agent_bindings 表）
//  2. 桥接 SalesEngine / SmartCSOrchestrator（只通过 interface）
//  3. 订阅 internal/event/bus.go 的 customer.message.received 主题
//
// 依赖方向（铁律）：
//
//	agent_runtime ──→ tools (interface)        // 业务能力
//	agent_runtime ──→ event/bus                // 事件基础设施
//	agent_runtime ──→ sales_engine (interface) // AI 引擎接口
//	agent_runtime ──→ smart_cs (interface)     // 智能体接口
//
// 禁止：
//   - 直接 import internal/service 下的具体 Service 实现
//   - 直接访问 *gorm.DB
//   - 在业务 Service 内部被同步调用（只通过 Event Bus 触发）
package agent_runtime

import (
	"context"
	"sync"
	"time"
)

// AgentContext 智能体运行时上下文
//
// 从 ai_agents 表加载的完整智能体配置 + 渠道绑定关系 + 缓存
// 这是 agent_runtime 包的"领域模型"，与 model.AIAgent(数据库模型)解耦
type AgentContext struct {
	AgentID   uint
	AgentCode string
	Name      string
	AgentType string

	Persona      string
	SystemPrompt string
	Greeting     string

	RagProductIDs []string

	FAQEntryIDs    []string
	SOPTemplateIDs []string

	SOPIDs           []string
	ScriptLibraryIDs []string
	ABExperimentIDs  []string

	LLMModel         string
	Temperature      float64
	MaxTokens        int
	TopP             float64
	FrequencyPenalty float64
	PresencePenalty  float64

	EnableRAG            bool
	EnableScriptMatch    bool
	EnableHumanizePolish bool
	EnableContentAudit   bool
	EnablePlaybook       bool
	RAGTopK              int

	ConfidenceThreshold float64
	MaxAIConsecutive    int

	Version   int
	LoadedAt  time.Time
	Channel   string
	AccountID string
}

// AgentRuntime 智能体运行时统一入口
//
// 所有 AI 触发都通过本接口进入，外部不直接持有 SalesEngine / SmartCSOrchestrator
// 这是 agent_runtime 包对外暴露的唯一入口
type AgentRuntime interface {
	HandleCustomerMessage(ctx context.Context, payload CustomerMessagePayload) (*SalesResponse, error)

	LoadAgentContext(ctx context.Context, channelType, accountID string) (*AgentContext, error)

	RefreshCache(ctx context.Context, agentID uint) error

	Stop(ctx context.Context) error
}

// CustomerMessagePayload 客户消息事件载荷
//
// 由 Channel Adapter 在 webhook 处理完成后 publish 到 event bus
// agent_runtime.EventSubscriber.Handle 消费
//
// 关联主题：event.TopicCustomerMessageReceived = "customer.message.received"
type CustomerMessagePayload struct {
	ChannelType string
	AccountID   string
	CustomerID  string
	SessionID   string
	Content     string
	MessageType string
	Timestamp   time.Time
	TraceID     string

	Raw map[string]any `json:"raw,omitempty"`
}

// SalesResponse 销售智能体响应
type SalesResponse struct {
	ReplyContent string
	ReplyType    string
	Confidence   float64

	AgentID    uint
	AgentCode  string
	Channel    string
	CustomerID string
	TraceID    string

	ToolsCalled []string
	LLMModel    string
	TokensUsed  int

	HandoffToHuman bool
	StopReason     string

	Duration time.Duration
}

// EventSubscriber 事件订阅者
//
// 实现 internal/event.Handler 接口
// 订阅 customer.message.received 主题
type EventSubscriber interface {
	Handle(evt Event) error
}

// Event 内部事件类型（来自 internal/event）
//
// 这里重新声明类型签名，避免循环依赖
// 实际实现时由 internal/event 包的 Event 类型转换而来
type Event struct {
	Topic     string
	Payload   any
	Timestamp time.Time
	Source    string
}

type cacheKey struct {
	Channel   string
	AccountID string
}

func (k cacheKey) String() string {
	return k.Channel + ":" + k.AccountID
}

type defaultAgentRuntime struct {
	mu         sync.RWMutex
	cache      map[cacheKey]*cachedContext
	cacheTTL   time.Duration
	loader     AgentContextLoader
	salesSales SalesEngineBridge
	csBridge   SmartCSBridge
	stopped    bool
}

type cachedContext struct {
	ctx      *AgentContext
	cachedAt time.Time
}

// NewAgentRuntime 创建智能体运行时实例
func NewAgentRuntime(loader AgentContextLoader, sales SalesEngineBridge, cs SmartCSBridge) AgentRuntime {
	rt := &defaultAgentRuntime{
		cache:      make(map[cacheKey]*cachedContext),
		cacheTTL:   5 * time.Minute,
		loader:     loader,
		salesSales: sales,
		csBridge:   cs,
	}
	return rt
}

// AgentContextLoader 智能体上下文加载器
//
// 负责从 ai_agents + channel_agent_bindings 表加载配置
// agent_runtime 通过本接口与持久层解耦
type AgentContextLoader interface {
	LoadByChannelAccount(ctx context.Context, channelType, accountID string) (*AgentContext, error)

	Invalidate(ctx context.Context, agentID uint) error
}

// SalesEngineBridge 销售引擎桥接器
//
// agent_runtime 通过本接口调用 SalesEngine，不直接持有具体类型
// 这是解耦的关键：agent_runtime 不 import internal/service/sales_engine.go
type SalesEngineBridge interface {
	HandleWithAgent(ctx context.Context, agentCtx *AgentContext, req *SalesRequest) (*SalesResponse, error)
}

// SalesRequest 销售请求（agent_runtime → SalesEngine 桥接）
type SalesRequest struct {
	Channel    string
	AccountID  string
	CustomerID string
	Content    string
	TraceID    string
	Raw        map[string]any
}

// ErrBridgeNotInitialized 桥接器未初始化
var ErrBridgeNotInitialized = &RuntimeError{Code: "bridge_nil", Message: "engine bridge not initialized"}

// SmartCSBridge 智能体桥接器
type SmartCSBridge interface {
	HandleIncomingWithAgent(ctx context.Context, agentCtx *AgentContext, req *SalesRequest) (*SalesResponse, error)
}
