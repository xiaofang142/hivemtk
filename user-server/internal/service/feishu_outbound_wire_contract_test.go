package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"

	"gorm.io/gorm"
)

// 飞书出站请求的官方契约夹具测试（无需真凭据）。
// 契约出处：
//   发消息 https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json
//     —— receive_id_type 取值只有 open_id|union_id|user_id|email|chat_id；
//        content 必须是「JSON 结构体序列化后的字符串」，text 消息即 {"text":"..."}。
//   错误码 https://open.feishu.cn/document/server-docs/im-v1/error-codes
//     —— 业务错误走 HTTP 200 + 非零 code（230013 等），必须按 code 判失败。

type feishuWireTransport struct {
	lastReq      *http.Request
	lastBody     []byte
	respHTTPCode int
	respBody     string
}

func (tr *feishuWireTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/open-apis/im/v1/messages" {
		cp := r.Clone(r.Context())
		tr.lastReq = cp
		tr.lastBody, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	body := tr.respBody
	status := tr.respHTTPCode
	if status == 0 {
		status = http.StatusOK
	}
	if body == "" {
		body = `{"code":0,"msg":"success","data":{"message_id":"om_contract_1"}}`
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func newFeishuWireFixture(t *testing.T) (*FeishuIntegrationService, *gorm.DB, *model.FeishuAccount, *feishuWireTransport) {
	t.Helper()
	database := setupFeishuTestDB(t)
	tr := &feishuWireTransport{}
	orig := httpclient.Client
	httpclient.Client = &http.Client{Transport: tr}
	t.Cleanup(func() { httpclient.Client = orig })
	acc := newFeishuTestAccount(t, database)
	return NewFeishuIntegrationService(database), database, acc, tr
}

func decodeWireBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("出站请求体不是合法 JSON: %v（raw=%s）", err, raw)
	}
	return body
}

func TestFeishuSendMessage_TextContentMustBeStringifiedJSON(t *testing.T) {
	svc, _, acc, tr := newFeishuWireFixture(t)

	if err := svc.SendMessage(context.Background(), acc.ID, "ou_abc123", "价格 1999 起", "open_id", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	body := decodeWireBody(t, tr.lastBody)

	contentRaw, ok := body["content"].(string)
	if !ok {
		t.Fatalf("官方契约要求 content 是 JSON 字符串，got %T（%v）", body["content"], body["content"])
	}
	var inner map[string]string
	if err := json.Unmarshal([]byte(contentRaw), &inner); err != nil {
		t.Fatalf("content 必须能再反序列化为 JSON 对象，got %q", contentRaw)
	}
	if inner["text"] != "价格 1999 起" {
		t.Errorf("content.text 应等于原文，got %q", inner["text"])
	}
	if body["msg_type"] != "text" {
		t.Errorf("msg_type=%v", body["msg_type"])
	}
	if body["receive_id"] != "ou_abc123" {
		t.Errorf("receive_id=%v", body["receive_id"])
	}
	if got := tr.lastReq.URL.Query().Get("receive_id_type"); got != "open_id" {
		t.Errorf("receive_id_type=%q，want open_id", got)
	}
}

func TestFeishuSendMessage_GroupReceiveIDTypeMustBeChatID(t *testing.T) {
	svc, _, acc, tr := newFeishuWireFixture(t)

	if err := svc.SendMessage(context.Background(), acc.ID, "oc_group_1", "群回复", "open_chat_id", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// 服务内部沿用 open_chat_id 作群聊标记，但上线必须是官方取值 chat_id。
	if got := tr.lastReq.URL.Query().Get("receive_id_type"); got != "chat_id" {
		t.Errorf("上线 receive_id_type=%q，官方取值集合里没有这个值，群发必被拒", got)
	}
	if body := decodeWireBody(t, tr.lastBody); body["receive_id"] != "oc_group_1" {
		t.Errorf("receive_id=%v", body["receive_id"])
	}
}

func TestFeishuSendMessage_BusinessErrorOnHTTP200MustFail(t *testing.T) {
	svc, database, acc, tr := newFeishuWireFixture(t)
	tr.respBody = `{"code":230013,"msg":"out of app contact scope","data":{}}`

	err := svc.SendMessage(context.Background(), acc.ID, "ou_abc123", "在范围外", "open_id", "")
	if err == nil {
		t.Fatal("HTTP 200 + 非零业务 code 被当成投递成功：飞书并未收到消息，重试通道也就永不触发")
	}
	if !strings.Contains(err.Error(), "230013") {
		t.Errorf("错误应带上业务码便于排障，got %v", err)
	}

	var updated model.FeishuAccount
	if err := database.First(&updated, acc.ID).Error; err != nil {
		t.Fatalf("读回账号: %v", err)
	}
	if updated.LastErrorMsg == "" {
		t.Error("业务错误应与 HTTP 错误同口径落到 LastErrorMsg")
	}
}
