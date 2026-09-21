// Package lifecycle 定义智能体的「双模式」生命周期。
//
// 设计依据：docs/architecture/adr/-agent-two-mode.md
//
// 两种模式共享同一套 Tool 工具链与 AgentContext，差异仅在"谁触发"：
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ 被动模式 PassiveAgentLifecycle                                │
//	│   触发：渠道消息/事件进入系统（用户主动发消息 / 系统事件）      │
//	│   流程：Ingest → MessageHub → 解析智能体 → Tool 链(RAG/私信/业务)│
//	│         → 生成回复 → Send 回原渠道                              │
//	│   归属：智能体 / 智能体应答主路径（对话域）                   │
//	└─────────────────────────────────────────────────────────────┘
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ 主动模式 ActiveAgentLifecycle                                 │
//	│   触发：定时任务 / 营销事件 / 运营策略                          │
//	│   流程：策略选材 → 智能体决策 → Tool 链(私信/短信/邮件/卡片)     │
//	│         → 触达用户 → 召回进入被动会话 / 归因 OneID              │
//	│   归属：营销唤起、沉睡唤醒、主动跟进（更多由人工策略编排）       │
//	└─────────────────────────────────────────────────────────────┘
//
// 说明：本文件是双模式的领域契约（命名与接口边界）；两个实现都在 active.go：
// 被动复用既有会话引擎，主动**只编排 SOP**（C7 裁定 —— 不新建自由 Agent 循环）。
// 因此本包不许 import 会话引擎所在的包，由
// TestActiveNeverImportsConversationEngine 静态钉住；装配与按模式分派在 internal/app。
package lifecycle

import (
	"context"

	"hivemtk-user/internal/dto"
)

// AgentLifecycle 智能体生命周期统一接口。
// 被动/主动两种模式都实现本接口，由上层根据 model.AgentMode 选择。
type AgentLifecycle interface {
	Mode() string
	Run(ctx context.Context, agentCtx *dto.AgentContext, req *LifecycleRequest) (*LifecycleResult, error)
}

// LifecycleRequest 生命周期请求（被动/主动通用）。
type LifecycleRequest struct {
	Channel    string
	AccountID  string
	CustomerID string
	Content    string
	// SessionID 仅被动模式用：既有会话引擎靠它串上下文。主动模式自己生成执行会话键。
	SessionID string
	// OneID 已知的客户唯一键（可为空；主动模式会回查客户行补上，见 active.go）。
	OneID   string
	TraceID string
	Raw     map[string]any
}

// LifecycleResult 生命周期结果。
type LifecycleResult struct {
	ReplyContent string
	ToolsCalled  []string
	Handoff      bool
	StopReason   string

	// Mode 实际执行的是哪个生命周期 —— 必须回显，因为"未知模式回退 Passive"
	// 是本卡的验收条件之一，而不回显时运营分不清"它真跑了主动"与"它悄悄回退了"。
	Mode string
	// ExecutionID 主动模式产出的 SOP 执行记录 ID；被动模式恒为 0（它不建执行）。
	ExecutionID uint
	// OneID 本次运行归因到的客户唯一键，空 = 该客户行还没有 OneID。
	OneID string
}

// Resolver 按智能体模式选择生命周期实现。
// 被动模式返回 Passive；主动模式返回 Active；未知回退 Passive。
func Resolver(passive, active AgentLifecycle) func(mode string) AgentLifecycle {
	return func(mode string) AgentLifecycle {
		if mode == "active" && active != nil {
			return active
		}
		return passive
	}
}
