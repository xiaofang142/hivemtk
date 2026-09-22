package controller

// 批G-2：抖音 verify_webhook 的 challenge 回显必须在**真实路由**上测。
//
// 为什么不能只在 service 里测（批A/批D-04 两次踩过的同一个洞）：
// challenge 是抖音控制台上「保存回调地址」那一步的唯一门槛 —— 回显不对，
// URL 根本注册不上，后面的 im_receive_msg 一条都收不到。而回显要同时满足：
//   - 头能穿过控制器的取参白名单（X-Douyin-Signature）；
//   - 验签在回显**之前**（否则任何人都能拿这个端点当免费回声弹）；
//   - challenge 的 JSON 类型原样保留（官方示例是数字 12345，不是 "12345"）。
//
// 官方取证（A 档原文与字节数见审计文档 §16.1）：
// developer.open-douyin.com/docs/resource/zh-CN/dop/develop/webhooks/summarize
//
//	请求  {"event":"verify_webhook","client_key":"","content":{"challenge":12345}}
//	响应  {"challenge":12345}
//	「当你收到开放平台 POST 验证请求时，你需要解析出 challenge 值，并立即返回该 challenge 值作为响应」

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const gDyHTTPSecret = "dy_client_secret_http_batchg"

// gDyHTTPSign 独立造官方签名：hex(sha1(client_secret ‖ 原始 body))。
// 不复用被测实现里的 douyinSignature —— 拿实现的输出当夹具等于让实现给自己出题。
func gDyHTTPSign(secret string, body []byte) string {
	h := sha1.New()
	h.Write([]byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// gDyLegacyHTTPSign 复刻修复前的错误口径（HMAC-SHA256(body)），仅作反向锚点。
func gDyLegacyHTTPSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newGdyDouyinRoute(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	g, _, db := newWebhookRouteEnv(t)
	if err := db.Create(&model.IntegrationAccount{Platform: "douyin", APISecret: gDyHTTPSecret, Status: 1}).Error; err != nil {
		t.Fatalf("seed douyin account: %v", err)
	}
	return g, db
}

// TestWebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge 官方数字形态 challenge
// 必须原样回显（响应体就是 {"challenge":12345}）。
//
// 修复前：verify_webhook 走通用 Receive 漏斗，回的是 {"accepted":true,...} —— 抖音侧
// 拿不到 challenge，回调地址保存失败，整个渠道在注册这一步就死（HTTP 还是 200，看不出问题）。
func TestWebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge(t *testing.T) {
	g, _ := newGdyDouyinRoute(t)

	body := []byte(`{"event":"verify_webhook","client_key":"","content":{"challenge":12345}}`)
	rec := postWebhookHeaders(t, g, "/api/webhook/douyin/1", body,
		map[string]string{"X-Douyin-Signature": gDyHTTPSign(gDyHTTPSecret, body)})

	if rec.Code != http.StatusOK {
		t.Fatalf("合法 verify_webhook 必须 200，got %d %s", rec.Code, rec.Body.String())
	}
	// 逐字节比：多一个引号、多一个字段，抖音侧都可能判校验不过。
	if got := strings.TrimSpace(rec.Body.String()); got != `{"challenge":12345}` {
		t.Errorf("challenge 必须原样回显（数字不得变字符串、不得包外壳），got %s", got)
	}
}

// TestWebhookRoute_Douyin_ChallengeTypePreserved 官方文档别处也给过字符串形态的 challenge
// （以及带大数、前导零的写法）。这里用 json.Number 级别的「原文回显」而不是解成 Go 类型
// 再编回去：解成 float64 会让 19 位 ID 型 challenge 掉精度，解成 interface{} 会把
// 1e5 这种写法改成 "100000"。
func TestWebhookRoute_Douyin_ChallengeTypePreserved(t *testing.T) {
	g, _ := newGdyDouyinRoute(t)

	for _, want := range []string{`"abc-123"`, `12345`, `168130328599700001`, `"12345"`} {
		body := []byte(`{"event":"verify_webhook","client_key":"ck","content":{"challenge":` + want + `}}`)
		rec := postWebhookHeaders(t, g, "/api/webhook/douyin/1", body,
			map[string]string{"X-Douyin-Signature": gDyHTTPSign(gDyHTTPSecret, body)})
		if rec.Code != http.StatusOK {
			t.Errorf("challenge=%s 必须 200，got %d %s", want, rec.Code, rec.Body.String())
			continue
		}
		var echoed map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
			t.Errorf("响应必须是 JSON 对象（官方：text 格式的 json 数据），got %q err=%v", rec.Body.String(), err)
			continue
		}
		if got := strings.TrimSpace(string(echoed["challenge"])); got != want {
			t.Errorf("challenge 原文必须逐字节回显，want %s got %s", want, got)
		}
	}
}

// TestWebhookRoute_Douyin_VerifyWebhookRejectsUnsignedChallenge 反向：验签必须在回显之前。
// 否则这个端点就成了一个「谁都能拿到 challenge」的回声弹，而 challenge 本来是
// 用来证明「这个回调地址真的收到了这条带签名的请求」。
func TestWebhookRoute_Douyin_VerifyWebhookRejectsUnsignedChallenge(t *testing.T) {
	g, db := newGdyDouyinRoute(t)

	body := []byte(`{"event":"verify_webhook","client_key":"ck","content":{"challenge":98765}}`)

	cases := map[string]map[string]string{
		"完全没有签名头":          {},
		"签名与 body 不匹配":     {"X-Douyin-Signature": strings.Repeat("0", 40)},
		"旧 HMAC-SHA256 口径": {"X-Douyin-Signature": gDyLegacyHTTPSign(gDyHTTPSecret, body)},
	}
	for name, headers := range cases {
		rec := postWebhookHeaders(t, g, "/api/webhook/douyin/1", body, headers)
		if strings.Contains(rec.Body.String(), "98765") {
			t.Errorf("%s：未通过验签不得回显 challenge，got %d %s", name, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusOK {
			t.Errorf("%s：未通过验签不得回 200（抖音会以为注册成功），got %s", name, rec.Body.String())
		}
	}

	var n int64
	if err := db.Model(&model.MessageHub{}).Count(&n).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if n != 0 {
		t.Errorf("verify_webhook 不得落会话行，实际 %d 行", n)
	}
}

// TestWebhookRoute_Douyin_MessageEventNotSwallowedByChallengeBranch 正向回归：
// challenge 分支加在通用漏斗之前，不能把 im_receive_msg 一起吃掉（那是 G-3 的主路径）。
func TestWebhookRoute_Douyin_MessageEventNotSwallowedByChallengeBranch(t *testing.T) {
	g, db := newGdyDouyinRoute(t)

	body := []byte(`{"event":"im_receive_msg","client_key":"ck","from_user_id":"u-http-1","to_user_id":"bot",` +
		`"content":{"conversation_short_id":"@c-http-1","server_message_id":"@m-http-1",` +
		`"create_time":1681303285997,"message_type":"text","text":"真实客户消息"}}`)
	rec := postWebhookHeaders(t, g, "/api/webhook/douyin/1", body,
		map[string]string{"X-Douyin-Signature": gDyHTTPSign(gDyHTTPSecret, body)})

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"accepted":true`) {
		t.Fatalf("会话事件仍要走通用漏斗，got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "challenge") {
		t.Errorf("会话事件不得被 challenge 分支回显，got %s", rec.Body.String())
	}
	// 入站派发是队列异步的（Receive 只负责验签+落事件行+入队），所以这里必须轮询，
	// 不能"请求返回即断言"。轮询到 1 行为止；超时按当前值报错。
	var rows int64
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := db.Model(&model.MessageHub{}).Where("sender_id = ?", "u-http-1").Count(&rows).Error; err != nil {
			t.Fatalf("count hub: %v", err)
		}
		if rows >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if rows != 1 {
		t.Errorf("官方 im_receive_msg 经真实路由必须落 1 行，实际 %d 行", rows)
	}
}
