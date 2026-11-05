// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// PerformanceTestController 性能压测控制器
type PerformanceTestController struct {
	svc *service.PerformanceTestService
}

// NewPerformanceTestController 创建控制器
func NewPerformanceTestController() *PerformanceTestController {
	return &PerformanceTestController{svc: service.NewPerformanceTestService()}
}

// RunTest 启动压测
func (c *PerformanceTestController) RunTest(ctx *gin.Context) {
	var req service.TestRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	record, err := c.svc.RunTest(ctx.Request.Context(), req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "压测启动失败: "+err.Error())
		return
	}
	response.Success(ctx, record, "压测已启动")
}

// GetResult 压测结果
func (c *PerformanceTestController) GetResult(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	r, err := c.svc.GetResult(uint(id))
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "压测记录不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, r, "查询成功")
}

// ListResults 压测历史
func (c *PerformanceTestController) ListResults(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.ListResults(page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}
