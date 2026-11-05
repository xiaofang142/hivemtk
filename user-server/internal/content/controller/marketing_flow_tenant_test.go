package controller

// 契约锁（实体审查 #17，HTTP 出口层）。
//
// 营销流程这条链在改前有两类只有 on 模式才暴露的故障：
//   1. 两个仓储与服务的读写面整面不带 context.Context——off 模式逐字节等价跑通，
//      on 模式每一次读写都撞 `[tenant] …ctx 未绑定` 的 fail-closed 闸，10 个 handler
//      （business_routes.go:108-121 的 10 条 + frontend_aliases.go:177-180 的 3 条别名）
//      全部回 500；详情口还把闸折叠成 404 并原样回显错误文本。
//   2. `TriggerFlow` 把异步体接到 context.Background() 上——脱离请求 ctx 是对的
//      （delay 节点最长等 300s，不能随响应返回被取消），但连租户归属一起丢了，
//      全仓 20+ 处异步写库走的是 tenant.BackgroundWithTenant，这里漏了。
// 身份口径同样锁住：CreateFlow 的 userID.(uint) 不带 ok，团队侧中间件存 float64 即 panic。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	mflTenantA     = uint(97)
	mflTenantB     = uint(98)
	mflUserA       = uint(930101)
	mflUserB       = uint(930102)
	mflTeamUserF   = 930103 // 团队侧中间件把 user_id 存成 float64
	mflMissingFlow = uint(99999999)
)

func mflDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &contentmodel.MarketingFlow{}, &contentmodel.FlowExecution{})
	if database == nil {
		t.Fatal("测试库不可达（不 Skip，跳过等于没锁）")
	}
	db.SetTestDB(database)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	return database
}

// mflSeedFlow 经 SystemScope 预置：夹具要跨租户建行，业务写口本身是被测对象。
func mflSeedFlow(t *testing.T, database *gorm.DB, tenantID uint, name string, status contentmodel.FlowStatus, createdBy uint) *contentmodel.MarketingFlow {
	t.Helper()
	row := &contentmodel.MarketingFlow{
		TenantID:  tenantID,
		Name:      name,
		Status:    status,
		FlowData:  `{"nodes":[{"id":"t1","type":"trigger","name":"起始","config":{},"next_nodes":[]}]}`,
		Version:   1,
		CreatedBy: createdBy,
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置流程 %s 失败: %v", name, err)
	}
	return row
}

// mflPhysicalFlow 物理读：断言「别租户那行没被动过」不能被隔离闸挡住而假绿。
func mflPhysicalFlow(t *testing.T, database *gorm.DB, id uint) *contentmodel.MarketingFlow {
	t.Helper()
	var row contentmodel.MarketingFlow
	err := database.WithContext(tenant.WithSystemScope(context.Background())).First(&row, id).Error
	if err != nil {
		t.Fatalf("物理读取流程 %d 失败: %v", id, err)
	}
	return &row
}

func mflPhysicalExecutions(t *testing.T, database *gorm.DB, flowID uint) []*contentmodel.FlowExecution {
	t.Helper()
	var rows []*contentmodel.FlowExecution
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("flow_id = ?", flowID).Find(&rows).Error; err != nil {
		t.Fatalf("物理读取执行记录失败: %v", err)
	}
	return rows
}

// mflRouter 复刻真实挂载：读口在 auth 组、写口在 admin 组（business_routes.go:108-121；
// 别名层 frontend_aliases.go:177-180 只有三个 GET 读口）。
// AdminAuthMiddleware 在路由层，本锁只关心 handler 拿到身份与租户之后的行为，故角色由
// X-Test-Role 头注入且默认**非** admin —— 只有非管理员身份才会走到 handler 内的归属判定。
func mflRouter(t *testing.T, bindTenant bool) *gin.Engine {
	t.Helper()
	ctrl := newTestMarketingFlowController()
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(ctx *gin.Context) {
		idStr := ctx.GetHeader("X-Test-Uid")
		switch ctx.GetHeader("X-Test-Uid-Kind") {
		case "float64":
			if f, err := strconv.ParseFloat(idStr, 64); err == nil {
				ctx.Set("user_id", f)
			}
		case "uint":
			if uid, err := strconv.ParseUint(idStr, 10, 32); err == nil && uid != 0 {
				ctx.Set("user_id", uint(uid))
			}
		}
		ctx.Set("role", ctx.GetHeader("X-Test-Role"))
		if bindTenant {
			ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), mflTenantA))
		}
		ctx.Next()
	})

	flows := router.Group("/marketing-flows")
	flows.GET("", ctrl.GetFlowList)
	flows.GET("/:id", ctrl.GetFlowByID)
	flows.GET("/:id/executions", ctrl.GetExecutionList)
	flows.GET("/:id/stats", ctrl.GetExecutionStats)
	flows.POST("", ctrl.CreateFlow)
	flows.PUT("/:id", ctrl.UpdateFlow)
	flows.DELETE("/:id", ctrl.DeleteFlow)
	flows.POST("/:id/activate", ctrl.ActivateFlow)
	flows.POST("/:id/pause", ctrl.PauseFlow)
	flows.POST("/:id/stop", ctrl.StopFlow)
	flows.POST("/:id/trigger", ctrl.Trigger)
	return router
}

func mflCall(router *gin.Engine, method, path, uid, kind string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body == nil {
		req, _ = http.NewRequest(method, path, nil)
	} else {
		req, _ = http.NewRequest(method, path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Test-Uid", uid)
	req.Header.Set("X-Test-Uid-Kind", kind)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func mflOwnUID() string { return strconv.FormatUint(uint64(mflUserA), 10) }

func mflFlowPath(id uint, suffix string) string {
	return "/marketing-flows/" + strconv.FormatUint(uint64(id), 10) + suffix
}

// TestMarketingFlowReadsAreTenantFactored 读口：列表只回本租户、详情区分 404 与闸、
// 未绑定租户必须 500（不能伪装成「没有流程」）。
func TestMarketingFlowReadsAreTenantFactored(t *testing.T) {
	database := mflDB(t)
	mflSeedFlow(t, database, mflTenantA, "A 家一号", contentmodel.FlowStatusDraft, mflUserA)
	mflSeedFlow(t, database, mflTenantA, "A 家二号", contentmodel.FlowStatusActive, mflUserA)
	foreign := mflSeedFlow(t, database, mflTenantB, "B 家一号", contentmodel.FlowStatusActive, mflUserB)

	bound := mflRouter(t, true)

	var payload struct {
		Data struct {
			List  []contentmodel.MarketingFlow `json:"list"`
			Total int64                        `json:"total"`
		} `json:"data"`
	}
	w := mflCall(bound, http.MethodGet, "/marketing-flows?page=1&page_size=20", mflOwnUID(), "uint", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析流程列表失败: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Total != 2 || len(payload.Data.List) != 2 {
		t.Errorf("A 租户只应看到自己的 2 条，实得 total=%d len=%d body=%s", payload.Data.Total, len(payload.Data.List), w.Body.String())
	}
	for _, f := range payload.Data.List {
		if strings.Contains(f.Name, "B 家") {
			t.Errorf("列表泄漏别租户流程：%+v", f)
		}
	}

	if w := mflCall(bound, http.MethodGet, mflFlowPath(foreign.ID, ""), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
		t.Errorf("读别租户流程详情应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := mflCall(bound, http.MethodGet, mflFlowPath(mflMissingFlow, ""), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
		t.Errorf("不存在的流程应 404，实得 %d body=%s", w.Code, w.Body.String())
	}

	// 未绑定租户：配置级故障说成故障，既不折叠成 404 也不回显内部原文。
	unbound := mflRouter(t, false)
	for _, path := range []string{"/marketing-flows", mflFlowPath(foreign.ID, ""), mflFlowPath(foreign.ID, "/executions"), mflFlowPath(foreign.ID, "/stats")} {
		w := mflCall(unbound, http.MethodGet, path, mflOwnUID(), "uint", nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("未绑定租户时 %s 应 500，实得 %d body=%s", path, w.Code, w.Body.String())
		}
		if body := w.Body.String(); strings.Contains(body, "租户上下文") || strings.Contains(body, "[tenant]") {
			t.Errorf("未绑定租户时 %s 回显了内部错误原文：%s", path, body)
		}
	}
	if w := mflCall(unbound, http.MethodGet, mflFlowPath(foreign.ID, ""), mflOwnUID(), "uint", nil); strings.Contains(w.Body.String(), "不存在") {
		t.Errorf("租户闸故障被说成「流程不存在」：%s", w.Body.String())
	}
}

// TestMarketingFlowWritesRejectForeignTenant 写口：改/删/状态机只能落在本租户，
// 别租户那行必须逐字段原样留着。
func TestMarketingFlowWritesRejectForeignTenant(t *testing.T) {
	database := mflDB(t)
	own := mflSeedFlow(t, database, mflTenantA, "A 家待改", contentmodel.FlowStatusDraft, mflUserA)
	foreign := mflSeedFlow(t, database, mflTenantB, "B 家待改", contentmodel.FlowStatusActive, mflUserB)

	bound := mflRouter(t, true)
	patch, _ := json.Marshal(map[string]any{"name": "被别租户改名"})
	del := mflCall(bound, http.MethodPut, mflFlowPath(foreign.ID, ""), mflOwnUID(), "uint", patch)
	if del.Code != http.StatusNotFound {
		t.Errorf("改别租户流程应 404，实得 %d body=%s", del.Code, del.Body.String())
	}
	if got := mflPhysicalFlow(t, database, foreign.ID); got.Name != "B 家待改" || got.Status != contentmodel.FlowStatusActive {
		t.Errorf("别租户流程被改写：%+v", got)
	}

	for _, suffix := range []string{"/activate", "/pause", "/stop"} {
		if w := mflCall(bound, http.MethodPost, mflFlowPath(foreign.ID, suffix), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
			t.Errorf("对别租户流程 %s 应 404，实得 %d body=%s", suffix, w.Code, w.Body.String())
		}
	}
	if w := mflCall(bound, http.MethodDelete, mflFlowPath(foreign.ID, ""), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
		t.Errorf("删别租户流程应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := mflPhysicalFlow(t, database, foreign.ID); got.Version != 1 {
		t.Errorf("别租户流程 version 被动过：%+v", got)
	}

	if w := mflCall(bound, http.MethodPut, mflFlowPath(own.ID, ""), mflOwnUID(), "uint", patch); w.Code != http.StatusOK {
		t.Fatalf("改本租户流程应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := mflPhysicalFlow(t, database, own.ID); got.Name != "被别租户改名" || got.Version != 2 {
		t.Errorf("本租户流程未被更新：%+v", got)
	}

	// 未绑定租户的写口同样 fail-closed，且别租户/本租户的行都不许动。
	unbound := mflRouter(t, false)
	if w := mflCall(unbound, http.MethodPut, mflFlowPath(own.ID, ""), mflOwnUID(), "uint", patch); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户时改名应 500，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := mflPhysicalFlow(t, database, own.ID); got.Name != "被别租户改名" {
		t.Errorf("fail-closed 之后本租户行仍应停在改名后的值，实得 %+v", got)
	}
}

// TestMarketingFlowCreateStampsCallerTenant 建流程落库必须带调用方租户与创建人，
// 否则「谁能改/删」的归属判定和隔离都失去依据。
func TestMarketingFlowCreateStampsCallerTenant(t *testing.T) {
	database := mflDB(t)
	bound := mflRouter(t, true)

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "新建流程",
		"trigger_type": "user_follow",
		"flow_data": map[string]any{
			"nodes": []map[string]any{{"id": "t1", "type": "trigger", "name": "起始", "config": map[string]any{}}},
		},
	})
	w := mflCall(bound, http.MethodPost, "/marketing-flows", mflOwnUID(), "uint", reqBody)
	if w.Code != http.StatusOK {
		t.Fatalf("建流程应 200，实得 %d body=%s", w.Code, w.Body.String())
	}

	var created struct {
		Data contentmodel.MarketingFlow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析建流程响应失败: %v body=%s", err, w.Body.String())
	}
	if created.Data.ID == 0 {
		t.Fatalf("响应没带回新建流程的 id：%s", w.Body.String())
	}
	got := mflPhysicalFlow(t, database, created.Data.ID)
	if got.TenantID != mflTenantA {
		t.Errorf("新建流程租户应为 %d，实得 %d", mflTenantA, got.TenantID)
	}
	if got.CreatedBy != mflUserA {
		t.Errorf("新建流程 created_by 应为 %d，实得 %d", mflUserA, got.CreatedBy)
	}

	// 无身份：401，不是带着 created_by=0 落库。
	if w := mflCall(bound, http.MethodPost, "/marketing-flows", "", "none", reqBody); w.Code != http.StatusUnauthorized {
		t.Errorf("无身份建流程应 401，实得 %d body=%s", w.Code, w.Body.String())
	}
	// 未绑定租户：500 且不回显原文。
	if w := mflCall(mflRouter(t, false), http.MethodPost, "/marketing-flows", mflOwnUID(), "uint", reqBody); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户建流程应 500，实得 %d body=%s", w.Code, w.Body.String())
	} else if strings.Contains(w.Body.String(), "租户上下文") {
		t.Errorf("未绑定租户建流程回显了内部错误原文：%s", w.Body.String())
	}
}

// TestMarketingFlowTriggerKeepsTenantInAsyncBody 触发的异步执行体必须续上租户归属：
// 执行记录落到本租户，且后台把状态写回（不脱离请求 ctx，但不能脱离租户）。
func TestMarketingFlowTriggerKeepsTenantInAsyncBody(t *testing.T) {
	database := mflDB(t)
	flow := mflSeedFlow(t, database, mflTenantA, "A 家触发流程", contentmodel.FlowStatusActive, mflUserA)

	w := mflCall(mflRouter(t, true), http.MethodPost, mflFlowPath(flow.ID, "/trigger"), mflOwnUID(), "uint", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("触发本租户流程应 200，实得 %d body=%s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	var rows []*contentmodel.FlowExecution
	for {
		rows = mflPhysicalExecutions(t, database, flow.ID)
		if len(rows) == 1 && rows[0].Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("异步执行体没把状态写回（改前 context.Background() 丢租户 ⇒ 隔离闸拒绝落库）：%+v", rows)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if rows[0].TenantID != mflTenantA {
		t.Errorf("执行记录租户应为 %d，实得 %d", mflTenantA, rows[0].TenantID)
	}
	if rows[0].Status != "completed" {
		t.Errorf("单触发器节点流程应 completed，实得 %s err=%q", rows[0].Status, rows[0].ErrorMessage)
	}
	if rows[0].UserID != mflOwnUID() {
		t.Errorf("执行记录应归属触发人 %s，实得 %s", mflOwnUID(), rows[0].UserID)
	}

	// 别租户流程不可触发；本租户执行记录不可被别租户读到（读口的租户裁剪）。
	foreign := mflSeedFlow(t, database, mflTenantB, "B 家触发流程", contentmodel.FlowStatusActive, mflUserB)
	if w := mflCall(mflRouter(t, true), http.MethodPost, mflFlowPath(foreign.ID, "/trigger"), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
		t.Errorf("触发别租户流程应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if rows := mflPhysicalExecutions(t, database, foreign.ID); len(rows) != 0 {
		t.Errorf("别租户流程被写了执行记录：%+v", rows)
	}
	// 未绑定租户：500，且一行都不落。
	if w := mflCall(mflRouter(t, false), http.MethodPost, mflFlowPath(flow.ID, "/trigger"), mflOwnUID(), "uint", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户触发应 500，实得 %d body=%s", w.Code, w.Body.String())
	}
}

// TestMarketingFlowTeamIdentityDoesNotPanic 身份类型口径：团队侧中间件存 float64，
// 不带 ok 的 userID.(uint) 断言会 panic 成 500；无身份应是 401/403 而不是 500。
func TestMarketingFlowTeamIdentityDoesNotPanic(t *testing.T) {
	database := mflDB(t)
	own := mflSeedFlow(t, database, mflTenantA, "A 家流程", contentmodel.FlowStatusDraft, mflUserA)
	bound := mflRouter(t, true)
	teamUID := strconv.FormatUint(mflTeamUserF, 10)
	patch, _ := json.Marshal(map[string]any{"name": "团队改名"})

	// float64 身份 + 非本流程创建人：拿到的是权限判定，不是 panic。
	if w := mflCall(bound, http.MethodPut, mflFlowPath(own.ID, ""), teamUID, "float64", patch); w.Code != http.StatusForbidden {
		t.Errorf("float64 身份改他人流程应 403，实得 %d body=%s", w.Code, w.Body.String())
	}
	// 无身份：403（创建人不匹配），不是 panic 兜出来的 500。
	if w := mflCall(bound, http.MethodPut, mflFlowPath(own.ID, ""), "", "none", patch); w.Code != http.StatusForbidden {
		t.Errorf("无身份改流程应 403，实得 %d body=%s", w.Code, w.Body.String())
	}
	// float64 身份创建人自己：应放行，证明身份没被降级成 0。
	teamFlow := mflSeedFlow(t, database, mflTenantA, "团队家流程", contentmodel.FlowStatusDraft, mflTeamUserF)
	if w := mflCall(bound, http.MethodPut, mflFlowPath(teamFlow.ID, ""), teamUID, "float64", patch); w.Code != http.StatusOK {
		t.Errorf("float64 身份改自己的流程应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := mflCall(bound, http.MethodPost, "/marketing-flows", teamUID, "float64", func() []byte {
		b, _ := json.Marshal(map[string]any{
			"name":         "团队建流程",
			"trigger_type": "user_follow",
			"flow_data":    map[string]any{"nodes": []map[string]any{{"id": "t1", "type": "trigger", "config": map[string]any{}}}},
		})
		return b
	}()); w.Code != http.StatusOK {
		t.Errorf("float64 身份建流程应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
}

// TestMarketingFlowExecutionReadsDoNotEchoInternals 执行记录读口的错误分类：
// 「流程不存在」与「没绑租户」不能同码，响应体不带内部错误原文。
func TestMarketingFlowExecutionReadsDoNotEchoInternals(t *testing.T) {
	database := mflDB(t)
	flow := mflSeedFlow(t, database, mflTenantA, "A 家执行流程", contentmodel.FlowStatusActive, mflUserA)
	bound := mflRouter(t, true)

	for _, suffix := range []string{"/executions", "/stats"} {
		if w := mflCall(bound, http.MethodGet, mflFlowPath(flow.ID, suffix), mflOwnUID(), "uint", nil); w.Code != http.StatusOK {
			t.Errorf("本租户 %s 应 200，实得 %d body=%s", suffix, w.Code, w.Body.String())
		}
		foreign := mflSeedFlow(t, database, mflTenantB, "B 家执行流程", contentmodel.FlowStatusActive, mflUserB)
		if w := mflCall(bound, http.MethodGet, mflFlowPath(foreign.ID, suffix), mflOwnUID(), "uint", nil); w.Code != http.StatusNotFound {
			t.Errorf("对别租户流程读 %s 应 404，实得 %d body=%s", suffix, w.Code, w.Body.String())
		}
	}

	unbound := mflRouter(t, false)
	for _, suffix := range []string{"/executions", "/stats"} {
		w := mflCall(unbound, http.MethodGet, mflFlowPath(flow.ID, suffix), mflOwnUID(), "uint", nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("未绑定租户读 %s 应 500，实得 %d body=%s", suffix, w.Code, w.Body.String())
		}
		if body := w.Body.String(); strings.Contains(body, "租户上下文") || strings.Contains(body, "[tenant]") {
			t.Errorf("未绑定租户读 %s 回显了内部错误原文：%s", suffix, body)
		}
	}
}
