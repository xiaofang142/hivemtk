// human_task_routes_test.go T-P3-03：待办 API 的入参口径、身份口径与错误状态码。
//
// 全部走**真库 + 真 handler**（只把 JWT 中间件换成"塞一个 user_id"的那一行）：
// 这一层要钉的就是"HTTP 形状"——查询参数怎么翻成过滤条件、没登录怎么答、
// 底座不可用时回 503 还是回空列表。这些都不在 service 用例的覆盖面里，
// 而把它们写成桩对象上的断言只会测到"我写的桩会返回错误"。
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// newHumanTaskTestEngine 挂一条"已登录"的中间件（user_id 是 uint，与 jwt.go 的写入类型一致）。
// as: 传 "" 表示不带身份（复现未登录 / claims 里没 user_id 那一支）。
func newHumanTaskTestEngine(svc *service.HumanTaskService, uid any) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	controller.NewHumanTaskController(svc).RegisterRoutes(auth)
	return engine
}

// setupHumanTaskRoutesDB 建表 + 造服务。**一个用例只调一次**（见下面 List 的注释）。
func setupHumanTaskRoutesDB(t *testing.T) (*service.HumanTaskService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.HumanTask{})
	return service.NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(database), nil), database
}

type humanTaskResp struct {
	Code    any            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func doHumanTask(t *testing.T, h http.Handler, method, path, body string) (int, humanTaskResp) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var out humanTaskResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s 响应不是合法 JSON：%v —— %s", method, path, err, w.Body.String())
	}
	return w.Code, out
}

// submitViaSvc 用服务投一条待办（测试里的造行也走真实写入口：
// 直插库造的行走不到 kind/SLA 的落列，端点测的就不是同一份数据形状）。
func submitViaSvc(t *testing.T, svc *service.HumanTaskService, in service.HumanTaskSubmitInput) *model.HumanTask {
	t.Helper()
	task, created, err := svc.Submit(context.Background(), in)
	if err != nil || !created {
		t.Fatalf("造行失败：(%v,%v)", created, err)
	}
	return task
}

func handoffTaskInput(id string) service.HumanTaskSubmitInput {
	return service.HumanTaskSubmitInput{
		Kind: model.HumanTaskKindConversationHandoff, SubjectType: "customer_session",
		SubjectID: id, Title: "客户请求转人工",
	}
}

// approvalTaskInput 审批类待办：SLA 由投递方给（会话类才由配置代填），
// 这里给一天后的到期时刻 —— 不为绕过校验，而是审批类**必须**带，否则服务侧就该拒。
func approvalTaskInput(id string) service.HumanTaskSubmitInput {
	at := time.Now().Add(24 * time.Hour)
	return service.HumanTaskSubmitInput{
		Kind: model.HumanTaskKindApproval, SubjectType: "approval_request",
		SubjectID: id, Title: "等报价审批", SlaDueAt: &at,
	}
}

// --- 路由表 ------------------------------------------------------------------

// 七条端点都要在表里。少注册一条 = 待办中心有一个按钮点了报 404，
// 而 404 在网关日志里与"路径写错"长得一模一样，没人会想起是装配漏了。
func TestSetupHumanTaskRoutes_AllRegistered(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller.NewHumanTaskController(svc).RegisterRoutes(engine.Group("/api"))

	want := []string{
		"GET /api/human-tasks",
		"GET /api/human-tasks/counts",
		"GET /api/human-tasks/:id",
		"POST /api/human-tasks/:id/claim",
		"POST /api/human-tasks/:id/release",
		"POST /api/human-tasks/:id/complete",
		"POST /api/human-tasks/:id/cancel",
	}
	have := map[string]bool{}
	for _, r := range engine.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, line := range want {
		if !have[line] {
			t.Errorf("路由未注册 %s", line)
		}
	}
}

// --- 列表与聚合 --------------------------------------------------------------

// 条数断言用精确值是有前提的，两条都要知道：
//   - NewTestDB(t, &model.HumanTask{}) 会先 DROP 再 AutoMigrate 这张表 ⇒ 每个用例进来时
//     human_tasks 是空的（本卡实测：仓储/服务两层同法跑绿）；
//   - 所以**同一个用例里不能再调第二次** —— 第二次会把第一个连接刚写下的行连表一起清掉，
//     需要直接落库旁路时用 setup 返回的那个句柄。
func TestHumanTaskAPI_List(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	for i := 0; i < 3; i++ {
		submitViaSvc(t, svc, handoffTaskInput(fmt.Sprintf("sess_api_%d", i)))
	}
	submitViaSvc(t, svc, approvalTaskInput("apr_api_1"))
	h := newHumanTaskTestEngine(svc, uint(42))

	code, body := doHumanTask(t, h, http.MethodGet, "/api/human-tasks", "")
	if code != http.StatusOK {
		t.Fatalf("列表应回 200，实际 %d：%s", code, body.Message)
	}
	if body.Code != float64(0) {
		t.Errorf("code = %v，期望 0（CLAUDE.md 契约：成功是数字 0）", body.Code)
	}
	list, _ := body.Data["list"].([]any)
	if len(list) != 4 {
		t.Errorf("list %d 项，期望 4 项", len(list))
	}
	if total, _ := body.Data["total"].(float64); total != 4 {
		t.Errorf("total = %v，期望 4", body.Data["total"])
	}

	// 按 kind 过滤（AC③）
	code, body = doHumanTask(t, h, http.MethodGet, "/api/human-tasks?kind=approval", "")
	list, _ = body.Data["list"].([]any)
	if code != http.StatusOK || len(list) != 1 {
		t.Errorf("kind=approval 应 200 且 1 项，实际 %d / %d 项", code, len(list))
	}

	// 两类合并（坐席收件箱"会话 + 催收"那种视图）
	code, body = doHumanTask(t, h, http.MethodGet,
		"/api/human-tasks?kind=conversation_handoff&kind=collection_escalation", "")
	list, _ = body.Data["list"].([]any)
	if code != http.StatusOK || len(list) != 3 {
		t.Errorf("两类合并应 3 项，实际 %d / %d 项", code, len(list))
	}

	// 分页：只给 page_size 也要能成立（默认页码 1），且 total 不受页大小影响
	code, body = doHumanTask(t, h, http.MethodGet, "/api/human-tasks?page_size=2", "")
	list, _ = body.Data["list"].([]any)
	total, _ := body.Data["total"].(float64)
	if code != http.StatusOK || len(list) != 2 || total != 4 {
		t.Errorf("page_size=2 应 2 项/total 4，实际 %d / %d 项 / total %v", code, len(list), total)
	}

	// 未知 kind 必须报错，不能回空列表（空列表是一句业务结论）
	if code, _ = doHumanTask(t, h, http.MethodGet, "/api/human-tasks?kind=bogus", ""); code != http.StatusBadRequest {
		t.Errorf("未知 kind 应回 400，实际 %d", code)
	}
}

// AC③ 的"未读数聚合"+ AC④ 的"指标隔离"在 HTTP 出口的样子：
// 三类都要有键（缺键与 0 在 JSON 里读出来不同）、每类回显自己那一档 SLA 列。
func TestHumanTaskAPI_Counts(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	submitViaSvc(t, svc, handoffTaskInput("sess_cnt_1"))
	submitViaSvc(t, svc, approvalTaskInput("apr_cnt_1"))
	h := newHumanTaskTestEngine(svc, uint(42))

	code, body := doHumanTask(t, h, http.MethodGet, "/api/human-tasks/counts", "")
	if code != http.StatusOK {
		t.Fatalf("counts 应 200，实际 %d：%s", code, body.Message)
	}
	byKind, _ := body.Data["by_kind"].([]any)
	if len(byKind) != len(model.HumanTaskKinds) {
		t.Fatalf("by_kind %d 项，期望 %d 项（每类都要有键，缺键与 0 在 JSON 里读起来不同）",
			len(byKind), len(model.HumanTaskKinds))
	}
	if total, _ := body.Data["total_open"].(float64); total != 2 {
		t.Errorf("total_open = %v，期望 2", body.Data["total_open"])
	}
	if at, _ := body.Data["at"].(string); at == "" {
		t.Error("at 必须回显（逾期判据所用的时刻，没有它读数无法复算）")
	}
	wantCols := []string{"sla_first_response_at", "sla_decide_at", "sla_escalate_at"}
	for i, item := range byKind {
		row, _ := item.(map[string]any)
		if row["kind"] != model.HumanTaskKinds[i] {
			t.Errorf("by_kind[%d].kind = %v，期望 %v（顺序即待办中心展示顺序）",
				i, row["kind"], model.HumanTaskKinds[i])
		}
		if row["sla_column"] != wantCols[i] {
			t.Errorf("%v 类回显的 SLA 档 = %v，期望 %v（AC④ 的隔离要在响应里看得见）",
				row["kind"], row["sla_column"], wantCols[i])
		}
	}
}

// 坐席收件箱的主查询：只看落在自己名下的。
// `assignee=me` 必须取登录态身份，而不是让前端把自己的 user id 拼进 URL ——
// 后者会退化成"任何人都能按别人的 id 查别人的待办"，而那是读接口，没人拦。
func TestHumanTaskAPI_ListFilterByAssignee(t *testing.T) {
	svc, database := setupHumanTaskRoutesDB(t)
	mineTask := submitViaSvc(t, svc, handoffTaskInput("sess_own_mine"))
	otherTask := submitViaSvc(t, svc, handoffTaskInput("sess_own_other"))
	h := newHumanTaskTestEngine(svc, uint(42))

	for _, task := range []*model.HumanTask{mineTask, otherTask} {
		if code, _ := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/claim", ""); code != http.StatusOK {
			t.Fatalf("前置条件破了：认领 %s 失败 %d", task.ID, code)
		}
	}
	// 第二件改挂到别人名下（旁路直接落库：service 没有"转让"这个动作）
	if err := database.Model(&model.HumanTask{}).Where("id = ?", otherTask.ID).
		Update("assignee_user_id", "77").Error; err != nil {
		t.Fatalf("改挂他人失败: %v", err)
	}
	// 第三条留作未认领（名下无人），用来钉"me 不该把池子里的算成我的"
	submitViaSvc(t, svc, handoffTaskInput("sess_own_pool"))

	rows := func(query string) map[string]bool {
		t.Helper()
		code, body := doHumanTask(t, h, http.MethodGet, query, "")
		if code != http.StatusOK {
			t.Fatalf("%s 应 200，实际 %d：%s", query, code, body.Message)
		}
		got := map[string]bool{}
		for _, row := range humanTaskRows(t, body.Data["list"]) {
			sid, _ := row["subject_id"].(string)
			got[sid] = true
		}
		return got
	}

	mine := rows("/api/human-tasks?assignee=me")
	if !mine["sess_own_mine"] {
		t.Errorf("assignee=me 应含自己认领的那条，实际 %v", mine)
	}
	for _, notMine := range []string{"sess_own_other", "sess_own_pool"} {
		if mine[notMine] {
			t.Errorf("assignee=me 不该含 %s（一条挂在他人名下、一条还没人认领），实际 %v", notMine, mine)
		}
	}
	other := rows("/api/human-tasks?assignee=77")
	if !other["sess_own_other"] || len(other) != 1 {
		t.Errorf("assignee=77 应只含挂到 77 的那条，实际 %v", other)
	}
}

// humanTaskRows 把 list 解成行数组。
func humanTaskRows(t *testing.T, raw any) []map[string]any {
	t.Helper()
	list, _ := raw.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("list 里出现非对象项 %#v", item)
		}
		out = append(out, row)
	}
	return out
}

// 底座不可用时必须 503，且**不附带任何计数**：
// {"total_open":0} 会被前端读成"人工终于清完了"，而事实是这次压根没读到。
func TestHumanTaskAPI_UnavailableHandleIs503(t *testing.T) {
	svc := service.NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(nil), nil)
	h := newHumanTaskTestEngine(svc, uint(42))

	for _, path := range []string{"/api/human-tasks", "/api/human-tasks/counts"} {
		code, body := doHumanTask(t, h, http.MethodGet, path, "")
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s 应 503，实际 %d", path, code)
		}
		raw, _ := json.Marshal(body)
		for _, forbidden := range []string{"total_open", `"list"`, "by_kind"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("%s 的 503 出口不该带 %s（会被读成一句业务结论）：%s", path, forbidden, raw)
			}
		}
	}
}

// --- 动作 --------------------------------------------------------------------

func TestHumanTaskAPI_ClaimReleaseComplete(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	task := submitViaSvc(t, svc, handoffTaskInput("sess_act_1"))
	h := newHumanTaskTestEngine(svc, uint(42))

	// 未登录：动作类端点必须 401，而不是"操作者记为空串"
	anon := newHumanTaskTestEngine(svc, nil)
	if code, _ := doHumanTask(t, anon, http.MethodPost, "/api/human-tasks/"+task.ID+"/claim", ""); code != http.StatusUnauthorized {
		t.Errorf("匿名认领应 401，实际 %d", code)
	}

	code, body := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/claim", "")
	if code != http.StatusOK {
		t.Fatalf("认领应 200，实际 %d：%s", code, body.Message)
	}
	if status, _ := body.Data["status"].(string); status != model.HumanTaskStatusClaimed {
		t.Errorf("认领后 status = %v，期望 claimed", body.Data["status"])
	}
	if who, _ := body.Data["assignee_user_id"].(string); who != "42" {
		t.Errorf("assignee_user_id = %v，期望 \"42\"（登录态里的 user id，待办按它归人）", body.Data["assignee_user_id"])
	}

	// 同一人重复认领 ⇒ 409（不是 500：这条是并发/重复点击的常态）
	if code, _ = doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/claim", ""); code != http.StatusConflict {
		t.Errorf("重复认领应 409，实际 %d", code)
	}
	if code, body = doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/release", ""); code != http.StatusOK {
		t.Fatalf("释放应 200，实际 %d：%s", code, body.Message)
	}
	if status, _ := body.Data["status"].(string); status != model.HumanTaskStatusPending {
		t.Errorf("释放后 status = %v", body.Data["status"])
	}
	if who, _ := body.Data["assignee_user_id"].(string); who != "" {
		t.Errorf("释放后仍记着 %q（认领人要一起清掉）", who)
	}

	if code, body = doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/complete", ""); code != http.StatusOK {
		t.Fatalf("完成应 200，实际 %d：%s", code, body.Message)
	}
	if status, _ := body.Data["status"].(string); status != model.HumanTaskStatusDone {
		t.Errorf("完成后 status = %v", body.Data["status"])
	}
	// 终态再动一次是 409（"这条已经处理完了"），不是 500
	if code, _ = doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/complete", ""); code != http.StatusConflict {
		t.Errorf("重复完成应 409，实际 %d", code)
	}
}

func TestHumanTaskAPI_CancelNeedsReason(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	task := submitViaSvc(t, svc, handoffTaskInput("sess_cancel_1"))
	h := newHumanTaskTestEngine(svc, uint(42))

	if code, _ := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/cancel", `{"reason":"  "}`); code != http.StatusBadRequest {
		t.Errorf("无因撤销应 400，实际 %d", code)
	}
	if code, _ := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/cancel", `not json`); code != http.StatusBadRequest {
		t.Errorf("非法请求体应 400，实际 %d", code)
	}
	code, body := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+task.ID+"/cancel", `{"reason":"客户已自行解决"}`)
	if code != http.StatusOK {
		t.Fatalf("撤销应 200，实际 %d：%s", code, body.Message)
	}
	if status, _ := body.Data["status"].(string); status != model.HumanTaskStatusCancelled {
		t.Errorf("撤销后 status = %v", body.Data["status"])
	}
	if reason, _ := body.Data["cancel_reason"].(string); reason != "客户已自行解决" {
		t.Errorf("cancel_reason = %v", body.Data["cancel_reason"])
	}
}

// 审批类待办不可认领（C3），API 出口必须是 409 且带一句能照做的答复。
func TestHumanTaskAPI_ApprovalTaskCannotBeClaimed(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	apr := submitViaSvc(t, svc, approvalTaskInput("apr_no_claim"))
	h := newHumanTaskTestEngine(svc, uint(42))

	code, body := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+apr.ID+"/claim", "")
	if code != http.StatusConflict {
		t.Fatalf("审批类认领应 409，实际 %d", code)
	}
	if !strings.Contains(body.Message, model.HumanTaskKindApproval) {
		t.Errorf("提示里应说明是哪一类不可认领：%q", body.Message)
	}
	// 裁决走 complete：这条路是通的
	if code, _ = doHumanTask(t, h, http.MethodPost, "/api/human-tasks/"+apr.ID+"/complete", ""); code != http.StatusOK {
		t.Errorf("审批类完成应 200，实际 %d", code)
	}
}

func TestHumanTaskAPI_MissingTaskIs404(t *testing.T) {
	svc, _ := setupHumanTaskRoutesDB(t)
	h := newHumanTaskTestEngine(svc, uint(42))

	if code, _ := doHumanTask(t, h, http.MethodGet, "/api/human-tasks/ht_absent", ""); code != http.StatusNotFound {
		t.Errorf("读不存在的待办应 404，实际 %d", code)
	}
	if code, _ := doHumanTask(t, h, http.MethodPost, "/api/human-tasks/ht_absent/claim", ""); code != http.StatusNotFound {
		t.Errorf("认领不存在的待办应 404，实际 %d", code)
	}

	// 超长 id 判 400：提示里会回显 id，没有长度上限就等于给调用方一个响应体放大器。
	if code, _ := doHumanTask(t, h, http.MethodGet, "/api/human-tasks/"+strings.Repeat("x", 200), ""); code != http.StatusBadRequest {
		t.Errorf("超长 id 应 400，实际 %d", code)
	}
}

// 全局服务未装配（nil）时路由仍要挂上、且要诚实回 503。
//
// 这条测的是装配顺序：setupHumanTaskRoutes 在 router.go 里取全局实例，一旦哪天
// 有人把 InitHumanTaskRuntime 挪到路由之后，现象会是"每个请求 panic / 500"，
// 而正确的现象是"底座不可用"这句能照着处置的话。
func TestSetupHumanTaskRoutes_NilGlobalStillServes503(t *testing.T) {
	t.Cleanup(func() { service.SetGlobalHumanTaskService(nil) })
	service.SetGlobalHumanTaskService(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	setupHumanTaskRoutes(engine.Group("/api"))

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/human-tasks/counts", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配应 503，实际 %d：%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "by_kind") {
		t.Errorf("503 出口不该带计数结构：%s", w.Body.String())
	}
}
