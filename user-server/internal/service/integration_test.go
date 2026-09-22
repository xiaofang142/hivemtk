package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupIntegrationServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.IntegrationAccount{},
		&model.SyncLog{},
		&model.ExternalCustomer{},
		&model.ExternalOrder{},
		&model.ExternalProduct{},
		&model.WebhookEvent{},
		// payments 在名单里不是为了测回款腿本身（它有 service/payment_test.go 那一套），
		// 而是为了让"这条回调没有记下任何一笔钱"成为一个**数得出来**的断言：
		// 表不存在时"没记上"与"没地方记"在计数上长成同一个样子（relation does not exist）。
		&model.Payment{},
		// bills 同理，且它是**前置条件**那一格：回款要冲一张应收，而 webhook 的入参里
		// 只有 bill_id。没有这张表，"腿没装配"与"库里没这张单"都红在同一个地方（见
		// order_webhook_payment_wiring_test.go）。
		&model.Bill{},
	)
	db.SetTestDB(database)
	return database
}

func setupIntegrationService(t *testing.T) *IntegrationService {
	setupIntegrationServiceTestDB(t)
	return NewIntegrationService()
}

// TestNewIntegrationService 测试创建服务实例
func TestNewIntegrationService(t *testing.T) {
	service := NewIntegrationService()
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.accountRepo == nil {
		t.Error("Expected accountRepo to be initialized")
	}
	if service.syncLogRepo == nil {
		t.Error("Expected syncLogRepo to be initialized")
	}
	if service.customerRepo == nil {
		t.Error("Expected customerRepo to be initialized")
	}
	if service.orderRepo == nil {
		t.Error("Expected orderRepo to be initialized")
	}
	if service.productRepo == nil {
		t.Error("Expected productRepo to be initialized")
	}
	if service.webhookEventRepo == nil {
		t.Error("Expected webhookEventRepo to be initialized")
	}
}

// TestIntegrationService_CreateIntegrationAccount 测试创建对接账号
func TestIntegrationService_CreateIntegrationAccount(t *testing.T) {
	service := setupIntegrationService(t)

	req := &CreateIntegrationAccountRequest{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_api_key",
		APISecret:   "test_api_secret",
		Config:      map[string]any{"key": "value"},
	}

	account, err := service.CreateIntegrationAccount(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateIntegrationAccount failed: %v", err)
	}

	if account == nil {
		t.Fatal("Expected account to be returned")
	}
	if account.Platform != "crm_xiaoshouyi" {
		t.Errorf("Expected platform 'crm_xiaoshouyi', got '%s'", account.Platform)
	}
	if account.AccountName != "Test Account" {
		t.Errorf("Expected account name 'Test Account', got '%s'", account.AccountName)
	}
	if account.APIKey != "test_api_key" {
		t.Errorf("Expected API key 'test_api_key', got '%s'", account.APIKey)
	}
	if account.Status != 1 {
		t.Errorf("Expected status 1, got %d", account.Status)
	}
}

// TestIntegrationService_CreateIntegrationAccount_EmptyConfig 测试创建对接账号（空配置）
func TestIntegrationService_CreateIntegrationAccount_EmptyConfig(t *testing.T) {
	service := setupIntegrationService(t)

	req := &CreateIntegrationAccountRequest{
		Platform:    "crm_fenxiangxiao",
		AccountName: "Test Account 2",
		APIKey:      "test_key_2",
		APISecret:   "test_secret_2",
	}

	account, err := service.CreateIntegrationAccount(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateIntegrationAccount failed: %v", err)
	}

	if account.Config != "" {
		t.Errorf("Expected empty config, got '%s'", account.Config)
	}
}

// TestIntegrationService_GetIntegrationAccountList 测试获取对接账号列表
func TestIntegrationService_GetIntegrationAccountList(t *testing.T) {
	service := setupIntegrationService(t)

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.IntegrationAccount{
			Platform:    "crm_xiaoshouyi",
			AccountName: "Account",
			APIKey:      "key",
			APISecret:   "secret",
			Status:      1,
		})
	}

	accounts, err := service.GetIntegrationAccountList(context.Background())
	if err != nil {
		t.Fatalf("GetIntegrationAccountList failed: %v", err)
	}

	if len(accounts) != 3 {
		t.Errorf("Expected 3 accounts, got %d", len(accounts))
	}
}

// TestIntegrationService_GetIntegrationAccountByID 测试获取对接账号详情
func TestIntegrationService_GetIntegrationAccountByID(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	result, err := service.GetIntegrationAccountByID(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("GetIntegrationAccountByID failed: %v", err)
	}

	if result.AccountName != "Test Account" {
		t.Errorf("Expected account name 'Test Account', got '%s'", result.AccountName)
	}
}

// TestIntegrationService_GetIntegrationAccountByID_SingleTenant 单租户访问验证
// 单租户私有部署：所有数据归当前部署实例所有，GetIntegrationAccountByID 不做跨租户校验。
func TestIntegrationService_GetIntegrationAccountByID_SingleTenant(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	got, err := service.GetIntegrationAccountByID(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("GetIntegrationAccountByID should succeed in single-tenant mode, got: %v", err)
	}
	if got == nil || got.ID != account.ID {
		t.Errorf("Expected account ID %d, got %v", account.ID, got)
	}
}

// TestIntegrationService_UpdateIntegrationAccount 测试更新对接账号
func TestIntegrationService_UpdateIntegrationAccount(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Old Name",
		APIKey:      "old_key",
		APISecret:   "old_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	req := &CreateIntegrationAccountRequest{
		AccountName: "New Name",
		APIKey:      "new_key",
		APISecret:   "new_secret",
		Config:      map[string]any{"updated": true},
	}

	updated, err := service.UpdateIntegrationAccount(context.Background(), account.ID, req)
	if err != nil {
		t.Fatalf("UpdateIntegrationAccount failed: %v", err)
	}

	if updated.AccountName != "New Name" {
		t.Errorf("Expected account name 'New Name', got '%s'", updated.AccountName)
	}
	if updated.APIKey != "new_key" {
		t.Errorf("Expected API key 'new_key', got '%s'", updated.APIKey)
	}
}

// TestIntegrationService_UpdateIntegrationAccount_SingleTenant 单租户更新验证
// 单租户私有部署：所有数据归当前部署实例所有，UpdateIntegrationAccount 不做跨租户校验。
func TestIntegrationService_UpdateIntegrationAccount_SingleTenant(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	req := &CreateIntegrationAccountRequest{
		AccountName: "Updated",
	}
	updated, err := service.UpdateIntegrationAccount(context.Background(), account.ID, req)
	if err != nil {
		t.Fatalf("UpdateIntegrationAccount should succeed in single-tenant mode, got: %v", err)
	}
	if updated.AccountName != "Updated" {
		t.Errorf("Expected account name 'Updated', got '%s'", updated.AccountName)
	}
}

// TestIntegrationService_DeleteIntegrationAccount 测试删除对接账号
func TestIntegrationService_DeleteIntegrationAccount(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	err := service.DeleteIntegrationAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("DeleteIntegrationAccount failed: %v", err)
	}

	_, err = service.GetIntegrationAccountByID(context.Background(), account.ID)
	if err == nil {
		t.Error("Expected account to be deleted")
	}
}

// TestIntegrationService_DeleteIntegrationAccount_SingleTenant 单租户删除验证
// 单租户私有部署：所有数据归当前部署实例所有，DeleteIntegrationAccount 不做跨租户校验。
func TestIntegrationService_DeleteIntegrationAccount_SingleTenant(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Test Account",
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Status:      1,
	}
	db.GetDB().Create(account)

	if err := service.DeleteIntegrationAccount(context.Background(), account.ID); err != nil {
		t.Fatalf("DeleteIntegrationAccount should succeed in single-tenant mode, got: %v", err)
	}

	var count int64
	db.GetDB().Model(&model.IntegrationAccount{}).Where("id = ?", account.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected account to be deleted, got count %d", count)
	}
}

// TestIntegrationService_SyncCustomers_UnsupportedPlatform 测试不支持的平台
func TestIntegrationService_SyncCustomers_UnsupportedPlatform(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform: "unsupported_platform",
	}

	_, err := service.SyncCustomers(context.Background(), account)
	if err == nil {
		t.Error("Expected error for unsupported platform")
	}
}

// TestIntegrationService_SyncOrders_UnsupportedPlatform 测试不支持的平台同步订单
func TestIntegrationService_SyncOrders_UnsupportedPlatform(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform: "unsupported_platform",
	}

	_, err := service.SyncOrders(context.Background(), account)
	if err == nil {
		t.Error("Expected error for unsupported platform")
	}
}

// TestIntegrationService_SyncProducts_UnsupportedPlatform 测试不支持的平台同步商品
func TestIntegrationService_SyncProducts_UnsupportedPlatform(t *testing.T) {
	service := setupIntegrationService(t)

	account := &model.IntegrationAccount{
		Platform: "unsupported_platform",
	}

	_, err := service.SyncProducts(context.Background(), account)
	if err == nil {
		t.Error("Expected error for unsupported platform")
	}
}

// TestIntegrationService_GetSyncLogs 测试获取同步日志
func TestIntegrationService_GetSyncLogs(t *testing.T) {
	service := setupIntegrationService(t)

	for i := 0; i < 5; i++ {
		db.GetDB().Create(&model.SyncLog{
			Platform:    "crm_xiaoshouyi",
			SyncType:    "customer",
			Status:      1,
			RecordCount: 10,
		})
	}

	logs, total, err := service.GetSyncLogs(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected 5 total logs, got %d", total)
	}
	if len(logs) != 5 {
		t.Errorf("Expected 5 logs, got %d", len(logs))
	}
}

// TestIntegrationService_GetExternalCustomers 测试获取外部客户列表
func TestIntegrationService_GetExternalCustomers(t *testing.T) {
	service := setupIntegrationService(t)

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.ExternalCustomer{
			Platform:   "crm_xiaoshouyi",
			ExternalID: "ext_id",
			Name:       "Customer",
		})
	}

	customers, total, err := service.GetExternalCustomers(context.Background(), "crm_xiaoshouyi", 1, 10)
	if err != nil {
		t.Fatalf("GetExternalCustomers failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total customers, got %d", total)
	}
	if len(customers) != 3 {
		t.Errorf("Expected 3 customers, got %d", len(customers))
	}
}

// TestIntegrationService_GetExternalCustomers_NoPlatformFilter 测试获取外部客户列表（不筛选平台）
func TestIntegrationService_GetExternalCustomers_NoPlatformFilter(t *testing.T) {
	service := setupIntegrationService(t)

	db.GetDB().Create(&model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "ext_id_1",
		Name:       "Customer 1",
	})
	db.GetDB().Create(&model.ExternalCustomer{
		Platform:   "crm_fenxiangxiao",
		ExternalID: "ext_id_2",
		Name:       "Customer 2",
	})

	customers, total, err := service.GetExternalCustomers(context.Background(), "", 1, 10)
	if err != nil {
		t.Fatalf("GetExternalCustomers failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 total customers, got %d", total)
	}
	if len(customers) != 2 {
		t.Errorf("Expected 2 customers, got %d", len(customers))
	}
}

// TestIntegrationService_GetExternalOrders 测试获取外部订单列表
func TestIntegrationService_GetExternalOrders(t *testing.T) {
	service := setupIntegrationService(t)

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.ExternalOrder{
			Platform: "ecommerce_taobao",
			OrderID:  fmt.Sprintf("order_id_%d", i),
			Status:   "paid",
		})
	}

	orders, total, err := service.GetExternalOrders(context.Background(), "ecommerce_taobao", 1, 10)
	if err != nil {
		t.Fatalf("GetExternalOrders failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total orders, got %d", total)
	}
	if len(orders) != 3 {
		t.Errorf("Expected 3 orders, got %d", len(orders))
	}
}

// TestIntegrationService_GetExternalOrders_NoPlatformFilter 测试获取外部订单列表（不筛选平台）
func TestIntegrationService_GetExternalOrders_NoPlatformFilter(t *testing.T) {
	service := setupIntegrationService(t)

	db.GetDB().Create(&model.ExternalOrder{
		Platform: "ecommerce_taobao",
		OrderID:  "order_1",
		Status:   "paid",
	})
	db.GetDB().Create(&model.ExternalOrder{
		Platform: "ecommerce_jd",
		OrderID:  "order_2",
		Status:   "shipped",
	})

	orders, total, err := service.GetExternalOrders(context.Background(), "", 1, 10)
	if err != nil {
		t.Fatalf("GetExternalOrders failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 total orders, got %d", total)
	}
	if len(orders) != 2 {
		t.Errorf("Expected 2 orders, got %d", len(orders))
	}
}

// TestIntegrationService_GetExternalProducts 测试获取外部商品列表
func TestIntegrationService_GetExternalProducts(t *testing.T) {
	service := setupIntegrationService(t)

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.ExternalProduct{
			Platform:  "ecommerce_taobao",
			ProductID: "product_id",
			Name:      "Product",
			Price:     10000,
			Stock:     10,
		})
	}

	products, total, err := service.GetExternalProducts(context.Background(), "ecommerce_taobao", 1, 10)
	if err != nil {
		t.Fatalf("GetExternalProducts failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total products, got %d", total)
	}
	if len(products) != 3 {
		t.Errorf("Expected 3 products, got %d", len(products))
	}
}

// TestNewXiaoshouyiClient 测试创建销售易客户端
func TestNewXiaoshouyiClient(t *testing.T) {
	account := &model.IntegrationAccount{
		ID:        1,
		Platform:  "crm_xiaoshouyi",
		APIKey:    "test_key",
		APISecret: "test_secret",
	}

	clientRepo := NewIntegrationService().accountRepo
	client := NewXiaoshouyiClient(account, clientRepo)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.account != account {
		t.Error("Expected account to be set")
	}
	if client.httpClient == nil {
		t.Error("Expected httpClient to be initialized")
	}
}

// TestNewFenxiangxiaoClient 测试创建纷享销客客户端
func TestNewFenxiangxiaoClient(t *testing.T) {
	account := &model.IntegrationAccount{
		ID:        1,
		Platform:  "crm_fenxiangxiao",
		APIKey:    "test_key",
		APISecret: "test_secret",
	}

	clientRepo := NewIntegrationService().accountRepo
	client := NewFenxiangxiaoClient(account, clientRepo)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.account != account {
		t.Error("Expected account to be set")
	}
	if client.httpClient == nil {
		t.Error("Expected httpClient to be initialized")
	}
}

// TestNewTaobaoClient 测试创建淘宝客户端
func TestNewTaobaoClient(t *testing.T) {
	account := &model.IntegrationAccount{
		ID:        1,
		Platform:  "ecommerce_taobao",
		APIKey:    "test_key",
		APISecret: "test_secret",
	}

	client := NewTaobaoClient(account)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.account != account {
		t.Error("Expected account to be set")
	}
	if client.appKey != "test_key" {
		t.Errorf("Expected appKey 'test_key', got '%s'", client.appKey)
	}
	if client.appSecret != "test_secret" {
		t.Errorf("Expected appSecret 'test_secret', got '%s'", client.appSecret)
	}
}

// TestNewJDClient 测试创建京东客户端
func TestNewJDClient(t *testing.T) {
	account := &model.IntegrationAccount{
		ID:        1,
		Platform:  "ecommerce_jd",
		APIKey:    "test_key",
		APISecret: "test_secret",
	}

	client := NewJDClient(account)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.account != account {
		t.Error("Expected account to be set")
	}
	if client.appKey != "test_key" {
		t.Errorf("Expected appKey 'test_key', got '%s'", client.appKey)
	}
	if client.appSecret != "test_secret" {
		t.Errorf("Expected appSecret 'test_secret', got '%s'", client.appSecret)
	}
}
