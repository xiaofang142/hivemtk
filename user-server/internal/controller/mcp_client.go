package controller

// mcp_client.go P1-3：外部 MCP server 配置 HTTP 面（与 custom-tools 同形状）。

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// MCPClientController 外部 MCP server 管理。
type MCPClientController struct {
	svc *service.MCPClientService
}

// NewMCPClientController 构造。
func NewMCPClientController() *MCPClientController {
	return &MCPClientController{svc: service.NewMCPClientService()}
}

// @Summary      注册 MCP 服务
// @Tags      mcp-client
// @Router    /api/mcp-servers [post]
// Create POST /api/mcp-servers
func (c *MCPClientController) Create(ctx *gin.Context) {
	var req service.MCPImportInput
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体格式错误")
		return
	}
	row, tools, err := c.svc.Create(ctx.Request.Context(), req)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "创建 MCP 配置失败")
		return
	}
	response.Success(ctx, gin.H{"server": row, "tool_count": tools}, "已保存")
}

// @Summary      MCP 服务列表
// @Tags      mcp-client
// @Router    /api/mcp-servers [get]
// List GET /api/mcp-servers
func (c *MCPClientController) List(ctx *gin.Context) {
	rows, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取 MCP 配置列表失败")
		return
	}
	response.Success(ctx, rows, "获取成功")
}

// @Summary      删除 MCP 服务
// @Tags      mcp-client
// @Router    /api/mcp-servers/{id} [delete]
// Delete DELETE /api/mcp-servers/:id
func (c *MCPClientController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		// 不存在 ≠ 服务器故障：repo 对 0 行返回 gorm.ErrRecordNotFound，
		// 直接 500 会让前端把「这条早删了」读成「服务挂了」（A16）。
		// ErrorFromDB 统一把 gorm.ErrRecordNotFound 映射成 404（含被 %w 包裹的情况）
		response.ErrorFromDB(ctx, err, "MCP 配置不存在")
		return
	}
	response.Success(ctx, nil, "已删除")
}

// @Summary      启停 MCP 服务
// @Tags      mcp-client
// @Router    /api/mcp-servers/{id}/enabled [put]
// SetEnabled PUT /api/mcp-servers/:id/enabled
func (c *MCPClientController) SetEnabled(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体格式错误")
		return
	}
	row, tools, err := c.svc.SetEnabled(ctx.Request.Context(), uint(id), req.Enabled)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "启停 MCP 配置失败")
		return
	}
	response.Success(ctx, gin.H{"server": row, "tool_count": tools}, "已更新")
}

// @Summary      测试 MCP 服务连通性
// @Tags      mcp-client
// @Router    /api/mcp-servers/{id}/test [get]
// Test GET /api/mcp-servers/:id/test
func (c *MCPClientController) Test(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	names, err := c.svc.Test(ctx.Request.Context(), uint(id))
	if err != nil {
		replyCustomIntegrationError(ctx, err, "连通性测试失败")
		return
	}
	response.Success(ctx, gin.H{"tools": names, "tool_count": len(names)}, "连通成功")
}
