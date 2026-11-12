package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/utils/logger"
	"strings"

	"gorm.io/gorm"
)

// AIContentService AI 内容服务
type AIContentService struct {
	llmService   llm.LLMServiceInterface
	recordRepo   repository.AIGenerationRecordRepository
	templateRepo repository.PromptTemplateRepository
}

// AIContentDeps AI 内容服务的仓储依赖。
type AIContentDeps struct {
	RecordRepo   repository.AIGenerationRecordRepository
	TemplateRepo repository.PromptTemplateRepository
}

// AIContentDeps 的装配（仓储构造）由装配处完成：internal/router、internal/app
// 直接填本结构体，service 层不再持 *gorm.DB 形参（AV-04c）。

// NewAIContentService 创建 AI 内容服务实例（依赖由装配处注入）。
func NewAIContentService(deps AIContentDeps) *AIContentService {
	return &AIContentService{
		llmService:   llm.NewLLMService(),
		recordRepo:   deps.RecordRepo,
		templateRepo: deps.TemplateRepo,
	}
}

// GenerateContentRequest 生成内容请求
type GenerateContentRequest struct {
	Type       model.AIGenerationType `json:"type" binding:"required"`
	Input      string                 `json:"input" binding:"required"`
	TemplateID uint                   `json:"template_id"`
	Variables  map[string]any         `json:"variables"`
	Model      string                 `json:"model"`
}

// GenerateContentResponse 生成内容响应
type GenerateContentResponse struct {
	ID         uint   `json:"id"`
	Output     string `json:"output"`
	TokensUsed int    `json:"tokens_used"`
	Model      string `json:"model"`
}

// GenerateContent 生成内容
func (s *AIContentService) GenerateContent(ctx context.Context, userID uint, req *GenerateContentRequest) (*GenerateContentResponse, error) {
	apiKey := s.resolveAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("AI service not configured")
	}

	prompt, err := s.buildPrompt(ctx, req)
	if err != nil {
		return nil, err
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "gpt-3.5-turbo"
	}

	config := &llm.LLMConfig{
		Model:          modelName,
		APIType:        "openai",
		APIKey:         apiKey,
		BaseURL:        s.resolveBaseURL(),
		Temperature:    0.7,
		MaxTokens:      2000,
		ResponseFormat: "text",
	}

	output, err := s.llmService.Generate(ctx, config, prompt)
	if err != nil {
		return nil, fmt.Errorf("生成失败：%v", err)
	}

	tokensUsed := len(prompt)/4 + len(output)/4

	record := &model.AIGenerationRecord{
		UserID:     userID,
		Type:       req.Type,
		Input:      req.Input,
		Output:     output,
		TemplateID: req.TemplateID,
		Model:      modelName,
		TokensUsed: tokensUsed,
	}

	if err := s.recordRepo.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("生成记录落库失败：%v", err)
	}

	return &GenerateContentResponse{
		ID:         record.ID,
		Output:     output,
		TokensUsed: tokensUsed,
		Model:      modelName,
	}, nil
}

// buildPrompt 模板正文的读取走仓储：模板属于租户，跨租户的模板必须报「不存在」，
// 而闸门本身的故障（ctx 没绑、隔离被拒）不能被折叠成同一句话——
// 那会让运维去查「这个 id 为什么没有」，而真相是请求根本没带租户。
func (s *AIContentService) buildPrompt(ctx context.Context, req *GenerateContentRequest) (string, error) {
	if req.TemplateID > 0 {
		template, err := s.templateRepo.GetByID(ctx, req.TemplateID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", fmt.Errorf("模板不存在")
			}
			return "", err
		}
		return s.fillTemplate(template.Template, req.Variables)
	}

	return s.buildDefaultPrompt(req.Type, req.Input, req.Variables)
}

func (s *AIContentService) fillTemplate(template string, variables map[string]any) (string, error) {
	result := template
	for key, value := range variables {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
	}
	return result, nil
}

func (s *AIContentService) buildDefaultPrompt(typ model.AIGenerationType, input string, variables map[string]any) (string, error) {
	switch typ {
	case model.AIGenerationTypeCopywriting:
		return fmt.Sprintf("请为以下内容生成营销文案：\n\n%s\n\n要求：语言生动有感染力，突出核心卖点。", input), nil
	case model.AIGenerationTypeTitle:
		count := 5
		if c, ok := variables["count"].(float64); ok {
			count = int(c)
		}
		return fmt.Sprintf("请为以下内容生成%d个吸引人的标题：\n\n%s", count, input), nil
	case model.AIGenerationTypeSummary:
		return fmt.Sprintf("请为以下内容生成摘要：\n\n%s", input), nil
	case model.AIGenerationTypeReply:
		return fmt.Sprintf("请针对以下内容生成专业、友好的回复：\n\n%s", input), nil
	case model.AIGenerationTypeRewrite:
		style := "简洁明了"
		if s, ok := variables["style"].(string); ok {
			style = s
		}
		return fmt.Sprintf("请将以下内容改写，风格要求%s：\n\n%s", style, input), nil
	case model.AIGenerationTypeExpand:
		return fmt.Sprintf("请对以下内容进行扩写，增加更多细节和深度：\n\n%s", input), nil
	case model.AIGenerationTypePolish:
		return fmt.Sprintf("请润色以下内容，使其更加专业流畅：\n\n%s", input), nil
	case model.AIGenerationTypeKeywords:
		return fmt.Sprintf("请从以下内容中提取关键词：\n\n%s", input), nil
	case model.AIGenerationTypeAdCopy:
		return fmt.Sprintf("请为以下产品生成广告文案：\n\n%s\n\n要求：突出卖点，吸引点击，包含行动号召。", input), nil
	case model.AIGenerationTypeSocialPost:
		platform := "社交媒体"
		if p, ok := variables["platform"].(string); ok {
			platform = p
		}
		return fmt.Sprintf("请为%s平台生成一条帖子：\n\n%s\n\n要求：符合平台特点，吸引用户互动。", platform, input), nil
	case model.AIGenerationTypeScript:
		return fmt.Sprintf("请生成销售话术：\n\n%s\n\n要求：开场白吸引注意，突出价值，引导成交。", input), nil
	case model.AIGenerationTypeEmail:
		return fmt.Sprintf("请撰写一封邮件：\n\n%s\n\n要求：主题明确，内容简洁专业。", input), nil
	default:
		return input, nil
	}
}

// CreateHistoryRequest 创建生成历史记录请求
type CreateHistoryRequest struct {
	Type       model.AIGenerationType `json:"type" binding:"required"`
	Input      string                 `json:"input" binding:"required"`
	Output     string                 `json:"output"`
	TemplateID uint                   `json:"template_id"`
	Model      string                 `json:"model"`
}

// CreateHistory 直接创建生成历史记录（不调用外部 AI 服务）
func (s *AIContentService) CreateHistory(ctx context.Context, userID uint, req *CreateHistoryRequest) (*model.AIGenerationRecord, error) {
	modelName := req.Model
	if modelName == "" {
		modelName = "manual"
	}

	record := &model.AIGenerationRecord{
		UserID:     userID,
		Type:       req.Type,
		Input:      req.Input,
		Output:     req.Output,
		TemplateID: req.TemplateID,
		Model:      modelName,
	}

	if err := s.recordRepo.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("创建历史记录失败：%v", err)
	}

	return record, nil
}

// GetGenerationHistory 获取生成历史
func (s *AIContentService) GetGenerationHistory(ctx context.Context, userID uint, page, pageSize int, filters map[string]any) ([]*model.AIGenerationRecord, int64, error) {
	return s.recordRepo.GetByMerchantAndUser(ctx, userID, page, pageSize, filters)
}

// GetRecordByID 获取生成记录（只限归属人）
func (s *AIContentService) GetRecordByID(ctx context.Context, id, userID uint) (*model.AIGenerationRecord, error) {
	return s.recordRepo.GetByID(ctx, id, userID)
}

// SaveRecord 保存生成记录。管理口：只挂在 admin 路由组下、由 AdminAuthMiddleware 证明角色，
// 整理的是本租户的内容库，因此不按 user_id 收窄（租户边界仍由 ctx 保证）。
func (s *AIContentService) SaveRecord(ctx context.Context, id uint) error {
	return s.recordRepo.UpdateSaved(ctx, id, true)
}

// FavoriteRecord 收藏生成记录
func (s *AIContentService) FavoriteRecord(ctx context.Context, id, userID uint, isFavorite bool) error {
	return s.recordRepo.UpdateFavorite(ctx, id, userID, isFavorite)
}

// RateRecord 评分生成记录
func (s *AIContentService) RateRecord(ctx context.Context, id, userID uint, rating int) error {
	if rating < 1 || rating > 5 {
		return fmt.Errorf("评分必须在 1-5 之间")
	}
	return s.recordRepo.UpdateRating(ctx, id, userID, rating)
}

// DeleteRecord 删除生成记录。管理口，与 SaveRecord 同一权限宽度（只有 admin 路由挂它）。
// 不再先读一遍判存在：读改写之间那一步没有任何东西会失效，但多出来的那次 SELECT 会让
// 「查不到」和「不在这个租户」在出口混成两种错误码。
func (s *AIContentService) DeleteRecord(ctx context.Context, id uint) error {
	return s.recordRepo.Delete(ctx, id)
}

// PromptTemplateService 提示词模板服务
type PromptTemplateService struct {
	templateRepo repository.PromptTemplateRepository
}

// PromptTemplateDeps 提示词模板服务的仓储依赖。
type PromptTemplateDeps struct {
	TemplateRepo repository.PromptTemplateRepository
}

// PromptTemplateDeps 的装配同 AIContentDeps：仓储构造收口在装配处（AV-04c）。

// NewPromptTemplateService 创建提示词模板服务实例（依赖由装配处注入）。
func NewPromptTemplateService(deps PromptTemplateDeps) *PromptTemplateService {
	return &PromptTemplateService{templateRepo: deps.TemplateRepo}
}

// CreateTemplateRequest 创建模板请求
type CreateTemplateRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Type        model.AIGenerationType `json:"type" binding:"required"`
	Template    string                 `json:"template" binding:"required"`
	Variables   string                 `json:"variables"`
	Description string                 `json:"description"`
	Example     string                 `json:"example"`
}

// GetTemplates 获取模板列表
func (s *PromptTemplateService) GetTemplates(ctx context.Context, templateType string) ([]*model.PromptTemplate, error) {
	return s.templateRepo.ListByType(ctx, templateType)
}

// GetTemplateByID 获取模板详情
func (s *PromptTemplateService) GetTemplateByID(ctx context.Context, id uint) (*model.PromptTemplate, error) {
	return s.templateRepo.GetByID(ctx, id)
}

// CreateTemplate 创建模板
func (s *PromptTemplateService) CreateTemplate(ctx context.Context, req *CreateTemplateRequest) (*model.PromptTemplate, error) {
	template := &model.PromptTemplate{
		Name:        req.Name,
		Type:        req.Type,
		Template:    req.Template,
		Variables:   req.Variables,
		Description: req.Description,
		Example:     req.Example,
		IsSystem:    false,
	}

	if err := s.templateRepo.Create(ctx, template); err != nil {
		return nil, err
	}

	return template, nil
}

// UpdateTemplate 更新模板
func (s *PromptTemplateService) UpdateTemplate(ctx context.Context, id uint, req *CreateTemplateRequest) (*model.PromptTemplate, error) {
	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if template.IsSystem {
		return nil, fmt.Errorf("系统模板不能修改")
	}

	template.Name = req.Name
	template.Type = req.Type
	template.Template = req.Template
	template.Variables = req.Variables
	template.Description = req.Description
	template.Example = req.Example

	if err := s.templateRepo.Update(ctx, template); err != nil {
		return nil, err
	}

	return template, nil
}

// DeleteTemplate 删除模板
func (s *PromptTemplateService) DeleteTemplate(ctx context.Context, id uint) error {
	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if template.IsSystem {
		return fmt.Errorf("系统模板不能删除")
	}

	return s.templateRepo.Delete(ctx, id)
}

// InitSystemTemplates 初始化系统模板。
//
// **生产零调用方**（见 model-list #14 的 4.4：这是一条没接上的引导，而不是可以删掉的死码——
// `is_system` 那两条守卫与 `ListByType` 的 `is_system DESC` 排序都靠这批种子才有意义）。
// 接线时 ctx 必须是**已绑定租户**的 ctx：模板行落在哪个租户由 ctx 决定，
// 用 `context.Background()`（bootstrap 的常态）在 on 模式会被隔离 callback 当场拒绝，
// 而 system scope 下写入会落默认租户 1（见 tenancyidx 的 create 早退分支）。
func (s *PromptTemplateService) InitSystemTemplates(ctx context.Context) error {
	for _, template := range model.SystemPromptTemplates {
		existing, _ := s.templateRepo.GetByTypeAndName(ctx, template.Type, template.Name)
		if existing == nil {
			templateCopy := template
			templateCopy.Status = 1
			if err := s.templateRepo.Create(ctx, &templateCopy); err != nil {
				logger.Warnf("[PromptTemplate] 初始化系统模板失败 type=%s name=%s: %v", template.Type, template.Name, err)
			}
		}
	}
	return nil
}

// GetTemplateTypes 获取模板类型列表
func (s *PromptTemplateService) GetTemplateTypes() []map[string]string {
	return []map[string]string{
		{"value": string(model.AIGenerationTypeCopywriting), "label": "营销文案"},
		{"value": string(model.AIGenerationTypeTitle), "label": "标题生成"},
		{"value": string(model.AIGenerationTypeSummary), "label": "内容摘要"},
		{"value": string(model.AIGenerationTypeReply), "label": "智能回复"},
		{"value": string(model.AIGenerationTypeRewrite), "label": "内容改写"},
		{"value": string(model.AIGenerationTypeExpand), "label": "内容扩写"},
		{"value": string(model.AIGenerationTypePolish), "label": "内容润色"},
		{"value": string(model.AIGenerationTypeKeywords), "label": "关键词提取"},
		{"value": string(model.AIGenerationTypeAdCopy), "label": "广告文案"},
		{"value": string(model.AIGenerationTypeSocialPost), "label": "社交媒体"},
		{"value": string(model.AIGenerationTypeScript), "label": "销售话术"},
		{"value": string(model.AIGenerationTypeEmail), "label": "邮件撰写"},
	}
}

// resolveAPIKey 与 aiagent/llm 同口径：LLM 密钥只来自参数中心 llm.global_override，
// 不读环境变量（SaaS 兼容：LLM API 连接信息禁止走 env，由平台端在线配）。
// 原实现绕道 loadSystemLLMConfig，而那个「Config」也只读环境变量（ctx 从头到尾没用过，
// BaseURL/Model 两个字段没有任何读者），错误信息却写「数据库无 LLM 配置」——
// 三跳之后交出的还是同一个环境变量，故直接收口为一次查表。
func (s *AIContentService) resolveAPIKey() string {
	if o := llm.GetGlobalLLMOverride(); o != nil && o.Enabled && strings.TrimSpace(o.BaseURL) != "" {
		return o.APIKey
	}
	return ""
}

func (s *AIContentService) resolveBaseURL() string {
	if o := llm.GetGlobalLLMOverride(); o != nil && o.Enabled && strings.TrimSpace(o.BaseURL) != "" {
		return o.BaseURL
	}
	return "https://api.openai.com"
}
