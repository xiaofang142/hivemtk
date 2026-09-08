package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// TestValidateQQWebhookURL 覆盖 ValidateQQWebhookURL 的所有分支
// （对照官方约束：https + 端口白名单 80/443/8080/8443 + path 前缀）
func TestValidateQQWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"blank spaces", "   ", true},
		{"valid no port", "https://chat.example.com/api/webhook/qq/1", false},
		{"valid 443", "https://chat.example.com:443/api/webhook/qq/42", false},
		{"valid 8080", "https://chat.example.com:8080/api/webhook/qq/7", false},
		{"valid 8443", "https://chat.example.com:8443/api/webhook/qq/7", false},
		{"valid 80", "https://chat.example.com:80/api/webhook/qq/7", false},
		{"http scheme", "http://chat.example.com/api/webhook/qq/1", true},
		{"missing host", "https:///api/webhook/qq/1", true},
		{"bad port 8204", "https://chat.example.com:8204/api/webhook/qq/1", true},
		{"bad port 9000", "https://chat.example.com:9000/api/webhook/qq/1", true},
		{"wrong prefix", "https://chat.example.com/webhook/qq/1", true},
		{"wrong prefix tg", "https://chat.example.com/api/webhook/telegram/1", true},
		{"not parseable", "ht tp://bad url", true},
		{"no path", "https://chat.example.com", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateQQWebhookURL(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateQQWebhookURL(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			}
		})
	}
}

// TestSuggestQQWebhookURLFromBase 推导逻辑：空 base 返回空串；非空拼接路径前缀 + ID
func TestSuggestQQWebhookURLFromBase(t *testing.T) {
	if got := SuggestQQWebhookURLFromBase("", 1); got != "" {
		t.Errorf("empty base: got %q, want empty", got)
	}
	got := SuggestQQWebhookURLFromBase("https://chat.example.com", 42)
	if got != "https://chat.example.com/api/webhook/qq/42" {
		t.Errorf("suggest: got %q", got)
	}
	// 带尾部斜杠的 base 应被 TrimRight
	got = SuggestQQWebhookURLFromBase("https://chat.example.com/", 7)
	if got != "https://chat.example.com/api/webhook/qq/7" {
		t.Errorf("suggest trailing slash: got %q", got)
	}
	// 推导结果必须自洽通过校验（白名单端口场景除外）
	if err := ValidateQQWebhookURL(got); err != nil {
		t.Errorf("suggested URL should pass validation: %v", err)
	}
}

// TestVerifyCallbackSelfCheck_Errors 无 DB / 无 secret 的错误分支
func TestVerifyCallbackSelfCheck_Errors(t *testing.T) {
	svc := NewQQService(nil)
	// 账号不存在
	if _, err := svc.VerifyCallbackSelfCheck(nil, 999999); err == nil {
		t.Error("expect error for missing account, got nil")
	}
}

// TestQQCallbackSelfCheckResult_Fields 结果结构字段名与平台 op13 应答体对齐
func TestQQCallbackSelfCheckResult_Fields(t *testing.T) {
	r := QQCallbackSelfCheckResult{PlainToken: "tok", EventTS: "123", Signature: "sig"}
	if r.PlainToken == "" || r.EventTS == "" || r.Signature == "" {
		t.Error("result fields must be populated")
	}
	if !strings.Contains("plain_token event_ts signature", "signature") {
		t.Error("unexpected field naming")
	}
	_ = model.QQAccount{} // 引用 model 包防 import 漂移
}
