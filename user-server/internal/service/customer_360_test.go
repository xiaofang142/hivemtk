package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	_type "hivemtk-user/internal/pkg/utils/type"
	"hivemtk-user/internal/repository"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestCustomerListFilteredPagination 纯单测：匹配后稳定分页，空页不丢总数。
func TestCustomerListFilteredPagination(t *testing.T) {
	matched := map[string]*Customer360DTO{}
	for _, id := range []string{"z", "a", "m"} {
		dto := &Customer360DTO{BasicInfo: &CustomerBasicInfo{SourcePlatform: "web"}, UserProfile: &UserProfile{ActivityLevel: "active", PurchasePower: "low"}}
		if matchesCustomerFilters(dto, map[string]string{"platform": "WEB", "activity_level": "ACTIVE", "purchase_power": "LOW"}) {
			matched[id] = dto
		}
	}
	if len(matched) != 3 || matchesCustomerFilters(matched["a"], map[string]string{"activity_level": "silent"}) {
		t.Fatal("画像匹配必须保留原先各维度交集语义")
	}
	for _, tc := range []struct {
		page int
		want string
	}{{1, "a"}, {2, "m"}, {3, "z"}, {4, ""}} {
		rows, total := paginateCustomerResults(matched, tc.page, 1)
		if total != 3 {
			t.Fatalf("第%d页total=%d，期望3", tc.page, total)
		}
		if tc.want == "" {
			if len(rows) != 0 {
				t.Fatalf("越界页不应有客户：%v", rows)
			}
		} else if len(rows) != 1 || rows[tc.want] != matched[tc.want] {
			t.Fatalf("第%d页应为%s，实际%v", tc.page, tc.want, rows)
		}
	}
}

// TestCustomerListServiceIntegration 按客户聚合完整会话，画像筛选总数不能降为本页数量。
func TestCustomerListServiceIntegration(t *testing.T) {
	database := setupCustomer360TestDB(t)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	svc := setupCustomer360Service(t, database)
	ctx := tenant.WithTenantID(context.Background(), 71)
	base := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	for _, user := range []string{"a", "b", "c"} {
		for i := 0; i < 25; i++ {
			name := "普通客户"
			if i == 24 {
				name = "独有搜索目标"
			}
			if err := database.WithContext(ctx).Create(&model.CustomerSession{UserID: user, SessionID: fmt.Sprintf("%s-%d", user, i), UserName: name, AgentID: 9, Platform: model.PlatformDouyin, CreatedAt: base.Add(time.Duration(i) * time.Minute)}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	scope := SessionScope{Role: "staff", UserID: 9}
	for _, filters := range []map[string]string{{"keyword": "独有搜索目标"}, {"platform": "douyin", "activity_level": "active", "purchase_power": "low"}} {
		for page := 1; page <= 4; page++ {
			rows, total, err := svc.GetCustomerList(ctx, page, 1, filters, scope)
			if err != nil || total != 3 {
				t.Fatalf("筛选%v第%d页total应为3，实际%d/%v", filters, page, total, err)
			}
			if page == 4 {
				if len(rows) != 0 {
					t.Fatalf("越界页应为空：%v", rows)
				}
				continue
			}
			id := string(rune('a' + page - 1))
			dto := rows[id]
			if len(rows) != 1 || dto == nil || dto.SessionStats.TotalSessions != 25 || len(dto.SessionHistory) != 25 {
				t.Fatalf("第%d页应仅客户%s且聚合25会话，实际%v", page, id, rows)
			}
		}
	}
}

func setupCustomer360TestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.Clue{},
		&model.Order{},
		&model.Customer{},
	)
}

func setupCustomer360Service(t *testing.T, db *gorm.DB) *Customer360Service {
	return &Customer360Service{
		sessionRepo:    repository.NewCustomerSessionRepositoryWithDB(db),
		messageRepo:    repository.NewSessionMessageRepositoryWithDB(db),
		clueRepo:       repository.NewClueRepositoryWithDB(db),
		orderRepo:      repository.NewOrderRepositoryWithDB(db),
		unifiedMsgRepo: repository.NewUnifiedMessageRepositoryWithDB(db),
		customerRepo:   repository.NewCustomerRepository(),
		eventRepo:      repository.NewCustomerEventRepository(),
	}
}

// TestCustomer360Service_GetByCustomerID_ClueOrderByIdentity 回归：by-ID 路径线索/订单不再恒空。
// 历史缺陷：GetCustomer360ByCustomerID 把 cust.UnifiedID（盐化哈希，如 "phone:<64hex>"）
// 传给 buildClueInfo/buildOrderInfo，要求 session.UserID == unified_id 匹配——
// 盐化哈希恒不等于平台会话 user_id，导致 360 页签线索/订单永远空白。
// 修复后按客户档案 phone/email + 会话 account 聚合，无需该匹配成立。
func TestCustomer360Service_GetByCustomerID_ClueOrderByIdentity(t *testing.T) {
	db := setupCustomer360TestDB(t)
	service := setupCustomer360Service(t, db)

	prev := dbUtil.GetDB()
	dbUtil.SetTestDB(db)
	defer dbUtil.SetTestDB(prev)

	cust := &model.Customer{
		ID:        "cust-360-001",
		Name:      "张三",
		Phone:     "13800138000",
		Email:     "z@example.com",
		UnifiedID: "phone:" + string(make([]byte, 0)) + "abc123def456abc123def456abc123def456abc123def456abc123def45612",
	}
	if err := db.Create(cust).Error; err != nil {
		t.Fatalf("seed customer 失败: %v", err)
	}

	sess := &model.CustomerSession{
		SessionID: "sess-360-001",
		UserID:    "tg-user-777",
		OneID:     cust.UnifiedID,
		AccountID: "account-360",
		Platform:  model.PlatformDouyin,
		Status:    model.SessionStatusResolved,
	}
	if err := db.Create(sess).Error; err != nil {
		t.Fatalf("seed session 失败: %v", err)
	}

	clue := &model.Clue{ID: "clue-360-001", Account: "account-360", Name: "线索甲", IsVerify: 1}
	if err := db.Create(clue).Error; err != nil {
		t.Fatalf("seed clue 失败: %v", err)
	}
	order := &model.Order{AccountID: "account-360", Price: "99.90", Status: _type.OrderStatusSuccess}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("seed order 失败: %v", err)
	}

	dto, err := service.GetCustomer360ByCustomerID(context.Background(), cust.ID)
	if err != nil {
		t.Fatalf("GetCustomer360ByCustomerID 失败: %v", err)
	}
	if dto == nil {
		t.Fatal("dto 为 nil")
	}
	if dto.BasicInfo == nil || dto.BasicInfo.UserID != cust.ID {
		t.Fatalf("BasicInfo 应基于客户档案构建, got %+v", dto.BasicInfo)
	}
	if dto.ClueInfo == nil {
		t.Error("ClueInfo 不应为空（回归：session.UserID==unified_id 恒不匹配导致线索恒空）")
	} else if dto.ClueInfo.ClueID != clue.ID {
		t.Errorf("ClueInfo.ClueID = %q, want %q", dto.ClueInfo.ClueID, clue.ID)
	}
	if dto.OrderInfo == nil || len(dto.OrderInfo.Orders) == 0 {
		t.Error("OrderInfo 不应为空（回归：订单恒空）")
	} else {
		if dto.OrderInfo.TotalOrders != 1 {
			t.Errorf("TotalOrders = %d, want 1", dto.OrderInfo.TotalOrders)
		}
		if dto.OrderInfo.Orders[0].OrderID != order.ID {
			t.Errorf("OrderID = %q, want %q", dto.OrderInfo.Orders[0].OrderID, order.ID)
		}
	}
}

// TestCustomer360Service_GetByCustomerID_NoSessions_ReturnsProfile 回归：无会话客户仍返回档案+身份聚合结果。
func TestCustomer360Service_GetByCustomerID_NoSessions_ReturnsProfile(t *testing.T) {
	db := setupCustomer360TestDB(t)
	service := setupCustomer360Service(t, db)

	prev := dbUtil.GetDB()
	dbUtil.SetTestDB(db)
	defer dbUtil.SetTestDB(prev)

	cust := &model.Customer{
		ID:        "cust-360-no-sess",
		Name:      "李四",
		Phone:     "13900139000",
		UnifiedID: "phone:def456def456def456def456def456def456def456def456def456def45612",
	}
	if err := db.Create(cust).Error; err != nil {
		t.Fatalf("seed customer 失败: %v", err)
	}

	clue := &model.Clue{ID: "clue-phone-001", Account: "13900139000"}
	if err := db.Create(clue).Error; err != nil {
		t.Fatalf("seed clue 失败: %v", err)
	}

	dto, err := service.GetCustomer360ByCustomerID(context.Background(), cust.ID)
	if err != nil {
		t.Fatalf("无会话客户不应报错: %v", err)
	}
	if dto.BasicInfo == nil {
		t.Fatal("应返回基本档案而非 nil")
	}
	if dto.ClueInfo == nil {
		t.Error("有手机号+匹配线索时 ClueInfo 不应为空")
	}
}

// TestCustomer360Service_buildClueInfo 测试构建线索信息
func TestCustomer360Service_buildClueInfo(t *testing.T) {
	db := setupCustomer360TestDB(t)
	service := setupCustomer360Service(t, db)

	session := &model.CustomerSession{
		SessionID: "session-123",
		UserID:    "user-123",
		UserPhone: "13800138000",
		AccountID: "account-123",
	}
	db.Create(session)

	clue := &model.Clue{
		Account:    "13800138000",
		Type:       1,
		Name:       "Test User",
		City:       "Beijing",
		IsVerify:   1,
		CreateTime: 1700000000,
	}
	db.Create(clue)

	tests := []struct {
		name       string
		merchantID string
		userID     string
		wantErr    bool
		wantNil    bool
	}{
		{
			name: "get clue info success",

			userID:  "user-123",
			wantErr: false,
			wantNil: false,
		},
		{
			name: "user not found",

			userID:  "user-999",
			wantErr: false,
			wantNil: true,
		},
		{
			name: "no matching clue",

			userID:  "user-456",
			wantErr: false,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "no matching clue" {
				session2 := &model.CustomerSession{
					SessionID: "session-456",
					UserID:    tt.userID,
					UserPhone: "99999999999",
					AccountID: "account-456",
				}
				db.Create(session2)
			}

			userSessions, _ := service.sessionRepo.GetByUserID(context.Background(), tt.userID)
			result, err := service.buildClueInfo(context.Background(), tt.userID, userSessions)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildClueInfo() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantNil && result != nil {
				t.Errorf("Expected nil result, got %v", result)
			}

			if !tt.wantNil && result == nil {
				t.Errorf("Expected non-nil result")
			}

			if !tt.wantNil && !tt.wantErr {
				if result.Name != "Test User" {
					t.Errorf("Expected Name 'Test User', got '%s'", result.Name)
				}
				if result.Status != "qualified" {
					t.Errorf("Expected Status 'qualified', got '%s'", result.Status)
				}
			}
		})
	}
}

// TestCustomer360Service_buildOrderInfo 测试构建订单信息
func TestCustomer360Service_buildOrderInfo(t *testing.T) {
	db := setupCustomer360TestDB(t)
	service := setupCustomer360Service(t, db)

	session := &model.CustomerSession{
		SessionID: "session-123",
		UserID:    "user-123",
		AccountID: "account-123",
	}
	db.Create(session)

	order1 := &model.Order{
		ID:        "order-1",
		AccountID: "account-123",
		Price:     "100.00",
		Status:    1,
		TgID:      12345,
	}
	db.Create(order1)

	time.Sleep(10 * time.Millisecond)

	order2 := &model.Order{
		ID:        "order-2",
		AccountID: "account-123",
		Price:     "200.50",
		Status:    0,
		TgID:      12345,
	}
	db.Create(order2)

	db.Create(&model.Order{
		ID:        "order-3",
		AccountID: "account-456",
		Price:     "300.00",
		Status:    1,
		TgID:      67890,
	})

	tests := []struct {
		name       string
		merchantID string
		userID     string
		wantErr    bool
		wantTotal  int64
		wantAmount float64
	}{
		{
			name: "get order info success",

			userID:     "user-123",
			wantErr:    false,
			wantTotal:  2,
			wantAmount: 300.50,
		},
		{
			name: "user not found",

			userID:     "user-999",
			wantErr:    false,
			wantTotal:  0,
			wantAmount: 0,
		},
		{
			name: "no orders",

			userID:     "user-456",
			wantErr:    false,
			wantTotal:  0,
			wantAmount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userSessions, _ := service.sessionRepo.GetByUserID(context.Background(), tt.userID)
			result, err := service.buildOrderInfo(context.Background(), tt.userID, userSessions)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildOrderInfo() error = %v, wantErr %v", err, tt.wantErr)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if result.TotalOrders != tt.wantTotal {
				t.Errorf("Expected TotalOrders %d, got %d", tt.wantTotal, result.TotalOrders)
			}

			if abs(result.TotalAmount-tt.wantAmount) > 0.01 {
				t.Errorf("Expected TotalAmount %.2f, got %.2f", tt.wantAmount, result.TotalAmount)
			}

			if tt.wantTotal > 0 {
				if result.LastOrderID == "" {
					t.Error("Expected LastOrderID to be set")
				}
				if result.LastOrderAt == "" {
					t.Error("Expected LastOrderAt to be set")
				}
			}
		})
	}
}

// TestCustomer360Service_GetCustomer360 测试获取客户 360 视图
func TestCustomer360Service_GetCustomer360(t *testing.T) {
	db := setupCustomer360TestDB(t)
	service := setupCustomer360Service(t, db)

	session := &model.CustomerSession{
		SessionID: "session-123",
		UserID:    "user-123",
		UserName:  "Test User",
		UserPhone: "13800138000",
		AccountID: "account-123",
		Platform:  model.PlatformDouyin,
		Status:    model.SessionStatusResolved,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("创建测试会话失败：%v", err)
	}

	tests := []struct {
		name       string
		merchantID string
		userID     string
		wantErr    bool
		wantNil    bool
	}{
		{
			name: "get customer 360 success",

			userID:  "user-123",
			wantErr: false,
			wantNil: false,
		},
		{
			name: "customer not found",

			userID:  "user-999",
			wantErr: true,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetCustomer360(context.Background(), tt.userID, SessionScope{Role: "admin"})

			if (err != nil) != tt.wantErr {
				t.Errorf("GetCustomer360(%q) error = %v, wantErr %v", tt.userID, err, tt.wantErr)
				return
			}

			if (result == nil) != tt.wantNil {
				t.Errorf("GetCustomer360(%q) result == nil: %v, wantNil %v", tt.userID, result == nil, tt.wantNil)
				return
			}

			if !tt.wantNil && result != nil {
				if result.BasicInfo == nil {
					t.Error("Expected BasicInfo to be non-nil")
				} else {
					if result.BasicInfo.UserID != tt.userID {
						t.Errorf("BasicInfo.UserID = %q, want %q", result.BasicInfo.UserID, tt.userID)
					}
					if result.BasicInfo.UserName == "" {
						t.Error("Expected BasicInfo.UserName to be non-empty")
					}
				}

				if result.SessionStats == nil {
					t.Error("Expected SessionStats to be non-nil")
				} else if result.SessionStats.TotalSessions < 1 {
					t.Errorf("SessionStats.TotalSessions = %d, want >= 1", result.SessionStats.TotalSessions)
				}

				if len(result.SessionHistory) == 0 {
					t.Error("Expected SessionHistory to be non-empty")
				}

				if result.UserProfile == nil {
					t.Error("Expected UserProfile to be non-nil")
				}
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestCustomer360Service_orderStatusToString 测试订单状态转换
func TestCustomer360Service_orderStatusToString(t *testing.T) {
	tests := []struct {
		name   string
		status _type.OrderStatusType
		want   string
	}{
		{"pending status", 0, "pending"},
		{"success status", 1, "success"},
		{"force success status", 2, "success"},
		{"timeout status", -1, "timeout"},
		{"force close status", -2, "closed"},
		{"unknown status", 999, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := orderStatusToString(tt.status)
			if result != tt.want {
				t.Errorf("orderStatusToString(%d) = %s, want %s", tt.status, result, tt.want)
			}
		})
	}
}
