// custom_tool.go P1-2：OpenAPI 工具导入 HTTP 面（list/import/delete）。
package controller

import (
	"errors"
	"net/http"
	"strconv"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/errhttp"
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name、base_url、schema_json 均为必填（description / auth_header / auth_value / timeout_ms 可选）")
		return
	}
	n, rec, err := c.svc.Import(ctx.Request.Context(), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "导入工具集失败")
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
		replyCustomIntegrationError(ctx, err, "获取工具集列表失败")
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
		replyCustomIntegrationError(ctx, err, "删除工具集失败")
		return
	}
	response.Success(ctx, gin.H{"deleted": id}, "删除成功")
}

// replyCustomIntegrationError 统一工具集/MCP 配置的错误出口：
// 业务码 4004→404、4009→409、其余 4xxx→400（由 ErrorFromDB 转），未知错误泛化为 fallbackMsg。
func replyCustomIntegrationError(ctx *gin.Context, err error, fallbackMsg string) {
	var be *bizerr.BizError
	if errors.As(err, &be) {
		switch be.Code {
		case bizerr.CodeNotFound:
			response.NotFound(ctx, be.Error())
			return
		case bizerr.CodeConflict:
			response.Error(ctx, http.StatusConflict, be.Error())
			return
		}
	}
	if errhttp.IsNotFoundError(err) {
		response.NotFound(ctx, fallbackMsg)
		return
	}
	errhttp.HandleDBError(ctx, err, fallbackMsg)
}
