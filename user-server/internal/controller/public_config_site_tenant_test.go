package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
)

// 回归：线上 SaaS 登录页看不到「注册商户账号」入口。
//
// 现象：https://work.xapptool.cn/login 页面上没有注册链接。
// 链路：Login.vue 读 GET /api/public/config 的 enable_register 决定要不要渲染入口；
// 该接口在 **on 模式且请求不带 tenant_code** 时，走 resolvePublicTenantCtx →
// ResolvePublicTenantID("") → 报「缺少租户短码」⇒ 控制器回保守值
// enable_register=false ⇒ 前端隐藏入口。
//
// 为什么这是缺陷而不该照现状：注册是**新商户自助开户**（POST /api/tenant/register），
// 开户时人还没有任何租户短码 —— 让入口依赖"已有租户短码"是死结。站点自己的归属租户
// （官网站点 = 四川同咨科技有限公司，code=tz3rmdj）就是应该提供这个短码的那一方。
//
// 修法：加一个部署期配置 SITE_TENANT_CODE（站点默认租户短码），
// 公开配置在**请求没带短码**时用它兜底 —— 于是匿名访问也能拿到该站点的
// 站点名与注册开关；显式带短码时仍以请求为准（S 租户看到的还是 S 租户的配置）。
func TestPublicConfig_FallsBackToSiteTenantCode(t *testing.T) {
	orig := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(orig)

	setupTestControllerDB(t)
	// setupTestControllerDB 的建表清单里没有 system_config（公开配置读它），
	// 这里补建 —— 表不在 ⇒ 读配置必失败 ⇒ 拿不到 enable_register。
	// AutoMigrate 也要带租户上下文：system_config 是租户表，无租户的建表被隔离闸拦。
	if err := db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), 1)).
		AutoMigrate(&model.SystemConfig{}); err != nil {
		t.Fatalf("建 system_config 表失败: %v", err)
	}
	// 手动建表发生在 NewTestDB 的登记表快照之后：登记表是一次性内省，
	// 不重建的话 system_config 不在租户登记表内 ⇒ 隔离 callback 判它为
	// 非租户表不注入 WHERE ⇒ 测试读到跨租户配置行（实测踩中：显式短码
	// 请求读到了站点租户的行）。InitTenantRegistry 幂等，重建即可。
	if err := tenancyidx.InitTenantRegistry(db.GetDB()); err != nil {
		t.Fatalf("重建租户登记表失败: %v", err)
	}

	// 站点归属租户：官网域名的主体（四川同咨科技有限公司）
	if err := db.GetDB().Create(&model.Tenant{
		ID:     18,
		Code:   "tz3rmdj",
		Name:   "四川同咨科技有限公司",
		Status: model.TenantStatusActive,
	}).Error; err != nil {
		t.Fatalf("建租户失败: %v", err)
	}
	// 该租户的站点配置：开放注册
	if err := db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), 18)).Create(&model.SystemConfig{
		Name:           "四川同咨科技有限公司",
		EnableRegister: true,
		EnableRAG:      true,
	}).Error; err != nil {
		t.Fatalf("建站点配置失败: %v", err)
	}

	t.Setenv("SITE_TENANT_CODE", "tz3rmdj")

	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.GET("/public/config", authCtrl.PublicConfig)

	// 匿名访问（不带 tenant_code）⇒ 应回落到站点租户，给出 enable_register=true
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/config", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			SiteName       string `json:"site_name"`
			EnableRegister bool   `json:"enable_register"`
			TenantMode     bool   `json:"tenant_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	if !resp.Data.EnableRegister {
		t.Errorf("匿名访问应读到站点租户的注册开关（enable_register=true），实际 false —— 登录页会因此隐藏注册入口")
	}
	if resp.Data.SiteName != "四川同咨科技有限公司" {
		t.Errorf("站点名应为站点租户的名字，实际 %q", resp.Data.SiteName)
	}
}

// 显式带 tenant_code 时以请求为准：S 租户不能读到别人的站点配置。
//
// 这是修法的另一半边界：兜底只在**请求没给短码**时生效；给了就必须用给的，
// 否则 S 租户会读到站点租户的注册开关（原本要防的就是这个）。
func TestPublicConfig_ExplicitTenantCodeWinsOverSiteFallback(t *testing.T) {
	orig := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(orig)

	setupTestControllerDB(t)

	// setupTestControllerDB 的建表清单里没有 system_config（公开配置读它），补建。
	// AutoMigrate 也要带租户上下文：system_config 是租户表，无租户的建表被隔离闸拦。
	if err := db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), 1)).
		AutoMigrate(&model.SystemConfig{}); err != nil {
		t.Fatalf("建 system_config 表失败: %v", err)
	}
	// 手动建表发生在 NewTestDB 的登记表快照之后：登记表是一次性内省，
	// 不重建的话 system_config 不在租户登记表内 ⇒ 隔离 callback 判它为
	// 非租户表不注入 WHERE ⇒ 测试读到跨租户配置行（实测踩中：显式短码
	// 请求读到了站点租户的行）。InitTenantRegistry 幂等，重建即可。
	if err := tenancyidx.InitTenantRegistry(db.GetDB()); err != nil {
		t.Fatalf("重建租户登记表失败: %v", err)
	}

	// 清掉可能残留的站点配置（测试库跨用例复用，唯一约束 uni_system_config_tenant 会撞）。
	// 删除要带租户上下文：system_config 是租户表，无租户的删除会被隔离闸拒掉。
	for _, tid := range []uint{18, 21} {
		_ = db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), tid)).
			Where("tenant_id = ?", tid).Delete(&model.SystemConfig{}).Error
	}

	// 站点租户：开放注册
	if err := db.GetDB().Create(&model.Tenant{
		ID:     18,
		Code:   "tz3rmdj",
		Name:   "四川同咨科技有限公司",
		Status: model.TenantStatusActive,
	}).Error; err != nil {
		t.Fatalf("建站点租户失败: %v", err)
	}
	if err := db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), 18)).Create(&model.SystemConfig{
		Name:           "四川同咨科技有限公司",
		EnableRegister: true,
	}).Error; err != nil {
		t.Fatalf("建站点配置失败: %v", err)
	}
	// 另一个租户：关闭注册
	if err := db.GetDB().Create(&model.Tenant{
		ID:     21,
		Code:   "tgzgc8d",
		Name:   "上海云启智数科技有限公司",
		Status: model.TenantStatusActive,
	}).Error; err != nil {
		t.Fatalf("建第二个租户失败: %v", err)
	}
	// EnableRegister=false 是 bool 零值，GORM 的 Create 会省略零值字段 ⇒ 写进去的是 true。
	// 用 map 显式写列，才能造出"该租户关闭注册"这个前提。
	if err := db.GetDB().WithContext(tenant.OnlyTenant(context.Background(), 21)).
		Table("system_config").Create(map[string]any{
		"tenant_id":       21,
		"name":            "上海云启智数科技有限公司",
		"enable_register": false,
		"enable_rag":      true,
	}).Error; err != nil {
		t.Fatalf("建第二个站点配置失败: %v", err)
	}

	t.Setenv("SITE_TENANT_CODE", "tz3rmdj")

	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.GET("/public/config", authCtrl.PublicConfig)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/config?tenant_code=tgzgc8d", nil)
	router.ServeHTTP(w, req)

	var resp struct {
		Data struct {
			SiteName       string `json:"site_name"`
			EnableRegister bool   `json:"enable_register"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	if resp.Data.EnableRegister {
		t.Error("显式带 tenant_code 时应以请求租户为准（该租户关闭注册），不该被站点兜底覆盖成 true")
	}
	if resp.Data.SiteName != "上海云启智数科技有限公司" {
		t.Errorf("站点名应为请求租户的名字，实际 %q", resp.Data.SiteName)
	}
}

// 站点租户短码配错时不能把注册入口打开成"假开"：查不到租户就按保守值回。
func TestPublicConfig_UnknownSiteTenantCodeKeepsRegisterClosed(t *testing.T) {
	orig := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(orig)

	setupTestControllerDB(t)
	t.Setenv("SITE_TENANT_CODE", "no_such_code")

	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.GET("/public/config", authCtrl.PublicConfig)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/config", nil)
	router.ServeHTTP(w, req)

	var resp struct {
		Data struct {
			EnableRegister bool `json:"enable_register"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	if resp.Data.EnableRegister {
		t.Error("站点租户短码查不到时应按保守值回（注册关闭），不该假开")
	}
	_ = os.Getenv("SITE_TENANT_CODE")
}
