package controller

// 契约锁（实体审查 #19，HTTP 出口层，TENANT_MODE=on）。
//
// 话术模板这条链（script_templates / script_categories / script_recommendations）
// 改前的故障全部只在 on 模式现形：
//   1. 三个仓储 15 个方法与服务 11 个方法整层没有 context.Context 形参，
//      仓储拿的是裸 handle ⇒ on 模式每一次读写都撞
//      `[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定` 的 fail-closed 闸，
//      business_routes.go:165-175 的 10 条挂载与 frontend_aliases.go:192-199 的
//      7 条别名全部回 500。
//   2. 详情口把闸的报错当成「话术不存在」回 404，并把 err.Error() 原文回显；
//      列表/搜索/公开/分类/推荐五个口直接把 err.Error() 拼进 500 文案。
//   3. 按主键的读写口没有归属门：ctx 一旦接上，同租户互改互读不在话下，
//      跨租户则靠闸挡读——但 SyncToLibrary 走的是裸 handle，读写都不带租户，
//      把别租户同名 (category,title) 的话术覆盖进运行时话术库，
//      新建的 script_library 行落进 tenant_id 列默认值 1（默认租户）。
//   4. CreateTemplate 用 userID.(uint) 无条件断言：user_id 缺席时是 nil 断言，
//      panic 被 Recovery 吃成一个 500，看不出是「没登录」还是「服务坏了」。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contentmodel "hivemtk-user/internal/content/model"
	contentrepo "hivemtk-user/internal/content/repository"
	contentservice "hivemtk-user/internal/content/service"
	mainmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	scrTenantA = uint(131)
	scrTenantB = uint(132)
	scrUserA   = uint(950101)
)

func scrDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&contentmodel.ScriptTemplate{},
		&contentmodel.ScriptCategory{},
		&mainmodel.ScriptLibrary{},
	)
	if database == nil {
		t.Fatal("测试库不可达（不 Skip，跳过等于没锁）")
	}
	db.SetTestDB(database)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	return database
}

// scrSeedTemplate 经 SystemScope 预置：夹具要跨租户建行，业务写口本身是被测对象。
func scrSeedTemplate(t *testing.T, database *gorm.DB, tenantID uint, category, title, content string) *contentmodel.ScriptTemplate {
	t.Helper()
	row := &contentmodel.ScriptTemplate{
		TenantID: tenantID,
		Category: category,
		Name:     title,
		Title:    title,
		Content:  content,
		IsPublic: true,
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置话术 %s 失败: %v", title, err)
	}
	return row
}

func scrSeedCategory(t *testing.T, database *gorm.DB, tenantID uint, name string) *contentmodel.ScriptCategory {
	t.Helper()
	row := &contentmodel.ScriptCategory{TenantID: tenantID, Name: name, SortOrder: 1}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置分类 %s 失败: %v", name, err)
	}
	return row
}

func scrPhysicalTemplateCount(t *testing.T, database *gorm.DB, id uint) int64 {
	t.Helper()
	var n int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&contentmodel.ScriptTemplate{}).Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("物理计数话术 %d 失败: %v", id, err)
	}
	return n
}

func scrPhysicalLibrary(t *testing.T, database *gorm.DB, category, title string) []*mainmodel.ScriptLibrary {
	t.Helper()
	var rows []*mainmodel.ScriptLibrary
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("category = ? AND title = ?", category, title).Find(&rows).Error; err != nil {
		t.Fatalf("物理读取运行时话术库失败: %v", err)
	}
	return rows
}

// scrRouter 复刻真实挂载顺序（business_routes.go:165-175）：
// :id 在前、静态段在后，别名层同样按这个次序注册，锁就得按生产次序立。
func scrRouter(t *testing.T, bindTenant bool) *gin.Engine {
	t.Helper()
	ctrl := newTestScriptTemplateController()
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(ctx *gin.Context) {
		if bindTenant {
			ctx.Set("user_id", scrUserA)
			ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), scrTenantA))
		}
		ctx.Next()
	})
	auth := router.Group("")
	auth.GET("/scripts", ctrl.GetTemplateList)
	auth.GET("/scripts/:id", ctrl.GetTemplateByID)
	auth.POST("/scripts", ctrl.CreateTemplate)
	auth.PUT("/scripts/:id", ctrl.UpdateTemplate)
	auth.DELETE("/scripts/:id", ctrl.DeleteTemplate)
	auth.GET("/scripts/categories", ctrl.GetCategories)
	auth.GET("/scripts/search", ctrl.SearchTemplates)
	auth.GET("/scripts/public", ctrl.GetPublicTemplates)
	auth.POST("/scripts/recommend", ctrl.RecommendScript)
	auth.POST("/scripts/sync-to-library", ctrl.SyncToLibrary)
	return router
}

func scrCall(router *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body == nil {
		req, _ = http.NewRequest(method, path, nil)
	} else {
		req, _ = http.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// scrEchoCheck 内部错误原文不许出现在任何出口响应里。
func scrEchoCheck(t *testing.T, w *httptest.ResponseRecorder, what string) {
	t.Helper()
	body := w.Body.String()
	if strings.Contains(body, "[tenant]") || strings.Contains(body, "租户上下文") || strings.Contains(body, "SQLSTATE") || strings.Contains(body, "gorm") {
		t.Errorf("%s 回显了内部错误原文：%s", what, body)
	}
}

// TestScriptReadsAreTenantFactored on 模式绑定租户后，6 个读口必须回 200。
// 改前：整层没有 ctx ⇒ 每一口都被 fail-closed 闸拒成 500，且 500 文案里
// 原样躺着闸的报错原文。
func TestScriptReadsAreTenantFactored(t *testing.T) {
	database := scrDB(t)
	scrSeedTemplate(t, database, scrTenantA, "sales", "本租户话术", "本租户内容")
	scrSeedCategory(t, database, scrTenantA, "本租户分类")
	router := scrRouter(t, true)

	own := scrSeedTemplate(t, database, scrTenantA, "sales", "详情用话术", "详情内容")
	cases := []struct{ method, path, what string }{
		{http.MethodGet, "/scripts", "本租户话术列表"},
		{http.MethodGet, "/scripts/categories", "本租户分类列表"},
		{http.MethodGet, "/scripts/search?keyword=本租户", "本租户话术搜索"},
		{http.MethodGet, "/scripts/public", "本租户公开话术"},
		{http.MethodGet, fmt.Sprintf("/scripts/%d", own.ID), "本租户话术详情"},
	}
	for _, tc := range cases {
		w := scrCall(router, tc.method, tc.path, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s 应 200，实得 %d body=%s", tc.what, w.Code, w.Body.String())
		}
		scrEchoCheck(t, w, tc.what)
	}
}

// TestScriptCrossTenantDetailIsNotFound 别租户的话术按 id 寻址：只能 404，
// 不许 200 回内容，也不许把闸的报错原文递出去。
func TestScriptCrossTenantDetailIsNotFound(t *testing.T) {
	database := scrDB(t)
	foreign := scrSeedTemplate(t, database, scrTenantB, "sales", "别租户话术", "别租户内容")
	router := scrRouter(t, true)

	w := scrCall(router, http.MethodGet, fmt.Sprintf("/scripts/%d", foreign.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("读别租户话术详情应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "别租户话术详情")
	if strings.Contains(w.Body.String(), "别租户内容") {
		t.Errorf("别租户话术正文漏进了响应：%s", w.Body.String())
	}
}

// TestScriptCrossTenantUpdateLeavesOtherTenantRow 别租户的 id 改不动，
// 且那一行必须原样留着（读不到不等于改不到，Save 的 upsert 会把别人的行搬过来）。
func TestScriptCrossTenantUpdateLeavesOtherTenantRow(t *testing.T) {
	database := scrDB(t)
	foreign := scrSeedTemplate(t, database, scrTenantB, "sales", "别租户原标题", "别租户原内容")
	router := scrRouter(t, true)

	body, _ := json.Marshal(map[string]any{"title": "被改掉的标题", "content": "被改掉的内容"})
	w := scrCall(router, http.MethodPut, fmt.Sprintf("/scripts/%d", foreign.ID), body)
	if w.Code != http.StatusNotFound {
		t.Errorf("改别租户话术应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "别租户话术改写")

	var row contentmodel.ScriptTemplate
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		First(&row, foreign.ID).Error; err != nil {
		t.Fatalf("物理读回别租户话术失败: %v", err)
	}
	if row.Title != "别租户原标题" || row.Content != "别租户原内容" {
		t.Errorf("别租户那行被动过了：%+v", row)
	}
	if row.TenantID != scrTenantB {
		t.Errorf("别租户话术的归属被搬走了，tenant_id 应为 %d，实得 %d", scrTenantB, row.TenantID)
	}
}

// TestScriptCreateKeepsCallerTenant 建话术必须落在调用方租户，
// 而不是列默认值 1；created_by 取 ctx 里的 user_id。
func TestScriptCreateKeepsCallerTenant(t *testing.T) {
	database := scrDB(t)
	router := scrRouter(t, true)

	body, _ := json.Marshal(map[string]any{
		"name": "新建话术", "category": "sales", "title": "新建话术标题", "content": "新建话术内容",
	})
	w := scrCall(router, http.MethodPost, "/scripts", body)
	if w.Code != http.StatusOK {
		t.Fatalf("本租户建话术应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "本租户建话术")

	var created struct {
		ID uint `json:"id"`
	}
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if err := json.Unmarshal(payload.Data, &created); err != nil || created.ID == 0 {
		t.Fatalf("响应里没拿到新行 ID，body=%s", w.Body.String())
	}

	var row contentmodel.ScriptTemplate
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		First(&row, created.ID).Error; err != nil {
		t.Fatalf("物理读回新建话术失败: %v", err)
	}
	if row.TenantID != scrTenantA {
		t.Errorf("新建话术租户应为 %d，实得 %d", scrTenantA, row.TenantID)
	}
	if row.CreatedBy != scrUserA {
		t.Errorf("created_by 应为 %d，实得 %d", scrUserA, row.CreatedBy)
	}
}

// TestScriptUpdateOfDeletedRowIsNotFound 更新已删除的话术：404，且不许复活。
func TestScriptUpdateOfDeletedRowIsNotFound(t *testing.T) {
	database := scrDB(t)
	row := scrSeedTemplate(t, database, scrTenantA, "sales", "将被删除的话术", "原内容")
	router := scrRouter(t, true)

	if w := scrCall(router, http.MethodDelete, fmt.Sprintf("/scripts/%d", row.ID), nil); w.Code != http.StatusOK {
		t.Fatalf("删除本租户话术应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if n := scrPhysicalTemplateCount(t, database, row.ID); n != 0 {
		t.Fatalf("删除后物理行数应为 0，实得 %d", n)
	}

	body, _ := json.Marshal(map[string]any{"title": "改后的标题"})
	w := scrCall(router, http.MethodPut, fmt.Sprintf("/scripts/%d", row.ID), body)
	if w.Code != http.StatusNotFound {
		t.Errorf("更新已删除的话术应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "更新已删除话术")
	if n := scrPhysicalTemplateCount(t, database, row.ID); n != 0 {
		t.Errorf("已删除的话术被写口复活了，物理行数 %d", n)
	}
}

// TestScriptUnboundTenantFailsClosedWithoutEcho 没绑租户时读写必须被拒，
// 但拒绝的方式是 500 加一句不含内部原文的文案（fail-closed 不等于泄密）。
func TestScriptUnboundTenantFailsClosedWithoutEcho(t *testing.T) {
	database := scrDB(t)
	scrSeedTemplate(t, database, scrTenantA, "sales", "未绑定租户读的话术", "内容")
	scrSeedTemplate(t, database, scrTenantA, "sales", "未绑定租户改的话术", "内容")
	router := scrRouter(t, false)

	w := scrCall(router, http.MethodGet, "/scripts", nil)
	if w.Code == http.StatusOK {
		t.Errorf("未绑定租户读话术列表不该成功，body=%s", w.Body.String())
	}
	scrEchoCheck(t, w, "未绑定租户话术列表")

	w = scrCall(router, http.MethodGet, "/scripts/categories", nil)
	if w.Code == http.StatusOK {
		t.Errorf("未绑定租户读分类不该成功，body=%s", w.Body.String())
	}
	scrEchoCheck(t, w, "未绑定租户分类列表")
}

// TestScriptRecommendAndSearchAreTenantFactored 推荐与搜索这两个读口
// 走的是 Go 侧打分链路（SearchTemplates + calculateRelevance），
// 也要接上租户：改前同样是整层无 ctx ⇒ 500。
func TestScriptRecommendAndSearchAreTenantFactored(t *testing.T) {
	database := scrDB(t)
	scrSeedTemplate(t, database, scrTenantA, "sales", "价格异议应对", "先讲价值再谈价格，价格不是问题")
	router := scrRouter(t, true)

	w := scrCall(router, http.MethodPost, "/scripts/recommend",
		[]byte(`{"session_id":"s-1","message":"先讲价值再谈价格"}`))
	if w.Code != http.StatusOK {
		t.Errorf("本租户推荐话术应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "本租户推荐话术")

	foreign := scrSeedTemplate(t, database, scrTenantB, "sales", "别租户公开话术", "别租户公开内容")
	w = scrCall(router, http.MethodGet, "/scripts/public", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("本租户读公开话术应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), foreign.Content) {
		t.Errorf("别租户话术漏进本租户公开列表：%s", w.Body.String())
	}
}

// TestScriptCreateWithoutUserIDDoesNotPanic CreateTemplate 用 userID.(uint)
// 无条件断言：user_id 缺席时应回 401/400，而不是 panic 后被 Recovery 转成 500。
func TestScriptCreateWithoutUserIDDoesNotPanic(t *testing.T) {
	scrDB(t)
	// 走一条没有 Set("user_id") 的挂载：中间件的有无是装配决定的，
	// 这里直接构造缺 user_id 的请求面，判据落在「崩」与「明确拒绝」之间。
	noUID := gin.New()
	noUID.Use(gin.Recovery())
	noUID.Use(func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), scrTenantA))
		ctx.Next()
	})
	noUID.POST("/scripts", newTestScriptTemplateController().CreateTemplate)

	body, _ := json.Marshal(map[string]any{"name": "n", "category": "sales", "title": "t", "content": "c"})
	w := scrCall(noUID, http.MethodPost, "/scripts", body)
	if w.Code == http.StatusInternalServerError && strings.Contains(w.Body.String(), "interface conversion") {
		t.Errorf("缺 user_id 时崩在类型断言上：%s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Errorf("缺 user_id 应回 400/401，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "缺 user_id 建话术")
}

// TestScriptSyncToLibraryKeepsCallerTenant 同步口把管理端模板搬进运行时话术库：
// 读模板与写 script_library 都必须带调用方租户，
// 新行落进列默认值 1（默认租户）等于把内容交给别的租户看。
func TestScriptSyncToLibraryKeepsCallerTenant(t *testing.T) {
	database := scrDB(t)
	scrSeedTemplate(t, database, scrTenantA, "objection", "同步用话术", "先讲价值再谈价格")
	router := scrRouter(t, true)

	w := scrCall(router, http.MethodPost, "/scripts/sync-to-library", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("本租户同步话术应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	scrEchoCheck(t, w, "本租户同步话术")

	rows := scrPhysicalLibrary(t, database, "objection", "同步用话术")
	if len(rows) != 1 {
		t.Fatalf("同步后运行时话术库应有 1 行，实得 %d", len(rows))
	}
	if rows[0].TenantID != scrTenantA {
		t.Errorf("同步出来的运行时话术应归调用方租户 %d，实得 %d（落到列默认值就是跨租户写）", scrTenantA, rows[0].TenantID)
	}
}

// TestScriptSyncToLibraryIsTenantFactored 两个租户有同名 (category,title) 时，
// 各自同步只能动自己那一份：改前的查重键不带租户，B 的同步会把 A 的正文覆盖掉。
func TestScriptSyncToLibraryIsTenantFactored(t *testing.T) {
	database := scrDB(t)
	scrSeedTemplate(t, database, scrTenantA, "objection", "同名话术", "A 家内容")
	scrSeedTemplate(t, database, scrTenantB, "objection", "同名话术", "B 家内容")

	// A 先同步：用 A 身份走一次服务口。
	aRouter := scrRouter(t, true)
	if w := scrCall(aRouter, http.MethodPost, "/scripts/sync-to-library", nil); w.Code != http.StatusOK {
		t.Fatalf("A 家同步应 200，实得 %d body=%s", w.Code, w.Body.String())
	}

	// B 再同步：必须新长一行归 B 的，不许覆写 A 那行。
	bCtx := tenant.WithTenantID(context.Background(), scrTenantB)
	// AV-04c：service 生产层不再持 *gorm.DB 形参，测试侧直构 Deps。
	bSvc := contentservice.NewScriptTemplateSyncService(contentservice.ScriptTemplateSyncDeps{
		TemplateRepo: contentrepo.NewScriptTemplateRepository(db.GetDB()),
	})
	if _, err := bSvc.SyncToLibrary(bCtx); err != nil {
		t.Fatalf("B 家同步失败: %v", err)
	}

	rows := scrPhysicalLibrary(t, database, "objection", "同名话术")
	if len(rows) != 2 {
		t.Fatalf("两个租户各一行才不串，实得 %d 行", len(rows))
	}
	owners := map[uint]string{}
	for _, r := range rows {
		owners[r.TenantID] = r.Content
	}
	if owners[scrTenantA] != "A 家内容" {
		t.Errorf("A 家的运行时话术被覆写了：实得 %q", owners[scrTenantA])
	}
	if owners[scrTenantB] != "B 家内容" {
		t.Errorf("B 家没有自己的运行时话术：实得 %q", owners[scrTenantB])
	}
}

// TestScriptRecommendSurfacesReadFailure 推荐口不许把「读话术失败」说成「没话术」：
// 改前 `templates, _, _ := SearchTemplates(...)` 把错误丢在地上，于是 on 模式下
// 隔离闸拒绝读取（上下文/配置坏了）出口照样 200 + 空列表——调用方看不出区别。
func TestScriptRecommendSurfacesReadFailure(t *testing.T) {
	scrDB(t)
	router := scrRouter(t, false)

	w := scrCall(router, http.MethodPost, "/scripts/recommend", []byte(`{"message":"价格"}`))
	if w.Code == http.StatusOK {
		t.Errorf("未绑定租户时推荐口不该回 200（闸的拒绝被丢成空推荐），实得 body=%s", w.Body.String())
	}
	scrEchoCheck(t, w, "未绑定租户推荐话术")
}

// TestScriptSyncReadsEveryTemplate 同步口整批读：不能借分页口读全量。
// 分页口有 pageSize 上限夹紧，`GetAll(ctx, "", 1, 1<<20)` 在夹紧生效后只会同步前 20 条，
// 剩下的模板静默不进运行时话术库（比改前的 LIMIT 1048576 更错：那是超限，这是截断）。
func TestScriptSyncReadsEveryTemplate(t *testing.T) {
	database := scrDB(t)
	for i := 0; i < 25; i++ {
		scrSeedTemplate(t, database, scrTenantA, "batch", fmt.Sprintf("批量同步话术 %02d", i), "同步内容")
	}
	router := scrRouter(t, true)

	w := scrCall(router, http.MethodPost, "/scripts/sync-to-library", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("本租户批量同步应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			Scanned int `json:"scanned"`
			Created int `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是 JSON: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Scanned != 25 {
		t.Errorf("同步应扫到 25 条，实得 %d（读口被分页夹紧截断）", payload.Data.Scanned)
	}

	var n int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&mainmodel.ScriptLibrary{}).
		Where("tenant_id = ? AND category = ?", scrTenantA, "batch").Count(&n).Error; err != nil {
		t.Fatalf("统计同步结果失败: %v", err)
	}
	if n != 25 {
		t.Errorf("运行时话术库应有 25 行，实得 %d", n)
	}
}
