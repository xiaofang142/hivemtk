package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
)

// 订单回调验签中间件（R-3 / T-P2-02）。
//
// 为什么要换契约：`POST /api/integration/order-webhook/:platform` 原本挂在 auth 组里，
// 外部电商平台必须先登录本系统拿一个会话 JWT 才能推订单 —— 而电商平台不会替我们保管
// 会话凭证，实际能对接上的只有"给它开一个长期账号"这种更糟的做法。对外回调的惯例是
// 公开路径 + 每平台共享密钥的 HMAC 签名。
//
// 三条口径先说清楚，它们决定了很多看起来"过度严格"的实现：
//
//  1. **新路径没有影子档。** 一般接线类改动要 `off|shadow|enforce` 三态，是为了让
//     "改变现网行为"可以先只观察。这里恰恰反过来：在签名之前把路由公开出去，本身就
//     是本次要修的那个洞（今天这条路径至少还要一张会话票）。所以新路径一旦注册就
//     必须验签，无密钥 ⇒ 503（宁可回"我没配好"，绝不回"我先放着"）；灰度发生在
//     **对接方**那一侧：旧路径继续按 JWT 服务一个大版本，谁准备好了谁切。
//  2. **时间戳窗口的存在意义是给重放缓存划上界。** nonce 的 TTL 取 2×窗口 ⇒ 任何一条
//     nonce 已被逐出时，它对应的时间戳必然已经出窗、会被先判掉，中间不留空隙。
//  3. **验签失败的原因可以回给调用方，但不回"正确答案"。** 缺什么头、时间戳偏了多少
//     这些是对方自己能修的；不回显期望签名，也不把"平台没配密钥"和"密钥不对"混成
//     同一个 401（前者是我们的故障，要给运维一个能听见的码）。
//
// 本文件刻意不使用 `IsTestMode` 跳过：给安全中间件开后门，测试就只能验"后门开着"的样子。
// 用例一律注入自己的密钥源与 nonce 存储。

const (
	// WebhookSignatureHeader 签名头。允许 `sha256=<hex>` 前缀写法（GitHub/Stripe 同款），
	// 也允许裸 hex。
	WebhookSignatureHeader = "X-Webhook-Signature"
	// WebhookTimestampHeader Unix 秒；不是毫秒、不是 RFC3339 —— 回调方最容易写错的一项。
	WebhookTimestampHeader = "X-Webhook-Timestamp"
	// WebhookNonceHeader 一次性随机串，配合 nonce 存储做重放拦截。
	WebhookNonceHeader = "X-Webhook-Nonce"

	// OrderWebhookPlatformParam 路径参数名，控制器侧用的是同一个。
	OrderWebhookPlatformParam = "platform"

	defaultWebhookSkew        = 300 * time.Second
	minWebhookSkew            = 10 * time.Second
	maxWebhookSkew            = 15 * time.Minute
	defaultWebhookMaxBody     = int64(256 << 10)
	minWebhookMaxBody         = int64(1024)
	maxWebhookMaxBody         = int64(4 << 20)
	webhookNonceTTLMultiplier = 2

	// webhookSecretKeyPrefix / webhookSecretPrevSuffix：每平台一条密钥，放
	// system_config_kv（运行时可改、有管理端点、不入库代码仓库）。_prev 是轮换灰度位，
	// 与桥接通道凭证同款语义。
	webhookSecretKeyPrefix  = "order_webhook_secret_"
	webhookSecretPrevSuffix = "_prev"

	// webhookNonceKeyPrefix 与挽回 worker 的认领锁同一套命名。
	webhookNonceKeyPrefix = "mtk:wh:order:nonce:"

	// VerifiedWebhookKey 验签通过后写入 ctx，控制器据此二次核对"这条请求确实过了 guard"。
	VerifiedWebhookKey = "webhook_verified_platform"

	// OrderWebhookVerifiedPathPrefix 公开验签入口的路径前缀（含 /api，与注册时的完整路径一致）。
	//
	// 它是 guard 与控制器之间的唯一契约来源：控制器不假设"路由一定挂了中间件"，而是按这个
	// 前缀判定入口身份（见 IntegrationController.ReceiveOrderWebhook）。router 侧的同名常量
	// 由 TestOrderWebhook_BothPathsRegistered 钉成同值，漂了就红。
	OrderWebhookVerifiedPathPrefix = "/api/integration/webhook/order/"

	// DeprecatedWebhookHeader 旧路径的 deprecation 标记（HTTP 标准头）。
	DeprecatedWebhookHeader = "Deprecation"
)

// webhookSecretStore 只用到 Get 一个方法：密钥源可注入（测试用假实现）。
type webhookSecretStore interface {
	Get(ctx context.Context, key string) (string, error)
}

// webhookNonceStore 是 cache.Cache 的子集：SetNX 返回 true 即"这条 nonce 第一次见"。
type webhookNonceStore interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
}

// OrderWebhookGuard 订单回调验签器。零值不可用，请走 NewOrderWebhookGuard。
type OrderWebhookGuard struct {
	secrets     webhookSecretStore
	nonces      webhookNonceStore
	now         func() time.Time
	skew        time.Duration
	maxBody     int64
	strictNonce bool

	// nonceOutages / lastNonceWarn：nonce 存储故障时 fail-open，但必须出声，
	// 且要限速 —— Redis 抖动期间每个回调都刷一行日志会把真正的问题淹掉。
	nonceOutages  atomic.Int64
	lastNonceWarn atomic.Int64
}

// OrderWebhookGuardOption 装配期选项（环境变量之外，测试与特殊部署可覆盖）。
type OrderWebhookGuardOption func(*OrderWebhookGuard)

// WithWebhookSkew 覆盖时间戳容忍窗口。
func WithWebhookSkew(d time.Duration) OrderWebhookGuardOption {
	return func(g *OrderWebhookGuard) { g.applySkew(d) }
}

// WithWebhookMaxBody 覆盖请求体上限（字节）。
func WithWebhookMaxBody(n int64) OrderWebhookGuardOption {
	return func(g *OrderWebhookGuard) {
		if n >= minWebhookMaxBody && n <= maxWebhookMaxBody {
			g.maxBody = n
		}
	}
}

// WithWebhookClock 注入时钟：不固定"现在"就没法稳定地测"过期"。
func WithWebhookClock(now func() time.Time) OrderWebhookGuardOption {
	return func(g *OrderWebhookGuard) {
		if now != nil {
			g.now = now
		}
	}
}

// WithWebhookNonceStore 注入 nonce 存储（默认全局 cache；多副本部署要靠它才共享）。
func WithWebhookNonceStore(s webhookNonceStore) OrderWebhookGuardOption {
	return func(g *OrderWebhookGuard) {
		if s != nil {
			g.nonces = s
		}
	}
}

// WithWebhookStrictNonce 让 nonce 存储故障时判失败（默认 fail-open，理由见 checkReplay）。
func WithWebhookStrictNonce(on bool) OrderWebhookGuardOption {
	return func(g *OrderWebhookGuard) { g.strictNonce = on }
}

// applySkew 夹到 [minWebhookSkew, maxWebhookSkew]：窗口设成 0 等于所有回调都判过期，
// 设成一天则重放缓存要养一整天 —— 两种都是"配错比不配更危险"。
func (g *OrderWebhookGuard) applySkew(d time.Duration) {
	switch {
	case d < minWebhookSkew:
		logger.Warnf("[order-webhook] 时间戳窗口 %s 低于下限 %s ⇒ 沿用 %s", d, minWebhookSkew, g.skew)
	case d > maxWebhookSkew:
		logger.Warnf("[order-webhook] 时间戳窗口 %s 超过上限 %s ⇒ 收敛为 %s", d, maxWebhookSkew, maxWebhookSkew)
		g.skew = maxWebhookSkew
	default:
		g.skew = d
	}
}

// NewOrderWebhookGuard 构造验签器。secrets 传 nil 时用全局 system_config_kv 仓储。
func NewOrderWebhookGuard(secrets webhookSecretStore, opts ...OrderWebhookGuardOption) *OrderWebhookGuard {
	if secrets == nil {
		secrets = repository.NewSystemConfigKVRepository()
	}
	g := &OrderWebhookGuard{
		secrets:     secrets,
		nonces:      cache.GetGlobalCache(),
		now:         time.Now,
		skew:        envDurationOr("ORDER_WEBHOOK_MAX_SKEW_SECONDS", time.Second, defaultWebhookSkew, minWebhookSkew, maxWebhookSkew),
		maxBody:     envInt64Or("ORDER_WEBHOOK_MAX_BODY_BYTES", defaultWebhookMaxBody, minWebhookMaxBody, maxWebhookMaxBody),
		strictNonce: strings.EqualFold(strings.TrimSpace(os.Getenv("ORDER_WEBHOOK_NONCE_STRICT")), "on"),
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// Middleware 返回 gin 中间件。必须挂在带 :platform 路径参数的公开路由上。
func (g *OrderWebhookGuard) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Param(OrderWebhookPlatformParam)
		if !validWebhookPlatform(platform) {
			rejectWebhook(c, http.StatusBadRequest,
				"platform 需为 1–32 位小写字母/数字/下划线/短横线，且以字母或数字开头")
			return
		}

		sig := strings.TrimSpace(c.GetHeader(WebhookSignatureHeader))
		tsRaw := strings.TrimSpace(c.GetHeader(WebhookTimestampHeader))
		nonce := strings.TrimSpace(c.GetHeader(WebhookNonceHeader))
		if missing := missingWebhookHeaders(sig, tsRaw, nonce); len(missing) > 0 {
			rejectWebhook(c, http.StatusBadRequest, "缺少必需的签名头："+strings.Join(missing, "、"))
			return
		}
		if !validWebhookNonce(nonce) {
			rejectWebhook(c, http.StatusBadRequest, "nonce 需为 8–64 位字母/数字/下划线/短横线")
			return
		}
		ts, err := strconv.ParseInt(tsRaw, 10, 64)
		if err != nil || ts <= 0 {
			rejectWebhook(c, http.StatusBadRequest, WebhookTimestampHeader+" 需为 10 位 Unix 秒时间戳")
			return
		}

		// 先判时效再验签：过期请求没必要为它花一次 HMAC 与一次密钥读库。
		if skew := absDuration(g.now().Sub(time.Unix(ts, 0))); skew > g.skew {
			rejectWebhook(c, http.StatusUnauthorized,
				"时间戳超出容忍窗口（偏差 "+strconv.FormatInt(int64(skew.Seconds()), 10)+"s，窗口 "+g.skew.String()+"）")
			return
		}

		body, err := g.readBody(c)
		if err != nil {
			rejectWebhook(c, http.StatusRequestEntityTooLarge, "请求体读取失败："+err.Error())
			return
		}

		secret, ok := g.secretCandidates(c.Request.Context(), platform)
		if !ok {
			// fail-closed：没有密钥就不存在"可信的回调"。503 而非 401 —— 这多半是本侧
			// 没配好，对方的签名可能完全正确，回 401 会让对接方去改自己没错的代码。
			rejectWebhook(c, http.StatusServiceUnavailable,
				"平台 "+platform+" 未配置回调密钥（system_config_kv 键 "+g.secretKey(platform)+"）")
			return
		}

		if !signatureMatchesAny(secret, CanonicalWebhookPayload(platform, tsRaw, nonce, body), sig) {
			rejectWebhook(c, http.StatusUnauthorized, "签名校验失败")
			return
		}

		if denied, why := g.checkReplay(c.Request.Context(), platform, nonce); denied {
			rejectWebhook(c, http.StatusConflict, why)
			return
		}

		c.Set(VerifiedWebhookKey, platform)
		c.Next()
	}
}

// CanonicalWebhookPayload 签名串。平台名进签名是有意的：不带它，A 平台的一份合法报文
// 就能原封不动地推成 B 平台的订单（两家共用同一密钥时无人拦得住）。
func CanonicalWebhookPayload(platform, timestamp, nonce string, body []byte) []byte {
	out := make([]byte, 0, len(platform)+len(timestamp)+len(nonce)+len(body)+3)
	out = append(out, platform...)
	out = append(out, '\n')
	out = append(out, timestamp...)
	out = append(out, '\n')
	out = append(out, nonce...)
	out = append(out, '\n')
	return append(out, body...)
}

// signatureMatchesAny 逐个候选密钥比对（轮换期新旧并存，语义同桥接通道凭证）。
// 候选数上限由调用侧保证只有 2 个；比对走常量时间，且从不回显期望签名。
func signatureMatchesAny(secrets []string, payload []byte, got string) bool {
	a, err := hex.DecodeString(strings.ToLower(stripSignaturePrefix(strings.TrimSpace(got))))
	if err != nil || len(a) != sha256.Size {
		return false
	}
	for _, s := range secrets {
		if subtle.ConstantTimeCompare(a, []byte(hmacSHA256Raw(s, payload))) == 1 {
			return true
		}
	}
	return false
}

// stripSignaturePrefix 容忍 `sha256=<hex>` 写法：GitHub / Stripe / 各大支付平台都用这个格式，
// 对接方照抄自家旧代码时带上前缀是常态。若在 hex 解码失败后才报错，对方只会看到
// "签名校验失败" 而完全不知道错在自己多写了 7 个字符 —— 所以在格式层就吃掉它。
// 前缀大小写不敏感；只吃一层，避免 "sha256=sha256=…" 这种把歧义留在系统里。
func stripSignaturePrefix(got string) string {
	if len(got) > 7 && strings.EqualFold(got[:7], "sha256=") {
		return got[7:]
	}
	return got
}

func hmacSHA256Raw(secret string, payload []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return mac.Sum(nil)
}

// readBody 读满请求体并算签名，然后把同一份字节交还给下游（控制器仍按原样 ShouldBindJSON）。
// 上限用 MaxBytesReader：这是公开端点，不设上界就是"任何人一条 POST 就能要多少内存"。
func (g *OrderWebhookGuard) readBody(c *gin.Context) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, g.maxBody))
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	c.Request.ContentLength = int64(len(raw))
	return raw, nil
}

func (g *OrderWebhookGuard) secretKey(platform string) string {
	return WebhookSecretKeyFor(platform)
}

// WebhookSecretKeyFor 暴露密钥键名的拼法：配置该键的一侧（管理端/运维文档/契约测试）
// 必须与读取侧共用同一份规则。两侧各自手拼时，改前缀只会表现为"配了密钥却说没配"，
// 而这条断言能把它挡在编译期之后、上线之前。
func WebhookSecretKeyFor(platform string) string { return webhookSecretKeyPrefix + platform }

// WebhookSecretPrevKeyFor 轮换灰度位键名。
func WebhookSecretPrevKeyFor(platform string) string {
	return webhookSecretKeyPrefix + platform + webhookSecretPrevSuffix
}

func (g *OrderWebhookGuard) prevSecretKey(platform string) string {
	return WebhookSecretPrevKeyFor(platform)
}

// secretCandidates 取该平台的密钥候选（当前 + 轮换灰度位）。
//
// 读库出错与"确实没配"都判 fail-closed，但只有后者会被回给调用方当作"未配置"：
// system_config_kv 一抖就全平台 503 时，日志里那句 Errorf 是本侧故障的唯一线索，
// 不能让运维去问电商平台"你们密钥是不是没配"。
func (g *OrderWebhookGuard) secretCandidates(ctx context.Context, platform string) ([]string, bool) {
	out := make([]string, 0, 2)
	var readErr error
	for _, key := range []string{g.secretKey(platform), g.prevSecretKey(platform)} {
		v, err := g.secrets.Get(ctx, key)
		if err != nil {
			readErr = err
			continue
		}
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 && readErr != nil {
		logger.Errorf("[order-webhook] 读取平台 %s 密钥失败 ⇒ 按未配置处理（fail-closed）：%v", platform, readErr)
	}
	return out, len(out) > 0
}

// checkReplay 用 SetNX 登记 nonce：拿到 false 说明窗口内已见过 ⇒ 判重放。
//
// 存储故障时**fail-open**，方向与挽回 worker 的认领锁相反，理由同样是对称性：
// 这里被放过的是一条"签名与时间戳都已验证"的回调，最坏后果是同一订单状态被重复
// upsert 一次（幂等）；而 fail-closed 的后果是 Redis 一抖，全部电商平台的订单推送
// 都进不来 —— 订单镜像比"可能重放一次"贵得多。放过的那一次会计数并限速告警。
func (g *OrderWebhookGuard) checkReplay(ctx context.Context, platform, nonce string) (bool, string) {
	key := webhookNonceKeyPrefix + platform + ":" + nonce
	ok, err := g.nonces.SetNX(ctx, key, "1", time.Duration(webhookNonceTTLMultiplier)*g.skew)
	if err != nil {
		g.nonceOutages.Add(1)
		if g.strictNonce {
			g.warnNonceOutage(err, "本轮 fail-closed（严格模式）")
			return true, "重放校验暂不可用（严格模式）"
		}
		g.warnNonceOutage(err, "本轮 fail-open（签名已验证，最坏是重复 upsert）")
		return false, ""
	}
	if !ok {
		return true, "该 nonce 在时间窗口内已被使用过（重放）"
	}
	return false, ""
}

func (g *OrderWebhookGuard) warnNonceOutage(err error, action string) {
	const gap = int64(time.Minute / time.Second)
	// 限速用的是真实墙钟而不是注入的业务时钟：这个计数器量的是"运维多久该看到一次提醒"，
	// 而 g.now 是为"时间戳是否出窗"服务的，测试里会被冻住。
	now := time.Now().Unix()
	last := g.lastNonceWarn.Load()
	if now-last < gap && last != 0 {
		return
	}
	if !g.lastNonceWarn.CompareAndSwap(last, now) {
		return
	}
	logger.Warnf("[order-webhook] ⚠️ nonce 存储不可用（累计 %d 次）⇒ %s：%v",
		g.nonceOutages.Load(), action, err)
}

// NonceOutages 返回 nonce 存储故障累计次数（观察端点/自检用）。
func (g *OrderWebhookGuard) NonceOutages() int64 { return g.nonceOutages.Load() }

// LegacyDeprecationNotice 旧路径的去留告示：路径已经"错"了一次，不能再悄悄改掉。
//
// 这里同时是**取证手段**：下线旧路径的前提是"确知无人再用"，而本地看不到生产流量。
// 于是每次命中都回一个标准 deprecation 头，并按 60s 全局限速记一行 Warnf
// （含平台、来源 IP 与 UA）—— 日志本身就是下线前的那份名单。
// 限速是全局而非按平台：平台名来自 URL，按平台开键等于让调用方决定本进程要养多少个键。
func LegacyDeprecationNotice(successorPrefix string) gin.HandlerFunc {
	var last atomic.Int64
	return func(c *gin.Context) {
		c.Header(DeprecatedWebhookHeader, "true")
		// rel=successor-version（RFC 8288）：给出这次调用对应的替代路径，但不写 Sunset 日期 ——
		// 下线时点取决于"还有谁在用"，而那份名单正由下面的限速日志现场收集。
		successor := successorPrefix + c.Param(OrderWebhookPlatformParam)
		c.Header("Link", "<"+successor+">; rel=\"successor-version\"")

		now := time.Now().Unix()
		prev := last.Load()
		if now-prev >= 60 && last.CompareAndSwap(prev, now) {
			logger.Warnf("[order-webhook] 旧路径仍被调用：POST %s platform=%s from=%s ua=%q ⇒ 它是会话鉴权语义，"+
				"外部平台请改用 %s<platform> 并配 HMAC 签名（本行按 60s 限速，累计命中即下线前的名单）",
				c.Request.URL.Path, c.Param(OrderWebhookPlatformParam), c.ClientIP(),
				c.Request.UserAgent(), successorPrefix)
		}
		c.Next()
	}
}

func rejectWebhook(c *gin.Context, httpCode int, message string) {
	response.Error(c, httpCode, message)
	c.Abort()
}

func missingWebhookHeaders(sig, ts, nonce string) []string {
	missing := make([]string, 0, 3)
	if ts == "" {
		missing = append(missing, WebhookTimestampHeader)
	}
	if nonce == "" {
		missing = append(missing, WebhookNonceHeader)
	}
	if sig == "" {
		missing = append(missing, WebhookSignatureHeader)
	}
	return missing
}

// validWebhookPlatform 约束平台名：它同时是 kv 键的一部分与 nonce 键的一部分，
// 不校验就等于让外部输入去拼配置键（`..`、超长串、换行都会污染键空间）。
func validWebhookPlatform(p string) bool {
	if len(p) == 0 || len(p) > 32 {
		return false
	}
	for i := 0; i < len(p); i++ {
		ch := p[i]
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-'
		if !ok {
			return false
		}
	}
	return (p[0] >= 'a' && p[0] <= 'z') || (p[0] >= '0' && p[0] <= '9')
}

func validWebhookNonce(n string) bool {
	if len(n) < 8 || len(n) > 64 {
		return false
	}
	for i := 0; i < len(n); i++ {
		ch := n[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func envDurationOr(key string, unit, fallback, lo, hi time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		logger.Warnf("[order-webhook] %s=%q 不是整数 ⇒ 沿用 %s", key, raw, fallback)
		return fallback
	}
	d := time.Duration(n) * unit
	switch {
	case d < lo || d > hi:
		logger.Warnf("[order-webhook] %s=%s 越界 [%s,%s] ⇒ 沿用 %s", key, d, lo, hi, fallback)
		return fallback
	}
	return d
}

func envInt64Or(key string, fallback, lo, hi int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < lo || n > hi {
		logger.Warnf("[order-webhook] %s=%q 非法或越界 [%d,%d] ⇒ 沿用 %d", key, raw, lo, hi, fallback)
		return fallback
	}
	return n
}
