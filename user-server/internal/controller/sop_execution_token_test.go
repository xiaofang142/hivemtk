// sop_execution_token_test.go 钉住"恢复凭证不出现在 SOP 执行读端点的响应里"。
//
// 为什么单独一条而不是并进 approval_routes_test.go：那条测的是审批域自己的出口
// （/api/approvals/*、/api/human-tasks/*），而这一格的凭证是审批桥**替流程**写进
// sop_executions.execution_data 的（checkpoint 要凭它续跑，见 service.ApprovalOutcomeTokenKey
// 与 sop_approval_resume_test 里那条 checkpoint 断言）。于是同一个值有两条出口，
// 只锁审批那张表等于没锁 —— 这条测的就是第二条。
//
// 两条判据配成对：
//  1. 禁串（凭证值、`resume_token` 字段名）一次都不许出现；
//  2. 正控制（同一份 execution_data 里其余三格必须照旧出现）。
//     少了第 2 条，"把整格抹平"和"只遮一枚值"在测试里长得一样，
//     而前者会让运维读不出"这条流程等过审批"。
package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// tokenNeedle 只在本次运行里出现的"凭证"。断言它不出现在响应里，
// 比断言某个通用前缀不出现准：后者会在响应恰好没有数据时假绿。
const tokenNeedle = "rt_needle_f1a2b3c4d5e6"

// approvalBearingData 一份"审批等过、结论已回读"的执行数据：四格各有用途，
// 除凭证外全是运维要看的。
func approvalBearingData() model.JSONMap {
	return model.JSONMap{
		service.ApprovalOutcomeTokenKey:  tokenNeedle,
		service.ApprovalOutcomeStatusKey: model.ApprovalStatusApproved,
		service.ApprovalOutcomeIDKey:     "apr_f1_probe",
		"customer_name":                  "张先生",
	}
}

func setupSOPTokenDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.SOPExecution{})
	if database == nil {
		t.Skip("PostgreSQL 测试库不可达 ⇒ 本文件未跑（不是通过）")
	}
	return database
}

func newSOPTokenEngine(database *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewSOPController(service.NewSOPService(database, nil))
	engine := gin.New()
	// 路径与 router/service_routes.go 里那两条一致：改路由名不改这里，测的就不是同一条出口。
	engine.GET("/api/sop/executions", ctrl.ListExecutions)
	engine.GET("/api/sop/executions/:id", ctrl.GetExecution)
	return engine
}

func seedApprovalBearingExecution(t *testing.T, database *gorm.DB, customerID string) *model.SOPExecution {
	t.Helper()
	exec := &model.SOPExecution{
		SOPID:         900001,
		CustomerID:    customerID,
		CurrentNode:   "wait_approval",
		Status:        "waiting",
		StartedAt:     time.Now(),
		ExecutionData: approvalBearingData(),
	}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("造执行记录失败：%v", err)
	}
	if exec.ID == 0 {
		t.Fatal("执行记录主键没拿到，后面的 :id 请求会打到别的数据上")
	}
	return exec
}

// assertTokenMasked 两条判据一起过：禁串零命中 + 正控制全命中。
func assertTokenMasked(t *testing.T, body, endpoint string) {
	t.Helper()
	for _, needle := range []string{tokenNeedle, "resume_token", `"rt_`} {
		if strings.Contains(body, needle) {
			t.Errorf("%s 响应里出现了 %q ⇒ 恢复凭证出网：body=%s", endpoint, needle, body)
		}
	}
	if !strings.Contains(body, approvalTokenMask) {
		t.Errorf("%s 响应里没有遮罩值 %q：凭证那一格该被遮而不是整格消失（读报告的人要能看出这里本来有东西）\nbody=%s",
			endpoint, approvalTokenMask, body)
	}
	// 正控制：同格其余内容必须照旧在。
	for _, want := range []string{"_approval_status", "approved", "_approval_id", "apr_f1_probe", "张先生"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s 响应里缺正控制 %q ⇒ 判据可能在量一个空响应（空响应永远不含禁串）\nbody=%s",
				endpoint, want, body)
		}
	}
}

func TestSOPGetExecution_RedactsResumeToken(t *testing.T) {
	database := setupSOPTokenDB(t)
	seed := seedApprovalBearingExecution(t, database, "cust_f1_detail")
	engine := newSOPTokenEngine(database)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sop/executions/"+itoa(int(seed.ID)), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；body=%s", w.Code, w.Body.String())
	}
	assertTokenMasked(t, w.Body.String(), "GET /api/sop/executions/:id")

	// 出口遮了而库里没动：checkpoint 里的凭证是流程续跑要读的，遮罩只能发生在响应上。
	var back model.SOPExecution
	if err := database.First(&back, seed.ID).Error; err != nil {
		t.Fatalf("回读执行记录失败：%v", err)
	}
	if got, _ := back.ExecutionData[service.ApprovalOutcomeTokenKey].(string); got != tokenNeedle {
		t.Errorf("库里的凭证被响应出口改写：%q，期望原样 %q（续跑读的就是这一格）", got, tokenNeedle)
	}
}

func TestSOPListExecutions_RedactsResumeToken(t *testing.T) {
	database := setupSOPTokenDB(t)
	seedApprovalBearingExecution(t, database, "cust_f1_list")
	engine := newSOPTokenEngine(database)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sop/executions?customer_id=cust_f1_list", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；body=%s", w.Code, w.Body.String())
	}
	assertTokenMasked(t, w.Body.String(), "GET /api/sop/executions")
}

// 副本语义单独测：不复制的话同一进程里 Execute/Step 返回的活对象会被改，
// 而那次改写会影响后续要读 ExecutionData 的调用方（不是一次请求内的事）。
func TestRedactApprovalTokenDoesNotMutateInput(t *testing.T) {
	data := approvalBearingData()
	got := redactApprovalToken(data)

	if in, _ := data[service.ApprovalOutcomeTokenKey].(string); in != tokenNeedle {
		t.Errorf("入参被改写：%q", in)
	}
	if masked, _ := got[service.ApprovalOutcomeTokenKey].(string); masked != approvalTokenMask {
		t.Errorf("返回值没遮：%q，期望 %q", masked, approvalTokenMask)
	}
	if len(got) != len(data) {
		t.Errorf("遮罩改了格数：%d vs %d（只许换值不许增删键）", len(got), len(data))
	}

	// 没有凭证的执行记录（绝大多数）必须原样返回同一份 map：为它们各建一份副本是白付的分配，
	// 而这条断言就是"别顺手改掉那个短路"的守卫。
	plain := model.JSONMap{"customer_name": "张先生"}
	if redactApprovalToken(plain) == nil {
		t.Error("无凭证时应原样返回，实际 nil")
	}
	if got := redactApprovalToken(plain); !sameMap(got, plain) {
		t.Errorf("无凭证时被改写了：%v vs %v", got, plain)
	}
	if redactExecution(nil) != nil {
		t.Error("nil 执行记录必须回 nil，不能让上层拿到一个空壳当成一条真记录")
	}
}

func sameMap(a, b model.JSONMap) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
