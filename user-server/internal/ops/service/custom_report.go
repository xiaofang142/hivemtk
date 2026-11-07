package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	sysrepo "hivemtk-user/internal/repository"

	bizerr "hivemtk-user/internal/domain/errors"

	"gorm.io/gorm"
)

// CustomReportService 自定义报表服务
//
// AV-04c 批次7 刀2：不再持有 *gorm.DB（五段数据源聚合 SQL 下沉
// opsrepo.CustomReportRepository，谓词拼接下沉 opsrepo.ApplyConds），
// 构造统一走仓储默认构造（各自解析同一全局句柄，与原同连接）。
type CustomReportService struct {
	reportRepo  *opsrepo.CustomReportRepository
	sessionRepo *sysrepo.CustomerSessionRepository
	clueRepo    sysrepo.ClueRepository
	userRfmRepo *sysrepo.UserRFMRepository
}

// NewCustomReportService 创建自定义报表服务
func NewCustomReportService() *CustomReportService {
	return &CustomReportService{
		reportRepo:  opsrepo.NewCustomReportRepository(),
		sessionRepo: sysrepo.NewCustomerSessionRepository(),
		clueRepo:    sysrepo.NewClueRepositoryDefault(),
		userRfmRepo: sysrepo.NewUserRFMRepository(),
	}
}

// NewCustomReportServiceWithRepos repo 注入构造（测试/装配方显式注入仓储；
// 替代原 NewCustomReportServiceWithDB 的 *gorm.DB 形参，句柄不出仓储层）。
func NewCustomReportServiceWithRepos(reportRepo *opsrepo.CustomReportRepository,
	sessionRepo *sysrepo.CustomerSessionRepository, clueRepo sysrepo.ClueRepository,
	userRfmRepo *sysrepo.UserRFMRepository) *CustomReportService {
	return &CustomReportService{
		reportRepo:  reportRepo,
		sessionRepo: sessionRepo,
		clueRepo:    clueRepo,
		userRfmRepo: userRfmRepo,
	}
}

// 报表读写口的领域错误。控制器据 errors.Is 映射状态码：
// 不存在→404、无权限→403，避免把 DB 故障也压成「报表不存在」。
var (
	ErrReportNotFound  = errors.New("报表不存在")
	ErrReportForbidden = errors.New("无权操作该报表")
)

// CreateReportRequest 创建报表请求
type CreateReportRequest struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	DataSource  string                  `json:"data_source"`
	Dimensions  []model.ReportDimension `json:"dimensions"`
	Metrics     []model.ReportMetric    `json:"metrics"`
	Filters     []model.ReportFilter    `json:"filters"`
	ChartType   string                  `json:"chart_type"`
	ChartConfig map[string]any          `json:"chart_config"`
	IsPublic    bool                    `json:"is_public"`
}

// UpdateReportRequest 更新报表请求
type UpdateReportRequest struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	DataSource  string                  `json:"data_source"`
	Dimensions  []model.ReportDimension `json:"dimensions"`
	Metrics     []model.ReportMetric    `json:"metrics"`
	Filters     []model.ReportFilter    `json:"filters"`
	ChartType   string                  `json:"chart_type"`
	ChartConfig map[string]any          `json:"chart_config"`
	IsPublic    bool                    `json:"is_public"`
}

// CreateReport 创建报表
func (s *CustomReportService) CreateReport(ctx context.Context, createdBy uint, req *CreateReportRequest) (*model.CustomReport, error) {
	if !isValidDataSource(req.DataSource) {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "不支持的数据源类型")
	}

	if !isValidChartType(req.ChartType) {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "不支持的图表类型")
	}

	dimensionsJSON, _ := json.Marshal(req.Dimensions)
	metricsJSON, _ := json.Marshal(req.Metrics)
	filtersJSON, _ := json.Marshal(req.Filters)
	chartConfigJSON, _ := json.Marshal(req.ChartConfig)

	report := &model.CustomReport{
		Name:        req.Name,
		Description: req.Description,
		DataSource:  req.DataSource,
		Dimensions:  string(dimensionsJSON),
		Metrics:     string(metricsJSON),
		Filters:     string(filtersJSON),
		ChartType:   req.ChartType,
		ChartConfig: string(chartConfigJSON),
		IsPublic:    req.IsPublic,
		CreatedBy:   createdBy,
	}

	err := s.reportRepo.Create(ctx, report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// GetReport 获取报表详情（带权限校验：公开或本人或管理员可读）
func (s *CustomReportService) GetReport(ctx context.Context, id uint, userID uint, isAdmin bool) (*model.CustomReport, error) {
	report, err := s.reportRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w（id=%d）", ErrReportNotFound, id)
	}

	if !report.IsPublic && !isAdmin && report.CreatedBy != userID {
		return nil, fmt.Errorf("%w：该报表属于他人（id=%d）", ErrReportForbidden, id)
	}

	return report, nil
}

// GetReportList 获取报表列表（管理员看全部，普通用户看自己创建的 + 公开的）
func (s *CustomReportService) GetReportList(ctx context.Context, page, pageSize int, userID uint, isAdmin bool) ([]*model.CustomReport, int64, error) {
	if isAdmin {
		return s.reportRepo.GetAll(ctx, page, pageSize)
	}
	return s.reportRepo.GetByCreatorOrPublic(ctx, userID, page, pageSize)
}

// UpdateReport 更新报表（只有创建者或管理员可修改）
func (s *CustomReportService) UpdateReport(ctx context.Context, id uint, userID uint, isAdmin bool, req *UpdateReportRequest) (*model.CustomReport, error) {
	report, err := s.reportRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w（id=%d）", ErrReportNotFound, id)
	}

	if !isAdmin && report.CreatedBy != userID {
		return nil, fmt.Errorf("%w：该报表属于他人（id=%d）", ErrReportForbidden, id)
	}

	if !isValidDataSource(req.DataSource) {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "不支持的数据源类型")
	}

	if !isValidChartType(req.ChartType) {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "不支持的图表类型")
	}

	dimensionsJSON, _ := json.Marshal(req.Dimensions)
	metricsJSON, _ := json.Marshal(req.Metrics)
	filtersJSON, _ := json.Marshal(req.Filters)
	chartConfigJSON, _ := json.Marshal(req.ChartConfig)

	report.Name = req.Name
	report.Description = req.Description
	report.DataSource = req.DataSource
	report.Dimensions = string(dimensionsJSON)
	report.Metrics = string(metricsJSON)
	report.Filters = string(filtersJSON)
	report.ChartType = req.ChartType
	report.ChartConfig = string(chartConfigJSON)
	report.IsPublic = req.IsPublic

	err = s.reportRepo.Update(ctx, report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// DeleteReport 删除报表（只有创建者或管理员可删除）
func (s *CustomReportService) DeleteReport(ctx context.Context, id uint, userID uint, isAdmin bool) error {
	report, err := s.reportRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("%w（id=%d）", ErrReportNotFound, id)
	}

	if !isAdmin && report.CreatedBy != userID {
		return fmt.Errorf("%w：该报表属于他人（id=%d）", ErrReportForbidden, id)
	}

	return s.reportRepo.Delete(ctx, id)
}

// GetPublicTemplates 获取公开模板
func (s *CustomReportService) GetPublicTemplates(ctx context.Context) ([]*model.CustomReport, error) {
	return s.reportRepo.GetPublicTemplates(ctx)
}

// UseTemplate 使用模板
func (s *CustomReportService) UseTemplate(ctx context.Context, templateID uint, createdBy uint) (*model.CustomReport, error) {
	report, err := s.reportRepo.UseTemplate(ctx, templateID, createdBy)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w（模板 id=%d）", ErrReportNotFound, templateID)
		}
		return nil, err
	}
	return report, nil
}

// QueryReportData 查询报表数据
func (s *CustomReportService) QueryReportData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	switch report.DataSource {
	case "sessions":
		return s.querySessionData(ctx, report, params)
	case "messages":
		return s.queryMessageData(ctx, report, params)
	case "clues":
		return s.queryClueData(ctx, report, params)
	case "rfm":
		return s.queryRFMData(ctx, report, params)
	case "users":
		return s.queryUserData(ctx, report, params)
	case "agents":
		return s.queryAgentData(ctx, report, params)
	default:
		return nil, errors.New("不支持的数据源")
	}
}

func isValidDataSource(dataSource string) bool {
	validSources := map[string]bool{
		"sessions": true,
		"messages": true,
		"orders":   true,
		"clues":    true,
		"users":    true,
		"rfm":      true,
		"agents":   true,
	}
	return validSources[dataSource]
}

func isValidChartType(chartType string) bool {
	validTypes := map[string]bool{
		"table": true,
		"line":  true,
		"bar":   true,
		"pie":   true,
		"area":  true,
		"card":  true,
	}
	return validTypes[chartType]
}

func (s *CustomReportService) querySessionData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {

	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	conds, args := BuildReportFilterSQL("sessions", report.Filters)

	dimField := "status"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	aggs, err := s.reportRepo.AggregateSessionsByDim(ctx, dimField, conds, args)
	if err != nil {
		return nil, err
	}

	data := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		row := map[string]any{
			dimField: a.DimValue,
		}
		for _, metric := range metrics {
			if metric.Field == "session_count" {
				row["session_count"] = a.SessionCount
			} else if metric.Field == "message_count" {
				row["message_count"] = a.MessageSum
			}
		}
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}

	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      int64(len(data)),
	}, nil
}

func (s *CustomReportService) queryMessageData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	dimField := "msg_type"
	metricField := "message_count"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	if len(metrics) > 0 {
		metricField = metrics[0].Field
	}

	conds, args := BuildReportFilterSQL("messages", report.Filters)

	aggs, err := s.reportRepo.AggregateMessagesByDim(ctx, dimField, conds, args)
	if err != nil {
		return nil, err
	}

	data := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		row := make(map[string]any)
		row[dimField] = a.DimValue
		row[metricField] = a.Count
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}

	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      int64(len(data)),
	}, nil
}

func (s *CustomReportService) queryClueData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	conds, args := BuildReportFilterSQL("clues", report.Filters)

	dimField := "type"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	aggs, err := s.reportRepo.AggregateCluesByDim(ctx, dimField, conds, args)
	if err != nil {
		return nil, err
	}

	metricField := "clue_count"
	if len(metrics) > 0 {
		metricField = metrics[0].Field
	}

	data := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		row := map[string]any{
			dimField:    a.DimValue,
			metricField: a.Count,
		}
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}

	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      int64(len(data)),
	}, nil
}

func (s *CustomReportService) queryRFMData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	var layer string
	if v, ok := params["layer"]; ok && v != nil {
		if s, ok := v.(string); ok {
			layer = s
		}
	}
	var rfms []*sysmodel.UserRFM
	var total int64
	var err error

	if layer != "" {
		rfms, total, err = s.userRfmRepo.GetByLayer(ctx, layer, 1, 1000)
		if err != nil {
			return nil, err
		}
	} else {
		rfms, total, err = s.userRfmRepo.GetAll(ctx, 1, 1000)
		if err != nil {
			return nil, err
		}
	}

	data := make([]map[string]any, 0)
	layerCount := make(map[string]int)

	for _, rfm := range rfms {
		layerCount[rfm.Layer]++
	}

	for layerName, count := range layerCount {
		row := make(map[string]any)
		for _, dim := range dimensions {
			if dim.Field == "layer" {
				row["layer"] = layerName
			}
		}
		for _, metric := range metrics {
			if metric.Field == "user_count" {
				row["user_count"] = count
			}
		}
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}

	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      total,
	}, nil
}

func (s *CustomReportService) queryUserData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	dimField := "date"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}

	conds, args := BuildReportFilterSQL("users", report.Filters)
	aggs, err := s.reportRepo.AggregateUsersByDim(ctx, dimField, conds, args)
	if err != nil {
		return nil, err
	}

	data := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		row := make(map[string]any)
		row[dimField] = a.DimValue
		row["count"] = a.Count
		if len(metrics) > 0 {
			row[metrics[0].Field] = a.Count
		}
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}
	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      int64(len(data)),
	}, nil
}

func (s *CustomReportService) queryAgentData(ctx context.Context, report *model.CustomReport, params map[string]any) (*model.ReportData, error) {
	var dimensions []model.ReportDimension
	if err := json.Unmarshal([]byte(report.Dimensions), &dimensions); err != nil {
		return nil, fmt.Errorf("解析报表维度配置失败: %w", err)
	}

	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(report.Metrics), &metrics); err != nil {
		return nil, fmt.Errorf("解析报表指标配置失败: %w", err)
	}

	aggs, err := s.reportRepo.AggregateAgentSessions(ctx)
	if err != nil {
		return nil, err
	}

	data := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		row := make(map[string]any)
		row["agent_name"] = a.AgentName
		row["session_count"] = a.SessionCount
		row["avg_response_time"] = a.AvgResponseTime
		data = append(data, row)
	}

	dimNames := make([]string, len(dimensions))
	for i, dim := range dimensions {
		dimNames[i] = dim.Label
	}
	metricNames := make([]string, len(metrics))
	for i, metric := range metrics {
		metricNames[i] = metric.Label
	}

	return &model.ReportData{
		Dimensions: dimNames,
		Metrics:    metricNames,
		Data:       data,
		Total:      int64(len(data)),
	}, nil
}
