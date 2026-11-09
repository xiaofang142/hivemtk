// custom_intent.go T-09：自定义意图词条 HTTP 面（管理端 CRUD，AdminAuth 闸）。
// 五层归属：Handler（参数绑定+调 Service+统一响应出口），不含业务判断。
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CustomIntentController 自定义意图词条控制器
type CustomIntentController struct {
	svc *service.CustomIntentService
}

func NewCustomIntentController(svc *service.CustomIntentService) *CustomIntentController {
	return &CustomIntentController{svc: svc}
}

func (c *CustomIntentController) ready(ctx *gin.Context) bool {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "自定义意图服务未装配（InitIntentChainRuntime 未执行）")
		return false
	}
	return true
}

// List GET /api/intent/custom?page=&page_size=
func (c *CustomIntentController) List(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	list, total, err := c.svc.List(ctx.Request.Context(), page, pageSize)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取自定义意图列表失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Create POST /api/intent/custom
func (c *CustomIntentController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var in service.CustomIntentUpsertInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：type/name/keywords 为必填")
		return
	}
	rec, err := c.svc.Create(ctx.Request.Context(), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "创建自定义意图失败")
		return
	}
	response.Success(ctx, rec, "创建成功（约 30 秒内进入识别词典）")
}

// Update PUT /api/intent/custom/:id（type 不可改）
func (c *CustomIntentController) Update(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	var in service.CustomIntentUpsertInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	rec, err := c.svc.Update(ctx.Request.Context(), uint(id), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "更新自定义意图失败")
		return
	}
	response.Success(ctx, rec, "更新成功")
}

// Delete DELETE /api/intent/custom/:id
func (c *CustomIntentController) Delete(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		replyCustomIntegrationError(ctx, err, "删除自定义意图失败")
		return
	}
	response.Success(ctx, gin.H{"deleted": id}, "删除成功")
}
