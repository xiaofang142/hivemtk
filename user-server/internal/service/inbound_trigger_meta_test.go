package service

import (
	"testing"
	"time"
)

func TestTriggerInboundMetaCarriesLLMContext(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 9, 8, 7, 0, time.UTC)
	meta := &TriggerInboundMeta{}
	opts := []TriggerInboundOption{
		WithSenderName("张三"),
		WithGroup("group-1", "客户群"),
		WithSessionWebhook("https://example.invalid/hook", 1700000000),
		WithChannelMsgID("event-1"),
		WithBusLLMSessionID("llmsess-canonical"),
		WithConversationID("conv-1"),
		WithDirection("inbound"),
		WithOriginalSentAt(sentAt),
		WithPlatformMsgID("platform-msg-1"),
		WithMessageID("hub:42"),
		WithCorrelationID("corr-1"),
	}
	for _, opt := range opts {
		opt(meta)
	}
	if meta.BusLLMSessionID != "llmsess-canonical" || meta.ConversationID != "conv-1" || meta.Direction != "inbound" {
		t.Fatalf("总线会话/会话/方向丢失：%+v", meta)
	}
	if meta.PlatformMsgID != "platform-msg-1" || meta.MessageID != "hub:42" || meta.CorrelationID != "corr-1" {
		t.Fatalf("消息身份丢失：%+v", meta)
	}
	if !meta.OriginalSentAt.Equal(sentAt) {
		t.Fatalf("原始时间丢失：%+v", meta.OriginalSentAt)
	}
	if !meta.IsGroup || meta.GroupID != "group-1" || meta.SenderName != "张三" {
		t.Fatalf("旧元数据丢失：%+v", meta)
	}
}

func TestTriggerInboundMetaEffectiveSentAt(t *testing.T) {
	fallback := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	meta := &TriggerInboundMeta{}
	if got := meta.EffectiveSentAt(fallback); !got.Equal(fallback) {
		t.Fatalf("无原始时间时才能使用兜底：%v", got)
	}
	original := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	WithOriginalSentAt(original)(meta)
	if got := meta.EffectiveSentAt(fallback); !got.Equal(original) {
		t.Fatalf("原始时间不得被当前时间覆盖：%v", got)
	}
}
