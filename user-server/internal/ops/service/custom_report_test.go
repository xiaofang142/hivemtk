package service

import (
	"context"
	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/db"
	sysrepo "hivemtk-user/internal/repository"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupCustomReportServiceTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.CustomReport{},
		&sysmodel.CustomerSession{},
		&sysmodel.Clue{},
		&sysmodel.UserRFM{},
		&sysmodel.UnifiedMessage{},
		&sysmodel.Customer{},
	)
}

func setupCustomReportService(t *testing.T, testDB *gorm.DB) *CustomReportService {
	db.SetTestDB(testDB)
	return &CustomReportService{
		db:          testDB,
		reportRepo:  opsrepo.NewCustomReportRepository(),
		sessionRepo: sysrepo.NewCustomerSessionRepository(),
		clueRepo:    sysrepo.NewClueRepository(),
		userRfmRepo: sysrepo.NewUserRFMRepository(),
	}
}

// TestNewCustomReportService 测试创建服务实例
func TestNewCustomReportService(t *testing.T) {
	service := NewCustomReportService()
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.reportRepo == nil {
		t.Error("Expected reportRepo to be initialized")
	}
	if service.sessionRepo == nil {
		t.Error("Expected sessionRepo to be initialized")
	}
	if service.clueRepo == nil {
		t.Error("Expected clueRepo to be initialized")
	}
	if service.userRfmRepo == nil {
		t.Error("Expected userRfmRepo to be initialized")
	}
}

// TestCustomReportService_CreateReport_Success 测试创建报表成功
func TestCustomReportService_CreateReport_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	req := &CreateReportRequest{
		Name:        "Test Report",
		Description: "Test Description",
		DataSource:  "sessions",
		Dimensions:  []model.ReportDimension{{Field: "date", Label: "日期"}},
		Metrics:     []model.ReportMetric{{Field: "session_count", Label: "会话数"}},
		Filters:     []model.ReportFilter{{Field: "status", Operator: "=", Value: "active"}},
		ChartType:   "line",
		ChartConfig: map[string]any{"showLegend": true},
		IsPublic:    false,
	}

	report, err := service.CreateReport(123, req)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	if report.Name != "Test Report" {
		t.Errorf("Expected name 'Test Report', got '%s'", report.Name)
	}

	if report.CreatedBy != 123 {
		t.Errorf("Expected created_by 123, got %d", report.CreatedBy)
	}
}

// TestCustomReportService_CreateReport_InvalidDataSource 测试无效数据源
func TestCustomReportService_CreateReport_InvalidDataSource(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	req := &CreateReportRequest{
		Name:       "Invalid Report",
		DataSource: "invalid_source",
		ChartType:  "line",
	}

	_, err := service.CreateReport(123, req)
	if err == nil {
		t.Error("Expected error for invalid data source")
	}
}

// TestCustomReportService_CreateReport_InvalidChartType 测试无效图表类型
func TestCustomReportService_CreateReport_InvalidChartType(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	req := &CreateReportRequest{
		Name:       "Invalid Report",
		DataSource: "sessions",
		ChartType:  "invalid_chart",
	}

	_, err := service.CreateReport(123, req)
	if err == nil {
		t.Error("Expected error for invalid chart type")
	}
}

// TestCustomReportService_GetReport_Success 测试获取报表成功
func TestCustomReportService_GetReport_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Get Test",
		DataSource: "sessions",
		ChartType:  "bar",
		IsPublic:   true,
	}
	created, _ := service.CreateReport(123, createReq)

	report, err := service.GetReport(created.ID, 123, true)
	if err != nil {
		t.Fatalf("GetReport failed: %v", err)
	}

	if report.Name != "Get Test" {
		t.Errorf("Expected name 'Get Test', got '%s'", report.Name)
	}
}

// TestCustomReportService_GetReport_NotFound 测试获取不存在的报表
func TestCustomReportService_GetReport_NotFound(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	_, err := service.GetReport(999999, 123, true)
	if err == nil {
		t.Error("Expected error for non-existent report")
	}
}

// TestCustomReportService_GetReport_NoPermission 测试无权限查看私有报表
func TestCustomReportService_GetReport_NoPermission(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Private Report",
		DataSource: "sessions",
		ChartType:  "table",
		IsPublic:   false,
	}
	created, _ := service.CreateReport(123, createReq)

	_, err := service.GetReport(created.ID, 999, false)
	if err == nil {
		t.Error("Expected error for no permission")
	}
}

// TestCustomReportService_GetReportList_Success 测试获取报表列表
func TestCustomReportService_GetReportList_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	for i := 0; i < 5; i++ {
		createReq := &CreateReportRequest{
			Name:       "Report " + string(rune('A'+i)),
			DataSource: "sessions",
			ChartType:  "line",
		}
		service.CreateReport(123, createReq)
	}

	reports, total, err := service.GetReportList(1, 10, 123, true)
	if err != nil {
		t.Fatalf("GetReportList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(reports) != 5 {
		t.Errorf("Expected 5 reports, got %d", len(reports))
	}
}

// TestCustomReportService_UpdateReport_Success 测试更新报表成功
func TestCustomReportService_UpdateReport_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Original",
		DataSource: "sessions",
		ChartType:  "bar",
	}
	created, _ := service.CreateReport(123, createReq)

	updateReq := &UpdateReportRequest{
		Name:        "Updated",
		Description: "New Description",
		DataSource:  "messages",
		ChartType:   "pie",
		IsPublic:    true,
	}

	updated, err := service.UpdateReport(created.ID, 123, true, updateReq)
	if err != nil {
		t.Fatalf("UpdateReport failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("Expected name 'Updated', got '%s'", updated.Name)
	}
	if updated.DataSource != "messages" {
		t.Errorf("Expected data_source 'messages', got '%s'", updated.DataSource)
	}
}

// TestCustomReportService_UpdateReport_NotFound 测试更新不存在的报表
func TestCustomReportService_UpdateReport_NotFound(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	updateReq := &UpdateReportRequest{
		Name:       "Updated",
		DataSource: "sessions",
		ChartType:  "line",
	}

	_, err := service.UpdateReport(999999, 123, true, updateReq)
	if err == nil {
		t.Error("Expected error for non-existent report")
	}
}

// TestCustomReportService_UpdateReport_SingleTenant 单租户模式下更新报表无需用户级别鉴权
func TestCustomReportService_UpdateReport_SingleTenant(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Other Report",
		DataSource: "sessions",
		ChartType:  "bar",
	}
	created, _ := service.CreateReport(123, createReq)

	updateReq := &UpdateReportRequest{
		Name:       "Updated",
		DataSource: "sessions",
		ChartType:  "bar",
	}

	updated, err := service.UpdateReport(created.ID, 123, true, updateReq)
	if err != nil {
		t.Fatalf("UpdateReport failed: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("Expected name 'Updated', got '%s'", updated.Name)
	}
}

// TestCustomReportService_DeleteReport_Success 测试删除报表成功
func TestCustomReportService_DeleteReport_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Delete Me",
		DataSource: "sessions",
		ChartType:  "table",
	}
	created, _ := service.CreateReport(123, createReq)

	err := service.DeleteReport(created.ID, 123, true)
	if err != nil {
		t.Fatalf("DeleteReport failed: %v", err)
	}

	_, err = service.GetReport(created.ID, 123, true)
	if err == nil {
		t.Error("Expected error after deletion")
	}
}

// TestCustomReportService_DeleteReport_NotFound 测试删除不存在的报表
func TestCustomReportService_DeleteReport_NotFound(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	err := service.DeleteReport(999999, 123, true)
	if err == nil {
		t.Error("Expected error for non-existent report")
	}
}

// TestCustomReportService_GetPublicTemplates_Success 测试获取公开模板
func TestCustomReportService_GetPublicTemplates_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Public Template",
		DataSource: "sessions",
		ChartType:  "line",
		IsPublic:   true,
	}
	service.CreateReport(1, createReq)

	templates, err := service.GetPublicTemplates()
	if err != nil {
		t.Fatalf("GetPublicTemplates failed: %v", err)
	}

	if len(templates) != 1 {
		t.Errorf("Expected 1 template, got %d", len(templates))
	}
}

// TestCustomReportService_UseTemplate_Success 测试使用模板
func TestCustomReportService_UseTemplate_Success(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Template",
		DataSource: "sessions",
		ChartType:  "bar",
		IsPublic:   true,
	}
	template, _ := service.CreateReport(1, createReq)

	_, err := service.UseTemplate(template.ID, 123)
	if err != nil {
		t.Fatalf("UseTemplate failed: %v", err)
	}

	_ = err
}

// TestCustomReportService_QueryReportData_Sessions 测试查询会话数据
func TestCustomReportService_QueryReportData_Sessions(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	session := &sysmodel.CustomerSession{
		UserID:       "user_123",
		AgentName:    "客服 A",
		Status:       sysmodel.SessionStatusPending,
		MessageCount: 10,
	}
	db.Create(session)

	createReq := &CreateReportRequest{
		Name:       "Session Report",
		DataSource: "sessions",
		Dimensions: []model.ReportDimension{{Field: "agent_name", Label: "客服"}},
		Metrics:    []model.ReportMetric{{Field: "session_count", Label: "会话数"}},
		ChartType:  "table",
	}
	if err := db.Create(&sysmodel.UnifiedMessage{ContentType: "text", Content: "hello"}).Error; err != nil {
		t.Fatalf("seed unified_message: %v", err)
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	if data.Total < 1 {
		t.Errorf("Expected at least 1 record, got %d", data.Total)
	}
}

// TestCustomReportService_QueryReportData_Clues 测试查询线索数据
func TestCustomReportService_QueryReportData_Clues(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	clue := &sysmodel.Clue{
		SourceID: "source_123",
		Account:  "user_123",
		Type:     1,
		IsVerify: 0,
		Name:     "Test User",
		Desc:     "Test content",
	}
	db.Create(clue)

	createReq := &CreateReportRequest{
		Name:       "Clue Report",
		DataSource: "clues",
		Dimensions: []model.ReportDimension{{Field: "type", Label: "类型"}},
		Metrics:    []model.ReportMetric{{Field: "clue_count", Label: "线索数"}},
		ChartType:  "pie",
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	if len(data.Data) < 1 {
		t.Errorf("Expected at least 1 record, got %d", len(data.Data))
	}
}

// TestCustomReportService_QueryReportData_RFM 测试查询 RFM 数据
func TestCustomReportService_QueryReportData_RFM(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	now := time.Now()
	rfm := &sysmodel.UserRFM{
		UserID:            123,
		Layer:             "important_value",
		RScore:            5,
		FScore:            5,
		MScore:            5,
		TotalScore:        15,
		LastTransactionAt: &now,
		TransactionCount:  5,
		TotalAmount:       100000,
		AvgAmount:         20000,
	}
	db.Create(rfm)

	createReq := &CreateReportRequest{
		Name:       "RFM Report",
		DataSource: "rfm",
		Dimensions: []model.ReportDimension{{Field: "layer", Label: "分层"}},
		Metrics:    []model.ReportMetric{{Field: "user_count", Label: "用户数"}},
		ChartType:  "bar",
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, map[string]any{"layer": ""})
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	if data.Total < 1 {
		t.Errorf("Expected at least 1 record, got %d", data.Total)
	}
}

// TestCustomReportService_QueryReportData_InvalidDataSource 测试无效数据源查询
func TestCustomReportService_QueryReportData_InvalidDataSource(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	report := &model.CustomReport{
		DataSource: "invalid_source",
	}

	_, err := service.QueryReportData(context.Background(), report, nil)
	if err == nil {
		t.Error("Expected error for invalid data source")
	}
}

// TestCustomReportService_QueryReportData_Messages 测试查询消息数据
func TestCustomReportService_QueryReportData_Messages(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "Message Report",
		DataSource: "messages",
		Dimensions: []model.ReportDimension{{Field: "msg_type", Label: "消息类型"}},
		Metrics:    []model.ReportMetric{{Field: "message_count", Label: "消息数"}},
		ChartType:  "area",
	}
	if err := db.Create(&sysmodel.UnifiedMessage{ContentType: "text", Content: "hello"}).Error; err != nil {
		t.Fatalf("seed unified_message: %v", err)
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	if data.Total < 1 {
		t.Errorf("Expected at least 1 record, got %d", data.Total)
	}
}

// TestCustomReportService_QueryReportData_Agents 测试查询客服数据
func TestCustomReportService_QueryReportData_Agents(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	if err := db.Create(&sysmodel.CustomerSession{AgentName: "agent_a", AvgResponseTime: 120}).Error; err != nil {
		t.Fatalf("seed customer_session: %v", err)
	}
	createReq := &CreateReportRequest{
		Name:       "Agent Report",
		DataSource: "agents",
		Dimensions: []model.ReportDimension{{Field: "agent_name", Label: "客服"}},
		Metrics:    []model.ReportMetric{{Field: "session_count", Label: "会话数"}, {Field: "avg_response_time", Label: "平均响应时间"}},
		ChartType:  "table",
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	if data.Total < 1 {
		t.Errorf("Expected at least 1 record, got %d", data.Total)
	}
}

// TestCustomReportService_QueryReportData_Users 测试查询用户数据
func TestCustomReportService_QueryReportData_Users(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	service := setupCustomReportService(t, db)

	createReq := &CreateReportRequest{
		Name:       "User Report",
		DataSource: "users",
		Dimensions: []model.ReportDimension{{Field: "gender", Label: "性别"}},
		Metrics:    []model.ReportMetric{{Field: "user_count", Label: "用户数"}},
		ChartType:  "pie",
	}
	report, err := service.CreateReport(123, createReq)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}

	data, err := service.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData failed: %v", err)
	}

	_ = data
}

// TestIsValidDataSource 测试数据源验证
func TestIsValidDataSource(t *testing.T) {
	tests := []struct {
		source   string
		expected bool
	}{
		{"sessions", true},
		{"messages", true},
		{"orders", true},
		{"clues", true},
		{"users", true},
		{"rfm", true},
		{"agents", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isValidDataSource(tt.source)
		if result != tt.expected {
			t.Errorf("isValidDataSource(%q) = %v, expected %v", tt.source, result, tt.expected)
		}
	}
}

// TestIsValidChartType 测试图表类型验证
func TestIsValidChartType(t *testing.T) {
	tests := []struct {
		chartType string
		expected  bool
	}{
		{"table", true},
		{"line", true},
		{"bar", true},
		{"pie", true},
		{"area", true},
		{"card", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isValidChartType(tt.chartType)
		if result != tt.expected {
			t.Errorf("isValidChartType(%q) = %v, expected %v", tt.chartType, result, tt.expected)
		}
	}
}
