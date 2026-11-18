package onebot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
)

func TestParseEventMessageAndNotice(t *testing.T) {
	raw := []byte(`{"time":1700000000,"self_id":10001,"post_type":"message","message_type":"group","message_id":88,"group_id":777,"user_id":42,"raw_message":"hi","message":[{"type":"text","data":{"text":"hi"}}],"sender":{"nickname":"张三","card":"名片"}}`)
	e, err := ParseEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	in := e.ToInbound("3")
	if in == nil || in.MessageID != "onebot_88" || in.SenderID != "42" || in.GroupID != "777" || in.Content != "hi" {
		t.Fatalf("message inbound = %+v", in)
	}
	if in.EventType != "" && in.EventType != model.EventTypeMessageReceived {
		t.Fatalf("message 事件类型 = %q", in.EventType)
	}

	recall := []byte(`{"time":1700000001,"self_id":1,"post_type":"notice","notice_type":"group_recall","group_id":777,"user_id":42,"operator_id":42,"message_id":88}`)
	e2, err := ParseEvent(recall)
	if err != nil {
		t.Fatal(err)
	}
	in2 := e2.ToInbound("3")
	if in2 == nil || in2.EventType != model.EventTypeMessageDeleted {
		t.Fatalf("recall inbound = %+v", in2)
	}

	if _, err := ParseEvent([]byte(`{"time":1}`)); err == nil {
		t.Fatal("缺 post_type 应报错")
	}
}

func TestVerifyTokenAndEscape(t *testing.T) {
	if !VerifyToken("tok", "Bearer tok", "") {
		t.Fatal("Bearer token 应通过")
	}
	if VerifyToken("tok", "Bearer nope", "") {
		t.Fatal("错误 token 必须拒绝")
	}
	if !VerifyToken("tok", "", "tok") {
		t.Fatal("query token 应通过")
	}
	if got := EscapeCQ(`[CQ:at,qq=1] &`); got != `&#91;CQ:at,qq=1&#93; &amp;` {
		t.Fatalf("EscapeCQ = %q", got)
	}
}

func TestSplitText(t *testing.T) {
	got := SplitText(strings.Repeat("a", 25), 10)
	if len(got) != 3 {
		t.Fatalf("切块数 = %d (%v)", len(got), got)
	}
	for _, c := range got {
		if len([]rune(c)) > 10 {
			t.Fatalf("块超限: %q", c)
		}
	}
}

func TestSendGroupMsgEcho(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("缺 Authorization: %v", r.Header)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"status":"ok","retcode":0,"data":{"message_id":99}}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	id, err := c.SendGroupMsg(context.Background(), 777, "hello")
	if err != nil || id != 99 {
		t.Fatalf("SendGroupMsg = %d, %v", id, err)
	}
	if got["action"] != "send_group_msg" {
		t.Fatalf("action = %v", got["action"])
	}
}

var _ = core.BaseClient{}
