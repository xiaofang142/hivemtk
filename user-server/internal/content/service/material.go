package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	contentdto "hivemtk-user/internal/content/dto"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/dto"
	usermodel "hivemtk-user/internal/model"
	userrepo "hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"
	"image"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ObsService 抽象 OBS 配置服务接口（避免与 system service 包循环依赖）
type ObsService interface {
	UploadFile(file multipart.File, header *multipart.FileHeader, licenseID string, folder string) (string, error)
	GetDefaultConfig(licenseID string) (*dto.ObsConfigResponse, error)
}

type MaterialService interface {
	GetCategoryList(licenseID string, parentID string, materialType string, page int, limit int) (*contentdto.GetMaterialCategoryListResponse, error)
	GetCategoryTree(licenseID string, materialType string) ([]*contentdto.MaterialCategoryResponse, error)
	GetCategory(id string) (*contentdto.MaterialCategoryResponse, error)
	CreateCategory(req *contentdto.CreateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error)
	UpdateCategory(id string, req *contentdto.UpdateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error)
	DeleteCategory(id string) error

	GetMaterialList(licenseID string, categoryID string, materialType string, search string, page int, limit int) (*contentdto.GetMaterialListResponse, error)
	GetMaterial(id string) (*contentdto.MaterialResponse, error)
	CreateMaterial(req *contentdto.CreateMaterialRequest) (*contentdto.MaterialResponse, error)
	UpdateMaterial(id string, req *contentdto.UpdateMaterialRequest) (*contentdto.MaterialResponse, error)
	DeleteMaterial(id string) error

	UploadMaterial(file multipart.File, header *multipart.FileHeader, req *contentdto.UploadMaterialRequest) (*contentdto.MaterialResponse, error)
	GetMaterialSelector(licenseID string, materialType string) (*contentdto.MaterialSelectorResponse, error)
	UpdateMaterialUsage(id string) error
	GetMaterialStats(licenseID string) (*contentdto.MaterialStatsResponse, error)
}

type materialService struct {
	categoryRepo repository.MaterialCategoryRepository
	materialRepo repository.MaterialRepository
	obsService   ObsService
}

func NewMaterialService() MaterialService {
	return &materialService{
		categoryRepo: repository.NewMaterialCategoryRepository(),
		materialRepo: repository.NewMaterialRepository(),
		obsService:   NewObsConfigServiceAdapter(),
	}
}

// NewMaterialServiceWithDB creates a new MaterialService with the given database connection (for testing)
func NewMaterialServiceWithDB(db *gorm.DB) MaterialService {
	return &materialService{
		categoryRepo: repository.NewMaterialCategoryRepositoryWithDB(db),
		materialRepo: repository.NewMaterialRepositoryWithDB(db),
		obsService:   NewObsConfigServiceAdapter(),
	}
}

type obsConfigServiceAdapter struct {
	cfgRepo userrepo.ObsConfigRepository
}

// NewObsConfigServiceAdapter 返回立即可用的 adapter（不再依赖外部注入）
func NewObsConfigServiceAdapter() *obsConfigServiceAdapter {
	return &obsConfigServiceAdapter{
		cfgRepo: userrepo.NewObsConfigRepository(),
	}
}

func (a *obsConfigServiceAdapter) UploadFile(file multipart.File, header *multipart.FileHeader, licenseID string, folder string) (string, error) {
	_ = licenseID

	cfg, err := a.cfgRepo.GetDefault(context.Background())
	if err != nil {
		return "", fmt.Errorf("no default obs config: %w", err)
	}

	driver, err := storage.Factory(cfg)
	if err != nil {
		return "", fmt.Errorf("storage factory: %w", err)
	}

	publicURL, _, err := driver.UploadMultipart(context.Background(), file, header, folder)
	if err != nil {
		return "", fmt.Errorf("driver upload: %w", err)
	}
	return publicURL, nil
}

func (a *obsConfigServiceAdapter) GetDefaultConfig(licenseID string) (*dto.ObsConfigResponse, error) {
	_ = licenseID
	cfg, err := a.cfgRepo.GetDefault(context.Background())
	if err != nil {
		return nil, fmt.Errorf("no default obs config: %w", err)
	}
	return obsModelToDTO(cfg), nil
}

func obsModelToDTO(c *usermodel.ObsConfig) *dto.ObsConfigResponse {
	return &dto.ObsConfigResponse{
		ID:         c.ID,
		Name:       c.Name,
		Provider:   string(c.Provider),
		AccessKey:  c.AccessKey,
		Bucket:     c.Bucket,
		Region:     c.Region,
		Endpoint:   c.Endpoint,
		Domain:     c.Domain,
		PathPrefix: c.PathPrefix,
		MaxSize:    c.MaxSize,
		MaxCount:   c.MaxCount,
		IsDefault:  c.IsDefault,
		Status:     string(c.Status),
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

func (s *materialService) GetCategoryList(licenseID string, parentID string, materialType string, page int, limit int) (*contentdto.GetMaterialCategoryListResponse, error) {
	categories, total, err := s.categoryRepo.GetList(licenseID, parentID, materialType, page, limit)
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

func (s *materialService) GetCategoryTree(licenseID string, materialType string) ([]*contentdto.MaterialCategoryResponse, error) {
	categories, err := s.categoryRepo.GetTree(licenseID, materialType)
	if err != nil {
		return nil, err
	}

	list := make([]*contentdto.MaterialCategoryResponse, len(categories))
	for i, category := range categories {
		list[i] = s.convertCategoryToDTO(category)
	}

	return list, nil
}

func (s *materialService) GetCategory(id string) (*contentdto.MaterialCategoryResponse, error) {
	category, err := s.categoryRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return s.convertCategoryToDTO(category), nil
}

func (s *materialService) CreateCategory(req *contentdto.CreateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error) {

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

	err := s.categoryRepo.Create(category)
	if err != nil {
		return nil, err
	}

	return s.convertCategoryToDTO(category), nil
}

func (s *materialService) UpdateCategory(id string, req *contentdto.UpdateMaterialCategoryRequest) (*contentdto.MaterialCategoryResponse, error) {
	category, err := s.categoryRepo.GetByID(id)
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

	err = s.categoryRepo.Update(category)
	if err != nil {
		return nil, err
	}

	return s.convertCategoryToDTO(category), nil
}

func (s *materialService) DeleteCategory(id string) error {
	return s.categoryRepo.Delete(id)
}

func (s *materialService) GetMaterialList(licenseID string, categoryID string, materialType string, search string, page int, limit int) (*contentdto.GetMaterialListResponse, error) {
	materials, total, err := s.materialRepo.GetList(licenseID, categoryID, materialType, search, page, limit)
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

func (s *materialService) GetMaterial(id string) (*contentdto.MaterialResponse, error) {
	material, err := s.materialRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) CreateMaterial(req *contentdto.CreateMaterialRequest) (*contentdto.MaterialResponse, error) {
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

	err := s.materialRepo.Create(material)
	if err != nil {
		return nil, err
	}

	if req.CategoryID != "" {
		s.categoryRepo.UpdateMaterialCount(req.CategoryID)
	}

	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) UpdateMaterial(id string, req *contentdto.UpdateMaterialRequest) (*contentdto.MaterialResponse, error) {
	material, err := s.materialRepo.GetByID(id)
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

	err = s.materialRepo.Update(material)
	if err != nil {
		return nil, err
	}

	return s.convertMaterialToDTO(material), nil
}

func (s *materialService) DeleteMaterial(id string) error {
	material, err := s.materialRepo.GetByID(id)
	if err != nil {
		return err
	}

	err = s.materialRepo.Delete(id)
	if err != nil {
		return err
	}

	if material.CategoryID != "" {
		s.categoryRepo.UpdateMaterialCount(material.CategoryID)
	}

	return nil
}

func (s *materialService) UploadMaterial(file multipart.File, header *multipart.FileHeader, req *contentdto.UploadMaterialRequest) (*contentdto.MaterialResponse, error) {
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	fileHash := hex.EncodeToString(hash.Sum(nil))

	existing, err := s.materialRepo.GetByHash(fileHash, req.LicenseID)
	if err == nil && existing != nil {
		return s.convertMaterialToDTO(existing), nil
	}

	if _, err := file.(io.Seeker).Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	fileName := fmt.Sprintf("materials/%s%s", time.Now().Format("20060102150405"), ext)

	fileURL, err := s.obsService.UploadFile(file, header, req.LicenseID, "materials")
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
		StoragePath: fileName,
		LicenseID:   req.LicenseID,
		UserID:      req.UserID,
		Tags:        req.Tags,
		Description: req.Description,
	}

	return s.CreateMaterial(createMaterial)
}

func (s *materialService) GetMaterialSelector(licenseID string, materialType string) (*contentdto.MaterialSelectorResponse, error) {
	categories, err := s.GetCategoryTree(licenseID, materialType)
	if err != nil {
		return nil, err
	}

	materials, err := s.GetMaterialList(licenseID, "", materialType, "", 1, 50)
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

func (s *materialService) UpdateMaterialUsage(id string) error {
	material, err := s.materialRepo.GetByID(id)
	if err != nil {
		return err
	}

	material.UsageCount++
	now := time.Now()
	material.LastUsedAt = &now

	return s.materialRepo.Update(material)
}

func (s *materialService) GetMaterialStats(licenseID string) (*contentdto.MaterialStatsResponse, error) {
	materials, _, err := s.materialRepo.GetList(licenseID, "", "", "", 1, 1000)
	if err != nil {
		return nil, err
	}

	stats := &contentdto.MaterialStatsResponse{
		TotalMaterials: int64(len(materials)),
	}

	for _, material := range materials {
		switch material.Type {
		case "image":
			stats.ImageCount++
		case "video":
			stats.VideoCount++
		case "audio":
			stats.AudioCount++
		case "file":
			stats.FileCount++
		}
		stats.TotalSize += material.Size
		stats.TotalUsageCount += int64(material.UsageCount)

		if material.CreatedAt.Format("2006-01-02") == time.Now().Format("2006-01-02") {
			stats.TodayAddedCount++
		}

		if material.LastUsedAt != nil && material.LastUsedAt.Format("2006-01-02") == time.Now().Format("2006-01-02") {
			stats.TodayUsageCount++
		}
	}

	return stats, nil
}
