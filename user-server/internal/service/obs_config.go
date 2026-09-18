// Package service 提供 OBS（对象存储配置）业务逻辑层。
//
// 存储驱动已下沉到 internal/storage 包；本层负责：
//  1. CRUD + SetDefault + TestConnection（配置管理）
//  2. UploadFile —— 查默认配置 → 构造 Driver → 调用 Drive
package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"
	"mime/multipart"
	"os"
	"path/filepath"
)

// ObsConfigService OBS（对象存储）配置服务
//
// 开源版：已移除 LicenseID 维度隔离（不再做"按 License 分组"的查询/默认设置）。
// 列表/默认配置走"全局"语义：第一个创建即为默认。
type ObsConfigService interface {
	GetConfigList(ctx context.Context, page, limit int, provider, status string) (*dto.GetObsConfigListResponse, error)
	GetConfig(ctx context.Context, id string) (*dto.ObsConfigResponse, error)
	CreateConfig(ctx context.Context, req *dto.CreateObsConfigRequest) (*dto.ObsConfigResponse, error)
	UpdateConfig(ctx context.Context, id string, req *dto.UpdateObsConfigRequest) (*dto.ObsConfigResponse, error)
	DeleteConfig(ctx context.Context, id string) error
	SetDefaultConfig(ctx context.Context, id string) error
	TestConnection(ctx context.Context, config *dto.ObsConfigResponse) error
	// UploadFile 业务入口：查默认配置 → 构造 Driver → 写文件 → 返回公开 URL
	UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, error)
	GetDefaultConfig(ctx context.Context) (*dto.ObsConfigResponse, error)
}

type obsConfigService struct {
	repo repository.ObsConfigRepository
}

func NewObsConfigService() ObsConfigService {
	return &obsConfigService{
		repo: repository.NewObsConfigRepository(),
	}
}

func (s *obsConfigService) GetConfigList(ctx context.Context, page, limit int, provider, status string) (*dto.GetObsConfigListResponse, error) {
	configs, total, err := s.repo.GetList(ctx, page, limit, provider, status)
	if err != nil {
		return nil, err
	}

	list := make([]*dto.ObsConfigResponse, len(configs))
	for i, config := range configs {
		list[i] = s.convertToDTO(ctx, config)
	}

	return &dto.GetObsConfigListResponse{
		List:  list,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

func (s *obsConfigService) GetConfig(ctx context.Context, id string) (*dto.ObsConfigResponse, error) {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.convertToDTO(ctx, config), nil
}

func (s *obsConfigService) CreateConfig(ctx context.Context, req *dto.CreateObsConfigRequest) (*dto.ObsConfigResponse, error) {
	if err := s.validateCreateRequest(ctx, req); err != nil {
		return nil, err
	}

	config := &model.ObsConfig{
		Name:       req.Name,
		Provider:   model.ObsProvider(req.Provider),
		AccessKey:  req.AccessKey,
		SecretKey:  req.SecretKey,
		Bucket:     req.Bucket,
		Region:     req.Region,
		Endpoint:   req.Endpoint,
		Domain:     req.Domain,
		PathPrefix: req.PathPrefix,
		Config:     req.Config,
		MaxSize:    req.MaxSize,
		MaxCount:   req.MaxCount,
		Status:     model.ObsStatusActive,
	}

	count, err := s.repo.Count(ctx)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		config.IsDefault = true
	}

	err = s.repo.Create(ctx, config)
	if err != nil {
		return nil, err
	}

	return s.convertToDTO(ctx, config), nil
}

func (s *obsConfigService) UpdateConfig(ctx context.Context, id string, req *dto.UpdateObsConfigRequest) (*dto.ObsConfigResponse, error) {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		config.Name = req.Name
	}
	if req.Provider != "" {
		config.Provider = model.ObsProvider(req.Provider)
	}
	if req.AccessKey != "" {
		config.AccessKey = req.AccessKey
	}
	if req.SecretKey != "" {
		config.SecretKey = req.SecretKey
	}
	if req.Bucket != "" {
		config.Bucket = req.Bucket
	}
	if req.Region != "" {
		config.Region = req.Region
	}
	if req.Endpoint != "" {
		config.Endpoint = req.Endpoint
	}
	if req.Domain != "" {
		config.Domain = req.Domain
	}
	if req.PathPrefix != "" {
		config.PathPrefix = req.PathPrefix
	}
	if req.Config != "" {
		config.Config = req.Config
	}
	if req.MaxSize > 0 {
		config.MaxSize = req.MaxSize
	}
	if req.MaxCount > 0 {
		config.MaxCount = req.MaxCount
	}

	err = s.repo.Update(ctx, config)
	if err != nil {
		return nil, err
	}

	return s.convertToDTO(ctx, config), nil
}

func (s *obsConfigService) DeleteConfig(ctx context.Context, id string) error {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if config.IsDefault {
		return errors.New("不能删除默认配置")
	}

	return s.repo.Delete(ctx, id)
}

func (s *obsConfigService) SetDefaultConfig(ctx context.Context, id string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}

	if err := s.repo.ClearDefault(ctx); err != nil {
		return err
	}

	return s.repo.SetDefault(ctx, id)
}

func (s *obsConfigService) TestConnection(ctx context.Context, config *dto.ObsConfigResponse) error {
	switch model.ObsProvider(config.Provider) {
	case model.ObsProviderAliyun:
		if config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
			return errors.New("阿里云OSS配置不完整")
		}
		if _, err := storage.Factory(obsConfigResponseToModel(config)); err != nil {
			return fmt.Errorf("驱动构造失败: %w", err)
		}
		logger.Infof("[OBS] Testing connection for Aliyun: bucket=%s, region=%s (配置格式OK)", config.Bucket, config.Region)
		return nil
	case model.ObsProviderQiniu:
		if config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
			return errors.New("七牛云存储配置不完整")
		}
		if _, err := storage.Factory(obsConfigResponseToModel(config)); err != nil {
			return fmt.Errorf("驱动构造失败: %w", err)
		}
		logger.Infof("[OBS] Testing connection for Qiniu: bucket=%s (配置格式OK)", config.Bucket)
		return nil
	case model.ObsProviderTencent:
		if config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
			return errors.New("腾讯云COS配置不完整")
		}
		if _, err := storage.Factory(obsConfigResponseToModel(config)); err != nil {
			return fmt.Errorf("驱动构造失败: %w", err)
		}
		logger.Infof("[OBS] Testing connection for Tencent: bucket=%s, region=%s (配置格式OK)", config.Bucket, config.Region)
		return nil
	case model.ObsProviderAWS:
		if config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
			return errors.New("AWS S3配置不完整")
		}
		if _, err := storage.Factory(obsConfigResponseToModel(config)); err != nil {
			return fmt.Errorf("驱动构造失败: %w", err)
		}
		logger.Infof("[OBS] Testing connection for AWS: bucket=%s (配置格式OK)", config.Bucket)
		return nil
	case model.ObsProviderLocal:

		baseDir := config.Endpoint
		if baseDir == "" {
			baseDir = os.Getenv("STORAGE_LOCAL_BASE_DIR")
		}
		if baseDir == "" {
			baseDir = "./uploads"
		}

		info, err := os.Stat(baseDir)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("local 存储目录不存在: %s（请先创建或检查路径配置）", baseDir)
			}
			return fmt.Errorf("local 存储目录访问失败: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("local 存储路径不是目录: %s", baseDir)
		}

		testFile := filepath.Join(baseDir, ".obs_test_write")
		if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
			return fmt.Errorf("local 存储目录不可写: %w", err)
		}
		if err := os.Remove(testFile); err != nil {
			logger.Warnf("[OBS] 清理测试写入文件失败 %s: %v", testFile, err)
		}
		logger.Infof("[OBS] Testing connection for local storage: OK (baseDir=%s, writable=true)", baseDir)
		return nil
	default:
		return errors.New("不支持的存储提供商")
	}
}

func (s *obsConfigService) UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, error) {
	config, err := s.repo.GetDefault(ctx)
	if err != nil {
		return "", fmt.Errorf("未找到默认存储配置: %w", err)
	}

	if !obsConfigIsFileSizeAllowed(config, header.Size) {
		return "", fmt.Errorf("文件大小超过限制: %d bytes (最大: %d bytes)", header.Size, config.MaxSize)
	}

	driver, err := storage.Factory(config)
	if err != nil {
		return "", fmt.Errorf("构造存储驱动失败: %w", err)
	}

	publicURL, storagePath, err := driver.UploadMultipart(ctx, file, header, folder)
	if err != nil {
		return "", fmt.Errorf("上传失败 (provider=%s, folder=%s): %w", config.Provider, folder, err)
	}

	logger.Infof("[OBS] file uploaded: provider=%s folder=%s path=%s url=%s",
		config.Provider, folder, storagePath, publicURL)

	config.FileCount++
	config.TotalSize += header.Size
	if err := s.repo.Update(ctx, config); err != nil {
		logger.Warnf("[OBS] update file count failed (non-blocking): %v", err)
	}

	return publicURL, nil
}

func (s *obsConfigService) GetDefaultConfig(ctx context.Context) (*dto.ObsConfigResponse, error) {
	config, err := s.repo.GetDefault(ctx)
	if err != nil {
		return nil, err
	}
	return s.convertToDTO(ctx, config), nil
}

func (s *obsConfigService) validateCreateRequest(ctx context.Context, req *dto.CreateObsConfigRequest) error {
	_ = ctx
	if req.Name == "" {
		return errors.New("配置名称不能为空")
	}
	provider := model.ObsProvider(req.Provider)
	switch provider {
	case model.ObsProviderLocal:

		if req.Endpoint == "" {
			if v := os.Getenv("STORAGE_LOCAL_BASE_DIR"); v != "" {
				req.Endpoint = v
			} else {
				req.Endpoint = "./uploads"
			}
		}
		if req.Domain == "" {
			if v := os.Getenv("STORAGE_LOCAL_PUBLIC_URL"); v != "" {
				req.Domain = v
			} else {
				req.Domain = "/files"
			}
		}
		return nil
	case model.ObsProviderAliyun, model.ObsProviderTencent, model.ObsProviderQiniu, model.ObsProviderAWS:
		if req.AccessKey == "" {
			return errors.New("云存储 AccessKey 不能为空")
		}
		if req.SecretKey == "" {
			return errors.New("云存储 SecretKey 不能为空")
		}
		if req.Bucket == "" {
			return errors.New("云存储 Bucket 不能为空")
		}
		return nil
	default:
		return fmt.Errorf("不支持的存储提供商: %s", req.Provider)
	}
}

func (s *obsConfigService) convertToDTO(ctx context.Context, config *model.ObsConfig) *dto.ObsConfigResponse {
	return &dto.ObsConfigResponse{
		ID:           config.ID,
		Name:         config.Name,
		Provider:     string(config.Provider),
		ProviderName: obsConfigProviderName(config),
		AccessKey:    config.AccessKey,
		SecretKey:    "***",
		Bucket:       config.Bucket,
		Region:       config.Region,
		Endpoint:     config.Endpoint,
		Domain:       config.Domain,
		PathPrefix:   config.PathPrefix,
		Config:       config.Config,
		MaxSize:      config.MaxSize,
		MaxCount:     config.MaxCount,
		Status:       string(config.Status),
		LastError:    config.LastError,
		LastTestAt:   config.LastTestAt,
		TotalSize:    config.TotalSize,
		FileCount:    config.FileCount,
		IsDefault:    config.IsDefault,
		CreatedAt:    config.CreatedAt,
		UpdatedAt:    config.UpdatedAt,
	}
}

func obsConfigProviderName(c *model.ObsConfig) string {
	switch c.Provider {
	case model.ObsProviderAliyun:
		return "阿里云OSS"
	case model.ObsProviderQiniu:
		return "七牛云存储"
	case model.ObsProviderTencent:
		return "腾讯云COS"
	case model.ObsProviderAWS:
		return "AWS S3"
	case model.ObsProviderLocal:
		return "本地存储"
	default:
		return "未知"
	}
}

func obsConfigIsFileSizeAllowed(c *model.ObsConfig, size int64) bool {
	return size <= c.MaxSize
}

func obsConfigResponseToModel(d *dto.ObsConfigResponse) *model.ObsConfig {
	if d == nil {
		return nil
	}
	return &model.ObsConfig{
		ID:         d.ID,
		Name:       d.Name,
		Provider:   model.ObsProvider(d.Provider),
		AccessKey:  d.AccessKey,
		SecretKey:  d.SecretKey,
		Bucket:     d.Bucket,
		Region:     d.Region,
		Endpoint:   d.Endpoint,
		Domain:     d.Domain,
		PathPrefix: d.PathPrefix,
		Config:     d.Config,
		MaxSize:    d.MaxSize,
		MaxCount:   d.MaxCount,
		Status:     model.ObsStatus(d.Status),
		LastError:  d.LastError,
		LastTestAt: d.LastTestAt,
		TotalSize:  d.TotalSize,
		FileCount:  d.FileCount,
		IsDefault:  d.IsDefault,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}
