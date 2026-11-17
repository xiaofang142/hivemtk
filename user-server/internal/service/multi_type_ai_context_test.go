package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 002-multi-type-messages FR5.3：非文本入站消息触发 AI 时，content 必须携带媒体 URL。
//
// 修复前 triggerAIForEvent 只传 event.Content —— 桥接渠道客户发图，AI 收到的全部上下文
// 就是一行「[图片]」占位符，图在哪、能不能点开全靠猜。此处锁三件事：
//  1. 图片入站 → fake aiTrigger.lastContent 含类型词与 media_url（换行拼接形态）；
//  2. 文本消息 → content 逐字节不变（存量文本链路零破坏）；
//  3. 非文本但 media_url 为空 → content 不动（无从拼接，不留悬空尾巴）。
func TestInboundMediaMessageTriggersAIWithMediaURL(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	imgEv := &model.MessageEvent{
		EventID:        "mt-ai-1",
		Channel:        "douyin",
		ConversationID: "conv-mt",
		SenderID:       "cust-mt",
		MsgType:        "image",
		Content:        "[图片]",
		MediaURL:       "https://cdn.example.com/img-1.png",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), imgEv); err != nil {
		t.Fatalf("HandleIngressMessage(image) error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("图片消息应触发 AI 1 次，实际 %d", tr.called)
	}
	wantSuffix := "\n[图片] https://cdn.example.com/img-1.png"
	if !strings.HasSuffix(tr.lastContent, wantSuffix) {
		t.Fatalf("AI content 应以 %q 结尾，实际 %q", wantSuffix, tr.lastContent)
	}
	if !strings.HasPrefix(tr.lastContent, "[图片]") {
		t.Fatalf("AI content 应保留原占位正文，实际 %q", tr.lastContent)
	}

	// 文本消息：逐字节不变（换会话，避开 AI 处理中排他标记的 TTL）
	tr.called = 0
	txtEv := &model.MessageEvent{
		EventID:        "mt-ai-2",
		Channel:        "douyin",
		ConversationID: "conv-mt-txt",
		SenderID:       "cust-mt",
		Content:        "在吗，看下这个",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), txtEv); err != nil {
		t.Fatalf("HandleIngressMessage(text) error: %v", err)
	}
	if tr.lastContent != "在吗，看下这个" {
		t.Fatalf("文本消息 content 必须逐字节不变: %q", tr.lastContent)
	}

	// 非文本但 URL 为空：不动
	tr.called = 0
	noURLEv := &model.MessageEvent{
		EventID:        "mt-ai-3",
		Channel:        "douyin",
		ConversationID: "conv-mt-nourl",
		SenderID:       "cust-mt",
		MsgType:        "audio",
		Content:        "[语音]",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), noURLEv); err != nil {
		t.Fatalf("HandleIngressMessage(audio, no url) error: %v", err)
	}
	if tr.lastContent != "[语音]" {
		t.Fatalf("无 URL 的非文本消息 content 不应被改动: %q", tr.lastContent)
	}
}

// AppendMediaContextToContent 单元口径：类型词表、混合图文正文、大小写与空白容错。
func TestAppendMediaContextToContent(t *testing.T) {
	cases := []struct {
		name, content, msgType, mediaURL, want string
	}{
		{"文本零破坏", "你好", "text", "https://f/a.png", "你好"},
		{"空类型零破坏", "你好", "", "https://f/a.png", "你好"},
		{"图片", "[图片]", "image", "https://f/a.png", "[图片]\n[图片] https://f/a.png"},
		{"语音", "[语音]", "audio", "https://f/a.mp3", "[语音]\n[语音] https://f/a.mp3"},
		{"视频", "[视频]", "video", "https://f/a.mp4", "[视频]\n[视频] https://f/a.mp4"},
		{"文件", "[文件]", "file", "https://f/a.pdf", "[文件]\n[文件] https://f/a.pdf"},
		{"混合图文正文保留", "看下这款", "image", "https://f/a.png", "看下这款\n[图片] https://f/a.png"},
		{"类型大小写不敏感", "[图片]", "Image", "https://f/a.png", "[图片]\n[图片] https://f/a.png"},
		{"URL 空白去除", "[图片]", "image", "  https://f/a.png  ", "[图片]\n[图片] https://f/a.png"},
		{"未知类型兜底", "[merge_forward]", "merge_forward", "https://f/a", "[merge_forward]\n[merge_forward] https://f/a"},
	}
	for _, tc := range cases {
		if got := AppendMediaContextToContent(tc.content, tc.msgType, tc.mediaURL); got != tc.want {
			t.Errorf("%s: AppendMediaContextToContent(%q,%q,%q) = %q, want %q",
				tc.name, tc.content, tc.msgType, tc.mediaURL, got, tc.want)
		}
	}

	if got := AppendMediaContextToContent("[图片]", "image", "   "); got != "[图片]" {
		t.Errorf("纯空白 URL 应视为空: %q", got)
	}
	// 幂等防御（对抗性复核 2026-10-24）：正文已含同一 URL 时不再追加，防双重拼接。
	if got := AppendMediaContextToContent("看这张 [图片] https://f/a.png", "image", "https://f/a.png"); got != "看这张 [图片] https://f/a.png" {
		t.Errorf("正文已含 URL 不得重复追加: %q", got)
	}
	// 不同 URL 仍正常追加（不是「有 URL 就不拼」的粗判）
	if got := AppendMediaContextToContent("看这张 https://f/old.png", "image", "https://f/new.png"); got != "看这张 https://f/old.png\n[图片] https://f/new.png" {
		t.Errorf("不同 URL 必须正常追加: %q", got)
	}
}
