package controller

import (
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ObsConfigController 对象存储配置控制器（开源版）
//
// 开源版：License 模型已删除，授权流程下线，配置管理走"全局"语义。
// 第一个创建的配置即为默认配置，不需 license_id 入参。
type ObsConfigController struct {
	service service.ObsConfigService
}

// NewObsConfigController 创建对象存储配置控制器
func NewObsConfigController() *ObsConfigController {
	return &ObsConfigController{
		service: service.NewObsConfigService(),
	}
}

// @Summary      查询 OBS 存储配置列表
// @Tags      obs-config
// @Router    /api/obs-configs [get]
// GetConfigList 获取配置列表（全局）
func (c *ObsConfigController) GetConfigList(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	provider := ctx.Query("provider")
	status := ctx.Query("status")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	resp, err := c.service.GetConfigList(ctx.Request.Context(), page, limit, provider, status)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询OBS配置列表失败: "+err.Error())
		return
	}

	response.Success(ctx, resp, "查询成功")
}

// @Summary      按 ID 查询 OBS 存储配置
// @Tags      obs-config
// @Router    /api/obs-configs/{id} [get]
// GetConfig 获取配置详情
func (c *ObsConfigController) GetConfig(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, 400, "INVALID_PARAM", "配置ID不能为空")
		return
	}

	config, err := c.service.GetConfig(ctx.Request.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, 404, "NOT_FOUND", err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "INTERNAL_ERROR", err.Error())
		return
	}

	response.Success(ctx, config, "查询成功")
}

// @Summary      创建 OBS 存储配置
// @Tags      obs-config
// @Router    /api/obs/config [post]
// CreateConfig 创建配置
func (c *ObsConfigController) CreateConfig(ctx *gin.Context) {
	var req dto.CreateObsConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误: "+err.Error())
		return
	}

	config, err := c.service.CreateConfig(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, config, "创建成功")
}

// @Summary      更新 OBS 存储配置
// @Tags      obs-config
// @Router    /api/obs/config/{id} [put]
// UpdateConfig 更新配置
func (c *ObsConfigController) UpdateConfig(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, 400, "INVALID_PARAM", "配置ID不能为空")
		return
	}

	var req dto.UpdateObsConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误: "+err.Error())
		return
	}

	config, err := c.service.UpdateConfig(ctx.Request.Context(), id, &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, config, "更新成功")
}

// @Summary      删除 OBS 存储配置
// @Tags      obs-config
// @Router    /api/obs/config/{id} [delete]
// DeleteConfig 删除配置
func (c *ObsConfigController) DeleteConfig(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, 400, "INVALID_PARAM", "配置ID不能为空")
		return
	}

	if HandleDBError(ctx, c.service.DeleteConfig(ctx.Request.Context(), id), "删除OBS配置") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary 测试连接
// @Tags      obs-config
// @Router    /api/obs/config/{id}/test [post]
// TestConnection 测试连接
func (c *ObsConfigController) TestConnection(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, 400, "INVALID_PARAM", "配置ID不能为空")
		return
	}

	config, err := c.service.GetConfig(ctx.Request.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, 404, "NOT_FOUND", err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "INTERNAL_ERROR", err.Error())
		return
	}

	if err := c.service.TestConnection(ctx.Request.Context(), config); err != nil {
		response.Error(ctx, 400, "CONNECTION_FAILED", err.Error())
		return
	}

	response.Success(ctx, nil, "连接测试成功")
}

// @Summary 设为默认配置（开源版：全局唯一默认）
// @Tags      obs-config
// @Router    /api/obs/config/{id}/default [post]
// SetDefault 设为默认配置（开源版：全局唯一默认）
func (c *ObsConfigController) SetDefault(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, 400, "INVALID_PARAM", "配置ID不能为空")
		return
	}

	if err := c.service.SetDefaultConfig(ctx.Request.Context(), id); err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, 404, "NOT_FOUND", err.Error())
			return
		}
		response.Error(ctx, 400, "SET_DEFAULT_FAILED", err.Error())
		return
	}

	response.Success(ctx, nil, "设为默认配置成功")
}

// @Summary      暂无默认配置
// @Tags      obs-config
// @Router    /api/obs-configs/default [get]
// GetDefaultConfig 获取默认配置（开源版：全局默认）
func (c *ObsConfigController) GetDefaultConfig(ctx *gin.Context) {
	config, err := c.service.GetDefaultConfig(ctx.Request.Context())
	if err != nil {
		if isNotFoundError(err) {
			response.Success(ctx, nil, "暂无默认配置")
			return
		}
		response.ErrorFromDB(ctx, err, "INTERNAL_ERROR", err.Error())
		return
	}

	response.Success(ctx, config, "查询成功")
}
