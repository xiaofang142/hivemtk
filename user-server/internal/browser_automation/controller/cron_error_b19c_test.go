package controller

// 契约锁：触发器域的错误分类。
// cron 的四个写入口此前一律 `response.Error(ctx, 400, err.Error())`——把三类完全不同的结论
// 折成一句「客户端参数错」，还把内部原文（gorm 的 "record not found"、DB 故障的 SQLSTATE）
// 直接吐给前端弹条。同一域的任务侧已经按类型分流了，这里没跟上就是漂移：
//   - 别人的/不存在的触发器 id → 404，且主语是「触发器」；
//   - 前置条件不满足（非 cron 任务、表达式无效、已有触发器）→ 400/409 + 原因原文；
//   - 真·未知错误 → 500（不许被洗成客户端错误）。
// 断言全部走真 handler + 真库，不用静态锁：门的形状改了，字面量锁会照样绿。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	b19cOwner = uint(781403)
	b19cAlien = uint(781404)
)

func b19cDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{}, &model.BrowserCronTrigger{})
	if db == nil {
		t.Fatal("测试库不可达：本文件的状态码分流无法判定（不 Skip，跳过等于没锁）")
	}
	return db
}

func newB19cCtrl(t *testing.T) (*CronController, *gorm.DB) {
	t.Helper()
	db := b19cDB(t)
	// taskSvc 传 nil：四条腿都在碰到执行链之前就返回，用不上它——
	// 一旦哪天分类逻辑挪到了执行之后，这里会当场 panic，比断言更硬。
	return NewCronController(basvc.NewCronService(
		barepo.NewBrowserCronTriggerRepositoryWithDB(db),
		barepo.NewBrowserTaskRepositoryWithDB(db), nil,
	)), db
}

func b19cSeedTask(t *testing.T, db *gorm.DB, ctx context.Context, userID uint, taskType, name string) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{Name: name, TaskType: taskType, Status: "ready",
		Url: "https://example.com", UserID: userID, Platform: "xiaohongshu",
		TimeoutSec: 120, Steps: []byte(`[{"action":"open_tab"}]`)}
	if err := barepo.NewBrowserTaskRepositoryWithDB(db).Create(ctx, task); err != nil {
		t.Fatalf("种子任务 %s 落库失败：%v", name, err)
	}
	return task
}

func b19cCall(t *testing.T, h gin.HandlerFunc, method, path, body string, idParam string, userID uint) (int, errBody) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	ctx.Request = req
	if idParam != "" {
		ctx.Params = gin.Params{{Key: "id", Value: idParam}}
	}
	ctx.Set("user_id", userID)
	h(ctx)
	var b errBody
	if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil {
		t.Fatalf("响应体不是合法 JSON（%v）: %s", e, w.Body.String())
	}
	return w.Code, b
}

const b19cExpr = `{"cron_expr":"*/5 * * * *","time_zone":"Asia/Shanghai"}`

func TestCronUnknownTriggerIDIs404Not400(t *testing.T) {
	ctrl, _ := newB19cCtrl(t)
	// id 取一个必然不存在的：browser_cron_triggers.id 从 1 起，测试库里到不了这个量级
	for name, h := range map[string]gin.HandlerFunc{
		"Update":  ctrl.Update,
		"Delete":  ctrl.Delete,
		"Disable": ctrl.Disable,
	} {
		method := http.MethodPut
		if name == "Delete" {
			method = http.MethodDelete
		} else if name == "Disable" {
			method = http.MethodPost
		}
		httpCode, body := b19cCall(t, h, method, "/api/browser-automation/cron/987654321", b19cExpr, "987654321", b19cOwner)
		if httpCode != http.StatusNotFound {
			t.Errorf("%s 不存在的触发器 → HTTP %d want 404（body=%v %q）", name, httpCode, body.Code, body.Message)
			continue
		}
		if body.Code != string(utils.ErrorCodeNotFound) {
			t.Errorf("%s → code=%v want %s", name, body.Code, utils.ErrorCodeNotFound)
		}
		// 内部原文不许出现在给用户的文案里
		if strings.Contains(strings.ToLower(body.Message), "record not found") {
			t.Errorf("%s 把 gorm 原文回给了前端：%q", name, body.Message)
		}
		if !strings.Contains(body.Message, "触发器") {
			t.Errorf("%s 文案得说清是哪样东西不存在：%q", name, body.Message)
		}
	}
}

func TestCronCreateOnOthersTaskIs404AboutTheTask(t *testing.T) {
	ctrl, db := newB19cCtrl(t)
	ctx := context.Background()
	alien := b19cSeedTask(t, db, ctx, b19cAlien, "cron", "b19c-alien")
	body := fmt.Sprintf(`{"task_id":%d,"cron_expr":"*/5 * * * *"}`, alien.ID)
	httpCode, resp := b19cCall(t, ctrl.Create, http.MethodPost, "/api/browser-automation/cron", body, "", b19cOwner)
	if httpCode != http.StatusNotFound {
		t.Errorf("挂在别人任务上建触发器 → HTTP %d want 404（code=%v %q）", httpCode, resp.Code, resp.Message)
	}
	// 主语必须是「任务」：回「触发器不存在」会把用户支到错误的排查方向
	if !strings.Contains(resp.Message, "任务") {
		t.Errorf("文案主语错了：%q", resp.Message)
	}
	if strings.Contains(resp.Message, "触发器") {
		t.Errorf("文案把主语说成了触发器：%q", resp.Message)
	}
}

func TestCronNonCronTaskIsClientError(t *testing.T) {
	ctrl, db := newB19cCtrl(t)
	ctx := context.Background()
	own := b19cSeedTask(t, db, ctx, b19cOwner, "one_shot", "b19c-oneshot")
	body := fmt.Sprintf(`{"task_id":%d,"cron_expr":"*/5 * * * *"}`, own.ID)
	httpCode, resp := b19cCall(t, ctrl.Create, http.MethodPost, "/api/browser-automation/cron", body, "", b19cOwner)
	// 任务类型是那条任务本身的属性，换一条任务就能过 ⇒ 入参不合法（400）。
	// 这里不收 409：把"选错任务"记成"状态冲突"，前端会提示用户等一等再试，而它永远不会自己变好。
	if httpCode != http.StatusBadRequest || resp.Code != string(utils.ErrorCodeInvalidParameter) {
		t.Errorf("非 cron 任务配触发器 → HTTP %d code=%v want 400/%s（%q）",
			httpCode, resp.Code, utils.ErrorCodeInvalidParameter, resp.Message)
	}
	if !strings.Contains(resp.Message, "cron") {
		t.Errorf("原因得带给前端：%q", resp.Message)
	}
}

// 重复建触发器：task_id 上有唯一索引，"已存在"是一条换个时机/换个任务就不成立的前置冲突，
// 既不是入参不合法（400：改了请求照样撞唯一键）也不是服务端故障（500）。
// 这一条是对既有正确行为的锁（本批没改它），给电池当靶用。
func TestCronDuplicateTriggerIsConflict(t *testing.T) {
	ctrl, db := newB19cCtrl(t)
	ctx := context.Background()
	own := b19cSeedTask(t, db, ctx, b19cOwner, "cron", "b19c-dup")
	if err := barepo.NewBrowserCronTriggerRepositoryWithDB(db).Create(ctx, &model.BrowserCronTrigger{
		TaskID: own.ID, CronExpr: "*/5 * * * *", Enabled: true,
	}); err != nil {
		t.Fatalf("种子触发器落库失败：%v", err)
	}
	body := fmt.Sprintf(`{"task_id":%d,"cron_expr":"*/10 * * * *"}`, own.ID)
	httpCode, resp := b19cCall(t, ctrl.Create, http.MethodPost, "/api/browser-automation/cron", body, "", b19cOwner)
	if httpCode != http.StatusConflict {
		t.Errorf("重复建触发器 → HTTP %d want 409（code=%v %q）", httpCode, resp.Code, resp.Message)
	}
	if !strings.Contains(resp.Message, "已存在") {
		t.Errorf("原因得带给前端：%q", resp.Message)
	}
}

// 反向锁：分流不许把真故障洗成客户端错误
func TestCronUnknownErrorStays500(t *testing.T) {
	cases := []struct {
		err      error
		wantHTTP int
		wantCode utils.ErrorCode
	}{
		{fmt.Errorf("包裹一层: %w", &basvc.InvalidInputError{Msg: "cron 表达式无效"}), http.StatusBadRequest, utils.ErrorCodeInvalidParameter},
		{&basvc.StateConflictError{Msg: "该任务已存在触发器"}, http.StatusConflict, utils.ErrorCodeBrowserStateConflict},
		{&basvc.NotFoundError{Msg: "任务不存在"}, http.StatusNotFound, utils.ErrorCodeNotFound},
		{gorm.ErrRecordNotFound, http.StatusNotFound, utils.ErrorCodeNotFound},
		{fmt.Errorf("dial tcp 127.0.0.1:8232: connect: connection refused"), http.StatusInternalServerError, utils.ErrorCodeInternalError},
	}
	for _, c := range cases {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/browser-automation/cron/1", nil)
		cronErrToResponse(ctx, c.err)
		var body errBody
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatalf("响应体不是合法 JSON（%v）: %s", e, w.Body.String())
		}
		if w.Code != c.wantHTTP || body.Code != string(c.wantCode) {
			t.Errorf("%v → HTTP %d code=%v want %d/%s", c.err, w.Code, body.Code, c.wantHTTP, c.wantCode)
		}
	}
}

// 共享映射的回归锁：任务侧的 404 文案不能被 cron 的「触发器不存在」顶掉
func TestTaskNotFoundWordingUnchanged(t *testing.T) {
	httpCode, body := runTaskErr(t, gorm.ErrRecordNotFound)
	if httpCode != http.StatusNotFound || body.Message != "任务不存在" {
		t.Errorf("任务侧 404 → HTTP %d %q want 404/任务不存在", httpCode, body.Message)
	}
}
