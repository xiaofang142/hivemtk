package controller

// 契约锁（实体审查 #16，HTTP 出口层）。
//
// 改前这一层有三处会直接影响调用方看得到的东西：
//   1. `GetTemplateByID` 把服务层任何 error 一律回 404 且 `err.Error()` 原样吐给客户端——
//      租户闸故障（「[tenant] …ctx 未绑定」）既披上「模板不存在」的外衣，又把内部实现细节写进响应体；
//      列表/官方/搜索/我的下载四个口同样把 err.Error() 直接回显。
//   2. `DownloadTemplate` 里 `userID.(uint)` 是不带 ok 的断言：团队侧 JWT 中间件存的是 float64/string，
//      身份类型一换就是 panic；没取到 user_id 时 `nil.(uint)` 同样 panic。
//   3. 「我的下载」不认用户，回的是本租户全员流水。
// 挂载口径按 `business_routes.go:151-161` 与 `frontend_aliases.go:320-328` 的实际形状钉：
//   auth 组（无角色闸）：GET/POST /templates…、GET /market-templates…（别名同 handler）

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	mtcTenantA   = uint(95)
	mtcTenantB   = uint(96)
	mtcUserA     = uint(920101)
	mtcUserB     = uint(920102)
	mtcTeamUserF = 920103 // 团队侧中间件把 user_id 存成 float64
)

func mtcDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MarketTemplate{}, &model.MarketTemplateDownload{})
	if database == nil {
		t.Fatal("测试库不可达（不 Skip，跳过等于没锁）")
	}
	db.SetTestDB(database)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	return database
}

func mtcSeedTemplate(t *testing.T, database *gorm.DB, tenantID uint, name string) *model.MarketTemplate {
	t.Helper()
	row := &model.MarketTemplate{
		TenantID:   tenantID,
		Name:       name,
		Category:   "dashboard",
		Type:       "flow",
		IsOfficial: true,
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置模板 %s 失败: %v", name, err)
	}
	return row
}

func mtcPhysicalDownloads(t *testing.T, database *gorm.DB, templateID uint) []*model.MarketTemplateDownload {
	t.Helper()
	var rows []*model.MarketTemplateDownload
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("template_id = ?", templateID).Find(&rows).Error; err != nil {
		t.Fatalf("物理读取下载流水失败: %v", err)
	}
	return rows
}

// mtcRouter 复刻真实挂载。uidKind 区分两种中间件写入的身份类型：
// "uint"（用户侧 JWT）与 "float64"（团队侧 JWT）；none 表示压根没有身份。
func mtcRouter(t *testing.T, bindTenant bool) *gin.Engine {
	t.Helper()
	ctrl := newTestTemplateMarketController()
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(ctx *gin.Context) {
		raw := ctx.GetHeader("X-Test-Uid-Kind")
		idStr := ctx.GetHeader("X-Test-Uid")
		switch raw {
		case "float64":
			if f, err := strconv.ParseFloat(idStr, 64); err == nil {
				ctx.Set("user_id", f)
			}
		case "uint":
			if uid, err := strconv.ParseUint(idStr, 10, 32); err == nil && uid != 0 {
				ctx.Set("user_id", uint(uid))
			}
		}
		if bindTenant {
			ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), mtcTenantA))
		}
		ctx.Next()
	})

	auth := router.Group("/templates")
	auth.GET("", ctrl.GetTemplateList)
	auth.GET("/official", ctrl.GetOfficialTemplates)
	auth.GET("/search", ctrl.SearchTemplates)
	auth.GET("/my-downloads", ctrl.GetMyDownloads)
	auth.GET("/:id", ctrl.GetTemplateByID)
	auth.POST("", ctrl.CreateTemplate)
	auth.POST("/:id/download", ctrl.DownloadTemplate)
	auth.POST("/:id/rate", ctrl.RateTemplate)
	return router
}

func mtcCall(router *gin.Engine, method, path, uid, kind string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, nil)
	req.Header.Set("X-Test-Uid", uid)
	req.Header.Set("X-Test-Uid-Kind", kind)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestMarketTemplateDetailSeparatesMissingFromUnboundTenant 详情口：404 只留给「真的没有」，
// 且任何内部错误文本都不得回显给客户端。
func TestMarketTemplateDetailSeparatesMissingFromUnboundTenant(t *testing.T) {
	database := mtcDB(t)
	own := strconv.FormatUint(uint64(mtcUserA), 10)
	row := mtcSeedTemplate(t, database, mtcTenantA, "本家模板")

	unbound := mtcRouter(t, false)
	w := mtcCall(unbound, http.MethodGet, "/templates/"+strconv.FormatUint(uint64(row.ID), 10), own, "uint")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("租户上下文未绑定应回 500，实得 %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "不存在") {
		t.Errorf("配置级故障被说成「模板不存在」：%s", body)
	}
	if strings.Contains(body, "租户上下文") || strings.Contains(body, "tenant") {
		t.Errorf("内部租户错误原文回显给客户端：%s", body)
	}

	bound := mtcRouter(t, true)
	if w := mtcCall(bound, http.MethodGet, "/templates/"+strconv.FormatUint(uint64(row.ID), 10), own, "uint"); w.Code != http.StatusOK {
		t.Fatalf("本租户详情应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	foreign := mtcSeedTemplate(t, database, mtcTenantB, "别家模板")
	if w := mtcCall(bound, http.MethodGet, "/templates/"+strconv.FormatUint(uint64(foreign.ID), 10), own, "uint"); w.Code != http.StatusNotFound {
		t.Errorf("读别租户模板应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := mtcCall(bound, http.MethodGet, "/templates/99999999", own, "uint"); w.Code != http.StatusNotFound {
		t.Errorf("不存在的 id 应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
}

// TestMarketTemplateListReadsDoNotEchoInternals 三个列表口：装配缺失回 500 且不回显原文。
func TestMarketTemplateListReadsDoNotEchoInternals(t *testing.T) {
	database := mtcDB(t)
	mtcSeedTemplate(t, database, mtcTenantA, "A 家一号")
	mtcSeedTemplate(t, database, mtcTenantA, "A 家二号")
	mtcSeedTemplate(t, database, mtcTenantB, "B 家一号")

	own := strconv.FormatUint(uint64(mtcUserA), 10)

	w := mtcCall(mtcRouter(t, true), http.MethodGet, "/templates?page=1&page_size=20", own, "uint")
	var payload struct {
		Data struct {
			List  []model.MarketTemplate `json:"list"`
			Total int64                  `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析模板列表失败: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Total != 2 || len(payload.Data.List) != 2 {
		t.Errorf("A 租户只应看到自己的 2 条，实得 total=%d len=%d", payload.Data.Total, len(payload.Data.List))
	}

	if w := mtcCall(mtcRouter(t, true), http.MethodGet, "/templates/search?keyword=B+家", own, "uint"); w.Code != http.StatusOK {
		t.Fatalf("搜索应 200，实得 %d body=%s", w.Code, w.Body.String())
	} else if body := w.Body.String(); strings.Contains(body, "B 家") {
		t.Errorf("别租户模板经搜索泄漏：%s", body)
	}

	for _, path := range []string{"/templates", "/templates/official", "/templates/search?keyword=A"} {
		if w := mtcCall(mtcRouter(t, false), http.MethodGet, path, own, "uint"); w.Code != http.StatusInternalServerError {
			t.Errorf("未绑定租户时 %s 应 500 而不是空列表，实得 %d body=%s", path, w.Code, w.Body.String())
		} else if strings.Contains(w.Body.String(), "租户上下文") {
			t.Errorf("未绑定租户时 %s 回显了内部错误原文：%s", path, w.Body.String())
		}
	}
}

// TestMarketTemplateDownloadAcceptsTeamIdentity 下载口不得因身份类型 panic，且流水必须带归属。
func TestMarketTemplateDownloadAcceptsTeamIdentity(t *testing.T) {
	database := mtcDB(t)
	row := mtcSeedTemplate(t, database, mtcTenantA, "可下载模板")
	path := "/templates/" + strconv.FormatUint(uint64(row.ID), 10) + "/download"

	bound := mtcRouter(t, true)

	// 团队侧中间件写的是 float64：改前 userID.(uint) 直接 panic（gin.Recovery 兜成 500）
	w := mtcCall(bound, http.MethodPost, path, strconv.FormatUint(mtcTeamUserF, 10), "float64")
	if w.Code != http.StatusOK {
		t.Fatalf("float64 身份下载本租户模板应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	rows := mtcPhysicalDownloads(t, database, row.ID)
	if len(rows) != 1 || rows[0].UserID != mtcTeamUserF || rows[0].TenantID != mtcTenantA {
		t.Errorf("下载流水应恰好 1 条、归属该用户与 A 租户，实得 %+v", rows)
	}

	// 完全没有身份：401，不是 panic
	if w := mtcCall(bound, http.MethodPost, path, "", "none"); w.Code != http.StatusUnauthorized {
		t.Errorf("无身份下载应 401，实得 %d body=%s", w.Code, w.Body.String())
	}

	// 别租户模板：404，且不给别租户留下流水
	own := strconv.FormatUint(uint64(mtcUserA), 10)
	foreign := mtcSeedTemplate(t, database, mtcTenantB, "别家模板")
	if w := mtcCall(bound, http.MethodPost, "/templates/"+strconv.FormatUint(uint64(foreign.ID), 10)+"/download", own, "uint"); w.Code != http.StatusNotFound {
		t.Errorf("下载别租户模板应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if rows := mtcPhysicalDownloads(t, database, foreign.ID); len(rows) != 0 {
		t.Errorf("别租户模板被写了下载流水：%+v", rows)
	}

	// 装配缺失：500 且不回显原文
	if w := mtcCall(mtcRouter(t, false), http.MethodPost, path, own, "uint"); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户时下载应 500，实得 %d body=%s", w.Code, w.Body.String())
	} else if strings.Contains(w.Body.String(), "租户上下文") {
		t.Errorf("未绑定租户时下载回显了内部错误原文：%s", w.Body.String())
	}
}

// TestMarketTemplateMyDownloadsIsPerUser 「我的下载」在出口层按用户裁剪。
func TestMarketTemplateMyDownloadsIsPerUser(t *testing.T) {
	database := mtcDB(t)
	row := mtcSeedTemplate(t, database, mtcTenantA, "共用模板")

	for _, uid := range []uint{mtcUserA, mtcUserA, mtcUserB} {
		w := mtcCall(mtcRouter(t, true), http.MethodPost,
			"/templates/"+strconv.FormatUint(uint64(row.ID), 10)+"/download",
			strconv.FormatUint(uint64(uid), 10), "uint")
		if w.Code != http.StatusOK {
			t.Fatalf("用户 %d 下载失败: %d body=%s", uid, w.Code, w.Body.String())
		}
	}

	var payload struct {
		Data struct {
			List  []model.MarketTemplateDownload `json:"list"`
			Total int64                          `json:"total"`
		} `json:"data"`
	}
	w := mtcCall(mtcRouter(t, true), http.MethodGet, "/templates/my-downloads?page=1&page_size=10",
		strconv.FormatUint(uint64(mtcUserA), 10), "uint")
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析下载流水失败: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Total != 2 || len(payload.Data.List) != 2 {
		t.Errorf("A 用户只应看到自己 2 条流水，实得 total=%d len=%d", payload.Data.Total, len(payload.Data.List))
	}

	if w := mtcCall(mtcRouter(t, true), http.MethodGet, "/templates/my-downloads",
		strconv.FormatUint(uint64(mtcUserB), 10), "uint"); w.Code != http.StatusOK {
		t.Fatalf("B 用户读取失败: %d", w.Code)
	} else if body := w.Body.String(); strings.Count(body, `"template_id":`+strconv.FormatUint(uint64(row.ID), 10)) != 1 {
		t.Errorf("B 用户应只看到自己 1 条流水，实得 body=%s", body)
	}

	if w := mtcCall(mtcRouter(t, true), http.MethodGet, "/templates/my-downloads", "", "none"); w.Code != http.StatusUnauthorized {
		t.Errorf("无身份读「我的下载」应 401，实得 %d body=%s", w.Code, w.Body.String())
	}
}
