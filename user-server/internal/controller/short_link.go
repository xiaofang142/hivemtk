package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ShortLinkController 短链控制器
type ShortLinkController struct {
	shortLinkService service.ShortLinkService
}

// NewShortLinkController 创建短链控制器实例
func NewShortLinkController(shortLinkService service.ShortLinkService) *ShortLinkController {
	return &ShortLinkController{
		shortLinkService: shortLinkService,
	}
}

// Create godoc
// @Summary      创建短链
// @Description  生成短码并指向长链，支持 UTM 参数自动追加
// @Tags         Short Link
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.CreateShortLinkRequest  true  "创建短链请求"
// @Success      200   {object}  response.Response  "成功"
// @Router       /api/short-links [post]
func (c *ShortLinkController) Create(ctx *gin.Context) {
	var req dto.CreateShortLinkRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	resp, err := c.shortLinkService.Create(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, resp, "创建成功")
}

// Update godoc
// @Summary      更新短链
// @Description  修改短链指向、过期时间
// @Tags         Short Link
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "短链 ID"
// @Param        body  body  dto.UpdateShortLinkRequest   true  "更新参数"
// @Success      200   {object}  response.Response  "成功"
// @Router       /api/short-links/{id} [put]
func (c *ShortLinkController) Update(ctx *gin.Context) {
	var req dto.UpdateShortLinkRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	idStr := ctx.Param("id")
	idFromURI, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID格式")
		return
	}

	if req.ID != uint(idFromURI) {
		response.Error(ctx, http.StatusBadRequest, "ID不一致: JSON中的ID与URI参数中的ID不匹配")
		return
	}

	resp, err := c.shortLinkService.Update(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "更新短链") {
		return
	}

	response.Success(ctx, resp, "更新成功")
}

// Delete 删除短链
func (c *ShortLinkController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	err = c.shortLinkService.Delete(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "删除短链") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetByID 根据ID获取短链
func (c *ShortLinkController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	resp, err := c.shortLinkService.GetByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取短链") {
		return
	}

	response.Success(ctx, resp, "获取成功")
}

// GetList 获取短链列表
func (c *ShortLinkController) GetList(ctx *gin.Context) {
	var req dto.ListShortLinkRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	resp, err := c.shortLinkService.GetList(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, resp, "获取成功")
}

// AccessShortLink 访问短链
func (c *ShortLinkController) AccessShortLink(ctx *gin.Context) {
	var req dto.AccessShortLinkRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	resp, err := c.shortLinkService.AccessShortLink(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, accessErrorStatus(err), err.Error())
		return
	}

	response.Success(ctx, resp, "访问成功")
}

func accessErrorStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "密码"):
		return http.StatusUnauthorized
	case strings.Contains(msg, "过期") || strings.Contains(msg, "禁用"):
		return http.StatusForbidden
	case strings.Contains(msg, "不存在") || strings.Contains(msg, "not found"):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

// GenerateShortCode 生成短码
func (c *ShortLinkController) GenerateShortCode(ctx *gin.Context) {
	var req dto.GenerateShortCodeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	resp, err := c.shortLinkService.GenerateShortCode(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, resp, "生成成功")
}
