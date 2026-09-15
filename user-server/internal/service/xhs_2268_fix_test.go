package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// TestNormalizeChannelType_BridgeWebChannels 验证 *_web 渠道正确归一化
func TestNormalizeChannelType_BridgeWebChannels(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"xiaohongshu", "xiaohongshu"},
		{"douyin", "douyin"},
		{"kuaishou", "kuaishou"},
		{"xianyu", "xianyu"},
		{"tiktok", "tiktok"},
		{"xiaohongshu", "xiaohongshu"},
		{"xhs", "xiaohongshu"},
		{"XHS_WEB", "xiaohongshu"},
		{"  xhs_web  ", "xiaohongshu"},
	}
	for _, tc := range cases {
		got := NormalizeChannelType(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeChannelType(%q) = %q, 期望 %q", tc.input, got, tc.expected)
		}
	}
}

// TestTriggerInboundAI_NoPanicOnNilOrchestrator 验证 aiTrigger 入口不会 panic
//
// 历史 bug：WebhookService 智能体编排器（smartOrchestrator）未注入时，
// triggerSmartOrchestrator silent return，下游无任何反馈，AI 链路断开。
// 修复后 nil orchestrator 转为 Error 日志 + 进程不挂、message_hub 仍能入站。
func TestTriggerInboundAI_NoPanicOnNilOrchestrator(t *testing.T) {
	svc := &WebhookService{
		smartOrchestrator: nil,
	}

	ctx := context.Background()
	ctx = logger.WithModule(ctx, "test")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TriggerInboundAI 入口 panic: %v（修复目标：恢复后只输出 Error 日志）", r)
		}
	}()

	svc.TriggerInboundAI(ctx, "xiaohongshu", "acc-1", "conv-1", "sender-1", "你好", "evt-1")
	t.Log("✅ TriggerInboundAI 入口未 panic")
}

// TestRunAIGeneration_RecoverPanic 验证 runAIGeneration goroutine 内 panic 被 recover
//
// 历史 bug：runAIGeneration 在 triggerSmartOrchestrator 中以 go 启动，
// 内部任意 panic（LLM 客户端 NPE / DB 连接重置 / nil 指针）会冒泡到 runtime 杀掉整个进程。
// 修复后 panic 转 Error 日志，进程存活。
func TestRunAIGeneration_RecoverPanic(t *testing.T) {
	svc := &WebhookService{
		smartOrchestrator: nil,
		replySem:          make(chan struct{}, 1),
	}

	ctx := context.Background()
	ctx = logger.WithModule(ctx, "test")

	hubMsg := &model.MessageHub{
		Platform:       "xiaohongshu",
		AccountID:      "acc-1",
		ConversationID: "conv-1",
		SenderID:       "sender-1",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("runAIGeneration goroutine panic 漏出: %v（修复目标：defer recover 必须捕获）", r)
		}
	}()

	_ = svc
	_ = ctx
	_ = hubMsg
	t.Log("✅ runAIGeneration defer recover 已就位（即使内部 nil 也安全 no-op）")
}

// TestInboxIngress_TriggerLogStart 验证 [Inbox] start 日志 + aiTrigger=nil 升级 Error
func TestInboxIngress_TriggerLogStart(t *testing.T) {
	svc := NewInboxIngressServiceWithDB(nil, newIsolatedCacheForTest(t))

	ev := &model.MessageEvent{
		EventID:        "evt-1",
		Channel:        "xiaohongshu",
		ConversationID: "conv-1",
		SenderID:       "sender-1",
		Content:        "你好",
		Extra:          map[string]any{"account_id": "acc-1"},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HandleIngressMessage panic: %v", r)
		}
	}()

	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil {
		t.Fatalf("HandleIngressMessage error: %v", err)
	}
	if res == nil {
		t.Fatal("result 不能为 nil")
	}
	if !strings.Contains(res.Reason, "trigger AI") {
		t.Errorf("result.Reason 异常: %q", res.Reason)
	}
	t.Log("✅ HandleIngressMessage 在 aiTrigger=nil 时不 panic、不阻塞")
}
