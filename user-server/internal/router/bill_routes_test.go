// bill_routes_test.go T-P7-01：账单出口的挂载点、契约面与"真 Setup 走一遍"。
//
// 与 controller/bill_test.go 的分工：那一份钉的是"八个哨兵在状态码上分不分得开"，
// 本份钉的是"这一条端点在不在、挂在哪个鉴权组下、摘掉挂载那一条会不会有人发现"，
// 外加一条本卡真正的关闸用例（TestBillRoutes_LiveThroughRealSetup）：
// 装配点给的那两把句柄接的是同一把库，且从 HTTP 打到库里那一行为止，
// AC①（由已成交报价派生）与 AC②（金额=该版合计）同时成立。
//
// 静态锁那两条（路由表里没有内联闭包、每条端点有 @Router 注解）与商机/报价两族同口径，
// 理由也一样：只有"把那一处摘掉/加回去"的注码能让它们红，所以配套的变异在电池里。
package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// billRouteSpecs 本卡交付的全部端点（顺序无关，判据按集合比）。
//
// 少一条 = 销售工作台上那个"客户已确认"点了没反应（而 accepted 的唯一生产写入口就是它，
// 整条回款域会退化成"库里没有账单"）；多一条 = 有一个没人声明的入口在跑
// —— 本域最危险的那一条是"能把账单改成已收"，它属于 T-P7-02 的回款入账，不属于这里。
var billRouteSpecs = []string{"POST /api/bill"}

func billReadSource(t *testing.T) string {
	t.Helper()
	return readSourceFile(t, "../controller/bill.go")
}

// newBillTestEngine 只挂控制器（不经过装配层）：nil 复现"派生腿没装配"。
func newBillTestEngine(derive controller.BillDeriver, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewBillController(derive).RegisterRoutes(auth)
	return engine
}

func doBillRequest(t *testing.T, h http.Handler, method, path, tok, body string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	raw := w.Body.String()
	var env struct {
		Code    any             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return w.Code, nil, raw
	}
	var data map[string]any
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return w.Code, nil, raw
		}
	}
	return w.Code, data, raw
}

// —— ① 路由表 ——————————————————————————————————————————

func TestBillRoutes_TableIsExactlyTheDeclaredSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller.NewBillController(nil).RegisterRoutes(engine.Group("/api"))

	var got []string
	for _, r := range engine.Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, billRouteSpecs) {
		t.Errorf("路由表与本卡声明的集合不一致：\n 得到 %v\n 期望 %v", got, billRouteSpecs)
	}
}

// TestBillRoutes_NoStatusOrReadEndpoints 本卡不许存在的入口，逐条 404 探针。
//
// 判据不是"还没写"，是"写了就错"：
//   - /pay、/void、/status：结清与作废要读回款行（T-P7-02），逾期与催收属 T-P7-03；
//     今天开这一格，就是给财务台账开一条**不走回款**的捷径，而 AC② 的对账正要读那两格。
//   - GET /api/bill（列表）：账单没有租户列（判据见 model/bill.go），
//     列全表的端点在补上归属列之前等于"任何人可看全公司的应收"。
//   - PUT /api/bill/:id：金额列建后即不可改（它是对账的凭证），可改的那格不叫凭证。
//
// 光看路由表不够（有人会把动作塞进已有端点的请求体里），所以再读一遍控制器源码：
// UpdateStatus / MarkPaid 这类名字一旦出现在这一层，就说明有人接了状态写口。
func TestBillRoutes_NoStatusOrReadEndpoints(t *testing.T) {
	engine := newBillTestEngine(&billFakeDeriver{}, uint(7))
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/bill"},
		{http.MethodGet, "/api/bill/b_1"},
		{http.MethodPut, "/api/bill/b_1"},
		{http.MethodPatch, "/api/bill/b_1"},
		{http.MethodDelete, "/api/bill/b_1"},
		{http.MethodPost, "/api/bill/b_1/pay"},
		{http.MethodPost, "/api/bill/b_1/void"},
		{http.MethodPost, "/api/bill/b_1/status"},
		{http.MethodPost, "/api/bill/quote/q_1/accept"},
		{http.MethodPost, "/api/bill/b_1/send"},
	} {
		if code, _, raw := doBillRequest(t, engine, probe.method, probe.path, "", ""); code != http.StatusNotFound {
			t.Errorf("%s %s 回 %d，期望 404：本卡不得暴露任何写回款/作废/账期或列全表的入口（%s）",
				probe.method, probe.path, code, raw)
		}
	}

	src := billReadSource(t)
	for _, needle := range []string{"UpdateStatus(", "MarkPaid", "Void(", "List(", "GetByID("} {
		if strings.Contains(src, needle) {
			t.Errorf("控制器源码里出现了 %q：状态跃迁与账单读口都不经本卡的 HTTP 出口", needle)
		}
	}
}

// billFakeDeriver 只用来挂路由表（上面两条用例判的是路由形状，不是业务结论）。
type billFakeDeriver struct{}

func (billFakeDeriver) Available() bool { return true }

func (billFakeDeriver) DeriveFromQuote(context.Context, service.BillDeriveInput) (*service.BillView, error) {
	return &service.BillView{ID: "b_probe", Status: model.BillStatusOpen}, nil
}

// —— ② 无内联 handler + ③ Swagger 覆盖 ——————————————————————

func TestBillRoutes_RouterFileHasNoInlineHandler(t *testing.T) {
	src := readSourceFile(t, "bill_routes.go")

	for _, needle := range []string{"gin.Context", ".JSON(", "AbortWithStatus", "response.Success", "response.Error"} {
		if strings.Contains(src, needle) {
			t.Errorf("路由文件里出现了 %q：本层只做 URL→Controller 映射，响应必须由控制器写", needle)
		}
	}
	if !strings.Contains(src, "RegisterRoutes(") {
		t.Error("路由文件没有把挂载委托给控制器的 RegisterRoutes：端点清单因此不在唯一一处可查")
	}
}

func TestBillRoutes_SwaggerCoversEveryRoute(t *testing.T) {
	src := billReadSource(t)

	declared := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "// @Router") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(trimmed, "// @Router"))
		if len(fields) < 2 {
			t.Errorf("@Router 注解形状不对：%q", trimmed)
			continue
		}
		declared[strings.ToUpper(strings.Trim(fields[1], "[]"))+" "+fields[0]] = true
	}
	for _, spec := range billRouteSpecs {
		if !declared[spec] {
			t.Errorf("%s 没有对应的 @Router 注解：Swagger 面上缺这条端点", spec)
		}
	}
	for _, needle := range []string{"@Summary", "@Tags", "@Description", "@Success", "@Failure", "@Param", "@Security"} {
		if !strings.Contains(src, needle) {
			t.Errorf("控制器缺 %s 注解：生成的文档只剩路径，读的人不知道这条端点回答什么问题", needle)
		}
	}
	// 409 那一档必须写在文档里：三种修法不同的失败共用一个状态码，
	// 只写 400/404 的文档会让前端把"这条链已经成交过"实现成"再点一次"。
	if !strings.Contains(src, "@Failure      409") && !strings.Contains(src, "@Failure 409") {
		t.Error("派生那一条没有 @Failure 409 注解：conflict 这一档在生成的文档里不存在")
	}
}

// —— ④ 挂载位置：装配之后、鉴权中间件之后 ——————————————

// TestBillRoutes_MountedAtTheRightPlace 两道顺序锁（与报价那一条同判据，各挡一种"挂上了但挂错地方"）。
//
// 为什么是静态锁而不是匿名探针：这一条端点在没有身份时**本来就该**回 401（控制器要操作者身份），
// 所以"匿名回非 2xx"在挂对与挂错两种情况下都绿 —— 有牙的那一格在 ⑥，用带令牌的 200 做对照。
//
//  1. `app.InitBillRuntime` 必须先于挂载：挂载时读一次全局实例，读早了就是永久的 nil
//     ⇒ 端点永久 503，而 503 在别处也是"合法"响应，没人会去查装配顺序。
//  2. 必须先于 `auth.Use(JWTAuthMiddleware())` 之后挂载：gin 的 Use 只对注册之后建起来的路由生效。
func TestBillRoutes_MountedAtTheRightPlace(t *testing.T) {
	src := readSourceFile(t, "router.go")
	initAt := strings.Index(src, "app.InitBillRuntime(gormDB)")
	mountAt := strings.Index(src, "setupBillRoutes(auth)")
	useAt := strings.Index(src, "auth.Use(middleware.JWTAuthMiddleware())")

	if initAt < 0 {
		t.Error("router.go 里没有 app.InitBillRuntime(gormDB)：账单派生腿在生产启动时没人装")
	}
	if mountAt < 0 {
		t.Error("router.go 里没有 setupBillRoutes(auth)：本卡交付的端点没进生产路由树")
	}
	if useAt < 0 {
		t.Fatal("router.go 里找不到 auth.Use(middleware.JWTAuthMiddleware()) 这一句：本条的第二道锁失去了参照物，请先确认鉴权组那一段还在不在")
	}
	if mountAt < 0 || initAt < 0 {
		return
	}
	if initAt > mountAt {
		t.Errorf("InitBillRuntime（第 %d 字节）写在 setupBillRoutes（第 %d 字节）之后：挂载时派生腿还是空的，/api/bill 永久 503",
			initAt, mountAt)
	}
	if mountAt < useAt {
		t.Error("setupBillRoutes 写在 auth.Use(JWTAuthMiddleware()) 之前：那条端点拿不到鉴权链，匿名即可开出应收")
	}
	// 挂到哪个组是第三个字面量判据：参数写成 public/r 时上面两道顺序锁全部照样绿。
	if strings.Contains(src, "setupBillRoutes(public") || strings.Contains(src, "setupBillRoutes(r,") || strings.Contains(src, "setupBillRoutes(r)") {
		t.Error("setupBillRoutes 的接收者不是 auth 组：账单端点落在了鉴权链之外")
	}
	// 派生腿读的是报价那一族的仓储，装配必须排在报价之后（InitQuoteRuntime 在前只是顺序读法，
	// 真正要守的是"别排在它之前又依赖它建过的东西"——本卡不依赖，所以只登记不判红）。
	if strings.Index(src, "app.InitQuoteRuntime(gormDB)") < 0 {
		t.Error("router.go 里找不到 app.InitQuoteRuntime 这一句：本条用例对装配顺序的参照物没了")
	}
}

// TestBillRoutes_MountedBySetup 判据只有一条：Setup() 之后这一条端点在引擎上。
//
// 摘掉 setupBillRoutes 那一句，本条立刻红，而上面所有用例仍然全绿。
func TestBillRoutes_MountedBySetup(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{}, &model.Bill{})

	prevDB := dbutil.GetDB()
	defer dbutil.SetTestDB(prevDB)
	dbutil.SetTestDB(database)

	routes := billRoutesFromSetup(t, database)
	got := map[string]bool{}
	for _, spec := range routes {
		got[spec] = true
	}
	for _, spec := range billRouteSpecs {
		if !got[spec] {
			t.Errorf("Setup() 挂出来的表里没有 %s：本卡的端点没进生产路由", spec)
		}
	}
}

func billRoutesFromSetup(t *testing.T, database *gorm.DB) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	var out []string
	for _, r := range engine.Routes() {
		if strings.HasPrefix(r.Path, "/api/bill") {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

// —— ⑤ 未装配 ——————————————————————————————————————————

// TestBillRoutes_UnassembledAnswersFiveOhThree 不装配 ⇒ 503 且 data 非空。
//
// 这一条是本卡的关闸窗口：路由已进 Setup 而装配点还没跑（或那把库是 nil）时，
// 出口必须说"我起不来"。回 200 + 空对象会让前端长成"这单还没开账"，
// 而那是业务结论 —— 此刻的事实是"一次都没查、一张都没开"。
func TestBillRoutes_UnassembledAnswersFiveOhThree(t *testing.T) {
	engine := newBillTestEngine(nil, uint(7))
	code, data, raw := doBillRequest(t, engine, http.MethodPost, "/api/bill", "", `{"quote_row_id":"q_1"}`)
	if code != http.StatusServiceUnavailable {
		t.Errorf("未装配时回 %d，期望 503：%s", code, raw)
	}
	if data == nil || data["reason"] != "unavailable" {
		t.Errorf("503 的 data=%v，期望带 reason=unavailable：%s", data, raw)
	}
}

// TestBillRoutes_UnassembledStillMountsRoute 未装配时**那条路由还在**，且由装配函数自己挂上。
//
// 上面那条 503 用例走的是 newBillTestEngine —— 它绕过装配层、自己调 RegisterRoutes，
// 所以它只证明"控制器在句柄缺失时回 503"，证明不了 `setupBillRoutes` 里那一次
// RegisterRoutes 有没有跑。把挂载改成"没装配就干脆不挂"，全部既有用例照样绿，
// 而线上症状从"503 + reason=unavailable（运维查得到、前端知道去催装配）"
// 变成"404（这套 API 不存在）"—— 文件头把这两件事分开写，就得有用例把它们分开。
//
// 这条用例刻意**不**经过 Setup：Setup 会先把派生腿装上（derive≠nil），
// 那条路已由 TestBillRoutes_LiveThroughRealSetup 守；这里要的就是 nil 那一支。
func TestBillRoutes_UnassembledStillMountsRoute(t *testing.T) {
	prev := service.GlobalBillService()
	defer service.SetGlobalBillService(prev)
	service.SetGlobalBillService(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Next()
	})
	setupBillRoutes(auth)

	var mounted []string
	for _, r := range engine.Routes() {
		mounted = append(mounted, r.Method+" "+r.Path)
	}
	if len(mounted) != len(billRouteSpecs) || mounted[0] != billRouteSpecs[0] {
		t.Fatalf("未装配时 setupBillRoutes 挂出来的表 = %v，期望 %v（挂不挂在装配之前判，不挂在装配之后）",
			mounted, billRouteSpecs)
	}

	code, data, raw := doBillRequest(t, engine, http.MethodPost, "/api/bill", "", `{"quote_row_id":"q_1"}`)
	if code != http.StatusServiceUnavailable {
		t.Errorf("经装配函数挂载、未装配的端点回 %d，期望 503：%s", code, raw)
	}
	if data == nil || data["reason"] != "unavailable" {
		t.Errorf("503 的 data=%v，期望带 reason=unavailable：%s", data, raw)
	}
}

// —— ⑥ 活体：真 Setup + 真库，从 HTTP 打到库里那一行 ——————————————

const (
	billLiveOppID = "opp_bill_live"
	billLiveRowID = "q_bill_live_1"
)

// TestBillRoutes_LiveThroughRealSetup 本卡的关闸用例。
//
// 它钉住五件上面那些用例各自钉不住的事：
//  1. **Setup 自己**把派生腿装起来了（开局先清全局：不洗这一把的话，摘掉
//     `app.InitBillRuntime` 那一行仍然能靠上一条用例留在全局里的实例读到 200 —— 商机竖 M33 的老路）；
//  2. 装配点给的两把句柄接的是**同一把库**（缺任何一把只会回 503，而 503 在这里是红）；
//  3. AC① 落库：库里真多出一行 bills，且它钉在**那一版**上（quote_row_id），
//     报价行同时被推到 accepted —— 这两件事不在同一行里看，就看不出"派生"与"记账"分家；
//  4. AC② 落库：账单金额等于该版行项目的手算合计 369.99（期望值不来自被测代码）；
//  5. 鉴权：同一个体、同一个路径，去掉令牌之后既不能回 2xx、也不能留下任何账单行。
//
// 顺带钉住幂等在 HTTP 面上的形状：第二次确认回 reused=true 而账单仍然只有一行。
func TestBillRoutes_LiveThroughRealSetup(t *testing.T) {
	prev := service.GlobalBillService()
	defer service.SetGlobalBillService(prev)
	service.SetGlobalBillService(nil)

	database := testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{}, &model.Bill{},
		&model.SystemConfigKV{}, &model.SalesEvent{}, &model.ApprovalRequest{})

	prevDB := dbutil.GetDB()
	defer dbutil.SetTestDB(prevDB)
	dbutil.SetTestDB(database)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	if svc := service.GlobalBillService(); svc == nil || !svc.Available() {
		t.Fatal("Setup 之后账单派生腿仍未装配：/api/bill 只会回 503（app.InitBillRuntime 没跑，或跑在路由之后）")
	}
	billSeedLiveQuote(t, database)

	tok, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("签发测试令牌失败：%v", err)
	}
	body := `{"quote_row_id":"` + billLiveRowID + `"}`

	code, data, raw := doBillRequest(t, engine, http.MethodPost, "/api/bill", tok, body)
	if code != http.StatusOK {
		t.Fatalf("派生回 %d：%s", code, raw)
	}
	billID, _ := data["id"].(string)
	if billID == "" {
		t.Fatalf("响应里没有账单号：%v", data)
	}
	if data["reused"] != false {
		t.Errorf("首次派生的 reused=%v，期望 false：%v", data["reused"], data)
	}
	if amt, ok := data["amount"].(float64); !ok || amt != 369.99 {
		t.Errorf("响应金额=%v，期望手算合计 369.99（3×100 打九折 + 99.99）", data["amount"])
	}
	if v, present := data["due_at"]; present && v != nil {
		t.Errorf("due_at=%v，期望 null（本卡没有任何一处定义过付款条件）", v)
	}

	// 断言打在**落库那一行**上，不是返回值：返回值是服务层自己拼的对象，
	// 而"账单由已成交报价派生"这件事只有在库里才成立。
	var stored model.Bill
	if err := database.First(&stored, "id = ?", billID).Error; err != nil {
		t.Fatalf("读回账单 %s 失败：%v", billID, err)
	}
	if stored.QuoteRowID != billLiveRowID || stored.QuoteID != billLiveChainID {
		t.Errorf("库里那一张钉在 %q / %q，期望版本行 %s 与链 %s",
			stored.QuoteRowID, stored.QuoteID, billLiveRowID, billLiveChainID)
	}
	if stored.Status != model.BillStatusOpen {
		t.Errorf("新开的账单状态=%s，期望 %s（结清要等回款，T-P7-02）", stored.Status, model.BillStatusOpen)
	}
	if stored.Amount != 369.99 {
		t.Errorf("库里金额=%v，期望 369.99 —— AC② 判的是这一格，不是响应里那一格", stored.Amount)
	}
	if stored.OpportunityID != billLiveOppID {
		t.Errorf("库里 opportunity_id=%q，期望 %q", stored.OpportunityID, billLiveOppID)
	}
	var quote model.Quote // 独立零值 struct：复用已填充的会把旧字段并进 WHERE
	if err := database.Where("id = ?", billLiveRowID).First(&quote).Error; err != nil {
		t.Fatalf("读回报价行失败：%v", err)
	}
	if quote.Status != model.QuoteStatusAccepted {
		t.Errorf("派生之后报价行仍是 %s，期望 %s —— 账单与成交各说各话，回款域就没有起点",
			quote.Status, model.QuoteStatusAccepted)
	}

	// 第二次确认：复用、库里仍然只有一行。
	code, data, raw = doBillRequest(t, engine, http.MethodPost, "/api/bill", tok, body)
	if code != http.StatusOK {
		t.Fatalf("第二次派生回 %d：%s", code, raw)
	}
	if data["reused"] != true || data["id"] != billID {
		t.Errorf("第二次派生得到 %v / %v，期望 reused=true 且账单号仍是 %s", data["reused"], data["id"], billID)
	}
	if n := billLiveCount(t, database); n != 1 {
		t.Errorf("重复确认之后账单有 %d 行，期望 1（一次成交一张应收）", n)
	}

	// 匿名探针：与上面那条 200 同一个体、同一个路径，去掉令牌。
	// 前面的 503/401 探针判不出挂载位置（那两条挂在 public 组上也会照样红），
	// 这一条不一样：它**本可以**成功，所以非 2xx 才是鉴权链的证据。
	before := billLiveCount(t, database)
	code, _, _ = doBillRequest(t, engine, http.MethodPost, "/api/bill", "", body)
	if code >= 200 && code < 300 {
		t.Errorf("匿名派生回 %d：应收写入口落在了鉴权链之外", code)
	}
	if after := billLiveCount(t, database); after != before {
		t.Errorf("匿名调用之后账单从 %d 行变成 %d 行：请求被拒了但**副作用**留下来了", before, after)
	}
}

// 活体夹具：写的都是运营/销售写得到的东西（走真仓储的写路径），不直插、不改全局变量。
const billLiveChainID = "QT-BILL-LIVE"

func billSeedLiveQuote(t *testing.T, database *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(ctx, &model.Opportunity{
		ID: billLiveOppID, Code: "OPP-BILL-LIVE", CustomerID: "cus_bill_live", OneID: "one_bill_live",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_live",
	}); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
	repo := repository.NewQuoteRepositoryWithDB(database)
	row := &model.Quote{
		ID: billLiveRowID, QuoteID: billLiveChainID, OpportunityID: billLiveOppID,
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault,
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("落报价第一版失败：%v", err)
	}
	// 行项目与状态都走仓储的写路径：直插会替仓储兜住"这一格根本写不进去"那类错误。
	lines := []*model.QuoteLineItem{
		{LineNo: 1, ProductID: "p-a", Title: "标准版坐席 ×3", Quantity: 3, UnitPrice: 100, DiscountPercent: 10, Amount: 270},
		{LineNo: 2, ProductID: "p-b", Title: "实施包", Quantity: 1, UnitPrice: 99.99, DiscountPercent: 0, Amount: 99.99},
	}
	if err := repo.AddLines(ctx, billLiveRowID, lines); err != nil {
		t.Fatalf("落行项目失败：%v", err)
	}
	if err := repo.UpdateStatus(ctx, billLiveRowID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("把报价推到 sent 失败：%v", err)
	}
}

func billLiveCount(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.Bill{}).Count(&n).Error; err != nil {
		t.Fatalf("清点账单失败：%v", err)
	}
	return n
}
