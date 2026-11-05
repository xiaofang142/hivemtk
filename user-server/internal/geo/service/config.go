package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"

	"gorm.io/gorm"
)

// ConfigService GEO 配置服务
type ConfigService struct {
	configRepo repository.GeoConfigRepository
	llm        *LLMAdapter
}

// NewConfigService 创建 GEO 配置服务
func NewConfigService(cr repository.GeoConfigRepository, adapter *LLMAdapter) *ConfigService {
	return &ConfigService{configRepo: cr, llm: adapter}
}

// GetConfig 获取 GEO 配置
// 首次使用（库中无配置）时返回默认配置而非 404，保证前端初始化体验
func (s *ConfigService) GetConfig(ctx context.Context) (*model.GeoConfig, error) {
	config, err := s.configRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.GeoConfig{Language: "zh"}, nil
		}
		return nil, err
	}
	return config, nil
}

// ConfigUpdateInput GEO 配置的合并更新入参。
//
// 用结构体而不是长参数列表：产品叙事的 5 个字段加进来后，位置参数已经 13 个，
// 调用点错位不会编译报错（同型 string 太多），排查成本高。
type ConfigUpdateInput struct {
	Brand            string
	BrandDescription string
	Advantages       string
	Competitors      []string
	Domain           string
	DefaultModel     string
	VerifyModels     []string
	NegativeKeywords []string

	ProductCategory string
	TargetAudience  string
	KeyFeatures     string
	GenerationNote  string
	VersionLabel    string
}

// UpdateConfig 更新 GEO 配置（合并更新，保留未提供的字段）
// negativeKeywords 为 nil 时保留现有负面监控种子词配置
func (s *ConfigService) UpdateConfig(ctx context.Context, in ConfigUpdateInput) error {
	existing, err := s.configRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		existing = &model.GeoConfig{Language: "zh"}
	}
	if in.Brand != "" {
		existing.BrandName = in.Brand
	}
	if in.BrandDescription != "" {
		existing.BrandDescription = in.BrandDescription
	}
	if in.Advantages != "" {
		existing.Advantages = in.Advantages
	}
	if len(in.Competitors) > 0 {
		existing.Competitors = strings.Join(in.Competitors, "、")
	}
	if in.Domain != "" {
		existing.Domain = in.Domain
	}
	if in.DefaultModel != "" {
		existing.DefaultModel = in.DefaultModel
	}
	if len(in.VerifyModels) > 0 {
		existing.VerifyModels = strings.Join(in.VerifyModels, "、")
	}
	if in.NegativeKeywords != nil {
		existing.NegativeKeywords = strings.Join(in.NegativeKeywords, ",")
	}
	if in.ProductCategory != "" {
		existing.ProductCategory = in.ProductCategory
	}
	if in.TargetAudience != "" {
		existing.TargetAudience = in.TargetAudience
	}
	if in.KeyFeatures != "" {
		existing.KeyFeatures = in.KeyFeatures
	}
	if in.GenerationNote != "" {
		existing.GenerationNote = in.GenerationNote
	}
	if in.VersionLabel != "" {
		existing.VersionLabel = in.VersionLabel
	}
	if existing.Language == "" {
		existing.Language = "zh"
	}
	return s.configRepo.Update(ctx, existing)
}

type configOptimizeResult struct {
	Summary             string           `json:"summary"`
	Suggestions         map[string]any   `json:"suggestions"`
	RecommendedVersions []map[string]any `json:"recommended_versions"`
	ExpectedEffects     map[string]any   `json:"expected_effects"`
}

// OptimizeConfig 优化 GEO 配置
func (s *ConfigService) OptimizeConfig(ctx context.Context, brandName, advantages string, competitors []string) (map[string]any, error) {
	competitorsStr := strings.Join(competitors, "、")
	prompt := ConfigOptimizePrompt(ctx, brandName, advantages, competitorsStr)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("配置优化失败: %w", err)
	}

	parsed := &configOptimizeResult{}
	jsonStr := extractJSONObject(resp.Content)
	if jsonStr != "" {
		_ = json.Unmarshal([]byte(jsonStr), parsed)
	}
	return configOptimizeToMap(parsed, resp.Provider, resp.Model), nil
}

func configOptimizeToMap(r *configOptimizeResult, provider, model string) map[string]any {
	return map[string]any{
		"provider":             provider,
		"model":                model,
		"summary":              r.Summary,
		"suggestions":          r.Suggestions,
		"recommended_versions": r.RecommendedVersions,
		"expected_effects":     r.ExpectedEffects,
	}
}
