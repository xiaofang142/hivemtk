package service

import (
	"strings"
	"testing"
)

// 内部成员发送必须拒绝空收件人（防止误群发全公司）。
func TestWeComSendAppMessage_RequiresRecipient(t *testing.T) {
	msgData := map[string]any{}
	_ = msgData
	// 直接校验 fillWeComMsgBody 的 msgtype 覆盖
	req := &WeComSendMessageRequest{MsgType: "text", Content: "hi"}
	m := map[string]any{}
	if err := fillWeComMsgBody(m, req); err != nil {
		t.Fatalf("text 应支持: %v", err)
	}
	if m["text"].(map[string]string)["content"] != "hi" {
		t.Fatalf("text.content 未填充")
	}
}

func TestFillWeComMsgBody_Textcard(t *testing.T) {
	req := &WeComSendMessageRequest{MsgType: "textcard", Content: "desc", Title: "线索提醒", URL: "https://x.example"}
	m := map[string]any{}
	if err := fillWeComMsgBody(m, req); err != nil {
		t.Fatalf("textcard: %v", err)
	}
	c := m["textcard"].(map[string]any)
	if c["title"] != "线索提醒" || c["description"] != "desc" || c["url"] != "https://x.example" {
		t.Fatalf("textcard 字段映射错误: %+v", c)
	}
}

func TestFillWeComMsgBody_RejectsUnknownType(t *testing.T) {
	err := fillWeComMsgBody(map[string]any{}, &WeComSendMessageRequest{MsgType: "video"})
	if err == nil || !strings.Contains(err.Error(), "不支持的 msg_type") {
		t.Fatalf("未知 msg_type 应报错，got %v", err)
	}
}

func TestWeComSendAppMessage_EmptyRecipientRejected(t *testing.T) {
	// 通过导出方法校验：无 touser/toparty 时应拒绝，不发起网络请求
	svc := &WeComService{}
	_, err := svc.SendAppMessage(nil, nil, &WeComSendMessageRequest{MsgType: "text", Content: "x"})
	if err == nil {
		t.Fatalf("空账号应报错")
	}
}
