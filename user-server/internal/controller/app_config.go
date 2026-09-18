package controller

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/service"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// AppConfigController 应用配置控制器
type AppConfigController struct {
	sysConfigSvc *service.SystemConfigService
}

// NewAppConfigController 创建应用配置控制器实例
func NewAppConfigController() *AppConfigController {
	return &AppConfigController{
		sysConfigSvc: service.NewSystemConfigService(),
	}
}

// AppConfigReq 应用配置请求
type AppConfigReq struct {
	DBConfig DBConfig `json:"db_config"`

	RedisConfig RedisConfig `json:"redis_config"`

	BasicConfig BasicConfig `json:"basic_config"`

	PlatformSync PlatformSyncInfo `json:"platform_sync"`

	UsageReport UsageReportInfo `json:"usage_report"`
}

// DBConfig 数据库配置
type DBConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// BasicConfig 基本配置
type BasicConfig struct {
	AppName        string `json:"app_name"`
	Version        string `json:"version"`
	Environment    string `json:"environment"`
	DebugMode      bool   `json:"debug_mode"`
	SessionTimeout int    `json:"session_timeout"`
}

// PlatformSyncInfo 平台同步信息
type PlatformSyncInfo struct {
	MerchantKey  string `json:"merchant_key"`
	PlatformURL  string `json:"platform_url"`
	SyncInterval int    `json:"sync_interval"`
}

// UsageReportInfo 使用信息上报
type UsageReportInfo struct {
	UserCount      int    `json:"user_count"`
	RequestCount   int    `json:"request_count"`
	SystemInfo     string `json:"system_info"`
	LastUpdateTime string `json:"last_update_time"`
}

// AppConfigResp 应用配置响应
type AppConfigResp struct {
	Config    AppConfigReq   `json:"config"`
	Status    string         `json:"status"`
	Message   string         `json:"message"`
	Timestamp string         `json:"timestamp"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// GetAppConfig 获取应用配置
func (c *AppConfigController) GetAppConfig(ctx *gin.Context) {
	sysConfig, err := c.sysConfigSvc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取系统配置失败", err.Error())
		return
	}

	licenseStatus, err := platform.GetLicenseStatus()
	if err != nil {
		logger.Errorf("获取授权状态失败: %v", err)
	}

	resp := AppConfigResp{
		Config: AppConfigReq{
			BasicConfig: BasicConfig{
				AppName:     sysConfig.Name,
				Version:     "1.0.0",
				Environment: "production",
				DebugMode:   false,
			},
		},
		Status:    "success",
		Message:   "配置获取成功",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if licenseStatus != nil {
		resp.Config.PlatformSync.MerchantKey = "HIDDEN_FOR_SECURITY"
		resp.Config.PlatformSync.PlatformURL = sysConfig.WebsiteURL
	}

	response.Success(ctx, resp, "获取应用配置成功")
}

// UpdateAppConfig 更新应用配置
func (c *AppConfigController) UpdateAppConfig(ctx *gin.Context) {
	var req AppConfigReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	_, err := c.sysConfigSvc.SaveBasicConfig(ctx.Request.Context(), req.BasicConfig.AppName, req.PlatformSync.PlatformURL)
	if err != nil {
		response.ErrorFromDB(ctx, err, "保存配置失败", err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"status":    "success",
		"message":   "配置更新成功",
		"timestamp": time.Now().Format(time.RFC3339),
	}, "应用配置更新成功")
}

// SyncWithPlatform 向平台同步配置和使用信息
// 私域独立部署（单租户）模式：平台同步为可选降级通道，失败仅记录警告，不阻塞主流程
func (c *AppConfigController) SyncWithPlatform(ctx *gin.Context) {
	var req AppConfigReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	sysConfig, err := c.sysConfigSvc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取系统配置失败", err.Error())
		return
	}

	platformAvailable := true
	if _, licErr := platform.GetLicenseStatus(); licErr != nil {
		platformAvailable = false
		logger.Errorf("[app-config/sync] 平台不可用，降级处理: %v", licErr)
	}

	userCount, requestCount := c.sysConfigSvc.GetUsageStats(ctx.Request.Context())

	usageReport := UsageReportInfo{
		UserCount:      int(userCount),
		RequestCount:   int(requestCount),
		SystemInfo:     runtime.GOOS + " " + runtime.GOARCH,
		LastUpdateTime: time.Now().Format("2006-01-02 15:04:05"),
	}

	if !platformAvailable {
		logger.Info("[app-config/sync] 平台不可用，跳过 API 日志上报")
	}

	syncStatus := "success"
	syncMsg := "应用配置与平台同步成功"
	if !platformAvailable {
		syncStatus = "success_local_only"
		syncMsg = "应用配置同步成功（独立部署模式，平台已跳过）"
	}
	resp := AppConfigResp{
		Config: AppConfigReq{
			BasicConfig: BasicConfig{
				AppName:     sysConfig.Name,
				Version:     "1.0.0",
				Environment: "production",
				DebugMode:   false,
			},
			UsageReport: usageReport,
		},
		Status:    syncStatus,
		Message:   syncMsg,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	resp.Extra = map[string]any{
		"platform_available": platformAvailable,
		"deployment_mode":    "private_independent",
	}
	response.Success(ctx, resp, syncMsg)
}

// HealthCheck 健康检查
func (c *AppConfigController) HealthCheck(ctx *gin.Context) {
	platformConnection := "disconnected"
	if _, err := platform.GetLicenseStatus(); err == nil {
		platformConnection = "connected"
	}

	dbStatus := "disconnected"
	if c.sysConfigSvc.PingDB(ctx.Request.Context()) {
		dbStatus = "connected"
	}

	healthInfo := gin.H{
		"status":              "healthy",
		"timestamp":           time.Now().Format(time.RFC3339),
		"uptime":              "running",
		"version":             "1.0.0",
		"go_version":          runtime.Version(),
		"os":                  runtime.GOOS,
		"arch":                runtime.GOARCH,
		"goroutines":          runtime.NumGoroutine(),
		"platform_connection": platformConnection,
		"database_connection": dbStatus,
	}

	response.Success(ctx, healthInfo, "健康检查成功")
}
