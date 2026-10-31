package controller

import (
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/migration/migrations"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// MigrationController 数据库迁移控制器
//
// 暂保留旧名（UpgradeController / UpgradeTask*）以兼容现有调用。
type MigrationController struct {
	migrationService *migration.MigrationService
}

// NewMigrationController 创建迁移控制器实例
//
// migrationService 由 router 注入（router 负责创建 registry 并调用
// migration.NewMigrationService(registry, gormDB, migrations.RegisterMigrations)）。
//
// 测试场景下传入 nil 时自动构造默认服务（依赖 dbUtil.GetDB()，
// 由 testutil.NewTestDB + db.SetTestDB 设置全局 DB）。
func NewMigrationController(migrationService *migration.MigrationService) *MigrationController {
	if migrationService == nil {
		registry := migration.NewMigrationRegistry()
		migrationService = migration.NewMigrationServiceDefault(registry, migrations.RegisterMigrations)
	}
	return &MigrationController{migrationService: migrationService}
}

// @Summary      查询升级任务状态
// @Tags      migration
// @Router    /api/migration/task/{id} [get]
// GetUpgradeTask 获取迁移任务详情（保留方法名以兼容 router）
func (c *MigrationController) GetUpgradeTask(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的迁移任务 ID")
		return
	}

	task, err := c.migrationService.GetUpgradeTask(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, task, "获取成功")
}

// @Summary      查询升级历史记录
// @Tags      migration
// @Router    /api/migration/history [get]
// GetUpgradeHistory 获取迁移历史
func (c *MigrationController) GetUpgradeHistory(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	tasks, total, err := c.migrationService.GetUpgradeHistory(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      tasks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      查询迁移执行记录
// @Tags      migration
// @Router    /api/migration/records [get]
// GetMigrationRecords 获取迁移记录
func (c *MigrationController) GetMigrationRecords(ctx *gin.Context) {
	records, err := c.migrationService.GetMigrationRecords(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, records, "获取成功")
}

// @Summary      获取当前数据库版本
// @Tags      migration
// @Router    /api/migration/current-version [get]
// GetCurrentVersion 获取当前数据库版本
func (c *MigrationController) GetCurrentVersion(ctx *gin.Context) {
	version, err := c.migrationService.GetCurrentVersion(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"version": version}, "获取成功")
}

// @Summary      迁移任务已创建
// @Tags      migration
// @Router    /api/migration/task [post]
// CreateUpgradeTask 创建迁移任务
func (c *MigrationController) CreateUpgradeTask(ctx *gin.Context) {
	var req struct {
		ToVersion string `json:"to_version" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	currentVersion, err := c.migrationService.GetCurrentVersion(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	task, err := c.migrationService.ExecuteUpgrade(ctx.Request.Context(), currentVersion, req.ToVersion)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, task, "迁移任务已创建")
}

// @Summary 回滚到指定版本
// @Tags      migration
// @Router    /api/migration/rollback [post]
// Rollback 回滚到指定版本
func (c *MigrationController) Rollback(ctx *gin.Context) {
	var req struct {
		TargetVersion string `json:"target_version" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.migrationService.Rollback(ctx.Request.Context(), req.TargetVersion); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, nil, "回滚成功")
}

// @Summary      获取可执行的迁移列表
// @Tags      migration
// @Router    /api/migration/available [get]
// GetAvailableUpgrades 获取可执行的迁移列表
func (c *MigrationController) GetAvailableUpgrades(ctx *gin.Context) {
	currentVersion, err := c.migrationService.GetCurrentVersion(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	pendingMigrations, err := c.migrationService.GetPendingMigrations(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	type AvailableUpgrade struct {
		Version     string `json:"version"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsNext      bool   `json:"is_next"`
	}

	var upgrades []AvailableUpgrade
	for i, m := range pendingMigrations {
		upgrades = append(upgrades, AvailableUpgrade{
			Version:     m.Version(),
			Name:        m.Name(),
			Description: m.Description(),
			IsNext:      i == 0,
		})
	}

	response.Success(ctx, gin.H{
		"current_version":    currentVersion,
		"available_upgrades": upgrades,
	}, "获取成功")
}
