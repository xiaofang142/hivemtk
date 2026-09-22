package service

// 批B 入站验签安全收口回归（对应审计 §5 S-01/S-02/S-03/S-05）。
//
// 每条用例都对应审计里「坐实」的一个缺陷：
//   S-01 飞书 EncryptKey 缺失即 return true —— 明文模式必须校验 VerificationToken
//   S-02 ALLOW_INSECURE_WEBHOOK 一刀切绕过所有渠道 —— 只准豁免「未配置密钥」的账号
//   S-03 微信签名 == 比对（非常数时间）
//   S-05 企微回调无 freshness 时窗

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// ---------------------------------------------------------------------------
// S-01 飞书明文模式（未配 EncryptKey）必须校验 VerificationToken
// ---------------------------------------------------------------------------

// 官方契约：飞书事件订阅在「未配置 Encrypt Key」时以明文 JSON 推送，
// 此时 **不会** 发送 X-Lark-Signature 可验的签名，事件体自带的 token 字段
// （v2.0 schema 在 header.token，v1.0 在顶层 token）就是唯一的来源凭证，
// 必须与后台配置的 Verification Token 比对。
func setupFeishuVerify(t *testing.T, verificationToken, encryptKey string) (*WebhookService, string) {
	t.Helper()
	db := testutil.NewTestDBOrSkip(t, &model.FeishuAccount{}, &model.IntegrationAccount{})
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })
	acc := &model.FeishuAccount{
		AccountName: "批B飞书号", AppID: "cli_batchb", AppSecret: "sec",
		VerificationToken: verificationToken, EncryptKey: encryptKey, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed feishu account: %v", err)
	}
	return NewWebhookService(db), fmt.Sprintf("%d", acc.ID)
}

func feishuV2EventBody(token, eventID string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":    eventID,
			"event_type":  "im.message.receive_v1",
			"create_time": fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond)),
			"token":       token,
			"app_id":      "cli_batchb",
			"tenant_key":  "tk",
		},
		"event": map[string]any{"sender": map[string]any{}},
	})
	return raw
}

func TestVerify_Feishu_PlaintextModeRequiresVerificationToken(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	svc, accountID := setupFeishuVerify(t, "vtok-batchb", "")
	ctx := context.Background()

	cases := []struct {
		name string
		body []byte
		want bool
	}{
		{"v2 header.token 正确", feishuV2EventBody("vtok-batchb", "evt-ok"), true},
		{"v2 header.token 错误", feishuV2EventBody("attacker-token", "evt-bad"), false},
		{"v2 缺 token 字段", func() []byte {
			raw, _ := json.Marshal(map[string]any{
				"schema": "2.0",
				"header": map[string]any{"event_id": "evt-notoken", "event_type": "im.message.receive_v1"},
				"event":  map[string]any{},
			})
			return raw
		}(), false},
		{"v1 顶层 token 正确", func() []byte {
			raw, _ := json.Marshal(map[string]any{
				"uuid": "u-1", "token": "vtok-batchb", "type": "event_callback", "event": map[string]any{},
			})
			return raw
		}(), true},
		{"非事件体的任意 JSON", []byte(`{"hello":"world"}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Verify(ctx, ChannelFeishu, accountID, tc.body, map[string]string{}, map[string]string{})
			if tc.want {
				if err != nil || !got {
					t.Fatalf("合法 VerificationToken 事件应通过，got=%v err=%v", got, err)
				}
				return
			}
			if got {
				t.Fatalf("非法明文事件必须拒收，却返回 verified=true（S-01 fail-open）")
			}
		})
	}
}

func TestVerify_Feishu_MissingBothKeysFailsClosed(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	svc, accountID := setupFeishuVerify(t, "", "")
	got, _ := svc.Verify(context.Background(), ChannelFeishu, accountID,
		feishuV2EventBody("whatever", "evt-fc"), map[string]string{}, map[string]string{})
	if got {
		t.Fatal("EncryptKey 与 VerificationToken 都未配置时必须 fail-closed（S-01）")
	}
}

func TestVerify_Feishu_EncryptedModeStillNeedsSignature(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	svc, accountID := setupFeishuVerify(t, "vtok-batchb", "encKey-batchb")
	env, _ := json.Marshal(map[string]string{"encrypt": "not-a-real-ciphertext"})
	got, _ := svc.Verify(context.Background(), ChannelFeishu, accountID, env,
		map[string]string{}, map[string]string{})
	if got {
		t.Fatal("配了 EncryptKey 却无 X-Lark-Signature 的信封必须拒收")
	}
}

// ---------------------------------------------------------------------------
// S-02 ALLOW_INSECURE_WEBHOOK 只准豁免「未配置密钥」的账号
// ---------------------------------------------------------------------------

func TestVerify_InsecureBypassDoesNotSkipConfiguredSecret(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := testutil.NewTestDBOrSkip(t, &model.QQAccount{}, &model.IntegrationAccount{})
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	acc := &model.QQAccount{
		AccountName: "批B-QQ号", AppID: "appid-b", AppSecret: "s",
		WebhookSecret: "real-bot-secret", Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed qq account: %v", err)
	}
	svc := NewWebhookService(db)
	body := []byte(`{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"m1"}}`)
	forged := map[string]string{
		"X-Signature-Ed25519":   strings.Repeat("00", 64),
		"X-Signature-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
	}
	got, err := svc.Verify(context.Background(), ChannelQQ, fmt.Sprintf("%d", acc.ID), body, forged, nil)
	if got {
		t.Fatalf("开发开关不得绕过已配置密钥账号的验签，却放行了伪造签名（err=%v）", err)
	}
}

// ---------------------------------------------------------------------------
// S-03 微信签名比对
// ---------------------------------------------------------------------------

func wechatOfficialSign(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func TestWechatVerifySignature_OfficialContract(t *testing.T) {
	svc := &WechatService{}
	// 固定输入让签名可复现；freshness 不在本函数职责内（见 S-05 的企微时窗用例）
	ts := "1700000000"
	good := wechatOfficialSign("tok-1", ts, "n-1")

	if !svc.VerifySignature("tok-1", good, ts, "n-1") {
		t.Fatal("官方算法签名应通过")
	}
	// 确定性翻转首位十六进制字符，避免随机时间戳下的极小概率同值
	flipped := []byte(good)
	if flipped[0] == '0' {
		flipped[0] = '1'
	} else {
		flipped[0] = '0'
	}
	if svc.VerifySignature("tok-1", string(flipped), ts, "n-1") {
		t.Fatal("首位被翻转的签名不应通过")
	}
	// sha1 hex 固定 40 位：截断/加长/改字符都必须判不匹配
	for _, bad := range []string{"", good[:39], good + "00", "zz" + good[2:]} {
		if svc.VerifySignature("tok-1", bad, ts, "n-1") {
			t.Fatalf("畸形签名 %q 不应通过", bad)
		}
	}
	if svc.VerifySignature("tok-other", good, ts, "n-1") {
		t.Fatal("token 不同却签名相同不应通过")
	}
}

// TestWechatVerifySignature_ConstantTimeCompare 是源码级守卫：
// 签名比对一旦退回 `==`，长度前缀试探的计时侧信道就回来了，而行为测试
// 无法在 CI 里稳定区分两者，故直接锁死实现方式。
func TestWechatVerifySignature_ConstantTimeCompare(t *testing.T) {
	src, err := os.ReadFile("wechat.go")
	if err != nil {
		t.Fatalf("read wechat.go: %v", err)
	}
	const marker = "func (s *WechatService) VerifySignature("
	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatal("VerifySignature 已不存在，守卫测试需同步更新")
	}
	body := string(src[i:])
	if j := strings.Index(body[len(marker):], "\n}"); j > 0 {
		body = body[:len(marker)+j+2]
	}
	if !strings.Contains(body, "subtle.ConstantTimeCompare") {
		t.Error("VerifySignature 必须用 subtle.ConstantTimeCompare 比对签名（S-03）")
	}
	if strings.Contains(body, "== signature") {
		t.Error("VerifySignature 退回 `== signature` 非常数时间比对（S-03）")
	}
}

// ---------------------------------------------------------------------------
// S-05 企微回调 freshness 时窗
// ---------------------------------------------------------------------------

func wecomOfficialSign(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func TestVerifyWeCom_TimestampFreshnessWindow(t *testing.T) {
	const token = "wecom-callback-token"
	mk := func(ts string) []byte {
		enc := "cipher-blob"
		q := map[string]string{
			"msg_signature": wecomOfficialSign(token, ts, "nonce-1", enc),
			"timestamp":     ts,
			"nonce":         "nonce-1",
			"echostr":       enc,
		}
		body, _ := json.Marshal(map[string]string{
			"msg_signature": q["msg_signature"],
			"timestamp":     ts,
			"nonce":         "nonce-1",
			"encrypt":       enc,
		})
		return body
	}

	fresh := fmt.Sprintf("%d", time.Now().Unix())
	if ok, err := verifyWeCom(token, "", mk(fresh), nil); !ok {
		t.Fatalf("新鲜时间戳的合法签名应通过，got err=%v", err)
	}

	stale := fmt.Sprintf("%d", time.Now().Add(-3*time.Hour).Unix())
	if ok, err := verifyWeCom(token, "", mk(stale), nil); ok {
		t.Fatalf("3 小时前的旧签名必须因超出 freshness 时窗被拒（S-05），却通过了")
	} else if err == nil {
		t.Error("拒绝旧时间戳必须给出原因")
	}

	future := fmt.Sprintf("%d", time.Now().Add(3*time.Hour).Unix())
	if ok, _ := verifyWeCom(token, "", mk(future), nil); ok {
		t.Fatalf("未来 3 小时的时间戳同样应被拒（S-05）")
	}

	garbage := []byte(`{"encrypt":"x","msg_signature":"deadbeef","timestamp":"not-a-number","nonce":"n"}`)
	if ok, _ := verifyWeCom(token, "", garbage, map[string]string{
		"msg_signature": "deadbeef", "timestamp": "not-a-number", "nonce": "n",
	}); ok {
		t.Fatal("非数字 timestamp 不得通过")
	}
}
