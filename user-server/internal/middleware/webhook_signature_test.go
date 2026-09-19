package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ─── 测试替身 ────────────────────────────────────────────────────────────────

// fakeSecretStore 模拟 system_config_kv：只按键查表，可选注入读错误。
type fakeSecretStore struct {
	mu     sync.Mutex
	values map[string]string
	err    error
	calls  []string
}

func newFakeSecrets(kv map[string]string) *fakeSecretStore {
	return &fakeSecretStore{values: kv}
}

func (f *fakeSecretStore) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	if f.err != nil {
		return "", f.err
	}
	return f.values[key], nil
}

func (f *fakeSecretStore) getKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeNonceStore 模拟 cache 的 SetNX：第一次 true、重复 false，可注入故障。
type fakeNonceStore struct {
	mu       sync.Mutex
	seen     map[string]bool
	fail     error
	ttlSeen  []time.Duration
	setCalls int
}

func newFakeNonces() *fakeNonceStore {
	return &fakeNonceStore{seen: map[string]bool{}}
}

func (f *fakeNonceStore) SetNX(_ context.Context, key string, _ any, expiration time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	f.ttlSeen = append(f.ttlSeen, expiration)
	if f.fail != nil {
		return false, f.fail
	}
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

func (f *fakeNonceStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func (f *fakeNonceStore) ttls() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.ttlSeen...)
}

// ─── 测试脚手架 ──────────────────────────────────────────────────────────────

const (
	testWebhookSecret = "unit-test-key"
	testWebhookPrev   = "unit-test-key-old"
	testOrderBody     = `{"order_id":"O-2026-0919-01","status":"paid","amount":199.00}`
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

var webhookTestNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// sign 复刻对接方应有的算法：HMAC-SHA256(secret, "platform\nts\nnonce\nbody")。
// 这里独立实现一遍而不调用被测代码的 hmacSHA256Raw，否则实现写错时测试会跟着一起错。
func sign(t *testing.T, secret, platform, ts, nonce string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(platform + "\n" + ts + "\n" + nonce + "\n")); err != nil {
		t.Fatalf("签名计算失败：%v", err)
	}
	if _, err := mac.Write(body); err != nil {
		t.Fatalf("签名计算失败：%v", err)
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// webhookHarness 把 guard 挂到与生产同一条路径上，下游 handler 按控制器的方式绑定 JSON。
type webhookHarness struct {
	t        *testing.T
	engine   *gin.Engine
	guard    *OrderWebhookGuard
	secrets  *fakeSecretStore
	nonces   *fakeNonceStore
	downSeen []string
}

func newWebhookHarness(t *testing.T, opts ...OrderWebhookGuardOption) *webhookHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &webhookHarness{t: t, secrets: newFakeSecrets(defaultTestSecrets()), nonces: newFakeNonces()}
	opts = append([]OrderWebhookGuardOption{
		WithWebhookClock(fixedClock(webhookTestNow)),
		WithWebhookNonceStore(h.nonces),
	}, opts...)
	guard := NewOrderWebhookGuard(h.secrets, opts...)

	r := gin.New()
	// 与 internal/router 完全同一路径形状：中间件依赖 :platform 参数，路径写歪了测不出来。
	public := r.Group("/api")
	public.POST("/integration/webhook/order/:"+OrderWebhookPlatformParam, guard.Middleware(), func(c *gin.Context) {
		var payload map[string]any
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "下游绑定失败：" + err.Error()})
			return
		}
		verified := c.GetString(VerifiedWebhookKey)
		h.downSeen = append(h.downSeen, verified+"|"+fmt.Sprint(payload["order_id"]))
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
	})
	h.engine = r
	h.guard = guard
	return h
}

func defaultTestSecrets() map[string]string {
	return map[string]string{
		webhookSecretKeyPrefix + "taobao":                           testWebhookSecret,
		webhookSecretKeyPrefix + "taobao" + webhookSecretPrevSuffix: testWebhookPrev,
	}
}

// do 发一次带签名头的回调；ts/nonce/sig 传空表示"故意不发那个头"。
func (h *webhookHarness) do(platform, ts, nonce, sig string, body []byte) *http.Response {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integration/webhook/order/"+platform, strings.NewReader(string(body)))
	if ts != "" {
		req.Header.Set(WebhookTimestampHeader, ts)
	}
	if nonce != "" {
		req.Header.Set(WebhookNonceHeader, nonce)
	}
	if sig != "" {
		req.Header.Set(WebhookSignatureHeader, sig)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.engine.ServeHTTP(w, req)
	return w.Result()
}

func (h *webhookHarness) signedReq(platform, nonce string, body []byte, secret string) (string, string) {
	h.t.Helper()
	ts := strconv.FormatInt(webhookTestNow.Unix(), 10)
	return ts, sign(h.t, secret, platform, ts, nonce, body)
}

func readMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败：%v", err)
	}
	return string(raw)
}

// ─── AC① 合法签名放行 ──────────────────────────────────────────────────────

func TestOrderWebhookGuard_AcceptsValidSignature(t *testing.T) {
	cases := []struct {
		name   string
		nonce  string
		secret string
		style  string // bare | prefixed | upper | padded
	}{
		{"裸 hex", "nonce-bare", testWebhookSecret, "bare"},
		{"sha256= 前缀（GitHub/Stripe 同款）", "nonce-prefixed", testWebhookSecret, "prefixed"},
		{"大写 hex", "nonce-upper", testWebhookSecret, "upper"},
		{"前后空白", "nonce-padded", testWebhookSecret, "padded"},
		{"轮换期旧密钥（_prev 候选）", "nonce-prev", testWebhookPrev, "bare"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWebhookHarness(t)
			body := []byte(testOrderBody)
			ts, sig := h.signedReq("taobao", tc.nonce, body, tc.secret)
			switch tc.style {
			case "prefixed":
				sig = "sha256=" + sig
			case "upper":
				sig = strings.ToUpper(sig)
			case "padded":
				sig = "  " + sig + "\t"
			}
			resp := h.do("taobao", ts, tc.nonce, sig, body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("合法签名应 200，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
			}
			resp.Body.Close()
			if len(h.downSeen) != 1 {
				t.Fatalf("下游应被调用 1 次，实际 %d", len(h.downSeen))
			}
			if got := h.downSeen[0]; got != "taobao|O-2026-0919-01" {
				t.Errorf("下游看到的已验平台/请求体不对：%q（期望 taobao|O-2026-0919-01）⇒ 说明签名后没有把 body 原样交还", got)
			}
		})
	}
}

func TestOrderWebhookGuard_NonceTTLCoversTwoSkews(t *testing.T) {
	h := newWebhookHarness(t, WithWebhookSkew(120*time.Second))
	body := []byte(testOrderBody)
	nonce := "nonce-ttl-check"
	ts, sig := h.signedReq("taobao", nonce, body, testWebhookSecret)
	resp := h.do("taobao", ts, nonce, sig, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
	ttl := h.nonces.ttls()
	if len(ttl) != 1 {
		t.Fatalf("应登记 1 次 nonce，实际 %d", len(ttl))
	}
	// 口径 2：nonce TTL 必须是窗口的 2 倍，否则"已逐出但仍在窗内"会留下空隙。
	if ttl[0] != 240*time.Second {
		t.Errorf("nonce TTL = %s，期望 2×窗口 = 240s", ttl[0])
	}
}

// ─── AC② 三类坏例逐个命中 ──────────────────────────────────────────────────

func TestOrderWebhookGuard_RejectsWrongSignature(t *testing.T) {
	h := newWebhookHarness(t)
	body := []byte(testOrderBody)
	ts := strconv.FormatInt(webhookTestNow.Unix(), 10)
	nonce := "nonce-badsig"

	other := sign(t, "wrong-secret-entirely", "taobao", ts, nonce, body)
	resp := h.do("taobao", ts, nonce, other, body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错签应 401，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	msg := readMessage(t, resp)
	if !strings.Contains(msg, "签名校验失败") {
		t.Errorf("应回明确的签名失败：%s", msg)
	}
	if strings.Contains(msg, testWebhookSecret) || strings.Contains(msg, other) {
		t.Errorf("响应不得回显密钥/期望签名：%s", msg)
	}
	if h.nonces.count() != 0 {
		t.Errorf("错签不得登记 nonce（否则任何人可用随机签名探测 nonce 空间）：%d", h.nonces.count())
	}
	if len(h.downSeen) != 0 {
		t.Errorf("错签不得到达下游：%v", h.downSeen)
	}

	// 签名字节被截断 / 非 hex 都必须拒绝，而不是 hex.Decode 出错后"当成不匹配"以外放行。
	for _, bad := range []string{other[:60], "not-hex-at-all", strings.Repeat("00", sha256.Size+1), "1234"} {
		resp := h.do("taobao", ts, "nonce-trunc-"+hexlite(bad), bad, body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("签名 %q 应 401，实际 %d", bad, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestOrderWebhookGuard_RejectsStaleAndFutureTimestamp(t *testing.T) {
	skew := 300 * time.Second
	cases := []struct {
		name string
		ts   time.Time
	}{
		{"窗口外（早于 301s）", webhookTestNow.Add(-301 * time.Second)},
		{"窗口外（未来 301s）", webhookTestNow.Add(301 * time.Second)},
		{"远在十年前", webhookTestNow.Add(-10 * 365 * 24 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWebhookHarness(t, WithWebhookSkew(skew))
			body := []byte(testOrderBody)
			ts := strconv.FormatInt(tc.ts.Unix(), 10)
			nonce := "nonce-ts-" + hexlite(tc.name)
			sig := sign(t, testWebhookSecret, "taobao", ts, nonce, body)
			resp := h.do("taobao", ts, nonce, sig, body)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("时间戳出窗应 401，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
			}
			resp.Body.Close()
			if len(h.downSeen) != 0 {
				t.Errorf("出窗请求不得到达下游")
			}
		})
	}

	// 边界内一侧必须通过：把"过期就全拒"和"按窗口判"区分开。
	h := newWebhookHarness(t, WithWebhookSkew(skew))
	body := []byte(testOrderBody)
	ts := strconv.FormatInt(webhookTestNow.Add(-299*time.Second).Unix(), 10)
	nonce := "nonce-in-window"
	resp := h.do("taobao", ts, nonce, sign(t, testWebhookSecret, "taobao", ts, nonce, body), body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("窗口内（-299s）应 200，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
}

func TestOrderWebhookGuard_RejectsReplay(t *testing.T) {
	h := newWebhookHarness(t)
	body := []byte(testOrderBody)
	ts, sig := h.signedReq("taobao", "nonce-replay", body, testWebhookSecret)

	first := h.do("taobao", ts, "nonce-replay", sig, body)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("首次应 200，实际 %d：%s", first.StatusCode, readMessage(t, first))
	}
	first.Body.Close()

	second := h.do("taobao", ts, "nonce-replay", sig, body)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("重放应 409，实际 %d：%s", second.StatusCode, readMessage(t, second))
	}
	msg := readMessage(t, second)
	if !strings.Contains(msg, "重放") {
		t.Errorf("应回重放原因：%s", msg)
	}
	if len(h.downSeen) != 1 {
		t.Errorf("重放不得二次落库，下游被调 %d 次", len(h.downSeen))
	}

	// 换 nonce 必须放行：证明拦的是 nonce，不是这条路径本身。
	thirdTS, thirdSig := h.signedReq("taobao", "nonce-replay-2", body, testWebhookSecret)
	resp := h.do("taobao", thirdTS, "nonce-replay-2", thirdSig, body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("新 nonce 应 200，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
}

// TestOrderWebhookGuard_BadRequestShape 覆盖 AC② 之外的畸形输入：都是 4xx，且不许到下游。
func TestOrderWebhookGuard_BadRequestShape(t *testing.T) {
	body := []byte(testOrderBody)
	goodTS := strconv.FormatInt(webhookTestNow.Unix(), 10)
	goodSig := func(n string) string { return sign(t, testWebhookSecret, "taobao", goodTS, n, body) }

	cases := []struct {
		name     string
		platform string
		ts       string
		nonce    string
		sig      string
		body     []byte
		want     int
		contains string
	}{
		{"缺三个头", "taobao", "", "", "", body, http.StatusBadRequest, WebhookTimestampHeader},
		{"只缺签名", "taobao", goodTS, "nonce-abcdef", "", body, http.StatusBadRequest, WebhookSignatureHeader},
		{"nonce 太短", "taobao", goodTS, "abc", goodSig("abc"), body, http.StatusBadRequest, "nonce"},
		{"nonce 含非法字符", "taobao", goodTS, "nonce/../..", goodSig("nonce/../.."), body, http.StatusBadRequest, "nonce"},
		{"时间戳非数字", "taobao", "not-a-number", "nonce-abcdef", sign(t, testWebhookSecret, "taobao", "not-a-number", "nonce-abcdef", body), body, http.StatusBadRequest, WebhookTimestampHeader},
		{"时间戳为 0", "taobao", "0", "nonce-abcdef", sign(t, testWebhookSecret, "taobao", "0", "nonce-abcdef", body), body, http.StatusBadRequest, WebhookTimestampHeader},
		{"平台名含大写", "TaoBao", goodTS, "nonce-abcdef", goodSig("nonce-abcdef"), body, http.StatusBadRequest, "platform"},
		{"平台名以短横线开头", "-evil", goodTS, "nonce-abcdef", goodSig("nonce-abcdef"), body, http.StatusBadRequest, "platform"},
		{"平台名超长", strings.Repeat("a", 33), goodTS, "nonce-abcdef", goodSig("nonce-abcdef"), body, http.StatusBadRequest, "platform"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWebhookHarness(t)
			resp := h.do(tc.platform, tc.ts, tc.nonce, tc.sig, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("期望 %d，实际 %d：%s", tc.want, resp.StatusCode, readMessage(t, resp))
			}
			msg := readMessage(t, resp)
			if !strings.Contains(msg, tc.contains) {
				t.Errorf("响应应包含 %q（让对接方自己修得动），实际：%s", tc.contains, msg)
			}
			if len(h.downSeen) != 0 {
				t.Errorf("畸形请求不得到达下游")
			}
		})
	}
}

func TestOrderWebhookGuard_RejectsOversizedBody(t *testing.T) {
	h := newWebhookHarness(t, WithWebhookMaxBody(1024))
	big := []byte(`{"order_id":"` + strings.Repeat("x", 4096) + `"}`)
	ts, sig := h.signedReq("taobao", "nonce-bigbody", big, testWebhookSecret)
	resp := h.do("taobao", ts, "nonce-bigbody", sig, big)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大请求体应 413，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
	if len(h.downSeen) != 0 {
		t.Errorf("超大体不得转交下游")
	}
}

// TestOrderWebhookGuard_TimestampCheckedBeforeBody 钉住顺序：过期请求没必要为它读满一个 4MB 体。
func TestOrderWebhookGuard_TimestampCheckedBeforeBody(t *testing.T) {
	h := newWebhookHarness(t, WithWebhookMaxBody(minWebhookMaxBody))
	big := []byte(`{"order_id":"` + strings.Repeat("y", 4096) + `"}`)
	ts := strconv.FormatInt(webhookTestNow.Add(-time.Hour).Unix(), 10)
	resp := h.do("taobao", ts, "nonce-order-check", sign(t, testWebhookSecret, "taobao", ts, "nonce-order-check", big), big)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("时效先于体积：应 401，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
	if h.nonces.count() != 0 {
		t.Errorf("过期请求不应登记 nonce")
	}
}

// ─── 密钥缺失 / 读库故障：503，且与 401 区分 ─────────────────────────────────

func TestOrderWebhookGuard_NoSecretConfiguredFailsClosed(t *testing.T) {
	h := newWebhookHarness(t)
	h.secrets.mu.Lock()
	h.secrets.values = map[string]string{"some-other-key": "x"}
	h.secrets.mu.Unlock()

	body := []byte(testOrderBody)
	ts, sig := h.signedReq("taobao", "nonce-nosecret", body, testWebhookSecret)
	resp := h.do("taobao", ts, "nonce-nosecret", sig, body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未配密钥应 503（本侧故障，不是对方的签名错），实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	msg := readMessage(t, resp)
	if !strings.Contains(msg, webhookSecretKeyPrefix+"taobao") {
		t.Errorf("503 应指到那个 kv 键，方便运维一行配置修好：%s", msg)
	}
	if len(h.downSeen) != 0 {
		t.Errorf("无密钥时不得放行任何请求（新路径没有影子档）")
	}
}

func TestOrderWebhookGuard_SecretStoreErrorIsAlsoFailClosed(t *testing.T) {
	h := newWebhookHarness(t)
	h.secrets.mu.Lock()
	h.secrets.err = errors.New("connection reset by peer")
	h.secrets.mu.Unlock()

	body := []byte(testOrderBody)
	ts, sig := h.signedReq("taobao", "nonce-kverr", body, testWebhookSecret)
	resp := h.do("taobao", ts, "nonce-kverr", sig, body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("kv 读故障应 fail-closed 503，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
	keys := h.secrets.getKeys()
	if len(keys) != 2 || keys[0] != webhookSecretKeyPrefix+"taobao" {
		t.Errorf("应依次读 当前/轮换 两个键，实际：%v", keys)
	}
}

// TestOrderWebhookGuard_RotationKeysAreBothConsulted 证明轮换不是"改了就把对接方打死"。
func TestOrderWebhookGuard_RotationKeysAreBothConsulted(t *testing.T) {
	h := newWebhookHarness(t)
	body := []byte(testOrderBody)
	// 主密钥已换成新值，对接方还没切 ⇒ 只能靠 _prev 候选接住。
	h.secrets.mu.Lock()
	h.secrets.values[webhookSecretKeyPrefix+"taobao"] = "brand-new-key"
	h.secrets.mu.Unlock()

	ts, sig := h.signedReq("taobao", "nonce-rot-1", body, testWebhookPrev)
	resp := h.do("taobao", ts, "nonce-rot-1", sig, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("轮换期旧密钥签名应仍放行，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()

	// 主密钥为空时也必须接受 _prev（半配置状态不该把对接方打死）。
	h2 := newWebhookHarness(t)
	h2.secrets.mu.Lock()
	h2.secrets.values = map[string]string{
		webhookSecretKeyPrefix + "taobao":                           "   ",
		webhookSecretKeyPrefix + "taobao" + webhookSecretPrevSuffix: testWebhookPrev,
	}
	h2.secrets.mu.Unlock()
	ts2, sig2 := h2.signedReq("taobao", "nonce-rot-2", body, testWebhookPrev)
	if resp := h2.do("taobao", ts2, "nonce-rot-2", sig2, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("主密钥为空白时应回退 _prev，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	} else {
		resp.Body.Close()
	}
}

// ─── nonce 存储故障：默认 fail-open，严格模式 fail-closed ────────────────────

func TestOrderWebhookGuard_NonceStoreOutageFailsOpen(t *testing.T) {
	h := newWebhookHarness(t)
	h.nonces.fail = errors.New("redis: dial tcp: connection refused")

	body := []byte(testOrderBody)
	ts, sig := h.signedReq("taobao", "nonce-outage", body, testWebhookSecret)
	resp := h.do("taobao", ts, "nonce-outage", sig, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("nonce 故障默认应 fail-open（签名已验证，订单镜像更贵），实际 %d：%s",
			resp.StatusCode, readMessage(t, resp))
	}
	resp.Body.Close()
	if got := h.guard.NonceOutages(); got != 1 {
		t.Errorf("故障必须被计数，NonceOutages = %d，期望 1", got)
	}
}

func TestOrderWebhookGuard_NonceStoreOutageStrictMode(t *testing.T) {
	h := newWebhookHarness(t, WithWebhookStrictNonce(true))
	h.nonces.fail = errors.New("redis: dial tcp: connection refused")

	body := []byte(testOrderBody)
	ts, sig := h.signedReq("taobao", "nonce-strict", body, testWebhookSecret)
	resp := h.do("taobao", ts, "nonce-strict", sig, body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("严格模式下 nonce 故障应拒绝，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	}
	msg := readMessage(t, resp)
	if !strings.Contains(msg, "严格模式") {
		t.Errorf("应说明是严格模式所致：%s", msg)
	}
	if got := h.guard.NonceOutages(); got != 1 {
		t.Errorf("严格模式也要计数：%d", got)
	}
	if len(h.downSeen) != 0 {
		t.Errorf("严格模式拒绝后不得到下游")
	}
}

// ─── 跨平台复用签名必须失效（平台名进签名的意义） ────────────────────────────

func TestOrderWebhookGuard_SignatureIsBoundToPlatform(t *testing.T) {
	h := newWebhookHarness(t)
	h.secrets.mu.Lock()
	// 两家平台共用同一密钥 —— 现实中完全可能（同一家服务商代管）。
	h.secrets.values[webhookSecretKeyPrefix+"jd"] = testWebhookSecret
	h.secrets.values[webhookSecretKeyPrefix+"jd"+webhookSecretPrevSuffix] = testWebhookPrev
	h.secrets.mu.Unlock()

	body := []byte(testOrderBody)
	ts := strconv.FormatInt(webhookTestNow.Unix(), 10)
	nonce := "nonce-cross"
	sig := sign(t, testWebhookSecret, "taobao", ts, nonce, body)

	if resp := h.do("taobao", ts, nonce, sig, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("原平台应 200，实际 %d：%s", resp.StatusCode, readMessage(t, resp))
	} else {
		resp.Body.Close()
	}
	cross := h.do("jd", ts, "nonce-cross-2", sig, body)
	if cross.StatusCode != http.StatusUnauthorized {
		t.Fatalf("把 taobao 的签名搬到 jd 应 401，实际 %d：%s", cross.StatusCode, readMessage(t, cross))
	}
	cross.Body.Close()
	if len(h.downSeen) != 1 {
		t.Errorf("跨平台搬运不得落两次下游：%v", h.downSeen)
	}
}

// ─── 配置边界 ────────────────────────────────────────────────────────────────

func TestNewOrderWebhookGuard_ClampsSkew(t *testing.T) {
	cases := []struct{ in, want time.Duration }{
		{time.Millisecond, defaultWebhookSkew}, // 低于下限 ⇒ 保持默认（0 窗口等于全拒）
		{time.Hour, maxWebhookSkew},            // 超上限 ⇒ 收敛
		{60 * time.Second, 60 * time.Second},   // 合法 ⇒ 生效
	}
	for _, tc := range cases {
		guard := NewOrderWebhookGuard(newFakeSecrets(nil), WithWebhookSkew(tc.in))
		if guard.skew != tc.want {
			t.Errorf("WithWebhookSkew(%s) ⇒ skew = %s，期望 %s", tc.in, guard.skew, tc.want)
		}
	}
}

func TestNewOrderWebhookGuard_EnvironmentOverrides(t *testing.T) {
	t.Setenv("ORDER_WEBHOOK_MAX_SKEW_SECONDS", "45")
	t.Setenv("ORDER_WEBHOOK_MAX_BODY_BYTES", "8192")
	t.Setenv("ORDER_WEBHOOK_NONCE_STRICT", "ON")
	guard := NewOrderWebhookGuard(newFakeSecrets(nil))
	if guard.skew != 45*time.Second {
		t.Errorf("skew = %s，期望 45s", guard.skew)
	}
	if guard.maxBody != 8192 {
		t.Errorf("maxBody = %d，期望 8192", guard.maxBody)
	}
	if !guard.strictNonce {
		t.Error("NONCE_STRICT=ON（大小写不敏感）应开启严格模式")
	}

	t.Setenv("ORDER_WEBHOOK_MAX_SKEW_SECONDS", "abc")
	t.Setenv("ORDER_WEBHOOK_MAX_BODY_BYTES", "1") // 低于下限
	t.Setenv("ORDER_WEBHOOK_NONCE_STRICT", "maybe")
	guard2 := NewOrderWebhookGuard(newFakeSecrets(nil))
	if guard2.skew != defaultWebhookSkew {
		t.Errorf("非法窗口值应回退默认：%s", guard2.skew)
	}
	if guard2.maxBody != defaultWebhookMaxBody {
		t.Errorf("越界体积应回退默认：%d", guard2.maxBody)
	}
	if guard2.strictNonce {
		t.Error("无法识别的开关值不应开启严格模式")
	}
}

// ─── 纯函数与旧路径告示 ─────────────────────────────────────────────────────

func TestValidWebhookPlatformAndNonce(t *testing.T) {
	for _, p := range []string{"taobao", "jd", "p1", "a_b-c", "0start", strings.Repeat("a", 32)} {
		if !validWebhookPlatform(p) {
			t.Errorf("platform %q 应合法", p)
		}
	}
	for _, p := range []string{"", "A", "-x", "_x", "a/b", "a..b", "a b", "平台", strings.Repeat("a", 33), "a\nb"} {
		if validWebhookPlatform(p) {
			t.Errorf("platform %q 应被拒（它要参与拼 kv/nonce 键）", p)
		}
	}
	for _, n := range []string{"12345678", "abcdef-123_x", strings.Repeat("x", 64)} {
		if !validWebhookNonce(n) {
			t.Errorf("nonce %q 应合法", n)
		}
	}
	for _, n := range []string{"", "1234567", strings.Repeat("x", 65), "a b", "a:b", "../x", " nonce"} {
		if validWebhookNonce(n) {
			t.Errorf("nonce %q 应被拒", n)
		}
	}
}

func TestCanonicalWebhookPayload_Format(t *testing.T) {
	got := string(CanonicalWebhookPayload("taobao", "1700000000", "n0nce", []byte(`{"a":1}`)))
	want := "taobao\n1700000000\nn0nce\n{\"a\":1}"
	if got != want {
		t.Errorf("签名串格式变了会静默打断所有对接方：\n got=%q\nwant=%q", got, want)
	}
}

func TestSignatureMatchesAny_RejectsMalformedInput(t *testing.T) {
	payload := []byte("x")
	for _, got := range []string{"", "  ", "zz", "deadbeef", "sha256=zz", "0x" + strings.Repeat("00", sha256.Size)} {
		if signatureMatchesAny([]string{"k"}, payload, got) {
			t.Errorf("签名 %q 不应匹配", got)
		}
	}
	if signatureMatchesAny(nil, payload, strings.Repeat("00", sha256.Size)) {
		t.Error("无候选密钥时必须不匹配")
	}
}

func TestLegacyDeprecationNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var hit int
	auth := r.Group("/api")
	auth.POST("/integration/order-webhook/:"+OrderWebhookPlatformParam,
		LegacyDeprecationNotice(orderWebhookSuccessorPrefixForTest),
		func(c *gin.Context) { hit++; c.JSON(http.StatusOK, gin.H{"code": 0}) })

	req := httptest.NewRequest(http.MethodPost, "/api/integration/order-webhook/taobao", strings.NewReader(testOrderBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("旧路径仍要照常服务，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get(DeprecatedWebhookHeader); got != "true" {
		t.Errorf("Deprecation 头 = %q，期望 true", got)
	}
	link := resp.Header.Get("Link")
	if !strings.Contains(link, `rel="successor-version"`) {
		t.Errorf("Link 头应给出替代路径：%s", link)
	}
	if !strings.Contains(link, "/api/integration/webhook/order/taobao>") {
		t.Errorf("Link 应把 :platform 实参带进替代路径，实际：%s", link)
	}
	resp.Body.Close()
	if hit != 1 {
		t.Errorf("告示中间件不得拦掉旧路径的调用：hit=%d", hit)
	}
}

// orderWebhookSuccessorPrefixForTest 与 router 里的常量同值；一致性由 router 侧的
// TestOrderWebhook_SuccessorHeaderPointsToLiveRoute 真发一次请求钉住。
const orderWebhookSuccessorPrefixForTest = "/api/integration/webhook/order/"

func TestEnvHelpers(t *testing.T) {
	t.Setenv("EDGE_SKEW", "")
	if got := envDurationOr("EDGE_SKEW", time.Second, 30*time.Second, time.Second, time.Minute); got != 30*time.Second {
		t.Errorf("空值应回退默认：%s", got)
	}
	t.Setenv("EDGE_SKEW", "20")
	if got := envDurationOr("EDGE_SKEW", time.Second, 30*time.Second, time.Second, time.Minute); got != 20*time.Second {
		t.Errorf("合法值应生效：%s", got)
	}
	if got := envInt64Or("EDGE_MISSING", 4096, 1, 1<<20); got != 4096 {
		t.Errorf("缺失键应回退：%d", got)
	}
}

// hexlite 把用例名压成可放进 nonce 的短串。
func hexlite(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
