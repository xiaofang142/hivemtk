package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 平台公告输入卫生（2026-11-15 xapptool 会话实证）：公告不是客户发言——
// 不触发 AI、不进下行队列、IsAIReply 不误标。回复策略（是否应答）由 LLM 判断。
func TestIsPlatformSystemNotice(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"xhs 关注提示", "对方关注或回复你之前，24小时内最多只能发1条文字消息", true},
		{"xhs 聊天状态", "已连续聊天3天，获得聊天状态去看看", true},
		{"xhs 类型不支持", "暂不支持该消息类型，请到手机端查看", true},
		{"douyin 类型不支持", "暂不支持该消息类型展示 请到抖音手机端查看", true},
		{"变形空格插入", "对 方 关 注 或 回 复 你 之 前 ， 2 4 小 时 内 最 多 只 能 发 1 条", true},
		{"客户真提问", "你能介绍下你这个桥接层吗", false},
		{"客户追问", "价格呢？如何在", false},
		{"空文本", "", false},
		{"AI 回复正文", "HiveMTK 是开源的 AI 私域营销系统。核心能力：…", false},
	}
	for _, c := range cases {
		if got := IsPlatformSystemNotice(c.text); got != c.want {
			t.Errorf("%s: IsPlatformSystemNotice(%q)=%v, want %v", c.name, c.text, got, c.want)
		}
	}
}

// 公告不触发 AI（TriggerInboundAI 早退，绑定门之前）。
func TestTriggerInboundAI_PlatformNoticeSkipped(t *testing.T) {
	svc := NewWebhookService()
	// 无 orchestrator/fastpath 依赖：公告早退在一切下游之前，调用不 panic 即通过
	svc.TriggerInboundAI(context.Background(), "xiaohongshu", "acct", "conv", "cust",
		"evt-notice-1", "对方关注或回复你之前，24小时内最多只能发1条文字消息")
}

// 批次路径 interceptInbound 同样拦公告（留痕不触发）。
func TestInterceptInbound_PlatformNoticeBlocked(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	svc := NewInboxIngressService()
	svc.hubRepo = repository.NewMessageHubRepository()
	ev := &model.MessageEvent{
		EventID: "evt-notice-2", Channel: "xiaohongshu", SessionID: "xiaohongshu:cust",
		ConversationID: "conv-notice", SenderID: "cust", Content: "已连续聊天3天，获得聊天状态去看看",
	}
	d, err := svc.interceptInbound(context.Background(), ev)
	if err != nil {
		t.Fatalf("interceptInbound err: %v", err)
	}
	if !d.Blocked {
		t.Fatalf("平台公告应被输入卫生闸拦截")
	}
	if d.IsSelfEcho {
		t.Fatalf("公告不是自回声——必须留痕（IsSelfEcho=false 保证 persistMessage 落库）")
	}
}
