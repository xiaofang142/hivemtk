package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 002-multi-type-messages 对抗性复核（2026-10-23）：三处缺口的对应用例。
//
// 修复前三条链路的红因互不相同：
//  1. 桥端四渠道（douyin/tiktok/xhs/xianyu）解析器上报 msg_type='voice'（未归一），
//     triggerAIForEvent 直连用 event.MsgType，mediaContextTags 无 voice 键 ⇒
//     AI 上下文得到 "[voice] url" 而非 "[语音] url"；
//  2. RecheckUnrepliedAndTrigger 重建 ev 漏带 MediaURL ⇒ 补触发路径媒体消息
//     退回一行占位符、URL 丢失（FR5.3 主路径与补触发口径分叉）；
//  3. AppendMediaContextToContent("", "image", url) 产出前导 "\n" ⇒
//     纯媒体空正文的消息 prompt 顶上一个孤立换行。

// 桥端原始类型 voice 必须出 [语音]，不是 [voice]。
func TestAppendMediaContextToContent_VoiceAliasUnnormalized(t *testing.T) {
	got := AppendMediaContextToContent("[语音]", "voice", "https://f/a.mp3")
	want := "[语音]\n[语音] https://f/a.mp3"
	if got != want {
		t.Fatalf("voice 别名必须映射 [语音]: got %q want %q", got, want)
	}
	// 归一后的 audio 与 voice 必须同词（两条路径同口径）
	if AppendMediaContextToContent("[语音]", "audio", "https://f/a.mp3") != got {
		t.Fatal("voice 与 audio 的上下文词必须一致（避免主/补触发路径标签分叉）")
	}
}

// 空正文纯媒体：不带前导换行。
func TestAppendMediaContextToContent_EmptyContentNoLeadingNewline(t *testing.T) {
	got := AppendMediaContextToContent("", "image", "https://f/a.png")
	if got != "[图片] https://f/a.png" {
		t.Fatalf("空正文不得产出前导换行: %q", got)
	}
	if got == "" {
		t.Fatal("空正文纯媒体必须有上下文（否则 orchestrator 空判拦截）")
	}
}

// Recheck 补触发：媒体消息的 URL 必须随 ev 进 AI 上下文。
func TestRecheck_MediaMessageCarriesURL(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	db.Create(&model.MessageHub{
		MsgID: "in-media-1", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "image", SenderID: "cust1",
		SenderName: "客户A", Content: "[图片]",
		MediaURL:       "https://cdn.example.com/recheck.png",
		ConversationID: "conv-recheck-media",
		SentAt:         now.Add(-1 * time.Second),
	})

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressService()
	svc.cache = mc
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-recheck-media", "")

	if tr.called != 1 {
		t.Fatalf("媒体消息在窗口内应补触发 AI 1 次，实际 %d 次", tr.called)
	}
	wantSuffix := "\n[图片] https://cdn.example.com/recheck.png"
	if !hasSuffix(tr.lastContent, wantSuffix) {
		t.Fatalf("补触发 AI content 应携带媒体 URL（FR5.3），want 后缀 %q，实际 %q",
			wantSuffix, tr.lastContent)
	}
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// 桥端 voice 上报的完整链路：入站 → triggerAIForEvent → [语音] 标签（非 [voice]）。
func TestInboundVoiceMessageTriggersAIWithNormalizedTag(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	voiceEv := &model.MessageEvent{
		EventID:        "mt-voice-1",
		Channel:        "douyin",
		ConversationID: "conv-voice",
		SenderID:       "cust-voice",
		MsgType:        "voice", // 桥端解析器原样上报，未经 InboundHubMsgType 归一
		Content:        "[语音]",
		MediaURL:       "https://cdn.example.com/v.amr",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), voiceEv); err != nil {
		t.Fatalf("HandleIngressMessage(voice) error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("语音消息应触发 AI 1 次，实际 %d", tr.called)
	}
	want := "[语音]\n[语音] https://cdn.example.com/v.amr"
	if tr.lastContent != want {
		t.Fatalf("voice 未归一类型必须按 [语音] 出词: got %q want %q", tr.lastContent, want)
	}
}
