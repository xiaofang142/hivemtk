package controller

// 参数绑定出口的三条契约（每条都是在真实入口上撞出来的，不是推演）：
//
//	步骤数组必须逐元素校验。validator 对 slice 只跑数组级规则，元素规则要显式 dive 才执行；
//	少 dive 时 StepItem 上的 action/direction/assert_kind/query_kind 三个 oneof 和 retry_* 区间
//	一条都不生效——`{"steps":[{"action":"rm_rf"}]}` 会 200 建单、200 发布，直到执行期才在
//	dispatchStep 的 default 上失败，库里留下一行注定红的步骤。
//
//	表达式非法是用户输入，不是服务端故障。CronService.Update 若把 ValidateCronExpr 的结论
//	裸返回，就掉进 baErrToResponse 的 default ⇒ 500 INTERNAL_ERROR_6002；PUT
//	`{"cron_expr":"bad expr here"}` 实测曾返回 500。
//
//	绑定失败的回显只说得出对外字段名。直接回显 validator/json 原文会让用户拿到
//	"Key: 'CreateBrowserTaskReq.Steps' Error:Field validation ..."——既看不懂，又把 Go
//	结构体名和 tag 原文画给了外部。
//
// 每条都验两侧：文案要仍点得出字段（可用性），又不许出现 Go 结构体名与 tag 原文（泄露面）；
// 拒外侧的同时必须放过内侧，否则一条恒真的 400 锁比没有锁更会骗人。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils"
)

// leakBlacklist 内部细节黑名单：任何一条出现在面向用户的文案里都算回归。
var leakBlacklist = []string{"CreateBrowserTaskReq", "UpdateBrowserTaskReq", "CreateCronReq",
	"Field validation", "Key:", "validator", "json: cannot unmarshal"}

func assertNoInternalLeak(t *testing.T, msg string) {
	t.Helper()
	for _, s := range leakBlacklist {
		if strings.Contains(msg, s) {
			t.Errorf("文案泄露内部细节（含 %q）：%q", s, msg)
		}
	}
}

func taskBodyJSON(name, steps string) string {
	return fmt.Sprintf(`{"name":%q,"task_type":"one_shot","url":"https://example.com","steps":%s}`, name, steps)
}

// ---- B1：步内规则真的在入口生效 ----

func TestUnknownStepActionIsRejectedAtCreateAndUpdate(t *testing.T) {
	c, db := newB19eCtrl(t)
	for name, bad := range map[string]string{
		"未知动作":       `[{"action":"rm_rf"}]`,
		"动作缺失":       `[{"target":"x"}]`,
		"滚动方向不合法":    `[{"action":"scroll","direction":"sideways"}]`,
		"断言子类型不合法":   `[{"action":"assert","assert_kind":"regex"}]`,
		"重试次数越界":     `[{"action":"wait","retry_count":999}]`,
		"第 2 步里的坏动作": `[{"action":"wait"},{"action":"hover"}]`,
	} {
		code, body := b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks",
			taskBodyJSON("契约-bad", bad), "", b19eOwner)
		if code != http.StatusBadRequest || body.Code != string(utils.ErrorCodeInvalidParameter) {
			t.Errorf("%s → HTTP %d code=%v want 400/%s（msg=%q）", name, code, body.Code,
				utils.ErrorCodeInvalidParameter, body.Message)
			continue
		}
		if !strings.Contains(body.Message, "steps") {
			t.Errorf("%s 的文案得点出是 steps 这一列：%q", name, body.Message)
		}
		assertNoInternalLeak(t, body.Message)
	}

	// 内侧：平台预设模板的形状必须照常存得下（dive 加过头就是把用户的编排判死）
	if code, body := b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks",
		taskBodyJSON("契约-preset", `[{"action":"open_tab"},{"action":"wait","ms":4000},
			{"action":"extract","selectors":{"titles":"h1"}},{"action":"screenshot"}]`),
		"", b19eOwner); code != http.StatusOK {
		t.Errorf("合法编排被拒 → HTTP %d msg=%q", code, body.Message)
	}

	// 编辑路径同口径：两个 tag 是两处字面量，漏改任意一处都得有点名它的腿
	var seeded model.BrowserTask
	if err := db.Model(&model.BrowserTask{}).Where("user_id = ? AND name = ?", b19eOwner, "契约-preset").
		First(&seeded).Error; err != nil {
		t.Fatalf("内侧那条合法编排没落库，编辑腿无从判定：%v", err)
	}
	idParam := fmt.Sprint(seeded.ID)
	code, body := b19cCall(t, c.Update, http.MethodPut, "/api/browser-automation/tasks/"+idParam,
		taskBodyJSON("契约-bad-edit", `[{"action":"download_file"}]`), idParam, b19eOwner)
	if code != http.StatusBadRequest || body.Code != string(utils.ErrorCodeInvalidParameter) {
		t.Errorf("编辑提交未知动作 → HTTP %d code=%v want 400/%s（msg=%q）",
			code, body.Code, utils.ErrorCodeInvalidParameter, body.Message)
	}
	assertNoInternalLeak(t, body.Message)

	var after model.BrowserTask
	if err := db.WithContext(context.Background()).Where("id = ?", seeded.ID).First(&after).Error; err != nil {
		t.Fatalf("读回任务失败：%v", err)
	}
	var items []dto.StepItem
	if err := json.Unmarshal(after.Steps, &items); err != nil {
		t.Fatalf("被拒后 steps 解析失败：%v", err)
	}
	if len(items) != 4 {
		t.Errorf("被拒的编辑把 steps 换成了 %d 步（原样应为内侧那 4 步）", len(items))
	}
	for _, it := range items {
		if !dto.IsKnownStepAction(it.Action) {
			t.Errorf("被拒的坏动作 %q 落库了", it.Action)
		}
	}
}

// 被拒的请求一行都不许留下：B1 的失效形态正是「200 + 一条注定红的步骤行」，
// 只验状态码不验落库，等于允许它换成「200 但没落库」这种更难发现的形态。
func TestRejectedTaskPersistsNothing(t *testing.T) {
	c, db := newB19eCtrl(t)
	code, _ := b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks",
		taskBodyJSON("契约-no-persist", `[{"action":"rm_rf"}]`), "", b19eOwner)
	if code != http.StatusBadRequest {
		t.Fatalf("先决条件坏了：未知动作没被拒（HTTP %d）", code)
	}
	var n int64
	if err := db.Model(&model.BrowserTask{}).Where("user_id = ? AND name = ?", b19eOwner, "契约-no-persist").
		Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 0 {
		t.Errorf("被拒的请求仍落了 %d 行任务", n)
	}
}

// ---- 文案形状：只报对外字段名，不画内部结构 ----

func TestBindingErrorsUseAPIFieldNames(t *testing.T) {
	c, _ := newB19eCtrl(t)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"缺 name", `{"task_type":"one_shot","url":"https://example.com"}`, "name 为必填项"},
		{"task_type 不在枚举", `{"name":"契约-t","task_type":"every_day","url":"https://example.com"}`, "task_type 取值需为"},
		{"url 超长", fmt.Sprintf(`{"name":"契约-u","task_type":"one_shot","url":"https://e.com/%s"}`, strings.Repeat("a", 2100)), "url 上限 2048 个字符"},
		// 数值型的界得单独立腿：同一个 max 在 slice/string/number 上读法不同，
		// 实测第一版把它缝成了「retry_count 上限 取值 10」——两种读法缝在一起没人看得懂。
		{"步内 retry_count 越界", `{"name":"契约-n","task_type":"one_shot","url":"https://example.com","steps":[{"action":"wait","ms":10,"retry_count":99}]}`, "steps[0].retry_count 上限 10"},
		{"timeout_sec 低于下限", `{"name":"契约-m","task_type":"one_shot","url":"https://example.com","timeout_sec":5}`, "timeout_sec 不得小于 10"},
	}
	for _, cs := range cases {
		code, body := b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks", cs.body, "", b19eOwner)
		if code != http.StatusBadRequest {
			t.Errorf("%s → HTTP %d want 400（msg=%q）", cs.name, code, body.Message)
			continue
		}
		if !strings.Contains(body.Message, cs.want) {
			t.Errorf("%s 的文案 %q 没点出 %q", cs.name, body.Message, cs.want)
		}
		assertNoInternalLeak(t, body.Message)
	}
}

func TestMalformedJSONIsGenericBadRequest(t *testing.T) {
	c, _ := newB19eCtrl(t)
	for _, raw := range []string{`{"name":`, `[]`, `{"name":"契约","loop_count":"很多"}`} {
		code, body := b19cCall(t, c.Create, http.MethodPost, "/api/browser-automation/tasks", raw, "", b19eOwner)
		if code != http.StatusBadRequest || body.Code != string(utils.ErrorCodeInvalidParameter) {
			t.Errorf("坏请求体 %s → HTTP %d code=%v want 400/%s", raw, code, body.Code, utils.ErrorCodeInvalidParameter)
			continue
		}
		assertNoInternalLeak(t, body.Message)
	}
}

// query 侧同一条出口（List 的 limit 越界、status 不在枚举）
func TestQueryBindingUsesTheSameExit(t *testing.T) {
	c, _ := newB19eCtrl(t)
	for _, tc := range []struct{ q, want string }{
		{"limit=99999", "limit"},
		{"status=not_a_status", "status 取值需为"},
	} {
		code, body := b19cCall(t, c.List, http.MethodGet, "/api/browser-automation/tasks?"+tc.q, "", "", b19eOwner)
		if code != http.StatusBadRequest {
			t.Errorf("query %s → HTTP %d want 400（msg=%q）", tc.q, code, body.Message)
			continue
		}
		if !strings.Contains(body.Message, tc.want) {
			t.Errorf("query %s 的文案没点出字段：%q", tc.q, body.Message)
		}
		assertNoInternalLeak(t, body.Message)
	}
}

// apiFieldPath 是 B3 文案的成形处：路径错一位，用户就被指到别的输入框。
func TestAPIFieldPathShapes(t *testing.T) {
	names := apiFieldNames(&dto.CreateBrowserTaskReq{})
	for ns, want := range map[string]string{
		"CreateBrowserTaskReq.Steps[1].Action": "steps[1].action",
		"CreateBrowserTaskReq.Name":            "name",
		"CreateBrowserTaskReq.Steps":           "steps",
		"CreateBrowserTaskReq.Steps[0].Ms":     "steps[0].ms",
	} {
		if got := apiFieldPath(ns, names); got != want {
			t.Errorf("apiFieldPath(%q) = %q want %q", ns, got, want)
		}
	}
	// 表里没有的名字按原样带出：新增字段忘写 tag 时要点得出字段，而不是给一条空消息
	if got := apiFieldPath("X.SomeNewField", names); got != "SomeNewField" {
		t.Errorf("未知字段的兜底 = %q want SomeNewField", got)
	}
	// form tag 也要认（ListTaskReq 只有 form tag）
	if got := apiFieldPath("ListTaskReq.Limit", apiFieldNames(&dto.ListTaskReq{})); got != "limit" {
		t.Errorf("query 字段的对外名 = %q want limit", got)
	}
}

// ---- B2：表达式错的结论是「你改一下」而不是「我们坏了」 ----

func TestCronUpdateWithBadExprIsBadRequest(t *testing.T) {
	ctrl, db := newB19cCtrl(t)
	ctx := context.Background()
	own := b19cSeedTask(t, db, ctx, b19cOwner, "cron", "契约-cron-task")
	tr := &model.BrowserCronTrigger{TaskID: own.ID, CronExpr: "*/5 * * * *", TimeZone: "Asia/Shanghai", Enabled: false}
	if err := barepo.NewBrowserCronTriggerRepositoryWithDB(db).Create(ctx, tr); err != nil {
		t.Fatalf("种子触发器落库失败：%v", err)
	}
	idParam := fmt.Sprint(tr.ID)

	// Enabled=false 是关键：注册会往进程级 TaskManager 挂真任务，测试里不许发生
	for _, expr := range []string{"bad expr here", "* * *", "*/5 * * * * * *", "61 * * * *"} {
		code, body := b19cCall(t, ctrl.Update, http.MethodPut, "/api/browser-automation/cron/"+idParam,
			fmt.Sprintf(`{"cron_expr":%q,"time_zone":"Asia/Shanghai"}`, expr), idParam, b19cOwner)
		if code == http.StatusInternalServerError {
			t.Errorf("表达式 %q 写错 → 500（把入参问题记成了服务端的锅）msg=%q", expr, body.Message)
			continue
		}
		if code != http.StatusBadRequest || body.Code != string(utils.ErrorCodeInvalidParameter) {
			t.Errorf("表达式 %q → HTTP %d code=%v want 400/%s（msg=%q）", expr, code, body.Code,
				utils.ErrorCodeInvalidParameter, body.Message)
			continue
		}
		if !strings.Contains(body.Message, "cron 表达式无效") {
			t.Errorf("结论得说清是表达式的问题：%q", body.Message)
		}
	}
	// 时区那一支单独点明：文案要把「改表达式」和「改时区」分开，否则用户两样都去试
	if code, body := b19cCall(t, ctrl.Update, http.MethodPut, "/api/browser-automation/cron/"+idParam,
		`{"cron_expr":"*/5 * * * *","time_zone":"Mars/Olympus"}`, idParam, b19cOwner); code != http.StatusBadRequest ||
		!strings.Contains(body.Message, "时区无效") {
		t.Errorf("非法时区 → HTTP %d msg=%q want 400/含「时区无效」", code, body.Message)
	}

	var after model.BrowserCronTrigger
	if err := db.WithContext(ctx).Where("id = ?", tr.ID).First(&after).Error; err != nil {
		t.Fatalf("读回触发器失败：%v", err)
	}
	if after.CronExpr != "*/5 * * * *" {
		t.Errorf("被拒的编辑把 cron_expr 改成了 %q（应为原值）", after.CronExpr)
	}

	// 内侧：合法表达式必须过——上面那圈 400 断言否则是恒真的假锁
	if code, body := b19cCall(t, ctrl.Update, http.MethodPut, "/api/browser-automation/cron/"+idParam,
		`{"cron_expr":"*/10 * * * *","time_zone":"UTC"}`, idParam, b19cOwner); code != http.StatusOK {
		t.Errorf("合法表达式 → HTTP %d want 200（msg=%q）", code, body.Message)
	}
	if err := db.WithContext(ctx).Where("id = ?", tr.ID).First(&after).Error; err != nil {
		t.Fatalf("读回触发器失败：%v", err)
	}
	if after.CronExpr != "*/10 * * * *" {
		t.Errorf("合法编辑没落库：cron_expr = %q", after.CronExpr)
	}
}
