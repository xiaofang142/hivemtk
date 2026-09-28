// bad_case_routes_test.go T-P8-03：Bad Case API 的入参口径、身份口径与错误状态码。
//
// 全部走**真库 + 真 handler**（只把 JWT 中间件换成"塞一个 user_id"的那一行）：
// 这一层要钉的就是 HTTP 形状 —— 查询参数怎么翻成过滤条件、没登录怎么答、
// 底座不可用时回 503 还是回空列表、八个端点里静态路径有没有被 /:id 吞掉。
// 这些都不在 service 用例的覆盖面里，而把它们写成桩对象上的断言只会测到"我写的桩会返回错误"。
package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// newBadCaseTestEngine 挂一条"已登录"的中间件。uid 传 nil 表示不带身份
// （复现未登录 / claims 里没 user_id 那一支）。
func newBadCaseTestEngine(svc *service.BadCaseService, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewBadCaseController(svc).RegisterRoutes(auth)
	return engine
}

func setupBadCaseRoutesDB(t *testing.T) (*service.BadCaseService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.BadCase{})
	return service.NewBadCaseService(repository.NewBadCaseRepositoryWithDB(database)), database
}

type badCaseRawResp struct {
	Status  int
	Code    any
	Message string
	Body    string
	Data    map[string]any
	List    []any
	Total   any
}

func doBadCase(t *testing.T, h http.Handler, method, path, body string) badCaseRawResp {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	out := badCaseRawResp{Status: w.Code, Body: w.Body.String()}
	var parsed struct {
		Code    any            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("%s %s 响应不是合法 JSON：%v —— %s", method, path, err, w.Body.String())
	}
	out.Code = parsed.Code
	out.Message = parsed.Message
	out.Data = parsed.Data
	if v, ok := parsed.Data["list"]; ok {
		if arr, ok := v.([]any); ok {
			out.List = arr
		}
	}
	out.Total = parsed.Data["total"]
	return out
}

// seedBadCaseRow 经仓储写一行（不走被测的 HTTP 写口）：夹具与出口分家，
// 否则"POST 写坏了"会被"POST 读回来了"自我证实。
func seedBadCaseRow(t *testing.T, db *gorm.DB, id, source, status, label string) {
	t.Helper()
	dedup := id
	if source != model.BadCaseSourceManual {
		k, ok := model.BadCaseDedupKey(source, "sess_"+id, "msg_"+id)
		if !ok {
			t.Fatalf("夹具 dedup 键算不出：%s", id)
		}
		dedup = k
	}
	row := &model.BadCase{
		ID: id, Source: source, Status: status,
		SessionID: "sess_" + id, MessageID: "msg_" + id, SignalID: "sig_" + id,
		IntentType: "price_inquiry", QueryText: "问句 " + id, AnswerText: "答句 " + id,
		Confidence: 0.4, Threshold: 0.7, MarkReason: "夹具", DedupKey: dedup,
	}
	if label != "" {
		row.Label = label
		row.LabelNote = "判定依据"
		row.LabelerID = "42"
		row.FixLayer = model.FixLayerOfLabel(label)
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("夹具造行 %s 失败：%v", id, err)
	}
}

// 八条端点必须都能挂上：静态段（stats / taxonomy）与 /:id 在同一层，
// gin 的路由树对这种共存有历史包袱 —— 真挂一次才知道有没有炸。
func TestBadCaseRoutes_AllEightRegistered(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_rt_1", model.BadCaseSourceManual, model.BadCaseStatusPending, "")
	h := newBadCaseTestEngine(svc, uint(42))

	for _, path := range []string{"/api/bad-cases", "/api/bad-cases/stats", "/api/bad-cases/taxonomy", "/api/bad-cases/bc_rt_1"} {
		if got := doBadCase(t, h, http.MethodGet, path, ""); got.Status != http.StatusOK {
			t.Errorf("GET %s 应 200，实际 %d：%s", path, got.Status, got.Body)
		}
	}
	// 四条写口：路径必须存在（用空体撞 400/401，而不是 404 —— 404 说明根本没挂上）
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/bad-cases", `{"session_id":"s","query_text":"问","answer_text":"答"}`},
		{http.MethodPost, "/api/bad-cases/bc_rt_1/label", `{"label":"kb_missing","note":"依据"}`},
		{http.MethodPost, "/api/bad-cases/bc_rt_1/dismiss", `{"reason":"不是坏例"}`},
		{http.MethodPost, "/api/bad-cases/export", `{}`},
	} {
		got := doBadCase(t, h, c.method, c.path, c.body)
		if got.Status == http.StatusNotFound {
			t.Errorf("%s %s 撞 404 ⇒ 路由没挂上：%s", c.method, c.path, got.Body)
		}
	}
}

// 底座没装配（全局为 nil）时八个端点全部 503，且响应里**不许带数据字段**：
// 空列表与 {"by_status":{…:0}} 都是一句业务结论（"队列清完了"），
// 而此刻的事实是"一次都没读到"，两种读法在值班手上的动作完全相反。
func TestBadCaseRoutes_UnavailableIs503WithDataless(t *testing.T) {
	h := newBadCaseTestEngine(nil, uint(42))
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/bad-cases", ""},
		{http.MethodGet, "/api/bad-cases/stats", ""},
		{http.MethodPost, "/api/bad-cases", `{"session_id":"s","query_text":"问","answer_text":"答"}`},
		{http.MethodPost, "/api/bad-cases/bc_x/label", `{"label":"kb_missing","note":"依据"}`},
		{http.MethodPost, "/api/bad-cases/bc_x/dismiss", `{"reason":"理由"}`},
		{http.MethodPost, "/api/bad-cases/export", ""},
	} {
		got := doBadCase(t, h, c.method, c.path, c.body)
		if got.Status != http.StatusServiceUnavailable {
			t.Errorf("%s %s 应 503，实际 %d：%s", c.method, c.path, got.Status, got.Body)
		}
		for _, frag := range []string{"\"list\"", "\"total\"", "\"by_status\"", "\"count\"", "\"rows\"", "\"eval_set_id\""} {
			if strings.Contains(got.Body, frag) {
				t.Errorf("503 响应里带了数据字段 %s（会把\"读不动\"渲染成业务结论）：%s", frag, got.Body)
			}
		}
	}
	// taxonomy 是纯常量表、不碰库，所以它**不该** 503：
	// 让它跟着底座一起挂，前端会在底座恢复前拿不到类目，而那份表本来就是静态的。
	if got := doBadCase(t, h, http.MethodGet, "/api/bad-cases/taxonomy", ""); got.Status != http.StatusOK {
		t.Errorf("taxonomy 不该依赖底座，实际 %d：%s", got.Status, got.Body)
	}
}

// setupBadCaseRoutes 走的是全局登记处：装配过才有底座，撤掉后回 503。
// 这条钉的是"路由取的是那一份全局"，而不是自己 new 了一份（两处各一份就会漂移）。
func TestBadCaseRoutes_SetupUsesGlobalRegistry(t *testing.T) {
	// "未装配"这个前置必须由本用例自己钉，不能指望包内当时的状态：整包跑时有别的用例走
	// 真 router.Setup，那一句把全局设成了"服务在、库已随那个用例关掉"的形状 ⇒ 不打这行
	// 就读到 500（sql: database is closed）而不是 503，判据于是变成在测别人的收尾顺序。
	// 与 TestSetupHumanTaskRoutes_NilGlobalStillServes503 同一写法：进场清、退场也清。
	t.Cleanup(func() { service.SetGlobalBadCaseService(nil) })
	service.SetGlobalBadCaseService(nil)

	// 未装配：setupBadCaseRoutes 不该 panic，且挂出来的端点回 503
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() })
	setupBadCaseRoutes(auth)
	if got := doBadCase(t, engine, http.MethodGet, "/api/bad-cases", ""); got.Status != http.StatusServiceUnavailable {
		t.Errorf("未装配时列表应 503，实际 %d：%s", got.Status, got.Body)
	}

	// 装配后（同一条已挂好的路由）：立刻能读到 200 —— 证明控制器每次取的是服务实例，
	// 而 setup 之前那次 nil 没有把路由做成一次性快照。
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_gl_1", model.BadCaseSourceManual, model.BadCaseStatusPending, "")
	app.InitBadCaseRuntime(db)
	h2 := newBadCaseTestEngine(service.GlobalBadCaseService(), uint(7))
	if got := doBadCase(t, h2, http.MethodGet, "/api/bad-cases", ""); got.Status != http.StatusOK {
		t.Errorf("装配后列表应 200，实际 %d：%s", got.Status, got.Body)
	}
	// 撤装配（db=nil）必须把全局清空，否则路由会对着一句"未装配"的告警继续回 200
	if got := app.InitBadCaseRuntime(nil); got != nil {
		t.Errorf("InitBadCaseRuntime(nil) 应回 nil，实际 %v", got)
	}
	if svc != nil && svc.Available() != true {
		t.Error("夹具服务本身应可用（本用例前半段的前提）")
	}
	h3 := newBadCaseTestEngine(service.GlobalBadCaseService(), uint(7))
	if got := doBadCase(t, h3, http.MethodGet, "/api/bad-cases", ""); got.Status != http.StatusServiceUnavailable {
		t.Errorf("撤掉全局后应回 503，实际 %d：%s", got.Status, got.Body)
	}
}

func TestBadCaseRoutes_IdentityIsRequiredOnWriteEntries(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_id_1", model.BadCaseSourceManual, model.BadCaseStatusPending, "")

	anon := newBadCaseTestEngine(svc, nil)
	logged := newBadCaseTestEngine(svc, uint(42))

	for _, c := range []struct{ path, body string }{
		{"/api/bad-cases", `{"session_id":"s","query_text":"问","answer_text":"答"}`},
		{"/api/bad-cases/bc_id_1/label", `{"label":"kb_missing","note":"依据"}`},
		{"/api/bad-cases/bc_id_1/dismiss", `{"reason":"不是坏例"}`},
	} {
		got := doBadCase(t, anon, http.MethodPost, c.path, c.body)
		if got.Status != http.StatusUnauthorized {
			t.Errorf("匿名 POST %s 应 401，实际 %d：%s", c.path, got.Status, got.Body)
		}
		// 401 之前不许已经写进去：先鉴权后落库，否则未登录也能往队列里灌样本
		var rows []model.BadCase
		if err := db.Where("labeler_id = ?", "").Find(&rows).Error; err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) > 2 {
			t.Errorf("被拒的匿名请求仍写了行：%d", len(rows))
		}
		if ok := doBadCase(t, logged, http.MethodPost, c.path, c.body); ok.Status == http.StatusUnauthorized {
			t.Errorf("带身份 POST %s 仍被挡：%s", c.path, ok.Body)
		}
	}

	// 读侧不要求身份（登录态只用来追责"这条结论谁下的"，队列本身是同租户可见的）
	if got := doBadCase(t, anon, http.MethodGet, "/api/bad-cases", ""); got.Status != http.StatusOK {
		t.Errorf("匿名 GET 列表应 200，实际 %d：%s", got.Status, got.Body)
	}
	// user_id 类型是 JWT 那一路写进去的 uint；0 与空串都算"没身份"
	for _, uid := range []any{uint(0), 0, -3, "", "   "} {
		h := newBadCaseTestEngine(svc, uid)
		if got := doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_id_1/dismiss", `{"reason":"r"}`); got.Status != http.StatusUnauthorized {
			t.Errorf("user_id=%#v 应判无身份 → 401，实际 %d：%s", uid, got.Status, got.Body)
		}
	}
	// float64 是 claims 从 JSON 解出来的那一档（permission.go 同一路）
	if got := doBadCase(t, newBadCaseTestEngine(svc, float64(9)), http.MethodGet, "/api/bad-cases", ""); got.Status != http.StatusOK {
		t.Errorf("float64 身份应可用：%d %s", got.Status, got.Body)
	}
	// 落库的 labeler_id 必须是身份字符串而不是 "0"（打标的人是评测集里那一列的唯一出处）
	if got := doBadCase(t, newBadCaseTestEngine(svc, uint(42)), http.MethodPost,
		"/api/bad-cases", `{"session_id":"s_id","query_text":"问","answer_text":"答"}`); got.Status != http.StatusOK {
		t.Fatalf("补录失败：%d %s", got.Status, got.Body)
	}
	var stored model.BadCase
	if err := db.Where("session_id = ?", "s_id").First(&stored).Error; err != nil {
		t.Fatalf("回读补录行失败：%v", err)
	}
	if stored.LabelerID != "" {
		t.Errorf("补录不该把操作者写进 labeler_id（那是打标列）：%q", stored.LabelerID)
	}
	if stored.Source != model.BadCaseSourceManual {
		t.Errorf("POST 补录的来源应为 manual，实际 %q", stored.Source)
	}
}

// 查询参数的口径：空值丢掉（?label= 是"没选筛选器"的前端常态），
// 未知值必须一路红到仓储（那是业务结论，不能渲染成"这类一条都没有"）。
func TestBadCaseRoutes_QueryParams(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_q_p", model.BadCaseSourceLowConfidence, model.BadCaseStatusPending, "")
	seedBadCaseRow(t, db, "bc_q_l", model.BadCaseSourceManual, model.BadCaseStatusLabeled, model.BadCaseLabelKBMissing)
	h := newBadCaseTestEngine(svc, uint(42))

	// 默认视图：只列 pending（四态里的"等人判"那一档）
	got := doBadCase(t, h, http.MethodGet, "/api/bad-cases", "")
	if got.Status != http.StatusOK {
		t.Fatalf("默认列表失败：%s", got.Body)
	}
	if got.Total != float64(1) {
		t.Errorf("默认视图 total 应为 1（只有那条 pending），实际 %v", got.Total)
	}
	if len(got.List) != 1 {
		t.Errorf("默认视图应 1 行，实际 %d", len(got.List))
	}

	// 空值被丢掉而不是透传成 400
	for _, q := range []string{"?source=", "?status=", "?label=", "?fix_layer=", "?source=&status=&label="} {
		r := doBadCase(t, h, http.MethodGet, "/api/bad-cases"+q, "")
		if r.Status != http.StatusOK {
			t.Errorf("空筛选器 %q 应回默认视图 200，实际 %d：%s", q, r.Status, r.Body)
		}
	}

	// 可重复给：?status=a&status=b 是"合并视图"
	r := doBadCase(t, h, http.MethodGet, "/api/bad-cases?status=pending&status=labeled", "")
	if r.Status != http.StatusOK || r.Total != float64(2) {
		t.Errorf("两状态合并应 2 条：(%d,%v) %s", r.Status, r.Total, r.Body)
	}

	// 未知值 → 400（不是 200 空列表）
	for _, q := range []string{
		"?status=bogus", "?source=bogus", "?label=bogus", "?fix_layer=bogus",
		"?status=pending&label=bogus", // 混合：一个合法一个未知也必须红
	} {
		r := doBadCase(t, h, http.MethodGet, "/api/bad-cases"+q, "")
		if r.Status != http.StatusBadRequest {
			t.Errorf("未知筛选值 %q 应 400，实际 %d：%s", q, r.Status, r.Body)
		}
		if strings.Contains(r.Body, "\"list\"") {
			t.Errorf("400 响应仍带了 list（会被前端渲染成\"这类没有\"）：%s", r.Body)
		}
	}

	// 分页：非正/非数字都 400，不做"那就当第一页"的静默纠正
	for _, q := range []string{"?page=0", "?page=-1", "?page=abc", "?page_size=0", "?page_size=-2", "?page_size=xyz"} {
		r := doBadCase(t, h, http.MethodGet, "/api/bad-cases"+q, "")
		if r.Status != http.StatusBadRequest {
			t.Errorf("非法分页 %q 应 400，实际 %d：%s", q, r.Status, r.Body)
		}
	}
	// 正数页：page=9 越界是"没有这一页"，回 200 + 空 list + 正确 total
	if r := doBadCase(t, h, http.MethodGet, "/api/bad-cases?page=9&page_size=20", ""); r.Status != http.StatusOK || r.Total != float64(1) {
		t.Errorf("越界页应 200 且 total 仍是过滤后总数：(%d,%v)", r.Status, r.Total)
	}
	// 超大页由仓储夹住（HTTP 侧不报错：那是上限不是非法值）
	if r := doBadCase(t, h, http.MethodGet, "/api/bad-cases?page_size=999999", ""); r.Status != http.StatusOK {
		t.Errorf("超大 page_size 应被夹住而不是报错：%d %s", r.Status, r.Body)
	}

	// 责任层过滤（层是 label 的派生列，前端只会传层）
	if r := doBadCase(t, h, http.MethodGet, "/api/bad-cases?status=labeled&fix_layer=knowledge", ""); r.Total != float64(1) {
		t.Errorf("knowledge 层应 1 条，实际 %v：%s", r.Total, r.Body)
	}
	if r := doBadCase(t, h, http.MethodGet, "/api/bad-cases?status=labeled&label=kb_missing", ""); r.Total != float64(1) {
		t.Errorf("kb_missing 应 1 条，实际 %v", r.Total)
	}
}

func TestBadCaseRoutes_GetByID(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_g_1", model.BadCaseSourceZeroHit, model.BadCaseStatusPending, "")
	h := newBadCaseTestEngine(svc, uint(42))

	r := doBadCase(t, h, http.MethodGet, "/api/bad-cases/bc_g_1", "")
	if r.Status != http.StatusOK {
		t.Fatalf("按 id 读失败：%d %s", r.Status, r.Body)
	}
	if r.Data["id"] != "bc_g_1" || r.Data["source"] != model.BadCaseSourceZeroHit {
		t.Errorf("读回来的行不对：%+v", r.Data)
	}
	// 现场两列必须回显（打标的人就在这一屏判）
	if r.Data["query_text"] != "问句 bc_g_1" || r.Data["answer_text"] != "答句 bc_g_1" {
		t.Errorf("现场抄本没回显：%+v", r.Data)
	}
	// 不存在的 id → 404（服务层回 (nil,nil)，这个判断归本层）
	if r := doBadCase(t, h, http.MethodGet, "/api/bad-cases/bc_absent", ""); r.Status != http.StatusNotFound {
		t.Errorf("不存在的 id 应 404，实际 %d：%s", r.Status, r.Body)
	}
	// 过长 id → 400 而不是把整串回显进响应（否则就是免费的响应体放大器）
	long := "/api/bad-cases/" + strings.Repeat("x", 200)
	r = doBadCase(t, h, http.MethodGet, long, "")
	if r.Status != http.StatusBadRequest {
		t.Errorf("过长 id 应 400，实际 %d", r.Status)
	}
	if strings.Contains(r.Body, strings.Repeat("x", 50)) {
		t.Errorf("响应体回显了超长 id：%s", r.Body)
	}
}

// 打标出口的三档错误：入参非法 400、没有这条 404、状态不对 409。
// 三者必须分开，因为前端该做的动作完全不同。
func TestBadCaseRoutes_LabelAndDismissCodes(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_st_1", model.BadCaseSourceManual, model.BadCaseStatusPending, "")
	seedBadCaseRow(t, db, "bc_st_2", model.BadCaseSourceManual, model.BadCaseStatusExported, model.BadCaseLabelKBMissing)
	h := newBadCaseTestEngine(svc, uint(42))

	// 正常打标
	r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_1/label", `{"label":"retrieve_miss","note":"召回没带上"}`)
	if r.Status != http.StatusOK {
		t.Fatalf("打标失败：%d %s", r.Status, r.Body)
	}
	if r.Data["status"] != model.BadCaseStatusLabeled || r.Data["fix_layer"] != model.BadCaseFixLayerRetrieval {
		t.Errorf("打标响应不对：status=%v fix_layer=%v", r.Data["status"], r.Data["fix_layer"])
	}
	if r.Data["labeler_id"] != "42" {
		t.Errorf("labeler_id 没落身份：%v", r.Data["labeler_id"])
	}

	// 重复打标 → 409，且提示里带当前状态
	r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_1/label", `{"label":"kb_stale","note":"再判"}`)
	if r.Status != http.StatusConflict {
		t.Errorf("重复打标应 409，实际 %d：%s", r.Status, r.Body)
	}
	if !strings.Contains(r.Message, model.BadCaseStatusLabeled) {
		t.Errorf("409 提示里要带当前状态：%q", r.Message)
	}

	for _, c := range []struct {
		name string
		body string
		want int
	}{
		{"未知类目", `{"label":"bogus","note":"依据"}`, http.StatusBadRequest},
		{"缺 note", `{"label":"kb_missing"}`, http.StatusBadRequest},
		{"note 空白", `{"label":"kb_missing","note":"   "}`, http.StatusBadRequest},
		{"缺 label", `{"note":"依据"}`, http.StatusBadRequest},
		{"不是 JSON", `{"label":`, http.StatusBadRequest},
		{"空体", ``, http.StatusBadRequest},
	} {
		r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_2/label", c.body)
		if r.Status != c.want {
			t.Errorf("%s：应 %d，实际 %d：%s", c.name, c.want, r.Status, r.Body)
		}
	}
	// 不存在 → 404（不是含糊的 409）
	if r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_none/label", `{"label":"kb_missing","note":"依据"}`); r.Status != http.StatusNotFound {
		t.Errorf("不存在的行应 404，实际 %d：%s", r.Status, r.Body)
	}

	// 撤销：已导出的行不许撤（409），理由必填（400）
	if r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_2/dismiss", `{"reason":"想收回"}`); r.Status != http.StatusConflict {
		t.Errorf("已导出的行撤销应 409，实际 %d：%s", r.Status, r.Body)
	}
	if r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_1/dismiss", `{"reason":"  "}`); r.Status != http.StatusBadRequest {
		t.Errorf("空理由撤销应 400，实际 %d：%s", r.Status, r.Body)
	}
	if r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/bc_st_1/dismiss", `{"reason":"误报"}`); r.Status != http.StatusOK {
		t.Errorf("撤销应 200，实际 %d：%s", r.Status, r.Body)
	} else if r.Data["status"] != model.BadCaseStatusDismissed {
		t.Errorf("撤销后状态不对：%v", r.Data["status"])
	}
	// 撤销把库里那行改掉（不是只回一句成功）
	var stored model.BadCase
	if err := db.Where("id = ?", "bc_st_1").First(&stored).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if stored.Status != model.BadCaseStatusDismissed || stored.CancelReason != "误报" {
		t.Errorf("库里没落：%s/%q", stored.Status, stored.CancelReason)
	}
}

// 导出出口的三档：没有可导的 409、参数非法 400、成功 200 且带上盖了集 id 的行。
func TestBadCaseRoutes_Export(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	h := newBadCaseTestEngine(svc, uint(42))

	// 队列空 → 409（"没人跟你抢，也没有货"与"读不动"都得分开）
	if r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", ""); r.Status != http.StatusConflict {
		t.Errorf("空队列导出应 409，实际 %d：%s", r.Status, r.Body)
	}
	// 无 body 也能导（"全选导出"是常态）：这里仍是 409 而不是 400
	if r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", ""); r.Status == http.StatusBadRequest {
		t.Errorf("无请求体不该被当成非法：%s", r.Body)
	}
	if r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", `{"labels":["bogus"]}`); r.Status != http.StatusBadRequest {
		t.Errorf("未知类目应 400，实际 %d：%s", r.Status, r.Body)
	}
	if r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", `{"limit":-1}`); r.Status != http.StatusBadRequest {
		t.Errorf("负 limit 应 400，实际 %d：%s", r.Status, r.Body)
	}
	if r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", `{"limit":`); r.Status != http.StatusBadRequest {
		t.Errorf("坏 JSON 应 400，实际 %d：%s", r.Status, r.Body)
	}

	seedBadCaseRow(t, db, "bc_ex_1", model.BadCaseSourceManual, model.BadCaseStatusLabeled, model.BadCaseLabelKBMissing)
	seedBadCaseRow(t, db, "bc_ex_2", model.BadCaseSourceManual, model.BadCaseStatusPending, "")
	r := doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", "")
	if r.Status != http.StatusOK {
		t.Fatalf("导出失败：%d %s", r.Status, r.Body)
	}
	if r.Data["eval_set_id"] == nil || r.Data["eval_set_id"] == "" {
		t.Errorf("导出响应没带集 id：%s", r.Body)
	}
	if cnt, ok := r.Data["count"].(float64); !ok || cnt != 1 {
		t.Errorf("应只导出那条已打标的，count=%v：%s", r.Data["count"], r.Body)
	}
	rows, ok := r.Data["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows 形状不对：%s", r.Body)
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("rows[0] 不是对象：%T", rows[0])
	}
	for _, k := range []string{"case_id", "query_text", "answer_text", "label", "fix_layer", "label_note", "labeler_id", "labeled_at"} {
		if _, present := first[k]; !present {
			t.Errorf("导出行缺 %q：%+v", k, first)
		}
	}
	// 本卡给不出"标准答案"，所以导出产物里必须一个 expected 键都没有
	if strings.Contains(strings.ToLower(r.Body), "expected") {
		t.Errorf("导出响应里出现了 expected（下游会当成已有标准答案）：%s", r.Body)
	}
	// 库里那一行被盖上集 id
	var stored model.BadCase
	if err := db.Where("id = ?", "bc_ex_1").First(&stored).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if stored.Status != model.BadCaseStatusExported || stored.EvalSetID == "" {
		t.Errorf("库里没盖导出态：%s/%q", stored.Status, stored.EvalSetID)
	}
	// 同一批再导 → 409（它已经进了一份评测集，不该进第二份）
	if r = doBadCase(t, h, http.MethodPost, "/api/bad-cases/export", ""); r.Status != http.StatusConflict {
		t.Errorf("重复导出应 409，实际 %d：%s", r.Status, r.Body)
	}
}

// taxonomy 是前端唯一那份表：库里加了类目而端点没带出来，
// 就会出现"库里判得出来、页面上选不到"，而这条链的下游是评测集。
func TestBadCaseRoutes_TaxonomyMatchesModelTables(t *testing.T) {
	// 没底座也要能读（它是静态表，不该跟着底座一起 503）
	for _, svc := range []*service.BadCaseService{nil, mustAvailableBadCaseSvc(t)} {
		h := newBadCaseTestEngine(svc, uint(42))
		r := doBadCase(t, h, http.MethodGet, "/api/bad-cases/taxonomy", "")
		if r.Status != http.StatusOK {
			t.Fatalf("taxonomy 失败：%d %s", r.Status, r.Body)
		}
		for key, want := range map[string][]string{
			"sources":    model.BadCaseSources,
			"statuses":   model.BadCaseStatuses,
			"labels":     model.BadCaseLabels,
			"fix_layers": model.BadCaseFixLayers,
		} {
			got, ok := r.Data[key].([]any)
			if !ok {
				t.Fatalf("svc=%v 时 taxonomy 缺 %s：%s", svc != nil, key, r.Body)
			}
			if len(got) != len(want) {
				t.Errorf("svc=%v 时 %s 应 %d 项，实际 %d（表在漂移）", svc != nil, key, len(want), len(got))
			}
			for i, v := range want {
				if i < len(got) && got[i] != v {
					t.Errorf("%s[%d] 应为 %q，实际 %v", key, i, v, got[i])
				}
			}
		}
		// 类目→层映射也要一起给：前端按层分组显示七个类目，少一份映射就得在前端抄一遍
		mapping, ok := r.Data["label_layer"].(map[string]any)
		if !ok || len(mapping) != len(model.BadCaseLabelFixLayer) {
			t.Fatalf("label_layer 映射不对：%v", r.Data["label_layer"])
		}
		for label, layer := range model.BadCaseLabelFixLayer {
			if fmt.Sprint(mapping[label]) != layer {
				t.Errorf("%s 应归 %s 层，实际 %v", label, layer, mapping[label])
			}
		}
	}
}

func mustAvailableBadCaseSvc(t *testing.T) *service.BadCaseService {
	t.Helper()
	svc, _ := setupBadCaseRoutesDB(t)
	return svc
}

// Stats 的读数是队列看板：四态键恒在 + 已判率。
func TestBadCaseRoutes_Stats(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	seedBadCaseRow(t, db, "bc_sm_1", model.BadCaseSourceManual, model.BadCaseStatusPending, "")
	seedBadCaseRow(t, db, "bc_sm_2", model.BadCaseSourceManual, model.BadCaseStatusLabeled, model.BadCaseLabelKBMissing)
	h := newBadCaseTestEngine(svc, uint(42))

	r := doBadCase(t, h, http.MethodGet, "/api/bad-cases/stats", "")
	if r.Status != http.StatusOK {
		t.Fatalf("Stats 失败：%d %s", r.Status, r.Body)
	}
	byStatus, ok := r.Data["by_status"].(map[string]any)
	if !ok {
		t.Fatalf("by_status 形状不对：%s", r.Body)
	}
	for _, s := range model.BadCaseStatuses {
		if _, present := byStatus[s]; !present {
			t.Errorf("by_status 缺 %q 键（前端会渲染成 undefined 而不是 0）：%+v", s, byStatus)
		}
	}
	if fmt.Sprint(byStatus[model.BadCaseStatusPending]) != "1" {
		t.Errorf("pending 应为 1，实际 %v", byStatus[model.BadCaseStatusPending])
	}
	if _, present := r.Data["labeled_ratio"]; !present {
		t.Error("看板缺已判率（本卡北极星）")
	}
	if fmt.Sprint(r.Data["labeled_ratio"]) != "0.5" {
		t.Errorf("两行里判过一行，已判率应 0.5，实际 %v", r.Data["labeled_ratio"])
	}
	// 责任层分布也必须在（"该找谁修"是这块看板的存在理由）
	if layers, ok := r.Data["by_fix_layer"].(map[string]any); !ok || len(layers) != len(model.BadCaseFixLayers) {
		t.Errorf("by_fix_layer 不对：%v", r.Data["by_fix_layer"])
	}
}

// 补录出口：缺现场要 400（一条指不回会话的坏例在评测集里无法复核）。
func TestBadCaseRoutes_CreateManual(t *testing.T) {
	svc, db := setupBadCaseRoutesDB(t)
	h := newBadCaseTestEngine(svc, uint(42))

	for _, c := range []struct {
		name string
		body string
		want int
	}{
		{"缺问句", `{"session_id":"s","answer_text":"答"}`, http.StatusBadRequest},
		{"缺答句", `{"session_id":"s","query_text":"问"}`, http.StatusBadRequest},
		{"空白答句", `{"session_id":"s","query_text":"问","answer_text":"  "}`, http.StatusBadRequest},
		{"缺会话", `{"query_text":"问","answer_text":"答"}`, http.StatusBadRequest},
		{"坏 JSON", `{"session_id":`, http.StatusBadRequest},
		{"空体", ``, http.StatusBadRequest},
		{"给全了", `{"session_id":"s_ok","query_text":"问","answer_text":"答"}`, http.StatusOK},
	} {
		r := doBadCase(t, h, http.MethodPost, "/api/bad-cases", c.body)
		if r.Status != c.want {
			t.Errorf("%s：应 %d，实际 %d：%s", c.name, c.want, r.Status, r.Body)
		}
	}
	// 带 reason 时原样落库（那是坐席写的话，不是系统话术）
	r := doBadCase(t, h, http.MethodPost, "/api/bad-cases",
		`{"session_id":"s_reason","query_text":"问","answer_text":"答","reason":"客户当场指出价格说错"}`)
	if r.Status != http.StatusOK {
		t.Fatalf("带理由补录失败：%s", r.Body)
	}
	if fmt.Sprint(r.Data["mark_reason"]) != "客户当场指出价格说错" {
		t.Errorf("补录理由被覆盖：%v", r.Data["mark_reason"])
	}
	var stored model.BadCase
	if err := db.Where("session_id = ?", "s_reason").First(&stored).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if stored.Status != model.BadCaseStatusPending {
		t.Errorf("补录应为待判态：%q", stored.Status)
	}
	if stored.DedupKey != stored.ID {
		t.Errorf("手动来源的 dedup 键应等于本行 id：%q/%q", stored.DedupKey, stored.ID)
	}
}
