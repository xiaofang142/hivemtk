package service

import (
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func wechatSignature(token, ts, nonce string) string {
	parts := []string{token, ts, nonce}
	sort.Strings(parts)
	h := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:])
}

func newWechatVerifyService(t *testing.T, accounts ...*model.WechatAccount) *WebhookService {
	t.Helper()
	db := testutil.NewTestDB(t, &model.WechatAccount{}, &model.WebhookEvent{})
	for _, acc := range accounts {
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("create wechat account: %v", err)
		}
	}
	return NewWebhookService(db)
}

// TestVerify_Wechat_NoSecretConfiguredRejects 未配置 secret：fail-closed 拒绝验签。
//
// 历史沿革（勿回退）：本用例原名 ..._Skips，断言「未配置 secret 时跳过验签」（fail-open）。
// 后生产代码（internal/service/webhook.go:479-489）改为 fail-closed，理由充分 ——
// 公开 webhook 在无 secret 时放行，等于任何人都能伪造上行消息注入系统。
// 因此本用例同步改为断言「拒绝」，仅在显式 ALLOW_INSECURE_WEBHOOK=true 时才允许放行。
func TestVerify_Wechat_NoSecretConfiguredRejects(t *testing.T) {
	svc := newWechatVerifyService(t)
	defer svc.Stop(context.Background())

	ok, err := svc.Verify(context.Background(), ChannelWechat, "9",
		[]byte(`{}`), map[string]string{}, nil)
	if err == nil {
		t.Fatal("未配置 secret 时应拒绝验签（fail-closed），实际未返回错误")
	}
	if ok {
		t.Fatal("未配置 secret 时不得放行 —— fail-open 属安全缺陷，可被任意伪造上行消息")
	}
}

// TestVerify_Wechat_InsecureEscapeHatch 仅当显式 ALLOW_INSECURE_WEBHOOK=true 时才放行。
//
// 这条逃生通道是给本地联调用的，必须有测试钉住「默认不生效」，
// 否则一旦默认值被改，线上会在无验签状态下静默放行全部 webhook。
func TestVerify_Wechat_InsecureEscapeHatch(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")

	svc := newWechatVerifyService(t)
	defer svc.Stop(context.Background())

	ok, err := svc.Verify(context.Background(), ChannelWechat, "9",
		[]byte(`{}`), map[string]string{}, nil)
	if err != nil {
		t.Fatalf("显式开启 ALLOW_INSECURE_WEBHOOK 后不应报错，实际 %v", err)
	}
	if !ok {
		t.Fatal("显式开启 ALLOW_INSECURE_WEBHOOK 后应放行")
	}
}

// TestVerify_Wechat_ConfiguredTokenPasses 已配置 token：合法签名通过
func TestVerify_Wechat_ConfiguredTokenPasses(t *testing.T) {
	svc := newWechatVerifyService(t, &model.WechatAccount{
		ID: 3, AppID: "wx1", AppSecret: "sec", Token: "my-token", Status: "active",
	})
	defer svc.Stop(context.Background())

	ts, nonce := "1700000000", "nonce-abc"
	sig := wechatSignature("my-token", ts, nonce)
	ok, err := svc.Verify(context.Background(), ChannelWechat, "3",
		[]byte(`{}`), map[string]string{
			"X-Wechat-Timestamp": ts,
			"X-Wechat-Nonce":     nonce,
			"X-Wechat-Signature": sig,
		}, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("W-6 未达成：已配置 token 的合法签名未通过")
	}

	bad := wechatSignature("wrong-token", ts, nonce)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(bad)) == 1 {
		t.Fatal("test self-check failed: signatures should differ")
	}
	okBad, _ := svc.Verify(context.Background(), ChannelWechat, "3",
		[]byte(`{}`), map[string]string{
			"X-Wechat-Timestamp": ts,
			"X-Wechat-Nonce":     nonce,
			"X-Wechat-Signature": bad,
		}, nil)
	if okBad {
		t.Error("伪造签名不应通过")
	}
}

// TestGetWechatSecrets_FallbackFirstActive accountID 无法定位时回退第一个 active 账号
func TestGetWechatSecrets_FallbackFirstActive(t *testing.T) {
	svc := newWechatVerifyService(t, &model.WechatAccount{
		ID: 5, AppID: "wx2", AppSecret: "sec", Token: "fallback-token", Status: "active",
	})
	defer svc.Stop(context.Background())

	token, aesKey := svc.getWechatSecrets(context.Background(), "")
	if token != "fallback-token" {
		t.Errorf("expected fallback token, got %q", token)
	}
	if aesKey != "" {
		t.Errorf("expected empty aes key, got %q", aesKey)
	}
}
