package controller

// 契约锁（实体审查 #18，HTTP 出口层）。
//
// 素材库这条链（materials + material_categories）改前有三类只有 on 模式才暴露的故障：
//   1. 两个仓储与服务的读写面整面不带 context.Context——on 模式每一次读写都撞
//      `[tenant] …ctx 未绑定` 的 fail-closed 闸，14 个挂载口（content_routes.go:15-31
//      的 10 条 + materialAdmin 组 4 条，另 frontend_aliases.go:373-377 的 5 条别名）
//      全部回 500。
//   2. 两处 Update 走 Save()：全字段 upsert 会把读改写期间删掉的行插回来，
//      连 license_id / tenant_id 一起按结构体快照覆写。
//   3. 上传腿把 OBS 配置读取与存储驱动都接在 context.Background() 上——
//      GetDefault 被闸拒（上传永远失败），本地驱动拿不到租户就落下不带
//      tenant_{id}/ 前缀的路径（T011 的路径自证失效）。
// 出口层同样锁住：分页不夹紧（limit=0 被发成 LIMIT 0 ⇒ 恒回空列表，limit 过界 ⇒ 一次拖全表）、
// 统计口只取前 1000 行
// 在 Go 里数（超过 1000 就少报），上传失败把 err.Error() 原样拼进 500 文案。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	contentmodel "hivemtk-user/internal/content/model"
	contentservice "hivemtk-user/internal/content/service"
	usermodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	matTenantA = uint(121)
	matTenantB = uint(122)
	matUserA   = uint(940101)
	matLicense = "lic-a"
	// matMissingID 两个 :id 口都能寻址、但任何租户都没有这行。
	matMissingID = "00000000-0000-0000-0000-000000000000"
)

func matDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&contentmodel.Material{},
		&contentmodel.MaterialCategory{},
		&usermodel.ObsConfig{},
	)
	if database == nil {
		t.Fatal("测试库不可达（不 Skip，跳过等于没锁）")
	}
	db.SetTestDB(database)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	return database
}

// matSeedCategory 经 SystemScope 预置：夹具要跨租户建行，业务写口本身是被测对象。
func matSeedCategory(t *testing.T, database *gorm.DB, tenantID uint, licenseID, name string) *contentmodel.MaterialCategory {
	t.Helper()
	row := &contentmodel.MaterialCategory{
		TenantID:  tenantID,
		Name:      name,
		Type:      contentmodel.MaterialTypeImage,
		LicenseID: licenseID,
		Status:    "active",
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置分类 %s 失败: %v", name, err)
	}
	return row
}

func matSeedMaterial(t *testing.T, database *gorm.DB, tenantID uint, licenseID, categoryID, name string, matType contentmodel.MaterialType, size int64) *contentmodel.Material {
	t.Helper()
	row := &contentmodel.Material{
		TenantID:   tenantID,
		Name:       name,
		Type:       matType,
		CategoryID: categoryID,
		URL:        "https://cdn.example.com/" + name,
		Size:       size,
		MimeType:   string(matType) + "/plain",
		LicenseID:  licenseID,
		Status:     "active",
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置素材 %s 失败: %v", name, err)
	}
	return row
}

// matPhysicalMaterial 物理读：断言「别租户那行没被动过」不能被隔离闸挡住而假绿。
func matPhysicalMaterial(t *testing.T, database *gorm.DB, id string) *contentmodel.Material {
	t.Helper()
	var row contentmodel.Material
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("物理读取素材 %s 失败: %v", id, err)
	}
	return &row
}

func matPhysicalMaterialCount(t *testing.T, database *gorm.DB, id string) int64 {
	t.Helper()
	var n int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&contentmodel.Material{}).Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("物理计数素材 %s 失败: %v", id, err)
	}
	return n
}

func matPhysicalCategory(t *testing.T, database *gorm.DB, id string) *contentmodel.MaterialCategory {
	t.Helper()
	var row contentmodel.MaterialCategory
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("物理读取分类 %s 失败: %v", id, err)
	}
	return &row
}

func matPhysicalCategoryCount(t *testing.T, database *gorm.DB, id string) int64 {
	t.Helper()
	var n int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&contentmodel.MaterialCategory{}).Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("物理计数分类 %s 失败: %v", id, err)
	}
	return n
}

// matRouter 复刻真实挂载：读口与上传口在 auth 组，改删口在 materialAdmin 组
// （content_routes.go:15-31）。license_id 由头注入——它是这条链的业务归属列，
// 非管理员拿不到它（middleware/jwt.go:85 只在 role=admin 时 Set），
// 所以夹具必须能把「有 license」和「有租户」两种口径分开测。
func matRouter(t *testing.T, bindTenant bool) *gin.Engine {
	t.Helper()
	ctrl := NewMaterialController(contentservice.NewMaterialService(contentservice.NewMaterialDeps(db.GetDB())))
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", matUserA)
		ctx.Set("role", ctx.GetHeader("X-Test-Role"))
		if lic := ctx.GetHeader("X-Test-License"); lic != "" {
			ctx.Set("license_id", lic)
		}
		if bindTenant {
			ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), matTenantA))
		}
		ctx.Next()
	})

	// 与 content_routes.go 同一顺序：静态段先于 :id，否则 /material/stats 被 :id 吃掉。
	auth := router.Group("")
	auth.GET("/material/list", ctrl.GetMaterialList)
	auth.POST("/material", ctrl.UploadMaterial)
	auth.POST("/material/upload", ctrl.UploadMaterial)
	auth.PUT("/material/:id/usage", ctrl.UpdateMaterialUsage)
	auth.POST("/material/:id/usage", ctrl.UpdateMaterialUsage)
	auth.GET("/material/stats", ctrl.GetMaterialStats)
	auth.GET("/material/categories", ctrl.GetMaterialCategories)
	auth.GET("/material/categories/:id", ctrl.GetMaterialCategoryByID)
	auth.GET("/material/selector", ctrl.GetMaterialSelector)
	auth.GET("/material/:id", ctrl.GetMaterialByID)

	admin := auth.Group("")
	admin.DELETE("/material/:id", ctrl.DeleteMaterial)
	admin.POST("/material/categories", ctrl.CreateMaterialCategory)
	admin.PUT("/material/categories/:id", ctrl.UpdateMaterialCategory)
	admin.DELETE("/material/categories/:id", ctrl.DeleteMaterialCategory)
	return router
}

func matCall(router *gin.Engine, method, path, license, role string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body == nil {
		req, _ = http.NewRequest(method, path, nil)
	} else {
		req, _ = http.NewRequest(method, path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
	}
	if license != "" {
		req.Header.Set("X-Test-License", license)
	}
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// matEchoCheck 内部错误原文不许出现在任何出口响应里。
func matEchoCheck(t *testing.T, w *httptest.ResponseRecorder, what string) {
	t.Helper()
	body := w.Body.String()
	if strings.Contains(body, "[tenant]") || strings.Contains(body, "租户上下文") || strings.Contains(body, "SQLSTATE") || strings.Contains(body, "gorm") {
		t.Errorf("%s 回显了内部错误原文：%s", what, body)
	}
}

func TestMaterialReadsAreTenantFactored(t *testing.T) {
	database := matDB(t)
	ownCat := matSeedCategory(t, database, matTenantA, matLicense, "A 家图片分类")
	matSeedMaterial(t, database, matTenantA, matLicense, ownCat.ID, "A 家一号", contentmodel.MaterialTypeImage, 100)
	matSeedMaterial(t, database, matTenantA, matLicense, ownCat.ID, "A 家二号", contentmodel.MaterialTypeVideo, 200)
	foreignCat := matSeedCategory(t, database, matTenantB, "lic-b", "B 家分类")
	foreign := matSeedMaterial(t, database, matTenantB, "lic-b", foreignCat.ID, "B 家一号", contentmodel.MaterialTypeImage, 999)

	bound := matRouter(t, true)

	var payload struct {
		Data struct {
			List  []contentmodel.Material `json:"list"`
			Total int64                   `json:"total"`
		} `json:"data"`
	}
	w := matCall(bound, http.MethodGet, "/material/list?page=1&limit=20", matLicense, "", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析素材列表失败: %v body=%s", err, w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("本租户素材列表应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if payload.Data.Total != 2 || len(payload.Data.List) != 2 {
		t.Errorf("A 租户只应看到自己的 2 条，实得 total=%d len=%d", payload.Data.Total, len(payload.Data.List))
	}
	for _, m := range payload.Data.List {
		if strings.Contains(m.Name, "B 家") {
			t.Errorf("列表泄漏别租户素材：%+v", m)
		}
	}

	if got := matCall(bound, http.MethodGet, "/material/"+foreign.ID, matLicense, "", nil); got.Code != http.StatusNotFound {
		t.Errorf("读别租户素材详情应 404，实得 %d body=%s", got.Code, got.Body.String())
	}
	if got := matCall(bound, http.MethodGet, "/material/categories/"+foreignCat.ID, matLicense, "", nil); got.Code != http.StatusNotFound {
		t.Errorf("读别租户分类详情应 404，实得 %d body=%s", got.Code, got.Body.String())
	}
	if got := matCall(bound, http.MethodGet, "/material/categories/"+matMissingID, matLicense, "", nil); got.Code != http.StatusNotFound {
		t.Errorf("读不存在的分类详情应 404，实得 %d body=%s", got.Code, got.Body.String())
	}
	if got := matCall(bound, http.MethodGet, "/material/"+matMissingID, matLicense, "", nil); got.Code != http.StatusNotFound {
		t.Errorf("读不存在的素材应 404，实得 %d body=%s", got.Code, got.Body.String())
	}

	// 选择器与分类树同样是读口：本租户 200，且不把别租户的行带出来。
	if got := matCall(bound, http.MethodGet, "/material/selector", matLicense, "", nil); got.Code != http.StatusOK {
		t.Errorf("本租户选择器应 200，实得 %d body=%s", got.Code, got.Body.String())
	} else if strings.Contains(got.Body.String(), "B 家") {
		t.Errorf("选择器泄漏别租户数据：%s", got.Body.String())
	}
	if got := matCall(bound, http.MethodGet, "/material/categories?page=1&limit=50", matLicense, "", nil); got.Code != http.StatusOK {
		t.Errorf("本租户分类列表应 200，实得 %d body=%s", got.Code, got.Body.String())
	}

	// 未绑定租户：配置级故障就是故障，既不折叠成 404 也不回显原文。
	unbound := matRouter(t, false)
	for _, path := range []string{"/material/list", "/material/categories", "/material/stats", "/material/selector", "/material/" + foreign.ID} {
		got := matCall(unbound, http.MethodGet, path, matLicense, "", nil)
		if got.Code != http.StatusInternalServerError {
			t.Errorf("未绑定租户时 %s 应 500，实得 %d body=%s", path, got.Code, got.Body.String())
		}
		matEchoCheck(t, got, "未绑定租户读 "+path)
	}
	if got := matCall(unbound, http.MethodGet, "/material/"+foreign.ID, matLicense, "", nil); strings.Contains(got.Body.String(), "不存在") {
		t.Errorf("租户闸故障被说成「素材不存在」：%s", got.Body.String())
	}
}

func TestMaterialWritesRejectForeignTenant(t *testing.T) {
	database := matDB(t)
	ownCat := matSeedCategory(t, database, matTenantA, matLicense, "A 家待改")
	ownMat := matSeedMaterial(t, database, matTenantA, matLicense, ownCat.ID, "A 家待删", contentmodel.MaterialTypeImage, 100)
	foreignCat := matSeedCategory(t, database, matTenantB, "lic-b", "B 家待改")
	foreignMat := matSeedMaterial(t, database, matTenantB, "lic-b", foreignCat.ID, "B 家待删", contentmodel.MaterialTypeImage, 500)

	bound := matRouter(t, true)
	patch, _ := json.Marshal(map[string]any{"name": "被别租户改名"})

	if w := matCall(bound, http.MethodPut, "/material/categories/"+foreignCat.ID, matLicense, "admin", patch); w.Code != http.StatusNotFound {
		t.Errorf("改别租户分类应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalCategory(t, database, foreignCat.ID); got.Name != "B 家待改" {
		t.Errorf("别租户分类被改写：%+v", got)
	}

	if w := matCall(bound, http.MethodDelete, "/material/"+foreignMat.ID, matLicense, "admin", nil); w.Code != http.StatusNotFound {
		t.Errorf("删别租户素材应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalMaterialCount(t, database, foreignMat.ID); got != 1 {
		t.Errorf("别租户素材被删掉了，物理行数 %d", got)
	}

	if w := matCall(bound, http.MethodPost, "/material/"+foreignMat.ID+"/usage", matLicense, "", nil); w.Code != http.StatusNotFound {
		t.Errorf("对别租户素材计次应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalMaterial(t, database, foreignMat.ID); got.UsageCount != 0 {
		t.Errorf("别租户素材 usage_count 被动过：%+v", got)
	}

	// 本租户写口必须真的落库（改前被闸挡成 500）。
	if w := matCall(bound, http.MethodPut, "/material/categories/"+ownCat.ID, matLicense, "admin", patch); w.Code != http.StatusOK {
		t.Fatalf("改本租户分类应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalCategory(t, database, ownCat.ID); got.Name != "被别租户改名" {
		t.Errorf("本租户分类没被更新：%+v", got)
	}
	if got := matPhysicalCategory(t, database, ownCat.ID); got.TenantID != matTenantA {
		t.Errorf("改分类把 tenant_id 挪走了：%+v", got)
	}
	if w := matCall(bound, http.MethodPost, "/material/"+ownMat.ID+"/usage", matLicense, "", nil); w.Code != http.StatusOK {
		t.Errorf("本租户计次应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalMaterial(t, database, ownMat.ID); got.UsageCount != 1 || got.LastUsedAt == nil {
		t.Errorf("本租户计次没落库：%+v", got)
	}
	if w := matCall(bound, http.MethodDelete, "/material/"+ownMat.ID, matLicense, "admin", nil); w.Code != http.StatusOK {
		t.Errorf("删本租户素材应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalMaterialCount(t, database, ownMat.ID); got != 0 {
		t.Errorf("删除后物理行仍在：%d", got)
	}

	// 未绑定租户：写口 fail-closed，本租户的行停在原值。
	unbound := matRouter(t, false)
	badPatch := matCall(unbound, http.MethodPut, "/material/categories/"+ownCat.ID, matLicense, "admin", patch)
	if badPatch.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户改分类应 500，实得 %d body=%s", badPatch.Code, badPatch.Body.String())
	}
	matEchoCheck(t, badPatch, "未绑定租户改分类")
	if w := matCall(unbound, http.MethodDelete, "/material/"+foreignMat.ID, matLicense, "admin", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户删素材应 500，实得 %d body=%s", w.Code, w.Body.String())
	}
	if got := matPhysicalCategory(t, database, ownCat.ID); got.Name != "被别租户改名" {
		t.Errorf("fail-closed 之后本租户分类仍应停在改名后的值，实得 %+v", got)
	}

	// 建分类落库必须带调用方租户与 license。
	createBody, _ := json.Marshal(map[string]any{"name": "A 家新建", "type": "image"})
	created := matCall(bound, http.MethodPost, "/material/categories", matLicense, "admin", createBody)
	if created.Code != http.StatusOK {
		t.Fatalf("建分类应 200，实得 %d body=%s", created.Code, created.Body.String())
	}
	var createdPayload struct {
		Data contentmodel.MaterialCategory `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdPayload); err != nil {
		t.Fatalf("解析建分类响应失败: %v body=%s", err, created.Body.String())
	}
	if createdPayload.Data.ID == "" {
		t.Fatalf("建分类没带回 id：%s", created.Body.String())
	}
	if got := matPhysicalCategory(t, database, createdPayload.Data.ID); got.TenantID != matTenantA {
		t.Errorf("新建分类租户应为 %d，实得 %d", matTenantA, got.TenantID)
	} else if got.LicenseID != matLicense {
		t.Errorf("新建分类 license 应为 %s，实得 %s", matLicense, got.LicenseID)
	}

	// 命中不到行的三个写口：都要 404，不是带着「成功」空响。
	if w := matCall(bound, http.MethodPost, "/material/"+matMissingID+"/usage", matLicense, "", nil); w.Code != http.StatusNotFound {
		t.Errorf("对不存在的素材计次应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := matCall(bound, http.MethodPut, "/material/categories/"+matMissingID, matLicense, "admin", patch); w.Code != http.StatusNotFound {
		t.Errorf("改不存在的分类应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := matCall(bound, http.MethodDelete, "/material/"+matMissingID, matLicense, "admin", nil); w.Code != http.StatusNotFound {
		t.Errorf("删不存在的素材应 404，实得 %d body=%s", w.Code, w.Body.String())
	}
	if w := matCall(bound, http.MethodDelete, "/material/categories/"+matMissingID, matLicense, "admin", nil); w.Code != http.StatusNotFound {
		t.Errorf("删不存在的分类应 404，实得 %d body=%s", w.Code, w.Body.String())
	}

	// 分类下还有素材时不能直接删：给 400 业务口径，而不是把闸/约束错误说成 500。
	notEmptyCat := matSeedCategory(t, database, matTenantA, matLicense, "非空分类")
	matSeedMaterial(t, database, matTenantA, matLicense, notEmptyCat.ID, "非空分类下的素材", contentmodel.MaterialTypeImage, 10)
	notEmpty := matCall(bound, http.MethodDelete, "/material/categories/"+notEmptyCat.ID, matLicense, "admin", nil)
	if notEmpty.Code != http.StatusBadRequest {
		t.Errorf("删非空分类应 400，实得 %d body=%s", notEmpty.Code, notEmpty.Body.String())
	}
	matEchoCheck(t, notEmpty, "删非空分类")
	if n := matPhysicalCategoryCount(t, database, notEmptyCat.ID); n != 1 {
		t.Errorf("非空分类被删掉了，物理行数 %d", n)
	}
}

// TestMaterialUsageBumpSurvivesConcurrentEdits 计次口不许把整行快照写回去：
// 改前 UpdateMaterialUsage = 读整行 → ++ → Save（全字段 upsert），
// 与并发的改名互相覆盖，且 40 次并发计次丢得只剩个位数。
func TestMaterialUsageBumpSurvivesConcurrentEdits(t *testing.T) {
	database := matDB(t)
	cat := matSeedCategory(t, database, matTenantA, matLicense, "计次分类")
	mat := matSeedMaterial(t, database, matTenantA, matLicense, cat.ID, "并发计次", contentmodel.MaterialTypeImage, 100)

	bound := matRouter(t, true)
	const bumps = 40
	var wg sync.WaitGroup
	for i := 0; i < bumps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matCall(bound, http.MethodPost, "/material/"+mat.ID+"/usage", matLicense, "", nil)
		}()
	}
	wg.Wait()

	got := matPhysicalMaterial(t, database, mat.ID)
	if got.UsageCount != bumps {
		t.Errorf("并发 %d 次计次后 usage_count 应为 %d，实得 %d（读改写丢更新）", bumps, bumps, got.UsageCount)
	}
	if got.LastUsedAt == nil {
		t.Errorf("计次没落 last_used_at")
	}
}

// TestMaterialStatsCountsEveryRow 统计口必须数全租户的每一行。
// 改前是 GetList(...,1,1000) 取前 1000 行回 Go 里数：第 1001 行起静默少报，
// total_size / usage 也一起少。
func TestMaterialStatsCountsEveryRow(t *testing.T) {
	database := matDB(t)
	cat := matSeedCategory(t, database, matTenantA, matLicense, "统计分类")
	matSeedMaterial(t, database, matTenantB, "lic-b", "", "B 家统计", contentmodel.MaterialTypeImage, 7777)

	const rows = 1001
	batch := make([]*contentmodel.Material, 0, rows)
	for i := 0; i < rows; i++ {
		matType := contentmodel.MaterialTypeImage
		if i%2 == 1 {
			matType = contentmodel.MaterialTypeVideo
		}
		batch = append(batch, &contentmodel.Material{
			TenantID:   matTenantA,
			Name:       fmt.Sprintf("统计素材 %d", i),
			Type:       matType,
			CategoryID: cat.ID,
			URL:        "https://cdn.example.com/stat",
			Size:       10,
			MimeType:   string(matType) + "/plain",
			LicenseID:  matLicense,
			Status:     "active",
			UsageCount: 1,
		})
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		CreateInBatches(batch, 200).Error; err != nil {
		t.Fatalf("批量预置素材失败: %v", err)
	}

	w := matCall(matRouter(t, true), http.MethodGet, "/material/stats", matLicense, "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("统计口应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	var stats struct {
		Data struct {
			TotalMaterials  int64 `json:"total_materials"`
			ImageCount      int64 `json:"image_count"`
			VideoCount      int64 `json:"video_count"`
			TotalSize       int64 `json:"total_size"`
			TotalUsageCount int64 `json:"total_usage_count"`
			TodayAddedCount int64 `json:"today_added_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("解析统计响应失败: %v body=%s", err, w.Body.String())
	}
	got := stats.Data
	if got.TotalMaterials != rows {
		t.Errorf("统计应数到 %d 行，实得 %d（改前只取前 1000 行）", rows, got.TotalMaterials)
	}
	if got.ImageCount != 501 || got.VideoCount != 500 {
		t.Errorf("按类型分桶不对：image=%d video=%d（应 501/500）", got.ImageCount, got.VideoCount)
	}
	if got.TotalSize != int64(rows)*10 {
		t.Errorf("总体积应为 %d，实得 %d", int64(rows)*10, got.TotalSize)
	}
	if got.TotalUsageCount != rows {
		t.Errorf("总使用次数应为 %d，实得 %d", rows, got.TotalUsageCount)
	}
	if got.TodayAddedCount != rows {
		t.Errorf("今日新增应为 %d，实得 %d", rows, got.TodayAddedCount)
	}

	// 未绑定租户：500 且不回显原文。
	unbound := matCall(matRouter(t, false), http.MethodGet, "/material/stats", matLicense, "", nil)
	if unbound.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户读统计应 500，实得 %d body=%s", unbound.Code, unbound.Body.String())
	}
	matEchoCheck(t, unbound, "未绑定租户读统计")
}

// TestMaterialPagingIsClamped page=0/limit=0/limit 过界都要夹紧。
// 夹紧拦住的真实故障形状（变异 M6 复现）：limit=0 被 GORM 原样发成 LIMIT 0 ⇒ 列表恒空，
// 运维读成「素材被清空了」；page<=0 则被 GORM 吞成无偏移，不是 PG 报错。
func TestMaterialPagingIsClamped(t *testing.T) {
	database := matDB(t)
	cat := matSeedCategory(t, database, matTenantA, matLicense, "分页分类")
	for i := 0; i < 3; i++ {
		matSeedMaterial(t, database, matTenantA, matLicense, cat.ID, fmt.Sprintf("分页素材 %d", i), contentmodel.MaterialTypeImage, 10)
	}
	bound := matRouter(t, true)

	for _, q := range []string{"page=0&limit=2", "page=-7&limit=2", "page=1&limit=0", "page=1&limit=999999"} {
		w := matCall(bound, http.MethodGet, "/material/list?"+q, matLicense, "", nil)
		if w.Code != http.StatusOK {
			t.Errorf("分页参数 %s 应夹紧并回 200，实得 %d body=%s", q, w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List  []contentmodel.Material `json:"list"`
				Total int64                   `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析 %s 响应失败: %v body=%s", q, err, w.Body.String())
		}
		if resp.Data.Total != 3 {
			t.Errorf("分页参数 %s 的 total 应为 3，实得 %d", q, resp.Data.Total)
		}
		if len(resp.Data.List) == 0 {
			t.Errorf("分页参数 %s 回空列表（夹紧后应回第 1 页数据）", q)
		}
		if len(resp.Data.List) > 100 {
			t.Errorf("分页参数 %s 一次回了 %d 行，上限夹紧没生效", q, len(resp.Data.List))
		}
	}

	for _, q := range []string{"page=0&limit=0", "page=1&limit=999999"} {
		w := matCall(bound, http.MethodGet, "/material/categories?"+q, matLicense, "", nil)
		if w.Code != http.StatusOK {
			t.Errorf("分类分页参数 %s 应 200，实得 %d body=%s", q, w.Code, w.Body.String())
		}
	}
}

// TestMaterialUploadKeepsCallerTenant 上传腿必须把调用方租户带到 OBS 配置读取、
// 存储驱动与落库三段：改前三段都接在 context.Background() 上。
func TestMaterialUploadKeepsCallerTenant(t *testing.T) {
	database := matDB(t)
	baseDir := t.TempDir()
	cfg := &usermodel.ObsConfig{
		TenantID:  matTenantA,
		Name:      "A 家本地存储",
		Provider:  usermodel.ObsProviderLocal,
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "bucket",
		Endpoint:  baseDir,
		Domain:    "https://files.example.com",
		MaxSize:   104857600,
		Status:    usermodel.ObsStatusActive,
		IsDefault: true,
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(cfg).Error; err != nil {
		t.Fatalf("预置 OBS 配置失败: %v", err)
	}
	cat := matSeedCategory(t, database, matTenantA, matLicense, "上传分类")

	w := matUpload(t, matRouter(t, true), cat.ID, matLicense)
	if w.Code != http.StatusOK {
		t.Fatalf("本租户上传应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	matEchoCheck(t, w, "上传成功响应")

	var resp struct {
		Data contentmodel.Material `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析上传响应失败: %v body=%s", err, w.Body.String())
	}
	if resp.Data.ID == "" {
		t.Fatalf("上传响应没带回素材 id：%s", w.Body.String())
	}
	got := matPhysicalMaterial(t, database, resp.Data.ID)
	if got.TenantID != matTenantA {
		t.Errorf("上传的素材租户应为 %d，实得 %d", matTenantA, got.TenantID)
	}
	if got.LicenseID != matLicense {
		t.Errorf("上传的素材 license 应为 %s，实得 %s", matLicense, got.LicenseID)
	}
	if !strings.HasPrefix(got.StoragePath, fmt.Sprintf("tenant_%d/", matTenantA)) {
		t.Errorf("on 模式落盘路径要带租户段自证（local.go T011），实得 %q", got.StoragePath)
	}

	// 未绑定租户：500，且不把闸的原文（含存储配置线索）回显出去。
	unbound := matUpload(t, matRouter(t, false), cat.ID, matLicense)
	if unbound.Code != http.StatusInternalServerError {
		t.Errorf("未绑定租户上传应 500，实得 %d body=%s", unbound.Code, unbound.Body.String())
	}
	matEchoCheck(t, unbound, "未绑定租户上传")
}

func matUpload(t *testing.T, router *gin.Engine, categoryID, license string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("category_id", categoryID); err != nil {
		t.Fatalf("写 category_id 失败: %v", err)
	}
	part, err := mw.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatalf("建 multipart 文件段失败: %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader("hello material")); err != nil {
		t.Fatalf("写文件体失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "/material/upload", &buf)
	if err != nil {
		t.Fatalf("造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Test-License", license)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestMaterialSearchIsTenantFactored search 走 LIKE 拼接：既要把通配符当字面量，
// 也不能让一个租户搜到另一个租户的名字。
func TestMaterialSearchIsTenantFactored(t *testing.T) {
	database := matDB(t)
	cat := matSeedCategory(t, database, matTenantA, matLicense, "检索分类")
	matSeedMaterial(t, database, matTenantA, matLicense, cat.ID, "本家海报", contentmodel.MaterialTypeImage, 10)
	matSeedMaterial(t, database, matTenantB, "lic-b", "", "本家海报别家版", contentmodel.MaterialTypeImage, 10)

	bound := matRouter(t, true)
	w := matCall(bound, http.MethodGet, "/material/list?search="+urlQueryEscape("本家海报"), matLicense, "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("检索应 200，实得 %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "别家版") {
		t.Errorf("检索泄漏别租户素材：%s", w.Body.String())
	}

	// 通配符当字面量：搜 % 不该把全租户的行都捞出来当成「命中」。
	wild := matCall(bound, http.MethodGet, "/material/list?search="+urlQueryEscape("%"), matLicense, "", nil)
	if wild.Code != http.StatusOK {
		t.Fatalf("通配符检索应 200，实得 %d body=%s", wild.Code, wild.Body.String())
	}
	var resp struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wild.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析通配符检索响应失败: %v", err)
	}
	if resp.Data.Total != 0 {
		t.Errorf("字面 %% 应命中 0 行，实得 %d（LIKE 通配符未转义）", resp.Data.Total)
	}
}

func urlQueryEscape(s string) string {
	return strings.NewReplacer("%", "%25", " ", "%20").Replace(s)
}
