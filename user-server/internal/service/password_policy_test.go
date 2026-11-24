package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/bcrypt"

	"gorm.io/gorm"
)

func setupPasswordPolicyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.PasswordHistory{},
		&model.OperationLog{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewPasswordPolicyService 测试构造函数
func TestNewPasswordPolicyService(t *testing.T) {
	s := NewPasswordPolicyService()
	if s == nil {
		t.Fatal("NewPasswordPolicyService returned nil")
	}
}

// TestValidatePassword_TooShort 测试短密码
func TestValidatePassword_TooShort(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        10,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
	}
	err := s.ValidatePasswordWithPolicy(context.Background(), "Ab1", 0, policy)
	if err == nil {
		t.Fatal("短密码应验证失败")
	}
	if !strings.Contains(err.Error(), "长度") {
		t.Errorf("错误信息应提到长度: %v", err)
	}
}

// TestValidatePassword_TooLong 测试超长密码
func TestValidatePassword_TooLong(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        8,
		MaxLength:        16,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
	}
	longPwd := "Abc1234567890ABCDEFG"
	err := s.ValidatePasswordWithPolicy(context.Background(), longPwd, 0, policy)
	if err == nil {
		t.Fatal("超长密码应验证失败")
	}
	if !strings.Contains(err.Error(), "长度") {
		t.Errorf("错误信息应提到长度: %v", err)
	}
}

// TestValidatePassword_Empty 测试空密码
func TestValidatePassword_Empty(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &DefaultPasswordPolicy
	err := s.ValidatePasswordWithPolicy(context.Background(), "", 0, policy)
	if err == nil {
		t.Fatal("空密码应验证失败")
	}
}

// TestValidatePassword_MissingCharacterClasses 测试缺失字符类型
func TestValidatePassword_MissingCharacterClasses(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        8,
		MaxLength:        64,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
		RequireSpecial:   false,
		ForbidCommon:     false,
		ForbidReuse:      false,
	}

	cases := []struct {
		name string
		pwd  string
		want string
	}{
		{"缺大写", "abcdefg1", "大写字母"},
		{"缺小写", "ABCDEFG1", "小写字母"},
		{"缺数字", "Abcdefgh", "数字"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.ValidatePasswordWithPolicy(context.Background(), c.pwd, 0, policy)
			if err == nil {
				t.Fatalf("密码 %q 应验证失败（缺 %s）", c.pwd, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误信息应包含 %q: %v", c.want, err)
			}
		})
	}
}

// TestValidatePassword_RequireSpecial 测试需要特殊字符
func TestValidatePassword_RequireSpecial(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        8,
		MaxLength:        64,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
		RequireSpecial:   true,
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdefg1", 0, policy); err == nil {
		t.Error("缺特殊字符应失败")
	}
	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdefg1@", 0, policy); err != nil {
		t.Errorf("含特殊字符应通过: %v", err)
	}
}

// TestValidatePassword_ForbidCommon 测试弱密码拒绝
func TestValidatePassword_ForbidCommon(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        6,
		MaxLength:        64,
		RequireUppercase: false,
		RequireLowercase: false,
		RequireDigit:     false,
		ForbidCommon:     true,
		CommonPasswords:  []string{"123456", "password", "admin"},
	}

	cases := []string{
		"123456",
		"12345678",
		"password",
		"admin123",
		"AdminPASSWORD",
	}

	for _, pwd := range cases {
		t.Run(pwd, func(t *testing.T) {
			err := s.ValidatePasswordWithPolicy(context.Background(), pwd, 0, policy)
			if err == nil {
				t.Errorf("弱密码 %q 应被拒绝", pwd)
			}
		})
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Xkp9!aB#", 0, policy); err != nil {
		t.Errorf("强密码应通过: %v", err)
	}
}

// TestValidatePassword_ForbidReuse 测试历史密码重复拒绝
func TestValidatePassword_ForbidReuse(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        8,
		MaxLength:        64,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
		ForbidReuse:      true,
		ReuseCount:       5,
	}

	user := &model.SystemUser{
		Username: "reuse_user",
		Password: "Old" + "Pass" + "word123",
		Email:    "reuse@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	oldPwd := "OldPassword123"
	hashed, _ := bcrypt.HashPassword(oldPwd)
	history := &model.PasswordHistory{
		UserID:       user.ID,
		PasswordHash: hashed,
		ChangedAt:    time.Now().Add(-24 * time.Hour),
		Source:       model.PasswordSourceChangePassword,
	}
	if err := database.Create(history).Error; err != nil {
		t.Fatalf("写入密码历史失败: %v", err)
	}

	err := s.ValidatePasswordWithPolicy(context.Background(), oldPwd, user.ID, policy)
	if err == nil {
		t.Fatal("历史密码应被拒绝")
	}
	if !strings.Contains(err.Error(), "历史") {
		t.Errorf("错误信息应提到历史: %v", err)
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "BrandNewP@ss1", user.ID, policy); err != nil {
		t.Errorf("新密码应通过: %v", err)
	}
}

// TestValidatePassword_Boundary 测试边界值
func TestValidatePassword_Boundary(t *testing.T) {
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{
		MinLength:        8,
		MaxLength:        16,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
		ForbidCommon:     false,
		ForbidReuse:      false,
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdefg1", 0, policy); err != nil {
		t.Errorf("8 位密码应通过: %v", err)
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdefg123456789", 0, policy); err != nil {
		t.Errorf("15 位密码应通过: %v", err)
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdef1", 0, policy); err == nil {
		t.Error("7 位密码应失败")
	}

	if err := s.ValidatePasswordWithPolicy(context.Background(), "Abcdefg12345678901", 0, policy); err == nil {
		t.Error("17 位密码应失败")
	}
}

// TestRecordPasswordHistory 测试密码历史记录
func TestRecordPasswordHistory(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()

	user := &model.SystemUser{
		Username: "history_user",
		Password: "Passw" + "ord123",
		Email:    "history@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	if err := s.RecordPasswordHistory(context.Background(), user.ID, "NewPassword123", model.PasswordSourceChangePassword); err != nil {
		t.Fatalf("RecordPasswordHistory 失败: %v", err)
	}

	var histories []model.PasswordHistory
	if err := database.Where("user_id = ?", user.ID).Find(&histories).Error; err != nil {
		t.Fatalf("查询历史失败: %v", err)
	}
	if len(histories) != 1 {
		t.Fatalf("应有 1 条历史，实际 %d", len(histories))
	}
	if histories[0].Source != model.PasswordSourceChangePassword {
		t.Errorf("source = %s, want change_password", histories[0].Source)
	}
	if bcrypt.CheckPassword(histories[0].PasswordHash, "NewPassword123") != nil {
		t.Error("哈希应匹配 NewPassword123")
	}
}

// TestRecordPasswordHistory_DefaultSource 测试默认 source
func TestRecordPasswordHistory_DefaultSource(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()

	user := &model.SystemUser{
		Username: "default_source_user",
		Password: "Passw" + "ord123",
		Email:    "default@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	if err := s.RecordPasswordHistory(context.Background(), user.ID, "Test1234", ""); err != nil {
		t.Fatalf("RecordPasswordHistory 失败: %v", err)
	}

	var histories []model.PasswordHistory
	database.Where("user_id = ?", user.ID).Find(&histories)
	if len(histories) != 1 {
		t.Fatalf("应有 1 条历史，实际 %d", len(histories))
	}
	if histories[0].Source != model.PasswordSourceChangePassword {
		t.Errorf("默认 source 应为 change_password，实际 %s", histories[0].Source)
	}
}

// TestValidatePolicy 测试策略合理性校验
func TestValidatePolicy(t *testing.T) {
	s := NewPasswordPolicyService()

	cases := []struct {
		name   string
		policy *PasswordPolicy
		wantOK bool
	}{
		{"valid", &PasswordPolicy{MinLength: 8, MaxLength: 64, ReuseCount: 5, ExpiryDays: 90}, true},
		{"min_too_small", &PasswordPolicy{MinLength: 3, MaxLength: 64, ReuseCount: 5, ExpiryDays: 90}, false},
		{"max_less_than_min", &PasswordPolicy{MinLength: 10, MaxLength: 5, ReuseCount: 5, ExpiryDays: 90}, false},
		{"max_too_large", &PasswordPolicy{MinLength: 8, MaxLength: 1000, ReuseCount: 5, ExpiryDays: 90}, false},
		{"reuse_but_no_count", &PasswordPolicy{MinLength: 8, MaxLength: 64, ForbidReuse: true, ReuseCount: 0, ExpiryDays: 90}, false},
		{"expiry_negative", &PasswordPolicy{MinLength: 8, MaxLength: 64, ReuseCount: 5, ExpiryDays: -1}, false},
		{"expiry_zero_ok", &PasswordPolicy{MinLength: 8, MaxLength: 64, ReuseCount: 5, ExpiryDays: 0}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.validatePolicy(context.Background(), c.policy)
			if c.wantOK && err != nil {
				t.Errorf("期望通过但失败: %v", err)
			}
			if !c.wantOK && err == nil {
				t.Error("期望失败但通过")
			}
		})
	}
}

// TestIsPasswordExpired_Disabled 测试未启用过期
func TestIsPasswordExpired_Disabled(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()
	policy := &PasswordPolicy{MinLength: 8, MaxLength: 64, ExpiryDays: 0}
	if err := s.SavePolicy(context.Background(), policy); err != nil {
		t.Fatalf("SavePolicy failed: %v", err)
	}
	expired, err := s.IsPasswordExpired(context.Background(), 1)
	if err != nil {
		t.Fatalf("IsPasswordExpired 应无错: %v", err)
	}
	if expired {
		t.Error("ExpiryDays=0 时不应过期")
	}
	_ = database
}

// TestIsPasswordExpired_FreshHistory 测试刚改密不久
func TestIsPasswordExpired_FreshHistory(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()

	policy := &PasswordPolicy{ExpiryDays: 90}
	_ = s.SavePolicy(context.Background(), policy)
	s.InvalidatePolicyCache(context.Background())

	user := &model.SystemUser{
		Username: "fresh_user",
		Password: "Passw" + "ord123",
		Email:    "fresh@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	now := time.Now()
	history := &model.PasswordHistory{
		UserID:       user.ID,
		PasswordHash: "fake",
		ChangedAt:    now,
		Source:       model.PasswordSourceChangePassword,
	}
	database.Create(history)

	expired, err := s.IsPasswordExpired(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IsPasswordExpired 失败: %v", err)
	}
	if expired {
		t.Error("刚改密的密码不应过期")
	}
}

// TestIsPasswordExpired_OldHistory 测试老密码应过期
func TestIsPasswordExpired_OldHistory(t *testing.T) {
	database := setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()

	policy := &PasswordPolicy{MinLength: 8, MaxLength: 64, ExpiryDays: 1}
	if err := s.SavePolicy(context.Background(), policy); err != nil {
		t.Fatalf("SavePolicy failed: %v", err)
	}

	user := &model.SystemUser{
		Username: "old_user",
		Password: "Passw" + "ord123",
		Email:    "old@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	old := time.Now().Add(-2 * 24 * time.Hour)
	history := &model.PasswordHistory{
		UserID:       user.ID,
		PasswordHash: "fake",
		ChangedAt:    old,
		Source:       model.PasswordSourceChangePassword,
	}
	database.Create(history)

	expired, err := s.IsPasswordExpired(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IsPasswordExpired 失败: %v", err)
	}
	if !expired {
		t.Error("2 天前改的密码（expiry=1 天）应过期")
	}
}

// TestSavePolicy_Invalid 测试保存非法策略
func TestSavePolicy_Invalid(t *testing.T) {
	setupPasswordPolicyTestDB(t)
	s := NewPasswordPolicyService()

	err := s.SavePolicy(context.Background(), &PasswordPolicy{MinLength: 2, MaxLength: 64, ReuseCount: 5})
	if err == nil {
		t.Error("min<4 的策略应被拒")
	}
}

// TestDefaultPasswordPolicy_Valid 测试默认策略能通过合理密码
func TestDefaultPasswordPolicy_Valid(t *testing.T) {
	s := NewPasswordPolicyService()

	if err := s.ValidatePasswordWithPolicy(context.Background(), "StrongP@ssw0rd", 0, &DefaultPasswordPolicy); err != nil {
		t.Errorf("强密码应通过默认策略: %v", err)
	}
}

// TestValidatePasswordStrength 静态函数式校验
func TestValidatePasswordStrength(t *testing.T) {
	cases := []struct {
		pwd   string
		valid bool
	}{
		{"", false},
		{"short", false},
		{"alllower1234", false},
		{"Xkp9aBz3", true},
		{"Pa$$w0rdXyz9", true},
		{"123456", false},
		{"admin888", false},
		{"Abcdefg1", false},
		{"Xk1234567", false},
		{"Password", false},
	}
	for _, c := range cases {
		err := ValidatePasswordStrength(c.pwd)
		if c.valid && err != nil {
			t.Errorf("密码 %q 应通过: %v", c.pwd, err)
		}
		if !c.valid && err == nil {
			t.Errorf("密码 %q 应被拒", c.pwd)
		}
	}
}
