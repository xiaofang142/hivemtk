package controller

// 钉钉回调 HTTP 层契约测试：复用生产 WebhookController.RegisterRoutes，
// 覆盖 service 包内测试摸不到的一段——extractHeaders / extractQuery 的取参白名单。
// 官方把签名放在 header(sign/timestamp) 与 query(signature) 里，白名单漏一项
// 就等于线上永远验不到签名。

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const dtSimSessionWebhook = "https://oapi.dingtalk.com/robot/sendBySession?session=sim123"

// dtSimPlainBody 官方企业内部机器人「HTTP 模式」明文回调体
func dtSimPlainBody(msgID, staffID, content string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"conversationId":            "cid-sim-" + msgID,
		"conversationType":          "2",
		"msgId":                     msgID,
		"createAt":                  time.Now().UnixMilli(),
		"senderId":                  "$:LWCP_v1:$" + msgID,
		"senderStaffId":             staffID,
		"senderNick":                "模拟客户",
		"msgtype":                   "text",
		"text":                      map[string]string{"content": content},
		"sessionWebhook":            dtSimSessionWebhook,
		"sessionWebhookExpiredTime": time.Now().Add(30 * time.Minute).UnixMilli(),
	})
	return raw
}

// dtSimRobotSign 官方明文机器人回调验签串：base64(HMAC-SHA256("<ts>\n<appSecret>", key=appSecret))
func dtSimRobotSign(t *testing.T, appSecret string, ts int64) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(appSecret))
	if _, err := mac.Write([]byte(fmt.Sprintf("%d\n%s", ts, appSecret))); err != nil {
		t.Fatalf("hmac write: %v", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// dtSimEncrypt 官方事件订阅布局：random(16)+msg_len(4,大端)+msg+receiveId，IV=密钥前 16 字节
func dtSimEncrypt(t *testing.T, encodingAESKey, msg, receiveID string) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != 32 {
		t.Fatalf("EncodingAESKey 应为 43 位、补 = 后解出 32 字节 (err=%v len=%d)", err, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	parts := make([]byte, 16, 64)
	for i := range parts {
		parts[i] = byte(i)
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(msg)))
	parts = append(parts, lenBuf[:]...)
	parts = append(parts, msg...)
	parts = append(parts, receiveID...)
	pad := aes.BlockSize - len(parts)%aes.BlockSize
	parts = append(parts, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(parts))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(ct, parts)
	return base64.StdEncoding.EncodeToString(ct)
}

// dtSimEventSign 官方事件订阅验签：sha1(sort(token,timestamp,nonce,encrypt))
func dtSimEventSign(token, timestamp, nonce, encrypt string) string {
	vals := []string{token, timestamp, nonce, encrypt}
	for i := 1; i < len(vals); i++ {
		for j := i; j > 0 && vals[j-1] > vals[j]; j-- {
			vals[j-1], vals[j] = vals[j], vals[j-1]
		}
	}
	sum := sha1.Sum([]byte(strings.Join(vals, "")))
	return hex.EncodeToString(sum[:])
}

type dtFullchainEnv struct {
	db        *gorm.DB
	engine    *gin.Engine
	accountID string
	secret    string
	token     string
	aesKey    string
	webhook   *service.WebhookService
}

func setupDingTalkFullchain(t *testing.T) *dtFullchainEnv {
	t.Helper()
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")

	database := testutil.NewTestDB(t,
		&model.DingTalkAppAccount{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.UnifiedMessage{},
		&model.WebhookEvent{},
	)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	const (
		appSecret = "SEC-sim-app-secret"
		token     = "sim-evt-token"
	)
	// 43 位官方 EncodingAESKey：32 随机字节 base64 后去掉填充的 "="
	aesKey := strings.TrimRight(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)), "=")

	acc := &model.DingTalkAppAccount{
		AccountName: "钉钉全链路模拟号", AppKey: "sim-app-key", AppSecret: appSecret,
		Token: token, AESKey: aesKey, InboundEnabled: true, Status: 1,
	}
	if err := database.Create(acc).Error; err != nil {
		t.Fatalf("seed dingtalk account: %v", err)
	}

	webhookSvc := service.NewWebhookService(database)
	ingress := service.NewInboxIngressServiceWithDB(database, nil)
	ingress.SetInboxService(service.NewInboxServiceWithDB(database))
	webhookSvc.SetIngressSvc(ingress)

	gin.SetMode(gin.TestMode)
	g := gin.New()
	ctrl := NewWebhookController(webhookSvc)
	ctrl.SetDingTalkAppService(service.NewDingTalkAppService(database, webhookSvc))
	ctrl.RegisterRoutes(g)

	return &dtFullchainEnv{
		db: database, engine: g, accountID: strconv.FormatUint(uint64(acc.ID), 10),
		secret: appSecret, token: token, aesKey: aesKey, webhook: webhookSvc,
	}
}

func (e *dtFullchainEnv) post(t *testing.T, body []byte, query string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/dingtalk/"+e.accountID+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// TestDingTalkFullchain_PlainRobotCallbackAccepted 官方明文机器人回调经真实路由必须被受理并落库。
func TestDingTalkFullchain_PlainRobotCallbackAccepted(t *testing.T) {
	env := setupDingTalkFullchain(t)
	defer env.webhook.Stop(context.Background())

	ts := time.Now().UnixMilli()
	body := dtSimPlainBody("sim-plain-1", "staff-sim-1", "你们产品多少钱")
	rec := env.post(t, body, "", map[string]string{
		"timestamp": strconv.FormatInt(ts, 10),
		"sign":      dtSimRobotSign(t, env.secret, ts),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("合法明文机器人回调应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	var hub model.MessageHub
	if err := env.db.Where("platform = ? AND msg_id = ?", "dingtalk", "dt-1-sim-plain-1").
		First(&hub).Error; err != nil {
		t.Fatalf("回调应经消息中台落库（白名单没取到 sign/timestamp 头时这里就是空的）: %v", err)
	}
	if hub.SenderID != "staff-sim-1" || !strings.Contains(hub.Content, "多少钱") {
		t.Errorf("落库字段错位: %+v", hub)
	}
	if got, _ := hub.Extra["session_webhook"].(string); got != dtSimSessionWebhook {
		t.Errorf("sessionWebhook 必须透传给出站，got %q", got)
	}
}

func TestDingTalkFullchain_PlainRobotCallbackRejectedSignals(t *testing.T) {
	env := setupDingTalkFullchain(t)
	defer env.webhook.Stop(context.Background())

	fresh := time.Now().UnixMilli()
	validSign := dtSimRobotSign(t, env.secret, fresh)
	body := dtSimPlainBody("sim-plain-bad", "staff-sim-bad", "伪造消息")

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"无签名头", map[string]string{}},
		{"签名与密钥不匹配", map[string]string{
			"timestamp": strconv.FormatInt(fresh, 10),
			"sign":      dtSimRobotSign(t, "SEC-wrong", fresh),
		}},
		{"时间戳超过 1 小时", map[string]string{
			"timestamp": strconv.FormatInt(fresh-2*3600*1000, 10),
			"sign":      dtSimRobotSign(t, env.secret, fresh-2*3600*1000),
		}},
		{"签名正确但时间戳被改写", map[string]string{
			"timestamp": strconv.FormatInt(fresh-2*3600*1000, 10),
			"sign":      validSign,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.post(t, body, "", tc.headers)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("非法回调应 400，got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	var cnt int64
	if err := env.db.Model(&model.MessageHub{}).Where("platform = ?", "dingtalk").Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("非法回调不得产生任何落库副作用，实际 %d 行", cnt)
	}
}

// TestDingTalkFullchain_EventSubscriptionCallbackAccepted 事件订阅加密回调（query 验签）必须走通。
func TestDingTalkFullchain_EventSubscriptionCallbackAccepted(t *testing.T) {
	env := setupDingTalkFullchain(t)
	defer env.webhook.Stop(context.Background())

	enc := dtSimEncrypt(t, env.aesKey, string(dtSimPlainBody("sim-evt-1", "staff-sim-evt", "有优惠吗")), "ding-sim-corp")
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := "sim-nonce"
	query := fmt.Sprintf("?signature=%s&timestamp=%s&nonce=%s",
		dtSimEventSign(env.token, ts, nonce, enc), ts, nonce)
	body, _ := json.Marshal(map[string]string{"encrypt": enc})

	rec := env.post(t, body, query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法事件订阅回调应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	var hub model.MessageHub
	if err := env.db.Where("platform = ? AND msg_id = ?", "dingtalk", "dt-1-sim-evt-1").
		First(&hub).Error; err != nil {
		t.Fatalf("加密回调应解密入库（query 白名单漏 signature 时这里就是空的）: %v", err)
	}
}

func TestDingTalkFullchain_EventSubscriptionTamperedRejected(t *testing.T) {
	env := setupDingTalkFullchain(t)
	defer env.webhook.Stop(context.Background())

	enc := dtSimEncrypt(t, env.aesKey, string(dtSimPlainBody("sim-evt-bad", "staff-x", "伪造")), "ding-sim-corp")
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	body, _ := json.Marshal(map[string]string{"encrypt": enc})

	for _, q := range []string{
		fmt.Sprintf("?signature=%s&timestamp=%s&nonce=n", strings.Repeat("0", 40), ts),
		fmt.Sprintf("?timestamp=%s&nonce=n", ts),
	} {
		rec := env.post(t, body, q, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("签名缺失/错误的加密回调应 400，got %d body=%s (query=%s)", rec.Code, rec.Body.String(), q)
		}
	}
	var cnt int64
	if err := env.db.Model(&model.MessageHub{}).Where("msg_id = ?", "dt-1-sim-evt-bad").Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("被拒回调不得落库，实际 %d 行", cnt)
	}
}
