// quote_routes_test.go T-P6-03：报价 HTTP 出口的挂载点、契约面与"真 Setup 走一遍"。
//
// 与 controller/quote_test.go 的分工：那一份钉的是"四种处置在状态码上分不分得开"，
// 本份钉的是"这四条端点在不在、挂在哪个鉴权组下、以及摘掉挂载那一条会不会有人发现"。
// 两条都是非行为断言的静态锁（路由表里没有内联闭包、每条端点有 @Router 注解），
// 理由与商机那一路一样：只有"把那一处摘掉/加回去"的注码能让它们红，
// 所以配套的变异在电池里，而不是在这里自我声明。
package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
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

// quoteRouteSpecs 本卡交付的全部端点（顺序无关，判据按集合比）。
//
// 少一条 = 前端有一个按钮点了报 404，而 404 在网关日志里与"路径写错"长得一模一样；
// 多一条 = 有一个没人声明的入口在跑（本域最危险的两条是"能写 accepted 的那一条"
// 与"能改审批结论的那一条"）。
var quoteRouteSpecs = []string{
	"GET /api/quote/:id",
	"GET /api/quote/latest/:quoteID",
	"POST /api/quote",
	"POST /api/quote/:id/send",
}

func quoteReadSource(t *testing.T) string {
	t.Helper()
	return readSourceFile(t, "../controller/quote.go")
}

// newQuoteTestEngine 只挂控制器（不经过装配层）：两个 nil 复现"两条腿都没装配"。
func newQuoteTestEngine(reads controller.QuoteViewReader, sends controller.QuoteSender, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewQuoteController(reads, sends).RegisterRoutes(auth)
	return engine
}

// —— ① 路由表 ——————————————————————————————————————————

func TestQuoteRoutes_TableIsExactlyTheDeclaredSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller.NewQuoteController(nil, nil).RegisterRoutes(engine.Group("/api"))

	var got []string
	for _, r := range engine.Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(got)
	want := append([]string(nil), quoteRouteSpecs...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("路由表与本卡声明的集合不一致：\n 得到 %v\n 期望 %v", got, want)
	}
}

// TestQuoteRoutes_NoEndpointWritesApprovalOrAccepted 两条不许存在的入口。
//
// `/approve`：发送腿拿不到裁决入口是它全部的所以（批的人与发的人必须不是同一个人），
// HTTP 侧一开这个口，那条判据就只剩注释。
// `/accept`：接受报价是**客户**的动作，判据属于 P7（回款派生）。销售侧点得出 accepted，
// 漏斗上的"赢单"就会先于回款出现，而那条三元边表（只有 collection_completed 放行 won）
// 当场被绕开。
// 光看路由表不够（有人会把动作塞进已有端点的体里），所以再读一遍控制器源码：
// Decide 这个名字一旦出现在这一层，就说明有人接了裁决入口。
func TestQuoteRoutes_NoEndpointWritesApprovalOrAccepted(t *testing.T) {
	engine := newQuoteTestEngine(nil, nil, uint(7))
	for _, probe := range []struct{ method, path string }{
		{http.MethodPost, "/api/quote/q_1/approve"},
		{http.MethodPost, "/api/quote/q_1/accept"},
		{http.MethodPost, "/api/quote/q_1/reject"},
		{http.MethodPost, "/api/quote/q_1/expire"},
		{http.MethodPost, "/api/quote/q_1/status"},
		{http.MethodPut, "/api/quote/q_1"},
		{http.MethodDelete, "/api/quote/q_1"},
		{http.MethodGet, "/api/quote"},
		{http.MethodGet, "/api/quote/q_1/versions"},
	} {
		if code, _, _ := doQuoteRequest(t, engine, probe.method, probe.path, ""); code != http.StatusNotFound {
			t.Errorf("%s %s 回 %d，期望 404：本卡不得暴露任何写审批结论/accepted 状态或列全表的入口",
				probe.method, probe.path, code)
		}
	}

	src := quoteReadSource(t)
	for _, needle := range []string{"Decide(", "ExpireOverdue(", "UpdateStatus(", "MarkAccepted"} {
		if strings.Contains(src, needle) {
			t.Errorf("控制器源码里出现了 %q：裁决与状态跃迁都不经 HTTP 出口", needle)
		}
	}
}

// —— ② 无内联 handler + ③ Swagger 覆盖（同商机那两条的口径）——————————

func TestQuoteRoutes_RouterFileHasNoInlineHandler(t *testing.T) {
	src := readSourceFile(t, "quote_routes.go")

	for _, needle := range []string{"gin.Context", ".JSON(", "AbortWithStatus", "response.Success", "response.Error"} {
		if strings.Contains(src, needle) {
			t.Errorf("路由文件里出现了 %q：本层只做 URL→Controller 映射，响应必须由控制器写", needle)
		}
	}
	if !strings.Contains(src, "RegisterRoutes(") {
		t.Error("路由文件没有把挂载委托给控制器的 RegisterRoutes：端点清单因此不在唯一一处可查")
	}
}

func TestQuoteRoutes_SwaggerCoversEveryRoute(t *testing.T) {
	src := quoteReadSource(t)

	declared := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "// @Router") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, "// @Router")
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			t.Errorf("@Router 注解形状不对：%q", trimmed)
			continue
		}
		path := strings.NewReplacer("{id}", ":id", "{quoteID}", ":quoteID").Replace(fields[0])
		method := strings.Trim(fields[1], "[]")
		declared[strings.ToUpper(method)+" "+path] = true
	}
	for _, spec := range quoteRouteSpecs {
		if !declared[spec] {
			t.Errorf("%s 没有对应的 @Router 注解：Swagger 面上缺这条端点", spec)
		}
	}
	// 202 是这一版新增的形状：文档里没有 202 那一格，读文档的人就不知道
	// 这一格除了 200 还会回什么，前端照文档实现会漏掉"等审批"那一档。
	// 按字段比而不是照 "@Success 202" 这个字面量比：注解是列对齐写的
	// （`@Success      202`），字面量判据会在一份好文档上假红。
	if !swaggerDeclaresStatus(src, "202") {
		t.Error("发送那一条没有 @Success 202 注解：awaiting 这一档在生成的文档里不存在")
	}
	for _, needle := range []string{"@Summary", "@Tags", "@Success", "@Param"} {
		if !strings.Contains(src, needle) {
			t.Errorf("控制器缺 %s 注解：生成的文档只剩路径，读的人不知道这条端点回答什么问题", needle)
		}
	}
}

// swaggerDeclaresStatus 控制器源码里有没有任何一格 @Success 声明了这个状态码。
func swaggerDeclaresStatus(src, status string) bool {
	for _, line := range strings.Split(src, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "//" && f[1] == "@Success" && f[2] == status {
			return true
		}
	}
	return false
}

// —— ④ 挂载位置：装配之后、鉴权中间件之后 ——————————————

// TestQuoteRoutes_MountedAtTheRightPlace 两道顺序锁，各挡一种"挂上了但挂错地方"的坏法。
//
// 为什么是静态锁而不是匿名探针（本仓的匿名探针在报价这一族上判不出东西）：
// 这四条端点在没有身份时**本来就该**回非 2xx（GET 读不到行是 404、发送没有操作者是 401），
// 所以"匿名回非 2xx"在挂对与挂错两种情况下都绿 —— 那条判据在这里没有牙。
// 有牙的那一格（匿名调用若放理会真的写出东西）需要全套活夹具，归 LiveThroughRealSetup 的 ⑥。
//
//  1. `app.InitQuoteRuntime` 必须先于挂载：挂载时读一次全局实例，读早了就是两条永久的 nil
//     ⇒ 四条端点全部 503，而 503 在别处也是"合法"响应，没人会去查装配顺序。
//  2. 必须先于 `auth.Use(JWTAuthMiddleware())` 挂载：gin 的 Use 只对**注册之后**建起来的路由
//     生效（同文件里那段 2026-09 的历史缺陷注释就是这件事），写在 Use 之前的端点永远不带鉴权。
func TestQuoteRoutes_MountedAtTheRightPlace(t *testing.T) {
	src := readSourceFile(t, "router.go")
	initAt := strings.Index(src, "app.InitQuoteRuntime(gormDB)")
	mountAt := strings.Index(src, "setupQuoteRoutes(auth)")
	useAt := strings.Index(src, "auth.Use(middleware.JWTAuthMiddleware())")

	if initAt < 0 {
		t.Error("router.go 里没有 app.InitQuoteRuntime(gormDB)：报价两条腿在生产启动时没人装")
	}
	if mountAt < 0 {
		t.Error("router.go 里没有 setupQuoteRoutes(auth)：本卡交付的端点没进生产路由树")
	}
	if useAt < 0 {
		// 这一格红不是本卡的错，是"参照物被搬走了"：判据得知道自己比的是哪一行。
		t.Fatal("router.go 里找不到 auth.Use(middleware.JWTAuthMiddleware()) 这一句：本条的第二道锁失去了参照物，请先确认鉴权组那一段还在不在")
	}
	if mountAt < 0 || initAt < 0 {
		return
	}
	if initAt > mountAt {
		t.Errorf("InitQuoteRuntime（第 %d 字节）写在 setupQuoteRoutes（第 %d 字节）之后：挂载时两条腿还是空的，/api/quote/* 永久 503",
			initAt, mountAt)
	}
	if mountAt < useAt {
		t.Errorf("setupQuoteRoutes 写在 auth.Use(JWTAuthMiddleware()) 之前：那四条端点拿不到鉴权链，匿名即可读报价、写草稿")
	}
	// 挂到哪个组是第三个字面量判据：参数写成 public/r 时上面两道顺序锁全部照样绿。
	if !strings.Contains(src, "setupQuoteRoutes(auth)") || strings.Contains(src, "setupQuoteRoutes(public") || strings.Contains(src, "setupQuoteRoutes(r,") || strings.Contains(src, "setupQuoteRoutes(r)") {
		t.Error("setupQuoteRoutes 的接收者不是 auth 组：报价端点落在了鉴权链之外")
	}
}

// TestQuoteRoutes_MountedBySetup 判据只有一条：Setup() 之后这四条端点在引擎上。
//
// 摘掉 setupQuoteRoutes 那一句，本条立刻红，而上面所有用例仍然全绿 ——
// 这就是它必须单独存在的原因。
//
// 这里必须先把全局 DB 句柄指过去：Setup 内部的 RegisterAllAgentTools 会经
// LoadAgentSettingsConfig 读一次 system_config_kv，而那条路径用的是
// db.GetDB()（不是传进来的 gormDB）—— 句柄为 nil 时它不是返回 error 而是 panic。
// 这是本卡量出来的既有事实（不是报价竖引入的），已登记进交接清单。
func TestQuoteRoutes_MountedBySetup(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{})

	prevDB := dbutil.GetDB()
	defer dbutil.SetTestDB(prevDB)
	dbutil.SetTestDB(database)

	routes := quoteRoutesFromSetup(t, database)
	got := map[string]bool{}
	for _, spec := range routes {
		got[spec] = true
	}
	for _, spec := range quoteRouteSpecs {
		if !got[spec] {
			t.Errorf("Setup() 挂出来的表里没有 %s：本卡的端点没进生产路由", spec)
		}
	}
}

// quoteRoutesFromSetup 跑一次真 Setup 并列出 /api/quote* 的路由。
//
// Setup 是可重复调用的装配函数（测试进程里多个用例各跑一遍），这里每次都新建引擎：
// 复用同一份路由表会让"这一次到底挂没挂"读成上一次的结果。
func quoteRoutesFromSetup(t *testing.T, database *gorm.DB) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	var out []string
	for _, r := range engine.Routes() {
		if strings.HasPrefix(r.Path, "/api/quote") {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

// TestQuoteRoutes_LiveThroughRealSetup 真 Setup + 真装配 + 真库走一遍：
// 生成 → 发送（未批 ⇒ 202）→ 读回（含开放审批那一格）→ 按链读最新 → 越界入参拒。
//
// 这一条是整个出口的关闸用例。它钉住四件上面那些用例各自钉不住的事：
//  1. **Setup 自己**把两条腿装起来了（开局先清全局：不洗这一把的话，摘掉
//     `app.InitQuoteRuntime` 那一行仍然能靠上一条用例留在全局里的实例读到 200 ——
//     商机竖的 M33 就是这么活过去的）；
//  2. 装配点给的那九个句柄接的是**同一把库**（缺任何一个都只会回 503，而 503 在这里是红）；
//  3. 停在 pending 这件事在 HTTP 面上**读得出来**（AC① 的"100% 停在 pending"若只能从
//     服务层返回值看，操作者刷新的时候看到的就是一个没有任何变化的草稿页）；
//  4. 这四条端点在鉴权链之内 —— 用 ⑥ 那一格判：同一个能让 ① 写出行的体，去掉令牌之后
//     既不能回 2xx、也不能留下任何行。
func TestQuoteRoutes_LiveThroughRealSetup(t *testing.T) {
	prevGen, prevSend := service.GlobalQuoteService(), service.GlobalQuoteSendService()
	defer service.SetGlobalQuoteService(prevGen)
	defer service.SetGlobalQuoteSendService(prevSend)
	service.SetGlobalQuoteService(nil)
	service.SetGlobalQuoteSendService(nil)

	database := testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{},
		&model.ScriptLibrary{}, &model.ScriptVersion{}, &model.HumanTask{})

	// 全局句柄必须在本格任何装配之前指过去：ltc.config 的服务是**第一次被读的那一刻**
	// 捕获 db.GetDB() 的，晚指就等于让闸门永远握着一把 nil 句柄（读不到 ⇒ 按"关"处理，
	// 于是这一格红在"闸门关着"上，红因看着像运营没开阶段）。
	prevDB := dbutil.GetDB()
	defer dbutil.SetTestDB(prevDB)
	dbutil.SetTestDB(database)
	// 让闸门服务在**这把库**上重建（同包里的上一条用例可能已把它建在别的句柄上），
	// 收尾同样置空：留着会把这把测试库的句柄递给下一条用例。
	service.SetGlobalLTCConfig(nil)
	defer service.SetGlobalLTCConfig(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	if service.GlobalQuoteService() == nil || service.GlobalQuoteSendService() == nil {
		t.Fatal("Setup 之后报价两条腿仍有空的：/api/quote/* 全部只会回 503（app.InitQuoteRuntime 没跑，或跑在路由之后）")
	}
	quoteOpenStage(t)
	quoteSeedScriptAndTemplate(t, database)
	quoteSeedOpportunity(t, database)

	tok, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("签发测试令牌失败：%v", err)
	}

	// ① 生成：拿回版本行键与本次解析到的话术。
	code, env, raw := doQuoteLive(t, engine, http.MethodPost, "/api/quote", tok,
		`{"opportunity_id":"`+quoteLiveOppID+`","template_code":"std"}`)
	if code != http.StatusOK {
		t.Fatalf("生成回 %d：%s —— %s", code, env.Message, raw)
	}
	view := quoteDataFrom(t, env)
	rowID, _ := view["id"].(string)
	if rowID == "" {
		t.Fatalf("生成结果里没有 id：%v", view)
	}
	if _, ok := view["script"]; !ok {
		t.Errorf("生成响应里没有 script：%v —— 话术取自哪一版是 AC① 要能被读出来的那件事", view)
	}

	// ② 未批就发：202 + 带回审批号（AC② 的形状：请求线程到此结束）。
	code, env, raw = doQuoteLive(t, engine, http.MethodPost, "/api/quote/"+rowID+"/send", tok, "")
	if code != http.StatusAccepted {
		t.Fatalf("未批的发送回 %d，期望 202：%s —— %s", code, env.Message, raw)
	}
	sent := quoteDataFrom(t, env)
	if sent["disposition"] != service.QuoteSendAwaiting {
		t.Errorf("disposition=%v，期望 %s", sent["disposition"], service.QuoteSendAwaiting)
	}
	approvalID, _ := sent["approval_id"].(string)
	if approvalID == "" {
		t.Fatalf("没带回审批号：%v", sent)
	}
	for _, needle := range []string{"resume_token", "ResumeToken", "rt_"} {
		if strings.Contains(raw, needle) {
			t.Errorf("响应里出现了 %q：恢复凭证不是业务字段", needle)
		}
	}
	// AC③ 的反向：审批入队**不**写发送事件。写成"发了"的事件会让漏斗与看板同时说谎，
	// 而那一刻库里那一版还是 draft。
	if n := quoteEventRows(t, database, rowID); n != 0 {
		t.Errorf("未批的发送在 sales_events 里留下 %d 行，期望 0（事件是发送成功的凭证，不是点过按钮的凭证）", n)
	}

	// ③ 读回那一版：库里仍是 draft，而开放审批读得出来。
	code, env, raw = doQuoteLive(t, engine, http.MethodGet, "/api/quote/"+rowID, tok, "")
	if code != http.StatusOK {
		t.Fatalf("读回回 %d：%s —— %s", code, env.Message, raw)
	}
	back := quoteDataFrom(t, env)
	if back["status"] != model.QuoteStatusDraft {
		t.Errorf("库里那一版是 %v，期望 %s（未批不得外发，也不得改状态）", back["status"], model.QuoteStatusDraft)
	}
	if back["approval_lookup"] != "found" {
		t.Errorf("approval_lookup=%v，期望 found：%v", back["approval_lookup"], back)
	}
	oa, _ := back["open_approval"].(map[string]any)
	if oa["approval_id"] != approvalID {
		t.Errorf("读到的开放审批是 %v，与发送带回的 %s 不是同一条", oa["approval_id"], approvalID)
	}
	if oa["status"] != model.ApprovalStatusPending {
		t.Errorf("开放审批 status=%v，期望 %s", oa["status"], model.ApprovalStatusPending)
	}

	// ④ 按逻辑号读最新：拿到的行键与 ① 那一版同一个（sales_events 里只有逻辑号，
	// 这一格读不回行键的话，事后从一条事件回到"当时发的是哪一版"就只能翻日志）。
	code, env, raw = doQuoteLive(t, engine, http.MethodGet, "/api/quote/latest/"+quoteChainIDOf(t, database, rowID), tok, "")
	if code != http.StatusOK {
		t.Fatalf("按链读最新回 %d：%s —— %s", code, env.Message, raw)
	}
	if latest := quoteDataFrom(t, env); latest["id"] != rowID {
		t.Errorf("按链读最新得到 %v，期望 %s", latest["id"], rowID)
	}

	// ⑤ 收件人不是入参：带 one_id 的体必须 400，且服务层一次都没被调用（越界入参不落到"静默丢掉"）。
	code, env, _ = doQuoteLive(t, engine, http.MethodPost, "/api/quote", tok,
		`{"opportunity_id":"`+quoteLiveOppID+`","template_code":"std","one_id":"one_victim"}`)
	if code != http.StatusBadRequest {
		t.Errorf("带 one_id 的生成回 %d，期望 400：收件人一旦能从体里递进来，分桶就不再属于被发给的那个人", code)
	}
	if got := quoteReasonFrom(t, env); got != "input_invalid" {
		t.Errorf("reason=%q，期望 input_invalid", got)
	}

	// ⑥ 匿名调用那一条**能成功**的生成：这是四格里唯一有牙的鉴权探针。
	//
	// 前面几条端点在无身份时本来就回 4xx（读不到行是 404、发送没操作者是 401），
	// 所以"匿名回非 2xx"挂在 auth 组与挂在 public 组两种情况下都绿 —— 那种探针判不出挂载位置。
	// 这一条不一样：上面 ① 用同一个体、带着令牌拿到了 200 并真的写了一行。
	// 去掉令牌之后再打一次，若仍然 2xx 就说明这条端点在鉴权链之外，
	// 而后果不是"看到别人的数据"而是**任何人**都能在任意商机下长出报价草稿（每张草稿都要人去审）。
	body := `{"opportunity_id":"` + quoteLiveOppID + `","template_code":"std"}`
	before := quoteRowCount(t, database)
	code, env, raw = doQuoteLive(t, engine, http.MethodPost, "/api/quote", "", body)
	if code >= 200 && code < 300 {
		t.Errorf("匿名生成回 %d：%s —— 报价写入口落在了鉴权链之外（%s）", code, env.Message, raw)
	}
	if after := quoteRowCount(t, database); after != before {
		t.Errorf("匿名调用之后报价行数从 %d 变成 %d：请求被拒了但**副作用**留下来了", before, after)
	}
}

// quoteRowCount 库里报价行的总数（匿名副作用探针的对照量）。
func quoteRowCount(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.Quote{}).Count(&n).Error; err != nil {
		t.Fatalf("数报价行失败：%v", err)
	}
	return n
}

// —— 活体用例的夹具 ——————————————————————————————————
//
// 三份夹具写的都是**运营写得到的东西**（配置与内容行），不是被测代码：
// 直接改全局变量或注一份内存 gate 的话，这一格测的就是夹具而不是装配点。

const quoteLiveOppID = "opp_quote_live"

// quoteOpenStage 用运营用的同一个入口（LTCConfigService.Save）打开报价阶段。
// 往库里手写一份 JSON 等于绕过校验器，装配测试就会在"生产写不出来的配置"上变绿。
// 它读的是**装配点用的那同一个全局实例**，所以写的与判的是同一份缓存。
func quoteOpenStage(t *testing.T) {
	t.Helper()
	ltc := service.GlobalLTCConfig()
	if _, err := ltc.Save(context.Background(), &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
	}, 0); err != nil {
		t.Fatalf("打开报价阶段失败: %v", err)
	}
	ltc.InvalidateCache()
}

func quoteSeedOpportunity(t *testing.T, database *gorm.DB) {
	t.Helper()
	row := &model.Opportunity{
		ID: quoteLiveOppID, Code: "OPP-QUOTE-LIVE", CustomerID: "cus_quote_live", OneID: "one_quote_live",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_live",
	}
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(context.Background(), row); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
}

// quoteSeedScriptAndTemplate 配好模板与"生效话术"那一版（AC① 的正路：正文只取自快照）。
//
// 可变列 script_library.content 故意写一句**不同**的话：读回的是快照那句，才证明
// "版本指针 + 快照"这条路没被"顺手用可变列"顶掉（那条退路是 T-P6-02 文件头拒绝的东西）。
func quoteSeedScriptAndTemplate(t *testing.T, database *gorm.DB) {
	t.Helper()
	const (
		editingContent   = "编辑区里的草稿，不该出现在任何一张报价单上"
		publishedContent = "您好，以下是本次的正式报价，含税含实施。"
	)
	sc := &model.ScriptLibrary{
		Category: "quote", Subcategory: "quote_cover", Title: "报价封面",
		Content: editingContent, Version: 1, Status: "active",
	}
	if err := database.Create(sc).Error; err != nil {
		t.Fatalf("写话术分片失败：%v", err)
	}
	ver := &model.ScriptVersion{
		ScriptID: sc.ID, Version: 1, Title: "报价封面",
		Content: publishedContent, Status: "published",
	}
	if err := database.Create(ver).Error; err != nil {
		t.Fatalf("写话术版本快照失败：%v", err)
	}

	kv := repository.NewSystemConfigKVRepositoryWithDB(database)
	ctx := context.Background()
	if _, err := kv.Upsert(ctx, service.QuoteScriptIDKVKey, strconv.FormatUint(uint64(sc.ID), 10)); err != nil {
		t.Fatalf("写话术指针失败：%v", err)
	}
	if _, err := kv.Upsert(ctx, service.QuoteTemplateKVPrefix+"std",
		`{"currency":"CNY","valid_days":14,"lines":[`+
			`{"product_id":"p_seat","title":"标准版席位","quantity":10,"unit_price":199.00,"discount_percent":10}]}`); err != nil {
		t.Fatalf("写报价模板失败：%v", err)
	}
}

func quoteChainIDOf(t *testing.T, database *gorm.DB, rowID string) string {
	t.Helper()
	var q model.Quote // 独立零值 struct：复用已填充的会把旧字段并进 WHERE
	if err := database.Where("id = ?", rowID).First(&q).Error; err != nil {
		t.Fatalf("读回报价链号失败：%v", err)
	}
	return q.QuoteID
}

func quoteEventRows(t *testing.T, database *gorm.DB, rowID string) int64 {
	t.Helper()
	var chain string
	if err := database.Model(&model.Quote{}).Where("id = ?", rowID).Pluck("quote_id", &chain).Error; err != nil {
		t.Fatalf("读报价链号失败：%v", err)
	}
	var n int64
	if err := database.Model(&model.SalesEvent{}).Where("quote_id = ?", chain).Count(&n).Error; err != nil {
		t.Fatalf("数发送事件失败：%v", err)
	}
	return n
}

// doQuoteLive 带令牌走一次真请求；tok 为空串时**不带** Authorization 头（复现匿名），
// 而不是带一个空令牌 —— 后者会被中间件按"格式不对"拒掉，那测的就不是"没有身份"了。
func doQuoteLive(t *testing.T, h http.Handler, method, path, tok, body string) (int, quoteEnvelopeShape, string) {
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

	var env quoteEnvelopeShape
	raw := w.Body.String()
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return w.Code, env, raw
	}
	return w.Code, env, raw
}

func doQuoteRequest(t *testing.T, h http.Handler, method, path, body string) (int, quoteEnvelopeShape, string) {
	t.Helper()
	return doQuoteLive(t, h, method, path, "", body)
}

type quoteEnvelopeShape struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func quoteDataFrom(t *testing.T, env quoteEnvelopeShape) map[string]any {
	t.Helper()
	if len(env.Data) == 0 {
		t.Fatalf("响应里没有 data 字段：%s", env.Message)
	}
	var out map[string]any
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("data 不是对象：%v —— %s", err, env.Data)
	}
	return out
}

// —— 反向：装配点缺席时，路由必须诚实报错而不是给出空结论 ——————————————

// TestQuoteRoutes_UnassembledAnswersFiveOhThree 不装配 ⇒ 三条读口/写口全 503，data 非空。
//
// 这一条就是本卡的关闸：路由已进 Setup、而装配点还没跑（或旗子没开）的那个窗口里，
// 出口必须说"我起不来"。回 200 + 空对象会让前端长成"这个商机还没报过价"，
// 而那是一句业务结论 —— 此刻的事实是"一次都没查"。
func TestQuoteRoutes_UnassembledAnswersFiveOhThree(t *testing.T) {
	engine := newQuoteTestEngine(nil, nil, uint(7))
	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/quote/q_any", ""},
		{http.MethodGet, "/api/quote/latest/QT-any", ""},
		{http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{http.MethodPost, "/api/quote/q_any/send", `{}`},
	} {
		code, env, _ := doQuoteRequest(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s %s 在未装配时回 %d，期望 503：%s", probe.method, probe.path, code, env.Message)
		}
		if raw := string(env.Data); raw == "" || raw == "null" || raw == "{}" || raw == "[]" {
			t.Errorf("%s %s 的 503 把 data 留成了 %q：空对象会被读成「查过了，没有」", probe.method, probe.path, raw)
		} else if reason := quoteReasonFrom(t, env); reason != "unavailable" {
			t.Errorf("%s %s 的 reason=%q，期望 unavailable", probe.method, probe.path, reason)
		}
	}
}

func quoteReasonFrom(t *testing.T, env quoteEnvelopeShape) string {
	t.Helper()
	data := quoteDataFrom(t, env)
	reason, ok := data["reason"].(string)
	if !ok {
		t.Fatalf("错误响应缺机器可读的 data.reason：%s", env.Message)
	}
	return reason
}

// TestQuoteRoutes_FailingServiceIsNotMistakenForEmptyAnswers 底座抖动必须报 500，
// 不许报 404/空清单（与商机那一路同一族判据：一查就是错，绝不会是"没有"）。
func TestQuoteRoutes_FailingServiceIsNotMistakenForEmptyAnswers(t *testing.T) {
	boom := errors.New("connection reset by peer")
	engine := newQuoteTestEngine(failingQuoteReader{err: boom}, failingQuoteSender{err: boom}, uint(7))

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/quote/q_real", ""},
		{http.MethodGet, "/api/quote/latest/QT-real", ""},
		{http.MethodPost, "/api/quote", `{"opportunity_id":"opp_1","template_code":"std"}`},
		{http.MethodPost, "/api/quote/q_real/send", `{}`},
	} {
		code, env, _ := doQuoteRequest(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusInternalServerError {
			t.Errorf("%s %s 回 %d，期望 500：%s", probe.method, probe.path, code, env.Message)
		}
		if reason := quoteReasonFrom(t, env); reason != "internal" {
			t.Errorf("%s %s 的 reason=%q，期望 internal", probe.method, probe.path, reason)
		}
		if strings.Contains(env.Message, boom.Error()) {
			t.Errorf("底层错误串被透出：%q", env.Message)
		}
	}
}

// failingQuoteReader / failingQuoteSender 任何一问就是故障。
// 它们的存在是为了把"503 与 500 分得开"这件事断成真话：
// 只测未装配那一条，摘掉错误分支也看不出差别（全都回 503）。
type failingQuoteReader struct{ err error }

func (f failingQuoteReader) Available() bool { return true }

func (f failingQuoteReader) View(context.Context, string) (*service.QuoteView, error) {
	return nil, f.err
}

func (f failingQuoteReader) LatestView(context.Context, string) (*service.QuoteView, error) {
	return nil, f.err
}

func (f failingQuoteReader) Generate(context.Context, service.QuoteGenerateInput) (*service.QuoteView, error) {
	return nil, f.err
}

type failingQuoteSender struct{ err error }

func (f failingQuoteSender) Available() bool { return true }

func (f failingQuoteSender) Send(context.Context, service.QuoteSendInput) (*service.QuoteSendResult, error) {
	return nil, f.err
}

func (f failingQuoteSender) OpenApproval(context.Context, string) (*model.ApprovalRequest, error) {
	return nil, f.err
}

var (
	_ controller.QuoteViewReader = failingQuoteReader{}
	_ controller.QuoteSender     = failingQuoteSender{}
)
