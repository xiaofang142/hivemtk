// approval_routes_test.go T-P3-04：审批详情与人工裁决端点的 HTTP 口径。
//
// 与 human_task_routes_test.go 同一套跑法（真库 + 真 handler，只把 JWT 那一层换成
// "往上下文里塞 user_id / role"），因为这一层要钉的正是 HTTP 形状：
// 权限落在哪条腿上、裁决之后待办有没有跟着收口、凭证会不会出现在响应里。
//
// 三条判据各自对应一种"没测就会漏"的事故：
//  1. **resume_token 不出现在任何响应体**（⑫ 的边界收口）：泄漏一次就是永久可伪造续跑；
//  2. **裁决 → 待办 done 这条链在 HTTP 上真的通**：服务层用例各自绿，
//     中间断一环（controller 忘了 sink / wiring 没装）就是"批了但池子里那条还在"；
//  3. **403 与 401 分开**：权限中间件在 handler 之前，所以缺 role 只会到 403；
//     有 role 却没有 user_id 才是 401（否则一次裁决会记下空裁决者，
//     而 decided_by 正是"人工放行率"这个数的分母）。
package router

import (
	"context"
	"encoding/json"
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

// setupApprovalRoutesDB 建两张表、造两个服务，并把待办底座接到审批服务上。
//
// 必须一次建两张表：本卡要验的就是"裁决落库那一刻待办收口"这条跨域链，
// 只建 approval_requests 的话 sink 无处可投，链子在最要紧的那一环上被跳过。
func setupApprovalRoutesDB(t *testing.T) (*service.ApprovalRequestService, *service.HumanTaskService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ApprovalRequest{}, &model.HumanTask{})
	apr := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database), nil)
	tasks := service.NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(database), nil)
	apr.SetTaskSink(tasks)
	return apr, tasks, database
}

// newApprovalTestEngine 挂一条"已登录"的中间件。uid/role 传 nil/"" 表示上下文里没有那一项。
func newApprovalTestEngine(svc *service.ApprovalRequestService, uid any, role string) http.Handler {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(c *gin.Context) {
		if uid != nil {
			c.Set("user_id", uid)
		}
		if role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	controller.NewApprovalController(svc).RegisterRoutes(auth)
	return engine
}

type approvalResp struct {
	Code    any            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func doApproval(t *testing.T, h http.Handler, method, path, body string) (int, approvalResp, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	raw := w.Body.String()
	var out approvalResp
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%s %s 响应不是合法 JSON：%v —— %s", method, path, err, raw)
	}
	return w.Code, out, raw
}

// submitApprovalViaSvc 造一条 pending 审批（走真实写入口：直插库造的行没有 resume_token
// 与 expires_at，测的就不是同一份数据形状）。
func submitApprovalViaSvc(t *testing.T, svc *service.ApprovalRequestService, subjectID string) *model.ApprovalRequest {
	t.Helper()
	row, created, err := svc.Submit(context.Background(), service.ApprovalSubmitInput{
		SubjectType: "quote", SubjectID: subjectID, PolicyKey: "quote.send",
	})
	if err != nil || !created {
		t.Fatalf("造审批失败：(created=%v,err=%v)", created, err)
	}
	return row
}

// humanTaskForApproval 取那条审批名下的待办（没有则 nil）。
//
// 状态必须显式给全：List 的默认口径是"未落定"（与 uq_human_task_open 同源），
// 而本函数存在的意义恰恰是证明裁决后那一行**落定了** —— 用默认口径查会拿到
// "查不到"，并把一次正确的收口报成丢行。
func humanTaskForApproval(t *testing.T, tasks *service.HumanTaskService, approvalID string) *model.HumanTask {
	t.Helper()
	list, _, err := tasks.List(context.Background(), service.HumanTaskListQuery{
		Kinds:    []string{model.HumanTaskKindApproval},
		Statuses: model.HumanTaskStatuses,
	})
	if err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	for _, row := range list {
		if row.SubjectType == service.HumanTaskSubjectApprovalRequest && row.SubjectID == approvalID {
			return row
		}
	}
	return nil
}

// --- 路由表 ------------------------------------------------------------------

func TestSetupApprovalRoutes_AllRegistered(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	controller.NewApprovalController(svc).RegisterRoutes(engine.Group("/api"))

	have := map[string]bool{}
	for _, r := range engine.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, line := range []string{"GET /api/approvals/:id", "POST /api/approvals/:id/decide"} {
		if !have[line] {
			t.Errorf("路由未注册 %s", line)
		}
	}
}

// --- 详情 --------------------------------------------------------------------

func TestApprovalAPI_GetDetail(t *testing.T) {
	svc, tasks, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_1")
	// 前置条件：待办真的投出去了（否则下面的链断在别处，本用例会给出误导性的红）
	if humanTaskForApproval(t, tasks, row.ID) == nil {
		t.Fatal("前置条件破了：pending 审批没有对应待办")
	}

	h := newApprovalTestEngine(svc, uint(42), "user") // 读口不判角色（与待办列表同一口径）
	code, body, raw := doApproval(t, h, http.MethodGet, "/api/approvals/"+row.ID, "")
	if code != http.StatusOK {
		t.Fatalf("详情应 200，实际 %d：%s", code, body.Message)
	}
	if body.Data["id"] != row.ID || body.Data["status"] != model.ApprovalStatusPending {
		t.Errorf("详情形状不对：%v", body.Data)
	}
	if body.Data["subject_id"] != "q_http_1" || body.Data["policy_key"] != "quote.send" {
		t.Errorf("待办中心要看的两个字段不对：%v", body.Data)
	}
	// 状态机由服务端回显，前端不再自己抄一份 pending→能点什么。
	// 三个目标态是对的：expired 也在其中，因为 pending 确实会被清扫翻成它 ——
	// 这一列回答的是"这一行还能变成什么"，不是"人能点哪两个按钮"（后者是前者的子集，
	// 前端按 approved/rejected 两个名字筛，筛不出的那个就没有按钮）。
	targets, _ := body.Data["allowed_transitions"].([]any)
	if len(targets) != 3 {
		t.Errorf("pending 的 allowed_transitions = %v，期望 approved/rejected/expired 三个目标态",
			body.Data["allowed_transitions"])
	}

	// 凭证不外泄：值与字段名都不能出现在响应里
	if row.ResumeToken == "" {
		t.Fatal("前置条件破了：pending 审批没有恢复凭证")
	}
	if strings.Contains(raw, row.ResumeToken) || strings.Contains(raw, "resume_token") {
		t.Errorf("响应里出现了恢复凭证（⑫ 的边界失守）：%s", raw)
	}
}

// 读口不判角色：待办池本来就全池可见（否则坐席看不到自己名下那条审批的对象是什么），
// 这条钉的是"别顺手把 manager 门槛加到 GET 上"。
func TestApprovalAPI_GetNeedsNoManagerRole(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_role")
	for _, role := range []string{"user", "agent", "manager", "admin"} {
		h := newApprovalTestEngine(svc, uint(7), role)
		if code, body, _ := doApproval(t, h, http.MethodGet, "/api/approvals/"+row.ID, ""); code != http.StatusOK {
			t.Errorf("role=%s 读详情应 200，实际 %d：%s", role, code, body.Message)
		}
	}
}

func TestApprovalAPI_GetMissingIs404(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	h := newApprovalTestEngine(svc, uint(42), "user")

	if code, _, _ := doApproval(t, h, http.MethodGet, "/api/approvals/apr_absent", ""); code != http.StatusNotFound {
		t.Errorf("读不存在的审批应 404，实际 %d", code)
	}
	// 超长 id 判 400：提示里会回显 id（同 human_task 那一层的放大器判据）
	if code, _, _ := doApproval(t, h, http.MethodGet, "/api/approvals/"+strings.Repeat("x", 200), ""); code != http.StatusBadRequest {
		t.Errorf("超长 id 应 400，实际 %d", code)
	}
}

// --- 裁决 --------------------------------------------------------------------

// AC① 的"审"：HTTP 批准 ⇒ 审批行 approved + 裁决者是登录态那个人 + 待办收口成 done。
func TestApprovalAPI_DecideApproveClosesTask(t *testing.T) {
	svc, tasks, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_ok")
	h := newApprovalTestEngine(svc, uint(42), "manager")

	code, body, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"approved","note":"金额在授权范围内"}`)
	if code != http.StatusOK {
		t.Fatalf("批准应 200，实际 %d：%s", code, body.Message)
	}
	if body.Data["status"] != model.ApprovalStatusApproved {
		t.Errorf("批准后 status = %v", body.Data["status"])
	}
	if body.Data["decided_by"] != "42" {
		t.Errorf("decided_by = %v，期望 \"42\"（登录态里的 user id：放行率按它归人）", body.Data["decided_by"])
	}
	if body.Data["decision_note"] != "金额在授权范围内" {
		t.Errorf("decision_note = %v", body.Data["decision_note"])
	}
	// 跨域链：待办必须已经不在池子里（HTTP 层唯一能证明 sink 装上了的地方）
	task := humanTaskForApproval(t, tasks, row.ID)
	if task == nil {
		t.Fatal("裁决后找不到那条待办（连行都没了，收口判定无从谈起）")
	}
	if task.Status != model.HumanTaskStatusDone {
		t.Errorf("待办 status = %s，期望 done（裁决已落定而池子里那条还在 = 按钮还会被点）", task.Status)
	}
	if task.AssigneeUserID != "42" {
		t.Errorf("待办完成人 = %q，期望 42", task.AssigneeUserID)
	}
}

func TestApprovalAPI_DecideReject(t *testing.T) {
	svc, tasks, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_no")
	h := newApprovalTestEngine(svc, uint(42), "admin")

	code, body, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"rejected","note":"折扣越权"}`)
	if code != http.StatusOK {
		t.Fatalf("驳回应 200，实际 %d：%s", code, body.Message)
	}
	if body.Data["status"] != model.ApprovalStatusRejected {
		t.Errorf("驳回后 status = %v", body.Data["status"])
	}
	if task := humanTaskForApproval(t, tasks, row.ID); task == nil || task.Status != model.HumanTaskStatusDone {
		t.Errorf("驳回后待办没收口：%+v", task)
	}
}

// 重复裁决：409 且**带上当前那一行**。前端拿到 409 要能立刻回答"那到底批了没有"，
// 否则它会去刷列表，而列表读的是待办池——那一行可能已经不在池子里了。
func TestApprovalAPI_DecideTwiceIs409WithCurrentRow(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_twice")
	first := newApprovalTestEngine(svc, uint(42), "manager")
	if code, _, _ := doApproval(t, first, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"approved"}`); code != http.StatusOK {
		t.Fatalf("前置条件破了：首次裁决没成功 %d", code)
	}

	second := newApprovalTestEngine(svc, uint(43), "manager")
	code, body, raw := doApproval(t, second, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"rejected"}`)
	if code != http.StatusConflict {
		t.Fatalf("重复裁决应 409，实际 %d：%s", code, body.Message)
	}
	if body.Data == nil || body.Data["status"] != model.ApprovalStatusApproved || body.Data["decided_by"] != "42" {
		t.Errorf("409 应带回当前行（别人批过了要说得出是谁批的）：%v", body.Data)
	}
	if strings.Contains(raw, row.ResumeToken) {
		t.Errorf("409 的回带行里出现了凭证：%s", raw)
	}
}

// 权限与身份：缺 role ⇒ 403（中间件在 handler 之前），有 role 无 user_id ⇒ 401。
func TestApprovalAPI_DecidePermissionAndIdentity(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_perm")

	cases := []struct {
		name string
		uid  any
		role string
		want int
	}{
		{"无 role（未鉴权到角色）", uint(42), "", http.StatusForbidden},
		{"role=user（坐席无裁决权）", uint(42), "user", http.StatusForbidden},
		{"有 role 无 user_id", nil, "manager", http.StatusUnauthorized},
		{"user_id=0", uint(0), "manager", http.StatusUnauthorized},
		{"manager", uint(42), "manager", http.StatusOK},
	}
	for _, c := range cases {
		h := newApprovalTestEngine(svc, c.uid, c.role)
		code, body, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
			`{"verdict":"approved"}`)
		if code != c.want {
			t.Errorf("%s 应 %d，实际 %d：%s", c.name, c.want, code, body.Message)
		}
	}
}

func TestApprovalAPI_DecideValidatesVerdict(t *testing.T) {
	svc, _, _ := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_verify")
	h := newApprovalTestEngine(svc, uint(42), "manager")

	for _, body := range []string{`{}`, `{"verdict":"maybe"}`, `{"verdict":"approve"}`, `not json`, `{"verdict":100}`} {
		code, resp, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide", body)
		if code != http.StatusBadRequest {
			t.Errorf("请求体 %s 应 400，实际 %d：%s", body, code, resp.Message)
		}
	}
	// 上面那些都没改动作废：行还在等人
	cur, err := svc.Get(context.Background(), row.ID)
	if err != nil || cur == nil || cur.Status != model.ApprovalStatusPending {
		t.Fatalf("非法裁决把行改掉了：(%+v) %v", cur, err)
	}
}

// 已落定的审批（auto-approve 那一档）没有待办，也不该能被裁决。
func TestApprovalAPI_DecideOnDecidedRowIs409(t *testing.T) {
	svc, _, database := setupApprovalRoutesDB(t)
	// 旁路直接落一条 approved：auto-approve 走的是 policy，本用例不想拖进策略桩
	row := submitApprovalViaSvc(t, svc, "q_http_done")
	if _, err := svc.Decide(context.Background(), row.ID, service.ApprovalApprove, "99", ""); err != nil {
		t.Fatalf("前置条件破了: %v", err)
	}
	_ = database

	h := newApprovalTestEngine(svc, uint(42), "manager")
	code, body, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"rejected"}`)
	if code != http.StatusConflict {
		t.Errorf("已批准的再驳应 409，实际 %d：%s", code, body.Message)
	}
}

// --- 截止已过、清扫未到（那扇时间窗）------------------------------------------

// pushApprovalDeadline 把一行 pending 审批的 expires_at 改到指定时刻。
//
// 为什么不靠"等一会儿"或"注入时钟"造这一刻：HTTP 这一层跑的是真服务真库，
// 服务侧已有冻结时钟的用例覆盖判定本身；本层要钉的是**这一格数据形状真的能从
// 读端点和写端点分别拿到什么答复**。而"已过截止而仍是 pending"在生产里每个轮次
// 都存在（TTL 到期到清扫器跑上来之间最多一整轮，本模块实测 5 分钟），
// 把它直接写进行里就是在复现那个窗口，不是在伪造状态。
func pushApprovalDeadline(t *testing.T, database *gorm.DB, id string, at time.Time) {
	t.Helper()
	if err := database.Model(&model.ApprovalRequest{}).
		Where("id = ? AND status = ?", id, model.ApprovalStatusPending).
		UpdateColumn("expires_at", at).Error; err != nil {
		t.Fatalf("挪动 expires_at 失败：%v", err)
	}
}

// 详情里的两份判断必须能分开：allowed_transitions 答"这个状态去得了哪些态"，
// decidable_by_human 答"人现在还有没有机会去落其中一个"。
// 窗口里那一格就是它们唯一分开的地方（状态照旧 pending、三个目标照单全列、口已关）。
func TestApprovalAPI_GetDecidableByHumanSeparatesFromTransitions(t *testing.T) {
	svc, _, database := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_window")
	h := newApprovalTestEngine(svc, uint(42), "user")

	_, before, _ := doApproval(t, h, http.MethodGet, "/api/approvals/"+row.ID, "")
	if before.Data["decidable_by_human"] != true {
		t.Fatalf("未到期应可人工裁决，实际 %v", before.Data["decidable_by_human"])
	}

	pushApprovalDeadline(t, database, row.ID, time.Now().Add(-time.Minute))
	_, inside, _ := doApproval(t, h, http.MethodGet, "/api/approvals/"+row.ID, "")
	if inside.Data["status"] != model.ApprovalStatusPending {
		t.Fatalf("前置断了：窗口里那一行该还是 pending，实际 %v", inside.Data["status"])
	}
	if inside.Data["decidable_by_human"] != false {
		t.Errorf("已过截止的 pending 行仍报「可人工裁决」（前端据此会把按钮亮着）：%v", inside.Data)
	}
	// 同一行上两个答案相反，才是这一格存在的意义：若两者总是同进同退，
	// 前端直接读 allowed_transitions 就够了，这个字段是白加的。
	if targets, _ := inside.Data["allowed_transitions"].([]any); len(targets) != 3 {
		t.Errorf("窗口里的 allowed_transitions = %v，期望状态机照旧给三个目标态", inside.Data["allowed_transitions"])
	}

	// 清扫落定后仍是 false：证明这个字段读的是"截止"而不是"状态 != pending"的别名 ——
	// 后者在窗口里会答 true，于是这一格就只是状态的第二份抄本。
	if flipped, err := svc.ExpireOverdue(context.Background(), 10); err != nil || len(flipped) != 1 {
		t.Fatalf("前置断了：清扫没翻走这一行 (%d,%v)", len(flipped), err)
	}
	_, after, _ := doApproval(t, h, http.MethodGet, "/api/approvals/"+row.ID, "")
	if after.Data["status"] != model.ApprovalStatusExpired || after.Data["decidable_by_human"] != false {
		t.Errorf("清扫后应 expired + 不可人工裁决，实际 %v", after.Data)
	}
}

// 窗口里点"批准"：409、文案说清是时间到点而不是别人抢先，且**库里不留那条不存在的决定**。
func TestApprovalAPI_DecidePastDeadlineIs409AndWritesNothing(t *testing.T) {
	svc, tasks, database := setupApprovalRoutesDB(t)
	row := submitApprovalViaSvc(t, svc, "q_http_late")
	pushApprovalDeadline(t, database, row.ID, time.Now().Add(-time.Minute))

	h := newApprovalTestEngine(svc, uint(42), "manager")
	code, body, raw := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"approved","note":"想起来还有一单没批"}`)
	if code != http.StatusConflict {
		t.Fatalf("过截止的裁决应 409，实际 %d：%s", code, body.Message)
	}
	// 两个 sentinel 的文案不许塌成一句：读到"已由他人裁决"的人会去查是谁批的，
	// 而真答案是"没人批过，时间到点了"。
	if !strings.Contains(body.Message, "挂起时限") || strings.Contains(body.Message, "已由他人裁决") {
		t.Errorf("409 文案没分清超时与抢先：%q", body.Message)
	}
	if body.Data["status"] != model.ApprovalStatusPending || body.Data["decidable_by_human"] != false {
		t.Errorf("409 应带回当前行并给出「这扇门已关」的读数：%v", body.Data)
	}
	if strings.Contains(raw, row.ResumeToken) {
		t.Errorf("409 回带行里出现了凭证：%s", raw)
	}

	// 落库侧：一个字的裁决痕迹都不许留下。这条才是本卡真正的账：
	// decided_by/decided_at 一旦写上，事后复盘读到的是"这个人在截止后批了它"。
	cur, err := svc.Get(context.Background(), row.ID)
	if err != nil || cur == nil {
		t.Fatalf("回读失败：(%v,%v)", cur, err)
	}
	if cur.Status != model.ApprovalStatusPending || cur.DecidedBy != "" || cur.DecidedAt != nil {
		t.Errorf("被拒的裁决在库里留下了痕迹：status=%s decided_by=%q decided_at=%v",
			cur.Status, cur.DecidedBy, cur.DecidedAt)
	}
	// 待办也不该被收掉：这件事仍然悬着，等清扫器把它落成"没人处理"。
	if task := humanTaskForApproval(t, tasks, row.ID); task == nil ||
		task.Status != model.HumanTaskStatusPending {
		t.Errorf("裁决被拒后待办不该落定：%+v", task)
	}

	// 反向控制：把截止推回未来，同一个 handler 同一个人立刻就能批。
	// 少了这一格，上面的 409 也可能来自"直接 UPDATE 把行改坏了"而不是截止判定。
	pushApprovalDeadline(t, database, row.ID, time.Now().Add(time.Hour))
	if code, body, _ := doApproval(t, h, http.MethodPost, "/api/approvals/"+row.ID+"/decide",
		`{"verdict":"approved"}`); code != http.StatusOK {
		t.Fatalf("未到期时同一条路应 200（否则上面的红因不是截止判定）：%d %s", code, body.Message)
	}
}

// --- 关闸 --------------------------------------------------------------------

// 底座不可用（db 句柄为 nil）时 503 且不回数据：
// 与 human_task 同一口径 —— "没读到"不能伪装成"读到的答案是空"。
func TestApprovalAPI_UnavailableHandleIs503(t *testing.T) {
	svc := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(nil), nil)
	h := newApprovalTestEngine(svc, uint(42), "manager")

	code, body, raw := doApproval(t, h, http.MethodGet, "/api/approvals/apr_1", "")
	if code != http.StatusServiceUnavailable {
		t.Errorf("无底座应 503，实际 %d：%s", code, body.Message)
	}
	if strings.Contains(raw, "status") || strings.Contains(raw, "pending") {
		t.Errorf("503 出口不该带审批形状：%s", raw)
	}
	if code, _, _ := doApproval(t, h, http.MethodPost, "/api/approvals/apr_1/decide",
		`{"verdict":"approved"}`); code != http.StatusServiceUnavailable {
		t.Errorf("无底座裁决应 503，实际 %d", code)
	}
}

// 全局实例未登记（旗子 off / 无 DB）时路由仍挂、每个请求诚实回 503。
// 测的是装配顺序而不是 happy path：InitApprovalRuntime 在路由之前跑，off 档必须清成 nil。
func TestSetupApprovalRoutes_NilGlobalStillServes503(t *testing.T) {
	prev := service.GlobalApprovalRequestService()
	t.Cleanup(func() { service.SetGlobalApprovalRequestService(prev) })
	service.SetGlobalApprovalRequestService(nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	setupApprovalRoutes(engine.Group("/api"))

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/approvals/apr_any", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配应 503，实际 %d：%s", w.Code, w.Body.String())
	}
}
