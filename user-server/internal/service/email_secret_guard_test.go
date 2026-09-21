package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

// 邮件追踪 / 退订链接都用 HMAC-SHA256 签名，密钥来自环境变量。
// 本卡查出的缺陷：`EMAIL_TRACKING_SECRET` 未配置时 tracking 侧仍照常**签发**并用空密钥签名
// （`sign` 不判空），verify 也不判空 ⇒ 任何人都能按公开的 claim 结构算出合法签名；
// `EMAIL_UNSUBSCRIBE_SECRET` 未配置时签发虽已 fail-closed，但 verify 对
// `payload.`（空签名）这条腿会放行：`sign` 返回 ""、`hmac.Equal([]byte(""), []byte(""))` 为真。
// 两条合起来 = 缺密钥的部署里，追踪事件可伪造、任意收件人可被伪签退订链接退订。
//
// 用例不碰库：只走签发/校验两条纯密码学路径，因此用 nil repo 构造服务即可。

func signWithKey(key, payloadB64 string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payloadB64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// forgeToken 按**公开的** claim 结构造 token：key 为空即模拟"服务侧没配密钥"时
// 攻击者可自行算出的签名；emptySig 为真则造 `payload.`（空签名）这条腿。
func forgeToken(t *testing.T, claim any, key string, emptySig bool) string {
	t.Helper()
	payload, err := json.Marshal(claim)
	if err != nil {
		t.Fatalf("marshal claim: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	if emptySig {
		return payloadB64 + "."
	}
	return payloadB64 + "." + signWithKey(key, payloadB64)
}

func trackingClaim(email string) EmailTrackingClaim {
	return EmailTrackingClaim{
		Email:  email,
		JobID:  "job-forged",
		Type:   "open",
		Expire: time.Now().Add(time.Hour).Unix(),
		Nonce:  "forged-nonce",
	}
}

func unsubscribeClaim(email string) UnsubscribeClaim {
	return UnsubscribeClaim{
		Email:  email,
		JobID:  "job-forged",
		Expire: time.Now().Add(24 * time.Hour).Unix(),
		Nonce:  "forged-nonce",
	}
}

func TestEmailTrackingToken_SecretUnsetMustNotSignOrVerify(t *testing.T) {
	t.Setenv("EMAIL_TRACKING_SECRET", "")
	svc := NewEmailTrackingService(nil)
	ctx := context.Background()

	// 签发侧：缺密钥必须报错，而不是静默用空密钥签一个"看着像合法"的 token
	if _, err := svc.GenerateTrackingPixelToken(ctx, "victim@example.com", "job-1"); err == nil {
		t.Fatalf("EMAIL_TRACKING_SECRET 未配置时不得签发追踪 token")
	} else if !strings.Contains(err.Error(), "EMAIL_TRACKING_SECRET") {
		t.Fatalf("签发错误必须点名它读的那个环境变量，实际：%v", err)
	}
	if _, err := svc.GenerateClickTrackingLink(ctx, "victim@example.com", "job-1", "https://example.com"); err == nil {
		t.Fatalf("EMAIL_TRACKING_SECRET 未配置时不得签发点击追踪链接")
	}

	// 验证侧 A：按公开结构 + 空密钥伪造的 token 必须拒
	if _, err := svc.VerifyTrackingToken(ctx, forgeToken(t, trackingClaim("victim@example.com"), "", false)); err == nil {
		t.Fatalf("空密钥伪造的追踪 token 必须拒绝（缺密钥即不可用，不是退化成无密钥校验）")
	} else if !strings.Contains(err.Error(), "EMAIL_TRACKING_SECRET") {
		// 缺密钥时给运维的结论是"这个部署没法校验 token"，不是"某个 token 签名不对" —— 后者会把
		// 配置缺失一路追成一堆可疑的伪造请求。删掉 verify 里的密钥判空腿，这条就红。
		t.Fatalf("缺密钥时的校验错误必须点名环境变量，实际：%v", err)
	}
	// 验证侧 B：空签名（`payload.`）这条形态必须拒 —— `hmac.Equal(空,空)` 为真，不能靠它兜底。
	// 由谁拒的不重要（缺密钥时是密钥判空腿，配了密钥时是验签腿），两态各有一条用例盯着。
	if _, err := svc.VerifyTrackingToken(ctx, forgeToken(t, trackingClaim("victim@example.com"), "", true)); err == nil {
		t.Fatalf("缺密钥时空签名追踪 token 必须拒绝")
	}
}

func TestEmailTrackingToken_ConfiguredSecretRoundTrip(t *testing.T) {
	t.Setenv("EMAIL_TRACKING_SECRET", "tracking-secret-0123456789abcdef")
	svc := NewEmailTrackingService(nil)
	ctx := context.Background()

	token, err := svc.GenerateTrackingPixelToken(ctx, "Open@Demo.com", "job-2")
	if err != nil {
		t.Fatalf("配好密钥后必须能签发，实际报错：%v", err)
	}
	claim, err := svc.VerifyTrackingToken(ctx, token)
	if err != nil {
		t.Fatalf("自签发自校验必须通过，实际：%v", err)
	}
	if claim.Email != "open@demo.com" {
		t.Fatalf("claim 邮箱归一化不符：%q", claim.Email)
	}
	// 已配密钥时，空密钥伪造的 token 签名对不上，必须拒
	if _, err := svc.VerifyTrackingToken(ctx, forgeToken(t, trackingClaim("victim@example.com"), "", false)); err == nil {
		t.Fatalf("用空密钥伪造的 token 在已配置密钥下必须拒")
	}
	// 已配密钥 + 空签名（`payload.`）这条可达路径也必须拒：验签比较真在判，不是靠上游短路
	if _, err := svc.VerifyTrackingToken(ctx, forgeToken(t, trackingClaim("victim@example.com"), "", true)); err == nil {
		t.Fatalf("已配置密钥时空签名 token 必须拒")
	}
}

func TestEmailUnsubscribeToken_SecretUnsetMustRejectEmptySignature(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "")
	svc := NewEmailUnsubscribeService(nil)
	ctx := context.Background()

	if _, err := svc.GenerateUnsubscribeLink(ctx, "victim@example.com", "job-1"); err == nil {
		t.Fatalf("签发侧本就 fail-closed，不应在无密钥时签发")
	}
	if _, err := svc.VerifyUnsubscribeToken(ctx, forgeToken(t, unsubscribeClaim("victim@example.com"), "", true)); err == nil {
		t.Fatalf("缺密钥时空签名 token 必须拒（verify 也要 fail-closed）")
	}
	if _, err := svc.VerifyUnsubscribeToken(ctx, forgeToken(t, unsubscribeClaim("victim@example.com"), "", false)); err == nil {
		t.Fatalf("缺密钥时空密钥伪造的 token 必须拒")
	} else if !strings.Contains(err.Error(), "EMAIL_UNSUBSCRIBE_SECRET") {
		// 与追踪侧同一条口径：缺密钥要说"这个部署没法校验"，不能说"某个 token 签名不对"
		t.Fatalf("缺密钥时的校验错误必须点名环境变量，实际：%v", err)
	}
}

func TestEmailUnsubscribeToken_ConfiguredSecretRoundTrip(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "unsubscribe-secret-0123456789")
	svc := NewEmailUnsubscribeService(nil)
	ctx := context.Background()

	link, err := svc.GenerateUnsubscribeLink(ctx, "victim@example.com", "job-1")
	if err != nil {
		t.Fatalf("配好密钥后必须能签发退订链接：%v", err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("退订链接不是合法 URL：%v", err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("退订链接缺少 token 查询参数：%s", link)
	}
	claim, err := svc.VerifyUnsubscribeToken(ctx, token)
	if err != nil {
		t.Fatalf("自签发自校验必须通过，实际：%v", err)
	}
	if claim.Email != "victim@example.com" {
		t.Fatalf("claim 邮箱不符：%q", claim.Email)
	}
	if _, err := svc.VerifyUnsubscribeToken(ctx, forgeToken(t, unsubscribeClaim("attacker@example.com"), "", false)); err == nil {
		t.Fatalf("用空密钥伪造的退订 token 在已配置密钥下必须拒")
	}
	if _, err := svc.VerifyUnsubscribeToken(ctx, forgeToken(t, unsubscribeClaim("attacker@example.com"), "", true)); err == nil {
		t.Fatalf("已配置密钥时空签名退订 token 必须拒")
	}
}
