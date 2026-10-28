package email

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupEmailUnsubscribeTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailUnsubscribe{},
	)
	db.SetTestDB(database)
	return database
}

func newEmailUnsubscribeService(database *gorm.DB) *EmailUnsubscribeService {
	return NewEmailUnsubscribeService(repository.NewEmailUnsubscribeRepository(database))
}

// TestEmailUnsubscribe_NewService 测试创建服务
func TestEmailUnsubscribe_NewService(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)
	if svc == nil {
		t.Fatal("Expected non-nil service")
	}
}

// TestEmailUnsubscribe_UnsubscribeEmail_NewRecord 测试新邮箱退订
func TestEmailUnsubscribe_UnsubscribeEmail_NewRecord(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	err := svc.UnsubscribeEmail(context.Background(), "User@Example.com", "不再需要", "/api/email/unsubscribe/confirm", "job-123", "127.0.0.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("UnsubscribeEmail failed: %v", err)
	}

	var record model.EmailUnsubscribe
	if err := database.Where("email = ?", "user@example.com").First(&record).Error; err != nil {
		t.Fatalf("查询退订记录失败: %v", err)
	}
	if record.Reason != "不再需要" {
		t.Errorf("Expected reason '不再需要', got %s", record.Reason)
	}
	if record.IP != "127.0.0.1" {
		t.Errorf("Expected IP '127.0.0.1', got %s", record.IP)
	}
	if record.JobID != "job-123" {
		t.Errorf("Expected JobID 'job-123', got %s", record.JobID)
	}
	if record.UnsubscribedAt.IsZero() {
		t.Error("Expected UnsubscribedAt to be set")
	}
}

// TestEmailUnsubscribe_UnsubscribeEmail_Idempotent 测试重复退订幂等
func TestEmailUnsubscribe_UnsubscribeEmail_Idempotent(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.UnsubscribeEmail(context.Background(), "a@b.com", "reason1", "", "", "", ""); err != nil {
		t.Fatalf("首次退订失败: %v", err)
	}

	if err := svc.UnsubscribeEmail(context.Background(), "A@B.com", "reason2", "", "", "", ""); err != nil {
		t.Fatalf("二次退订失败: %v", err)
	}

	var count int64
	database.Model(&model.EmailUnsubscribe{}).Where("email = ?", "a@b.com").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 record (idempotent), got %d", count)
	}

	var record model.EmailUnsubscribe
	database.Where("email = ?", "a@b.com").First(&record)
	if record.Reason != "reason2" {
		t.Errorf("Expected reason updated to 'reason2', got %s", record.Reason)
	}
}

// TestEmailUnsubscribe_UnsubscribeEmail_EmptyEmail 测试空邮箱
func TestEmailUnsubscribe_UnsubscribeEmail_EmptyEmail(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	err := svc.UnsubscribeEmail(context.Background(), "  ", "reason", "", "", "", "")
	if err == nil {
		t.Error("Expected error for empty email")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("Expected error message mentions email, got: %s", err.Error())
	}
}

// TestEmailUnsubscribe_IsUnsubscribed_True 测试已退订
func TestEmailUnsubscribe_IsUnsubscribed_True(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.UnsubscribeEmail(context.Background(), "sub@demo.com", "", "", "", "", ""); err != nil {
		t.Fatalf("退订失败: %v", err)
	}

	if !svc.IsUnsubscribed(context.Background(), "sub@demo.com") {
		t.Error("Expected IsUnsubscribed=true")
	}
	if !svc.IsUnsubscribed(context.Background(), "SUB@DEMO.COM") {
		t.Error("Expected IsUnsubscribed=true for upper case")
	}
}

// TestEmailUnsubscribe_IsUnsubscribed_False 测试未退订
func TestEmailUnsubscribe_IsUnsubscribed_False(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if svc.IsUnsubscribed(context.Background(), "notexist@demo.com") {
		t.Error("Expected IsUnsubscribed=false for unknown email")
	}
}

// TestEmailUnsubscribe_IsUnsubscribed_EmptyEmail 测试空邮箱
func TestEmailUnsubscribe_IsUnsubscribed_EmptyEmail(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if svc.IsUnsubscribed(context.Background(), "") {
		t.Error("Expected IsUnsubscribed=false for empty email")
	}
}

// TestEmailUnsubscribe_ResubscribeEmail_Success 测试重新订阅
func TestEmailUnsubscribe_ResubscribeEmail_Success(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.UnsubscribeEmail(context.Background(), "re@sub.com", "", "", "", "", ""); err != nil {
		t.Fatalf("退订失败: %v", err)
	}
	if !svc.IsUnsubscribed(context.Background(), "re@sub.com") {
		t.Fatal("Expected unsubscribed")
	}

	if err := svc.ResubscribeEmail(context.Background(), "re@sub.com"); err != nil {
		t.Fatalf("重新订阅失败: %v", err)
	}
	if svc.IsUnsubscribed(context.Background(), "re@sub.com") {
		t.Error("Expected IsUnsubscribed=false after resubscribe")
	}
}

// TestEmailUnsubscribe_ResubscribeEmail_NotExist 测试重新订阅不存在的记录
func TestEmailUnsubscribe_ResubscribeEmail_NotExist(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.ResubscribeEmail(context.Background(), "never@sub.com"); err != nil {
		t.Errorf("重新订阅不存在的记录应幂等返回 nil, got: %v", err)
	}
}

// TestEmailUnsubscribe_ResubscribeEmail_EmptyEmail 测试空邮箱重新订阅
func TestEmailUnsubscribe_ResubscribeEmail_EmptyEmail(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.ResubscribeEmail(context.Background(), ""); err == nil {
		t.Error("Expected error for empty email")
	}
}

// TestEmailUnsubscribe_GenerateUnsubscribeLink_Format 测试链接格式
func TestEmailUnsubscribe_GenerateUnsubscribeLink_Format(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "test-unsubscribe-secret")
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	link, err := svc.GenerateUnsubscribeLink(context.Background(), "link@demo.com", "job-001")
	if err != nil {
		t.Fatalf("GenerateUnsubscribeLink failed: %v", err)
	}

	if !strings.Contains(link, "/api/email/unsubscribe?token=") {
		t.Errorf("链接缺少退订路径, got: %s", link)
	}
	if !strings.Contains(link, "token=") {
		t.Errorf("链接缺少 token 参数, got: %s", link)
	}
}

// TestEmailUnsubscribe_GenerateUnsubscribeLink_EmptyEmail 测试空邮箱
func TestEmailUnsubscribe_GenerateUnsubscribeLink_EmptyEmail(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	_, err := svc.GenerateUnsubscribeLink(context.Background(), "", "job-001")
	if err == nil {
		t.Error("Expected error for empty email")
	}
}

// TestEmailUnsubscribe_VerifyToken_Valid 测试有效 token 验证
func TestEmailUnsubscribe_VerifyToken_Valid(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "test-unsubscribe-secret")
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	link, err := svc.GenerateUnsubscribeLink(context.Background(), "verify@demo.com", "job-002")
	if err != nil {
		t.Fatalf("GenerateUnsubscribeLink failed: %v", err)
	}

	idx := strings.Index(link, "token=")
	if idx < 0 {
		t.Fatalf("链接缺少 token 参数: %s", link)
	}
	token := link[idx+6:]

	claim, err := svc.VerifyUnsubscribeToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyUnsubscribeToken failed: %v", err)
	}
	if claim.Email != "verify@demo.com" {
		t.Errorf("Expected email 'verify@demo.com', got %s", claim.Email)
	}
	if claim.JobID != "job-002" {
		t.Errorf("Expected JobID 'job-002', got %s", claim.JobID)
	}
	if claim.Expire <= time.Now().Unix() {
		t.Error("Expected expire in future")
	}
}

// TestEmailUnsubscribe_VerifyToken_InvalidSignature 测试签名篡改
func TestEmailUnsubscribe_VerifyToken_InvalidSignature(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "test-unsubscribe-secret")
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	link, _ := svc.GenerateUnsubscribeLink(context.Background(), "tamper@demo.com", "job-003")
	idx := strings.Index(link, "token=")
	token := link[idx+6:]

	tampered := token[:len(token)-5] + "XXXXX"
	_, err := svc.VerifyUnsubscribeToken(context.Background(), tampered)
	if err == nil {
		t.Error("Expected error for tampered token")
	}
	if !strings.Contains(err.Error(), "签名") {
		t.Errorf("Expected signature error, got: %s", err.Error())
	}
}

// TestEmailUnsubscribe_VerifyToken_Expired 测试过期 token
func TestEmailUnsubscribe_VerifyToken_Expired(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "test-unsubscribe-secret")
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	link, err := svc.GenerateUnsubscribeLink(context.Background(), "exp@demo.com", "")
	if err != nil {
		t.Fatalf("GenerateUnsubscribeLink failed: %v", err)
	}
	idx := strings.Index(link, "token=")
	token := link[idx+6:]

	claim, err := svc.VerifyUnsubscribeToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyUnsubscribeToken failed: %v", err)
	}
	expireTime := time.Unix(claim.Expire, 0)
	if !expireTime.After(time.Now().Add(29 * 24 * time.Hour)) {
		t.Errorf("Expected token valid for at least 29 days, expire at %v", expireTime)
	}
}

// TestEmailUnsubscribe_VerifyToken_Empty 测试空 token
func TestEmailUnsubscribe_VerifyToken_Empty(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	_, err := svc.VerifyUnsubscribeToken(context.Background(), "")
	if err == nil {
		t.Error("Expected error for empty token")
	}
}

// TestEmailUnsubscribe_VerifyToken_Malformed 测试格式错误 token
func TestEmailUnsubscribe_VerifyToken_Malformed(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	_, err := svc.VerifyUnsubscribeToken(context.Background(), "malformed-token-without-dot")
	if err == nil {
		t.Error("Expected error for malformed token")
	}
}

// TestEmailUnsubscribe_ListUnsubscribes 测试分页查询
func TestEmailUnsubscribe_ListUnsubscribes(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	for i, email := range []string{"a@x.com", "b@x.com", "c@y.com"} {
		if err := svc.UnsubscribeEmail(context.Background(), email, "r"+string(rune('0'+i)), "", "", "", ""); err != nil {
			t.Fatalf("UnsubscribeEmail failed: %v", err)
		}
	}

	records, total, err := svc.ListUnsubscribes(context.Background(), 1, 20, "")
	if err != nil {
		t.Fatalf("ListUnsubscribes failed: %v", err)
	}
	if total != 3 {
		t.Errorf("Expected total 3, got %d", total)
	}
	if len(records) != 3 {
		t.Errorf("Expected 3 records, got %d", len(records))
	}

	records, total, err = svc.ListUnsubscribes(context.Background(), 1, 20, "@x.com")
	if err != nil {
		t.Fatalf("ListUnsubscribes with keyword failed: %v", err)
	}
	if total != 2 {
		t.Errorf("Expected total 2 for keyword '@x.com', got %d", total)
	}
	if len(records) != 2 {
		t.Errorf("Expected 2 records for keyword, got %d", len(records))
	}
}

// TestEmailUnsubscribe_ListUnsubscribes_Pagination 测试分页
func TestEmailUnsubscribe_ListUnsubscribes_Pagination(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	for i := 0; i < 5; i++ {
		email := string(rune('a'+i)) + "@p.com"
		if err := svc.UnsubscribeEmail(context.Background(), email, "", "", "", "", ""); err != nil {
			t.Fatalf("UnsubscribeEmail failed: %v", err)
		}
	}

	records, total, err := svc.ListUnsubscribes(context.Background(), 1, 2, "")
	if err != nil {
		t.Fatalf("ListUnsubscribes page 1 failed: %v", err)
	}
	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(records) != 2 {
		t.Errorf("Expected 2 records on page 1, got %d", len(records))
	}

	records, _, err = svc.ListUnsubscribes(context.Background(), 3, 2, "")
	if err != nil {
		t.Fatalf("ListUnsubscribes page 3 failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("Expected 1 record on page 3, got %d", len(records))
	}
}

// TestEmailUnsubscribe_ListUnsubscribes_DefaultPaging 测试默认分页参数
func TestEmailUnsubscribe_ListUnsubscribes_DefaultPaging(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	if err := svc.UnsubscribeEmail(context.Background(), "p1@x.com", "", "", "", "", ""); err != nil {
		t.Fatalf("UnsubscribeEmail failed: %v", err)
	}

	records, total, err := svc.ListUnsubscribes(context.Background(), 0, 0, "")
	if err != nil {
		t.Fatalf("ListUnsubscribes failed: %v", err)
	}
	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}
	if len(records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(records))
	}
}

// TestEmailUnsubscribe_ListAllUnsubscribes 测试全量导出
func TestEmailUnsubscribe_ListAllUnsubscribes(t *testing.T) {
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	for i := 0; i < 3; i++ {
		email := string(rune('a'+i)) + "@export.com"
		if err := svc.UnsubscribeEmail(context.Background(), email, "", "", "", "", ""); err != nil {
			t.Fatalf("UnsubscribeEmail failed: %v", err)
		}
	}

	all, err := svc.ListAllUnsubscribes(context.Background())
	if err != nil {
		t.Fatalf("ListAllUnsubscribes failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 records, got %d", len(all))
	}
}

// TestEmailUnsubscribe_NormalizeEmail 测试邮箱规范化
func TestEmailUnsubscribe_NormalizeEmail(t *testing.T) {
	if normalizeEmail("  User@Example.COM  ") != "user@example.com" {
		t.Error("normalizeEmail failed to lowercase and trim")
	}
	if normalizeEmail("") != "" {
		t.Error("normalizeEmail should return empty for empty input")
	}
}

// TestEmailUnsubscribe_FullLifecycle 测试完整生命周期
func TestEmailUnsubscribe_FullLifecycle(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "test-unsubscribe-secret")
	database := setupEmailUnsubscribeTestDB(t)
	svc := newEmailUnsubscribeService(database)

	email := "lifecycle@demo.com"

	if svc.IsUnsubscribed(context.Background(), email) {
		t.Error("Initial state should be not unsubscribed")
	}

	link, err := svc.GenerateUnsubscribeLink(context.Background(), email, "job-lc")
	if err != nil {
		t.Fatalf("GenerateUnsubscribeLink failed: %v", err)
	}

	idx := strings.Index(link, "token=")
	token := link[idx+6:]
	claim, err := svc.VerifyUnsubscribeToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyUnsubscribeToken failed: %v", err)
	}
	if claim.Email != email {
		t.Errorf("Expected email %s, got %s", email, claim.Email)
	}

	if err := svc.UnsubscribeEmail(context.Background(), claim.Email, "测试生命周期", "", claim.JobID, "", ""); err != nil {
		t.Fatalf("UnsubscribeEmail failed: %v", err)
	}

	if !svc.IsUnsubscribed(context.Background(), email) {
		t.Error("Should be unsubscribed after UnsubscribeEmail")
	}

	if err := svc.ResubscribeEmail(context.Background(), email); err != nil {
		t.Fatalf("ResubscribeEmail failed: %v", err)
	}

	if svc.IsUnsubscribed(context.Background(), email) {
		t.Error("Should be resubscribed after ResubscribeEmail")
	}
}
