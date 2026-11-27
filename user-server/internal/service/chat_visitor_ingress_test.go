package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// TestVisitorReportToMessageBus 闸 9（G-02）：网页客服入站必须走统一总线，
// 且要带 channel-owned AI 标记（防止访客链路与中台各回一次），
// 并保证 handler 缺失/失败时不影响访客会话（总线是增强层不是单点故障）。
func TestVisitorReportToMessageBus(t *testing.T) {
	svc := &VisitorChatService{}
	var got *model.MessageEvent
	var gotCtx context.Context
	svc.SetIngressHandler(func(ctx context.Context, event *model.MessageEvent) error {
		gotCtx, got = ctx, event
		return nil
	})
	now := time.Now()
	svc.reportToMessageBus(context.Background(),
		&VisitorSendMessageRequest{VisitorID: "v-1", Content: "你好", ChannelID: "ch-1"},
		&model.CustomerSession{SessionID: "sess-1", AccountID: "ch-1", UserName: "访客"},
		&model.SessionMessage{ID: 42, CreatedAt: now},
		model.MessageTypeText,
	)
	if got == nil {
		t.Fatal("ingress handler 未被调用：web_embed 入站仍绕开消息总线")
	}
	if got.Channel != string(model.PlatformWebEmbed) {
		t.Errorf("channel=%q，期望 %q", got.Channel, model.PlatformWebEmbed)
	}
	if got.EventID != "visitor:sess-1:42" || got.SessionID != "sess-1" || got.ConversationID != "sess-1" {
		t.Errorf("身份字段不对: event=%q session=%q conv=%q", got.EventID, got.SessionID, got.ConversationID)
	}
	if got.SenderID != "v-1" || got.Content != "你好" || got.MsgType != "text" {
		t.Errorf("载荷字段不对: sender=%q content=%q type=%q", got.SenderID, got.Content, got.MsgType)
	}
	if !channelOwnsAITrigger(gotCtx) {
		t.Error("网页客服入站未带 channel-owned AI 标记：中台会二次回复访客")
	}

	// handler 缺失时不得 panic。
	empty := &VisitorChatService{}
	empty.reportToMessageBus(context.Background(), &VisitorSendMessageRequest{}, &model.CustomerSession{}, &model.SessionMessage{}, model.MessageTypeText)
}
