package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupIntegrationTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.IntegrationAccount{},
		&model.SyncLog{},
		&model.ExternalCustomer{},
		&model.ExternalOrder{},
		&model.ExternalProduct{},
		&model.WebhookEvent{},
	)
	db.SetTestDB(database)
	return database
}

func setupIntegrationRepositories(t *testing.T) (
	*IntegrationAccountRepository,
	*SyncLogRepository,
	*ExternalCustomerRepository,
	*ExternalOrderRepository,
	*ExternalProductRepository,
	*WebhookEventRepository) {

	setupIntegrationTestDB(t)
	return NewIntegrationAccountRepository(),
		NewSyncLogRepository(),
		NewExternalCustomerRepository(),
		NewExternalOrderRepository(),
		NewExternalProductRepository(),
		NewWebhookEventRepository()
}

// TestIntegrationAccountRepository_Create 测试创建对接账号
func TestIntegrationAccountRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	tests := []struct {
		name    string
		account *model.IntegrationAccount
		wantErr bool
	}{
		{
			name: "create account success",
			account: &model.IntegrationAccount{
				Platform:    "crm_xiaoshouyi",
				AccountName: "Test Account",
				APIKey:      "test-api-key",
				APISecret:   "test-secret",
				Status:      1,
			},
			wantErr: false,
		},
		{
			name: "create account with tokens",
			account: &model.IntegrationAccount{
				Platform:     "ecommerce_taobao",
				AccountName:  "TB Account",
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				Status:       1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.account)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.account.ID == 0 {
				t.Error("Expected account ID to be set after creation")
			}
		})
	}
}

// TestIntegrationAccountRepository_GetByID 测试根据 ID 获取对接账号
func TestIntegrationAccountRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "GetByID Account",
		APIKey:      "test-key",
	}
	repo.Create(ctx, account)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing account",
			id:      account.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing account",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.AccountName != "GetByID Account" {
					t.Errorf("Expected account name 'GetByID Account', got '%s'", result.AccountName)
				}
			}
		})
	}
}

// TestIntegrationAccountRepository_GetByPlatform 测试根据平台获取对接账号
func TestIntegrationAccountRepository_GetByPlatform(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	repo.Create(ctx, &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "CRM Account",
	})
	repo.Create(ctx, &model.IntegrationAccount{
		Platform:    "ecommerce_taobao",
		AccountName: "TB Account",
	})

	tests := []struct {
		name       string
		merchantID string
		platform   string
		wantErr    bool
	}{
		{
			name: "get crm account",

			platform: "crm_xiaoshouyi",
			wantErr:  false,
		},
		{
			name: "get ecommerce account",

			platform: "ecommerce_taobao",
			wantErr:  false,
		},
		{
			name: "get non-existing platform",

			platform: "non_existing",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByPlatform(context.Background(), tt.platform)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByPlatform() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Platform != tt.platform {
					t.Errorf("Expected platform '%s', got '%s'", tt.platform, result.Platform)
				}
			}
		})
	}
}

// TestIntegrationAccountRepository_Update 测试更新对接账号
func TestIntegrationAccountRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Original Name",
		Status:      1,
	}
	repo.Create(ctx, account)

	account.AccountName = "Updated Name"
	account.Status = 0

	err := repo.Update(ctx, account)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, account.ID)
	if updated.AccountName != "Updated Name" {
		t.Errorf("Expected account name 'Updated Name', got '%s'", updated.AccountName)
	}
	if updated.Status != 0 {
		t.Errorf("Expected status 0, got %d", updated.Status)
	}
}

// TestIntegrationAccountRepository_UpdateToken 测试更新访问令牌
func TestIntegrationAccountRepository_UpdateToken(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Token Test",
	}
	repo.Create(ctx, account)

	expiresAt := time.Now().Add(time.Hour * 24)
	err := repo.UpdateToken(context.Background(), account.ID, "new-access-token", &expiresAt)
	if err != nil {
		t.Errorf("UpdateToken() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, account.ID)
	if updated.AccessToken != "new-access-token" {
		t.Errorf("Expected access token 'new-access-token', got '%s'", updated.AccessToken)
	}
	if updated.TokenExpires == nil {
		t.Error("Expected token expires to be set")
	}
}

// TestIntegrationAccountRepository_Delete 测试删除对接账号
func TestIntegrationAccountRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "To Delete",
	}
	repo.Create(ctx, account)

	err := repo.Delete(ctx, account.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, account.ID)
	if err == nil {
		t.Error("Expected account to be deleted")
	}
}

// TestIntegrationAccountRepository_UpdateSyncTime 测试更新同步时间
func TestIntegrationAccountRepository_UpdateSyncTime(t *testing.T) {
	ctx := context.Background()
	repo, _, _, _, _, _ := setupIntegrationRepositories(t)

	account := &model.IntegrationAccount{
		Platform:    "crm_xiaoshouyi",
		AccountName: "Sync Test",
	}
	repo.Create(ctx, account)

	err := repo.UpdateSyncTime(context.Background(), account.ID)
	if err != nil {
		t.Errorf("UpdateSyncTime() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, account.ID)
	if updated.LastSyncAt == nil {
		t.Error("Expected LastSyncAt to be updated")
	}
}

// TestSyncLogRepository_Create 测试创建同步日志
func TestSyncLogRepository_Create(t *testing.T) {
	ctx := context.Background()
	_, syncRepo, _, _, _, _ := setupIntegrationRepositories(t)

	tests := []struct {
		name    string
		log     *model.SyncLog
		wantErr bool
	}{
		{
			name: "create sync log success",
			log: &model.SyncLog{
				Platform:    "crm_xiaoshouyi",
				SyncType:    "customer",
				Status:      0,
				RecordCount: 0,
			},
			wantErr: false,
		},
		{
			name: "create sync log with error",
			log: &model.SyncLog{
				Platform:     "ecommerce_taobao",
				SyncType:     "order",
				Status:       2,
				RecordCount:  50,
				ErrorMessage: "Connection timeout",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syncRepo.Create(ctx, tt.log)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.log.ID == 0 {
				t.Error("Expected log ID to be set after creation")
			}
		})
	}
}

// TestSyncLogRepository_GetByID 测试根据 ID 获取同步日志
func TestSyncLogRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	_, syncRepo, _, _, _, _ := setupIntegrationRepositories(t)

	log := &model.SyncLog{
		Platform:    "crm_xiaoshouyi",
		SyncType:    "customer",
		Status:      1,
		RecordCount: 100,
	}
	syncRepo.Create(ctx, log)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing log",
			id:      log.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing log",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := syncRepo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.SyncType != "customer" {
					t.Errorf("Expected sync type 'customer', got '%s'", result.SyncType)
				}
			}
		})
	}
}

// TestSyncLogRepository_Update 测试更新同步日志
func TestSyncLogRepository_Update(t *testing.T) {
	ctx := context.Background()
	_, syncRepo, _, _, _, _ := setupIntegrationRepositories(t)

	log := &model.SyncLog{
		Platform:    "crm_xiaoshouyi",
		SyncType:    "customer",
		Status:      0,
		RecordCount: 0,
	}
	syncRepo.Create(ctx, log)

	log.Status = 1
	log.RecordCount = 100

	err := syncRepo.Update(ctx, log)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := syncRepo.GetByID(ctx, log.ID)
	if updated.Status != 1 {
		t.Errorf("Expected status 1, got %d", updated.Status)
	}
	if updated.RecordCount != 100 {
		t.Errorf("Expected record count 100, got %d", updated.RecordCount)
	}
}

// TestSyncLogRepository_UpdateStatus 测试更新同步状态
func TestSyncLogRepository_UpdateStatus(t *testing.T) {
	ctx := context.Background()
	_, syncRepo, _, _, _, _ := setupIntegrationRepositories(t)

	log := &model.SyncLog{
		Platform: "crm_xiaoshouyi",
		SyncType: "customer",
		Status:   0,
	}
	syncRepo.Create(ctx, log)

	tests := []struct {
		name         string
		status       int
		recordCount  int
		errorMessage string
		wantErr      bool
	}{
		{
			name:         "update to success",
			status:       1,
			recordCount:  100,
			errorMessage: "",
			wantErr:      false,
		},
		{
			name:         "update to failed",
			status:       2,
			recordCount:  50,
			errorMessage: "Connection timeout",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syncRepo.UpdateStatus(context.Background(), log.ID, tt.status, tt.recordCount, tt.errorMessage)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := syncRepo.GetByID(ctx, log.ID)
				if updated.Status != tt.status {
					t.Errorf("Expected status %d, got %d", tt.status, updated.Status)
				}
				if updated.RecordCount != tt.recordCount {
					t.Errorf("Expected record count %d, got %d", tt.recordCount, updated.RecordCount)
				}
				if updated.ErrorMessage != tt.errorMessage {
					t.Errorf("Expected error message '%s', got '%s'", tt.errorMessage, updated.ErrorMessage)
				}
			}
		})
	}
}

// TestExternalCustomerRepository_Create 测试创建外部客户
func TestExternalCustomerRepository_Create(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	tests := []struct {
		name     string
		customer *model.ExternalCustomer
		wantErr  bool
	}{
		{
			name: "create customer success",
			customer: &model.ExternalCustomer{
				Platform:   "crm_xiaoshouyi",
				ExternalID: "ext-123",
				Name:       "Test Customer",
				Phone:      "13800138000",
				Email:      "test@example.com",
				Company:    "Test Company",
				Level:      "vip",
				Status:     "active",
			},
			wantErr: false,
		},
		{
			name: "create customer with tags",
			customer: &model.ExternalCustomer{
				Platform:   "crm_xiaoshouyi",
				ExternalID: "ext-456",
				Name:       "Tagged Customer",
				Phone:      "13900139000",
				Tags:       `["vip", "hot"]`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := customerRepo.Create(ctx, tt.customer)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.customer.ID == 0 {
				t.Error("Expected customer ID to be set after creation")
			}
		})
	}
}

// TestExternalCustomerRepository_GetByID 测试根据 ID 获取外部客户
func TestExternalCustomerRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	customer := &model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "ext-getbyid",
		Name:       "GetByID Customer",
		Phone:      "13800138000",
	}
	customerRepo.Create(ctx, customer)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing customer",
			id:      customer.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing customer",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := customerRepo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.Name != "GetByID Customer" {
					t.Errorf("Expected name 'GetByID Customer', got '%s'", result.Name)
				}
			}
		})
	}
}

// TestExternalCustomerRepository_GetByExternalID 测试根据外部 ID 获取外部客户
func TestExternalCustomerRepository_GetByExternalID(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	customer := &model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "unique-external-id",
		Name:       "Unique Customer",
	}
	customerRepo.Create(ctx, customer)

	result, err := customerRepo.GetByExternalID(context.Background(), "crm_xiaoshouyi", "unique-external-id")
	if err != nil {
		t.Errorf("GetByExternalID() error = %v", err)
	}

	if result.Name != "Unique Customer" {
		t.Errorf("Expected name 'Unique Customer', got '%s'", result.Name)
	}

	_, err = customerRepo.GetByExternalID(context.Background(), "crm_xiaoshouyi", "non-existing")
	if err == nil {
		t.Error("Expected error for non-existing external ID")
	}
}

func TestExternalCustomerRepository_GetByPlatform(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	customerRepo.Create(ctx, &model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "crm-1",
		Name:       "CRM Customer 1",
	})
	customerRepo.Create(ctx, &model.ExternalCustomer{
		Platform:   "ecommerce_taobao",
		ExternalID: "tb-1",
		Name:       "TB Customer 1",
	})

	tests := []struct {
		name       string
		merchantID string
		platform   string
		wantCount  int
		wantErr    bool
	}{
		{
			name: "get crm customers",

			platform:  "crm_xiaoshouyi",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "get ecommerce customers",

			platform:  "ecommerce_taobao",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := customerRepo.GetByPlatform(context.Background(), tt.platform, 1, 100)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByPlatform() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if int(total) != tt.wantCount {
				t.Errorf("Expected total %d, got %d", tt.wantCount, total)
			}
		})
	}
}

// TestExternalCustomerRepository_Update 测试更新外部客户
func TestExternalCustomerRepository_Update(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	customer := &model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "ext-update",
		Name:       "Original Name",
		Phone:      "13800138000",
	}
	customerRepo.Create(ctx, customer)

	customer.Name = "Updated Name"
	customer.Email = "updated@example.com"

	err := customerRepo.Update(ctx, customer)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := customerRepo.GetByID(ctx, customer.ID)
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.Email != "updated@example.com" {
		t.Errorf("Expected email 'updated@example.com', got '%s'", updated.Email)
	}
}

// TestExternalCustomerRepository_Delete 测试删除外部客户
func TestExternalCustomerRepository_Delete(t *testing.T) {
	ctx := context.Background()
	_, _, customerRepo, _, _, _ := setupIntegrationRepositories(t)

	customer := &model.ExternalCustomer{
		Platform:   "crm_xiaoshouyi",
		ExternalID: "ext-delete",
		Name:       "To Delete",
	}
	customerRepo.Create(ctx, customer)

	err := customerRepo.Delete(ctx, customer.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = customerRepo.GetByID(ctx, customer.ID)
	if err == nil {
		t.Error("Expected customer to be deleted")
	}
}

// TestExternalOrderRepository_Create 测试创建外部订单
func TestExternalOrderRepository_Create(t *testing.T) {
	ctx := context.Background()
	_, _, _, orderRepo, _, _ := setupIntegrationRepositories(t)

	tests := []struct {
		name    string
		order   *model.ExternalOrder
		wantErr bool
	}{
		{
			name: "create order success",
			order: &model.ExternalOrder{
				Platform:       "ecommerce_taobao",
				OrderID:        "tb-order-123",
				OrderNo:        "internal-order-123",
				UserID:         "user-123",
				UserName:       "Test User",
				UserPhone:      "13800138000",
				TotalAmount:    10000,
				PayAmount:      9000,
				DiscountAmount: 1000,
				Status:         "pending",
				Items:          `[{"id": "item-1", "name": "Item 1", "price": 100, "quantity": 1}]`,
				ShippingAddr:   `{"address": "Test Address", "city": "Shanghai"}`,
			},
			wantErr: false,
		},
		{
			name: "create order with payment time",
			order: &model.ExternalOrder{
				Platform:    "ecommerce_jd",
				OrderID:     "jd-order-456",
				OrderNo:     "internal-order-456",
				UserID:      "user-456",
				UserName:    "JD User",
				TotalAmount: 20000,
				PayAmount:   20000,
				Status:      "paid",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := orderRepo.Create(ctx, tt.order)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.order.ID == 0 {
				t.Error("Expected order ID to be set after creation")
			}
		})
	}
}

// TestExternalOrderRepository_GetByOrderID 测试根据外部订单号获取订单
func TestExternalOrderRepository_GetByOrderID(t *testing.T) {
	ctx := context.Background()
	_, _, _, orderRepo, _, _ := setupIntegrationRepositories(t)

	order := &model.ExternalOrder{
		Platform:    "ecommerce_taobao",
		OrderID:     "unique-order-id",
		OrderNo:     "internal-123",
		UserID:      "user-123",
		UserName:    "Order User",
		TotalAmount: 15000,
	}
	orderRepo.Create(ctx, order)

	tests := []struct {
		name    string
		orderID string
		wantErr bool
		wantNil bool
	}{
		{
			name:    "get existing order",
			orderID: "unique-order-id",
		},
		{
			// "库里没有这一行"必须与"读不到"分得开。老形状是 (非 nil 空结构体, ErrRecordNotFound)，
			// 而调用侧写成 `existing, _ :=`，于是两种情况都长成"有一行空的" ⇒ 走 Save 插行，
			// 且一次真实读故障会被当成首次见单（G15 第④条）。
			name:    "get non-existing order",
			orderID: "non-existing-order",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := orderRepo.GetByOrderID(context.Background(), "ecommerce_taobao", tt.orderID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByOrderID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantNil {
				if result != nil {
					t.Errorf("未命中应回 nil，实际 %+v", *result)
				}
				return
			}
			if result == nil {
				t.Fatal("命中却回 nil")
			}
			if result.OrderID != tt.orderID {
				t.Errorf("Expected order ID '%s', got '%s'", tt.orderID, result.OrderID)
			}
			if result.UserName != "Order User" {
				t.Errorf("Expected user name 'Order User', got '%s'", result.UserName)
			}
		})
	}
}

func TestExternalOrderRepository_GetByPlatform(t *testing.T) {
	ctx := context.Background()
	_, _, _, orderRepo, _, _ := setupIntegrationRepositories(t)

	orderRepo.Create(ctx, &model.ExternalOrder{
		Platform: "ecommerce_taobao",
		OrderID:  "tb-order-1",
		OrderNo:  "tb-1",
		UserID:   "user-1",
	})
	orderRepo.Create(ctx, &model.ExternalOrder{
		Platform: "ecommerce_jd",
		OrderID:  "jd-order-1",
		OrderNo:  "jd-1",
		UserID:   "user-2",
	})

	tests := []struct {
		name       string
		merchantID string
		platform   string
		wantCount  int
		wantErr    bool
	}{
		{
			name: "get taobao orders",

			platform:  "ecommerce_taobao",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "get jd orders",

			platform:  "ecommerce_jd",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := orderRepo.GetByPlatform(context.Background(), tt.platform, 1, 100)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByPlatform() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if int(total) != tt.wantCount {
				t.Errorf("Expected total %d, got %d", tt.wantCount, total)
			}
		})
	}
}

// TestExternalOrderRepository_Update 测试更新外部订单
func TestExternalOrderRepository_Update(t *testing.T) {
	ctx := context.Background()
	_, _, _, orderRepo, _, _ := setupIntegrationRepositories(t)

	order := &model.ExternalOrder{
		Platform:    "ecommerce_taobao",
		OrderID:     "update-order",
		OrderNo:     "internal-update",
		UserID:      "user-123",
		UserName:    "Original User",
		TotalAmount: 10000,
		Status:      "pending",
	}
	orderRepo.Create(ctx, order)

	order.UserName = "Updated User"
	order.Status = "paid"

	err := orderRepo.Update(ctx, order)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := orderRepo.GetByID(ctx, order.ID)
	if updated.UserName != "Updated User" {
		t.Errorf("Expected user name 'Updated User', got '%s'", updated.UserName)
	}
	if updated.Status != "paid" {
		t.Errorf("Expected status 'paid', got '%s'", updated.Status)
	}
}

// TestExternalOrderRepository_Delete 测试删除外部订单
func TestExternalOrderRepository_Delete(t *testing.T) {
	ctx := context.Background()
	_, _, _, orderRepo, _, _ := setupIntegrationRepositories(t)

	order := &model.ExternalOrder{
		Platform: "ecommerce_taobao",
		OrderID:  "delete-order",
		OrderNo:  "internal-delete",
		UserID:   "user-123",
	}
	orderRepo.Create(ctx, order)

	err := orderRepo.Delete(ctx, order.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = orderRepo.GetByID(ctx, order.ID)
	if err == nil {
		t.Error("Expected order to be deleted")
	}
}

// TestExternalProductRepository_Create 测试创建外部商品
func TestExternalProductRepository_Create(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, productRepo, _ := setupIntegrationRepositories(t)

	tests := []struct {
		name    string
		product *model.ExternalProduct
		wantErr bool
	}{
		{
			name: "create product success",
			product: &model.ExternalProduct{
				Platform:      "ecommerce_taobao",
				ProductID:     "prod-123",
				Name:          "Test Product",
				CategoryID:    "cat-1",
				CategoryName:  "Test Category",
				Price:         9900,
				OriginalPrice: 19900,
				Stock:         100,
				Sales:         50,
				Images:        `["image1.jpg", "image2.jpg"]`,
				Status:        1,
			},
			wantErr: false,
		},
		{
			name: "create product with minimal fields",
			product: &model.ExternalProduct{
				Platform:  "ecommerce_jd",
				ProductID: "prod-456",
				Name:      "JD Product",
				Price:     15000,
				Stock:     50,
				Status:    1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := productRepo.Create(ctx, tt.product)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.product.ID == 0 {
				t.Error("Expected product ID to be set after creation")
			}
		})
	}
}

// TestExternalProductRepository_GetByProductID 测试根据商品 ID 获取商品
func TestExternalProductRepository_GetByProductID(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, productRepo, _ := setupIntegrationRepositories(t)

	product := &model.ExternalProduct{
		Platform:      "ecommerce_taobao",
		ProductID:     "unique-product-id",
		Name:          "Unique Product",
		Price:         9900,
		OriginalPrice: 19900,
		Stock:         100,
	}
	productRepo.Create(ctx, product)

	tests := []struct {
		name      string
		productID string
		wantErr   bool
	}{
		{
			name:      "get existing product",
			productID: "unique-product-id",
			wantErr:   false,
		},
		{
			name:      "get non-existing product",
			productID: "non-existing-product",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := productRepo.GetByProductID(context.Background(), "ecommerce_taobao", tt.productID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByProductID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ProductID != tt.productID {
					t.Errorf("Expected product ID '%s', got '%s'", tt.productID, result.ProductID)
				}
				if result.Name != "Unique Product" {
					t.Errorf("Expected name 'Unique Product', got '%s'", result.Name)
				}
			}
		})
	}
}

// TestExternalProductRepository_Update 测试更新外部商品
func TestExternalProductRepository_Update(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, productRepo, _ := setupIntegrationRepositories(t)

	product := &model.ExternalProduct{
		Platform:  "ecommerce_taobao",
		ProductID: "update-product",
		Name:      "Original Name",
		Price:     9900,
		Stock:     100,
		Status:    1,
	}
	productRepo.Create(ctx, product)

	product.Name = "Updated Name"
	product.Price = 14900
	product.Stock = 50

	err := productRepo.Update(ctx, product)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := productRepo.GetByID(ctx, product.ID)
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.Price != 14900 {
		t.Errorf("Expected price 14900, got %d", updated.Price)
	}
	if updated.Stock != 50 {
		t.Errorf("Expected stock 50, got %d", updated.Stock)
	}
}

// TestExternalProductRepository_Delete 测试删除外部商品
func TestExternalProductRepository_Delete(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, productRepo, _ := setupIntegrationRepositories(t)

	product := &model.ExternalProduct{
		Platform:  "ecommerce_taobao",
		ProductID: "delete-product",
		Name:      "To Delete",
		Price:     9900,
	}
	productRepo.Create(ctx, product)

	err := productRepo.Delete(ctx, product.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = productRepo.GetByID(ctx, product.ID)
	if err == nil {
		t.Error("Expected product to be deleted")
	}
}

// TestWebhookEventRepository_Create 测试创建 Webhook 事件
func TestWebhookEventRepository_Create(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, webhookRepo := setupIntegrationRepositories(t)

	tests := []struct {
		name    string
		event   *model.WebhookEvent
		wantErr bool
	}{
		{
			name: "create event success",
			event: &model.WebhookEvent{
				Platform:  "ecommerce_taobao",
				EventID:   "event-123",
				EventType: "order.paid",
				RawData:   `{"order_id": "123", "status": "paid"}`,
				Processed: false,
			},
			wantErr: false,
		},
		{
			name: "create processed event",
			event: &model.WebhookEvent{
				Platform:  "crm_xiaoshouyi",
				EventID:   "event-456",
				EventType: "customer.created",
				RawData:   `{"customer_id": "456"}`,
				Processed: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhookRepo.Create(ctx, tt.event)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.event.ID == 0 {
				t.Error("Expected event ID to be set after creation")
			}
		})
	}
}

// TestWebhookEventRepository_GetByEventID 测试根据事件 ID 获取 Webhook 事件
func TestWebhookEventRepository_GetByEventID(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, webhookRepo := setupIntegrationRepositories(t)

	event := &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "unique-event-id",
		EventType: "order.paid",
		RawData:   `{"data": "test"}`,
	}
	webhookRepo.Create(ctx, event)

	tests := []struct {
		name    string
		eventID string
		wantErr bool
	}{
		{
			name:    "get existing event",
			eventID: "unique-event-id",
			wantErr: false,
		},
		{
			name:    "get non-existing event",
			eventID: "non-existing-event",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := webhookRepo.GetByEventID(context.Background(), tt.eventID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByEventID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.EventID != tt.eventID {
					t.Errorf("Expected event ID '%s', got '%s'", tt.eventID, result.EventID)
				}
				if result.EventType != "order.paid" {
					t.Errorf("Expected event type 'order.paid', got '%s'", result.EventType)
				}
			}
		})
	}
}

// TestWebhookEventRepository_GetUnprocessed 测试获取未处理的 Webhook 事件
func TestWebhookEventRepository_GetUnprocessed(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, webhookRepo := setupIntegrationRepositories(t)

	webhookRepo.Create(ctx, &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "unprocessed-1",
		EventType: "order.paid",
		Processed: false,
	})
	webhookRepo.Create(ctx, &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "unprocessed-2",
		EventType: "order.shipped",
		Processed: false,
	})
	webhookRepo.Create(ctx, &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "processed-1",
		EventType: "order.delivered",
		Processed: true,
	})

	results, err := webhookRepo.GetUnprocessed(context.Background(), "ecommerce_taobao", 10)
	if err != nil {
		t.Errorf("GetUnprocessed() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 unprocessed events, got %d", len(results))
	}
}

// TestWebhookEventRepository_MarkProcessed 测试标记 Webhook 事件为已处理
func TestWebhookEventRepository_MarkProcessed(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, webhookRepo := setupIntegrationRepositories(t)

	event := &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "mark-processed",
		EventType: "order.paid",
		Processed: false,
	}
	webhookRepo.Create(ctx, event)

	err := webhookRepo.MarkProcessed(context.Background(), event.ID)
	if err != nil {
		t.Errorf("MarkProcessed() error = %v", err)
	}

	updated, _ := webhookRepo.GetByID(ctx, event.ID)
	if !updated.Processed {
		t.Error("Expected event to be marked as processed")
	}
	if updated.ProcessedAt == nil {
		t.Error("Expected ProcessedAt to be set")
	}
}

// TestWebhookEventRepository_Update 测试更新 Webhook 事件
func TestWebhookEventRepository_Update(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, _, webhookRepo := setupIntegrationRepositories(t)

	event := &model.WebhookEvent{
		Platform:  "ecommerce_taobao",
		EventID:   "update-event",
		EventType: "order.paid",
		RawData:   `{"original": "data"}`,
		Processed: false,
	}
	webhookRepo.Create(ctx, event)

	event.EventType = "order.updated"
	event.RawData = `{"updated": "data"}`

	err := webhookRepo.Update(ctx, event)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := webhookRepo.GetByID(ctx, event.ID)
	if updated.EventType != "order.updated" {
		t.Errorf("Expected event type 'order.updated', got '%s'", updated.EventType)
	}
	if updated.RawData != `{"updated": "data"}` {
		t.Errorf("Expected raw data '{\"updated\": \"data\"}', got '%s'", updated.RawData)
	}
}
