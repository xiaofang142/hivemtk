package controller

import (
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// BackupController 备份控制器
type BackupController struct {
	backupService *service.BackupService
}

const (
	// DefaultBackupPageSizeCap 备份列表单页条数上限，对应参数中心 misc.backup_page_size。
	// 代码兜底值，参数读不到时用它。
	DefaultBackupPageSizeCap = 1000

	// defaultBackupPageSize 备份列表默认每页条数。
	//
	// 与上面那个上限刻意分开：种子里那条的默认值是 1000，只有当"单页上限"才说得通。
	// 当"默认分页大小"接的话，升级后备份列表每页会从 10 行静默变成 1000 行，
	// 一页几百条记录，且没人会觉得那是配置生效的结果。
	defaultBackupPageSize = 10
)

var backupPageSizeCapProvider func() int

// SetBackupPageSizeCapProvider 注入备份列表单页上限的取值来源（装配层用）。
func SetBackupPageSizeCapProvider(fn func() int) { backupPageSizeCapProvider = fn }

func backupPageSizeCap() int {
	if backupPageSizeCapProvider == nil {
		return DefaultBackupPageSizeCap
	}
	if v := backupPageSizeCapProvider(); v > 0 {
		return v
	}
	return DefaultBackupPageSizeCap
}

// clampBackupPageSize 把入参规整进 [1, 单页上限]。
func clampBackupPageSize(pageSize int) int {
	if pageSize <= 0 {
		return defaultBackupPageSize
	}
	if cap := backupPageSizeCap(); pageSize > cap {
		return cap
	}
	return pageSize
}

// atoiOrDefault 解析失败（含空串、脏字符）时回落到 def，而不是回落 0。
func atoiOrDefault(raw string, def int) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

// NewBackupController 创建备份控制器实例
func NewBackupController() *BackupController {
	return &BackupController{
		backupService: service.NewBackupService(),
	}
}

func isAdmin(ctx *gin.Context) bool {
	role, exists := ctx.Get("role")
	if !exists {
		return false
	}
	roleStr, ok := role.(string)
	if !ok {
		return false
	}
	return roleStr == "admin"
}

func requireAdminAuth(ctx *gin.Context) (uint, bool) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return 0, false
	}
	uid, ok := userID.(uint)
	if !ok || uid == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return 0, false
	}
	if !isAdmin(ctx) {
		response.Error(ctx, http.StatusForbidden, "仅管理员可执行备份操作")
		return 0, false
	}
	return uid, true
}

// CreateBackup 创建备份
func (c *BackupController) CreateBackup(ctx *gin.Context) {
	uid, ok := requireAdminAuth(ctx)
	if !ok {
		return
	}
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	var req service.CreateBackupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	backup, err := c.backupService.CreateBackup(ctx.Request.Context(), uid, &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, backup, "创建备份任务成功")
}

// GetBackupList 获取备份列表
func (c *BackupController) GetBackupList(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	if _, ok := userID.(uint); !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize := clampBackupPageSize(atoiOrDefault(ctx.DefaultQuery("page_size", ""), defaultBackupPageSize))

	backups, total, err := c.backupService.GetBackupList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      backups,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetBackupByID 获取备份详情
func (c *BackupController) GetBackupByID(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	if _, ok := userID.(uint); !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的备份 ID")
		return
	}

	backup, err := c.backupService.GetBackupByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, backup, "获取成功")
}

// DeleteBackup 删除备份
func (c *BackupController) DeleteBackup(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	if _, ok := userID.(uint); !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的备份 ID")
		return
	}

	if HandleDBError(ctx, c.backupService.DeleteBackup(ctx.Request.Context(), uint(id)), "删除备份") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// RestoreController 恢复控制器
type RestoreController struct {
	restoreService *service.RestoreService
}

// NewRestoreController 创建恢复控制器实例
func NewRestoreController() *RestoreController {
	return &RestoreController{
		restoreService: service.NewRestoreService(),
	}
}

// RestoreBackup 恢复备份
func (c *RestoreController) RestoreBackup(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	var req service.RestoreBackupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	record, err := c.restoreService.RestoreBackup(ctx.Request.Context(), uid, &req)
	if HandleDBError(ctx, err, "恢复备份") {
		return
	}

	response.Success(ctx, record, "创建恢复任务成功")
}

// GetRestoreList 获取恢复记录列表
func (c *RestoreController) GetRestoreList(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	if _, ok := userID.(uint); !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	records, total, err := c.restoreService.GetRestoreList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      records,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetLastRestore 获取最近一次恢复记录
//
// 无恢复记录是正常状态（从未执行过恢复），应降级返回 code=0, data=null，
// 而不是 NOT_FOUND_1002。只有真正的数据库错误才返回错误。
func (c *RestoreController) GetLastRestore(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	if _, ok := userID.(uint); !ok {
		response.Error(ctx, http.StatusUnauthorized, "无效的用户信息")
		return
	}

	record, err := c.restoreService.GetLastRestore(ctx.Request.Context())
	if err != nil {
		if utils.IsRecordNotFound(err) {

			response.Success(ctx, nil, "暂无恢复记录")
			return
		}
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, record, "获取成功")
}
