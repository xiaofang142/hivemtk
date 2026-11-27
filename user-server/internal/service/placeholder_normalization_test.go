package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

func TestPlaceholderMsgTypeRecognizesOnlyExactPlaceholders(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"[图片]", model.MsgTypeImage},
		{" [动画表情] ", model.MsgTypeImage},
		{"[视频]", model.MsgTypeVideo},
		{"[语音]", model.MsgTypeAudio},
		{"[文件]", model.MsgTypeFile},
		{"[链接]", model.MsgTypeLink},
		{"[卡片]", model.MsgTypeCard},
		{"[位置]", model.MsgTypeLocation},
		{"[撤回]", ""}, // 系统事件不在中台词表，保持 text
		{"我刚发了一张[图片]，请查收", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := PlaceholderMsgType(c.content); got != c.want {
			t.Errorf("PlaceholderMsgType(%q)=%q want %q", c.content, got, c.want)
		}
	}
}

func TestNormalizeEventRewritesPlaceholderMsgType(t *testing.T) {
	svc := &InboxIngressService{}
	cases := []struct {
		content  string
		msgType  string
		wantType string
	}{
		{"[图片]", model.MsgTypeText, model.MsgTypeImage},
		{"[表情]", "", model.MsgTypeImage},
		{"[视频]", model.MsgTypeText, model.MsgTypeVideo},
		{"[撤回]", model.MsgTypeText, model.MsgTypeText},
		{"我要约个时间", model.MsgTypeText, model.MsgTypeText},
	}
	for _, c := range cases {
		ev := &model.MessageEvent{
			Channel:  "personal_wx",
			SenderID: "u1",
			Content:  c.content,
			MsgType:  c.msgType,
			Extra:    map[string]any{},
		}
		if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
			t.Fatalf("NormalizeEvent(%q): %v", c.content, err)
		}
		if ev.MsgType != c.wantType {
			t.Errorf("NormalizeEvent(%q).MsgType=%q want %q", c.content, ev.MsgType, c.wantType)
		}
		if c.wantType != model.MsgTypeText {
			if ev.Extra["placeholder_normalized"] != c.wantType {
				t.Errorf("placeholder_normalized missing for %q: %v", c.content, ev.Extra)
			}
			if ev.Extra["source_placeholder"] != c.content {
				t.Errorf("source_placeholder lost for %q: %v", c.content, ev.Extra)
			}
		}
	}
}

func TestIsNonTextPlaceholderHub(t *testing.T) {
	if IsNonTextPlaceholderHub(nil) {
		t.Fatal("nil hub must not be placeholder")
	}
	if IsNonTextPlaceholderHub(&model.MessageHub{Content: "[图片]", MsgType: model.MsgTypeImage, MediaURL: "https://x/y.png"}) {
		t.Fatal("hub with real media URL is not a placeholder")
	}
	if !IsNonTextPlaceholderHub(&model.MessageHub{Content: "[图片]", MsgType: model.MsgTypeText}) {
		t.Fatal("legacy text-typed placeholder must be recognized")
	}
	if !IsNonTextPlaceholderHub(&model.MessageHub{Content: "[表情]", MsgType: model.MsgTypeImage}) {
		t.Fatal("normalized placeholder must be recognized")
	}
	if IsNonTextPlaceholderHub(&model.MessageHub{Content: "我想约时间", MsgType: model.MsgTypeText}) {
		t.Fatal("real text must not be treated as placeholder")
	}
}
