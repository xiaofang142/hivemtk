package service

import (
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestSalesRequestFromIncomingContextPreservesSource(t *testing.T) {
	sent := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	view := &agentcontext.AgentConversationContext{SnapshotID: "snapshot-1"}
	in := &IncomingContext{
		Platform:        model.Platform("telegram"),
		AccountID:       "acc-1",
		SenderID:        "u1",
		SenderName:      "张三",
		Content:         "你好",
		MessageID:       "evt-1",
		ConversationID:  "conv-1",
		BusSessionID:    "channel:acc-1:conv-1",
		BusLLMSessionID: "llmsess_bus_1",
		Direction:       "inbound",
		OriginalSentAt:  sent,
		PlatformMsgID:   "platform-1",
		CorrelationID:   "corr-1",
		IsGroup:         true,
		GroupID:         "group-1",
		GroupName:       "客户群",
		OneID:           "one-1",
		ContextView:     view,
	}

	got := salesRequestFromIncomingContext("session-1", "customer-1", in)

	if got.SessionID != "session-1" || got.CustomerID != "customer-1" || got.OneID != "one-1" {
		t.Fatalf("基础字段应保留, got %+v", got)
	}
	if got.ConversationID != "conv-1" || got.MessageID != "evt-1" || got.SenderID != "u1" || got.SenderName != "张三" {
		t.Fatalf("来源/发送者/消息标识应保留, got %+v", got)
	}
	if got.BusSessionID != in.BusSessionID || got.BusLLMSessionID != in.BusLLMSessionID {
		t.Fatalf("总线会话标识应保留, got %+v", got)
	}
	if got.Direction != "inbound" || got.PlatformMsgID != "platform-1" || got.CorrelationID != "corr-1" {
		t.Fatalf("方向/回执/correlation 应保留, got %+v", got)
	}
	if got.OriginalSentAt != sent {
		t.Fatalf("原始时间应保留, got %v want %v", got.OriginalSentAt, sent)
	}
	if !got.IsGroup || got.GroupID != "group-1" || got.GroupName != "客户群" {
		t.Fatalf("群聊来源应保留, got %+v", got)
	}
	if got.ContextView != view {
		t.Fatalf("ContextView 应以同一指针透传, got %p want %p", got.ContextView, view)
	}
	if got.Platform != "telegram" || got.UserMessage != "你好" {
		t.Fatalf("平台与正文应保留, got %+v", got)
	}
}

func TestSalesRequestContextViewDefaultsNil(t *testing.T) {
	got := salesRequestFromIncomingContext("s", "c", &IncomingContext{Content: "hi"})
	if got.ContextView != nil {
		t.Fatalf("无上下文视图时应保持 nil, got %+v", got.ContextView)
	}
	var _ *dto.SalesRequest = got
}
