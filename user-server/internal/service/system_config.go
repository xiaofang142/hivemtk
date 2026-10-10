package service

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"context"

	"gorm.io/gorm"
)

// SystemConfigService 系统配置服务
type SystemConfigService struct {
	repo repository.SystemConfigRepository
}

// NewSystemConfigService 创建系统配置服务实例
func NewSystemConfigService() *SystemConfigService {
	return &SystemConfigService{repo: repository.NewSystemConfigRepository()}
}

// GetConfig 获取系统配置
func (s *SystemConfigService) GetConfig(ctx context.Context) (*model.SystemConfig, error) {
	config, err := s.repo.GetConfig(ctx)
	if err != nil {
		return s.defaultConfig(ctx), nil
	}
	return config, nil
}

// ResolveUploadMaxBytes 解析「系统配置里的上传上限」，返回**字节**；返回 0 表示"库里没有可用的值"。
//
// 刻意不走 GetConfig：那条路读不到行时会拿 defaultConfig() 的 50MB 顶上，
// 而上传链路真正生效的兜底是 controller/upload.go 的 10MB。
// 跟着 GetConfig 走等于把存量站点的上限悄悄从 10MB 放宽到 5 倍，且运维不知情。
// 这里只认"行真实存在且 > 0"的值；其余（没行 / 值非法 / 库没指对）一律 0，
// 由调用方按自己的口径兜底——0 是"我没读到值"，不是"上限是 0"。
func (s *SystemConfigService) ResolveUploadMaxBytes(ctx context.Context) int64 {
	if s == nil || s.repo == nil {
		return 0
	}
	config, err := s.repo.GetConfig(ctx)
	if err != nil || config == nil || config.MaxUploadSizeMB <= 0 {
		return 0
	}
	return int64(config.MaxUploadSizeMB) * 1024 * 1024
}

// SaveConfig 保存系统配置
func (s *SystemConfigService) SaveConfig(ctx context.Context, config *model.SystemConfig) (*model.SystemConfig, error) {
	if config == nil {
		return nil, gorm.ErrInvalidData
	}
	if config.MaxUsers < 0 {
		config.MaxUsers = 0
	}
	if config.MaxUploadSizeMB <= 0 {
		config.MaxUploadSizeMB = 50
	}
	if config.ThemeColor == "" {
		config.ThemeColor = "#409EFF"
	}
	return s.repo.SaveConfig(ctx, config)
}

// SaveBasicConfig 仅更新应用基础配置（名称、站点 URL），由 service 组装 model 实体
func (s *SystemConfigService) SaveBasicConfig(ctx context.Context, appName, websiteURL string) (*model.SystemConfig, error) {
	return s.SaveConfig(ctx, &model.SystemConfig{
		Name:       appName,
		WebsiteURL: websiteURL,
	})
}

// GetUsageStats 统计用量信息（用户数、请求数近似值）
// 供 app-config 上报使用，避免 controller 直连数据库
func (s *SystemConfigService) GetUsageStats(ctx context.Context) (userCount int64, requestCount int64) {
	if n, err := s.repo.CountUsers(ctx); err == nil {
		userCount = n
	}
	return userCount, 0
}

// PingDB 检查数据库连通性，供健康检查使用
func (s *SystemConfigService) PingDB(ctx context.Context) bool {
	return s.repo.PingDB(ctx)
}

func (s *SystemConfigService) defaultConfig(ctx context.Context) *model.SystemConfig {
	return &model.SystemConfig{
		Name:                 "",
		WebsiteURL:           "",
		LogoURL:              "",
		ThemeColor:           "#409EFF",
		SEOKeywords:          "",
		SEODescription:       "",
		ServicePhone:         "",
		ServiceEmail:         "",
		ICPRecord:            "",
		PoliceRecord:         "",
		EnableRegister:       true,
		EnableEmailMarketing: true,
		EnableRAG:            true,
		MaintenanceMode:      false,
		MaxUsers:             0,
		MaxUploadSizeMB:      50,
	}
}
