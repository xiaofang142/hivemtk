package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"strings"
	"time"

	contentdto "hivemtk-user/internal/content/dto"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	userrepo "hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"
)

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ObsService 抽象 OBS 配置服务接口（避免与 system service 包循环依赖：
// internal/service 反向依赖本包，见 internal/service/marketing_flow_sms_init.go:9）。
//
// 每个方法都收 ctx：默认存储配置落在 obs_config 这张租户表上，
// 用 context.Background() 读它在 on 模式被隔离闸拒（表现为「素材库上传永远失败」），
// 本地驱动也靠同一个 ctx 给落盘路径加 tenant_{id}/ 段（storage/local.go T011）。
type ObsService interface {
	UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error)
}

type MaterialService interface {
	GetCategoryList(ctx context.Context, licenseID string, parentID string, materialType string, page int, limit int) (*contentdto.GetMaterialCategoryListResponse, error)
	GetCategoryTree(ctx context.Context, licenseID string, materialType string) ([]*contentdto.MaterialCategoryResponse, error)
	GetCategory(ctx context.Context, id string) (*contentdto.MaterialCategoryResponse, error)
	CreateCategory(ctx context.Context, req *contentdto.CreateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error)
	UpdateCategory(ctx context.Context, id string, req *contentdto.UpdateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error)
	DeleteCategory(ctx context.Context, id string) error

	GetMaterialList(ctx context.Context, licenseID string, categoryID string, materialType string, search string, page int, limit int) (*contentdto.GetMaterialListResponse, error)
	GetMaterial(ctx context.Context, id string) (*contentdto.MaterialResponse, error)
	CreateMaterial(ctx context.Context, req *contentdto.CreateMaterialRequest) (*contentdto.MaterialResponse, error)
	UpdateMaterial(ctx context.Context, id string, req *contentdto.UpdateMaterialRequest) (*contentdto.MaterialResponse, error)
	DeleteMaterial(ctx context.Context, id string) error

	UploadMaterial(ctx context.Context, file multipart.File, header *multipart.FileHeader, req *contentdto.UploadMaterialRequest) (*contentdto.MaterialResponse, error)
	GetMaterialSelector(ctx context.Context, licenseID string, materialType string) (*contentdto.MaterialSelectorResponse, error)
	UpdateMaterialUsage(ctx context.Context, id string) error
	GetMaterialStats(ctx context.Context, licenseID string) (*contentdto.MaterialStatsResponse, error)
}

type materialService struct {
	categoryRepo repository.MaterialCategoryRepository
	materialRepo repository.MaterialRepository
	obsService   ObsService
}

// MaterialDeps 素材服务的仓储/OBS 依赖。
type MaterialDeps struct {
	CategoryRepo repository.MaterialCategoryRepository
	MaterialRepo repository.MaterialRepository
	ObsService   ObsService
}

// MaterialDeps 的装配（仓储/OBS adapter 构造）由装配处完成：internal/router
// 直接填本结构体，service 层不再持 *gorm.DB 形参（AV-04c）。

// NewMaterialService 创建素材服务（依赖由装配处注入）。
func NewMaterialService(deps MaterialDeps) MaterialService {
	return &materialService{
		categoryRepo: deps.CategoryRepo,
		materialRepo: deps.MaterialRepo,
		obsService:   deps.ObsService,
	}
}

type obsConfigServiceAdapter struct {
	cfgRepo userrepo.ObsConfigRepository
}

// NewObsConfigServiceAdapter 用装配点交来的 OBS 配置仓储构造 adapter
// （句柄→仓储的转换在装配处完成，AV-04c）。
func NewObsConfigServiceAdapter(cfgRepo userrepo.ObsConfigRepository) *obsConfigServiceAdapter {
	return &obsConfigServiceAdapter{
		cfgRepo: cfgRepo,
	}
}

func (a *obsConfigServiceAdapter) UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error) {
	cfg, err := a.cfgRepo.GetDefault(ctx)
	if err != nil {
		return "", "", fmt.Errorf("no default obs config: %w", err)
	}

	driver, err := storage.Factory(cfg)
	if err != nil {
		return "", "", fmt.Errorf("storage factory: %w", err)
	}

	publicURL, storagePath, err := driver.UploadMultipart(ctx, file, header, folder)
	if err != nil {
		return "", "", fmt.Errorf("driver upload: %w", err)
	}
	return publicURL, storagePath, nil
}

func (s *materialService) GetCategoryList(ctx context.Context, licenseID string, parentID string, materialType string, page int, limit int) (*contentdto.GetMaterialCategoryListResponse, error) {
	categories, total, err := s.categoryRepo.GetList(ctx, licenseID, parentID, materialType, page, limit)
	if err != nil {
		return nil, err
	}

	list := make([]contentdto.MaterialCategoryResponse, len(categories))
	for i, category := range categories {
		list[i] = *s.convertCategoryToDTO(category)
	}

	return &contentdto.GetMaterialCategoryListResponse{
		List:  list,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

func (s *materialService) GetCategoryTree(ctx context.Context, licenseID string, materialType string) ([]*contentdto.MaterialCategoryResponse, error) {
	categories, err := s.categoryRepo.GetTree(ctx, licenseID, materialType)
	if err != nil {
		return nil, err
	}

	list := make([]*contentdto.MaterialCategoryResponse, len(categories))
	for i, category := range categories {
		list[i] = s.convertCategoryToDTO(category)
	}

	return list, nil
}

func (s *materialService) GetCategory(ctx context.Context, id string) (*contentdto.MaterialCategoryResponse, error) {
	category, err := s.categoryRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.convertCategoryToDTO(category), nil
}

func (s *materialService) CreateCategory(ctx context.Context, req *contentdto.CreateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error) {

	var pid *string
	if req.ParentID != "" && req.ParentID != "0" {
		s := req.ParentID
		pid = &s
	}
	category := &model.MaterialCategory{
		Name:        req.Name,
		Type:        model.MaterialType(req.Type),
		ParentID:    pid,
		Icon:        req.Icon,
		Color:       req.Color,
		Sort:        req.Sort,
		Description: req.Description,
		LicenseID:   req.LicenseID,
		UserID:      req.UserID,
		Status:      "active",
	}

	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	return s.convertCategoryToDTO(category), nil
}

func (s *materialService) UpdateCategory(ctx context.Context, id string, req *contentdto.UpdateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error) {
	category, err := s.categoryRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		category.Name = req.Name
	}
	if req.Type != "" {
		category.Type = model.MaterialType(req.Type)
	}
	if req.ParentID != "" {
		s := req.ParentID
		category.ParentID = &s
	}
	if req.Icon != "" {
		category.Icon = req.Icon
	}
	if req.Color != "" {
		category.Color = req.Color
	}
	if req.Sort != 0 {
		category.Sort = req.Sort
	}
	if req.Description != "" {
		category.Description = req.Description
	}
	if req.Status != "" {
		category.Status = req.Status
	}

	if err := s.categoryRepo.Update(ctx, category); err != nil {
		return nil, err
	}

	return s.convertCategoryToDTO(category), nil
}

// DeleteCategory 把「分类非空」这条数据事实翻译成业务校验错误：
// 改前仓储伪造 gorm.ErrForeignKeyViolated 上抛（schema 里其实没有这条外键约束），
// 文案不含「不存在」⇒ 出口层只能给 500，运维看不出是「分类下还有东西」。
func (s *materialService) DeleteCategory(ctx context.Context, id string) error {
	err := s.categoryRepo.Delete(ctx, id)
	if errors.Is(err, repository.ErrMaterialCategoryNotEmpty) {
		return fmt.Errorf("%w: 素材分类下仍有子分类或素材，无法删除", utils.ErrInvalidInput)
	}
	return err
}

func (s *materialService) GetMaterialList(ctx context.Context, licenseID string, categoryID string, materialType string, search string, page int, limit int) (*contentdto.GetMaterialListResponse, error) {
	materials, total, err := s.materialRepo.GetList(ctx, licenseID, categoryID, materialType, search, page, limit)
	if err != nil {
		return nil, err
	}

	list := make([]contentdto.MaterialResponse, len(materials))
	for i, material := range materials {
		list[i] = *s.convertMaterialToDTO(material)
	}

	return &contentdto.GetMaterialListResponse{
		List:  list,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

func (s *materialService) GetMaterial(ctx context.Context, id string) (*contentdto.MaterialResponse, error) {
	material, err := s.materialRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) CreateMaterial(ctx context.Context, req *contentdto.CreateMaterialRequest) (*contentdto.MaterialResponse, error) {
	material := &model.Material{
		Name:        req.Name,
		Type:        model.MaterialType(req.Type),
		CategoryID:  req.CategoryID,
		URL:         req.URL,
		Size:        req.Size,
		MimeType:    req.MimeType,
		Hash:        req.Hash,
		Width:       req.Width,
		Height:      req.Height,
		Duration:    req.Duration,
		Provider:    req.Provider,
		StoragePath: req.StoragePath,
		LicenseID:   req.LicenseID,
		UserID:      req.UserID,
		Tags:        req.Tags,
		Description: req.Description,
		Status:      "active",
	}

	if err := s.materialRepo.Create(ctx, material); err != nil {
		return nil, err
	}

	if req.CategoryID != "" {
		if err := s.categoryRepo.UpdateMaterialCount(ctx, req.CategoryID); err != nil {
			logger.Warnf("[Material] 更新分类素材数量失败 categoryID=%s: %v", req.CategoryID, err)
		}
	}

	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) UpdateMaterial(ctx context.Context, id string, req *contentdto.UpdateMaterialRequest) (*contentdto.MaterialResponse, error) {
	material, err := s.materialRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		material.Name = req.Name
	}
	if req.CategoryID != "" {
		material.CategoryID = req.CategoryID
	}
	if req.Tags != "" {
		material.Tags = req.Tags
	}
	if req.Description != "" {
		material.Description = req.Description
	}
	if req.Status != "" {
		material.Status = req.Status
	}

	if err := s.materialRepo.Update(ctx, material); err != nil {
		return nil, err
	}

	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) DeleteMaterial(ctx context.Context, id string) error {
	material, err := s.materialRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.materialRepo.Delete(ctx, id); err != nil {
		return err
	}

	if material.CategoryID != "" {
		if err := s.categoryRepo.UpdateMaterialCount(ctx, material.CategoryID); err != nil {
			logger.Warnf("[Material] 删除后更新分类素材数量失败 categoryID=%s: %v", material.CategoryID, err)
		}
	}

	return nil
}

func (s *materialService) UploadMaterial(ctx context.Context, file multipart.File, header *multipart.FileHeader, req *contentdto.UploadMaterialRequest) (*contentdto.MaterialResponse, error) {
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	fileHash := hex.EncodeToString(hash.Sum(nil))

	existing, err := s.materialRepo.GetByHash(ctx, fileHash, req.LicenseID)
	if err == nil && existing != nil {
		return s.convertMaterialToDTO(existing), nil
	}

	if _, err := file.(io.Seeker).Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// storage_path 存驱动真正落盘的那个键：本地驱动按 tenant_{id}/materials/年/月/uuid.ext 生成，
	// 后续的 Download / Delete 都拿它去 resolve()。改前这里自己拼了个
	// "materials/<时间戳>.ext" 塞进列——库里记的路径在磁盘上从来不存在，
	// 还丢了租户段，等于把「这一行的对象落在谁家」的自证一起丢了。
	fileURL, storagePath, err := s.obsService.UploadFile(ctx, file, header, "materials")
	if err != nil {
		return nil, err
	}

	width, height := 0, 0
	duration := 0
	if strings.HasPrefix(mimeType, "image/") {
		config, _, err := image.DecodeConfig(file)
		if err == nil {
			width, height = config.Width, config.Height
		}
	}

	createMaterial := &contentdto.CreateMaterialRequest{
		Name:        req.Name,
		Type:        getMaterialTypeByMimeType(mimeType),
		CategoryID:  req.CategoryID,
		URL:         fileURL,
		Size:        header.Size,
		MimeType:    mimeType,
		Hash:        fileHash,
		Width:       width,
		Height:      height,
		Duration:    duration,
		Provider:    "obs",
		StoragePath: storagePath,
		LicenseID:   req.LicenseID,
		UserID:      req.UserID,
		Tags:        req.Tags,
		Description: req.Description,
	}

	return s.CreateMaterial(ctx, createMaterial)
}

func (s *materialService) GetMaterialSelector(ctx context.Context, licenseID string, materialType string) (*contentdto.MaterialSelectorResponse, error) {
	categories, err := s.GetCategoryTree(ctx, licenseID, materialType)
	if err != nil {
		return nil, err
	}

	materials, err := s.GetMaterialList(ctx, licenseID, "", materialType, "", 1, 50)
	if err != nil {
		return nil, err
	}

	materialList := make([]contentdto.MaterialResponse, len(materials.List))
	copy(materialList, materials.List)

	categoryList := make([]contentdto.MaterialCategoryResponse, len(categories))
	for i, category := range categories {
		categoryList[i] = *category
	}

	return &contentdto.MaterialSelectorResponse{
		Categories: categoryList,
		Materials:  materialList,
		Total:      materials.Total,
	}, nil
}

func (s *materialService) convertCategoryToDTO(category *model.MaterialCategory) *contentdto.MaterialCategoryResponse {
	resp := &contentdto.MaterialCategoryResponse{
		ID:            category.ID,
		Name:          category.Name,
		Type:          string(category.Type),
		ParentID:      strVal(category.ParentID),
		Icon:          category.Icon,
		Color:         category.Color,
		Sort:          category.Sort,
		Description:   category.Description,
		LicenseID:     category.LicenseID,
		UserID:        category.UserID,
		MaterialCount: category.MaterialCount,
		Status:        category.Status,
		CreatedAt:     category.CreatedAt,
		UpdatedAt:     category.UpdatedAt,
	}

	if category.Parent != nil {
		resp.Parent = s.convertCategoryToDTO(category.Parent)
	}

	if len(category.Children) > 0 {
		children := make([]contentdto.MaterialCategoryResponse, len(category.Children))
		for i, child := range category.Children {
			children[i] = *s.convertCategoryToDTO(&child)
		}
		resp.Children = children
	}

	return resp
}

func (s *materialService) convertMaterialToDTO(material *model.Material) *contentdto.MaterialResponse {
	resp := &contentdto.MaterialResponse{
		ID:          material.ID,
		Name:        material.Name,
		Type:        string(material.Type),
		CategoryID:  material.CategoryID,
		URL:         material.URL,
		Size:        material.Size,
		MimeType:    material.MimeType,
		Hash:        material.Hash,
		Width:       material.Width,
		Height:      material.Height,
		Duration:    material.Duration,
		Provider:    material.Provider,
		StoragePath: material.StoragePath,
		LicenseID:   material.LicenseID,
		UserID:      material.UserID,
		UsageCount:  material.UsageCount,
		LastUsedAt:  material.LastUsedAt,
		Status:      material.Status,
		Tags:        material.Tags,
		Description: material.Description,
		CreatedAt:   material.CreatedAt,
		UpdatedAt:   material.UpdatedAt,
	}

	if material.Category != nil {
		resp.Category = s.convertCategoryToDTO(material.Category)
	}

	return resp
}

func getMaterialTypeByMimeType(mimeType string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	} else if strings.HasPrefix(mimeType, "video/") {
		return "video"
	} else if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	return "file"
}

// UpdateMaterialUsage 走库内自增。改前这里读整行、++、再 Save 回去：
// 计次只该动 usage_count / last_used_at 两列，整行写回会把并发改过的别的列顶回陈旧快照，
// 并发计次本身也丢更新（实测 40 次并发后 usage_count 停在个位数）。
func (s *materialService) UpdateMaterialUsage(ctx context.Context, id string) error {
	return s.materialRepo.IncrementUsage(ctx, id)
}

// GetMaterialStats 由库侧聚合三查得到。改前是 GetList(..., 1, 1000) 取前一千行回 Go 里数：
// 素材过一千就静默少报，总体积与总使用次数一起少。
func (s *materialService) GetMaterialStats(ctx context.Context, licenseID string) (*contentdto.MaterialStatsResponse, error) {
	byType, err := s.materialRepo.StatsByType(ctx, licenseID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	todayAdded, err := s.materialRepo.CountCreatedSince(ctx, licenseID, today)
	if err != nil {
		return nil, err
	}
	todayUsed, err := s.materialRepo.CountUsedSince(ctx, licenseID, today)
	if err != nil {
		return nil, err
	}

	stats := &contentdto.MaterialStatsResponse{
		TodayAddedCount: todayAdded,
		TodayUsageCount: todayUsed,
	}

	for _, row := range byType {
		stats.TotalMaterials += row.Count
		stats.TotalSize += row.TotalSize
		stats.TotalUsageCount += row.TotalUsage
		switch model.MaterialType(row.Type) {
		case model.MaterialTypeImage:
			stats.ImageCount = row.Count
		case model.MaterialTypeVideo:
			stats.VideoCount = row.Count
		case model.MaterialTypeAudio:
			stats.AudioCount = row.Count
		case model.MaterialTypeFile:
			stats.FileCount = row.Count
		}
	}

	return stats, nil
}
