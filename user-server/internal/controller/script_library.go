// script_library_controller.go 话术版本管理 + AB 曝光统计（T-6/T-7）
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ScriptLibraryController 话术版本/AB 控制器
type ScriptLibraryController struct {
	abSvc *service.ScriptABService
}

// NewScriptLibraryController 创建控制器
func NewScriptLibraryController() *ScriptLibraryController {

	return &ScriptLibraryController{abSvc: service.NewScriptABServiceFromGlobal()}
}

func parseUintParam(ctx *gin.Context, name string) (uint, bool) {
	raw := ctx.Param(name)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的 "+name+" 参数")
		return 0, false
	}
	return uint(id), true
}

// @Summary      查询话术库版本列表
// @Tags      script-library
// @Router    /api/script-library/{id}/versions [get]
// ListVersions GET /api/script-library/:id/versions
func (c *ScriptLibraryController) ListVersions(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	versions, err := c.abSvc.ListVersions(ctx.Request.Context(), id)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": versions, "total": len(versions)}, "ok")
}

// @Summary      新版本已发布并激活
// @Tags      script-library
// @Router    /api/script-library/{id}/versions [post]
// CreateVersion POST /api/script-library/:id/versions（发布新版本=创建快照并激活）
func (c *ScriptLibraryController) CreateVersion(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = ctx.ShouldBindJSON(&req)
	uid, _ := ctx.Get("user_id")
	createdBy, _ := uid.(uint)
	v, err := c.abSvc.CreateVersion(ctx.Request.Context(), id, req.Note, createdBy)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, v, "新版本已发布并激活")
}

// @Summary      版本已激活
// @Tags      script-library
// @Router    /api/script-library/{id}/versions/{vid}/activate [put]
// ActivateVersion PUT /api/script-library/:id/versions/:vid/activate（回滚=激活旧版本）
func (c *ScriptLibraryController) ActivateVersion(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	vid, ok := parseUintParam(ctx, "vid")
	if !ok {
		return
	}
	if err := c.abSvc.ActivateVersion(ctx.Request.Context(), id, int(vid)); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"script_id": id, "version": vid}, "版本已激活")
}

// @Summary      话术已过期下线
// @Tags      script-library
// @Router    /api/script-library/{id}/expire [post]
// ExpireScript POST /api/script-library/:id/expire（过期下线）
func (c *ScriptLibraryController) ExpireScript(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.abSvc.ExpireScript(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"script_id": id, "status": "expired"}, "话术已过期下线")
}

// @Summary      查询话术库 A/B 测试统计
// @Tags      script-library
// @Router    /api/script-library/{id}/ab-stats [get]
// GetABStats GET /api/script-library/:id/ab-stats
func (c *ScriptLibraryController) GetABStats(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	stats, err := c.abSvc.ABStats(ctx.Request.Context(), id)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, stats, "ok")
}

// @Summary      AB 配置已保存
// @Tags      script-library
// @Router    /api/script-library/{id}/ab-config [put]
// UpdateABConfig PUT /api/script-library/:id/ab-config
func (c *ScriptLibraryController) UpdateABConfig(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.ScriptABConfig
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if err := c.abSvc.SaveConfig(ctx.Request.Context(), id, req); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, req, "AB 配置已保存")
}

// @Summary POST /api/script-ab/conversion
// @Tags      script-library
// @Router    /api/script-ab/conversion [post]
// RecordConversion POST /api/script-ab/conversion（转化回写：one_id/conversation_id/outcome）
func (c *ScriptLibraryController) RecordConversion(ctx *gin.Context) {
	var req struct {
		OneID          string `json:"one_id" binding:"required"`
		ConversationID string `json:"conversation_id"`
		Outcome        string `json:"outcome"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if err := c.abSvc.RecordConversion(ctx.Request.Context(), req.OneID, req.ConversationID, req.Outcome); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"recorded": true}, "ok")
}
