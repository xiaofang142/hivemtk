package service

import (
	"context"
	"encoding/json"
	"errors"
	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	_db "hivemtk-user/internal/pkg/db"
	sysrepo "hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// CustomReportService 自定义报表服务
type CustomReportService struct {
	db          *gorm.DB
	reportRepo  *opsrepo.CustomReportRepository
	sessionRepo *sysrepo.CustomerSessionRepository
	clueRepo    sysrepo.ClueRepository
	userRfmRepo *sysrepo.UserRFMRepository
}

// NewCustomReportService 创建自定义报表服务
func NewCustomReportService() *CustomReportService {
	db := _db.GetDB()
	return &CustomReportService{
		db:          db,
		reportRepo:  opsrepo.NewCustomReportRepositoryWithDB(db),
		sessionRepo: sysrepo.NewCustomerSessionRepositoryWithDB(db),
		clueRepo:    sysrepo.NewClueRepositoryWithDB(db),
		userRfmRepo: sysrepo.NewUserRFMRepositoryWithDB(db),
	}
}

// NewCustomReportServiceWithDB 创建指定数据库连接的自定义报表服务实例（用于测试）
func NewCustomReportServiceWithDB(db *gorm.DB) *CustomReportService {
	return &CustomReportService{
		db:          db,
		reportRepo:  opsrepo.NewCustomReportRepositoryWithDB(db),
		sessionRepo: sysrepo.NewCustomerSessionRepositoryWithDB(db),
		clueRepo:    sysrepo.NewClueRepositoryWithDB(db),
		userRfmRepo: sysrepo.NewUserRFMRepositoryWithDB(db),
	}
}

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
func (s *CustomReportService) CreateReport(createdBy uint, req *CreateReportRequest) (*model.CustomReport, error) {
	if !isValidDataSource(req.DataSource) {
		return nil, errors.New("不支持的数据源类型")
	}

	if !isValidChartType(req.ChartType) {
		return nil, errors.New("不支持的图表类型")
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

	err := s.reportRepo.Create(report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// GetReport 获取报表详情（带权限校验：公开或本人或管理员可读）
func (s *CustomReportService) GetReport(id uint, userID uint, isAdmin bool) (*model.CustomReport, error) {
	report, err := s.reportRepo.GetByID(id)
	if err != nil {
		return nil, errors.New("报表不存在")
	}

	if !report.IsPublic && !isAdmin && report.CreatedBy != userID {
		return nil, errors.New("无权限查看")
	}

	return report, nil
}

// GetReportList 获取报表列表（管理员看全部，普通用户看自己创建的 + 公开的）
func (s *CustomReportService) GetReportList(page, pageSize int, userID uint, isAdmin bool) ([]*model.CustomReport, int64, error) {
	if isAdmin {
		return s.reportRepo.GetAll(page, pageSize)
	}
	return s.reportRepo.GetByCreatorOrPublic(userID, page, pageSize)
}

// UpdateReport 更新报表（只有创建者或管理员可修改）
func (s *CustomReportService) UpdateReport(id uint, userID uint, isAdmin bool, req *UpdateReportRequest) (*model.CustomReport, error) {
	report, err := s.reportRepo.GetByID(id)
	if err != nil {
		return nil, errors.New("报表不存在")
	}

	if !isAdmin && report.CreatedBy != userID {
		return nil, errors.New("无权限修改此报表")
	}

	if !isValidDataSource(req.DataSource) {
		return nil, errors.New("不支持的数据源类型")
	}

	if !isValidChartType(req.ChartType) {
		return nil, errors.New("不支持的图表类型")
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

	err = s.reportRepo.Update(report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// DeleteReport 删除报表（只有创建者或管理员可删除）
func (s *CustomReportService) DeleteReport(id uint, userID uint, isAdmin bool) error {
	report, err := s.reportRepo.GetByID(id)
	if err != nil {
		return errors.New("报表不存在")
	}

	if !isAdmin && report.CreatedBy != userID {
		return errors.New("无权限删除此报表")
	}

	return s.reportRepo.Delete(id)
}

// GetPublicTemplates 获取公开模板
func (s *CustomReportService) GetPublicTemplates() ([]*model.CustomReport, error) {
	return s.reportRepo.GetPublicTemplates()
}

// UseTemplate 使用模板
func (s *CustomReportService) UseTemplate(templateID uint, createdBy uint) (*model.CustomReport, error) {
	return s.reportRepo.UseTemplate(templateID, createdBy)
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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

	conds, args := BuildReportFilterSQL("sessions", report.Filters)

	type sessionAgg struct {
		DimValue     string
		SessionCount int64
		MessageSum   int64
	}

	dimField := "status"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	var groupExpr, dimValueExpr string // 三个分支（含 default）各自赋值
	switch dimField {
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	case "agent_name":
		groupExpr = "COALESCE(NULLIF(agent_name, ''), '未分配')"
		dimValueExpr = "COALESCE(NULLIF(agent_name, ''), '未分配')"
	default:
		groupExpr = "status"
		dimValueExpr = "status::text"
	}

	q := s.db.WithContext(ctx).Table("customer_sessions").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS session_count, COALESCE(SUM(message_count), 0) AS message_sum").
		Group(groupExpr).
		Order(groupExpr)
	q = applyConds(q, conds, args)
	var aggs []sessionAgg
	if err := q.Scan(&aggs).Error; err != nil {
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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

	dimField := "msg_type"
	metricField := "message_count"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	if len(metrics) > 0 {
		metricField = metrics[0].Field
	}

	type msgAgg struct {
		DimValue string
		Count    int64
	}

	conds, args := BuildReportFilterSQL("messages", report.Filters)

	var dimValueExpr, groupExpr string // 三个分支（含 default）各自赋值
	switch dimField {
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	case "platform":
		groupExpr = "COALESCE(platform, 'unknown')"
		dimValueExpr = "COALESCE(platform, 'unknown')"
	default:
		groupExpr = "COALESCE(content_type, 'unknown')"
		dimValueExpr = "COALESCE(content_type, 'unknown')"
	}

	q := s.db.WithContext(ctx).
		Table("unified_messages").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = applyConds(q, conds, args)
	var aggs []msgAgg
	if err := q.Scan(&aggs).Error; err != nil {
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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

	conds, args := BuildReportFilterSQL("clues", report.Filters)

	dimField := "type"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}
	var dimValueExpr, groupExpr string // 各分支（含 default）各自赋值
	switch dimField {
	case "is_verify":
		dimValueExpr = "is_verify::text"
		groupExpr = "is_verify"
	case "level":
		dimValueExpr = "COALESCE(NULLIF(level, ''), 'warm')"
		groupExpr = "COALESCE(NULLIF(level, ''), 'warm')"
	case "is_group":
		dimValueExpr = "is_group::text"
		groupExpr = "is_group"
	default:
		dimValueExpr = "type::text"
		groupExpr = "type"
	}

	type clueAgg struct {
		DimValue string
		Count    int64
	}
	q := s.db.WithContext(ctx).Table("clues").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = applyConds(q, conds, args)
	var aggs []clueAgg
	if err := q.Scan(&aggs).Error; err != nil {
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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

	dimField := "date"
	if len(dimensions) > 0 {
		dimField = dimensions[0].Field
	}

	var groupExpr, dimValueExpr string // 三个分支（含 default）各自赋值
	switch dimField {
	case "user_type", "churn_risk":
		groupExpr = "churn_risk"
		dimValueExpr = "churn_risk::text"
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	default:
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	}

	type userAgg struct {
		DimValue string
		Count    int64
	}

	conds, args := BuildReportFilterSQL("users", report.Filters)
	q := s.db.WithContext(ctx).
		Table("customers").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = applyConds(q, conds, args)
	var aggs []userAgg
	if err := q.Scan(&aggs).Error; err != nil {
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
	json.Unmarshal([]byte(report.Dimensions), &dimensions)

	var metrics []model.ReportMetric
	json.Unmarshal([]byte(report.Metrics), &metrics)

	type agentAgg struct {
		AgentName       string
		SessionCount    int64
		AvgResponseTime float64
	}
	var aggs []agentAgg
	if err := s.db.WithContext(ctx).
		Table("customer_sessions").
		Select("agent_name AS agent_name, COUNT(*) AS session_count, COALESCE(AVG(avg_response_time), 0) AS avg_response_time").
		Where("agent_name IS NOT NULL AND agent_name <> ''").
		Group("agent_name").
		Order("session_count DESC").
		Scan(&aggs).Error; err != nil {
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
