// Package service 内容域话术模板服务。
//
// 双体系定位（T-5，MASTER_COMPETITIVE_DECISIONS.md M10/T-5）：
//   - 本文件及 script_templates 表：管理端 CRUD 模板库 —— 运营人工维护、分类管理、
//     管理端增删改查入口，面向内容运营。
//   - internal/model.ScriptLibrary（script_library 表）：运行时引擎话术库 ——
//     异议处理（objection_handler）与 SalesEngine 检索使用，面向会话执行链路。
//
// 两库不合并代码（避免破坏管理端），通过 script_template_sync.go 提供的
// SyncToLibrary 单向同步：template → library 幂等 upsert。
package service

import (
	"context"
	"encoding/json"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"strings"

	"gorm.io/gorm"
)

// ScriptTemplateService 话术模板服务
type ScriptTemplateService struct {
	templateRepo *repository.ScriptTemplateRepository
	categoryRepo *repository.ScriptCategoryRepository
}

// ScriptTemplateDeps 话术模板服务的仓储依赖。
type ScriptTemplateDeps struct {
	TemplateRepo *repository.ScriptTemplateRepository
	CategoryRepo *repository.ScriptCategoryRepository
}

// NewScriptTemplateDeps 由装配处用数据库句柄构造依赖包。
func NewScriptTemplateDeps(db *gorm.DB) ScriptTemplateDeps {
	return ScriptTemplateDeps{
		TemplateRepo: repository.NewScriptTemplateRepository(db),
		CategoryRepo: repository.NewScriptCategoryRepository(db),
	}
}

// NewScriptTemplateService 创建话术模板服务（依赖由装配处注入）。
func NewScriptTemplateService(deps ScriptTemplateDeps) *ScriptTemplateService {
	return &ScriptTemplateService{
		templateRepo: deps.TemplateRepo,
		categoryRepo: deps.CategoryRepo,
	}
}

// CreateScriptTemplateRequest 创建话术模板请求
type CreateScriptTemplateRequest struct {
	Name      string   `json:"name"`
	Category  string   `json:"category" binding:"required"`
	Title     string   `json:"title" binding:"required"`
	Content   string   `json:"content" binding:"required"`
	Variables []string `json:"variables"`
	Tags      string   `json:"tags"`
	IsPublic  bool     `json:"is_public"`
}

// CreateTemplate 创建话术模板
func (s *ScriptTemplateService) CreateTemplate(ctx context.Context, createdBy uint, req *CreateScriptTemplateRequest) (*model.ScriptTemplate, error) {
	variables, _ := json.Marshal(req.Variables)

	template := &model.ScriptTemplate{
		Category:  req.Category,
		Name:      req.Name,
		Title:     req.Title,
		Content:   req.Content,
		Variables: string(variables),
		Tags:      req.Tags,
		IsPublic:  req.IsPublic,
		CreatedBy: createdBy,
	}

	if err := s.templateRepo.Create(ctx, template); err != nil {
		return nil, err
	}

	return template, nil
}

// GetTemplateList 获取话术模板列表
func (s *ScriptTemplateService) GetTemplateList(ctx context.Context, category string, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	return s.templateRepo.GetAll(ctx, category, page, pageSize)
}

// GetTemplateByID 获取话术模板详情
func (s *ScriptTemplateService) GetTemplateByID(ctx context.Context, id uint) (*model.ScriptTemplate, error) {
	return s.templateRepo.GetByID(ctx, id)
}

// UpdateTemplateRequest 更新话术模板请求
type UpdateTemplateRequest struct {
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Variables    []string `json:"variables"`
	Tags         string   `json:"tags"`
	JourneyStage string   `json:"journey_stage"`
	IsPublic     bool     `json:"is_public"`
}

// UpdateTemplate 更新话术模板。
//
// 读前置必须走同一个 ctx：它既是「本租户才改得到」的归属门，也是 Save 那类
// 「按结构体快照重插」的替代方案——行不可见就在此处交 ErrRecordNotFound，
// 后面的列级 Update 再也不会碰到别租户或已删除的行。
func (s *ScriptTemplateService) UpdateTemplate(ctx context.Context, id uint, req *UpdateTemplateRequest) (*model.ScriptTemplate, error) {
	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		template.Name = req.Name
	}
	if req.Category != "" {
		template.Category = req.Category
	}
	if req.Title != "" {
		template.Title = req.Title
	}
	if req.Content != "" {
		template.Content = req.Content
	}
	if req.Variables != nil {
		variables, _ := json.Marshal(req.Variables)
		template.Variables = string(variables)
	}
	template.Tags = req.Tags
	template.JourneyStage = req.JourneyStage
	template.IsPublic = req.IsPublic

	if err := s.templateRepo.Update(ctx, template); err != nil {
		return nil, err
	}

	return template, nil
}

// DeleteTemplate 删除话术模板：命中 0 行由仓储交出缺失哨兵（出口 404），
// 不再「先查一次、把查到的值丢掉、再删一次」。
func (s *ScriptTemplateService) DeleteTemplate(ctx context.Context, id uint) error {
	return s.templateRepo.Delete(ctx, id)
}

// GetCategories 获取话术分类
func (s *ScriptTemplateService) GetCategories(ctx context.Context) ([]*model.ScriptCategory, error) {
	return s.categoryRepo.GetAll(ctx)
}

// CreateCategory 创建话术分类
func (s *ScriptTemplateService) CreateCategory(ctx context.Context, name string, parentID uint) (*model.ScriptCategory, error) {
	category := &model.ScriptCategory{
		Name:     name,
		ParentID: parentID,
	}

	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	return category, nil
}

// SearchTemplates 搜索话术
func (s *ScriptTemplateService) SearchTemplates(ctx context.Context, keyword string, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	return s.templateRepo.SearchTemplates(ctx, keyword, page, pageSize)
}

// UseTemplate 使用话术（增加使用次数）
func (s *ScriptTemplateService) UseTemplate(ctx context.Context, id uint) error {
	return s.templateRepo.IncrementUsage(ctx, id)
}

// GetPublicTemplates 获取公开话术模板
func (s *ScriptTemplateService) GetPublicTemplates(ctx context.Context, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	return s.templateRepo.GetPublicTemplates(ctx, page, pageSize)
}

// RecommendScript 推荐话术。
//
// 读模板的错误一律上抛：改前 `templates, _, _ := SearchTemplates(...)` 把错误丢在地上，
// 于是 on 模式下隔离闸拒绝读取（配置/上下文坏了）与「库里真没话术」长得一模一样，
// 出口还回 200 + 空列表。
func (s *ScriptTemplateService) RecommendScript(ctx context.Context, message string) ([]*model.ScriptTemplate, error) {
	templates, _, err := s.templateRepo.SearchTemplates(ctx, "", 1, 100)
	if err != nil {
		return nil, err
	}

	type scoredTemplate struct {
		template *model.ScriptTemplate
		score    float64
	}

	var scored []scoredTemplate
	for _, t := range templates {
		score := s.calculateRelevance(message, t)
		if score > 0.3 {
			scored = append(scored, scoredTemplate{template: t, score: score})
		}
	}

	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[i].score < scored[j].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	var result []*model.ScriptTemplate
	for i := 0; i < len(scored) && i < 5; i++ {
		result = append(result, scored[i].template)
	}

	return result, nil
}

func (s *ScriptTemplateService) calculateRelevance(message string, template *model.ScriptTemplate) float64 {
	message = strings.ToLower(message)
	content := strings.ToLower(template.Content)
	title := strings.ToLower(template.Title)

	matchCount := 0
	keywords := strings.Fields(content)
	for _, keyword := range keywords {
		if len(keyword) > 2 && strings.Contains(message, keyword) {
			matchCount++
		}
	}

	if len(keywords) == 0 {
		return 0
	}

	score := float64(matchCount) / float64(len(keywords))

	if strings.Contains(message, title) {
		score += 0.3
	}

	return score
}
