package controller

// 契约锁（实体审查 #14，HTTP 出口层）：`ai_generation_records` 的五个按主键口分成两档权限宽度，
// 两档都得在这一层钉，因为仓储用例看不见 handler 的取舍：
//   用户口（读详情 / 收藏 / 评分，挂 auth 组）——归属谓词写在 SQL 里，handler 少传一个 uid
//     不会让任何仓储用例变红，所以只能在这一层证明「uid 真的交出去了」：B 用 A 的 id 打这三条，一律 404。
//   管理口（save / delete，挂 admin 组 + AdminAuthMiddleware）——管理员整理的是**本租户**内容库，
//     不按 user_id 收窄，唯一上游闸门是角色。所以这里挂真实中间件而不是桩：
//     普通用户 403，管理员改得动别人的记录（这是设计意图，不是漏洞）。
// 跑在 off 模式（testutil 默认）：这里断言的是「归属/角色」而不是「租户遮挡」，后者由
// ai_content_tenant_test.go 在仓储层钉。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/middleware"

	"github.com/gin-gonic/gin"
)

const (
	aiCtrlOwner = uint(800101)
	aiCtrlPeek  = uint(800102)
)

// aiCtrlRouter 把 X-Test-Uid / X-Test-Role 当成 JWT claims 的替身塞进 gin ctx——
// 真实中间件从 token 里取，这里只换身份来源，handler 与 AdminAuthMiddleware 读的仍是同一批键。
func aiCtrlRouter(t *testing.T) *gin.Engine {
	t.Helper()
	if middleware.IsTestMode {
		t.Fatal("IsTestMode 为真时 AdminAuthMiddleware 会无条件放行 admin，本文件的 403 断言将失去意义")
	}
	ctrl := newTestAIContentController()
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		if uid, err := strconv.ParseUint(ctx.GetHeader("X-Test-Uid"), 10, 32); err == nil && uid != 0 {
			ctx.Set("user_id", uint(uid))
		}
		if role := ctx.GetHeader("X-Test-Role"); role != "" {
			ctx.Set("role", role)
		}
		ctx.Next()
	})

	user := router.Group("/ai")
	user.GET("/records/:id", ctrl.GetRecordByID)
	user.POST("/records/:id/favorite", ctrl.FavoriteRecord)
	user.POST("/records/:id/rate", ctrl.RateRecord)
	user.GET("/history", ctrl.GetGenerationHistory)

	// 与 business_routes.go 同构：管理口挂真实角色闸。
	admin := router.Group("/ai", middleware.AdminAuthMiddleware())
	admin.POST("/records/:id/save", ctrl.SaveRecord)
	admin.DELETE("/records/:id", ctrl.DeleteRecord)
	return router
}

func aiCtrlCall(router *gin.Engine, method, path, uid, role, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req, _ = http.NewRequest(method, path, nil)
	} else {
		req, _ = http.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Test-Uid", uid)
	req.Header.Set("X-Test-Role", role)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestAIRecordUserPortsRejectForeignOwnerAtHTTPBoundary 用户口：B 用 A 的 id 一律 404。
func TestAIRecordUserPortsRejectForeignOwnerAtHTTPBoundary(t *testing.T) {
	database := setupAIContentTestDB(t)
	router := aiCtrlRouter(t)

	rec := &model.AIGenerationRecord{
		UserID: aiCtrlOwner,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "A 的输入原文",
		Output: "A 的生成结果",
	}
	if err := database.Create(rec).Error; err != nil {
		t.Fatalf("种子记录失败: %v", err)
	}
	path := "/ai/records/" + strconv.FormatUint(uint64(rec.ID), 10)
	own := strconv.FormatUint(uint64(aiCtrlOwner), 10)

	// 正向对照：本人三条口都走得通（否则「404 全绿」可能只是路由或身份没接上）。
	if w := aiCtrlCall(router, http.MethodGet, path, own, "user", ""); w.Code != http.StatusOK {
		t.Fatalf("本人读自己的记录 got=%d body=%s", w.Code, w.Body.String())
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, path + "/favorite", `{"is_favorite":true}`},
		{http.MethodPost, path + "/rate", `{"rating":4}`},
	} {
		if w := aiCtrlCall(router, c.method, c.path, own, "user", c.body); w.Code != http.StatusOK {
			t.Errorf("本人 %s %s got=%d body=%s", c.method, c.path, w.Code, w.Body.String())
		}
	}

	// 越权：换成 B 的身份，三条用户口一律 404（「不存在」语义），不能是 200 也不能是 500。
	peek := strconv.FormatUint(uint64(aiCtrlPeek), 10)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, path, ""},
		{http.MethodPost, path + "/favorite", `{"is_favorite":true}`},
		{http.MethodPost, path + "/rate", `{"rating":5}`},
	} {
		w := aiCtrlCall(router, c.method, c.path, peek, "user", c.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("B 的 %s %s got=%d body=%s，期望 404（越权不得写穿，也不得报成故障）",
				c.method, c.path, w.Code, w.Body.String())
		}
	}

	// 越权之后：行还在、且一个字段都没被改动（只带本人那两次写入）。
	var after model.AIGenerationRecord
	if err := database.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("越权调用后记录消失: %v", err)
	}
	if after.UserID != aiCtrlOwner || after.Input != "A 的输入原文" || after.IsSaved || !after.IsFavorite || after.Rating != 4 {
		t.Errorf("越权调用后的行 %+v，期望仍是 A 的、且只带本人那两次写入", after)
	}
}

// TestAIRecordAdminPortsAreRoleGatedNotOwnerGated 管理口：角色闸拦普通用户，管理员可跨归属操作。
func TestAIRecordAdminPortsAreRoleGatedNotOwnerGated(t *testing.T) {
	database := setupAIContentTestDB(t)
	router := aiCtrlRouter(t)

	rec := &model.AIGenerationRecord{
		UserID: aiCtrlOwner,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "A 的输入原文",
		Output: "A 的生成结果",
	}
	if err := database.Create(rec).Error; err != nil {
		t.Fatalf("种子记录失败: %v", err)
	}
	path := "/ai/records/" + strconv.FormatUint(uint64(rec.ID), 10)
	peek := strconv.FormatUint(uint64(aiCtrlPeek), 10)

	// 普通用户（哪怕就是归属人自己）打管理口：403。这一条把「save/delete 只在 admin 组」钉住——
	// 若有人把它们挪回用户组，这里变红，而不是靠代码评审口头约定。
	own := strconv.FormatUint(uint64(aiCtrlOwner), 10)
	if w := aiCtrlCall(router, http.MethodPost, path+"/save", own, "user", ""); w.Code != http.StatusForbidden {
		t.Errorf("普通用户 POST save got=%d body=%s，期望 403", w.Code, w.Body.String())
	}
	if w := aiCtrlCall(router, http.MethodDelete, path, own, "user", ""); w.Code != http.StatusForbidden {
		t.Errorf("普通用户 DELETE got=%d body=%s，期望 403", w.Code, w.Body.String())
	}

	// 管理员用**别人的** id 也能操作：管理口按租户收窄、不按 user_id 收窄（设计意图）。
	if w := aiCtrlCall(router, http.MethodPost, path+"/save", peek, "admin", ""); w.Code != http.StatusOK {
		t.Fatalf("管理员 save got=%d body=%s", w.Code, w.Body.String())
	}
	var saved model.AIGenerationRecord
	if err := database.First(&saved, rec.ID).Error; err != nil || !saved.IsSaved {
		t.Errorf("管理员 save 后的行 %+v err=%v，期望 is_saved=true", saved, err)
	}
	if w := aiCtrlCall(router, http.MethodDelete, path, peek, "admin", ""); w.Code != http.StatusOK {
		t.Fatalf("管理员 delete got=%d body=%s", w.Code, w.Body.String())
	}
	if err := database.First(&model.AIGenerationRecord{}, rec.ID).Error; err == nil {
		t.Error("管理员删除后物理行仍在")
	}
}

// TestAIHistoryListIsPerUserAndRequiresIdentity 列表口只看自己那条身份链，且必须有身份。
func TestAIHistoryListIsPerUserAndRequiresIdentity(t *testing.T) {
	database := setupAIContentTestDB(t)
	router := aiCtrlRouter(t)

	database.Create(&model.AIGenerationRecord{UserID: aiCtrlOwner, Type: model.AIGenerationTypeTitle, Input: "A2", Output: "o"})

	w := aiCtrlCall(router, http.MethodGet, "/ai/history", strconv.FormatUint(uint64(aiCtrlPeek), 10), "user", "")
	var payload struct {
		Code int `json:"code"`
		Data struct {
			List  []model.AIGenerationRecord `json:"list"`
			Total int64                      `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析 B 的历史响应失败: %v body=%s", err, w.Body.String())
	}
	if payload.Data.Total != 0 || len(payload.Data.List) != 0 {
		t.Errorf("B 看到 %d 条（total=%d），期望 0 条：%+v", len(payload.Data.List), payload.Data.Total, payload.Data.List)
	}

	// 身份缺失：handler 必须自己拦下 401，不能带着 uid=0 去查库。
	if w := aiCtrlCall(router, http.MethodGet, "/ai/history", "", "user", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("无身份查历史 got=%d body=%s，期望 401", w.Code, w.Body.String())
	}
}
