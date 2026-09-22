package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 钉钉入站有两条互不相同的官方通道，二者验签算法完全不同：
//
//	① 企业内部机器人「HTTP 模式」接收消息：body 为明文 JSON，
//	   header 带 timestamp / sign，sign = base64(HMAC-SHA256("<ts>\n<appSecret>", key=appSecret))，
//	   ts 与本地时间差须 < 1 小时。
//	   （https://open.dingtalk.com/document/orgapp/receive-message）
//	② 事件订阅 HTTP 回调：body 为 {"encrypt":"..."}，query 带 signature/timestamp/nonce，
//	   signature = sha1(sort(token,timestamp,nonce,encrypt))，
//	   明文布局 = random(16) + msg_len(4, 大端) + msg + receiveId。
//	   （https://open.dingtalk.com/document/development/callback-event-message-body-encryption-and-decryption）
//
// 本文件夹具全部用独立实现的官方加密/签名构造，避免与仓库实现互相印证造成假绿。

// dtRobotPlainMsgJSON 按官方示例消息体逐字段构造（robot-message-type 页「消息体」一段）。
// 注意 atUsers 的官方语义是**被@人的信息**（dingtalkId / staffId / unionId），
// 机器人自己的加密 id 在 chatbotUserId —— 本夹具旧版写成 [{"dingtalkId":"bot"}] 是反的，
// 会让后来人以为「atUsers 里有 bot ⇒ 被@」而据此加判定门（官方口径：群里只有 @ 机器人才回调，无需判）。
const dtRobotPlainMsgJSON = `{"conversationId":"cid-77","atUsers":[{"dingtalkId":"$:LWCP_v1:$xyz","staffId":"staff-9","unionId":"edxxx34"}],"msgId":"m-77","createAt":1700000000000,"conversationType":"2","conversationTitle":"销售一组","senderId":"$:LWCP_v1:$xyz","senderStaffId":"staff-9","senderNick":"小明","isAdmin":false,"msgtype":"text","text":{"content":"你们产品多少钱"},"sessionWebhook":"https://oapi.dingtalk.com/robot/sendBySession?session=abc123","sessionWebhookExpiredTime":1893456000000}`

// dtLiveRobotMsg 给「应当触发 AI」的用例用：createAt 从批F-4c 起是**真实时序锚点**
// （hub.sent_at 取它），中台钩子3 会把超过 5 分钟的入站当历史堆积、只落库不触发 AI，
// 所以常量夹具里那个 1700000000000（2023-11）只能用在不进中台的拒绝类用例上。
// 会话 id 同样换成一次性 nonce：message_hub 的会话查询按 conversation_id 收敛
// （不带 platform/account_id），固定 "cid-77" 会把历次运行、别的账号的行算进同一段会话。
func dtLiveRobotMsg(t *testing.T) string {
	t.Helper()
	now := time.Now()
	nonce := strconv.FormatInt(now.UnixNano(), 10)
	return strings.NewReplacer(
		`"conversationId":"cid-77"`, `"conversationId":"cid-`+nonce+`"`,
		`"createAt":1700000000000`, `"createAt":`+strconv.FormatInt(now.UnixMilli(), 10),
	).Replace(dtRobotPlainMsgJSON)
}

// newDingTalkTestAESKey 生成官方 43 位 EncodingAESKey（钉钉控制台给到的形态）。
func newDingTalkTestAESKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i*7 + 3)
	}
	return strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
}

func dingTalkTestKey(t *testing.T, encodingAESKey string) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		t.Fatalf("官方 43 位 EncodingAESKey 应可补 = 后 base64 解码: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key 长度应为 32，got %d", len(key))
	}
	return key
}

// dtOfficialEncrypt 按官方布局加密事件订阅回调内容：random(16)+len(4BE)+msg+receiveId，
// PKCS#7 补齐，IV 取密钥前 16 字节。
func dtOfficialEncrypt(t *testing.T, encodingAESKey, msg, receiveID string) string {
	t.Helper()
	key := dingTalkTestKey(t, encodingAESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	buf := make([]byte, 16)
	for i := range buf {
		buf[i] = byte(0xA0 + i)
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(msg)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, msg...)
	buf = append(buf, receiveID...)
	pad := aes.BlockSize - len(buf)%aes.BlockSize
	buf = append(buf, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(ct, buf)
	return base64.StdEncoding.EncodeToString(ct)
}

// dtOfficialEventSign 事件订阅 query 验签：sha1(sort(token,timestamp,nonce,encrypt))。
func dtOfficialEventSign(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sortStrings(parts)
	return sha1Hex([]byte(strings.Join(parts, "")))
}

// setupDingTalkInbound 建账号 + 接一条带 fake AI 触发器的入站管线，返回服务与账号 ID。
func setupDingTalkInbound(t *testing.T, acc *model.DingTalkAppAccount) (*DingTalkAppService, *fakeAITrigger, uint) {
	t.Helper()
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	db := testutil.NewTestDBOrSkip(t, &model.DingTalkAppAccount{}, &model.MessageHub{})
	acc.UserID = 1
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create dingtalk account: %v", err)
	}
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	ingress := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	ingress.SetAITrigger(tr)
	return NewDingTalkAppService(db, &WebhookService{ingressSvc: ingress}), tr, acc.ID
}

func dtRobotHeaders(secret string, ts int64) map[string]string {
	return map[string]string{"timestamp": strconv.FormatInt(ts, 10), "sign": dingtalkSign(secret, ts)}
}

// ---------------------------------------------------------------------------
// ① 机器人明文回调
// ---------------------------------------------------------------------------

func TestDingTalkInbound_RobotPlainCallbackHappyPath(t *testing.T) {
	svc, tr, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-plain", AppSecret: "SECrobot", InboundEnabled: true, Status: 1,
	})
	headers := dtRobotHeaders("SECrobot", time.Now().UnixMilli())
	if err := svc.ReceiveMessage(context.Background(), id, []byte(dtLiveRobotMsg(t)), nil, headers); err != nil {
		t.Fatalf("官方明文机器人回调（合法 sign/timestamp）必须被接受，got %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("应触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastCust != "staff-9" {
		t.Errorf("发送者应归一到 senderStaffId，got %q", tr.lastCust)
	}
	if tr.lastMeta == nil || tr.lastMeta.SessionWebhook != "https://oapi.dingtalk.com/robot/sendBySession?session=abc123" {
		t.Fatalf("sessionWebhook 必须透传到出站链: %+v", tr.lastMeta)
	}
}

func TestDingTalkInbound_RobotPlainCallbackUnsignedRejected(t *testing.T) {
	svc, tr, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-nosig", AppSecret: "SECrobot", InboundEnabled: true, Status: 1,
	})
	err := svc.ReceiveMessage(context.Background(), id, []byte(dtRobotPlainMsgJSON), nil, map[string]string{})
	if err == nil {
		t.Fatal("缺少 sign/timestamp 的明文回调必须拒绝：否则任何人知道回调地址即可伪造客户消息驱动 AI")
	}
	if tr.called != 0 {
		t.Errorf("被拒请求不得触发 AI，called=%d", tr.called)
	}
}

func TestDingTalkInbound_RobotPlainCallbackWrongSecretRejected(t *testing.T) {
	svc, _, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-badsig", AppSecret: "SECrobot", InboundEnabled: true, Status: 1,
	})
	headers := dtRobotHeaders("SEC-other", time.Now().UnixMilli())
	if err := svc.ReceiveMessage(context.Background(), id, []byte(dtRobotPlainMsgJSON), nil, headers); err == nil {
		t.Fatal("用别的 appSecret 签出的 sign 必须验签失败")
	}
}

func TestDingTalkInbound_RobotPlainCallbackStaleTimestampRejected(t *testing.T) {
	svc, _, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-stale", AppSecret: "SECrobot", InboundEnabled: true, Status: 1,
	})
	stale := time.Now().Add(-2 * time.Hour).UnixMilli()
	headers := dtRobotHeaders("SECrobot", stale)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(dtRobotPlainMsgJSON), nil, headers); err == nil {
		t.Fatal("官方要求 timestamp 与本地时间差 < 1 小时，2 小时前的合法签名必须拒绝（防重放）")
	}
}

func TestDingTalkInbound_RobotPlainCallbackWithoutAppSecretFailsClosed(t *testing.T) {
	svc, _, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-nosecret", AppSecret: "", InboundEnabled: true, Status: 1,
	})
	headers := map[string]string{
		"timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10),
		"sign":      "irrelevant-without-secret",
	}
	err := svc.ReceiveMessage(context.Background(), id, []byte(dtRobotPlainMsgJSON), nil, headers)
	if err == nil {
		t.Fatal("appSecret 未配置时必须 fail-closed（不能因为无从验签就放行）")
	}
	if !strings.Contains(err.Error(), "app_secret") {
		t.Errorf("错误信息应指向 app_secret 未配置，got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ② 事件订阅加密回调
// ---------------------------------------------------------------------------

func TestDingTalkInbound_EventSubscriptionOfficialLayoutDecrypts(t *testing.T) {
	aesKey := newDingTalkTestAESKey(t)
	svc, tr, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-enc", AppSecret: "SECapp", Token: "evt-token", AESKey: aesKey,
		InboundEnabled: true, Status: 1,
	})
	enc := dtOfficialEncrypt(t, aesKey, dtLiveRobotMsg(t), "dingCorpIdXYZ")
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := "nonce-1"
	query := map[string]string{
		"signature": dtOfficialEventSign("evt-token", ts, nonce, enc),
		"timestamp": ts, "nonce": nonce,
	}
	env, _ := json.Marshal(map[string]string{"encrypt": enc})
	if err := svc.ReceiveMessage(context.Background(), id, env, query, nil); err != nil {
		t.Fatalf("官方布局（random16+len4+msg+receiveId）的事件订阅回调必须能解密并解析，got %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("应触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastCust != "staff-9" {
		t.Errorf("解析结果错位（说明未正确剥去 20 字节头 / 尾部 receiveId）：sender=%q", tr.lastCust)
	}
}

func TestDingTalkInbound_EventSubscriptionUnsignedEnvelopeRejected(t *testing.T) {
	aesKey := newDingTalkTestAESKey(t)
	svc, tr, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-enc-nosig", AppSecret: "SECapp", Token: "evt-token", AESKey: aesKey,
		InboundEnabled: true, Status: 1,
	})
	enc := dtOfficialEncrypt(t, aesKey, dtRobotPlainMsgJSON, "dingCorpIdXYZ")
	env, _ := json.Marshal(map[string]string{"encrypt": enc})
	if err := svc.ReceiveMessage(context.Background(), id, env, map[string]string{}, nil); err == nil {
		t.Fatal("缺 signature 的加密回调必须拒绝：能解密不等于来源可信")
	}
	if tr.called != 0 {
		t.Errorf("被拒请求不得触发 AI，called=%d", tr.called)
	}
}

func TestDingTalkInbound_EventSubscriptionBadSignatureRejected(t *testing.T) {
	aesKey := newDingTalkTestAESKey(t)
	svc, _, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-enc-badsig", AppSecret: "SECapp", Token: "evt-token", AESKey: aesKey,
		InboundEnabled: true, Status: 1,
	})
	enc := dtOfficialEncrypt(t, aesKey, dtRobotPlainMsgJSON, "dingCorpIdXYZ")
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	query := map[string]string{"signature": strings.Repeat("0", 40), "timestamp": ts, "nonce": "n"}
	env, _ := json.Marshal(map[string]string{"encrypt": enc})
	if err := svc.ReceiveMessage(context.Background(), id, env, query, nil); err == nil {
		t.Fatal("签名不匹配的加密回调必须拒绝")
	}
}

func TestDingTalkInbound_InboundDisabledRejected(t *testing.T) {
	svc, _, id := setupDingTalkInbound(t, &model.DingTalkAppAccount{
		AppKey: "ak-off", AppSecret: "SECrobot", InboundEnabled: false, Status: 1,
	})
	err := svc.ReceiveMessage(context.Background(), id, []byte(dtRobotPlainMsgJSON), nil,
		dtRobotHeaders("SECrobot", time.Now().UnixMilli()))
	if err == nil {
		t.Fatal("InboundEnabled=false 的账号不得收消息")
	}
}

// ---------------------------------------------------------------------------
// 出站：自定义机器人加签 URL 参数名须为官方 timestamp
// ---------------------------------------------------------------------------

func TestDingTalkSendRobot_SignQueryParamIsOfficialName(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	svc := NewDingTalkService()
	if _, err := svc.SendRobot(context.Background(), srv.URL+"|SECabc", "", "text", "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query %q: %v", rawQuery, err)
	}
	if _, bad := q["ts"]; bad {
		t.Errorf("官方无 ts 参数，钉钉会忽略未知参数并回 errcode=310000 签名不匹配: %q", rawQuery)
	}
	tsValue := q.Get("timestamp")
	if tsValue == "" {
		t.Fatalf("官方加签参数名为 timestamp，实际 query=%q", rawQuery)
	}
	if q.Get("sign") == "" {
		t.Fatalf("应携带 sign，实际 query=%q", rawQuery)
	}
	ms, err := strconv.ParseInt(tsValue, 10, 64)
	if err != nil {
		t.Fatalf("timestamp 必须是毫秒整数，got %q", tsValue)
	}
	if diff := time.Now().UnixMilli() - ms; diff < 0 || diff > 60_000 {
		t.Errorf("timestamp 应为当前毫秒，偏差 %dms", diff)
	}
}
