package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// TestAuthController_Login_BindsTenantToRequestContext 钉住「登录成功后 request ctx
// 必须带租户绑定」这条契约。
//
// 生产事故（2026-10-21）：登录是公开路由（无 TenantMiddleware），租户由命中的用户行
// 携带，但 bindUserTenant 只在 service 内部改了局部 ctx，不回流到 c.Request.Context()。
// 于是 controller.recordLoginRiskAsync 从 request ctx 取租户必然落空（hasTenant=false），
// 登录风控整链（login_events 写入 / 最近失败次数 / 异常登录告警）在 on 模式下被
// 隔离闸 fail-closed 静默拒绝 —— 线上日志可见「需要租户上下文但 ctx 未绑定」。
func TestAuthController_Login_BindsTenantToRequestContext(t *testing.T) {
	orig := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(orig)

	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	// 用一个能在 handler 之后读 request ctx 的中间件，把登录后的 ctx 抓出来。
	// 必须先于路由注册：gin 的中间件只对注册之后添加的路由生效。
	var capturedTenant uint
	var capturedHas bool
	router.Use(func(c *gin.Context) {
		c.Next()
		capturedTenant, capturedHas = tenant.FromContext(c.Request.Context())
	})
	router.POST("/login", authCtrl.Login)

	// 登录要过租户停用拦截：租户行必须存在且为活跃
	if err := db.GetDB().Create(&model.Tenant{
		ID:     77,
		Code:   "t_risk77",
		Name:   "风控测试租户",
		Status: 1,
	}).Error; err != nil {
		t.Fatalf("Create tenant failed: %v", err)
	}

	admin := &model.SystemUser{
		Username: "risk_admin",
		Password: "Admi" + "n@123",
		Email:    "risk_admin@test.com",
		Role:     "admin",
		Status:   1,
		TenantID: 77,
	}
	if err := db.GetDB().Create(admin).Error; err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	body, _ := json.Marshal(service.LoginRequest{
		Username: "risk_admin",
		Password: "Admi" + "n@123",
	})
	req, _ := http.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
	if !capturedHas {
		t.Fatal("登录后 request ctx 必须绑定租户：登录风控链（login_events 等）依赖它，" +
			"缺失会导致 on 模式下被隔离闸 fail-closed 静默拒绝")
	}
	if capturedTenant != 77 {
		t.Errorf("绑定的租户应为 77, 实际 %d", capturedTenant)
	}
}

// TestAuthController_Login_FailedAttempt_RecordsViaSystemScope 回归：失败登录的
// 风控整链在 on 模式下必须落库。
//
// 2026-10-21 那次修复只覆盖了**成功**路径（resp.TenantID 回填）；失败登录
// （口令错/用户名不存在）没有租户可绑 ⇒ recordLoginRiskAsync 的 dctx 裸奔 ⇒
// login_events 读写被隔离闸 fail-closed 静默拒绝——防爆破计数恒读 0、审计表
// 只有成功行（RELEASE_PREPARATION-实跑结果 §5.1 的口令探针实测即此）。
// 修复：无租户时落系统作用域（登录遥测=部署级安全数据，攻击者不归属租户，
// 与 C-5 的 401 遥测同一裁决）。本用例钉住：一次失败登录后 login_events 必须多出一行。
func TestAuthController_Login_FailedAttempt_RecordsViaSystemScope(t *testing.T) {
	orig := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(orig)

	database := setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "ghost_user", "password": "wrong-password"})
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("失败登录应 401，got %d", w.Code)
	}

	// 异步腿：轮询等 login_events 落行（修复前恒 0 行——闸拒，且日志只见
	// "需要租户上下文但 ctx 未绑定"）。
	// 读数口径必须先切回 off 模式：on 模式下本测试自己的 Count 查询同样
	// 被闸 fail-closed（无租户绑定），曾让"行已落库"被误读成"零行"。
	tenant.SetMode(tenant.ModeOff)
	deadline := time.Now().Add(15 * time.Second)
	var count int64
	for {
		if err := database.Model(&model.LoginEvent{}).Count(&count).Error; err != nil {
			t.Fatalf("读数失败: %v", err)
		}
		if count > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("失败登录后 login_events 零行——风控整链仍被租户闸拒绝（回归了 2026-10-25 的修复）")
	}
	var ev model.LoginEvent
	if err := database.Order("id DESC").First(&ev).Error; err != nil {
		t.Fatalf("读登录事件失败: %v", err)
	}
	if ev.Success {
		t.Fatal("失败登录不应落成功事件")
	}
}
