package service

import (
	"encoding/json"
	"errors"
	"hivemtk-user/internal/content/model"
	sysmodel "hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
)

var _ = sysmodel.JSONMap{}

// TemplateMarketService 模板市场服务
type TemplateMarketService struct {
	templateRepo *opsrepo.MarketTemplateRepository
	downloadRepo *opsrepo.MarketTemplateDownloadRepository
}

// NewTemplateMarketService 创建模板市场服务实例
func NewTemplateMarketService() *TemplateMarketService {
	return &TemplateMarketService{
		templateRepo: opsrepo.NewMarketTemplateRepository(),
		downloadRepo: opsrepo.NewMarketTemplateDownloadRepository(),
	}
}

// GetTemplateList 获取模板列表
func (s *TemplateMarketService) GetTemplateList(category, templateType string, page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.GetList(category, templateType, page, pageSize)
}

// GetTemplateByID 获取模板详情
func (s *TemplateMarketService) GetTemplateByID(id uint) (*model.MarketTemplate, error) {
	return s.templateRepo.GetByID(id)
}

// DownloadTemplate 下载模板
func (s *TemplateMarketService) DownloadTemplate(userID uint, templateID uint) (*model.MarketTemplate, error) {
	template, err := s.templateRepo.GetByID(templateID)
	if err != nil {
		return nil, errors.New("模板不存在")
	}

	record := &model.MarketTemplateDownload{
		TemplateID:   templateID,
		TemplateType: template.Type,
	}
	if err := s.downloadRepo.Create(record); err != nil {
		return nil, err
	}

	if err := s.templateRepo.IncrementDownload(templateID); err != nil {
		logger.Warnf("[TemplateMarket] 累加下载量失败 templateID=%d: %v", templateID, err)
	}

	return template, nil
}

// GetOfficialTemplates 获取官方模板
func (s *TemplateMarketService) GetOfficialTemplates(page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.GetOfficialTemplates(page, pageSize)
}

// SearchTemplates 搜索模板
func (s *TemplateMarketService) SearchTemplates(keyword string, page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.SearchTemplates(keyword, page, pageSize)
}

// GetMyDownloads 获取我的下载
func (s *TemplateMarketService) GetMyDownloads(page, pageSize int) ([]*model.MarketTemplateDownload, int64, error) {
	return s.downloadRepo.GetAll(page, pageSize)
}

// UseTemplate 使用模板（导入到商户）
func (s *TemplateMarketService) UseTemplate(userID uint, template *model.MarketTemplate) error {

	var err error
	switch template.Type {
	case "flow":
		err = s.importFlowTemplate(template)
	case "report":
		err = s.importReportTemplate(template)
	case "script":
		err = s.importScriptTemplate(userID, template)
	case "email":
		err = s.importEmailTemplate(template)
	default:
		return errors.New("不支持的模板类型")
	}

	if err != nil {
		return err
	}

	record := &model.MarketTemplateDownload{
		TemplateID:   template.ID,
		TemplateType: template.Type,
	}
	return s.downloadRepo.Create(record)
}

func (s *TemplateMarketService) importFlowTemplate(template *model.MarketTemplate) error {
	var flowData struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		TriggerType   string `json:"trigger_type"`
		TriggerConfig string `json:"trigger_config"`
		FlowData      string `json:"flow_data"`
	}
	if err := json.Unmarshal([]byte(template.Content), &flowData); err != nil {
		return errors.New("模板内容格式错误: " + err.Error())
	}

	if flowData.Name == "" {
		flowData.Name = template.Name
	}
	if flowData.FlowData == "" {
		flowData.FlowData = template.Content
	}

	flow := &model.MarketingFlow{
		Name:          flowData.Name,
		Description:   flowData.Description,
		Status:        model.FlowStatusDraft,
		TriggerType:   model.TriggerType(flowData.TriggerType),
		TriggerConfig: flowData.TriggerConfig,
		FlowData:      flowData.FlowData,
		Version:       1,
	}

	return db.GetDB().Create(flow).Error
}

func (s *TemplateMarketService) importReportTemplate(template *model.MarketTemplate) error {
	var reportData struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		DataSource  string `json:"data_source"`
		Dimensions  string `json:"dimensions"`
		Metrics     string `json:"metrics"`
		Filters     string `json:"filters"`
		ChartType   string `json:"chart_type"`
		ChartConfig string `json:"chart_config"`
	}
	if err := json.Unmarshal([]byte(template.Content), &reportData); err != nil {
		return errors.New("模板内容格式错误: " + err.Error())
	}

	if reportData.Name == "" {
		reportData.Name = template.Name
	}

	report := &opsmodel.CustomReport{
		Name:        reportData.Name,
		Description: reportData.Description,
		DataSource:  reportData.DataSource,
		Dimensions:  reportData.Dimensions,
		Metrics:     reportData.Metrics,
		Filters:     reportData.Filters,
		ChartType:   reportData.ChartType,
		ChartConfig: reportData.ChartConfig,
	}

	return db.GetDB().Create(report).Error
}

func (s *TemplateMarketService) importScriptTemplate(userID uint, template *model.MarketTemplate) error {
	var scriptData struct {
		Title     string `json:"title"`
		Category  string `json:"category"`
		Content   string `json:"content"`
		Variables string `json:"variables"`
		Tags      string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(template.Content), &scriptData); err != nil {
		return errors.New("模板内容格式错误: " + err.Error())
	}

	if scriptData.Title == "" {
		scriptData.Title = template.Name
	}
	if scriptData.Content == "" {
		scriptData.Content = template.Content
	}

	script := &model.ScriptTemplate{
		Category:  scriptData.Category,
		Title:     scriptData.Title,
		Content:   scriptData.Content,
		Variables: scriptData.Variables,
		Tags:      scriptData.Tags,
		CreatedBy: userID,
	}

	return db.GetDB().Create(script).Error
}

func (s *TemplateMarketService) importEmailTemplate(template *model.MarketTemplate) error {
	var emailData struct {
		Subject string `json:"subject"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(template.Content), &emailData); err != nil {
		return errors.New("模板内容格式错误: " + err.Error())
	}

	if emailData.Subject == "" {
		emailData.Subject = template.Name
	}
	if emailData.Content == "" {
		emailData.Content = template.Content
	}

	draft := &sysmodel.EmailDraft{
		Subject: emailData.Subject,
		Content: emailData.Content,
	}

	return db.GetDB().Create(draft).Error
}

// ApplyTemplate 应用模板（通用接口）
// 根据模板类型将模板内容导入到商户对应的模块
func (s *TemplateMarketService) ApplyTemplate(templateID uint, config map[string]any) error {
	template, err := s.templateRepo.GetByID(templateID)
	if err != nil {
		return errors.New("模板不存在")
	}

	if len(config) > 0 {
		var templateData map[string]any
		if err := json.Unmarshal([]byte(template.Content), &templateData); err != nil {
			return errors.New("模板内容格式错误: " + err.Error())
		}
		for k, v := range config {
			templateData[k] = v
		}
		mergedContent, err := json.Marshal(templateData)
		if err != nil {
			return errors.New("合并配置失败: " + err.Error())
		}
		template.Content = string(mergedContent)
	}

	switch template.Type {
	case "flow":
		return s.importFlowTemplate(template)
	case "report":
		return s.importReportTemplate(template)
	case "script":
		userID := uint(0)
		if v, ok := config["user_id"].(float64); ok {
			userID = uint(v)
		}
		return s.importScriptTemplate(userID, template)
	case "email":
		return s.importEmailTemplate(template)
	default:
		return errors.New("不支持的模板类型: " + template.Type)
	}
}

// CreateTemplate 创建模板
func (s *TemplateMarketService) CreateTemplate(template *model.MarketTemplate) error {
	if template.Name == "" {
		return errors.New("模板名称不能为空")
	}
	if template.Type == "" {
		return errors.New("模板类型不能为空")
	}
	return s.templateRepo.Create(template)
}

// RateTemplate 为模板评分（基于新评分更新平均评分）
func (s *TemplateMarketService) RateTemplate(id uint, rating float64) error {
	if rating < 0 || rating > 5 {
		return errors.New("评分范围 0-5")
	}
	template, err := s.templateRepo.GetByID(id)
	if err != nil {
		return errors.New("模板不存在")
	}

	var newRating float64
	if template.Rating > 0 {
		newRating = (template.Rating + rating) / 2
	} else {
		newRating = rating
	}
	return s.templateRepo.UpdateRating(id, newRating)
}
