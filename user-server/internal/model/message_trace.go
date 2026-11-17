package model

import "time"

// MessageTrace 业务链路追踪节点（span）。
//
// 设计目标（区别于系统监控）：
//
//	围绕核心业务生命周期「消息上报 → AI 处理 → 出站入队 → 收件箱同步 → 下行出库 → 送达确认」，
//	对每一个生命周期节点（node）追加一条不可变 span，记录该节点的：
//	  - 入参 Input（请求 / 触发条件，JSON）
//	  - 出参 Output（处理结果，JSON）
//	  - 响应时间 DurationMs（本节点处理耗时，毫秒）
//	  - 预期结果 Expected（正常应得到什么）
//	  - 实际 / 异常结果 Status(ok|abnormal) + Abnormal（偏离预期的异常详情 / error）
//
// 多渠道特性：
//
//	Channel 字段承载渠道维度（xiaohongshu / douyin / xianyu / telegram / ...），
//	监控按渠道聚合「各节点响应时间、异常率」，定位某渠道特有的链路问题。
//
// 关联键：
//   - conversation_id：会话级串联（同一会话的全部节点可见，可还原单会话全链路）
//   - trace_id：兼容历史关联；部分调用按会话派生，不能作为轮次唯一键
//   - node + node_order：节点在生命周期中的固定位置，用于画链路图 + 计算节点间时延
//
// 层级（hierarchy）：一个生命周期节点（span_kind=lifecycle）下可挂载 agent 多轮（span_kind=agent_turn）
// 与多工具调用（span_kind=tool_call），通过 parent_node + turn_index 还原「一条消息 → AI 多轮 → 多工具」的
// 完整调用树。该树由工具层 observer 自动采集，对业务代码零侵入。
//   - parent_node：父节点名（lifecycle 节点为空；agent_turn 指向 ai_dispatch；tool_call 指向 ai_dispatch）
//   - span_kind：lifecycle(生命周期节点) | agent_turn(AI 一轮) | tool_call(单次工具调用)
//   - turn_index：agent 轮次序号（tool_call 继承其所属轮次，便于归并到对应 agent_turn）
//   - tool_name / agent_id：工具名 / 智能体 ID（仅 tool_call / agent_turn 填充）
//
// 写入策略：best-effort，任何失败仅告警不阻断主业务。
type MessageTrace struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID          string     `gorm:"type:varchar(64);index:idx_mt_trace" json:"trace_id"`
	ConversationID   string     `gorm:"type:varchar(100);index:idx_mt_conv" json:"conversation_id"`
	AccountID        string     `gorm:"type:varchar(100);index" json:"account_id"`
	Channel          string     `gorm:"type:varchar(30);index:idx_mt_channel" json:"channel"`
	Node             string     `gorm:"type:varchar(40);index:idx_mt_node" json:"node"`
	NodeOrder        int        `gorm:"index" json:"node_order"`
	Direction        string     `gorm:"type:varchar(10)" json:"direction"`
	MsgID            string     `gorm:"type:varchar(100);index" json:"msg_id"`
	Input            string     `gorm:"type:text" json:"input"`
	Output           string     `gorm:"type:text" json:"output"`
	DurationMs       int64      `gorm:"index:idx_mt_dur" json:"duration_ms"`
	TurnID           string     `gorm:"type:varchar(64);index:idx_mt_business_turn;default:''" json:"turn_id"`
	AnswerAttempt    int        `gorm:"not null;default:0" json:"answer_attempt"`
	DeliveryAttempt  int        `gorm:"not null;default:0" json:"delivery_attempt"`
	TimingVersion    int        `gorm:"not null;default:0" json:"timing_version"`
	DurationMeasured bool       `gorm:"not null;default:false" json:"duration_measured"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	Expected         string     `gorm:"type:text" json:"expected"`
	Status           string     `gorm:"type:varchar(20);index" json:"status"`
	Abnormal         string     `gorm:"type:text" json:"abnormal"`
	Error            string     `gorm:"type:text" json:"error"`
	ParentNode       string     `gorm:"type:varchar(40);index:idx_mt_parent;default:''" json:"parent_node"`
	SpanKind         string     `gorm:"type:varchar(20);index:idx_mt_kind;default:'lifecycle'" json:"span_kind"`
	TurnIndex        int        `gorm:"index:idx_mt_turn;default:0" json:"turn_index"`
	ToolName         string     `gorm:"type:varchar(96);default:''" json:"tool_name"`
	AgentID          string     `gorm:"type:varchar(64);default:''" json:"agent_id"`
	CreatedAt        time.Time  `gorm:"autoCreateTime;index:idx_mt_created" json:"created_at"`
}

func (MessageTrace) TableName() string { return "message_trace" }

// 层级 span 种类常量
const (
	SpanKindLifecycle = "lifecycle"
	SpanKindAgentTurn = "agent_turn"
	SpanKindToolCall  = "tool_call"
)

// 层级 span 节点名（挂在 ai_dispatch 之下）
const (
	NodeAgentTurn = "agent_turn"
	NodeToolCall  = "tool_call"
)
