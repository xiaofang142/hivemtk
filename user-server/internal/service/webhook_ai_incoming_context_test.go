package service

import (
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

func TestNewInboundHubMessagePreservesAuthoritativeMetadata(t *testing.T) {
	sent := time.Date(2026, 11, 27, 10, 11, 12, 345000000, time.UTC)
	meta := &TriggerInboundMeta{
		SenderName:              "张三",
		IsGroup:                 true,
		GroupID:                 "group-1",
		GroupName:               "客户群",
		SessionWebhook:          "https://example.com/hook",
		SessionWebhookExpiredAt: 123,
		ChannelMsgID:            "qq-1",
		BusLLMSessionID:         "llmsess_bus_1",
		ConversationID:          "conv-authoritative",
		MessageID:               "evt-authoritative",
		Direction:               "inbound",
		PlatformMsgID:           "platform-1",
		CorrelationID:           "corr-1",
		OriginalSentAt:          sent,
	}

	got := newInboundHubMessage("telegram", "acc-1", "conv-fallback", "u1", "你好", "evt-fallback", meta)

	if got.SentAt != sent {
		t.Fatalf("原始时间不得被处理时刻覆盖: got %v want %v", got.SentAt, sent)
	}
	if got.ConversationID != "conv-authoritative" {
		t.Fatalf("应以权威 conversation_id 为准, got %q", got.ConversationID)
	}
	if got.MsgID != "evt-authoritative" {
		t.Fatalf("应以权威 message_id 为准, got %q", got.MsgID)
	}
	if got.Direction != "inbound" {
		t.Fatalf("方向应保留, got %q", got.Direction)
	}
	if got.PlatformMsgID != "platform-1" {
		t.Fatalf("平台消息号应保留, got %q", got.PlatformMsgID)
	}
	if got.Extra["llm_session_id"] != "llmsess_bus_1" {
		t.Fatalf("总线会话键应写入 Extra, got %v", got.Extra)
	}
	if got.Extra["correlation_id"] != "corr-1" {
		t.Fatalf("correlation 应写入 Extra, got %v", got.Extra)
	}
	if got.Extra["session_webhook"] != meta.SessionWebhook {
		t.Fatalf("钉钉 session webhook 应保留, got %v", got.Extra)
	}
	if got.Extra["channel_msg_id"] != "qq-1" {
		t.Fatalf("渠道消息号应保留, got %v", got.Extra)
	}
	if !got.IsGroup || got.GroupID != "group-1" || got.Extra["group_name"] != "客户群" {
		t.Fatalf("群聊元数据应保留, got is_group=%v group_id=%q extra=%v", got.IsGroup, got.GroupID, got.Extra)
	}
	if got.Visibility != model.VisibilityMember {
		t.Fatalf("群入站必须带成员级可见性证明, got %q", got.Visibility)
	}
	if len(got.AudienceJSON) != 1 || got.AudienceJSON[0] != "group-1" {
		t.Fatalf("群入站 audience 必须包含 group_id, got %v", got.AudienceJSON)
	}
	if got.VisibilityEvidence["source"] != "channel_ingress" {
		t.Fatalf("可见性证据应记录来源, got %v", got.VisibilityEvidence)
	}
}

func TestNewInboundHubMessageFallbackWithoutMeta(t *testing.T) {
	before := time.Now()
	got := newInboundHubMessage("telegram", "acc-1", "conv-9", "u1", "你好", "evt-9", nil)
	after := time.Now()

	if got.SentAt.Before(before) || got.SentAt.After(after) {
		t.Fatalf("缺少权威时间时才允许兜底为处理时刻, got %v", got.SentAt)
	}
	if got.ConversationID != "conv-9" || got.MsgID != "evt-9" || got.Direction != "inbound" {
		t.Fatalf("缺少 meta 时应使用调用方参数, got conv=%q msg=%q dir=%q", got.ConversationID, got.MsgID, got.Direction)
	}
}

func TestNewIncomingContextPreservesSourceMetadata(t *testing.T) {
	hub := &model.MessageHub{
		Platform:       "telegram",
		AccountID:      "acc-1",
		MsgID:          "evt-1",
		ConversationID: "conv-1",
		SenderID:       "u1",
		SenderName:     "张三",
		Direction:      "inbound",
		IsGroup:        true,
		GroupID:        "group-1",
		Visibility:     model.VisibilityMember,
		AudienceJSON:   model.JSONStrings{"group-1"},
		PlatformMsgID:  "platform-1",
		SentAt:         time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC),
		Extra: model.JSONMap{
			"llm_session_id": "llmsess_bus_1",
			"correlation_id": "corr-1",
			"sender_name":    "张三",
			"group_name":     "客户群",
		},
	}
	p := &ParsedPayload{EventID: "evt-1", Sender: "u1", Content: "你好"}

	in := newIncomingContext(WebhookChannel("telegram"), "acc-1", p, hub)

	if in.ConversationID != "conv-1" || in.MessageID != "evt-1" || in.SenderID != "u1" {
		t.Fatalf("来源/发送者/消息标识应保留, got %+v", in)
	}
	if in.BusLLMSessionID != "llmsess_bus_1" || in.CorrelationID != "corr-1" {
		t.Fatalf("总线会话键与 correlation 应保留, got %+v", in)
	}
	if in.Direction != "inbound" || in.PlatformMsgID != "platform-1" {
		t.Fatalf("方向与平台消息号应保留, got %+v", in)
	}
	if in.OriginalSentAt != hub.SentAt {
		t.Fatalf("原始时间应保留, got %v want %v", in.OriginalSentAt, hub.SentAt)
	}
	if !in.IsGroup || in.GroupID != "group-1" || in.GroupName != "客户群" {
		t.Fatalf("群聊元数据应保留, got %+v", in)
	}
	if in.Visibility != agentcontext.VisibilityMember || len(in.Audience) != 1 || in.Audience[0] != "group-1" {
		t.Fatalf("权威可见性/audience 未映射到入站上下文: visibility=%q audience=%v", in.Visibility, in.Audience)
	}
}
