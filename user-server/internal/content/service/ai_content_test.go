package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/content/model"
	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAIContentServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.AIGenerationRecord{},
		&model.PromptTemplate{},
		&sysmodel.DomainPool{},
		&sysmodel.ShortLink{},
	)
	db.SetTestDB(database)
	return database
}

// aiSvcCtx 本文件用例全部跑在 off 模式（隔离 callback 直通），ctx 只用于把参数补齐、
// 不承担租户语义；on 模式的真实读数（遮挡、fail-closed、归属门）由 content/repository
// 域的 ai_content_tenant_test.go 钉住，这一层只验服务自身的转发与校验。
var aiSvcCtx = context.Background()

// TestNewAIContentService 测试创建 AI 内容服务
func TestNewAIContentService(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestAIContentService_GenerateContent_Copywriting 测试生成营销文案
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_Copywriting(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeCopywriting,
		Input: "测试产品，核心卖点：高效、节能、环保",
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_Title 测试生成标题
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_Title(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeTitle,
		Input: "测试文章内容",
		Variables: map[string]any{
			"count": float64(5),
		},
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_Reply 测试生成客服回复
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_Reply(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeReply,
		Input: "客户咨询：产品什么时候发货？",
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_Rewrite 测试内容改写
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_Rewrite(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeRewrite,
		Input: "这是一段需要改写的文本",
		Variables: map[string]any{
			"style": "简洁明了",
		},
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_SocialPost 测试生成社交媒体帖子
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_SocialPost(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeSocialPost,
		Input: "产品推广内容",
		Variables: map[string]any{
			"platform": "微信朋友圈",
		},
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_WithTemplate 测试使用模板生成内容
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_WithTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()

	template := &model.PromptTemplate{
		Name:        "测试模板",
		Type:        model.AIGenerationTypeCopywriting,
		Template:    "请为{{product}}生成营销文案，目标受众：{{audience}}",
		Variables:   `[{"name":"product","type":"string"},{"name":"audience","type":"string"}]`,
		Description: "测试用模板",
		IsSystem:    false,
		Status:      1,
	}
	database.Create(template)

	req := &GenerateContentRequest{
		Type:       model.AIGenerationTypeCopywriting,
		Input:      "测试输入",
		TemplateID: template.ID,
		Variables: map[string]any{
			"product":  "测试产品",
			"audience": "年轻人群",
		},
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_WithCustomModel 测试使用自定义模型
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_WithCustomModel(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeCopywriting,
		Input: "测试产品",
		Model: "gpt-4",
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GenerateContent_DefaultModel 测试使用默认模型
// 未配置 LLM_API_KEY 时，应在调用外部 LLM API 前返回 "AI service not configured" 错误
func TestAIContentService_GenerateContent_DefaultModel(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	ctx := context.Background()
	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeCopywriting,
		Input: "测试产品",
	}

	userID := uint(1)

	resp, err := service.GenerateContent(ctx, userID, req)

	if err == nil {
		t.Fatal("Expected error when LLM API key not configured, got nil")
	}
	if !strings.Contains(err.Error(), "AI service not configured") {
		t.Errorf("Expected 'AI service not configured' error, got: %v", err)
	}
	if resp != nil {
		t.Errorf("Expected nil response when not configured, got: %v", resp)
	}
}

// TestAIContentService_GetGenerationHistory 测试获取生成历史
func TestAIContentService_GetGenerationHistory(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	for i := 1; i <= 5; i++ {
		record := &model.AIGenerationRecord{
			UserID: 1,
			Type:   model.AIGenerationTypeCopywriting,
			Input:  "测试输入",
			Output: "测试输出",
			Model:  "gpt-3.5-turbo",
		}
		database.Create(record)
	}

	userID := uint(1)

	records, total, err := service.GetGenerationHistory(aiSvcCtx, userID, 1, 10, nil)
	if err != nil {
		t.Fatalf("GetGenerationHistory failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(records))
	}
}

// TestAIContentService_GetGenerationHistory_WithFilters 测试带过滤条件的生成历史
func TestAIContentService_GetGenerationHistory_WithFilters(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record1 := &model.AIGenerationRecord{
		UserID:  1,
		Type:    model.AIGenerationTypeCopywriting,
		Input:   "测试输入 1",
		Output:  "测试输出 1",
		IsSaved: true,
	}
	database.Create(record1)

	record2 := &model.AIGenerationRecord{
		UserID:  1,
		Type:    model.AIGenerationTypeTitle,
		Input:   "测试输入 2",
		Output:  "测试输出 2",
		IsSaved: false,
	}
	database.Create(record2)

	userID := uint(1)

	filters := map[string]any{
		"type": string(model.AIGenerationTypeCopywriting),
	}

	records, total, err := service.GetGenerationHistory(aiSvcCtx, userID, 1, 10, filters)
	if err != nil {
		t.Fatalf("GetGenerationHistory failed: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if len(records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(records))
	}
}

// TestAIContentService_GetRecordByID_Success 测试获取记录成功
func TestAIContentService_GetRecordByID_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "测试输入",
		Output: "测试输出",
	}
	database.Create(record)

	fetchedRecord, err := service.GetRecordByID(aiSvcCtx, record.ID, 1)
	if err != nil {
		t.Fatalf("GetRecordByID failed: %v", err)
	}

	if fetchedRecord.ID != record.ID {
		t.Errorf("Expected ID %d, got %d", record.ID, fetchedRecord.ID)
	}
}

// 记录属于 user=1，以 user=2 的身份取同一条必须读不到：
// 改前这一口只看 id，同租户内任何登录用户换个 id 就能拿到别人的输入原文与生成结果。
func TestAIContentService_GetRecordByID_WrongOwner(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "测试输入",
		Output: "测试输出",
	}
	database.Create(record)

	fetchedRecord, err := service.GetRecordByID(aiSvcCtx, record.ID, 2)
	if err == nil {
		t.Errorf("以别人的 user_id 读到了 %+v，期望失败", fetchedRecord)
	}
}

// TestAIContentService_SaveRecord 测试保存记录
func TestAIContentService_SaveRecord(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID:  1,
		Type:    model.AIGenerationTypeCopywriting,
		Input:   "测试输入",
		Output:  "测试输出",
		IsSaved: false,
	}
	database.Create(record)

	err := service.SaveRecord(aiSvcCtx, record.ID)
	if err != nil {
		t.Fatalf("SaveRecord failed: %v", err)
	}

	var updatedRecord model.AIGenerationRecord
	database.First(&updatedRecord, record.ID)

	if !updatedRecord.IsSaved {
		t.Error("Expected record to be saved")
	}
}

// TestAIContentService_FavoriteRecord 测试收藏记录
func TestAIContentService_FavoriteRecord(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID:     1,
		Type:       model.AIGenerationTypeCopywriting,
		Input:      "测试输入",
		Output:     "测试输出",
		IsFavorite: false,
	}
	database.Create(record)

	err := service.FavoriteRecord(aiSvcCtx, record.ID, 1, true)
	if err != nil {
		t.Fatalf("FavoriteRecord failed: %v", err)
	}

	var updatedRecord model.AIGenerationRecord
	database.First(&updatedRecord, record.ID)

	if !updatedRecord.IsFavorite {
		t.Error("Expected record to be favorited")
	}
}

// TestAIContentService_RateRecord_Success 测试评分记录成功
func TestAIContentService_RateRecord_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "测试输入",
		Output: "测试输出",
		Rating: 0,
	}
	database.Create(record)

	err := service.RateRecord(aiSvcCtx, record.ID, 1, 5)
	if err != nil {
		t.Fatalf("RateRecord failed: %v", err)
	}

	var updatedRecord model.AIGenerationRecord
	database.First(&updatedRecord, record.ID)

	if updatedRecord.Rating != 5 {
		t.Errorf("Expected rating 5, got %d", updatedRecord.Rating)
	}
}

// TestAIContentService_RateRecord_InvalidRating 测试无效评分
func TestAIContentService_RateRecord_InvalidRating(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "测试输入",
		Output: "测试输出",
	}
	database.Create(record)

	err := service.RateRecord(aiSvcCtx, record.ID, 1, 10)
	if err == nil {
		t.Error("Expected error for invalid rating")
	}
}

// TestAIContentService_DeleteRecord_Success 测试删除记录成功
func TestAIContentService_DeleteRecord_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	record := &model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "测试输入",
		Output: "测试输出",
	}
	database.Create(record)

	err := service.DeleteRecord(aiSvcCtx, record.ID)
	if err != nil {
		t.Fatalf("DeleteRecord failed: %v", err)
	}

	var deletedRecord model.AIGenerationRecord
	err = database.First(&deletedRecord, record.ID).Error

	if err == nil {
		t.Error("Expected record to be deleted")
	}
}

// DeleteRecord 是管理口（只有 admin 路由挂它，角色由 AdminAuthMiddleware 证明），签名里没有 userID，
// 所以这一层能钉的是「命中 0 行不许静默成功」：改前 gorm 对 0 行 DELETE 回 nil error，
// 处理器随后对不存在的 id 回「删除成功」，界面显示已删、库里一行没动。
func TestAIContentService_DeleteRecord_MissingRow(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	if err := service.DeleteRecord(aiSvcCtx, 999999); err == nil {
		t.Error("删除不存在的 id 返回了 nil error")
	}
}

// TestPromptTemplateService_GetTemplates 测试获取模板列表
func TestPromptTemplateService_GetTemplates(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template1 := &model.PromptTemplate{
		Name:     "测试模板 1",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容 1",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template1)

	template2 := &model.PromptTemplate{
		Name:     "系统模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "系统模板内容",
		IsSystem: true,
		Status:   1,
	}
	database.Create(template2)

	templates, err := service.GetTemplates(aiSvcCtx, "")
	if err != nil {
		t.Fatalf("GetTemplates failed: %v", err)
	}

	if len(templates) != 2 {
		t.Errorf("Expected 2 templates, got %d", len(templates))
	}
}

// TestPromptTemplateService_GetTemplates_ByType 测试按类型获取模板
func TestPromptTemplateService_GetTemplates_ByType(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template1 := &model.PromptTemplate{
		Name:     "文案模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "文案模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template1)

	template2 := &model.PromptTemplate{
		Name:     "标题模板",
		Type:     model.AIGenerationTypeTitle,
		Template: "标题模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template2)

	templates, err := service.GetTemplates(aiSvcCtx, string(model.AIGenerationTypeCopywriting))
	if err != nil {
		t.Fatalf("GetTemplates failed: %v", err)
	}

	if len(templates) != 1 {
		t.Errorf("Expected 1 template, got %d", len(templates))
	}

	if templates[0].Type != model.AIGenerationTypeCopywriting {
		t.Errorf("Expected copywriting type, got %s", templates[0].Type)
	}
}

// TestPromptTemplateService_GetTemplateByID_Success 测试获取模板成功
func TestPromptTemplateService_GetTemplateByID_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "测试模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	fetchedTemplate, err := service.GetTemplateByID(aiSvcCtx, template.ID)
	if err != nil {
		t.Fatalf("GetTemplateByID failed: %v", err)
	}

	if fetchedTemplate.ID != template.ID {
		t.Errorf("Expected ID %d, got %d", template.ID, fetchedTemplate.ID)
	}

	if fetchedTemplate.Name != template.Name {
		t.Errorf("Expected name %s, got %s", template.Name, fetchedTemplate.Name)
	}
}

// TestPromptTemplateService_GetTemplateByID_SystemTemplate 测试获取系统模板
func TestPromptTemplateService_GetTemplateByID_SystemTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "系统模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "系统模板内容",
		IsSystem: true,
		Status:   1,
	}
	database.Create(template)

	fetchedTemplate, err := service.GetTemplateByID(aiSvcCtx, template.ID)
	if err != nil {
		t.Fatalf("GetTemplateByID failed: %v", err)
	}

	if fetchedTemplate.ID != template.ID {
		t.Errorf("Expected ID %d, got %d", template.ID, fetchedTemplate.ID)
	}
}

func TestPromptTemplateService_GetTemplateByID_WrongMerchant(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "测试模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	fetchedTemplate, err := service.GetTemplateByID(aiSvcCtx, template.ID)
	if err != nil {
		t.Fatalf("GetTemplateByID should succeed in single-tenant mode: %v", err)
	}
	if fetchedTemplate == nil {
		t.Error("Expected template to be returned in single-tenant mode")
	}
}

// TestPromptTemplateService_CreateTemplate 测试创建模板
func TestPromptTemplateService_CreateTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	req := &CreateTemplateRequest{
		Name:        "新模板",
		Type:        model.AIGenerationTypeCopywriting,
		Template:    "新模板内容",
		Variables:   `[{"name":"var1","type":"string"}]`,
		Description: "测试模板",
		Example:     "示例输出",
	}

	createdTemplate, err := service.CreateTemplate(aiSvcCtx, req)
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}

	if createdTemplate.ID == 0 {
		t.Error("Expected non-zero ID")
	}

	if createdTemplate.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, createdTemplate.Name)
	}

	if createdTemplate.IsSystem {
		t.Error("Expected user template to not be system template")
	}

	var savedTemplate model.PromptTemplate
	if firstErr := database.First(&savedTemplate, createdTemplate.ID).Error; firstErr != nil {
		t.Fatalf("模板创建后回读失败: %v", firstErr)
	}
	if savedTemplate.Template != req.Template {
		t.Errorf("落库正文 %q，期望 %q", savedTemplate.Template, req.Template)
	}
}

// TestPromptTemplateService_UpdateTemplate_Success 测试更新模板成功
func TestPromptTemplateService_UpdateTemplate_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "原模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "原模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	req := &CreateTemplateRequest{
		Name:        "更新后的模板",
		Type:        model.AIGenerationTypeTitle,
		Template:    "更新后的模板内容",
		Variables:   `[{"name":"var1","type":"string"}]`,
		Description: "更新后的描述",
		Example:     "更新后的示例",
	}

	updatedTemplate, err := service.UpdateTemplate(aiSvcCtx, template.ID, req)
	if err != nil {
		t.Fatalf("UpdateTemplate failed: %v", err)
	}

	if updatedTemplate.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, updatedTemplate.Name)
	}

	if updatedTemplate.Type != req.Type {
		t.Errorf("Expected type %s, got %s", req.Type, updatedTemplate.Type)
	}

	var savedTemplate model.PromptTemplate
	database.First(&savedTemplate, template.ID)

	if savedTemplate.Name != "更新后的模板" {
		t.Errorf("Expected updated name, got %s", savedTemplate.Name)
	}
}

// TestPromptTemplateService_UpdateTemplate_SystemTemplate 测试不能更新系统模板
func TestPromptTemplateService_UpdateTemplate_SystemTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "系统模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "系统模板内容",
		IsSystem: true,
		Status:   1,
	}
	database.Create(template)

	req := &CreateTemplateRequest{
		Name:     "更新后的模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "更新后的内容",
	}

	_, err := service.UpdateTemplate(aiSvcCtx, template.ID, req)
	if err == nil {
		t.Error("Expected error for updating system template")
	}
}

func TestPromptTemplateService_UpdateTemplate_WrongMerchant(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "测试模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	req := &CreateTemplateRequest{
		Name:     "更新后的模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "更新后的内容",
	}

	_, err := service.UpdateTemplate(aiSvcCtx, template.ID, req)
	if err != nil {
		t.Errorf("UpdateTemplate should succeed in single-tenant mode: %v", err)
	}
}

// TestPromptTemplateService_DeleteTemplate_Success 测试删除模板成功
func TestPromptTemplateService_DeleteTemplate_Success(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "测试模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	err := service.DeleteTemplate(aiSvcCtx, template.ID)
	if err != nil {
		t.Fatalf("DeleteTemplate failed: %v", err)
	}

	var deletedTemplate model.PromptTemplate
	err = database.First(&deletedTemplate, template.ID).Error

	if err == nil {
		t.Error("Expected template to be deleted")
	}
}

// TestPromptTemplateService_DeleteTemplate_SystemTemplate 测试不能删除系统模板
func TestPromptTemplateService_DeleteTemplate_SystemTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "系统模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "系统模板内容",
		IsSystem: true,
		Status:   1,
	}
	database.Create(template)

	err := service.DeleteTemplate(aiSvcCtx, template.ID)
	if err == nil {
		t.Error("Expected error for deleting system template")
	}
}

func TestPromptTemplateService_DeleteTemplate_WrongMerchant(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	template := &model.PromptTemplate{
		Name:     "测试模板",
		Type:     model.AIGenerationTypeCopywriting,
		Template: "测试模板内容",
		IsSystem: false,
		Status:   1,
	}
	database.Create(template)

	err := service.DeleteTemplate(aiSvcCtx, template.ID)
	if err != nil {
		t.Errorf("DeleteTemplate should succeed in single-tenant mode: %v", err)
	}
}

// TestPromptTemplateService_InitSystemTemplates 测试初始化系统模板
func TestPromptTemplateService_InitSystemTemplates(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	err := service.InitSystemTemplates(aiSvcCtx)

	if err != nil {
		t.Fatalf("InitSystemTemplates failed: %v", err)
	}

	var templates []model.PromptTemplate
	database.Where("is_system = ?", true).Find(&templates)

	t.Logf("Created %d system templates", len(templates))
	for _, tmpl := range templates {
		t.Logf("Template: ID=%d, Name=%s, Type=%s, Status=%d, IsSystem=%v",
			tmpl.ID, tmpl.Name, tmpl.Type, tmpl.Status, tmpl.IsSystem)
	}

	if len(templates) == 0 {
		t.Error("Expected system templates to be created")
	}

	hasSystemTemplate := false
	for _, template := range templates {
		if template.IsSystem && template.Status == 1 {
			hasSystemTemplate = true
			break
		}
	}

	if !hasSystemTemplate {
		t.Error("Expected at least one system template with status 1")
	}
}

// TestPromptTemplateService_GetTemplateTypes 测试获取模板类型列表
func TestPromptTemplateService_GetTemplateTypes(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewPromptTemplateService(newPromptTemplateDeps(database))

	types := service.GetTemplateTypes()

	if len(types) == 0 {
		t.Error("Expected non-empty template types")
	}

	foundCopywriting := false
	foundTitle := false

	for _, t := range types {
		if t["value"] == string(model.AIGenerationTypeCopywriting) {
			foundCopywriting = true
		}
		if t["value"] == string(model.AIGenerationTypeTitle) {
			foundTitle = true
		}
	}

	if !foundCopywriting {
		t.Error("Expected copywriting type in template types")
	}

	if !foundTitle {
		t.Error("Expected title type in template types")
	}
}

// TestAIContentService_buildDefaultPrompt_Copywriting 测试构建默认提示词 - 文案
func TestAIContentService_buildDefaultPrompt_Copywriting(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeCopywriting,
		Input: "测试产品",
	}

	prompt, err := service.buildPrompt(aiSvcCtx, req)

	if err != nil {
		t.Fatalf("buildPrompt failed: %v", err)
	}

	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}
}

// TestAIContentService_buildDefaultPrompt_Title 测试构建默认提示词 - 标题
func TestAIContentService_buildDefaultPrompt_Title(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	req := &GenerateContentRequest{
		Type:  model.AIGenerationTypeTitle,
		Input: "测试内容",
	}

	prompt, err := service.buildPrompt(aiSvcCtx, req)

	if err != nil {
		t.Fatalf("buildPrompt failed: %v", err)
	}

	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}
}

// TestAIContentService_fillTemplate 测试填充模板变量
func TestAIContentService_fillTemplate(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	service := NewAIContentService(newAIContentDeps(database))

	template := "你好，{{name}}，欢迎来到{{place}}"
	variables := map[string]any{
		"name":  "张三",
		"place": "北京",
	}

	result, err := service.fillTemplate(template, variables)

	if err != nil {
		t.Fatalf("fillTemplate failed: %v", err)
	}

	expected := "你好，张三，欢迎来到北京"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

// TestAIContentService_TypeVocabularyMatchesPrompts 类型表与提示词分支必须同步。
//
// 改前这里是一张手写的类型表 + `prompt != ""` 断言：`translation` 走的是 switch 的
// `default: return input`（把用户原文当提示词发给模型），因为原文非空，断言照样绿——
// 弱断言把「有类型、没实现」藏了整轮审查。现在两个方向都钉：
//
//	表里有 ⇒ 必须有专属分支（不能原样回显）；分支里有 ⇒ 必须出现在给前端的类型表里（否则选不到）。
func TestAIContentService_TypeVocabularyMatchesPrompts(t *testing.T) {
	database := setupAIContentServiceTestDB(t)
	content := NewAIContentService(newAIContentDeps(database))
	template := NewPromptTemplateService(newPromptTemplateDeps(database))

	listed := map[string]bool{}
	for _, item := range template.GetTemplateTypes() {
		value := item["value"]
		if value == "" {
			t.Errorf("类型表里有一项没有 value：%+v", item)
			continue
		}
		if listed[value] {
			t.Errorf("类型表里 %s 出现两次", value)
		}
		listed[value] = true

		const input = "测试内容"
		prompt, err := content.buildPrompt(aiSvcCtx, &GenerateContentRequest{Type: model.AIGenerationType(value), Input: input})
		if err != nil {
			t.Errorf("类型 %s 构建提示词失败: %v", value, err)
			continue
		}
		if prompt == input {
			t.Errorf("类型 %s 在类型表里却没有专属分支：原样回显用户输入，选了等于没选", value)
		}
	}

	for _, typ := range []model.AIGenerationType{
		model.AIGenerationTypeCopywriting, model.AIGenerationTypeTitle, model.AIGenerationTypeSummary,
		model.AIGenerationTypeReply, model.AIGenerationTypeRewrite, model.AIGenerationTypeExpand,
		model.AIGenerationTypePolish, model.AIGenerationTypeKeywords, model.AIGenerationTypeAdCopy,
		model.AIGenerationTypeSocialPost, model.AIGenerationTypeScript, model.AIGenerationTypeEmail,
	} {
		if !listed[string(typ)] {
			t.Errorf("类型 %s 有提示词分支却不在类型表里：前端选不到", typ)
		}
	}
}
