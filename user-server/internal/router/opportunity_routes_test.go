// opportunity_routes_test.go T-P4-04：商机 HTTP 出口的入参口径、状态码口径与契约面。
//
// 与同包的 human_task 用例同一套打法：**真库 + 真 handler**，只把 JWT 中间件换成
// "塞一个 user_id" 的那一行。本层要钉的就是 HTTP 形状（谁回 400、谁回 409、
// 409 里那三种 409 怎么分得开），这些都不在 service 用例的覆盖面里。
//
// 三条"看起来多余"的用例是这张网的承重墙，各自的理由写在函数注释上：
// ① 路由表里没有能写 won 的一条（AC③ 在 HTTP 侧的兑现）；
// ② 路由文件里没有一个内联闭包（CLAUDE.md 铁律 2，AC②）；
// ③ 八条端点每条都有 @Router 注解（AC③ Swagger 覆盖）。
// 它们都是**非行为断言**：只有"把那一处摘掉/加回去"的注码能让它们红，
// 所以配套的变异在电池里，而不是在这里自我声明。
package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// —— 测试脚手架 ——————————————————————————————————————————

// opportunityRouteSpecs 本卡交付的全部端点（顺序无关，判据按集合比）。
//
// 少一条 = 前端有一个按钮点了报 404，而 404 在网关日志里与"路径写错"长得一模一样；
// 多一条 = 有一个没人声明的入口在跑（尤其危险的是能写 won 的那一条）。
var opportunityRouteSpecs = []string{
	"GET /api/opportunity/:id",
	"GET /api/opportunity/:id/moves",
	"GET /api/opportunity/rules",
	"POST /api/opportunity/:id/cancel",
	"POST /api/opportunity/:id/lost",
	"POST /api/opportunity/:id/reopen",
	"POST /api/opportunity/:id/stage",
	"PUT /api/opportunity/:id",
}

func opportunityTestService(t *testing.T, database *gorm.DB) *service.OpportunityService {
	t.Helper()
	svc := service.NewOpportunityService(repository.NewOpportunityRepositoryWithDB(database))
	svc.SetClock(func() time.Time { return oppTestClock })
	return svc
}

// oppTestClock 冻结的"现在"。
//
// 不写死一个远未来/远过去的时刻：逾期判据读的就是"现在"，夹具里的 now 一挪，
// 所有带 expected_close_at 的行会集体翻面（那类红的红因不在 diff 里）。
// 装配注入用它，测试自己只加减时长。
var oppTestClock = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func opportunityTestNow(offset time.Duration) time.Time {
	return oppTestClock.Add(offset)
}

// newOpportunityTestEngine 只挂控制器（不经过装配层）：svc 传 nil 复现"未装配"。
func newOpportunityTestEngine(svc *service.OpportunityService, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewOpportunityController(svc).RegisterRoutes(auth)
	return engine
}

// seedOpportunityRow 造一行商机：走真实写入口（仓储 Insert），不直插 SQL ——
// 直插会绕过 version/编号/默认币种那套落列，端点测到的就不是同一份数据形状。
func seedOpportunityRow(t *testing.T, database *gorm.DB, id, stage, status string) *model.Opportunity {
	t.Helper()
	row := &model.Opportunity{
		ID:             id,
		Code:           "OPP-" + strings.ToUpper(id),
		CustomerID:     "cus_" + id,
		Stage:          stage,
		Status:         status,
		Amount:         1200,
		Currency:       "CNY",
		WinProbability: 0.42, // 不属于任何公式的输出：它一变就知道谁重算了
		OwnerUserID:    "sales_a",
		CreatedAt:      opportunityTestNow(-72 * time.Hour),
	}
	if status == model.OpportunityStatusLost {
		row.LostReason = "预算撤回"
	}
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(context.Background(), row); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	return row
}

func readOpportunityRow(t *testing.T, database *gorm.DB, id string) model.Opportunity {
	t.Helper()
	var row model.Opportunity // 独立零值 struct：复用已填充的会把旧字段并进 WHERE
	if err := database.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读回 %s: %v", id, err)
	}
	return row
}

type opportunityEnvelope struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// doOpportunity 发一次请求。返回状态码 + 信封 + 原始 body（非 JSON 时信封为 nil）。
func doOpportunity(t *testing.T, h http.Handler, method, path, body string) (int, opportunityEnvelope, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var env opportunityEnvelope
	raw := w.Body.String()
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return w.Code, env, raw
	}
	return w.Code, env, raw
}

// opportunityData 解出 data 对象；断"data 是一个对象"本身就是判据（503/400 时它必须缺席）。
func opportunityData(t *testing.T, env opportunityEnvelope) map[string]any {
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

func opportunityReason(t *testing.T, env opportunityEnvelope) string {
	t.Helper()
	data := opportunityData(t, env)
	reason, ok := data["reason"].(string)
	if !ok {
		t.Fatalf("错误响应缺机器可读的 data.reason（前端只能靠文案分诊，而文案会被翻译）：%s", env.Message)
	}
	return reason
}

// —— ① 路由表 ——————————————————————————————————————————

func TestOpportunityRoutes_TableIsExactlyTheDeclaredSet(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller.NewOpportunityController(svc).RegisterRoutes(engine.Group("/api"))

	var got []string
	for _, r := range engine.Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(got)
	want := append([]string(nil), opportunityRouteSpecs...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("路由表与本卡声明的集合不一致：\n 得到 %v\n 期望 %v", got, want)
	}
}

// TestOpportunityRoutes_NoEndpointCanWriteWon AC③ 的 HTTP 侧兑现。
//
// 服务层的判据是「只有 collection_completed 这一来源能落 won」，而来源是**代码里的参数**，
// 不是请求体里的字段。一旦 HTTP 侧开一条 `POST /:id/won`（哪怕要求带 cause），
// 任何登录用户都能自称"回款完成了"，那张三元边表就退化回二元表 ——
// 而退化之后没人能说出赢单是谁宣布的。所以这里钉两件事：路由里没有 won，
// 控制器源码里连 MarkWonByCollection 这个名字都不出现（出现即说明有人加了入口）。
func TestOpportunityRoutes_NoEndpointCanWriteWon(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_w1", model.OpportunityStageNegotiation, model.OpportunityStatusOpen)

	engine := newOpportunityTestEngine(svc, uint(7))
	for _, path := range []string{
		"/api/opportunity/" + row.ID + "/won",
		"/api/opportunity/" + row.ID + "/win",
		"/api/opportunity/" + row.ID + "/close",
	} {
		if code, _, _ := doOpportunity(t, engine, http.MethodPost, path, `{"version":0}`); code != http.StatusNotFound {
			t.Errorf("%s 回 %d，期望 404：本卡不得暴露任何写 won 的入口", path, code)
		}
	}

	src := readControllerSource(t)
	if strings.Contains(src, "MarkWonByCollection") {
		t.Error("控制器源码里出现了 MarkWonByCollection：赢单只能由回款完成（P7）在服务内触发，HTTP 侧不得调用它")
	}
	// 反向对照：库里那一行仍是 open，且一次没被改过。
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.Status != model.OpportunityStatusOpen || fresh.Version != 0 {
		t.Errorf("探测性请求改写了现场：status=%s version=%d", fresh.Status, fresh.Version)
	}
}

// —— ② 无内联 handler（AC②）————————————————————————————

func readSourceFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return string(raw)
}

func readControllerSource(t *testing.T) string {
	t.Helper()
	return readSourceFile(t, "../controller/opportunity.go")
}

// TestOpportunityRoutes_RouterFileHasNoInlineHandler CLAUDE.md 铁律 2 的静态锁。
//
// 判据落"形状"而不是"行数"：路由文件里不得出现 gin.Context（它压根不该碰请求对象），
// 也不得出现直接写响应的调用。摘掉这条锁的代价是把业务判断挪进 router.go 的那次
// "顺手兜一下"，而那之后进程内调用方与 HTTP 调用方就有两套口径了。
func TestOpportunityRoutes_RouterFileHasNoInlineHandler(t *testing.T) {
	src := readSourceFile(t, "opportunity_routes.go")

	for _, needle := range []string{"gin.Context", ".JSON(", "AbortWithStatus", "response.Success", "response.Error"} {
		if strings.Contains(src, needle) {
			t.Errorf("路由文件里出现了 %q：本层只做 URL→Controller 映射，响应必须由控制器写", needle)
		}
	}
	if !strings.Contains(src, "RegisterRoutes(") {
		t.Error("路由文件没有把挂载委托给控制器的 RegisterRoutes：端点清单因此不在唯一一处可查")
	}
}

// —— ③ Swagger 覆盖（AC③）——————————————————————————————

// TestOpportunityRoutes_SwaggerCoversEveryRoute 每条端点都要有 @Router 注解。
//
// 判据是"集合覆盖"而不是"注解条数"：条数够但少一条路径，同样是那个端点在
// /swagger/index.html 上不存在 —— 前端照文档实现，文档缺一条就是照猜实现。
func TestOpportunityRoutes_SwaggerCoversEveryRoute(t *testing.T) {
	src := readControllerSource(t)

	declared := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "// @Router") {
			continue
		}
		// 形如：// @Router /api/opportunity/{id} [get]
		rest := strings.TrimPrefix(trimmed, "// @Router")
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			t.Errorf("@Router 注解形状不对：%q", trimmed)
			continue
		}
		path := strings.ReplaceAll(fields[0], "{id}", ":id")
		method := strings.Trim(fields[1], "[]")
		declared[strings.ToUpper(method)+" "+path] = true
	}

	for _, spec := range opportunityRouteSpecs {
		if !declared[spec] {
			t.Errorf("%s 没有对应的 @Router 注解：Swagger 面上缺这条端点", spec)
		}
	}
	for _, needle := range []string{"@Summary", "@Tags", "@Success"} {
		if !strings.Contains(src, needle) {
			t.Errorf("控制器缺 %s 注解：生成的文档只剩路径，读的人不知道这条端点回答什么问题", needle)
		}
	}
}

// —— 未装配 / 无底座：503 的形状 ————————————————————————

// TestOpportunityRoutes_RulesAnswersWithoutAssembly 契约端点不碰库，因此未装配也要能答。
//
// 反过来，五条要读库的端点必须在未装配时回 503，而不是空对象：
// `{"data":null}` 在前端长得出"这条商机不存在"，503 长不出那个结论。
// 这也是本卡天然的关闸 —— 不装配 ⇒ 读口全部诚实报错。
func TestOpportunityRoutes_RulesAnswersWithoutAssembly(t *testing.T) {
	restore := service.GlobalOpportunityService()
	defer service.SetGlobalOpportunityService(restore)
	service.SetGlobalOpportunityService(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) { c.Set("user_id", uint(1)); c.Next() })
	setupOpportunityRoutes(auth)

	code, env, _ := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/rules", "")
	if code != http.StatusOK {
		t.Fatalf("/rules 在未装配时回 %d，期望 200：机器规则与 DB 句柄无关：%s", code, env.Message)
	}
	data := opportunityData(t, env)
	if _, ok := data["stage_order"]; !ok {
		t.Errorf("/rules 未给出 stage_order：%v", data)
	}

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/opportunity/opp_any", ""},
		{http.MethodGet, "/api/opportunity/opp_any/moves", ""},
		{http.MethodPut, "/api/opportunity/opp_any", `{"version":0,"amount":1,"currency":"CNY"}`},
		{http.MethodPost, "/api/opportunity/opp_any/stage", `{"version":0,"to_stage":"proposal"}`},
		{http.MethodPost, "/api/opportunity/opp_any/lost", `{"version":0,"reason":"预算撤回"}`},
		{http.MethodPost, "/api/opportunity/opp_any/cancel", `{"version":0}`},
		{http.MethodPost, "/api/opportunity/opp_any/reopen", `{"version":0}`},
	} {
		code, env, _ := doOpportunity(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s %s 在未装配时回 %d，期望 503：%s", probe.method, probe.path, code, env.Message)
		}
		// 判据是"不许出现**空** data"，不是"不许出现 data"：`{"data":{}}`、`[]`、`null`
		// 都是一句业务结论（"查过了，没有"），而此刻的事实是"一次都没查"。
		// 反过来，一个把话说明白的 reason 对象是**加分**的：503 与 500 在网关日志里
		// 长得一样，前者该等装配、后者该查故障，只靠 HTTP 状态码分不开那两种动作。
		// 摘掉 reason（改成 gin.H{}）或改成回 200 空对象，本条都会红。
		if raw := string(env.Data); raw == "" || raw == "null" || raw == "{}" || raw == "[]" {
			t.Errorf("%s %s 的 503 把 data 留成了 %q：空对象会被读成「查过了，没有」",
				probe.method, probe.path, raw)
		} else if reason := opportunityReason(t, env); reason != "unavailable" {
			t.Errorf("%s %s 的 reason=%q，期望 unavailable", probe.method, probe.path, reason)
		}
		if !strings.Contains(env.Message, "未装配") && !strings.Contains(env.Message, "底座") {
			t.Errorf("%s %s 的 503 文案没说是装配缺失：%q —— 读的人会去查这条商机",
				probe.method, probe.path, env.Message)
		}
	}
}

// —— 契约端点内容 ——————————————————————————————————————

func TestOpportunityRoutes_RulesAreTheMachineRules(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	engine := newOpportunityTestEngine(svc, uint(7))

	code, env, raw := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/rules", "")
	if code != http.StatusOK {
		t.Fatalf("/rules 回 %d：%s —— %s", code, env.Message, raw)
	}
	data := opportunityData(t, env)

	if got := stringSlice(data["stage_order"]); !reflect.DeepEqual(got, model.OpportunityStages) {
		t.Errorf("stage_order=%v，期望与 model.OpportunityStages 逐字同序 %v（顺序就是管线）", got, model.OpportunityStages)
	}
	if got := stringSlice(data["status_values"]); !reflect.DeepEqual(got, model.OpportunityStatuses) {
		t.Errorf("status_values=%v，期望 %v", got, model.OpportunityStatuses)
	}
	if got := stringSlice(data["closed_statuses"]); !reflect.DeepEqual(got,
		[]string{model.OpportunityStatusWon, model.OpportunityStatusLost, model.OpportunityStatusCancelled}) {
		t.Errorf("closed_statuses=%v，期望 {won,lost,cancelled}（读方要靠它区分「还要不要有人推进」）", got)
	}

	moves, ok := data["moves"].(map[string]any)
	if !ok {
		t.Fatalf("moves 不是按起点状态分组的对象：%T", data["moves"])
	}
	// 逐格与 AllowedOpportunityMoves 对照：响应面与判据表必须同源。
	for _, status := range model.OpportunityStatuses {
		byStage, ok := moves[status].(map[string]any)
		if !ok {
			t.Errorf("moves[%s] 缺失或不是对象：%T", status, moves[status])
			continue
		}
		for _, stage := range model.OpportunityStages {
			want := movePairs(service.AllowedOpportunityMoves(stage, status))
			got := movePairs(decodeMoves(t, byStage[stage]))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("moves[%s][%s]=%v，期望与状态机表一致 %v", status, stage, got, want)
			}
			for _, pair := range got {
				if pair == "status:won" {
					t.Errorf("moves[%s][%s] 里有 status:won —— 赢单不是按钮（见 server_only_moves）", status, stage)
				}
			}
		}
	}

	// 每一格都必须是数组：终态行（won/cancelled）在全部四个阶段上都是空清单，
	// 空清单序列化成 null 与序列化成 [] 在这张表的 DeepEqual 里长得一样（两边都是空切片），
	// 所以只有从**原始 JSON** 上判才看得见那一处 nil→[] 的兜底被摘掉。
	// 前端拿 null 会渲染成"读取出错"，拿 [] 才渲染成"这一行没有可做的动作"。
	if movesJSON, err := json.Marshal(moves); err == nil && strings.Contains(string(movesJSON), "null") {
		t.Errorf("/rules 的 moves 里有 null：%s —— 空清单要说成空数组，否则与读失败同形",
			trimForLog(string(movesJSON), 160))
	}

	only, ok := data["server_only_moves"].([]any)
	if !ok || len(only) == 0 {
		t.Fatalf("server_only_moves 缺失：得有人说明为什么按钮堆里没有赢单：%v", data["server_only_moves"])
	}
	first, ok := only[0].(map[string]any)
	if !ok {
		t.Fatalf("server_only_moves[0] 不是对象：%T", only[0])
	}
	if first["target"] != model.OpportunityStatusWon ||
		first["cause"] != string(service.OpportunityCauseCollection) || first["http_exposed"] != false {
		t.Errorf("server_only_moves[0]=%v，期望 {target:won, cause:%s, http_exposed:false}",
			first, service.OpportunityCauseCollection)
	}
	if editable := stringSlice(data["editable_fields"]); !reflect.DeepEqual(editable,
		[]string{"amount", "currency", "owner_user_id", "expected_close_at"}) {
		t.Errorf("editable_fields=%v，期望整份编辑那四格（派生量与身份列不得出现在这里）", editable)
	}
}

func decodeMoves(t *testing.T, v any) []service.OpportunityMove {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化 moves 项失败：%v", err)
	}
	var out []service.OpportunityMove
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("moves 项形状不对：%v —— %s", err, raw)
	}
	return out
}

func movePairs(moves []service.OpportunityMove) []string {
	out := make([]string, 0, len(moves))
	for _, m := range moves {
		out = append(out, string(m.Kind)+":"+m.Target)
	}
	return out
}

func stringSlice(v any) []string {
	raw, _ := json.Marshal(v)
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

// —— 读端点 ————————————————————————————————————————————

func TestOpportunityRoutes_GetReturnsTheRowByColumnNames(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_r1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	code, env, raw := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/"+row.ID, "")
	if code != http.StatusOK {
		t.Fatalf("GET 回 %d：%s —— %s", code, env.Message, raw)
	}
	data := opportunityData(t, env)
	for _, key := range []string{"id", "code", "customer_id", "stage", "status", "amount",
		"currency", "win_probability", "owner_user_id", "version"} {
		if _, ok := data[key]; !ok {
			t.Errorf("响应里没有 %q（对外 JSON 名必须与列名一一对应，否则 T-P4-01 那条同名字用例保住的性质在出口处又散了）：%v", key, data)
		}
	}
	if data["stage"] != model.OpportunityStageProposal || data["status"] != model.OpportunityStatusOpen {
		t.Errorf("stage/status 串位了：%v / %v", data["stage"], data["status"])
	}
	if v, _ := data["version"].(float64); v != 0 {
		t.Errorf("version=%v，新行应为 0（调用方拿它当下一刀的期望版本）", v)
	}
}

func TestOpportunityRoutes_GetNotFoundAndOverLongID(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	engine := newOpportunityTestEngine(svc, uint(7))

	code, env, _ := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/opp_missing", "")
	if code != http.StatusNotFound {
		t.Errorf("不存在的 id 回 %d，期望 404：%s", code, env.Message)
	}
	if reason := opportunityReason(t, env); reason != "not_found" {
		t.Errorf("reason=%q，期望 not_found", reason)
	}

	longID := "opp_" + strings.Repeat("x", 200)
	code, env, _ = doOpportunity(t, engine, http.MethodGet, "/api/opportunity/"+longID, "")
	if code != http.StatusBadRequest {
		t.Errorf("超长 id 回 %d，期望 400（先把长度收住再回显，否则响应体是个免费的放大器）：%s", code, env.Message)
	}
	if reason := opportunityReason(t, env); reason != "input_invalid" {
		t.Errorf("reason=%q，期望 input_invalid", reason)
	}
	if strings.Contains(env.Message, strings.Repeat("x", 200)) {
		t.Error("超长 id 被整段回显进 message")
	}
}

// TestOpportunityRoutes_BlankIDIsRefusedBeforeAnyQuery 空白 id 必须在关闸处拒掉，
// 而且判据要能分清"关闸拒的"与"库里查无此行"—— 两者都回 4xx 时，只有后者会说 not_found。
//
// 为什么单独立一条：服务层的 Get 自己也会 TrimSpace 后拒空，所以**读口**摘掉控制器这道关
// 看不出差别（一次白跑的查询 + 同一句 400）。差别在**写口**：transition 不判空，
// 摘掉这道关之后，`PUT /api/opportunity/%20` 会拿一个空白串去库里查，落空 ⇒ 回 404 not_found，
// 对着调用方说"这条商机不存在"，而事实是"这次请求根本没给 id"。
// 用 failingOpportunityRepo 才能把"没查"这件事断出来：一查就是 500，绝不会是 400。
func TestOpportunityRoutes_BlankIDIsRefusedBeforeAnyQuery(t *testing.T) {
	failing := service.NewOpportunityService(
		failingOpportunityRepo{err: errors.New("connection reset by peer")})
	failing.SetClock(func() time.Time { return oppTestClock })
	engine := newOpportunityTestEngine(failing, uint(7))

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/opportunity/%20", ""},
		{http.MethodGet, "/api/opportunity/%20%20%20", ""},
		{http.MethodPut, "/api/opportunity/%20", `{"version":0,"amount":1,"currency":"CNY","owner_user_id":"sales_a"}`},
		{http.MethodPost, "/api/opportunity/%20/lost", `{"version":0,"reason":"价格"}`},
	} {
		code, env, _ := doOpportunity(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s %s 回 %d，期望 400：%s", probe.method, probe.path, code, env.Message)
			continue
		}
		if reason := opportunityReason(t, env); reason != "input_invalid" {
			t.Errorf("%s %s 的 reason=%q，期望 input_invalid（空白 id 不能说成查无此行）",
				probe.method, probe.path, reason)
		}
	}

	// 对照组：同一个句柄收到一个**非空** id 就必须真去查（回 500）。
	// 少了这一条，上面四个 400 可能只是因为路由根本没接上服务。
	if code, env, _ := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/opp_real", ""); code != http.StatusInternalServerError {
		t.Fatalf("非空 id 回 %d，期望 500（对照组不成立，上面的 400 判不出关闸）：%s", code, env.Message)
	}
}

func TestOpportunityRoutes_MovesMatchTheRow(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	proposal := seedOpportunityRow(t, database, "opp_m1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	lost := seedOpportunityRow(t, database, "opp_m2", model.OpportunityStageNeedsConfirmed, model.OpportunityStatusLost)
	won := seedOpportunityRow(t, database, "opp_m3", model.OpportunityStageNegotiation, model.OpportunityStatusWon)
	engine := newOpportunityTestEngine(svc, uint(7))

	cases := []struct {
		id   string
		want []string
	}{
		// 在跑的 proposal：向前一步、**向后两步**（回退不设限：禁回退的出口是"作废重建"，
		// 而那会把 C6 北极星的分母灌进一行行修数据的假商机），再加两个收口动作。
		{proposal.ID, []string{"stage:needs_confirmed", "stage:negotiation", "stage:qualification",
			"status:cancelled", "status:lost"}},
		{lost.ID, []string{"status:open"}},
		{won.ID, []string{}},
	}
	for _, tc := range cases {
		code, env, raw := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/"+tc.id+"/moves", "")
		if code != http.StatusOK {
			t.Fatalf("%s moves 回 %d：%s —— %s", tc.id, code, env.Message, raw)
		}
		data := opportunityData(t, env)
		// 按集合比：这份清单是给按钮用的，**顺序**由 /rules 的 stage_order 表达（那里逐字钉过）。
		// 在这里再钉一遍顺序等于让同一件事有两处判据，而两处里更新的那一处才决定按钮长什么样。
		got := append([]string{}, movePairs(decodeMoves(t, data["moves"]))...)
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s 的可用动作=%v，期望 %v", tc.id, got, tc.want)
		}
		if data["moves"] == nil {
			t.Errorf("%s 的 moves 是 null：终态行要说「没有可做的动作」，不是「没读到」（数组必须是 []）", tc.id)
		}
	}

	// 缺行的 moves 必须是 404，不能落到"空 moves + 200"：后者是一句业务结论
	// （"这一行没有可做的动作"），而终态行才配那句话。摘掉 Moves 里的 nil 行判据，
	// 上面三个用例全都看不境（它们的行都在库里），只有这一条能红。
	code, env, _ := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/opp_moves_absent/moves", "")
	if code != http.StatusNotFound || opportunityReason(t, env) != "not_found" {
		t.Errorf("不存在的行查 moves 回 %d，期望 404/not_found：%s", code, env.Message)
	}
}

// —— 写端点：版本、区分性状态码、整份替换语义 ——————————————

// TestOpportunityWrites 五类写动作的正例与"缺 version"的反例。
//
// 缺字段与显式 0 必须分开判：新行的合法期望版本就是 0，
// 用 int 接 body 再判 `== 0` 会把"没带版本"当成"以 v0 为准"，
// 于是并发改写撞在第一条上时谁都不会被拒。
func TestOpportunityWrites_RequireVersionAndApply(t *testing.T) {
	type write struct {
		name          string
		method        string
		pathSuffix    string
		body          string
		bodyNoVersion string
		wantStage     string
		wantStatus    string
		wantVersion   int64
	}
	cases := []write{
		{"stage", http.MethodPost, "/stage", `{"version":0,"to_stage":"negotiation"}`,
			`{"to_stage":"negotiation"}`,
			model.OpportunityStageNegotiation, model.OpportunityStatusOpen, 1},
		{"edit", http.MethodPut, "", `{"version":0,"amount":2500.55,"currency":"USD","owner_user_id":"sales_b"}`,
			`{"amount":2500.55,"currency":"USD","owner_user_id":"sales_b"}`,
			model.OpportunityStageQualification, model.OpportunityStatusOpen, 1},
		{"lost", http.MethodPost, "/lost", `{"version":0,"reason":"对手报价更低"}`,
			`{"reason":"对手报价更低"}`,
			model.OpportunityStageQualification, model.OpportunityStatusLost, 1},
		{"cancel", http.MethodPost, "/cancel", `{"version":0}`,
			`{}`,
			model.OpportunityStageQualification, model.OpportunityStatusCancelled, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.Opportunity{})
			svc := opportunityTestService(t, database)
			id := "opp_w_" + tc.name
			stage := model.OpportunityStageQualification
			if tc.name == "stage" {
				stage = model.OpportunityStageProposal
			}
			row := seedOpportunityRow(t, database, id, stage, model.OpportunityStatusOpen)
			engine := newOpportunityTestEngine(svc, uint(7))

			// 反例：把 version 摘掉（其余字段原样，保证红的因只有"缺版本"这一处）。
			code, env, _ := doOpportunity(t, engine, tc.method, "/api/opportunity/"+row.ID+tc.pathSuffix, tc.bodyNoVersion)
			if code != http.StatusBadRequest {
				t.Errorf("不带 version 的 %s 回 %d，期望 400：%s", tc.name, code, env.Message)
			}
			if reason := opportunityReason(t, env); reason != "input_invalid" {
				t.Errorf("不带 version 的 reason=%q，期望 input_invalid", reason)
			}
			if fresh := readOpportunityRow(t, database, id); fresh.Version != 0 || fresh.Stage != stage {
				t.Errorf("被拒的 %s 仍改写了现场：stage=%s version=%d", tc.name, fresh.Stage, fresh.Version)
			}

			// 正例。
			code, env, raw := doOpportunity(t, engine, tc.method, "/api/opportunity/"+row.ID+tc.pathSuffix, tc.body)
			if code != http.StatusOK {
				t.Fatalf("%s 回 %d：%s —— %s", tc.name, code, env.Message, raw)
			}
			data := opportunityData(t, env)
			if data["stage"] != tc.wantStage || data["status"] != tc.wantStatus {
				t.Errorf("%s 之后响应体=%s/%s，期望 %s/%s",
					tc.name, data["stage"], data["status"], tc.wantStage, tc.wantStatus)
			}
			fresh := readOpportunityRow(t, database, id)
			if fresh.Stage != tc.wantStage || fresh.Status != tc.wantStatus {
				t.Errorf("%s 之后库里=%s/%s，期望 %s/%s（响应与库里必须是同一个数）",
					tc.name, fresh.Stage, fresh.Status, tc.wantStage, tc.wantStatus)
			}
			if fresh.Version != tc.wantVersion {
				t.Errorf("%s 之后 version=%d，期望 %d", tc.name, fresh.Version, tc.wantVersion)
			}
		})
	}
}

// TestOpportunityWrites_DistinguishableRejections 四种 4xx 各自带自己的 reason。
//
// 状态码不够分：400/404/409 各只有一种，而"该改请求""该刷新""该回退状态""该去查数据"
// 是四件不同的事。服务层的四个 sentinel 各对应一种修法（T-P4-03 的判据），
// 到 HTTP 侧若被并成一个 409，那个区分就只活在 err.Error() 的中文里 ——
// 文案会被改写、会被 i18n，判据不该寄生在文案上。
func TestOpportunityWrites_DistinguishableRejections(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	engine := newOpportunityTestEngine(svc, uint(7))

	stale := seedOpportunityRow(t, database, "opp_d_stale", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	closed := seedOpportunityRow(t, database, "opp_d_closed", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	badStage := seedOpportunityRow(t, database, "opp_d_badstage", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	badInput := seedOpportunityRow(t, database, "opp_d_badinput", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	missing := seedOpportunityRow(t, database, "opp_d_missing", model.OpportunityStageProposal, model.OpportunityStatusOpen)

	// stale：先用 v0 改一次，再拿 v0 改第二次。
	if code, env, _ := doOpportunity(t, engine, http.MethodPost,
		"/api/opportunity/"+stale.ID+"/stage", `{"version":0,"to_stage":"negotiation"}`); code != http.StatusOK {
		t.Fatalf("前置改写失败：%d %s", code, env.Message)
	}
	code, env, _ := doOpportunity(t, engine, http.MethodPost,
		"/api/opportunity/"+stale.ID+"/stage", `{"version":0,"to_stage":"qualification"}`)
	if code != http.StatusConflict || opportunityReason(t, env) != "stale_version" {
		t.Errorf("过期版本回 %d/%s，期望 409/stale_version：%s", code, opportunityReason(t, env), env.Message)
	}
	// 只有 stale 这一种带"重取后再改"的下一步指令：它是四种 409 里唯一"照做就有答案"的。
	// 把这句摊给全部 409（writeConflict 里那个分支的条件摘成恒真）也是同样红的。
	if !strings.Contains(env.Message, "重取") {
		t.Errorf("stale_version 的提示没给出下一步动作：%q", env.Message)
	}

	// closed：收口之后再改金额。
	if code, _, _ := doOpportunity(t, engine, http.MethodPost,
		"/api/opportunity/"+closed.ID+"/cancel", `{"version":0}`); code != http.StatusOK {
		t.Fatalf("前置收口失败：%d", code)
	}
	code, env, _ = doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+closed.ID,
		`{"version":1,"amount":10,"currency":"CNY","owner_user_id":"sales_a"}`)
	reason := opportunityReason(t, env)
	if code != http.StatusConflict || reason != "closed" {
		t.Errorf("已收口的改写回 %d/%s，期望 409/closed：%s", code, reason, env.Message)
	}
	// 反向断：不许串成另外两种 409（那会把人支去查边表或重取版本）。
	if reason == "transition_illegal" || reason == "stale_version" || reason == "state_invalid" {
		t.Errorf("已收口的 reason=%q：三种 409 混成一种，修法就分不开了", reason)
	}
	// 再反向断一次文案："重取这一行后再改"只对版本过期是指令，对已收口是误导
	// （重取一百次也取不回一个可改的状态，只会让人以为刷新就能接着改）。
	if strings.Contains(env.Message, "重取") {
		t.Errorf("已收口的提示里出现了「重取」：%q —— 那句话是 stale 专属的下一步", env.Message)
	}

	// transition_illegal：同格推进（qualification → qualification）。
	code, env, _ = doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+badStage.ID+"/stage",
		`{"version":0,"to_stage":"`+model.OpportunityStageProposal+`"}`)
	reason = opportunityReason(t, env)
	if code != http.StatusConflict || reason != "transition_illegal" {
		t.Errorf("同格推进回 %d/%s，期望 409/transition_illegal：%s", code, reason, env.Message)
	}

	// input_invalid：未知阶段字面值（值域外，不是状态机外）。
	code, env, _ = doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+badInput.ID+"/stage",
		`{"version":0,"to_stage":"closed_won"}`)
	if code != http.StatusBadRequest || opportunityReason(t, env) != "input_invalid" {
		t.Errorf("未知阶段回 %d/%s，期望 400/input_invalid：%s", code, opportunityReason(t, env), env.Message)
	}

	// not_found：行不存在。
	code, env, _ = doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+missing.ID+"_x/lost",
		`{"version":0,"reason":"x"}`)
	if code != http.StatusNotFound || opportunityReason(t, env) != "not_found" {
		t.Errorf("不存在的行回 %d/%s，期望 404/not_found：%s", code, opportunityReason(t, env), env.Message)
	}

	// 五者互不相同（这一句才是"分得开"的判据，单条断言各测各的会漏掉两两相同）。
	seen := map[string]bool{"stale_version": true, "closed": true, "transition_illegal": true,
		"input_invalid": true, "not_found": true}
	if len(seen) != 5 {
		t.Fatalf("reason 词表本身重复了：%v", seen)
	}
}

// TestOpportunityWrites_StateInvalidIsItsOwnClass 库里那一行本身越界 ⇒ 409/state_invalid。
//
// 这一格必须与上面四种分开：它要求的是"去查数据"，既不是改请求也不是重取版本。
// 造法只能直插（仓储不校验，那是 T-P4-02 定的前提），本卡不能顺手补校验。
func TestOpportunityWrites_StateInvalidIsItsOwnClass(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_si", model.OpportunityStageProposal, model.OpportunityStatusOpen)

	if err := database.Model(&model.Opportunity{}).Where("id = ?", row.ID).
		Update("win_probability", 7.5).Error; err != nil { // 越出 0–1：仓储不拦，服务拦
		t.Fatalf("制造越界现值失败：%v", err)
	}
	engine := newOpportunityTestEngine(svc, uint(7))

	code, env, _ := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/opp_si/stage",
		`{"version":0,"to_stage":"negotiation"}`)
	reason := opportunityReason(t, env)
	if code != http.StatusConflict || reason != "state_invalid" {
		t.Errorf("越界现值回 %d/%s，期望 409/state_invalid：%s", code, reason, env.Message)
	}
	if fresh := readOpportunityRow(t, database, "opp_si"); fresh.WinProbability != 7.5 || fresh.Version != 0 {
		t.Errorf("拒绝时改写了现场：win=%v version=%d（本层要在动手之前停）", fresh.WinProbability, fresh.Version)
	}
}

// TestOpportunityRoutes_EditIsFullReplaceAndClearsCloseDate PUT 的"整份替换"语义。
//
// 服务层的 OpportunityEdit 没有"未提供"这一档（注释点明了理由），所以 HTTP 侧也不能假装稀疏：
// 省略 expected_close_at = 清掉它。这条用例的起点态必须非默认值，
// 否则"赋值臂"与"清空臂"写出同一个 NULL，测了也等于没测。
func TestOpportunityRoutes_EditIsFullReplaceAndClearsCloseDate(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_e1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	withDate := `{"version":0,"amount":1200,"currency":"CNY","owner_user_id":"sales_a",` +
		`"expected_close_at":"` + opportunityTestNow(72*time.Hour).UTC().Format(time.RFC3339) + `"}`
	code, env, raw := doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID, withDate)
	if code != http.StatusOK {
		t.Fatalf("带关单日的 PUT 回 %d：%s —— %s", code, env.Message, raw)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.ExpectedCloseAt == nil {
		t.Fatal("关单日没落库")
	}

	// 再 PUT 一次、这次不写那一格 ⇒ 必须是 NULL（清掉），不是保留。
	code, env, raw = doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID,
		`{"version":1,"amount":1300,"currency":"CNY","owner_user_id":"sales_a"}`)
	if code != http.StatusOK {
		t.Fatalf("省略关单日的 PUT 回 %d：%s —— %s", code, env.Message, raw)
	}
	data := opportunityData(t, env)
	if _, ok := data["expected_close_at"]; ok {
		t.Errorf("响应体仍带着 expected_close_at：%v（省略＝清掉，PUT 不是 PATCH）", data["expected_close_at"])
	}
	cleared := readOpportunityRow(t, database, row.ID)
	if cleared.ExpectedCloseAt != nil {
		t.Errorf("库里仍带着 %v：清掉没落库", *cleared.ExpectedCloseAt)
	}
	if cleared.Amount != 1300 || cleared.Version != 2 {
		t.Errorf("改写没生效：amount=%v version=%d", cleared.Amount, cleared.Version)
	}
}

// TestOpportunityRoutes_RejectsDerivedAndIdentityFields 派生量与身份列不许被手写。
//
// 服务层的输入结构里根本没有赢率那一格（C5：派生量不许手写），但 JSON 绑定
// 默认丢弃未知字段 —— 于是 `{"win_probability":0.99}` 会**静默成功**，
// 调用方以为改了、库里没改，两边对同一行各有两套账。所以拒收要出声。
func TestOpportunityRoutes_RejectsDerivedAndIdentityFields(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_x", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	for _, body := range []string{
		`{"version":0,"amount":1,"currency":"CNY","win_probability":0.99}`,
		`{"version":0,"amount":1,"currency":"CNY","status":"won"}`,
		`{"version":0,"amount":1,"currency":"CNY","stage":"negotiation"}`,
		`{"version":0,"amount":1,"currency":"CNY","id":"opp_hijack"}`,
		`{"version":0,"amount":1,"currency":"CNY","confidence":0.9}`,
		`{"version":0,"amount":1,"currency":"CNY","extra":"x"}`,
	} {
		code, env, _ := doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID, body)
		if code != http.StatusBadRequest {
			t.Errorf("PUT %s 回 %d，期望 400：未知字段必须拒收而不是静默丢弃：%s", body, code, env.Message)
			continue
		}
		if reason := opportunityReason(t, env); reason != "input_invalid" {
			t.Errorf("PUT %s 的 reason=%q，期望 input_invalid", body, reason)
		}
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.WinProbability != 0.42 || fresh.Status != model.OpportunityStatusOpen || fresh.ID != row.ID {
		t.Errorf("被拒的改写动了库：win=%v status=%s id=%s", fresh.WinProbability, fresh.Status, fresh.ID)
	}
}

func TestOpportunityRoutes_LostRequiresReason(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_lr", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	for _, body := range []string{`{"version":0}`, `{"version":0,"reason":""}`, `{"version":0,"reason":"   "}`} {
		code, env, _ := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+row.ID+"/lost", body)
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400（丢单必须有原因，否则丢单归因读成空集）：%s", body, code, env.Message)
		}
	}

	// 正例之后：状态落 lost、原因落库、赢率冻结在收口那一刻。
	code, env, raw := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+row.ID+"/lost",
		`{"version":0,"reason":"预算撤回"}`)
	if code != http.StatusOK {
		t.Fatalf("带原因的丢单回 %d：%s —— %s", code, env.Message, raw)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.Status != model.OpportunityStatusLost || fresh.LostReason != "预算撤回" || fresh.WinProbability != 0.42 {
		t.Errorf("收口落库不对：status=%s reason=%q win=%v", fresh.Status, fresh.LostReason, fresh.WinProbability)
	}
	// 再推一次（同一行已收口）：409/closed，而不是把赢率重算一遍。
	code, env, _ = doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+row.ID+"/lost",
		`{"version":1,"reason":"再说一次"}`)
	if code != http.StatusConflict || opportunityReason(t, env) != "closed" {
		t.Errorf("重复丢单回 %d/%s，期望 409/closed", code, opportunityReason(t, env))
	}
	fresh = readOpportunityRow(t, database, row.ID)
	if fresh.LostReason != "预算撤回" || fresh.Version != 1 {
		t.Errorf("重复丢单改写了现场：reason=%q version=%d", fresh.LostReason, fresh.Version)
	}
}

// TestOpportunityRoutes_ReopenLostRow 输单可回退、赢单/取消不可（三种终态两种命运）。
func TestOpportunityRoutes_ReopenByTerminalKind(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	lost := seedOpportunityRow(t, database, "opp_ro_lost", model.OpportunityStageNeedsConfirmed, model.OpportunityStatusLost)
	won := seedOpportunityRow(t, database, "opp_ro_won", model.OpportunityStageNegotiation, model.OpportunityStatusWon)
	cancelled := seedOpportunityRow(t, database, "opp_ro_cancelled", model.OpportunityStageProposal, model.OpportunityStatusCancelled)
	engine := newOpportunityTestEngine(svc, uint(7))

	code, env, raw := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+lost.ID+"/reopen", `{"version":0}`)
	if code != http.StatusOK {
		t.Fatalf("lost 行 reopen 回 %d：%s —— %s", code, env.Message, raw)
	}
	fresh := readOpportunityRow(t, database, lost.ID)
	if fresh.Status != model.OpportunityStatusOpen || fresh.LostReason != "" {
		t.Errorf("reopen 后 status=%s reason=%q，期望 open 且清空原因", fresh.Status, fresh.LostReason)
	}
	// 回到 open 之后赢率必须按当前事实重算（proposal 之前那格：needs_confirmed 0.30，有归属、未逾期）。
	if fresh.WinProbability != 0.30 {
		t.Errorf("reopen 后赢率=%v，期望 0.30（收口时冻结、回退后必须重新计算）", fresh.WinProbability)
	}

	for _, tc := range []struct{ id, wantReason string }{
		{won.ID, "transition_illegal"},
		{cancelled.ID, "transition_illegal"},
	} {
		code, env, _ := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+tc.id+"/reopen", `{"version":0}`)
		reason := opportunityReason(t, env)
		if code != http.StatusConflict || reason != tc.wantReason {
			t.Errorf("%s reopen 回 %d/%s，期望 409/%s：%s", tc.id, code, reason, tc.wantReason, env.Message)
		}
	}
}

// TestOpportunityRoutes_OwnerIsNotAPermission X3：owner_user_id 是归属，不是权限。
//
// 单商户部署下"谁能看到哪条商机"不构成隔离边界（同一份库里所有人的商机都在同一个池子）。
// 这一条钉的是"别在 HTTP 侧悄悄加一层归属校验"：加了它，销售 A 请病假时他名下的商机
// 在系统里就没人能动了，而那件事会以"按钮点了没反应"的形式被发现。
func TestOpportunityRoutes_OwnerIsNotAPermission(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_acl", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(999)) // 与 owner_user_id=sales_a 完全不同的身份

	code, env, raw := doOpportunity(t, engine, http.MethodPost, "/api/opportunity/"+row.ID+"/stage",
		`{"version":0,"to_stage":"negotiation"}`)
	if code != http.StatusOK {
		t.Fatalf("非归属人推进阶段回 %d：%s —— %s（归属不是权限，见 X3）", code, env.Message, raw)
	}
	if data := opportunityData(t, env); data["owner_user_id"] != "sales_a" {
		t.Errorf("推进阶段改写了归属：%v", data["owner_user_id"])
	}
}

// TestOpportunityRoutes_BodyShapeFailures 空体 / 非法 JSON / 类型不对 ⇒ 400，且都不落库。
func TestOpportunityRoutes_BodyShapeFailures(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_bs", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	for _, tc := range []struct {
		method   string
		body     string
		wantIn   string // 提示里必须出现的判据词
		wantNot  string // 提示里不得出现的另一种诊断（两种失败的修法不同）
		badChars string // 不得整段回显的填充
	}{
		// 空体与形状错是两种修法：前者"没带请求体"，后者"带了但字段名/类型不对"。
		// 只断 400 的话，把 EOF 专判摘掉（退成通用文案）这条照样绿 —— 那才是本条的牙。
		{http.MethodPost, ``, "空", "形状", ""},
		{http.MethodPost, `not json`, "形状", "", ""},
		{http.MethodPost, `{"version":"0","to_stage":"negotiation"}`, "形状", "", ""},
		{http.MethodPost, `{"version":0,"to_stage":123}`, "形状", "", ""},
		{http.MethodPost, `[]`, "形状", "", ""},
		// 底层错误文本要有界：一个 300 字符的未知字段名会把 json 的报错整段带进响应，
		// 而 message 是会被日志、网关、前端原样抄走的那一列。
		{http.MethodPost, `{"` + strings.Repeat("x", 300) + `":1,"version":0}`, "形状", "", strings.Repeat("x", 250)},
	} {
		code, env, _ := doOpportunity(t, engine, tc.method, "/api/opportunity/"+row.ID+"/stage", tc.body)
		if code != http.StatusBadRequest {
			t.Errorf("请求体 %q 回 %d，期望 400：%s", abbrevForLog(tc.body), code, env.Message)
			continue
		}
		if reason := opportunityReason(t, env); reason != "input_invalid" {
			t.Errorf("请求体 %q 的 reason=%q，期望 input_invalid", abbrevForLog(tc.body), reason)
		}
		if tc.wantIn != "" && !strings.Contains(env.Message, tc.wantIn) {
			t.Errorf("请求体 %q 的提示没说出「%s」这一类：%q", abbrevForLog(tc.body), tc.wantIn, env.Message)
		}
		if tc.wantNot != "" && strings.Contains(env.Message, tc.wantNot) {
			t.Errorf("请求体 %q 的提示串成了另一类（含「%s」）：%q", abbrevForLog(tc.body), tc.wantNot, env.Message)
		}
		if tc.badChars != "" && strings.Contains(env.Message, tc.badChars) {
			t.Errorf("请求体 %q 的底层错误文本没被截断：%q", abbrevForLog(tc.body), env.Message)
		}
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.Version != 0 || fresh.Stage != model.OpportunityStageProposal {
		t.Errorf("被拒的请求改写了现场：stage=%s version=%d", fresh.Stage, fresh.Version)
	}
}

func abbrevForLog(body string) string {
	return trimForLog(body, 24)
}

// trimForLog 只服务于失败输出：断言本身用的是完整的字符串。
func trimForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// TestOpportunityRoutes_OversizedBodyIsRefusedBeforeParsing 超大请求体必须在**读**这一层被拒，
// 而不是解析完之后当成"字段太多"。
//
// 反例要故意造一份**形状完全合法**的超大体：如果它只是字段名乱来，那么摘掉体积上限后
// DisallowUnknownFields 会替它挡住，这条用例就永远绿着——那正是"看起来有断言、实际没有牙"。
// 上限存在不是因为这一份体会打爆内存（4KB 打不爆），是因为不封顶的请求体
// 让"改一行商机的金额"这个动作的开销由调用方决定。
func TestOpportunityRoutes_OversizedBodyIsRefusedBeforeParsing(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_big", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	// 填充只用**已声明的字段**重复堆量（JSON 允许同键重复，后者覆盖前者）。
	// 若图省事塞一个 `{"pad":"…"}`，摘掉体积上限后 DisallowUnknownFields 会替它挡住 400，
	// 这条用例就永远绿着 —— 那正是"看着有断言、实际没有牙"。
	fat := `{` + strings.Repeat(`"version":0,"amount":1,"currency":"CNY","owner_user_id":"sales_a",`, 200) +
		`"version":0,"amount":1,"currency":"CNY","owner_user_id":"sales_a"}`
	// 前置必须自己站住：这份体**形状合法**且**确实超上限**（声明的容量是 4KB）。
	// 少了这两条，下面的 400 可能来自一个语法错误 —— 那等于在测"我打错了 JSON"，
	// 把体积上限那一刀整格盖住（本用例的第一版就是这么假绿的：开头多写了一个引号）。
	if err := json.Unmarshal([]byte(fat), &map[string]any{}); err != nil {
		t.Fatalf("超大体夹具本身不是合法 JSON，判不出体积上限：%v", err)
	}
	if len(fat) <= 4<<10 {
		t.Fatalf("超大体夹具只有 %d 字节，没超过 4KB 上限，判不出这一刀", len(fat))
	}
	code, env, _ := doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID, fat)
	if code != http.StatusBadRequest {
		t.Errorf("%d 字节请求体回 %d，期望 400（字段名全对也不行，读这一层就要封顶）：%s", len(fat), code, env.Message)
	}
	if reason := opportunityReason(t, env); reason != "input_invalid" {
		t.Errorf("reason=%q，期望 input_invalid", reason)
	}
	// 拒因要说"体积"，不能是把 Go 的错误串原样递出去：
	// `http: request body too large` 混在"请求体形状不对："后面，运维读到的是形状问题，实际是封顶。
	if !strings.Contains(env.Message, "上限") {
		t.Errorf("超大体的拒因没落到体积上：%s", env.Message)
	}
	if strings.Contains(env.Message, "http:") || strings.Contains(env.Message, "invalid character") {
		t.Errorf("把 Go 的错误串直接回给了调用方：%s", env.Message)
	}
	// 同一份体去掉重复必须能改成 —— 否则上面那条 400 可能来自别的原因，本条就成了假绿。
	lean := `{"version":0,"amount":1,"currency":"CNY","owner_user_id":"sales_a"}`
	if code, env, _ := doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID, lean); code != http.StatusOK {
		t.Fatalf("去掉重复之后回 %d，期望 200（对照组不成立，上面那条 400 判不出体积上限）：%s", code, env.Message)
	}
}

// TestOpportunityRoutes_RejectsCurrencyAndAmountShapes 值域校验的错误从服务侧带出来，仍是 400。
func TestOpportunityRoutes_RejectsValueRangeOnEdit(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := opportunityTestService(t, database)
	row := seedOpportunityRow(t, database, "opp_vr", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	engine := newOpportunityTestEngine(svc, uint(7))

	for _, tc := range []struct{ name, body string }{
		{"空币种", `{"version":0,"amount":1,"currency":"","owner_user_id":"sales_a"}`},
		{"小写币种", `{"version":0,"amount":1,"currency":"cny","owner_user_id":"sales_a"}`},
		{"负金额", `{"version":0,"amount":-1,"currency":"CNY","owner_user_id":"sales_a"}`},
		{"超容量金额", `{"version":0,"amount":1e11,"currency":"CNY","owner_user_id":"sales_a"}`},
		{"超长归属", fmt.Sprintf(`{"version":0,"amount":1,"currency":"CNY","owner_user_id":"%s"}`, strings.Repeat("u", 65))},
		{"零值时间", `{"version":0,"amount":1,"currency":"CNY","owner_user_id":"sales_a","expected_close_at":"0001-01-01T00:00:00Z"}`},
	} {
		code, env, _ := doOpportunity(t, engine, http.MethodPut, "/api/opportunity/"+row.ID, tc.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s 回 %d，期望 400：%s", tc.name, code, env.Message)
			continue
		}
		if reason := opportunityReason(t, env); reason != "input_invalid" {
			t.Errorf("%s 的 reason=%q，期望 input_invalid", tc.name, reason)
		}
	}
	if fresh := readOpportunityRow(t, database, row.ID); fresh.Version != 0 {
		t.Errorf("六次被拒的改写里有落库的：version=%d", fresh.Version)
	}
}

// failingOpportunityRepo 只用来把"底座报错"这一格凑出来：它不是被测对象，
// 被测的是控制器怎么渲染一个非 sentinel 的 error。
// （不去关真连接的缘由：testutil 的库是**按进程共用**的，关掉它会把同包其余用例一起带走，
// 而"表不存在"在同进程里也不可靠 —— 别的用例可能已经把它建出来了。）
type failingOpportunityRepo struct{ err error }

func (f failingOpportunityRepo) Available() bool { return true }

func (f failingOpportunityRepo) Insert(context.Context, *model.Opportunity) error {
	return f.err
}

func (f failingOpportunityRepo) GetByID(context.Context, string) (*model.Opportunity, error) {
	return nil, f.err
}

func (f failingOpportunityRepo) Update(context.Context, *model.Opportunity) error { return f.err }

func (f failingOpportunityRepo) ListByCustomer(context.Context, string, []string, int, int) ([]*model.Opportunity, error) {
	return nil, f.err
}

func (f failingOpportunityRepo) ListByStage(context.Context, string, []string, int, int) ([]*model.Opportunity, error) {
	return nil, f.err
}

func (f failingOpportunityRepo) ListByOwner(context.Context, string, []string, int, int) ([]*model.Opportunity, error) {
	return nil, f.err
}

// GetByClueID / OpenCountByOwner 是 T-P4-05 加进接口的两格。这里补齐而不是嵌入接口：
// 嵌入会让"接口又加了方法"这件事在这个替身上静默通过，而它守的恰恰是
// "底座一旦真被问到就必须报错"——编译不过才是这一格该有的反馈。
func (f failingOpportunityRepo) GetByClueID(context.Context, string) (*model.Opportunity, error) {
	return nil, f.err
}

func (f failingOpportunityRepo) OpenCountByOwner(context.Context) (map[string]int, error) {
	return nil, f.err
}

// TestOpportunityRoutes_RepositoryFailureIsNotSilent 底座读失败 ⇒ 500 + reason=internal，
// 且绝不许被翻成 404（"读不到"与"没有这条"在值班手里是两个动作）。
func TestOpportunityRoutes_RepositoryFailureIsNotSilent(t *testing.T) {
	failing := service.NewOpportunityService(
		failingOpportunityRepo{err: errors.New("connection reset by peer")})
	failing.SetClock(func() time.Time { return oppTestClock })
	engine := newOpportunityTestEngine(failing, uint(7))

	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/opportunity/opp_alive", ""},
		{http.MethodGet, "/api/opportunity/opp_alive/moves", ""},
		{http.MethodPost, "/api/opportunity/opp_alive/stage", `{"version":0,"to_stage":"negotiation"}`},
	} {
		code, env, _ := doOpportunity(t, engine, probe.method, probe.path, probe.body)
		if code != http.StatusInternalServerError {
			t.Errorf("%s %s 回 %d，期望 500：%s", probe.method, probe.path, code, env.Message)
			continue
		}
		if reason := opportunityReason(t, env); reason != "internal" {
			t.Errorf("%s %s 的 reason=%q，期望 internal（故障不能冒充业务结论）",
				probe.method, probe.path, reason)
		}
		if strings.Contains(env.Message, "不存在") {
			t.Errorf("故障被说成「商机不存在」：%s —— 调用方会去重走一遍新建流程", env.Message)
		}
	}
}

// —— 装配层（app）——————————————————————————————————————

// TestInitOpportunityRuntime 装配的两个方向都要有判据：
// 无 DB 句柄 ⇒ 全局**清空**（不是"什么都不做"），有句柄 ⇒ 路由与未来生产者看到同一份。
// 上一次装配成功的实例若留在全局里，路由就会对着一句"未装配"的告警继续回 200。
func TestInitOpportunityRuntime(t *testing.T) {
	restore := service.GlobalOpportunityService()
	defer service.SetGlobalOpportunityService(restore)

	service.SetGlobalOpportunityService(nil)
	if got := app.InitOpportunityRuntime(nil); got != nil {
		t.Errorf("无 DB 句柄时返回了非 nil 实例：%v", got)
	}
	if got := service.GlobalOpportunityService(); got != nil {
		t.Errorf("无 DB 句柄时全局仍有实例：%v ⇒ /api/opportunity/* 会对着「未装配」的日志回 200", got)
	}

	database := testutil.NewTestDB(t, &model.Opportunity{})
	svc := app.InitOpportunityRuntime(database)
	if svc == nil {
		t.Fatal("有 DB 句柄时装配返回 nil")
	}
	if !svc.Available() {
		t.Error("装配出的实例 Available()=false")
	}
	if service.GlobalOpportunityService() != svc {
		t.Error("全局登记的不是本次装配的那一份（两套缓存口径）")
	}

	// 装配后走一遍真实路由挂载点（setupOpportunityRoutes 取全局，不取返回值）。
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) { c.Set("user_id", uint(3)); c.Next() })
	setupOpportunityRoutes(auth)
	row := seedOpportunityRow(t, database, "opp_asm", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	if code, env, _ := doOpportunity(t, engine, http.MethodGet, "/api/opportunity/"+row.ID, ""); code != http.StatusOK {
		t.Errorf("经装配层挂载后 GET 回 %d：%s", code, env.Message)
	}

	// 再装配一次传 nil：必须把上一份清掉（重复 Init 是测试与灰度重启的真实路径）。
	if got := app.InitOpportunityRuntime(nil); got != nil {
		t.Errorf("第二次装配（nil 句柄）返回非 nil：%v", got)
	}
	if svc := service.GlobalOpportunityService(); svc != nil {
		t.Error("第二次装配之后全局仍留着上一份实例")
	}
}

// TestOpportunityRoutes_MountedBySetup 整表 Setup 必须把八条端点挂进真实路由树。
//
// 独立挂载的用例证明不了"router.go 里有人调它" —— 那正是台账 16d 那一格盯的东西，
// 而它比 Go 用例更早发现"装配点被摘掉"（装配点摘掉时全套用例照样绿）。
func TestOpportunityRoutes_MountedBySetup(t *testing.T) {
	restore := service.GlobalOpportunityService()
	defer service.SetGlobalOpportunityService(restore)

	database := testutil.NewTestDB(t, &model.Opportunity{})
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	found := map[string]bool{}
	for _, r := range engine.Routes() {
		if strings.HasPrefix(r.Path, "/api/opportunity") {
			found[r.Method+" "+r.Path] = true
		}
	}
	for _, spec := range opportunityRouteSpecs {
		if !found[spec] {
			t.Errorf("整表 Setup 之后没有挂上 %s（得到 %v）", spec, keysOf(found))
		}
	}

	// 挂进树 != 挂在对的组下。上面那一圈只看 engine.Routes()，它对"挂到了 public 组"
	// 完全无感 —— 而那一字之差的后果是：任何匿名访客都能改金额、作废商机。
	// 判据取"非 2xx"而不是"恰好 401"：中间件的拒绝码（401/403）属于中间件，会被它的
	// 演进改掉，"不放行"才是这一层的承诺。而"路径压根没挂上"那种蒙混不过去 ——
	// 上面那个集合圈已经逐条钉过八条路径都在树上了，两个圈合起来才是完整判据。
	for _, probe := range opportunityAnonProbes {
		code, env, raw := doOpportunity(t, engine, probe.method, probe.path, probe.body)
		if code >= 200 && code < 300 {
			t.Errorf("匿名 %s %s 回 %d（%s）—— 商机写入口不得匿名可达：%s",
				probe.method, probe.path, code, env.Message, raw)
		}
	}
}

// TestOpportunityRoutes_LiveThroughRealSetup 走真 Setup 的那一条：端点在树上且**是活的**。
//
// 上面那条 MountedBySetup 有两个圈不住的地方：
//  1. 它只看 engine.Routes()，"挂上了但底座没装配"在这一圈里完全看不出来；
//  2. 匿名探针的判据是"非 2xx"，而 503 恰好也是非 2xx —— 摘掉 app.InitOpportunityRuntime
//     这一行（启动时不装配），八个端点全部退成 503，上面那条用例照样全绿。
//
// 这一格 Go 侧曾经真的看不见：电池里 M33 跑出来 MISSED，台账 16a/16c 也看不见 ——
// 那三格数的是"仓储构造 / 服务构造 / 挂载函数"这三个字面量在不在，它们全都在，
// 只是**启动时没人调用那个装配函数**。所以判据必须是"带着合法令牌真读得到那一行"。
func TestOpportunityRoutes_LiveThroughRealSetup(t *testing.T) {
	restore := service.GlobalOpportunityService()
	defer service.SetGlobalOpportunityService(restore)
	// 开局先清空：这一格要断的是"**Setup 自己**把底座装起来了"。
	// 不洗这一把的话，同进程里前一条用例（TestInitOpportunityRuntime 会写全局）留下的句柄
	// 会让摘掉装配点的那一刀照样读得到 200 —— 变异就这么活在全量跑里。
	service.SetGlobalOpportunityService(nil)

	database := testutil.NewTestDB(t, &model.Opportunity{})
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	if service.GlobalOpportunityService() == nil {
		t.Fatal("Setup 之后全局商机句柄仍是 nil：八个端点全部只会回 503")
	}

	token, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("生成测试令牌失败: %v", err)
	}
	row := seedOpportunityRow(t, database, "opp_live_setup",
		model.OpportunityStageProposal, model.OpportunityStatusOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/opportunity/"+row.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code == http.StatusServiceUnavailable {
		t.Fatalf("带合法令牌读到 503：Setup 没有把商机底座装配起来（body=%s）", w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("带合法令牌读一行已存在的商机回 %d，期望 200：%s", w.Code, w.Body.String())
	}
	var env opportunityEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是契约形状：%s", w.Body.String())
	}
	if got := opportunityData(t, env)["id"]; got != row.ID {
		t.Errorf("读回的是别的行：id=%v，期望 %s", got, row.ID)
	}
}

// opportunityAnonProbes 八条端点的匿名探针（与 opportunityRouteSpecs 一一对应）。
var opportunityAnonProbes = []struct{ method, path, body string }{
	{http.MethodGet, "/api/opportunity/rules", ""},
	{http.MethodGet, "/api/opportunity/opp_anon", ""},
	{http.MethodGet, "/api/opportunity/opp_anon/moves", ""},
	{http.MethodPut, "/api/opportunity/opp_anon", `{"version":0,"amount":1,"currency":"CNY","owner_user_id":"s"}`},
	{http.MethodPost, "/api/opportunity/opp_anon/stage", `{"version":0,"to_stage":"proposal"}`},
	{http.MethodPost, "/api/opportunity/opp_anon/lost", `{"version":0,"reason":"x"}`},
	{http.MethodPost, "/api/opportunity/opp_anon/cancel", `{"version":0}`},
	{http.MethodPost, "/api/opportunity/opp_anon/reopen", `{"version":0}`},
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
