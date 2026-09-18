package controller

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SystemOpsController 系统运维控制器：日志查看、系统统计、备份与恢复。
// 注意：测试环境不挂载鉴权中间件，故对用户态字段缺失做兼容（默认 0）。
type SystemOpsController struct {
	backupService  *service.BackupService
	restoreService *service.RestoreService
	monitorService *service.SystemMonitorService
}

// NewSystemOpsController 创建系统运维控制器实例
func NewSystemOpsController() *SystemOpsController {
	return &SystemOpsController{
		backupService:  service.NewBackupService(),
		restoreService: service.NewRestoreService(),
		monitorService: service.NewSystemMonitorService(),
	}
}

func currentUserID(ctx *gin.Context) uint {
	if v, ok := ctx.Get("user_id"); ok {
		if uid, ok2 := v.(uint); ok2 {
			return uid
		}
	}
	return 0
}

// RestartServer 系统热重启（高危，仅限 admin）。
// 实际重启由进程守护（systemd/Docker restart=always）接管：先回写响应，
// 仅当显式设置 ALLOW_SELF_RESTART=true 时才退出进程，避免误杀开发/调试进程。
func (c *SystemOpsController) RestartServer(ctx *gin.Context) {
	logger.Info("收到系统热重启请求")
	response.Success(ctx, nil, "重启指令已下发，服务即将重启")
	if os.Getenv("ALLOW_SELF_RESTART") == "true" {
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Exit(0)
		}()
	}
}

// GetSystemLogs 查看日志文件内容。file 参数缺省时读取默认日志。
func (c *SystemOpsController) GetSystemLogs(ctx *gin.Context) {
	file := ctx.Query("file")
	var logPath string
	if file == "" {
		logPath = defaultLogPath()
	} else {
		resolved, err := resolveLogPath(file)
		if err != nil {
			logger.Errorf("resolveLogPath failed: %v", err)
			response.Error(ctx, http.StatusBadRequest, "非法日志路径")
			return
		}
		logPath = resolved
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		content = []byte{}
	}
	response.Success(ctx, gin.H{
		"path":    logPath,
		"content": string(content),
	}, "获取成功")
}

func defaultLogPath() string {
	abs, err := filepath.Abs(filepath.Join("logs", "app.log"))
	if err != nil {
		return filepath.Join("logs", "app.log")
	}
	return abs
}

// GetSystemStats 获取系统统计信息
func (c *SystemOpsController) GetSystemStats(ctx *gin.Context) {
	stats, err := c.monitorService.GetSystemStats(ctx.Request.Context())
	if err != nil {
		logger.Errorf("GetSystemStats failed: %v", err)
		response.ErrorFromDB(ctx, err, "获取系统统计失败")
		return
	}
	response.Success(ctx, stats, "获取成功")
}

// GetBackupList 获取备份列表
func (c *SystemOpsController) GetBackupList(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	backups, total, err := c.backupService.GetBackupList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		logger.Errorf("GetBackupList failed: %v", err)
		response.ErrorFromDB(ctx, err, "获取备份列表失败")
		return
	}
	response.Success(ctx, gin.H{
		"list":      backups,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// CreateBackup 创建备份
func (c *SystemOpsController) CreateBackup(ctx *gin.Context) {
	var req service.CreateBackupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Errorf("CreateBackup bind failed: %v", err)
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}

	createdBy := currentUserID(ctx)
	backup, err := c.backupService.CreateBackup(ctx.Request.Context(), createdBy, &req)
	if err != nil {
		logger.Errorf("CreateBackup failed: %v", err)
		response.ErrorFromDB(ctx, err, "创建备份失败")
		return
	}
	response.Success(ctx, backup, "创建备份任务成功")
}

// RestoreBackup 恢复备份
func (c *SystemOpsController) RestoreBackup(ctx *gin.Context) {
	var req service.RestoreBackupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Errorf("RestoreBackup bind failed: %v", err)
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}

	createdBy := currentUserID(ctx)
	record, err := c.restoreService.RestoreBackup(ctx.Request.Context(), createdBy, &req)
	if err != nil {
		logger.Errorf("RestoreBackup failed: %v", err)
		if strings.Contains(err.Error(), "不存在") {
			response.Error(ctx, http.StatusNotFound, "备份记录不存在")
		} else {
			response.ErrorFromDB(ctx, err, "创建恢复任务失败")
		}
		return
	}
	response.Success(ctx, record, "创建恢复任务成功")
}

func resolveLogPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("路径为空")
	}
	cleaned := filepath.Clean(p)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("检测到路径遍历")
	}

	allowedBases := []string{
		"/var/log/user-server",
		"/var/log/marketing",
	}
	if filepath.IsAbs(cleaned) {
		for _, base := range allowedBases {
			if cleaned == base || strings.HasPrefix(cleaned, base+string(os.PathSeparator)) {
				return cleaned, nil
			}
		}
		return "", fmt.Errorf("路径超出允许的日志目录")
	}

	if !strings.HasPrefix(cleaned, "logs"+string(os.PathSeparator)) {
		return "", fmt.Errorf("相对路径必须位于 logs/ 下")
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	return abs, nil
}
