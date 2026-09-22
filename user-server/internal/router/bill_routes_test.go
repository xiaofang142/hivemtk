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

// billRouteSpecs 本域交付的全部端点（顺序无关，判据按集合比；这一份按字典序写死）。
//
// 少一条 = 销售工作台上那个"客户已确认"点了没反应（而 accepted 的唯一生产写入口就是它，
// 整条回款域会退化成"库里没有账单"），或者对账侧读不到"这张应收还差多少钱"；
// 多一条 = 有一个没人声明的入口在跑 —— 本域最危险的那一条是"能把账单改成已收"，
// 它只能由回款行累加出来（T-P7-02 的服务层），不许有 HTTP 出口。
//
// 两条 GET 是 T-P7-01 结转遗留①的兑现点：那张卡写下"账单没有读口"并把它挂在本卡，
// 理由是回款行落地之前读出来的只有金额，而"还差多少"要有钱进库才算得出。
var billRouteSpecs = []string{
	"GET /api/bill/:id",
	"GET /api/bill/of-quote/:quote_id",
	"POST /api/bill",
}

// billRouteReasonNotFound 404 那一档在**线上**长什么样：这一格写死成字面量，
// 而不是引控制器里的那个常量 —— 常量改名是本层的事，而 wire 上的词是契约。
// 引用生产常量会让"把 reason 改成 not_exist"这种一次都不红的改动成为可能。
const billRouteReasonNotFound = "not_found"

func billReadSource(t *testing.T) string {
	t.Helper()
	return readSourceFile(t, "../controller/bill.go")
}

// newBillTestEngine 只挂控制器（不经过装配层）：两个 nil 复现"两条腿都没装配"。
func newBillTestEngine(derive controller.BillDeriver, read controller.BillReader, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewBillController(derive, read).RegisterRoutes(auth)
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
	controller.NewBillController(nil, nil).RegisterRoutes(engine.Group("/api"))

	var got []string
	for _, r := range engine.Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, billRouteSpecs) {
		t.Errorf("路由表与本卡声明的集合不一致：\n 得到 %v\n 期望 %v", got, billRouteSpecs)
	}
}

// TestBillRoutes_NoStatusWriteEndpoints 不许存在的入口，逐条 404 探针。
//
// 判据不是"还没写"，是"写了就错"：
//   - /pay、/void、/status：结清与作废要读回款行（T-P7-02），逾期与催收属 T-P7-03；
//     今天开这一格，就是给财务台账开一条**不走回款**的捷径，而 AC② 的对账正要读那两格。
//   - GET /api/bill（列表）：账单没有租户列（判据见 model/bill.go），
//     列全表的端点在补上归属列之前等于"任何人可看全公司的应收"。
//     它与本卡新加的两条 GET 的分工：那两条**都必须带一把键**（账单号 / 报价号），
//     所以"读一张单"合法、"读所有单"仍然 404 —— 键就是这一族目前的归属判据。
//   - PUT /api/bill/:id：金额列建后即不可改（它是对账的凭证），可改的那格不叫凭证。
//
// 光看路由表不够（有人会把动作塞进已有端点的请求体里），所以再读一遍控制器源码：
// UpdateStatus / MarkPaid 这类名字一旦出现在这一层，就说明有人接了状态写口。
func TestBillRoutes_NoStatusWriteEndpoints(t *testing.T) {
	engine := newBillTestEngine(&billFakeDeriver{}, &billFakeReader{}, uint(7))
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/bill"},
		{http.MethodPut, "/api/bill/b_1"},
		{http.MethodPatch, "/api/bill/b_1"},
		{http.MethodDelete, "/api/bill/b_1"},
		{http.MethodPost, "/api/bill/b_1/pay"},
		{http.MethodPost, "/api/bill/b_1/void"},
		{http.MethodPost, "/api/bill/b_1/status"},
		{http.MethodPost, "/api/bill/quote/q_1/accept"},
		{http.MethodPost, "/api/bill/b_1/send"},
		// 读口的形状同样是契约：读**只能**读一张单、或读一张报价名下的全部单。
		// 这三格分别是"列一张单名下的回款行""按报价再套一层""按客户捞"，
		// 全部要求先有归属列（判据见 model/bill.go），且它们想要的结论 Statement 已经带出来了。
		{http.MethodGet, "/api/bill/b_1/payments"},
		{http.MethodGet, "/api/bill/of-quote/q_1/bills"},
		{http.MethodGet, "/api/bill/customer/13800000000"},
	} {
		if code, _, raw := doBillRequest(t, engine, probe.method, probe.path, "", ""); code != http.StatusNotFound {
			t.Errorf("%s %s 回 %d，期望 404：本卡不得暴露任何写回款/作废/账期或列全表的入口（%s）",
				probe.method, probe.path, code, raw)
		}
	}

	src := billReadSource(t)
	// 读腿落地之后这一族 needles **一格都没放松**。新的 BillReader 接缝里两格都叫 Statement*，
	// 不叫 GetByID / List：控制器要的是"这张单收了多少、还欠多少"这个**结论**，
	// 而不是库里两把表的行 —— 求和住在服务层（service/payment.go 的 statementOfBill），
	// 在这里重算一遍就是第二个事实源，而它正是 AC② 要对账的那个东西。
	for _, needle := range []string{"UpdateStatus(", "MarkPaid", "Void(", "List(", "GetByID("} {
		if strings.Contains(src, needle) {
			t.Errorf("控制器源码里出现了 %q：状态跃迁不经本层出口，读账单也不许绕过服务层去碰仓储", needle)
		}
	}
}

// billFakeReader 同上：只为把读腿那两条路由挂出来而存在（业务结论由 ⑥ 的活体用例判）。
type billFakeReader struct{}

func (billFakeReader) Available() bool { return true }

func (billFakeReader) Statement(context.Context, string) (*service.BillStatementView, error) {
	return &service.BillStatementView{BillID: "b_probe", Status: model.BillStatusOpen}, nil
}

func (billFakeReader) StatementsOfQuote(context.Context, string) ([]*service.BillStatementView, error) {
	return []*service.BillStatementView{{BillID: "b_probe", Status: model.BillStatusOpen}}, nil
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
		// Swagger 的 {id} 与 gin 的 :id 归一之后才谈得上"逐条对上"。
		path := fields[0]
		for strings.Contains(path, "{") {
			i := strings.Index(path, "{")
			j := strings.Index(path, "}")
			if j < i {
				break
			}
			path = path[:i] + ":" + path[i+1:j] + path[j+1:]
		}
		declared[strings.ToUpper(strings.Trim(fields[1], "[]"))+" "+path] = true
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

// TestBillRoutes_PaymentLegAssembledBeforeBothConsumers 回款腿的装配点必须排在**两个**消费点之前。
//
// 这一条与上面那条同判据但不同参照物：读腿的全局实例有**两处**读者，而第二处不在本族文件里。
// `NewIntegrationService()` 在**构造那一刻**取全局（不在每次回调里现取，理由写在那儿：
// 请求路径读包级全局会撞上 scripts/check-async-global-read.py 的零条目基线），
// 而它唯一的生产调用点在 setupIntegrationRoutes 里。于是把 `app.InitPaymentRuntime` 那一句
// 挪到它后面，症状是"订单 webhook 收了单、正文说回款腿未装配"——
// 本文件那九条用例**全部照样绿**（它们走的是 /api/bill 那条路，而且挂载点自己就在那句之后）。
// 所以这一格只能按字面量锁：三处文本的先后关系，缺一不可。
func TestBillRoutes_PaymentLegAssembledBeforeBothConsumers(t *testing.T) {
	src := readSourceFile(t, "router.go")
	initAt := strings.Index(src, "app.InitPaymentRuntime(gormDB)")
	billMountAt := strings.Index(src, "setupBillRoutes(auth)")
	hookMountAt := strings.Index(src, "setupIntegrationRoutes(auth)")

	if initAt < 0 {
		t.Error("router.go 里没有 app.InitPaymentRuntime(gormDB)：回款腿与对账读腿在生产启动时没人装")
	}
	if hookMountAt < 0 {
		t.Fatal("router.go 里找不到 setupIntegrationRoutes(auth) 这一句：本条用例对 webhook 挂载点的参照物没了")
	}
	if initAt < 0 {
		return
	}
	if billMountAt >= 0 && initAt > billMountAt {
		t.Errorf("InitPaymentRuntime（第 %d 字节）写在 setupBillRoutes（第 %d 字节）之后：两条 GET 永久 503",
			initAt, billMountAt)
	}
	if initAt > hookMountAt {
		t.Errorf("InitPaymentRuntime（第 %d 字节）写在 setupIntegrationRoutes（第 %d 字节）之后："+
			"集成服务构造时取到的是 nil ⇒ 带 payment 的回调永远记不到钱，而 /api/bill 看起来一切正常",
			initAt, hookMountAt)
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
	engine := newBillTestEngine(nil, nil, uint(7))
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
	prevPay := service.GlobalPaymentService()
	defer func() {
		service.SetGlobalBillService(prev)
		service.SetGlobalPaymentService(prevPay)
	}()
	service.SetGlobalBillService(nil)
	// 读腿也要洗：上面 TestBillRoutes_MountedBySetup 里的 Setup 已经把生产实例装进过全局，
	// 不洗的话本条的 503 探针读到的是**上一用例那把（可能已经收掉的）库**，
	// 得到 500 或 200 都被读成"装配形状不对"，而真正在判的那一格（挂载在装配之前）就没人看了。
	service.SetGlobalPaymentService(nil)

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
	sort.Strings(mounted)
	if !reflect.DeepEqual(mounted, billRouteSpecs) {
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
	// 读腿同理：装配缺失时"读不到"必须是 503，不能是 404 或 200 空单。
	// 404 会被前端读成"这张应收不存在"，而此刻的事实是"我们一次都没查"。
	if code, data, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/b_probe", "", ""); code != http.StatusServiceUnavailable {
		t.Errorf("未装配时读单回 %d，期望 503：%s", code, raw)
	} else if data == nil || data["reason"] != "unavailable" {
		t.Errorf("读腿 503 的 data=%v，期望带 reason=unavailable：%s", data, raw)
	}
	if code, data, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/of-quote/qt_probe", "", ""); code != http.StatusServiceUnavailable {
		t.Errorf("未装配时按报价读回 %d，期望 503：%s", code, raw)
	} else if data == nil || data["reason"] != "unavailable" {
		t.Errorf("按报价读 503 的 data=%v，期望带 reason=unavailable：%s", data, raw)
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
	billSeedChain(t, database, billLiveOppID, billLiveRowID, billLiveChainID)
}

// billSeedChain 造一条"已发出、带两行、合计 369.99"的报价链。
//
// 三把 ID 由调用方给：影子库按进程共享，写死同一把的话第二条活体用例一起步就撞
// "这条链已经成交过"（那是服务层的判据③在正常工作，不是用例要判的事）。
// 行项目与状态都走仓储的写路径：直插会替仓储兜住"这一格根本写不进去"那类错误。
func billSeedChain(t *testing.T, database *gorm.DB, oppID, rowID, chainID string) {
	t.Helper()
	ctx := context.Background()
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(ctx, &model.Opportunity{
		ID: oppID, Code: "OPP-" + chainID, CustomerID: "cus_" + oppID, OneID: "one_" + oppID,
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_live",
	}); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
	repo := repository.NewQuoteRepositoryWithDB(database)
	row := &model.Quote{
		ID: rowID, QuoteID: chainID, OpportunityID: oppID,
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault,
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("落报价第一版失败：%v", err)
	}
	lines := []*model.QuoteLineItem{
		{LineNo: 1, ProductID: "p-a", Title: "标准版坐席 ×3", Quantity: 3, UnitPrice: 100, DiscountPercent: 10, Amount: 270},
		{LineNo: 2, ProductID: "p-b", Title: "实施包", Quantity: 1, UnitPrice: 99.99, DiscountPercent: 0, Amount: 99.99},
	}
	if err := repo.AddLines(ctx, rowID, lines); err != nil {
		t.Fatalf("落行项目失败：%v", err)
	}
	if err := repo.UpdateStatus(ctx, rowID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("把报价推到 sent 失败：%v", err)
	}
}

// TestBillRoutes_LiveReadSideThroughRealSetup 读口的关闸用例：从 HTTP 读到"还差多少钱"。
//
// 它钉住的四件事，没有一件能在别处钉：
//  1. Setup **自己**把回款腿也装起来了（开局洗全局，同派生腿那条的老路）；
//     少了这一条，读口在生产上是"路由在、腿不在"⇒ 永久 503，而 503 在单测里也是合法答复；
//  2. 读出来的 settled / outstanding 与**库里回款行的和**一致（不是视图自己算的巧合）；
//  3. 钱进来之后账单状态由回款累加推动（open→partial→paid），且这一格是从库里读回来的；
//  4. 鉴权：账单没有租户列，读单落在鉴权链之外就等于任何人可看别人的应收。
//
// 入账走 service.GlobalPaymentService()（生产实例、真库），而不是再开一个 HTTP 入口：
// 回款进库的唯一生产通路是订单 webhook，那一条已由 package service 与 package controller
// 各自的文件守着；这里要判的是**读**的那一侧。
func TestBillRoutes_LiveReadSideThroughRealSetup(t *testing.T) {
	prevBill := service.GlobalBillService()
	prevPay := service.GlobalPaymentService()
	defer func() {
		service.SetGlobalBillService(prevBill)
		service.SetGlobalPaymentService(prevPay)
	}()
	service.SetGlobalBillService(nil)
	service.SetGlobalPaymentService(nil)

	database := testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{}, &model.Bill{},
		&model.Payment{}, &model.SystemConfigKV{}, &model.SalesEvent{}, &model.ApprovalRequest{})

	prevDB := dbutil.GetDB()
	defer dbutil.SetTestDB(prevDB)
	dbutil.SetTestDB(database)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	if svc := service.GlobalPaymentService(); svc == nil || !svc.Available() {
		t.Fatal("Setup 之后回款腿仍未装配：两条 GET 只会回 503（app.InitPaymentRuntime 没跑，或跑在路由之后）")
	}
	const (
		oppID   = "opp_bill_read"
		rowID   = "q_bill_read_1"
		chainID = "QT-BILL-READ"
	)
	billSeedChain(t, database, oppID, rowID, chainID)

	tok, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("签发测试令牌失败：%v", err)
	}
	code, data, raw := doBillRequest(t, engine, http.MethodPost, "/api/bill", tok, `{"quote_row_id":"`+rowID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("派生回 %d：%s", code, raw)
	}
	billID, _ := data["id"].(string)
	if billID == "" {
		t.Fatalf("派生响应里没有账单号：%v", data)
	}

	// ① 一张钱没收的单：settled 0、outstanding 等于全额、payments 是空数组而不是 null。
	code, st, raw := doBillRequest(t, engine, http.MethodGet, "/api/bill/"+billID, tok, "")
	if code != http.StatusOK {
		t.Fatalf("读单回 %d：%s", code, raw)
	}
	if st["bill_id"] != billID || st["quote_id"] != chainID || st["quote_row_id"] != rowID {
		t.Errorf("读到的三把键不对：%v", st)
	}
	if st["status"] != model.BillStatusOpen {
		t.Errorf("新单状态=%v，期望 %s", st["status"], model.BillStatusOpen)
	}
	if st["amount"] != 369.99 || st["settled"] != float64(0) || st["outstanding"] != 369.99 {
		t.Errorf("一分钱没收时 amount/settled/outstanding = %v/%v/%v，期望 369.99/0/369.99",
			st["amount"], st["settled"], st["outstanding"])
	}
	if rows, ok := st["payments"].([]any); !ok || len(rows) != 0 {
		t.Errorf("payments = %v，期望空数组（null 会让前端把'没有回款'与'没读到'并成一格）", st["payments"])
	}

	// ② 按报价号捞：同一张单，另一个入口。
	code, list, raw := doBillRequest(t, engine, http.MethodGet, "/api/bill/of-quote/"+chainID, tok, "")
	if code != http.StatusOK {
		t.Fatalf("按报价读回 %d：%s", code, raw)
	}
	items, _ := list["list"].([]any)
	if len(items) != 1 {
		t.Fatalf("按报价读到 %d 张，期望 1：%v", len(items), list)
	}
	if first, _ := items[0].(map[string]any); first["bill_id"] != billID {
		t.Errorf("两个入口读到不同的单：%v vs %s", first["bill_id"], billID)
	}

	// ③ 一笔 200 进来：settled 与 outstanding 都要跟着动，状态推到 partial。
	billBookPayment(t, billID, "ref-read-1", 200)
	code, st, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/"+billID, tok, "")
	if code != http.StatusOK {
		t.Fatalf("入账后读单回 %d：%s", code, raw)
	}
	if st["settled"] != 200.0 || st["outstanding"] != 169.99 {
		t.Errorf("入账 200 之后 settled/outstanding = %v/%v，期望 200/169.99", st["settled"], st["outstanding"])
	}
	if st["status"] != model.BillStatusPartial {
		t.Errorf("状态=%v，期望 %s（结清由回款累加推动，这是 T-P7-02 的全部判据）", st["status"], model.BillStatusPartial)
	}
	if rows, _ := st["payments"].([]any); len(rows) != 1 {
		t.Errorf("payments %d 行，期望 1：%v", len(rows), st["payments"])
	}

	// ④ 补足尾款：库里那一行必须自己变成 paid，而不是"读出来像 paid"。
	billBookPayment(t, billID, "ref-read-2", 169.99)
	code, st, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/"+billID, tok, "")
	if code != http.StatusOK {
		t.Fatalf("结清后读单回 %d：%s", code, raw)
	}
	if st["status"] != model.BillStatusPaid || st["settled"] != 369.99 || st["outstanding"] != float64(0) {
		t.Errorf("结清之后 = %v/%v/%v，期望 paid/369.99/0", st["status"], st["settled"], st["outstanding"])
	}
	var stored model.Bill
	if err := database.First(&stored, "id = ?", billID).Error; err != nil {
		t.Fatalf("读回账单失败：%v", err)
	}
	if stored.Status != model.BillStatusPaid {
		t.Errorf("库里那一行是 %s，期望 %s —— 视图说结清而库里没说，对账就有两份事实", stored.Status, model.BillStatusPaid)
	}

	// ⑤ 不存在的单：404 + reason，且不许是 500 或 200 空对象。
	if code, data, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/b_does_not_exist", tok, ""); code != http.StatusNotFound {
		t.Errorf("读不存在的单回 %d，期望 404：%s", code, raw)
	} else if data["reason"] != billRouteReasonNotFound {
		t.Errorf("404 的 reason=%v，期望 %q：%v", data["reason"], billRouteReasonNotFound, data)
	}

	// ⑥ 匿名：与上面那条 200 同一个体、同一个路径，去掉令牌。
	if code, _, raw = doBillRequest(t, engine, http.MethodGet, "/api/bill/"+billID, "", ""); code >= 200 && code < 300 {
		t.Errorf("匿名读单回 %d：应收明细落在了鉴权链之外（本表没有租户列）：%s", code, raw)
	}
}

// billBookPayment 用**生产实例**记一笔回款（入账的 HTTP 通路是订单 webhook，不在本文件判）。
func billBookPayment(t *testing.T, billID, ref string, amount float64) {
	t.Helper()
	svc := service.GlobalPaymentService()
	if svc == nil {
		t.Fatal("回款腿没装起来，无法入账")
	}
	if _, err := svc.RecordPayment(context.Background(), service.RecordPaymentInput{
		BillID: billID, ChannelRef: ref, Amount: amount, Platform: "taobao", OrderID: "ord-" + ref,
	}); err != nil {
		t.Fatalf("入账 %s %v 失败：%v", ref, amount, err)
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
