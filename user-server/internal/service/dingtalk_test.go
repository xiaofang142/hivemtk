package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func dingtalkTestServer(t *testing.T, wantErrcode int, capture *url.Values) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			q := r.URL.Query()
			capture.Add("timestamp", q.Get("timestamp"))
			capture.Add("sign", q.Get("sign"))
			capture.Add("access_token", q.Get("access_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": wantErrcode,
			"errmsg":  "ok",
		})
	}))
}

func TestDingTalkSign_Deterministic(t *testing.T) {
	a := dingtalkSign("my-secret", 1700000000000)
	b := dingtalkSign("my-secret", 1700000000000)
	if a == "" || a != b {
		t.Fatalf("sign 应确定且非空: %q vs %q", a, b)
	}
	if dingtalkSign("other", 1700000000000) == a {
		t.Fatal("不同 secret 应得出不同签名")
	}
}

func TestDingTalkSendRobot_HappyPath(t *testing.T) {
	srv := dingtalkTestServer(t, 0, nil)
	defer srv.Close()

	svc := NewDingTalkService()
	msgID, err := svc.SendRobot(context.Background(), srv.URL, "", "text", "hello dingtalk")
	if err != nil {
		t.Fatalf("SendRobot 失败: %v", err)
	}
	if !strings.HasPrefix(msgID, "dingtalk-") {
		t.Fatalf("msgID 格式异常: %q", msgID)
	}
}

func TestDingTalkSendRobot_AccessTokenOnly(t *testing.T) {
	srv := dingtalkTestServer(t, 0, nil)
	defer srv.Close()

	orig := dingtalkRobotBase
	dingtalkRobotBase = srv.URL
	defer func() { dingtalkRobotBase = orig }()

	svc := NewDingTalkService()
	if _, err := svc.SendRobot(context.Background(), "test-token-xyz", "", "text", "hi"); err != nil {
		t.Fatalf("仅 access_token 应自动拼接 base: %v", err)
	}
}

func TestDingTalkSendRobot_WithSign(t *testing.T) {
	captured := url.Values{}
	srv := dingtalkTestServer(t, 0, &captured)
	defer srv.Close()

	svc := NewDingTalkService()
	if _, err := svc.SendRobot(context.Background(), srv.URL+"|my-secret", "", "text", "signed"); err != nil {
		t.Fatalf("带签名发送失败: %v", err)
	}
	if captured.Get("sign") == "" || captured.Get("timestamp") == "" {
		t.Fatalf("加签模式下应携带官方 timestamp 与 sign 查询参数: %v", captured)
	}
}

func TestDingTalkSendRobot_ApiError(t *testing.T) {
	srv := dingtalkTestServer(t, 300001, nil)
	defer srv.Close()

	svc := NewDingTalkService()
	if _, err := svc.SendRobot(context.Background(), srv.URL, "", "text", "boom"); err == nil {
		t.Fatal("errcode != 0 应返回错误")
	}
}

func TestDingTalkSendRobot_MissingWebhook(t *testing.T) {
	svc := NewDingTalkService()
	if _, err := svc.SendRobot(context.Background(), "", "", "text", "x"); err == nil {
		t.Fatal("空 webhook 应返回错误")
	}
}

func TestIntegrationReachAdapter_SendDingTalk_NilService(t *testing.T) {
	svc := &DingTalkService{}
	if _, err := svc.SendRobot(context.Background(), "", "", "text", "x"); err == nil {
		t.Fatal("空 webhook 应返回错误（零值服务不应 panic）")
	}
}
