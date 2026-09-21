package email

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupEmailSmtpServiceTestDB(t *testing.T) *gorm.DB {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	database := testutil.NewTestDB(t,
		&model.EmailSmtp{},
		&model.EmailList{},
		&model.EmailJobs{},
		&model.Clue{},
		&model.SystemConfig{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewEmailSmtpService 测试创建邮件 SMTP 服务
func TestNewEmailSmtpService(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)

	service := NewEmailSmtpService()
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

func TestEmailSmtpService_CreateEmailSmtp(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		Name:     "test@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "test@qq.com",
		Password: "test-password",
		Limit:    100,
	}

	created, err := service.CreateEmailSmtp(context.Background(), emailSmtp)
	if err != nil {
		t.Fatalf("CreateEmailSmtp failed: %v", err)
	}

	if created.Name != "test@qq.com" {
		t.Errorf("Expected name 'test@qq.com', got %s", created.Name)
	}

	if created.Limit != 100 {
		t.Errorf("Expected limit 100, got %d", created.Limit)
	}

	var stored model.EmailSmtp
	database.Where("name = ?", "test@qq.com").First(&stored)
	if stored.Password == "test-password" {
		t.Errorf("密码明文落库! R50 fail-closed 未生效")
	}
	if stored.Password == "" {
		t.Errorf("密码为空, 加密链路异常")
	}

	var count int64
	database.Model(&model.EmailSmtp{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 SMTP record, got %d", count)
	}
}

// TestEmailSmtpService_GetEmailSmtp 测试根据 ID 获取 SMTP 配置
func TestEmailSmtpService_GetEmailSmtp(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "test-id-123",
		Name:     "test@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "test@qq.com",
		Password: "test-password",
		Limit:    100,
	}
	database.Create(&emailSmtp)

	retrieved, err := service.GetEmailSmtp(context.Background(), emailSmtp.ID)
	if err != nil {
		t.Fatalf("GetEmailSmtp failed: %v", err)
	}

	if retrieved.Name != "test@qq.com" {
		t.Errorf("Expected name 'test@qq.com', got %s", retrieved.Name)
	}

	if retrieved.Server != "smtp.qq.com" {
		t.Errorf("Expected server 'smtp.qq.com', got %s", retrieved.Server)
	}
}

// TestEmailSmtpService_GetEmailSmtp_NotFound 测试获取不存在的 SMTP 配置
func TestEmailSmtpService_GetEmailSmtp_NotFound(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	_, err := service.GetEmailSmtp(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent SMTP")
	}
}

// TestEmailSmtpService_GetEmailSmtpList 测试获取 SMTP 配置列表
func TestEmailSmtpService_GetEmailSmtpList(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	for i := 0; i < 3; i++ {
		emailSmtp := model.EmailSmtp{
			Name:     "test" + string(rune('0'+i)) + "@qq.com",
			Server:   "smtp.qq.com",
			Port:     465,
			Username: "test" + string(rune('0'+i)) + "@qq.com",
			Password: "password" + string(rune('0'+i)),
			Limit:    100,
		}
		database.Create(&emailSmtp)
	}

	list, err := service.GetEmailSmtpList(context.Background())
	if err != nil {
		t.Fatalf("GetEmailSmtpList failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 SMTP records, got %d", len(list))
	}
}

// TestEmailSmtpService_GetEmailSmtpList_Empty 测试获取空的 SMTP 配置列表
func TestEmailSmtpService_GetEmailSmtpList_Empty(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	list, err := service.GetEmailSmtpList(context.Background())
	if err != nil {
		t.Fatalf("GetEmailSmtpList failed: %v", err)
	}

	if len(list) != 0 {
		t.Errorf("Expected 0 SMTP records, got %d", len(list))
	}
}

// TestEmailSmtpService_UpdateEmailSmtp 测试更新 SMTP 配置
func TestEmailSmtpService_UpdateEmailSmtp(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "test-update-id",
		Name:     "old@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "old@qq.com",
		Password: "old-password",
		Limit:    100,
	}
	database.Create(&emailSmtp)

	emailSmtp.Name = "new@qq.com"
	emailSmtp.Limit = 200
	err := service.UpdateEmailSmtp(context.Background(), emailSmtp)
	if err != nil {
		t.Fatalf("UpdateEmailSmtp failed: %v", err)
	}

	var updated model.EmailSmtp
	database.Where("id = ?", emailSmtp.ID).First(&updated)
	if updated.Name != "new@qq.com" {
		t.Errorf("Expected name 'new@qq.com', got %s", updated.Name)
	}
	if updated.Limit != 200 {
		t.Errorf("Expected limit 200, got %d", updated.Limit)
	}

	if updated.Password == "old-password" {
		t.Errorf("更新后密码明文落库! R50 fail-closed 未生效")
	}
}

// TestEmailSmtpService_DeleteEmailSmtp 测试删除 SMTP 配置
func TestEmailSmtpService_DeleteEmailSmtp(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "test-delete-id",
		Name:     "delete@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "delete@qq.com",
		Password: "password",
		Limit:    100,
	}
	database.Create(&emailSmtp)

	err := service.DeleteEmailSmtp(context.Background(), emailSmtp.ID)
	if err != nil {
		t.Fatalf("DeleteEmailSmtp failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailSmtp{}).Where("id = ?", emailSmtp.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected SMTP to be deleted, got count %d", count)
	}
}

// TestEmailSmtpService_GetRandEmailSmtp 测试获取随机 SMTP 配置
func TestEmailSmtpService_GetRandEmailSmtp(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "test-rand-id",
		Name:     "rand@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "rand@qq.com",
		Password: "password",
		Limit:    100,
	}
	database.Create(&emailSmtp)

	retrieved, err := service.GetRandEmailSmtp(context.Background())
	if err != nil {
		t.Fatalf("GetRandEmailSmtp failed: %v", err)
	}

	if retrieved.Name != "rand@qq.com" {
		t.Errorf("Expected name 'rand@qq.com', got %s", retrieved.Name)
	}
}

// TestEmailSmtpService_GetRandEmailSmtp_EmptyList 测试空列表时获取随机 SMTP 配置
func TestEmailSmtpService_GetRandEmailSmtp_EmptyList(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	_, err := service.GetRandEmailSmtp(context.Background())
	if err == nil {
		t.Error("Expected error for empty SMTP list")
	}
}

// TestEmailSmtpService_GetRandEmailSmtp_NoAvailable 测试没有可用 SMTP 配置
func TestEmailSmtpService_GetRandEmailSmtp_NoAvailable(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "test-no-available-id",
		Name:     "nolimit@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "nolimit@qq.com",
		Password: "password",
		Limit:    0,
	}
	database.Create(&emailSmtp)

	_, err := service.GetRandEmailSmtp(context.Background())
	if err == nil {
		t.Error("Expected error for no available SMTP")
	}
}

// TestEmailSmtpService_GetRandEmailSmtp_WithLimit 测试 SMTP 限制逻辑
func TestEmailSmtpService_GetRandEmailSmtp_WithLimit(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp1 := model.EmailSmtp{
		ID:       "test-limit-1",
		Name:     "limit1@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "limit1@qq.com",
		Password: "password1",
		Limit:    2,
	}
	database.Create(&emailSmtp1)

	emailSmtp2 := model.EmailSmtp{
		ID:       "test-limit-2",
		Name:     "limit2@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "limit2@qq.com",
		Password: "password2",
		Limit:    10,
	}
	database.Create(&emailSmtp2)

	retrieved, err := service.GetRandEmailSmtp(context.Background())
	if err != nil {
		t.Fatalf("GetRandEmailSmtp failed: %v", err)
	}

	if retrieved.Name != "limit1@qq.com" {
		t.Errorf("Expected name 'limit1@qq.com', got %s", retrieved.Name)
	}
}

// TestEmailSmtpService_GetRandEmailSmtpQuotaCountedByAccount 限流额度的记账键必须是登录账号。
//
// email_list.from 落的是发信账号（Username），而额度统计读的是展示名（Name）。两者是运营
// 各填一格的字段，正常就不相等 —— 于是 GetTodayCountByFrom(展示名) 永远数到 0，
// `todayCount < Limit` 恒真，Limit 形同不存在：一台日限 500 的账号可以被拨穿任意多封。
// 这不是"多算几封"的口径问题，营销外发的日上限恰恰是唯一挡着发信域被拉黑的闸门。
//
// 上面几条既有用例把 Name 与 Username 写成同一个值，所以这个键漂移在测试里看不出来 ——
// 本用例刻意让两者不同。
func TestEmailSmtpService_GetRandEmailSmtpQuotaCountedByAccount(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	database.Create(&model.EmailSmtp{
		ID:       "ratelimit-by-account",
		Name:     "运营小号",
		Server:   "smtp.example.com",
		Port:     465,
		Username: "ops@example.com",
		Password: "pwd",
		Limit:    1,
	})
	// 今日已用满的那一封：记账列是 from + send_time，与 cron 发完后写的值同源。
	database.Create(&model.EmailList{
		Subject:   "上一波",
		To:        "lead@example.com",
		From:      "ops@example.com",
		IsSend:    1,
		IsSuccess: 1,
		SendTime:  time.Now(),
	})

	if _, err := service.GetRandEmailSmtp(context.Background()); err == nil {
		t.Fatal("该账号今日已发满 1 封（Limit=1）仍被选中 ⇒ 额度统计读的不是记账用的那个字段")
	}
}

func TestEmailSmtpService_CreateEmailSmtp_EmptyFields(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		Name:     "",
		Server:   "",
		Port:     0,
		Username: "",
		Password: "",
		Limit:    0,
	}

	created, err := service.CreateEmailSmtp(context.Background(), emailSmtp)
	if err != nil {
		t.Fatalf("CreateEmailSmtp failed: %v", err)
	}

	if created == nil {
		t.Error("Expected non-nil created SMTP")
	}
}

// TestEmailSmtpService_UpdateEmailSmtp_NotFound 测试更新不存在的 SMTP 配置
// 注：GORM 的 Save 方法在记录不存在时会插入新记录，所以这个测试主要验证可以插入
func TestEmailSmtpService_UpdateEmailSmtp_NotFound(t *testing.T) {
	database := setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	emailSmtp := model.EmailSmtp{
		ID:       "non-existent-id",
		Name:     "test@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "test@qq.com",
		Password: "password",
		Limit:    100,
	}

	err := service.UpdateEmailSmtp(context.Background(), emailSmtp)
	if err != nil {
		t.Errorf("UpdateEmailSmtp should not fail: %v", err)
	}

	var count int64
	database.Model(&model.EmailSmtp{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 SMTP record (inserted), got %d", count)
	}
}

// TestEmailSmtpService_DeleteEmailSmtp_NotFound 测试删除不存在的 SMTP 配置
func TestEmailSmtpService_DeleteEmailSmtp_NotFound(t *testing.T) {
	setupEmailSmtpServiceTestDB(t)
	service := NewEmailSmtpService()

	err := service.DeleteEmailSmtp(context.Background(), "non-existent-id")
	if err != nil {
		t.Errorf("DeleteEmailSmtp should not fail for non-existent ID: %v", err)
	}
}
