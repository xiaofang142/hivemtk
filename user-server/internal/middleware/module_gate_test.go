package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
)

// runThrough 把一个中间件挂到一次性 engine 上跑一遍，返回是否被 Abort。
//
// 用真 engine 而不是 CreateTestContext + 手改 handlers：后者要摸 gin 的
// 未导出字段，跨 gin 版本会碎；真 engine 的行为与生产完全一致，
// 且顺带验证了「中间件确实 abort 了请求」而不只是「函数返回了某个值」。
func runThroughPath(mw gin.HandlerFunc, path string) bool {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	reachedHandler := false
	r.GET(path, mw, func(c *gin.Context) { reachedHandler = true })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if reachedHandler {
		return false
	}
	// 兜底：handler 没跑到就是被拦了。无论它用 HTTP 状态还是响应体业务码表达
	// （本仓 ErrorWithBusinessCode 是 HTTP 恒 200 + 体内 code），"handler 未执行"
	// 才是与传输层无关的判据 —— 盯状态码会在两种错误通道之间反复误判。
	if w.Code != http.StatusOK {
		return true
	}
	var body struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code != 0
}

// 模块闸测试。
//
// 这一组用例守的是**收费模式的有效性**，不是"功能能不能用"：
// 一旦闸失效（未覆盖路径默认放行的那条放行得太多、或三态判错），
// 表现为"付费模块白送"，而这种 bug 从界面上完全看不出来 ——
// 用户的模块菜单都在、页面能开、数据都对，只是没付钱。
// 所以下面的断言里有相当一部分是在钉"**不许放行**"。

// fakeChecker 可编排的闸判定
type fakeChecker struct {
	enabled map[string]bool
}

func (f fakeChecker) IsEnabled(_ context.Context, k string) bool { return f.enabled[k] }

func withChecker(t *testing.T, c moduleGateChecker) {
	t.Helper()
	SetModuleGateChecker(c)
	t.Cleanup(func() { SetModuleGateChecker(nil) })
}

func TestModuleOwnerOf(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/geo/config", "geo"},
		{"/api/geo/content/optimize", "geo"},
		{"/api/knowledge/search", "knowledge"},
		{"/api/customer/oneid/list", "customer"},
		{"/api/llm/wallet", "llm-cloud"},
		{"/api/modules/my", "platform-link"},
	}
	for _, c := range cases {
		if got := ModuleOwnerOf(c.path); got != c.want {
			t.Errorf("ModuleOwnerOf(%q) = %q，期望 %q", c.path, got, c.want)
		}
	}
}

// TestModuleRouteRulesAllReferenceRealModules 表里不能有目录之外的模块 key ——
// 写错一个 key 意味着那条路径**永远**被判为未开通（闸恒拦），是最隐蔽的一种坏。
func TestModuleRouteRulesAllReferenceRealModules(t *testing.T) {
	all := ModuleRouteAll()
	if len(all) == 0 {
		t.Fatal("路由模式表为空：模块闸将对所有带标注的路径恒拦")
	}
	for _, r := range all {
		if r.ModuleKey == "" {
			t.Errorf("模式 %q 没有模块 key", r.Pattern)
			continue
		}
		if cat := model.OwnerModuleCategory(r.ModuleKey); cat == "" {
			t.Errorf("模式 %q 指向不存在的模块 %q", r.Pattern, r.ModuleKey)
		}
	}
}

// TestModuleRouteRulesPatternsUnique 同一模式不允许出现两次（后者会静默覆盖前者，
// 而"覆盖"在按顺序匹配时是确定性的，所以重复几乎总是拼错）。
func TestModuleRouteRulesPatternsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, r := range ModuleRouteAll() {
		if prev, ok := seen[r.Pattern]; ok {
			t.Errorf("模式 %q 重复：%s 与 %s", r.Pattern, prev, r.ModuleKey)
		}
		seen[r.Pattern] = r.ModuleKey
	}
}

// TestMatchModuleSegsSemantics 段匹配语义：`*` 吃掉恰好一段，段数必须相等。
func TestMatchModuleSegsSemantics(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// `*` 吃**恰好**一段，且段数必须相等 —— 与 menu_capability_routes.go
		// 的能力面守卫同一口径（那边是精确路径逐条列出，不靠 * 通配）。
		{"/api/customer/*", "/api/customer/360", true},
		{"/api/customer/*", "/api/customer/oneid/list", false}, // 2 段 ≠ 1 段
		{"/api/customer/*", "/api/customer", false},
		{"/api/geo/*", "/api/geo/config", true},
		{"/api/geo/*", "/api/geo/content/optimize", false},
		{"/api/llm/wallet", "/api/llm/wallet", true},
		{"/api/llm/wallet", "/api/llm/wallet/txns", false}, // 精确模式不吃前缀
	}
	for _, c := range cases {
		if m := matchModuleSegs(c.pattern, splitModulePath(c.path)); m != c.want {
			t.Errorf("matchModuleSegs(%q, %q) = %v，期望 %v", c.pattern, c.path, m, c.want)
		}
	}
}

func TestModulePatternUnknownPathIsUncovered(t *testing.T) {
	for _, p := range []string{
		"/api/this-does-not-exist",
		"/api/definitely/not/a/real/route/at/all",
		"/",
		"",
	} {
		if got := ModuleOwnerOf(p); got != "" {
			t.Errorf("ModuleOwnerOf(%q) = %q，期望空（未覆盖 → 放行并计入巡检）", p, got)
		}
	}
}

// TestMarketAPIsAreNotGated 市场与下单路径绝不能被闸拦。
//
// 这是最容易在"加闸"时顺手拦掉的一组，而拦掉的后果是**用户永远买不了模块**：
// 他点「订阅」，前端拿到 403 "该功能模块未开通"，而这句话本身就是系统告诉他的。
func TestMarketAPIsAreNotGated(t *testing.T) {
	mustNotGate := []string{
		"/api/module-market/list",
		"/api/module-market/detail/geo",
		"/api/module-market/order",
		"/api/module-market/orders",
		"/api/module-market/payment-guide",
		"/api/modules/my",
		"/api/modules/sync",
	}
	for _, p := range mustNotGate {
		got := ModuleOwnerOf(p)
		if got == "" {
			continue // 完全未覆盖：永远放行，最安全
		}
		// 基础模块恒 active，闸对它永远放行 —— 归到基础模块与不归等价
		if model.OwnerModuleCategory(got) == model.ModuleCategoryBase {
			continue
		}
		t.Errorf("购买入口 %q 被归到收费模块 %q：用户没开通就看不到自己买了什么，"+
			"永远买不了（付费依赖闭环），或订阅了却看不到订单", p, got)
	}
}

// TestSelfProvidedLLMIsNotGated 自带 Key 的模型管理不收费。
func TestSelfProvidedLLMIsNotGated(t *testing.T) {
	// llmRouting 归 llm-self（免费常开），模型 CRUD 属于自带 Key 路径
	if got := ModuleOwnerOf("/api/llm/models"); got == "llm-cloud" {
		t.Error("自带 Key 的模型管理被归到计费模块 llm-cloud：用户没买套餐却用不了自己的 Key")
	}
}

// TestUncoveredRoutesTracked ③态：未覆盖路径要被记录下来供巡检
func TestUncoveredRoutesTracked(t *testing.T) {
	ResetUncoveredModuleRoutes()
	withChecker(t, fakeChecker{enabled: map[string]bool{"core": true}})

	runThroughPath(GuardModulePath(), "/api/some/uncovered/path")
	if len(UncoveredModuleRoutes()) != 1 {
		t.Fatalf("未覆盖路径应被记入巡检清单，实际 %v", UncoveredModuleRoutes())
	}
	ResetUncoveredModuleRoutes()
}

// TestGateThreeStates 三态语义各一条
func TestGateThreeStates(t *testing.T) {
	ResetUncoveredModuleRoutes()

	// ① 未装配判定 → 放行（不能因为闸没接上就让整个系统不可用）
	withChecker(t, nil)
	if aborted := runThroughPath(RequireModule("geo"), "/api/geo/config"); aborted {
		t.Error("未装配判定时模块闸必须放行（否则免费用户也进不来）")
	}

	// ② 已开通 → 放行
	withChecker(t, fakeChecker{enabled: map[string]bool{"geo": true}})
	if aborted := runThroughPath(RequireModule("geo"), "/api/geo/config"); aborted {
		t.Error("已开通模块不应被拦")
	}

	// ③ 未开通（含超管——闸不看角色）→ 拦
	withChecker(t, fakeChecker{enabled: map[string]bool{"core": true}})
	if aborted := runThroughPath(RequireModule("geo"), "/api/geo/config"); !aborted {
		t.Error("未开通模块必须被拦：否则付费模块白送，商业化失效")
	}
}

// TestGateThreadsRequestContextToChecker 闸必须把**请求上下文**递给判定器。
//
// P1 回归：adapter 曾经在这里换 context.Background()，on 模式下租户镜像读
// fail-closed → 刷新失败 → bootstrapped=false → IsEnabled 全返回 true =
// 付费闸静默全开。TenantMiddleware 写进 request ctx 的租户绑定，
// 必须原封不动抵达 checker —— 这条用例钉的就是这条通路。
func TestGateThreadsRequestContextToChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	probe := &ctxProbeChecker{enabled: true}
	withChecker(t, probe)

	r := gin.New()
	// 模拟 TenantMiddleware 的写入位置（c.Request.WithContext）：
	// 置于闸之前，与生产的挂载顺序一致（鉴权/租户中间件 → 模块闸）。
	r.GET("/api/geo/config", func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), 7))
	}, GuardModulePath(), func(c *gin.Context) {})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/geo/config", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("放行路径应 200，实际 %d", w.Code)
	}
	if probe.got == nil {
		t.Fatal("checker 未被调用，前置不成立")
	}
	id, ok := tenant.FromContext(probe.got)
	if !ok || id != 7 {
		t.Fatalf("闸递给判定器的 ctx 丢了租户绑定（got ok=%v id=%d，期望 7）——"+
			"on 模式下这会让镜像读 fail-closed、闸全放行", ok, id)
	}
}

// ctxProbeChecker 记录判定器实际收到的 ctx
type ctxProbeChecker struct {
	got     context.Context
	enabled bool
}

func (c *ctxProbeChecker) IsEnabled(ctx context.Context, _ string) bool {
	c.got = ctx
	return c.enabled
}

// TestGateIgnoresUnannotatedModule 未标注 moduleKey 的路由一律放行
func TestGateIgnoresUnannotatedModule(t *testing.T) {
	withChecker(t, fakeChecker{enabled: map[string]bool{}})
	if aborted := runThroughPath(RequireModule(""), "/api/whatever"); aborted {
		t.Error("未标注模块的路由应放行（漏挂一处就全站 403 是灾难）")
	}
}

var _ = context.Background
var _ = gorm.ErrRecordNotFound

// TestMatchModuleSegsTailWildcard specs/017 兜底轮：尾部 `**` 吃掉剩余全部段。
// 单 `*` 语义不变（TestMatchModuleSegsSemantics 钉死），生成件不用 `**`。
func TestMatchModuleSegsTailWildcard(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/egolite/**", "/api/egolite/runner/status", true},
		{"/api/egolite/**", "/api/egolite/runner/tokens/123/revoke", true}, // 任意深度
		{"/api/egolite/**", "/api/egolite", true},                          // ** 可吃 0 段
		{"/api/egolite/**", "/api/egolite2/x", false},                      // 前缀必须段级一致
		{"/api/egolite/**", "/api/other/x", false},
		{"/api/browser-automation/**", "/api/browser-automation/platforms/123/locators", true},
		{"/api/customer/*", "/api/customer/oneid/list", false}, // 单 * 行为不受影响
	}
	for _, c := range cases {
		if m := matchModuleSegs(c.pattern, splitModulePath(c.path)); m != c.want {
			t.Errorf("matchModuleSegs(%q, %q) = %v，期望 %v", c.pattern, c.path, m, c.want)
		}
	}
}
