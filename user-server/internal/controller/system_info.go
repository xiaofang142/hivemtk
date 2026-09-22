package controller

import (
	"hivemtk-user/internal/pkg/shared/service"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/system/install"

	"github.com/gin-gonic/gin"
)

type SystemInfoController struct {
	service *service.SystemStatsService
}

func NewSystemInfoController() *SystemInfoController {
	return &SystemInfoController{
		service: service.NewSystemStatsService(),
	}
}

// GetSystemInfo 获取系统信息
// @Summary 获取系统信息
// @Description 获取系统基本信息，包括版本、运行环境等
// @Tags 系统
// @Produce json
// @Success 200 {object} object{data=service.SystemInfo} "获取成功"
// @Router /api/system/info [get]
func (c *SystemInfoController) GetSystemInfo(ctx *gin.Context) {
	info, err := c.service.GetSystemInfo()
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取系统信息失败")
		return
	}

	response.Success(ctx, info, "获取系统信息成功")
}

func (c *SystemInfoController) Health(ctx *gin.Context) {
	response.Success(ctx, gin.H{"status": "ok"}, "ok")
}

// SystemMenus 返回前端侧边栏菜单树。
// 当前开源版无动态菜单配置，返回空数组供前端做初始渲染占位。
func (c *SystemInfoController) SystemMenus(ctx *gin.Context) {
	response.Success(ctx, []any{}, "ok")
}

// IsSystemInitialized 检查系统是否已完成安装（install.lock.initialized=true）。
// 供 router / init 流程调用，避免路由层直接依赖 install 包。
func IsSystemInitialized() bool {
	return install.GetStatus().Initialized
}
