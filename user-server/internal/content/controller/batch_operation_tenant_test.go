package controller

// 契约锁（实体审查 #15，HTTP 出口层）：这一层要钉的是「错误分类」和「权限宽度」，仓储用例看不见。
//
// 改前 `GetHistoryByID` handler 把服务层的任何 error 一律回成 404「历史记录不存在」，
// 而服务层丢掉 ctx 时抛的是「[tenant] …ctx 未绑定」——配置级故障被伪装成「记录不存在」，
// 排查方向从「多租户中间件没接上」变成「这条历史怎么不见了」。
// 权限宽度按 `business_routes.go:20-30` 的实际挂载钉：
//   auth 组：GET /batch/histories、GET /batch/histories/:id（本租户内共享，只按租户收窄）
//   admin 组：POST /batch/histories/:id/cancel（角色闸，挂真实 AdminAuthMiddleware 而不是桩）

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	bopCtrlTenantA = uint(71)
	bopCtrlTenantB = uint(72)
	bopCtrlUID     = uint(830101)
)

func bopCtrlDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.BatchOperationHistory{})
	if database == nil {
		t.Fatal("测试库不可达（不 Skip，跳过等于没锁）")
	}
	db.SetTestDB(database)
	return database
}

func bopCtrlSeed(t *testing.T, database *gorm.DB, tenantID uint, status string) *model.BatchOperationHistory {
	t.Helper()
	row := &model.BatchOperationHistory{
		TenantID:      tenantID,
		OperationType: "export",
		DataType:      "clue",
		Status:        status,
		TotalCount:    7,
		StartedAt:     time.Now(),
	}
	scope := database.WithContext(tenant.WithSystemScope(context.Background()))
	if err := scope.Create(row).Error; err != nil {
		t.Fatalf("预置历史失败: %v", err)
	}
	return row
}

// bopCtrlRouter bindTenant 模拟「多租户中间件接上了 / 没接上」两种装配状态。
func bopCtrlRouter(t *testing.T, bindTenant bool) *gin.Engine {
	t.Helper()
	if middleware.IsTestMode {
		t.Fatal("IsTestMode 为真时 AdminAuthMiddleware 会无条件放行 admin，本文件的 403 断言将失去意义")
	}
	ctrl := newTestBatchOperationController()
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		if uid, err := strconv.ParseUint(ctx.GetHeader("X-Test-Uid"), 10, 32); err == nil && uid != 0 {
			ctx.Set("user_id", uint(uid))
		}
		if role := ctx.GetHeader("X-Test-Role"); role != "" {
			ctx.Set("role", role)
		}
		if bindTenant {
			ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), bopCtrlTenantA))
		}
		ctx.Next()
	})

	auth := router.Group("/batch")
	auth.GET("/histories", ctrl.GetHistories)
	auth.GET("/histories/:id", ctrl.GetHistoryByID)

	admin := router.Group("/batch", middleware.AdminAuthMiddleware())
	admin.POST("/histories/:id/cancel", ctrl.CancelHistory)
	return router
}

func bopCtrlCall(router *gin.Engine, method, path, uid, role string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, nil)
	req.Header.Set("X-Test-Uid", uid)
	req.Header.Set("X-Test-Role", role)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestBatchHistoryDetailSeparatesMissingFromUnboundTenant 详情口：404 只留给「真的没有」。
func TestBatchHistoryDetailSeparatesMissingFromUnboundTenant(t *testing.T) {
	database := bopCtrlDB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	own := strconv.FormatUint(uint64(bopCtrlUID), 10)

	// 装配缺失（ctx 没绑租户）：必须是 500，不能披上 404「历史记录不存在」的外衣。
	unbound := bopCtrlRouter(t, false)
	row := bopCtrlSeed(t, database, bopCtrlTenantA, "success")
	w := bopCtrlCall(unbound, http.MethodGet, "/batch/histories/"+strconv.FormatUint(uint64(row.ID), 10), own, "user")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("租户上下文未绑定应回 500，实得 %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "不存在") {
		t.Errorf("配置级故障被说成「记录不存在」：%s", w.Body.String())
	}

	bound := bopCtrlRouter(t, true)
	if w := bopCtrlCall(bound, http.MethodGet, "/batch/histories/"+strconv.FormatUint(uint64(row.ID), 10), own, "user"); w.Code != http.StatusOK {
		t.Fatalf("本租户详情应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	// 别人的行：遮挡成 404（这是「不存在」的正确用法）。
	foreign := bopCtrlSeed(t, database, bopCtrlTenantB, "success")
	if w := bopCtrlCall(bound, http.MethodGet, "/batch/histories/"+strconv.FormatUint(uint64(foreign.ID), 10), own, "user"); w.Code != http.StatusNotFound {
		t.Errorf("读别租户历史应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := bopCtrlCall(bound, http.MethodGet, "/batch/histories/99999999", own, "user"); w.Code != http.StatusNotFound {
		t.Errorf("不存在的 id 应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
}

// TestBatchHistoryListIsTenantScopedAtHTTPBoundary 列表口按租户收窄，且装配缺失同样不静默回空。
func TestBatchHistoryListIsTenantScopedAtHTTPBoundary(t *testing.T) {
	database := bopCtrlDB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	bopCtrlSeed(t, database, bopCtrlTenantA, "success")
	bopCtrlSeed(t, database, bopCtrlTenantA, "running")
	bopCtrlSeed(t, database, bopCtrlTenantB, "success")

	own := strconv.FormatUint(uint64(bopCtrlUID), 10)
	w := bopCtrlCall(bopCtrlRouter(t, true), http.MethodGet, "/batch/histories?page=1&page_size=20", own, "user")
	var payload struct {
		Data struct {
			List  []model.BatchOperationHistory `json:"list"`
			Total int64                         `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析历史列表失败: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Total != 2 || len(payload.Data.List) != 2 {
		t.Errorf("A 租户应看到 2 条，实得 total=%d len=%d", payload.Data.Total, len(payload.Data.List))
	}

	if w := bopCtrlCall(bopCtrlRouter(t, false), http.MethodGet, "/batch/histories", own, "user"); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户时列表应 500 而不是空列表，实得 %d body=%s", w.Code, w.Body.String())
	}
}

// TestBatchHistoryCancelIsRoleGated 取消口只挂 admin 组：归属人自己（role=user）也得被角色闸拦下。
func TestBatchHistoryCancelIsRoleGated(t *testing.T) {
	database := bopCtrlDB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	own := strconv.FormatUint(uint64(bopCtrlUID), 10)
	running := bopCtrlSeed(t, database, bopCtrlTenantA, "running")
	path := "/batch/histories/" + strconv.FormatUint(uint64(running.ID), 10) + "/cancel"

	if w := bopCtrlCall(bopCtrlRouter(t, true), http.MethodPost, path, own, "user"); w.Code != http.StatusForbidden {
		t.Errorf("普通用户打管理口应 403，实得 %d body=%s", w.Code, w.Body.String())
	}
	var after model.BatchOperationHistory
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).First(&after, running.ID).Error; err != nil {
		t.Fatalf("物理回读失败: %v", err)
	}
	if after.Status != "running" {
		t.Errorf("被 403 拦下后状态不得改变，实得 %q", after.Status)
	}

	// 管理员 + 本租户：取消生效（改前这里被服务层的 context.Background() 撞成 500）。
	if w := bopCtrlCall(bopCtrlRouter(t, true), http.MethodPost, path, own, "admin"); w.Code != http.StatusOK {
		t.Fatalf("管理员取消本租户操作应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).First(&after, running.ID).Error; err != nil {
		t.Fatalf("物理回读失败: %v", err)
	}
	if after.Status != "cancelled" {
		t.Errorf("取消后应为 cancelled，实得 %q", after.Status)
	}
}
