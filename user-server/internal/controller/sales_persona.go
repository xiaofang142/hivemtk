// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SalesPersonaController 销冠能力画像控制器
type SalesPersonaController struct {
	svc *service.SalesPersonaService
}

// NewSalesPersonaController 创建控制器
func NewSalesPersonaController() *SalesPersonaController {
	return &SalesPersonaController{svc: service.NewSalesPersonaService()}
}

// GetReport godoc
// @Summary      销冠能力画像
// @Description  根据员工 ID 生成多维度能力画像报告（沟通力/转化力/产品力等）
// @Tags         Sales Persona
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "员工 ID"
// @Success      200  {object}  response.Response  "成功"
// @Failure      400  {object}  response.Response  "参数错误"
// @Router       /api/sales-persona/{id} [get]
func (c *SalesPersonaController) GetReport(ctx *gin.Context) {
	staffIDStr := ctx.Param("id")
	staffID, err := strconv.ParseUint(staffIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的员工ID")
		return
	}
	rep, err := c.svc.BuildReport(ctx.Request.Context(), uint(staffID))
	if err != nil {
		response.ErrorFromDB(ctx, err, "报告生成失败: "+err.Error())
		return
	}
	response.Success(ctx, rep, "报告生成成功")
}

// ListStaffs godoc
// @Summary      销冠员工列表
// @Description  返回所有员工 ID 和基础信息
// @Tags         Sales Persona
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/sales-persona/staffs [get]
func (c *SalesPersonaController) ListStaffs(ctx *gin.Context) {
	staffs, err := c.svc.ListStaffs(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithList(ctx, staffs, int64(len(staffs)))
}
