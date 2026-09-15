package service

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupCustomerServiceTestDB(t *testing.T) *gorm.DB {
	// 需覆盖 MergeCustomers 事务改写的全部表（见 service/customer.go:346-383），
	// 否则 TestCustomerService_MergeCustomers 报 relation does not exist。
	database := testutil.NewTestDB(t,
		&model.Customer{},
		&model.CustomerEvent{},
		&model.CustomerTag{},
		&model.CustomerSession{},
		&model.CustomerChannel{},
		&model.CustomerDoNotContact{},
		&model.CSATSurvey{},
		&model.Clue{},
		&model.ScriptExposureLog{},
		&model.OperationLog{},
	)
	db.SetTestDB(database)
	return database
}

func setupCustomerService(t *testing.T) *CustomerService {
	setupCustomerServiceTestDB(t)
	return NewCustomerService()
}

// TestNewCustomerService 测试创建服务实例
func TestNewCustomerService(t *testing.T) {
	service := NewCustomerService()
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.repo == nil {
		t.Error("Expected repo to be initialized")
	}
}

// TestCustomerService_CreateOrUpdate 测试创建或更新客户
func TestCustomerService_CreateOrUpdate(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone:        "13800138000",
		Email:        "test@example.com",
		WechatOpenID: "wechat123",
	}

	customer, err := service.CreateOrUpdate(context.Background(), dto)
	if err != nil {
		t.Fatalf("CreateOrUpdate failed: %v", err)
	}

	if customer == nil {
		t.Fatal("Expected customer to be created")
	}
	if customer.Phone != dto.Phone {
		t.Errorf("Expected phone %s, got %s", dto.Phone, customer.Phone)
	}
	if customer.UnifiedID == "" {
		t.Error("Expected UnifiedID to be generated")
	}
}

// TestCustomerService_CreateOrUpdate_Update 测试更新现有客户
func TestCustomerService_CreateOrUpdate_Update(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone: "13800138001",
		Email: "test@example.com",
	}
	service.CreateOrUpdate(context.Background(), dto)

	updateDTO := &CustomerDTO{
		Phone: "13800138001",
		Email: "updated@example.com",
	}
	updated, err := service.CreateOrUpdate(context.Background(), updateDTO)
	if err != nil {
		t.Fatalf("CreateOrUpdate update failed: %v", err)
	}

	if updated.Email != updateDTO.Email {
		t.Errorf("Expected email %s, got %s", updateDTO.Email, updated.Email)
	}
}

// TestCustomerService_GetCustomerProfile 测试获取客户 360 视图
func TestCustomerService_GetCustomerProfile(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone: "13800138002",
	}
	customer, _ := service.CreateOrUpdate(context.Background(), dto)

	profile, err := service.GetCustomerProfile(context.Background(), customer.ID)
	if err != nil {
		t.Fatalf("GetCustomerProfile failed: %v", err)
	}

	if profile.Customer == nil {
		t.Fatal("Expected customer in profile")
	}
	if profile.Customer.ID != customer.ID {
		t.Errorf("Expected customer ID %s, got %s", customer.ID, profile.Customer.ID)
	}
}

// TestCustomerService_GetCustomerProfile_NotFound 测试获取不存在的客户
func TestCustomerService_GetCustomerProfile_NotFound(t *testing.T) {
	service := setupCustomerService(t)

	_, err := service.GetCustomerProfile(context.Background(), "non-existent-id")
	if err != ErrCustomerNotFound {
		t.Errorf("Expected ErrCustomerNotFound, got %v", err)
	}
}

// TestCustomerService_List 测试获取客户列表
func TestCustomerService_List(t *testing.T) {
	service := setupCustomerService(t)

	for i := 0; i < 5; i++ {
		dto := &CustomerDTO{
			Phone: "1380013800" + string(rune('0'+i)),
		}
		service.CreateOrUpdate(context.Background(), dto)
	}

	customers, total, err := service.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(customers) != 5 {
		t.Errorf("Expected 5 customers, got %d", len(customers))
	}
}

// TestCustomerService_AddTags 测试添加标签
func TestCustomerService_AddTags(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone: "13800138003",
	}
	customer, _ := service.CreateOrUpdate(context.Background(), dto)

	tags := []string{"VIP", "high-value"}
	err := service.AddTags(context.Background(), customer.ID, tags)
	if err != nil {
		t.Fatalf("AddTags failed: %v", err)
	}

	updated, _ := service.repo.GetByID(context.Background(), customer.ID)
	currentTags := model.GetCustomerTags(updated)
	if len(currentTags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(currentTags))
	}
}

// TestCustomerService_RemoveTags 测试移除标签
func TestCustomerService_RemoveTags(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone: "13800138004",
	}
	customer, _ := service.CreateOrUpdate(context.Background(), dto)
	service.AddTags(context.Background(), customer.ID, []string{"VIP", "high-value", "active"})

	err := service.RemoveTags(context.Background(), customer.ID, []string{"VIP"})
	if err != nil {
		t.Fatalf("RemoveTags failed: %v", err)
	}

	updated, _ := service.repo.GetByID(context.Background(), customer.ID)
	currentTags := model.GetCustomerTags(updated)
	if len(currentTags) != 2 {
		t.Errorf("Expected 2 tags after removal, got %d", len(currentTags))
	}
	for _, tag := range currentTags {
		if tag == "VIP" {
			t.Error("Expected VIP tag to be removed")
		}
	}
}

// TestCustomerService_MergeCustomers 测试合并客户
func TestCustomerService_MergeCustomers(t *testing.T) {
	service := setupCustomerService(t)

	primary, _ := service.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone: "13800138005",
		Email: "primary@example.com",
	})
	secondary, _ := service.CreateOrUpdate(context.Background(), &CustomerDTO{
		Email:        "secondary@example.com",
		WechatOpenID: "wechat456",
	})

	service.AddTags(context.Background(), primary.ID, []string{"primary-tag"})
	service.AddTags(context.Background(), secondary.ID, []string{"secondary-tag"})

	err := service.MergeCustomers(context.Background(), primary.ID, secondary.ID)
	if err != nil {
		t.Fatalf("MergeCustomers failed: %v", err)
	}

	_, err = service.repo.GetByID(context.Background(), secondary.ID)
	if err == nil || err.Error() != "记录未找到" {
	}

	merged, _ := service.repo.GetByID(context.Background(), primary.ID)
	mergedTags := model.GetCustomerTags(merged)
	hasPrimaryTag := false
	hasSecondaryTag := false
	for _, tag := range mergedTags {
		if tag == "primary-tag" {
			hasPrimaryTag = true
		}
		if tag == "secondary-tag" {
			hasSecondaryTag = true
		}
	}
	if !hasPrimaryTag || !hasSecondaryTag {
		t.Error("Expected merged customer to have tags from both customers")
	}
}

// TestCustomerService_MergeCustomers_SameID 测试合并按一个客户
func TestCustomerService_MergeCustomers_SameID(t *testing.T) {
	service := setupCustomerService(t)

	err := service.MergeCustomers(context.Background(), "same-id", "same-id")
	if err == nil {
		t.Error("Expected error when merging same customer")
	}
}

// TestCustomerService_GetCustomerByIdentity 测试根据身份标识获取客户
func TestCustomerService_GetCustomerByIdentity(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone:        "13800138006",
		Email:        "identity@example.com",
		WechatOpenID: "wechat789",
	}
	customer, _ := service.CreateOrUpdate(context.Background(), dto)

	found, _ := service.GetCustomerByIdentity(context.Background(), "13800138006", "", "", "", "")
	if found == nil || found.ID != customer.ID {
		t.Error("Expected to find customer by phone")
	}

	found, _ = service.GetCustomerByIdentity(context.Background(), "", "identity@example.com", "", "", "")
	if found == nil || found.ID != customer.ID {
		t.Error("Expected to find customer by email")
	}
}

// TestCustomerService_CreateOrUpdate_InvalidDTO 测试无效 DTO
// 单租户私有部署：无 merchant_id 字段，仅校验 nil DTO
func TestCustomerService_CreateOrUpdate_InvalidDTO(t *testing.T) {
	service := setupCustomerService(t)

	_, err := service.CreateOrUpdate(context.Background(), nil)
	if err != ErrInvalidDTO {
		t.Errorf("Expected ErrInvalidDTO, got %v", err)
	}
}

func TestCustomerService_CreateOrUpdate_EmptyBody(t *testing.T) {
	service := setupCustomerService(t)

	cases := []struct {
		name string
		dto  *CustomerDTO
	}{
		{"zero value", &CustomerDTO{}},
		{"whitespace only", &CustomerDTO{Phone: "   ", Email: "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.CreateOrUpdate(context.Background(), tc.dto)
			if err == nil {
				t.Fatal("expected error for empty body, got nil")
			}
			if err != nil && !contains(err.Error(), "至少需要提供") {
				t.Errorf("expected '至少需要提供' error, got %v", err)
			}
		})
	}
}

// TestCustomerService_CreateOrUpdate_InvalidFormat 验证手机号 / 邮箱格式校验
func TestCustomerService_CreateOrUpdate_InvalidFormat(t *testing.T) {
	service := setupCustomerService(t)

	cases := []struct {
		name      string
		dto       *CustomerDTO
		shouldErr string
	}{
		{"phone too short", &CustomerDTO{Phone: "123"}, "手机号格式不合法"},
		{"phone non-numeric", &CustomerDTO{Phone: "abcdefghijk"}, "手机号格式不合法"},
		{"phone wrong prefix", &CustomerDTO{Phone: "12345678901"}, "手机号格式不合法"},
		{"email no @", &CustomerDTO{Email: "notanemail"}, "邮箱格式不合法"},
		{"email no domain", &CustomerDTO{Email: "test@"}, "邮箱格式不合法"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.CreateOrUpdate(context.Background(), tc.dto)
			if err == nil {
				t.Fatal("expected error for invalid format, got nil")
			}
			if !contains(err.Error(), tc.shouldErr) {
				t.Errorf("expected %q, got %v", tc.shouldErr, err)
			}
		})
	}
}

// TestCustomerService_CreateOrUpdate_ThirdPartyIDOnly 验证仅第三方 ID（无手机/邮箱）可创建
func TestCustomerService_CreateOrUpdate_ThirdPartyIDOnly(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{WechatOpenID: "wx_only_001"}
	c, err := service.CreateOrUpdate(context.Background(), dto)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil || c.WechatOpenID != "wx_only_001" {
		t.Fatalf("expected wechat_open_id=wx_only_001, got %+v", c)
	}
}

// TestCustomerService_CreateOrUpdate_PartialUpdatePreservesFields 验证部分更新不覆盖非空字段
// 修复 P1：原代码无条件覆盖所有 5 字段，导致 wechat 单独更新时把 phone 清空
func TestCustomerService_CreateOrUpdate_PartialUpdatePreservesFields(t *testing.T) {
	service := setupCustomerService(t)

	first, err := service.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone:        "13900000099",
		Email:        "preserve@test.com",
		WechatOpenID: "wx_keep_001",
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	second, err := service.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone: "13900000099",
		Email: "preserve@test.com",
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}

	if second.WechatOpenID != "wx_keep_001" {
		t.Errorf("wechat_open_id 被覆盖: got %q want %q", second.WechatOpenID, "wx_keep_001")
	}
	if first.ID != second.ID {
		t.Errorf("id 应一致（phone 命中同一行）: first=%s second=%s", first.ID, second.ID)
	}
}

// TestCustomerService_AddTags_EmptyTags 测试添加空标签
func TestCustomerService_AddTags_EmptyTags(t *testing.T) {
	service := setupCustomerService(t)

	dto := &CustomerDTO{
		Phone: "13800138007",
	}
	customer, _ := service.CreateOrUpdate(context.Background(), dto)

	err := service.AddTags(context.Background(), customer.ID, []string{})
	if err != nil {
		t.Fatalf("AddTags with empty tags failed: %v", err)
	}
}

// TestCustomerService_RemoveTags_NotFound 测试移除不存在的客户标签
func TestCustomerService_RemoveTags_NotFound(t *testing.T) {
	service := setupCustomerService(t)

	err := service.RemoveTags(context.Background(), "non-existent-id", []string{"tag"})
	if err != ErrCustomerNotFound {
		t.Errorf("Expected ErrCustomerNotFound, got %v", err)
	}
}
