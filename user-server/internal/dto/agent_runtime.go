package dto

import agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"

// AgentInferenceRunRequest 推理闭环入口参数。
type AgentInferenceRunRequest struct {
	MessageID   string                      `json:"message_id"`
	AccountID   string                      `json:"account_id"`
	Mode        agent_runtime.ExecutionMode `json:"mode" binding:"omitempty,oneof=planning execution"`
	ChannelType string                      `json:"channel_type" binding:"required"`
	CustomerID  string                      `json:"customer_id" binding:"required"`
	SessionID   string                      `json:"session_id"`
	Content     string                      `json:"content" binding:"required"`
	MessageType string                      `json:"message_type"`
	TraceID     string                      `json:"trace_id"`
}

// AgentPermissionDefaultRequest 保留 false 作为有效默认策略。
type AgentPermissionDefaultRequest struct {
	DefaultAllow bool `json:"default_allow"`
}

// AgentToolWhitelistRequest 工具白名单设置参数。
type AgentToolWhitelistRequest struct {
	Tools []string `json:"tools" binding:"required"`
}

// AgentLifecycleRunRequest 主动模式允许不提供入站消息内容。
type AgentLifecycleRunRequest struct {
	AgentID    uint   `json:"agent_id" binding:"required"`
	CustomerID string `json:"customer_id" binding:"required"`
	Content    string `json:"content"`
	SessionID  string `json:"session_id"`
	Channel    string `json:"channel"`
	OneID      string `json:"one_id"`
	TraceID    string `json:"trace_id"`
}
