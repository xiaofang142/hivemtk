package service

import (
	"bytes"
	"testing"
)

// TestReadAll_WebhookBodyCap 第十六轮回归：公网未认证的 webhook 读体
// 收敛点必须封顶——超限流只读回上限长度，正常 payload 原样读全。
func TestReadAll_WebhookBodyCap(t *testing.T) {
	big := bytes.Repeat([]byte("A"), int(MaxWebhookBody)+1024)
	got, err := ReadAll(bytes.NewReader(big))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if int64(len(got)) != MaxWebhookBody {
		t.Fatalf("应截断到 %d，got %d", MaxWebhookBody, len(got))
	}
	payload := []byte(`{"event":"msg"}`)
	got, err = ReadAll(bytes.NewReader(payload))
	if err != nil || string(got) != string(payload) {
		t.Fatalf("正常 payload 应读全: %q err=%v", got, err)
	}
}
