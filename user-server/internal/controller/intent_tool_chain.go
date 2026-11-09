// intent_tool_chain.go TB-01：意图→工具链绑定 HTTP 面（管理端 CRUD + 词表）。
// 五层归属：Handler（参数绑定+调 Service+统一响应出口），不含业务判断。
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// IntentToolChainController 意图→工具链绑定控制器
type IntentToolChainController struct {
	svc *service.IntentToolChainService
}

// NewIntentToolChainController 构造（svc 由装配层注入；nil 时端点回 503）。
func NewIntentToolChainController(svc *service.IntentToolChainService) *IntentToolChainController {
	return &IntentToolChainController{svc: svc}
}

func (c *IntentToolChainController) ready(ctx *gin.Context) bool {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "意图工具链服务未装配（InitIntentChainRuntime 未执行）")
		return false
	}
	return true
}

// List GET /api/intent/tool-chains?intent_code=&page=&page_size=
func (c *IntentToolChainController) List(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	list, total, err := c.svc.List(ctx.Request.Context(), ctx.Query("intent_code"), page, pageSize)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取意图工具链列表失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Create POST /api/intent/tool-chains
func (c *IntentToolChainController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var in service.ChainUpsertInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：intent_code 与 steps 为必填")
		return
	}
	rec, err := c.svc.Create(ctx.Request.Context(), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "创建意图工具链失败")
		return
	}
	response.Success(ctx, rec, "创建成功")
}

// Update PUT /api/intent/tool-chains/:id
func (c *IntentToolChainController) Update(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	var in service.ChainUpsertInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：intent_code 与 steps 为必填")
		return
	}
	rec, err := c.svc.Update(ctx.Request.Context(), uint(id), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "更新意图工具链失败")
		return
	}
	response.Success(ctx, rec, "更新成功")
}

// Delete DELETE /api/intent/tool-chains/:id
func (c *IntentToolChainController) Delete(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		replyCustomIntegrationError(ctx, err, "删除意图工具链失败")
		return
	}
	response.Success(ctx, gin.H{"deleted": id}, "删除成功")
}

// Vocab GET /api/intent/tool-chains/vocab
// 意图词表 + 模板变量白名单 + 注册中心工具名（管理端配置页单一事实源）。
func (c *IntentToolChainController) Vocab(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	response.Success(ctx, c.svc.GetChainVocabulary(), "获取成功")
}
