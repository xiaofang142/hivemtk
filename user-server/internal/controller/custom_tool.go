// custom_tool.go P1-2：OpenAPI 工具导入 HTTP 面（list/import/delete）。
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CustomToolController 自定义工具控制器
type CustomToolController struct {
	svc *service.CustomToolService
}

// NewCustomToolController 构造
func NewCustomToolController() *CustomToolController {
	return &CustomToolController{svc: service.NewCustomToolService()}
}

// @Summary      导入自定义工具
// @Tags      custom-tool
// @Router    /api/custom-tools/import [post]
// Import POST /api/custom-tools/import
func (c *CustomToolController) Import(ctx *gin.Context) {
	var in service.ImportInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	n, rec, err := c.svc.Import(ctx.Request.Context(), in)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"tool_count": n, "record": rec}, "导入成功")
}

// @Summary      自定义工具列表
// @Tags      custom-tool
// @Router    /api/custom-tools [get]
// List GET /api/custom-tools
func (c *CustomToolController) List(ctx *gin.Context) {
	out, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, out, "获取成功")
}

// @Summary      删除自定义工具
// @Tags      custom-tool
// @Router    /api/custom-tools/{id} [delete]
// Delete DELETE /api/custom-tools/:id
func (c *CustomToolController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"deleted": id}, "删除成功")
}
