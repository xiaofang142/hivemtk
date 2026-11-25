package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMFATestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.UserMFA{},
		&model.SystemUser{},
		&model.PasswordHistory{},
		&model.LoginEvent{},
		&model.SecurityAlert{},
		&model.Notification{},
		&model.OperationLog{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewMFAService 测试构造函数
func TestNewMFAService(t *testing.T) {
	s := NewMFAService()
	if s == nil {
		t.Fatal("NewMFAService returned nil")
	}
}

// TestGenerateMFASecret 测试密钥生成
func TestGenerateMFASecret(t *testing.T) {
	s := NewMFAService()
	secret, err := s.GenerateMFASecret(context.Background())
	if err != nil {
		t.Fatalf("GenerateMFASecret 失败: %v", err)
	}
	if len(secret) != 32 {
		t.Errorf("密钥长度 = %d, want 32", len(secret))
	}
	for _, c := range secret {
		if !isBase32Char(c) {
			t.Errorf("密钥含非法字符: %q in %q", c, secret)
		}
	}

	secret2, _ := s.GenerateMFASecret(context.Background())
	if secret == secret2 {
		t.Error("两次生成的密钥相同（应该随机）")
	}
}

// TestGenerateTOTP 测试 TOTP 码生成
func TestGenerateTOTP(t *testing.T) {
	s := NewMFAService()
	secret, _ := s.GenerateMFASecret(context.Background())

	fixedTime := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	code, err := s.GenerateTOTP(context.Background(), secret, fixedTime)
	if err != nil {
		t.Fatalf("GenerateTOTP 失败: %v", err)
	}
	if len(code) != totpDigits {
		t.Errorf("TOTP 码长度 = %d, want %d", len(code), totpDigits)
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("TOTP 码含非数字字符: %q", c)
		}
	}

	code2, _ := s.GenerateTOTP(context.Background(), secret, fixedTime)
	if code != code2 {
		t.Errorf("相同输入应得到相同 TOTP: %s != %s", code, code2)
	}

	differentTime := fixedTime.Add(45 * time.Second)
	code3, _ := s.GenerateTOTP(context.Background(), secret, differentTime)
	if code == code3 {
		t.Error("不同时段不应得到相同 TOTP")
	}
}

// TestVerifyTOTP_SuccessAndFailures 测试 TOTP 验证
func TestVerifyTOTP_SuccessAndFailures(t *testing.T) {
	s := NewMFAService()
	secret, _ := s.GenerateMFASecret(context.Background())
	fixedTime := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	code, _ := s.GenerateTOTP(context.Background(), secret, fixedTime)

	if !s.VerifyTOTPAt(context.Background(), secret, code, fixedTime) {
		t.Error("正确的 TOTP 应该验证通过")
	}

	if s.VerifyTOTPAt(context.Background(), secret, "000000", fixedTime) {
		t.Error("错误的 TOTP 不应验证通过")
	}

	if s.VerifyTOTPAt(context.Background(), secret, "12345", fixedTime) {
		t.Error("5 位 TOTP 应验证失败")
	}
	if s.VerifyTOTPAt(context.Background(), secret, "1234567", fixedTime) {
		t.Error("7 位 TOTP 应验证失败")
	}
	if s.VerifyTOTPAt(context.Background(), secret, "", fixedTime) {
		t.Error("空 TOTP 应验证失败")
	}

	prevCode, _ := s.GenerateTOTP(context.Background(), secret, fixedTime.Add(-30*time.Second))
	if !s.VerifyTOTPAt(context.Background(), secret, prevCode, fixedTime) {
		t.Error("前一个时间窗口的码应验证通过（±1 窗口）")
	}
	nextCode, _ := s.GenerateTOTP(context.Background(), secret, fixedTime.Add(30*time.Second))
	if !s.VerifyTOTPAt(context.Background(), secret, nextCode, fixedTime) {
		t.Error("后一个时间窗口的码应验证通过（±1 窗口）")
	}

	farCode, _ := s.GenerateTOTP(context.Background(), secret, fixedTime.Add(120*time.Second))
	if s.VerifyTOTPAt(context.Background(), secret, farCode, fixedTime) {
		t.Error("超出 ±1 窗口的码应验证失败")
	}

	wrongSecret, _ := s.GenerateMFASecret(context.Background())
	if s.VerifyTOTPAt(context.Background(), wrongSecret, code, fixedTime) {
		t.Error("错误密钥 + 正确码应验证失败")
	}
}

// TestGenerateOTPAuthURL 测试 otpauth URL 生成
func TestGenerateOTPAuthURL(t *testing.T) {
	s := NewMFAService()
	secret := "JBSWY3DPEHPK3PXP"
	url := s.GenerateOTPAuthURL(context.Background(), secret, "alice@example.com", "MarketingSystem")

	if !strings.HasPrefix(url, "otpauth://totp/") {
		t.Errorf("URL 缺少 otpauth://totp/ 前缀: %s", url)
	}
	if !strings.Contains(url, "secret="+secret) {
		t.Errorf("URL 缺少 secret 参数: %s", url)
	}
	if !strings.Contains(url, "issuer=MarketingSystem") {
		t.Errorf("URL 缺少 issuer 参数: %s", url)
	}
	if !strings.Contains(url, "algorithm=SHA1") {
		t.Errorf("URL 缺少 algorithm 参数: %s", url)
	}
	if !strings.Contains(url, "digits=6") {
		t.Errorf("URL 缺少 digits=6 参数: %s", url)
	}
	if !strings.Contains(url, "period=30") {
		t.Errorf("URL 缺少 period=30 参数: %s", url)
	}
}

// TestGenerateOTPAuthURL_Defaults 测试默认值（空 issuer/account）
func TestGenerateOTPAuthURL_Defaults(t *testing.T) {
	s := NewMFAService()
	url := s.GenerateOTPAuthURL(context.Background(), "AAAA", "", "")
	if !strings.Contains(url, "issuer=MarketingSystem") {
		t.Errorf("默认 issuer 应为 MarketingSystem: %s", url)
	}
	if !strings.Contains(url, "MarketingSystem:user") {
		t.Errorf("默认 account 应为 user: %s", url)
	}
}

// TestTempToken_Lifecycle 测试临时令牌生命周期
func TestTempToken_Lifecycle(t *testing.T) {
	s := NewMFAService()

	token, err := s.IssueTempToken(context.Background(), 42, "alice", "admin")
	if err != nil {
		t.Fatalf("IssueTempToken 失败: %v", err)
	}
	if token == "" {
		t.Fatal("临时令牌为空")
	}

	uid, name, role, err := s.ValidateTempToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateTempToken 失败: %v", err)
	}
	if uid != 42 || name != "alice" || role != "admin" {
		t.Errorf("临时令牌内容错误: uid=%d name=%s role=%s", uid, name, role)
	}

	s.ConsumeTempToken(context.Background(), token)

	_, _, _, err = s.ValidateTempToken(context.Background(), token)
	if err == nil {
		t.Error("已消费的临时令牌应验证失败")
	}
}

// TestTempToken_Invalid 测试无效令牌
func TestTempToken_Invalid(t *testing.T) {
	s := NewMFAService()

	_, _, _, err := s.ValidateTempToken(context.Background(), "nonexistent")
	if err == nil {
		t.Error("不存在的临时令牌应验证失败")
	}

	_, _, _, err = s.ValidateTempToken(context.Background(), "")
	if err == nil {
		t.Error("空临时令牌应验证失败")
	}
}

// TestTempToken_Unique 测试多次颁发唯一性
func TestTempToken_Unique(t *testing.T) {
	s := NewMFAService()
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		tok, err := s.IssueTempToken(context.Background(), 1, "u", "viewer")
		if err != nil {
			t.Fatalf("IssueTempToken 第 %d 次失败: %v", i, err)
		}
		if seen[tok] {
			t.Fatalf("临时令牌重复: %s", tok)
		}
		seen[tok] = true
	}
	if len(seen) != 50 {
		t.Errorf("唯一令牌数 = %d, want 50", len(seen))
	}
}

// TestSetupMFA 端到端：MFA 设置
func TestSetupMFA(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	user := &model.SystemUser{
		Username: "test_mfa_user",
		Password: "Pass" + "word123",
		Email:    "mfa@example.com",
		Role:     "user",
		Status:   1,
	}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	resp, err := s.SetupMFA(context.Background(), user.ID, user.Username)
	if err != nil {
		t.Fatalf("SetupMFA 失败: %v", err)
	}
	if resp.Secret == "" {
		t.Error("SetupMFA 返回的 secret 为空")
	}
	if resp.OTPAuthURL == "" {
		t.Error("SetupMFA 返回的 otpauth_url 为空")
	}
	if !strings.HasPrefix(resp.QRCodeURL, "https://") {
		t.Errorf("QRCodeURL 应该是 https URL: %s", resp.QRCodeURL)
	}

	var mfa model.UserMFA
	if err := database.Where("user_id = ?", user.ID).First(&mfa).Error; err != nil {
		t.Fatalf("查询 MFA 记录失败: %v", err)
	}
	if mfa.MFAEnabled {
		t.Error("Setup 后 mfa_enabled 应为 false")
	}
	if mfa.MFASecret != resp.Secret {
		t.Error("DB secret 与响应 secret 不一致")
	}
}

// TestConfirmMFASetup_VerifyAndEnable 测试确认流程
func TestConfirmMFASetup_VerifyAndEnable(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	user := &model.SystemUser{
		Username: "confirm_user",
		Password: "Pass" + "word123",
		Email:    "confirm@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	resp, err := s.SetupMFA(context.Background(), user.ID, user.Username)
	if err != nil {
		t.Fatalf("SetupMFA 失败: %v", err)
	}

	var mfa model.UserMFA
	database.Where("user_id = ?", user.ID).First(&mfa)
	now := time.Now()
	code, _ := s.GenerateTOTP(context.Background(), mfa.MFASecret, now)

	if err := s.ConfirmMFASetup(context.Background(), user.ID, code); err != nil {
		t.Fatalf("ConfirmMFASetup 失败: %v", err)
	}

	database.Where("user_id = ?", user.ID).First(&mfa)
	if !mfa.MFAEnabled {
		t.Error("Confirm 后 mfa_enabled 应为 true")
	}
	if mfa.EnabledAt == nil {
		t.Error("Confirm 后 enabled_at 应被设置")
	}

	if err := s.ConfirmMFASetup(context.Background(), user.ID, "000000"); err == nil {
		t.Error("Confirm 错误码应失败")
	}

	if err := s.ConfirmMFASetup(context.Background(), user.ID, code); err == nil {
		t.Error("重复 Confirm 应失败")
	}

	_ = resp
}

// TestIsMFAEnabled 测试状态查询
func TestIsMFAEnabled(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	user := &model.SystemUser{
		Username: "is_enabled_user",
		Password: "Pass" + "word123",
		Email:    "is@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	enabled, err := s.IsMFAEnabled(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IsMFAEnabled 不存在应无错: %v", err)
	}
	if enabled {
		t.Error("未设置 MFA 的用户 enabled 应为 false")
	}

	resp, _ := s.SetupMFA(context.Background(), user.ID, user.Username)
	var mfa model.UserMFA
	database.Where("user_id = ?", user.ID).First(&mfa)
	code, _ := s.GenerateTOTP(context.Background(), resp.Secret, time.Now())
	s.ConfirmMFASetup(context.Background(), user.ID, code)

	enabled, err = s.IsMFAEnabled(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("IsMFAEnabled 应无错: %v", err)
	}
	if !enabled {
		t.Error("已启用 MFA 的用户 enabled 应为 true")
	}
}

// TestVerifyMFALogin_ReplayProtection 测试重放保护
func TestVerifyMFALogin_ReplayProtection(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	user := &model.SystemUser{
		Username: "replay_user",
		Password: "Pass" + "word123",
		Email:    "replay@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	resp, _ := s.SetupMFA(context.Background(), user.ID, user.Username)
	var mfa model.UserMFA
	database.Where("user_id = ?", user.ID).First(&mfa)
	code, _ := s.GenerateTOTP(context.Background(), resp.Secret, time.Now())
	if err := s.ConfirmMFASetup(context.Background(), user.ID, code); err != nil {
		t.Fatalf("ConfirmMFASetup 失败: %v", err)
	}

	tempToken1, _ := s.IssueTempToken(context.Background(), user.ID, user.Username, user.Role)
	uid, name, role, err := s.VerifyMFALogin(context.Background(), tempToken1, code)
	if err != nil {
		t.Fatalf("首次 VerifyMFALogin 失败: %v", err)
	}
	if uid != user.ID || name != user.Username || role != user.Role {
		t.Errorf("VerifyMFALogin 返回值错误: uid=%d name=%s role=%s", uid, name, role)
	}

	tempToken2, _ := s.IssueTempToken(context.Background(), user.ID, user.Username, user.Role)
	_, _, _, err = s.VerifyMFALogin(context.Background(), tempToken2, code)
	if err == nil {
		t.Error("60s 重放窗口内的同一 code 应被拒绝（防重放保护）")
	}
}

// TestGenerateBackupCodes 测试备用码生成
func TestGenerateBackupCodes(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	user := &model.SystemUser{
		Username: "backup_user",
		Password: "Pass" + "word123",
		Email:    "backup@example.com",
		Role:     "user",
		Status:   1,
	}
	database.Create(user)

	_, err := s.SetupMFA(context.Background(), user.ID, user.Username)
	if err != nil {
		t.Fatalf("SetupMFA 失败: %v", err)
	}

	codes, err := s.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes 失败: %v", err)
	}

	if len(codes) != 10 {
		t.Errorf("备用码数 = %d, want 10", len(codes))
	}

	seen := make(map[string]bool)
	for _, c := range codes {
		if c == "" {
			t.Error("备用码含空字符串")
		}
		if seen[c] {
			t.Errorf("备用码重复: %s", c)
		}
		seen[c] = true
	}

	var mfa model.UserMFA
	database.Where("user_id = ?", user.ID).First(&mfa)
	if mfa.BackupCodes == "" || mfa.BackupCodes == "[]" {
		t.Errorf("DB 中 backup_codes 应被存储, got: %q", mfa.BackupCodes)
	}
	if !strings.HasPrefix(mfa.BackupCodes, "[") {
		t.Errorf("backup_codes 应为 JSON 数组: %s", mfa.BackupCodes)
	}
}

func isBase32Char(c rune) bool {
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '2' && c <= '7' {
		return true
	}
	if c == '=' {
		return true
	}
	return false
}
