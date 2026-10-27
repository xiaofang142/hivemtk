package sms

import (
	"context"
	"hivemtk-user/internal/identity"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupSmsUnsubscribeTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SmsUnsubscribe{},
	)
	db.SetTestDB(database)
	return database
}

func newSmsUnsubscribeService(database *gorm.DB) *SmsUnsubscribeService {
	return NewSmsUnsubscribeService(repository.NewSmsUnsubscribeRepository(database))
}

// TestSmsUnsubscribe_NewService 测试创建服务
func TestSmsUnsubscribe_NewService(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)
	if svc == nil {
		t.Fatal("Expected non-nil service")
	}
}

// TestSmsUnsubscribe_UnsubscribePhone_NewRecord 测试新号码退订
func TestSmsUnsubscribe_UnsubscribePhone_NewRecord(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	err := svc.UnsubscribePhone(context.Background(), "138-0013-8000", "用户主动退订", "msg-001", "TD")
	if err != nil {
		t.Fatalf("UnsubscribePhone failed: %v", err)
	}

	var record model.SmsUnsubscribe
	if err := database.Where("phone = ?", "13800138000").First(&record).Error; err != nil {
		t.Fatalf("查询退订记录失败: %v", err)
	}
	if record.Reason != "用户主动退订" {
		t.Errorf("Expected reason '用户主动退订', got %s", record.Reason)
	}
	if record.KeywordMatched != "TD" {
		t.Errorf("Expected KeywordMatched 'TD', got %s", record.KeywordMatched)
	}
	if record.SourceMessageID != "msg-001" {
		t.Errorf("Expected SourceMessageID 'msg-001', got %s", record.SourceMessageID)
	}
	if record.UnsubscribedAt.IsZero() {
		t.Error("Expected UnsubscribedAt to be set")
	}
}

// TestSmsUnsubscribe_UnsubscribePhone_Idempotent 测试重复退订幂等
func TestSmsUnsubscribe_UnsubscribePhone_Idempotent(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.UnsubscribePhone(context.Background(), "13900139000", "reason1", "msg-1", "TD"); err != nil {
		t.Fatalf("首次退订失败: %v", err)
	}

	if err := svc.UnsubscribePhone(context.Background(), "13900139000", "reason2", "msg-2", "退订"); err != nil {
		t.Fatalf("二次退订失败: %v", err)
	}

	var count int64
	database.Model(&model.SmsUnsubscribe{}).Where("phone = ?", "13900139000").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 record (idempotent), got %d", count)
	}

	var record model.SmsUnsubscribe
	database.Where("phone = ?", "13900139000").First(&record)
	if record.Reason != "reason2" {
		t.Errorf("Expected reason updated to 'reason2', got %s", record.Reason)
	}
	if record.KeywordMatched != "退订" {
		t.Errorf("Expected KeywordMatched updated to '退订', got %s", record.KeywordMatched)
	}
}

// TestSmsUnsubscribe_UnsubscribePhone_EmptyPhone 测试空手机号
func TestSmsUnsubscribe_UnsubscribePhone_EmptyPhone(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	err := svc.UnsubscribePhone(context.Background(), "  ", "reason", "", "")
	if err == nil {
		t.Error("Expected error for empty phone")
	}
	if !strings.Contains(err.Error(), "phone") {
		t.Errorf("Expected error message mentions phone, got: %s", err.Error())
	}
}

// TestSmsUnsubscribe_UnsubscribePhone_WithCountryCode 测试带国家码的手机号规范化
func TestSmsUnsubscribe_UnsubscribePhone_WithCountryCode(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.UnsubscribePhone(context.Background(), "+86 13800138001", "", "", "TD"); err != nil {
		t.Fatalf("UnsubscribePhone failed: %v", err)
	}

	var record model.SmsUnsubscribe
	if err := database.Where("phone = ?", "13800138001").First(&record).Error; err != nil {
		t.Fatalf("查询带国家码退订记录失败: %v", err)
	}
}

// TestSmsUnsubscribe_IsUnsubscribed_True 测试已退订
func TestSmsUnsubscribe_IsUnsubscribed_True(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.UnsubscribePhone(context.Background(), "13700137000", "", "", "TD"); err != nil {
		t.Fatalf("退订失败: %v", err)
	}

	if !svc.IsUnsubscribed(context.Background(), "13700137000") {
		t.Error("Expected IsUnsubscribed=true")
	}
	if !svc.IsUnsubscribed(context.Background(), "+86-137-0013-7000") {
		t.Error("Expected IsUnsubscribed=true for formatted phone")
	}
}

// TestSmsUnsubscribe_IsUnsubscribed_False 测试未退订
func TestSmsUnsubscribe_IsUnsubscribed_False(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if svc.IsUnsubscribed(context.Background(), "13600136000") {
		t.Error("Expected IsUnsubscribed=false for unknown phone")
	}
}

// TestSmsUnsubscribe_IsUnsubscribed_EmptyPhone 测试空手机号
func TestSmsUnsubscribe_IsUnsubscribed_EmptyPhone(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if svc.IsUnsubscribed(context.Background(), "") {
		t.Error("Expected IsUnsubscribed=false for empty phone")
	}
}

// TestSmsUnsubscribe_ResubscribePhone_Success 测试重新订阅
func TestSmsUnsubscribe_ResubscribePhone_Success(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.UnsubscribePhone(context.Background(), "13500135000", "", "", "TD"); err != nil {
		t.Fatalf("退订失败: %v", err)
	}
	if !svc.IsUnsubscribed(context.Background(), "13500135000") {
		t.Fatal("Expected unsubscribed")
	}

	if err := svc.ResubscribePhone(context.Background(), "13500135000"); err != nil {
		t.Fatalf("重新订阅失败: %v", err)
	}
	if svc.IsUnsubscribed(context.Background(), "13500135000") {
		t.Error("Expected IsUnsubscribed=false after resubscribe")
	}
}

// TestSmsUnsubscribe_ResubscribePhone_NotExist 测试重新订阅不存在的记录
func TestSmsUnsubscribe_ResubscribePhone_NotExist(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.ResubscribePhone(context.Background(), "13400134000"); err != nil {
		t.Errorf("重新订阅不存在的记录应幂等返回 nil, got: %v", err)
	}
}

// TestSmsUnsubscribe_ResubscribePhone_EmptyPhone 测试空手机号重新订阅
func TestSmsUnsubscribe_ResubscribePhone_EmptyPhone(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.ResubscribePhone(context.Background(), ""); err == nil {
		t.Error("Expected error for empty phone")
	}
}

// TestSmsUnsubscribe_ProcessReply_MatchedTD 测试回复 TD 关键词
func TestSmsUnsubscribe_ProcessReply_MatchedTD(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	matched, err := svc.ProcessUnsubscribeReply(context.Background(), "13300133000", "TD", "msg-td")
	if err != nil {
		t.Fatalf("ProcessUnsubscribeReply failed: %v", err)
	}
	if matched == "" {
		t.Error("Expected matched keyword for 'TD'")
	}
	if !svc.IsUnsubscribed(context.Background(), "13300133000") {
		t.Error("Expected IsUnsubscribed=true after reply TD")
	}
}

// TestSmsUnsubscribe_ProcessReply_MatchedChinese 测试回复中文关键词
func TestSmsUnsubscribe_ProcessReply_MatchedChinese(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	cases := []string{"退订", "取消", "T退", "停止"}
	for i, kw := range cases {
		phone := "1330013300" + string(rune('0'+i))
		matched, err := svc.ProcessUnsubscribeReply(context.Background(), phone, kw, "")
		if err != nil {
			t.Errorf("ProcessUnsubscribeReply failed for '%s': %v", kw, err)
			continue
		}
		if matched == "" {
			t.Errorf("Expected matched keyword for '%s'", kw)
		}
		if !svc.IsUnsubscribed(context.Background(), phone) {
			t.Errorf("Expected IsUnsubscribed=true after reply '%s'", kw)
		}
	}
}

// TestSmsUnsubscribe_ProcessReply_NotMatched 测试回复非退订关键词
func TestSmsUnsubscribe_ProcessReply_NotMatched(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	cases := []string{
		"好的", "yes", "OK", "我同意", "继续",
		"NT999", "N200",
	}
	for i, content := range cases {
		phone := "1320013200" + string(rune('0'+i))
		matched, err := svc.ProcessUnsubscribeReply(context.Background(), phone, content, "")
		if err != nil {
			t.Errorf("ProcessUnsubscribeReply failed for '%s': %v", content, err)
			continue
		}
		if matched != "" {
			t.Errorf("Expected no match for '%s', got '%s'", content, matched)
		}
		if svc.IsUnsubscribed(context.Background(), phone) {
			t.Errorf("Expected IsUnsubscribed=false after reply '%s'", content)
		}
	}
}

// TestSmsUnsubscribe_ProcessReply_EmptyPhone 测试空手机号
func TestSmsUnsubscribe_ProcessReply_EmptyPhone(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	_, err := svc.ProcessUnsubscribeReply(context.Background(), "", "TD", "")
	if err == nil {
		t.Error("Expected error for empty phone")
	}
}

// TestSmsUnsubscribe_ProcessReply_EmptyContent 测试空内容
func TestSmsUnsubscribe_ProcessReply_EmptyContent(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	_, err := svc.ProcessUnsubscribeReply(context.Background(), "13100131000", "", "")
	if err == nil {
		t.Error("Expected error for empty content")
	}
}

// TestSmsUnsubscribe_MatchKeyword_Comprehensive 关键词匹配综合测试
func TestSmsUnsubscribe_MatchKeyword_Comprehensive(t *testing.T) {
	positive := []struct {
		content string
		expect  string
	}{
		{"TD", "TD"},
		{"td", "td"},
		{"Td", "Td"},
		{"TD", "TD"},
		{"  TD  ", "TD"},
		{"退订", "退订"},
		{"T退", "T退"},
		{"取消", "取消"},
		{"N", "N"},
		{"n", "n"},
		{"Q", "Q"},
		{"q", "q"},
		{"0", "0"},
		{"STOP", "STOP"},
		{"stop", "stop"},
		{"Unsubscribe", "Unsubscribe"},
		{"unsubscribe", "unsubscribe"},
		{"TD退订", "TD"},
		{"我要退订", "退订"},
		{"回复TD退订", "TD"},
	}
	for _, c := range positive {
		m := MatchUnsubscribeKeyword(c.content)
		if m == "" {
			t.Errorf("Expected match for %q, got empty", c.content)
		}
	}
}

// TestSmsUnsubscribe_MatchKeyword_Negative 非退订关键词不应误判
//
// 注意：以下场景应被识别为退订（合规优先原则，宁错杀不漏过）：
//   - "TD-RFID" → 命中 "TD"（"-" 作为分隔符，"TD" 独立成词）
//   - "TD,123" → 命中 "TD"（"," 作为分隔符）
//
// 以下场景应被识别为非退订（避免误判正常业务文本）：
//   - "NT999" → 不命中 "N"（"N" 后紧跟字母数字）
//   - "TDown" → 不命中 "TD"（"TD" 后紧跟字母）
func TestSmsUnsubscribe_MatchKeyword_Negative(t *testing.T) {
	negative := []string{
		"好的", "yes", "OK", "我同意", "继续",
		"NT999", "N200", "Q1234",
		"我想订阅", "请发送", "TDown", "TD12345",
		"", " ", "   ",
		"NT", "QT", "123",
		"NTDT",
	}
	for _, content := range negative {
		m := MatchUnsubscribeKeyword(content)
		if m != "" {
			t.Errorf("Expected no match for %q, got %q", content, m)
		}
	}
}

// TestSmsUnsubscribe_NormalizePhone 测试手机号规范化
func TestSmsUnsubscribe_NormalizePhone(t *testing.T) {
	cases := []struct {
		input  string
		expect string
	}{
		{"13800138000", "13800138000"},
		{"138-0013-8000", "13800138000"},
		{"138 0013 8000", "13800138000"},
		{"+8613800138000", "13800138000"},
		{"8613800138000", "13800138000"},
		{"+86 13800138000", "13800138000"},
		{"  13800138000  ", "13800138000"},
		{"", ""},
		{"  ", ""},
		{"abc", "abc"},
	}
	for _, c := range cases {
		got := identity.NormalizePhone(c.input)
		if got != c.expect {
			t.Errorf("identity.NormalizePhone(%q) = %q, want %q", c.input, got, c.expect)
		}
	}
}

// TestSmsUnsubscribe_ListUnsubscribes 测试分页查询
func TestSmsUnsubscribe_ListUnsubscribes(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	for i, phone := range []string{"13800138001", "13800138002", "13800138003"} {
		if err := svc.UnsubscribePhone(context.Background(), phone, "reason"+string(rune('0'+i)), "", "TD"); err != nil {
			t.Fatalf("UnsubscribePhone failed: %v", err)
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

	records, total, err = svc.ListUnsubscribes(context.Background(), 1, 20, "13800138001")
	if err != nil {
		t.Fatalf("ListUnsubscribes with keyword failed: %v", err)
	}
	if total != 1 {
		t.Errorf("Expected total 1 for keyword, got %d", total)
	}
	if len(records) != 1 {
		t.Errorf("Expected 1 record for keyword, got %d", len(records))
	}
}

// TestSmsUnsubscribe_ListUnsubscribes_Pagination 测试分页
func TestSmsUnsubscribe_ListUnsubscribes_Pagination(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	for i := 0; i < 5; i++ {
		phone := "1390013900" + string(rune('0'+i))
		if err := svc.UnsubscribePhone(context.Background(), phone, "", "", "TD"); err != nil {
			t.Fatalf("UnsubscribePhone failed: %v", err)
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

// TestSmsUnsubscribe_ListUnsubscribes_DefaultPaging 测试默认分页参数
func TestSmsUnsubscribe_ListUnsubscribes_DefaultPaging(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	if err := svc.UnsubscribePhone(context.Background(), "13700137001", "", "", "TD"); err != nil {
		t.Fatalf("UnsubscribePhone failed: %v", err)
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

// TestSmsUnsubscribe_ListAllUnsubscribes 测试全量导出
func TestSmsUnsubscribe_ListAllUnsubscribes(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	for i := 0; i < 3; i++ {
		phone := "1360013600" + string(rune('0'+i))
		if err := svc.UnsubscribePhone(context.Background(), phone, "", "", "TD"); err != nil {
			t.Fatalf("UnsubscribePhone failed: %v", err)
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

// TestSmsUnsubscribe_FullLifecycle 测试完整生命周期
func TestSmsUnsubscribe_FullLifecycle(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	phone := "13500135001"

	if svc.IsUnsubscribed(context.Background(), phone) {
		t.Error("Initial state should be not unsubscribed")
	}

	matched, err := svc.ProcessUnsubscribeReply(context.Background(), phone, "TD", "msg-lc")
	if err != nil {
		t.Fatalf("ProcessUnsubscribeReply failed: %v", err)
	}
	if matched == "" {
		t.Error("Expected matched keyword for 'TD'")
	}

	if !svc.IsUnsubscribed(context.Background(), phone) {
		t.Error("Should be unsubscribed after ProcessUnsubscribeReply")
	}

	if !svc.IsUnsubscribed(context.Background(), phone) {
		t.Error("SendSms pre-check should block unsubscribed phone")
	}

	if err := svc.ResubscribePhone(context.Background(), phone); err != nil {
		t.Fatalf("ResubscribePhone failed: %v", err)
	}

	if svc.IsUnsubscribed(context.Background(), phone) {
		t.Error("Should be resubscribed after ResubscribePhone")
	}
}

// TestSmsUnsubscribe_FullFlow_AllKeywords 全部关键词回归测试
func TestSmsUnsubscribe_FullFlow_AllKeywords(t *testing.T) {
	database := setupSmsUnsubscribeTestDB(t)
	svc := newSmsUnsubscribeService(database)

	keywords := []string{"TD", "td", "退订", "T退", "取消", "N", "Q", "0", "STOP", "stop"}
	for i, kw := range keywords {
		phone := "1340013400" + string(rune('0'+i%10)) + string(rune('0'+i/10))
		matched, err := svc.ProcessUnsubscribeReply(context.Background(), phone, kw, "")
		if err != nil {
			t.Errorf("ProcessUnsubscribeReply failed for '%s': %v", kw, err)
			continue
		}
		if matched == "" {
			t.Errorf("Expected matched keyword for '%s'", kw)
		}
		if !svc.IsUnsubscribed(context.Background(), phone) {
			t.Errorf("Expected IsUnsubscribed=true after reply '%s' (phone=%s)", kw, phone)
		}
	}
}
