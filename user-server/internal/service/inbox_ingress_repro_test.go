package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func TestReproduce_XhsWeb_NoAITrigger(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	ev := &model.MessageEvent{
		EventID:        "69c62722000000003203b0fe:你好:2",
		Channel:        "xiaohongshu",
		ConversationID: "69c62722000000003203b0fe",
		SenderID:       "69c62722000000003203b0fe",
		SenderName:     "小红薯69C69EDE",
		Content:        "你好",
		MsgType:        "text",
		Timestamp:      time.Now(),
		Extra: map[string]any{
			"account_id":  "69c730300000000034018cb2",
			"bridge":      true,
			"sender_type": "customer",
		},
	}
	if _, err := svc.HandleIngressMessage(context.Background(), ev); err != nil {
		t.Fatalf("HandleIngressMessage error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("期望 AI 触发 1 次，实际 %d。问题：xhs_web 渠道特定消息未触发 AI 编排器", tr.called)
	}
	t.Logf("✅ AI trigger 被正确调用: channel=%s accountID=%s", tr.lastChannel, tr.lastAccount)
}
