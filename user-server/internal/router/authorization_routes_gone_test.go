package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// TestNoLicenseRoutes 守护"hivemtk 无授权流程"这条产品口径落在路由层的结果：
// 引擎里不允许出现任何 /license 路径。
//
// 背景：/api/license/status 与 /api/license/features 曾在开源版里返回
// {"licensed": true, "message": "开源版无需授权"} —— 一个恒真的鉴权端点。
// 它拦不住任何人，却给前端留了个"授权即将到期"的告警区块可以亮起来，
// 2026-09 官网/平台线上域下线后彻底成为残留，已连同 handler 一起删除。
// 这里断言"不存在"而不是删掉测试，是为了防止有人从旧分支把 endpoint 抄回来。
func TestNoLicenseRoutes(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	for _, ri := range r.Routes() {
		if strings.Contains(strings.ToLower(ri.Path), "/license") {
			t.Errorf("路由表里仍有授权端点：%s %s（开源版不应存在鉴权路由）", ri.Method, ri.Path)
		}
	}

	for _, p := range []string{"/api/license/status", "/api/license/features"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s 应 404（端点已删除），实际 %d，body=%s", p, w.Code, w.Body.String())
		}
	}
}

// TestPublicMerchantRegisterRouteRemoved 钉住"/api/platform/register 不再是匿名公开路由"。
//
// 它曾是 public 组里唯一会触发对外写操作的端点（〔🔓〕：任何人不登录即可 POST 一份
// 商户名/联系人/设备信息，让本机去平台侧注册成商户）。商户身份现在只在
// PLATFORM_ENABLED=true 时由 platform.InitSync() 在进程内自动注册，
// 不需要一个公网可写的入口；handler 与它的错误路径测试保留，路由不再注册。
func TestPublicMerchantRegisterRouteRemoved(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	for _, ri := range r.Routes() {
		if ri.Path == "/api/platform/register" {
			t.Errorf("匿名商户注册路由又回来了：%s %s", ri.Method, ri.Path)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/platform/register", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("POST /api/platform/register 应 404（不再注册该路由），实际 %d body=%s", w.Code, w.Body.String())
	}
}
