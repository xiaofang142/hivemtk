package controller

// 批10 契约锁：409 域内三件事必须分码。
// HTTP 状态相同（都是 409）时，前端的分流依据只有响应体 code——
// 全部折成 errorCodeFromHTTPCode(409)=DUPLICATE_ENTRY_3003 的那版实现里，
// 「已有任务执行中」也会弹「去装 Host」的引导（真机走 UI 实测）。
// 因此这里断的是**码**，不是文案：文案会变，码是契约。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
)

type errBody struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

func runTaskErr(t *testing.T, err error) (int, errBody) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/browser-automation/tasks/1/run", nil)
	taskErrToResponse(ctx, err)
	var body errBody
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatalf("响应体不是合法 JSON（%v）: %s", e, w.Body.String())
	}
	return w.Code, body
}

func TestHostOfflineAndUserBusyCarryDistinctCodes(t *testing.T) {
	offHTTP, off := runTaskErr(t, basvc.ErrHostOffline)
	busyHTTP, busy := runTaskErr(t, fmt.Errorf("包裹一层: %w", basvc.ErrUserBusy))

	if offHTTP != 409 || busyHTTP != 409 {
		t.Errorf("HTTP 码 offline=%d busy=%d want 409/409", offHTTP, busyHTTP)
	}
	if off.Code != string(utils.ErrorCodeBrowserHostOffline) {
		t.Errorf("离线码=%v want %s", off.Code, utils.ErrorCodeBrowserHostOffline)
	}
	if busy.Code != string(utils.ErrorCodeBrowserTaskBusy) {
		t.Errorf("忙码=%v want %s", busy.Code, utils.ErrorCodeBrowserTaskBusy)
	}
	if off.Code == busy.Code {
		t.Errorf("两条 409 撞在同一码上（%v），前端无法分流", off.Code)
	}
	// 码必须带域内编号：只有 HTTP 派生码（DUPLICATE_ENTRY_3003）时这一条先红
	if off.Code == string(utils.ErrorCodeDuplicateEntry) || busy.Code == string(utils.ErrorCodeDuplicateEntry) {
		t.Errorf("退回成 HTTP 派生码了: offline=%v busy=%v", off.Code, busy.Code)
	}
	// 文案仍要透传域内错误原文（引导弹窗与提示条都靠它，不能被码配置顶掉）
	if off.Message == "" || busy.Message == "" {
		t.Errorf("错误文案丢失: offline=%q busy=%q", off.Message, busy.Message)
	}
}

// 其余 409 场景不许被顺手改道：依赖未满足仍是 HTTP 派生码
func TestOtherConflictsKeepGenericCode(t *testing.T) {
	for _, err := range []error{basvc.ErrTaskRunning, basvc.ErrDependencyNotMet} {
		httpCode, body := runTaskErr(t, err)
		if httpCode != 409 {
			t.Errorf("%v → HTTP %d want 409", err, httpCode)
		}
		if body.Code != string(utils.ErrorCodeDuplicateEntry) {
			t.Errorf("%v → code=%v want %s（本批改的是离线/忙两条，别把依赖未满足也换道）",
				err, body.Code, utils.ErrorCodeDuplicateEntry)
		}
	}
}

// 批10c：/run 请求面上的离线先验门。
// ErrHostOffline 过去只由执行期的 HostRegistry 产生，而 RunTask 是「建会话 + SafeGoDetached
// 异步跑」——离线点「执行」永远拿 200，8001 与前端引导弹窗都不可能命中（真机 C 腿实测：
// 服务端 host/status 已 count=0，/run 仍回 200 + session_id）。门要挂在交互路径上，
// cron/重试那条仍然允许排队等 Host。
type stubProber struct{ online bool }

func (s stubProber) EnsureOnline(uint) error {
	if s.online {
		return nil
	}
	return basvc.ErrHostOffline
}

func runHandler(t *testing.T, prober hostProber) (int, errBody) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/browser-automation/tasks/1/run", nil)
	// 测试上下文没有路由树，Param("id") 必须手工喂——不喂就是 parseID 先回 400，
	// 两条断言会以「invalid id」的形态一起红，看起来像门禁坏了（本次第一次跑就是这样）。
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}
	ctx.Set("user_id", uint(26))
	// svc 故意留 nil：探针放行后会走到 RunTask 并立刻 panic，测试当场炸——
	// 这比断言「没执行」更硬：它证明离线结论是在碰任务之前就给出的。
	c := NewTaskController(nil, prober)
	c.Run(ctx)
	var body errBody
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatalf("响应体不是合法 JSON（%v）: %s", e, w.Body.String())
	}
	return w.Code, body
}

func TestOfflineHostRefusesRunAtRequestTime(t *testing.T) {
	httpCode, body := runHandler(t, stubProber{online: false})
	if httpCode != 409 {
		t.Errorf("离线 /run → HTTP %d want 409", httpCode)
	}
	if body.Code != string(utils.ErrorCodeBrowserHostOffline) {
		t.Errorf("离线 /run → code=%v want %s（前端靠它开引导弹窗）", body.Code, utils.ErrorCodeBrowserHostOffline)
	}
	if !strings.Contains(body.Message, "未连接") {
		t.Errorf("离线文案要能告诉用户去装 Host: %q", body.Message)
	}
}

func TestOtherDomainErrorsStillReachService(t *testing.T) {
	// 反向锁：探针放行时不能把请求也一并挡掉（否则 8001 修好了，执行却永久失效）。
	// 断言必须落在「执行链路真的被碰到」上，不能只断「有没有 panic」：
	// 把先验门改成无条件拦死（`; true {`）同样会 panic —— taskErrToResponse 里
	// err 是 nil，err.Error() 当场炸——只断有无 panic 时这条变异是绿的（电池实测 SURVIVED）。
	// 所以这里验的是栈里出现 RunTask：进了执行链才可能碰它，被门吃掉时只到 taskErrToResponse。
	var stack string
	func() {
		defer func() {
			if r := recover(); r != nil {
				stack = string(debug.Stack())
			}
		}()
		runHandler(t, stubProber{online: true})
	}()
	if stack == "" {
		t.Fatal("svc 故意留 nil 却没炸，说明执行链路压根没被调用（先验门把正常路径也拦了）")
	}
	if !strings.Contains(stack, "(*TaskService).RunTask") {
		t.Errorf("Host 在线时 /run 没进到 RunTask（先验门放行失效）：\n%s", stack)
	}
}

// 批10b：真机 UI 腿在列表页点 draft 任务的「执行」，服务端回的是
// HTTP 500 INTERNAL_ERROR_6002 + 文案「任务状态 draft 不可执行（需先 publish）」——
// 用户一次正常误操作被记成服务端故障。前置条件类结论必须落 4xx 且文案原样透传，
// 而真·未知错误要继续是 500（否则这层分流会把真故障洗成客户端错误）。
func TestPreconditionErrorsAreNotReportedAsServerFaults(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		want     int
		wantCode string
		wantMsg  string
	}{
		{"状态不满足 → 409", &basvc.StateConflictError{Msg: "任务状态 draft 不可执行（需先 publish）"}, 409,
			string(utils.ErrorCodeBrowserStateConflict), "任务状态 draft 不可执行（需先 publish）"},
		{"入参不合法 → 400", &basvc.InvalidInputError{Msg: "检测到任务依赖环"}, 400,
			string(utils.ErrorCodeInvalidParameter), "检测到任务依赖环"},
		// 包一层前缀也要认出类型：409 发的是原因本身，外层上下文文字不进响应体
		{"包一层仍认出类型", fmt.Errorf("外层上下文: %w", &basvc.StateConflictError{Msg: "仅执行中任务可暂停，当前状态: draft"}), 409,
			string(utils.ErrorCodeBrowserStateConflict), "仅执行中任务可暂停，当前状态: draft"},
		{"未知错误保持 500", errors.New("FATAL: connection reset by peer"), 500,
			string(utils.ErrorCodeInternalError), "FATAL: connection reset by peer"},
	}
	for _, c := range cases {
		httpCode, body := runTaskErr(t, c.err)
		if httpCode != c.want {
			t.Errorf("%s → HTTP %d want %d（code=%v）", c.name, httpCode, c.want, body.Code)
		}
		// 码也在契约里：只断 HTTP 码时「409 状态不满足」会被折成 DUPLICATE_ENTRY_3003
		// （errorCodeFromHTTPCode 的默认口径），而那是唯一键冲突的意思——真机 A0 腿实测就是这样蹭绿的。
		if body.Code != c.wantCode {
			t.Errorf("%s → code=%v want %s", c.name, body.Code, c.wantCode)
		}
		if body.Message != c.wantMsg {
			t.Errorf("%s → 文案 %q want %q（分流不能顺手把原因吃掉）", c.name, body.Message, c.wantMsg)
		}
	}
}

// 装配锁（口径同批8/批9）：先验门用的探针必须是真注册表。
// 装配时传 nil，handler 里那两行一个字都不用改、每个请求照样执行，只是每次都在
// c.host.EnsureOnline 上 panic 被 gin 兜成 500 —— 在线离线一起坏，比没修更糟。
// 所以断的是「装配时给了谁」这条字面量，不是 NewTaskController 出现了几次
// （批8 吃过计数式静态锁的亏：语句还在、条件被抽空，锁照样绿）。
func TestRunPreGateIsWiredToRealRegistry(t *testing.T) {
	src, err := os.ReadFile("../../router/browser_automation_routes.go")
	if err != nil {
		t.Fatalf("读路由装配文件失败: %v", err)
	}
	const want = "bactrl.NewTaskController(taskSvc, registry)"
	if !strings.Contains(string(src), want) {
		t.Errorf("路由装配必须是 %s：探针传 nil 会让先验门在每个请求上 panic 成 500", want)
	}
}
