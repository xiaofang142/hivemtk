package router

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	dbutil "hivemtk-user/internal/pkg/db"
)

// T-P2-02 / R-3：订单回调契约的路由级验证。
//
// 中间件单件（internal/middleware/webhook_signature_test.go）用假依赖测判定逻辑；
// 这里刻意反过来 —— 真 router、真全局中间件链、真控制器、真库。
// 因为这一条任务的风险全在"接线"上：路径注册在哪一组、前面经过哪些中间件、
// 验签读过 body 之后控制器还读不读得到，只有在装配层才看得见。

// signOrderWebhook 独立复刻对接方的签名算法（不调用被测代码），
// 否则生产实现写错时测试会跟着一起错。
func signOrderWebhook(secret, platform, ts, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(platform + "\n" + ts + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newOrderWebhookEngine(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	// 必须显式列出要建的表：testutil.NewTestDB(t) 不带 models 时只连库、不建表，
	// 而本包既有路由测试只发不入库的请求，所以从没暴露过这一点
	// （实测第一版就是在这里报 relation "system_config_kv" does not exist）。
	database := testutil.NewTestDB(t,
		&model.SystemConfigKV{},
		&model.ExternalOrder{},
		&model.WebhookEvent{},
	)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)
	return r, database
}

// uniquePlatform 造一个不与既有数据冲突的平台名：测试库是同进程共享的，
// 固定平台名会让两个会话/两次运行在同一订单键上互相干扰。
func uniquePlatform(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("tp202%07d", time.Now().UnixNano()%10_000_000)
}

func seedWebhookSecret(t *testing.T, database *gorm.DB, platform, secret string) {
	t.Helper()
	key := middleware.WebhookSecretKeyFor(platform)
	row := model.SystemConfigKV{Key: key, Value: secret, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := database.Create(&row).Error; err != nil {
		t.Fatalf("写入 %s 失败：%v", key, err)
	}
	t.Cleanup(func() {
		_ = database.Where("key = ?", key).Delete(&model.SystemConfigKV{}).Error
	})
}

func cleanupOrderWebhookRows(t *testing.T, database *gorm.DB, platform string) {
	t.Helper()
	t.Cleanup(func() {
		_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
		_ = database.Where("platform = ?", platform).Delete(&model.WebhookEvent{}).Error
	})
}

func postOrderWebhook(t *testing.T, r *gin.Engine, path string, headers map[string]string, body []byte) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Result()
}

func signedHeaders(platform, secret, nonce string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return map[string]string{
		middleware.WebhookTimestampHeader: ts,
		middleware.WebhookNonceHeader:     nonce,
		middleware.WebhookSignatureHeader: signOrderWebhook(secret, platform, ts, nonce, body),
	}
}

func drainBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败：%v", err)
	}
	return string(raw)
}

// ─── 注册与接线 ──────────────────────────────────────────────────────────────

// routePaths 收集某条 (method, path) 的注册项数量。
//
// 原本想用 gin.RouteInfo 直接读出中间件链、断言"验签 guard 挂上了且只挂一次"，
// 实测 RouteInfo 只有 Method/Path/Handler 三个字段（没有链）—— 所以接线改成**行为断言**：
// guard 装没装，看"无密钥 503 / 错签 401 / 合法签名 200"这三条就够，而且比函数名更抗重构；
// "只装一次"由 gin 自身保证 —— 同一路径重复注册会 panic，见 RegistrationDoesNotPanic。
func routePaths(r *gin.Engine, method, path string) int {
	n := 0
	for _, rt := range r.Routes() {
		if rt.Method == method && rt.Path == path {
			n++
		}
	}
	return n
}

func TestOrderWebhook_BothPathsRegistered(t *testing.T) {
	r, _ := newOrderWebhookEngine(t)

	const publicPath = "/api/integration/webhook/order/:" + middleware.OrderWebhookPlatformParam
	const legacyPath = "/api/integration/order-webhook/:" + middleware.OrderWebhookPlatformParam

	if n := routePaths(r, http.MethodPost, publicPath); n != 1 {
		t.Fatalf("公开验签路径注册数 = %d，期望 1：%s", n, publicPath)
	}
	if n := routePaths(r, http.MethodPost, legacyPath); n != 1 {
		t.Errorf("旧 auth 路径注册数 = %d，期望 1：%s ⇒ 删了会打断现网内部调用方", n, legacyPath)
	}

	// Deprecation 头里指的路径必须就是刚注册的那条活路径（指错路不会编译报错，
	// 只会让对接方对着 404 猜）。
	got := strings.ReplaceAll(publicPath, ":"+middleware.OrderWebhookPlatformParam, "taobao")
	if got != orderWebhookSuccessorPrefix+"taobao" {
		t.Errorf("Link 头前缀 = %q，与实际注册路径 %q 不一致", orderWebhookSuccessorPrefix, got)
	}

	// 公开路径的控制器归属：它必须落在订单回调控制器上，而不是别的 handler。
	// 这里用"缺签名头 ⇒ 400 且点名缺失头"来证（guard 生效）；控制器的读到 body 由
	// EndToEndContract 用真库真链路证。
}

// TestOrderWebhook_RegistrationDoesNotPanic gin 的路由树在重复注册同一路径时会 **panic**
// （不是返回错误）。Setup  panic 会让整个进程起不来，而单元测试如果只发请求、
// 不在构造期兜一次，就会把"服务起不来"漏成一条红色请求而不是明确诊断。
func TestOrderWebhook_RegistrationDoesNotPanic(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("路由注册 panic（多半是同一路径被注册了两次）：%v", p)
		}
	}()
	newOrderWebhookEngine(t)
}

// TestOrderWebhook_PublicPathSkipsAuthButVerifies 公开路径不得要求会话票，
// 但少了签名头就必须 400 —— 两个条件一起才叫"换成了对外契约"。
func TestOrderWebhook_PublicPathSkipsAuthButVerifies(t *testing.T) {
	r, _ := newOrderWebhookEngine(t)
	path := "/api/integration/webhook/order/taobao"

	resp := postOrderWebhook(t, r, path, nil, []byte(`{"order_id":"O-1"}`))
	body := drainBody(t, resp)
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("公开路径被会话鉴权拦住了（401）：外部平台拿不到 JWT ⇒ %s", body)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺签名头应 400，实际 %d：%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, middleware.WebhookSignatureHeader) {
		t.Errorf("应点名缺哪个头，方便对接方自查：%s", body)
	}
}

// TestOrderWebhook_LegacyPathStillRequiresAuth 反向测试：这次改动绝不能把旧路径顺手公开。
// 旧路径若因挪动注册位置而丢了 AuthMiddleware，等于修洞的同时把门整个拆了。
func TestOrderWebhook_LegacyPathStillRequiresAuth(t *testing.T) {
	r, _ := newOrderWebhookEngine(t)

	resp := postOrderWebhook(t, r, "/api/integration/order-webhook/taobao", nil, []byte(`{"order_id":"O-1"}`))
	body := drainBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("旧路径无凭证必须仍 401，实际 %d：%s ⇒ 会话鉴权被挪丢了", resp.StatusCode, body)
	}
}

// ─── 全链路契约（真 router + 真控制器 + 真库） ───────────────────────────────

func TestOrderWebhook_EndToEndContract(t *testing.T) {
	r, database := newOrderWebhookEngine(t)
	platform := uniquePlatform(t)
	secret := "e2e-webhook-secret-" + platform
	seedWebhookSecret(t, database, platform, secret)
	cleanupOrderWebhookRows(t, database, platform)

	path := "/api/integration/webhook/order/" + platform
	orderID := "E2E-" + platform
	body := []byte(`{"order_id":"` + orderID + `","status":"paid","user_phone":"13800000000"}`)

	// ① 合法签名（带 sha256= 前缀）：200，且控制器真的读到了 body（body 交还）。
	ok := postOrderWebhook(t, r, path, signedHeaders(platform, secret, "e2e-"+platform, body), body)
	msg := drainBody(t, ok)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("合法签名应 200，实际 %d：%s", ok.StatusCode, msg)
	}
	if !strings.Contains(msg, orderID) {
		t.Errorf("响应应回显订单号（证明 ShouldBindJSON 读得到验签时消费过的 body）：%s", msg)
	}

	var orders []model.ExternalOrder
	if err := database.Where("platform = ? AND order_id = ?", platform, orderID).Find(&orders).Error; err != nil {
		t.Fatalf("查询镜像订单失败：%v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("external_orders 应有 1 条镜像，实际 %d", len(orders))
	}
	if orders[0].Status != "paid" || orders[0].UserPhone != "13800000000" {
		t.Errorf("镜像字段不对：status=%q phone=%q", orders[0].Status, orders[0].UserPhone)
	}
	var events []model.WebhookEvent
	if err := database.Where("platform = ?", platform).Find(&events).Error; err != nil {
		t.Fatalf("查询 webhook_events 失败：%v", err)
	}
	if len(events) != 1 {
		t.Errorf("webhook_events 应有 1 条，实际 %d", len(events))
	}

	// ② 同一 nonce 重放 → 409，且不再新增镜像。
	replayHeaders := signedHeaders(platform, secret, "e2e-"+platform, body)
	replay := postOrderWebhook(t, r, path, replayHeaders, body)
	if got := replay.StatusCode; got != http.StatusConflict {
		t.Errorf("重放应 409，实际 %d：%s", got, drainBody(t, replay))
	} else {
		replay.Body.Close()
	}
	if err := database.Where("platform = ? AND order_id = ?", platform, orderID).Find(&orders).Error; err != nil {
		t.Fatalf("复查镜像失败：%v", err)
	}
	if len(orders) != 1 {
		t.Errorf("重放不该新增镜像，实际 %d 条", len(orders))
	}

	// ③ 错签 → 401 且不落库。
	badHeaders := signedHeaders(platform, "totally-wrong-key", "e2e-badsig", body)
	bad := postOrderWebhook(t, r, path, badHeaders, body)
	if got := bad.StatusCode; got != http.StatusUnauthorized {
		t.Errorf("错签应 401，实际 %d：%s", got, drainBody(t, bad))
	} else {
		bad.Body.Close()
	}
	if n := countWebhookRows(t, database, platform); n != 1 {
		t.Errorf("错签后镜像仍应只有 1 条，实际 %d", n)
	}

	// ④ 新 nonce + 新状态 → 走更新而非新建（镜像按订单键收敛）。
	newBody := []byte(`{"order_id":"` + orderID + `","status":"shipped"}`)
	resp2 := postOrderWebhook(t, r, path, signedHeaders(platform, secret, "e2e-2-"+platform, newBody), newBody)
	if got := resp2.StatusCode; got != http.StatusOK {
		t.Fatalf("第二笔推送应 200，实际 %d：%s", got, drainBody(t, resp2))
	}
	resp2.Body.Close()
	if err := database.Where("platform = ? AND order_id = ?", platform, orderID).Find(&orders).Error; err != nil {
		t.Fatalf("再查镜像失败：%v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("同一订单应仍只有 1 条镜像，实际 %d", len(orders))
	}
	if orders[0].Status != "shipped" {
		t.Errorf("镜像状态未推进：%q", orders[0].Status)
	}
}

func countWebhookRows(t *testing.T, database *gorm.DB, platform string) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	return n
}

// TestOrderWebhook_UnconfiguredPlatformFailsClosed 没配密钥的平台不能被任何人推订单。
// 这是"公开路径 + 无影子档"的核心代价：配置缺失时服务不可用，而不是静默放行。
func TestOrderWebhook_UnconfiguredPlatformFailsClosed(t *testing.T) {
	r, database := newOrderWebhookEngine(t)
	platform := uniquePlatform(t)
	cleanupOrderWebhookRows(t, database, platform)

	path := "/api/integration/webhook/order/" + platform
	body := []byte(`{"order_id":"NO-SECRET-1","status":"paid"}`)
	resp := postOrderWebhook(t, r, path, signedHeaders(platform, "whatever", "nosec-"+platform, body), body)
	msg := drainBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未配密钥应 503（本侧没配好，不该让对方改代码），实际 %d：%s", resp.StatusCode, msg)
	}
	if n := countWebhookRows(t, database, platform); n != 0 {
		t.Errorf("无密钥时不得写入任何镜像，实际 %d 条", n)
	}
}

// TestOrderWebhook_LegacyPathAnouncesDeprecationAndPointsToLiveRoute 真凭证走旧路径：
// 仍然照常服务（AC③"保留一个大版本"）+ 回 deprecation 头，而头里指的替代路径要能被一次
// 真签名打通 —— 对接方照抄那个头就能迁移。这条同时钉住"旧路径别被顺手改成验签"。
func TestOrderWebhook_LegacyPathAnouncesDeprecationAndPointsToLiveRoute(t *testing.T) {
	r, database := newOrderWebhookEngine(t)
	platform := uniquePlatform(t)
	secret := "successor-" + platform
	seedWebhookSecret(t, database, platform, secret)
	cleanupOrderWebhookRows(t, database, platform)

	token, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("生成测试令牌失败：%v", err)
	}

	legacyBody := []byte(`{"order_id":"LEGACY-` + platform + `","status":"paid"}`)
	legacy := postOrderWebhook(t, r, "/api/integration/order-webhook/"+platform,
		map[string]string{"Authorization": "Bearer " + token}, legacyBody)
	legacyMsg := drainBody(t, legacy)
	if legacy.StatusCode != http.StatusOK {
		t.Fatalf("旧路径带凭证应仍 200（它不验签），实际 %d：%s ⇒ 语义被改了", legacy.StatusCode, legacyMsg)
	}
	if got := legacy.Header.Get(middleware.DeprecatedWebhookHeader); got != "true" {
		t.Errorf("旧路径缺 %s 头：实际 %q", middleware.DeprecatedWebhookHeader, got)
	}
	link := legacy.Header.Get("Link")
	start, end := strings.Index(link, "<"), strings.Index(link, ">")
	if !strings.Contains(link, `rel="successor-version"`) || start < 0 || end <= start {
		t.Fatalf("Link 头未给出可照抄的替代路径：%q", link)
	}
	successor := link[start+1 : end]
	if successor != "/api/integration/webhook/order/"+platform {
		t.Errorf("替代路径拼接不对：%q", successor)
	}

	body := []byte(`{"order_id":"SUCC-` + platform + `","status":"paid"}`)
	resp := postOrderWebhook(t, r, successor, signedHeaders(platform, secret, "succ-"+platform, body), body)
	msg := drainBody(t, resp)
	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("Link 头指向了 404：%s ⇒ 常量与注册路径漂移了", successor)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("沿 Link 头迁移后应能直接打通，实际 %d：%s", resp.StatusCode, msg)
	}
}

// TestOrderWebhook_KVSecretKeyContract 密钥键名由"读的一侧"与"配的一侧"各拼一遍：
// 中间件读 order_webhook_secret_x，运维在管理端写的也是这个键。这里把键名格式与
// 缺键语义钉死，免得一侧改动导致"明明配了却报未配置"。
func TestOrderWebhook_KVSecretKeyContract(t *testing.T) {
	_, database := newOrderWebhookEngine(t)
	repo := repository.NewSystemConfigKVRepository()
	if !repo.Available() {
		t.Fatal("kv 仓储不可用 ⇒ 全局 DB 未注入，中间件在生产也会拿不到密钥")
	}
	platform := uniquePlatform(t)
	key := middleware.WebhookSecretKeyFor(platform)
	// 字面量再钉一次：键名要写进运维文档/管理端表单，改前缀必须是有意识的动作。
	if got := middleware.WebhookSecretKeyFor("taobao"); got != "order_webhook_secret_taobao" {
		t.Errorf("密钥键名格式漂移：%q（期望 order_webhook_secret_<platform>）", got)
	}
	t.Cleanup(func() {
		_ = database.Where("key = ?", key).Delete(&model.SystemConfigKV{}).Error
	})

	if _, err := repo.Upsert(context.Background(), key, "kv-side-secret"); err != nil {
		t.Fatalf("Upsert 失败：%v", err)
	}
	got, err := repo.Get(context.Background(), key)
	if err != nil || got != "kv-side-secret" {
		t.Fatalf("kv 读写不对：%q %v", got, err)
	}
	// 缺键必须回空串而不是 error：中间件靠这条区分"没配"（503+对方可见）与"读故障"（503+本侧日志）。
	missing, err := repo.Get(context.Background(), key+"-never-set")
	if err != nil || missing != "" {
		t.Errorf("缺键语义漂移：%q %v", missing, err)
	}
}
