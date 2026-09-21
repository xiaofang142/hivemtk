package service

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

func setupEmailTrackingTestDB(t *testing.T) *gorm.DB {
	// EMAIL_TRACKING_SECRET 未配置时签发/校验一律 fail-closed（见 email_secret_guard_test.go）。
	// 本文件多数用例写成 `token, _ := Generate...` 把签发错误吞掉了 —— 缺密钥时它们
	// 只能靠空密钥自签自验才侥幸绿，故这里显式配好密钥，不再依赖进程环境。
	t.Setenv("EMAIL_TRACKING_SECRET", "test-tracking-secret")
	database := testutil.NewTestDB(t,
		&model.EmailTrackingEvent{},
		&model.EmailJobMetric{},
	)
	db.SetTestDB(database)
	return database
}

func newEmailTrackingService(database *gorm.DB) *EmailTrackingService {
	return NewEmailTrackingService(repository.NewEmailTrackingRepository(database))
}

// TestEmailTracking_NewService 测试创建服务
func TestEmailTracking_NewService(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)
	if svc == nil {
		t.Fatal("Expected non-nil service")
	}
}

// TestEmailTracking_GeneratePixelToken 测试生成像素 token
func TestEmailTracking_GeneratePixelToken(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	token, err := svc.GenerateTrackingPixelToken(context.Background(), "Open@Demo.com", "job-1")
	if err != nil {
		t.Fatalf("GenerateTrackingPixelToken failed: %v", err)
	}
	if !strings.Contains(token, ".") {
		t.Errorf("Expected token contains '.', got %s", token)
	}
}

// TestEmailTracking_GeneratePixelToken_EmptyEmail 测试空邮箱
func TestEmailTracking_GeneratePixelToken_EmptyEmail(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	_, err := svc.GenerateTrackingPixelToken(context.Background(), "", "job-1")
	if err == nil {
		t.Error("Expected error for empty email")
	}
}

// TestEmailTracking_GenerateClickLink 测试生成点击追踪链接
func TestEmailTracking_GenerateClickLink(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	link, err := svc.GenerateClickTrackingLink(context.Background(), "click@demo.com", "job-2", "https://example.com/landing")
	if err != nil {
		t.Fatalf("GenerateClickTrackingLink failed: %v", err)
	}
	if !strings.Contains(link, "/api/email/track/click/") {
		t.Errorf("链接缺少 click 路径, got: %s", link)
	}
	if !strings.Contains(link, "url=") {
		t.Errorf("链接缺少 url 参数, got: %s", link)
	}
}

// TestEmailTracking_VerifyToken_Valid 测试有效 token 校验
func TestEmailTracking_VerifyToken_Valid(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	token, _ := svc.GenerateTrackingPixelToken(context.Background(), "verify@demo.com", "job-3")
	claim, err := svc.VerifyTrackingToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyTrackingToken failed: %v", err)
	}
	if claim.Email != "verify@demo.com" {
		t.Errorf("Expected email 'verify@demo.com', got %s", claim.Email)
	}
	if claim.JobID != "job-3" {
		t.Errorf("Expected JobID 'job-3', got %s", claim.JobID)
	}
	if claim.Type != model.EmailEventTypeOpen {
		t.Errorf("Expected type 'open', got %s", claim.Type)
	}
}

// TestEmailTracking_VerifyToken_Invalid 测试无效 token
func TestEmailTracking_VerifyToken_Invalid(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	_, err := svc.VerifyTrackingToken(context.Background(), "malformed")
	if err == nil {
		t.Error("Expected error for malformed token")
	}
}

// TestEmailTracking_VerifyToken_Tampered 测试签名篡改
func TestEmailTracking_VerifyToken_Tampered(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	token, _ := svc.GenerateTrackingPixelToken(context.Background(), "tamper@demo.com", "job-4")
	tampered := token[:len(token)-5] + "XXXXX"
	_, err := svc.VerifyTrackingToken(context.Background(), tampered)
	if err == nil {
		t.Error("Expected error for tampered token")
	}
}

// TestEmailTracking_VerifyToken_Empty 测试空 token
func TestEmailTracking_VerifyToken_Empty(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	_, err := svc.VerifyTrackingToken(context.Background(), "")
	if err == nil {
		t.Error("Expected error for empty token")
	}
}

// TestEmailTracking_RecordOpenEvent 测试记录打开事件
func TestEmailTracking_RecordOpenEvent(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	token, _ := svc.GenerateTrackingPixelToken(context.Background(), "open-event@demo.com", "job-5")
	if err := svc.RecordOpenEvent(context.Background(), token, "127.0.0.1", "Mozilla/5.0"); err != nil {
		t.Fatalf("RecordOpenEvent failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailTrackingEvent{}).Where("email = ? AND event_type = ?", "open-event@demo.com", model.EmailEventTypeOpen).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 open event, got %d", count)
	}
}

// TestEmailTracking_RecordOpenEvent_InvalidToken 测试无效 token
func TestEmailTracking_RecordOpenEvent_InvalidToken(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	err := svc.RecordOpenEvent(context.Background(), "invalid.token", "", "")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

// TestEmailTracking_RecordClickEvent 测试记录点击事件 + 返回目标 URL
func TestEmailTracking_RecordClickEvent(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	link, _ := svc.GenerateClickTrackingLink(context.Background(), "click-event@demo.com", "job-6", "https://example.com/x")
	idx := strings.Index(link, "/click/") + len("/click/")
	endIdx := strings.Index(link[idx:], "?")
	if endIdx < 0 {
		endIdx = len(link) - idx
	}
	token := link[idx : idx+endIdx]

	target, err := svc.RecordClickEvent(context.Background(), token, "127.0.0.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("RecordClickEvent failed: %v", err)
	}
	if target != "https://example.com/x" {
		t.Errorf("Expected target 'https://example.com/x', got %s", target)
	}

	var count int64
	database.Model(&model.EmailTrackingEvent{}).Where("email = ? AND event_type = ?", "click-event@demo.com", model.EmailEventTypeClick).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 click event, got %d", count)
	}
}

// TestEmailTracking_RecordBounceEvent 测试记录退信事件
func TestEmailTracking_RecordBounceEvent(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	if err := svc.RecordBounceEvent(context.Background(), "bounce@demo.com", "job-7", "", ""); err != nil {
		t.Fatalf("RecordBounceEvent failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailTrackingEvent{}).Where("email = ? AND event_type = ?", "bounce@demo.com", model.EmailEventTypeBounce).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 bounce event, got %d", count)
	}
}

// TestEmailTracking_RecordUnsubscribeEvent 测试记录退订事件
func TestEmailTracking_RecordUnsubscribeEvent(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	if err := svc.RecordUnsubscribeEvent(context.Background(), "unsub@demo.com", "job-8", "", ""); err != nil {
		t.Fatalf("RecordUnsubscribeEvent failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailTrackingEvent{}).Where("email = ? AND event_type = ?", "unsub@demo.com", model.EmailEventTypeUnsubscribe).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 unsubscribe event, got %d", count)
	}
}

// TestEmailTracking_GetJobMetrics 测试任务指标
func TestEmailTracking_GetJobMetrics(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	jobID := "job-metrics"
	for _, email := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		if err := svc.recordEvent(context.Background(), email, jobID, model.EmailEventTypeOpen, "", "", ""); err != nil {
			t.Fatalf("recordEvent open failed: %v", err)
		}
	}
	if err := svc.recordEvent(context.Background(), "a@x.com", jobID, model.EmailEventTypeOpen, "", "", ""); err != nil {
		t.Fatalf("recordEvent open repeat failed: %v", err)
	}
	for _, email := range []string{"a@x.com", "b@x.com"} {
		if err := svc.recordEvent(context.Background(), email, jobID, model.EmailEventTypeClick, "", "", ""); err != nil {
			t.Fatalf("recordEvent click failed: %v", err)
		}
	}
	if err := svc.recordEvent(context.Background(), "d@x.com", jobID, model.EmailEventTypeBounce, "", "", ""); err != nil {
		t.Fatalf("recordEvent bounce failed: %v", err)
	}

	if err := svc.repo.UpsertJobMetric(context.Background(), &model.EmailJobMetric{JobID: jobID, TotalSent: 10}); err != nil {
		t.Fatalf("UpsertJobMetric failed: %v", err)
	}

	metric, err := svc.GetJobMetrics(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJobMetrics failed: %v", err)
	}
	if metric.TotalOpened != 3 {
		t.Errorf("Expected TotalOpened 3 (unique), got %d", metric.TotalOpened)
	}
	if metric.TotalClicked != 2 {
		t.Errorf("Expected TotalClicked 2 (unique), got %d", metric.TotalClicked)
	}
	if metric.TotalBounced != 1 {
		t.Errorf("Expected TotalBounced 1, got %d", metric.TotalBounced)
	}
	if metric.OpenRate != 30.00 {
		t.Errorf("Expected OpenRate 30.00, got %f", metric.OpenRate)
	}
	if metric.ClickRate != 20.00 {
		t.Errorf("Expected ClickRate 20.00, got %f", metric.ClickRate)
	}
}

// TestEmailTracking_GetJobMetrics_EmptyJob 测试无数据的任务
func TestEmailTracking_GetJobMetrics_EmptyJob(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	metric, err := svc.GetJobMetrics(context.Background(), "empty-job")
	if err != nil {
		t.Fatalf("GetJobMetrics failed: %v", err)
	}
	if metric == nil {
		t.Fatal("Expected non-nil metric")
	}
	if metric.TotalOpened != 0 {
		t.Errorf("Expected TotalOpened 0, got %d", metric.TotalOpened)
	}
}

// TestEmailTracking_GetJobMetrics_EmptyJobID 测试空 job_id
func TestEmailTracking_GetJobMetrics_EmptyJobID(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	_, err := svc.GetJobMetrics(context.Background(), "")
	if err == nil {
		t.Error("Expected error for empty job_id")
	}
}

// TestEmailTracking_GetEmailMetrics 测试区间指标
func TestEmailTracking_GetEmailMetrics(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)

	if err := svc.recordEvent(context.Background(), "r1@x.com", "job-r1", model.EmailEventTypeOpen, "", "", ""); err != nil {
		t.Fatalf("recordEvent failed: %v", err)
	}
	if err := svc.recordEvent(context.Background(), "r2@x.com", "job-r2", model.EmailEventTypeClick, "", "", ""); err != nil {
		t.Fatalf("recordEvent failed: %v", err)
	}
	if err := svc.recordEvent(context.Background(), "r3@x.com", "job-r3", model.EmailEventTypeBounce, "", "", ""); err != nil {
		t.Fatalf("recordEvent failed: %v", err)
	}

	metric, err := svc.GetEmailMetrics(context.Background(), start, end)
	if err != nil {
		t.Fatalf("GetEmailMetrics failed: %v", err)
	}
	if metric.TotalSent != 3 {
		t.Errorf("Expected TotalSent 3, got %d", metric.TotalSent)
	}
	if metric.TotalOpened != 1 {
		t.Errorf("Expected TotalOpened 1, got %d", metric.TotalOpened)
	}
	if metric.TotalClicked != 1 {
		t.Errorf("Expected TotalClicked 1, got %d", metric.TotalClicked)
	}
	if metric.TotalBounced != 1 {
		t.Errorf("Expected TotalBounced 1, got %d", metric.TotalBounced)
	}
}

// TestEmailTracking_GetEmailMetrics_InvalidRange 测试无效区间
func TestEmailTracking_GetEmailMetrics_InvalidRange(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	now := time.Now()
	_, err := svc.GetEmailMetrics(context.Background(), now, now.Add(-1*time.Hour))
	if err == nil {
		t.Error("Expected error for invalid range")
	}
}

// TestEmailTracking_GetEmailMetrics_EmptyRange 测试空区间
func TestEmailTracking_GetEmailMetrics_EmptyRange(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	_, err := svc.GetEmailMetrics(context.Background(), time.Time{}, time.Time{})
	if err == nil {
		t.Error("Expected error for empty range")
	}
}

// TestEmailTracking_RefreshJobMetrics 测试定时任务刷新指标
func TestEmailTracking_RefreshJobMetrics(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	jobID := "job-refresh"
	if err := svc.recordEvent(context.Background(), "a@x.com", jobID, model.EmailEventTypeOpen, "", "", ""); err != nil {
		t.Fatalf("recordEvent failed: %v", err)
	}
	if err := svc.recordEvent(context.Background(), "a@x.com", jobID, model.EmailEventTypeClick, "", "", ""); err != nil {
		t.Fatalf("recordEvent failed: %v", err)
	}

	if err := svc.RefreshJobMetrics(context.Background(), jobID, 5); err != nil {
		t.Fatalf("RefreshJobMetrics failed: %v", err)
	}

	var metric model.EmailJobMetric
	if err := database.Where("job_id = ?", jobID).First(&metric).Error; err != nil {
		t.Fatalf("查询指标失败: %v", err)
	}
	if metric.TotalSent != 5 {
		t.Errorf("Expected TotalSent 5, got %d", metric.TotalSent)
	}
	if metric.TotalOpened != 1 {
		t.Errorf("Expected TotalOpened 1, got %d", metric.TotalOpened)
	}
	if metric.OpenRate != 20.00 {
		t.Errorf("Expected OpenRate 20.00, got %f", metric.OpenRate)
	}
}

// TestEmailTracking_ListJobEvents 测试事件分页查询
func TestEmailTracking_ListJobEvents(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	jobID := "job-list"
	emails := []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com"}
	for _, e := range emails {
		if err := svc.recordEvent(context.Background(), e, jobID, model.EmailEventTypeOpen, "", "", ""); err != nil {
			t.Fatalf("recordEvent failed: %v", err)
		}
	}

	if err := svc.recordEvent(context.Background(), "a@x.com", jobID, model.EmailEventTypeOpen, "", "", ""); err != nil {
		t.Fatalf("recordEvent dup failed: %v", err)
	}
	totalAll, err := svc.repo.CountEventsByJob(context.Background(), jobID, model.EmailEventTypeOpen)
	if err != nil {
		t.Fatalf("CountEventsByJob failed: %v", err)
	}
	if totalAll != 5 {
		t.Fatalf("重复记录未被去重，期望 5 条，实际 %d", totalAll)
	}

	events, total, err := svc.ListJobEvents(context.Background(), jobID, 1, 2)
	if err != nil {
		t.Fatalf("ListJobEvents failed: %v", err)
	}
	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events on page 1, got %d", len(events))
	}
}

// TestEmailTracking_Round2 测试 round2 工具函数
func TestEmailTracking_Round2(t *testing.T) {
	if round2(33.333) != 33.33 {
		t.Errorf("round2(33.333) failed, got %f", round2(33.333))
	}
	if round2(0) != 0 {
		t.Errorf("round2(0) failed, got %f", round2(0))
	}
	if round2(99.999) != 100.00 {
		t.Errorf("round2(99.999) failed, got %f", round2(99.999))
	}
}

// TestEmailTracking_FullFlow 测试完整追踪流程
func TestEmailTracking_FullFlow(t *testing.T) {
	database := setupEmailTrackingTestDB(t)
	svc := newEmailTrackingService(database)

	email := "full-flow@demo.com"
	jobID := "job-full"

	pixelToken, err := svc.GenerateTrackingPixelToken(context.Background(), email, jobID)
	if err != nil {
		t.Fatalf("GenerateTrackingPixelToken failed: %v", err)
	}

	if err := svc.RecordOpenEvent(context.Background(), pixelToken, "10.0.0.1", "UA-1"); err != nil {
		t.Fatalf("RecordOpenEvent failed: %v", err)
	}

	clickLink, err := svc.GenerateClickTrackingLink(context.Background(), email, jobID, "https://target.example.com")
	if err != nil {
		t.Fatalf("GenerateClickTrackingLink failed: %v", err)
	}

	idx := strings.Index(clickLink, "/click/") + len("/click/")
	endIdx := strings.Index(clickLink[idx:], "?")
	if endIdx < 0 {
		endIdx = len(clickLink) - idx
	}
	clickToken := clickLink[idx : idx+endIdx]

	target, err := svc.RecordClickEvent(context.Background(), clickToken, "10.0.0.1", "UA-1")
	if err != nil {
		t.Fatalf("RecordClickEvent failed: %v", err)
	}
	if target != "https://target.example.com" {
		t.Errorf("Expected target 'https://target.example.com', got %s", target)
	}

	metric, err := svc.GetJobMetrics(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJobMetrics failed: %v", err)
	}
	if metric.TotalOpened != 1 {
		t.Errorf("Expected TotalOpened 1, got %d", metric.TotalOpened)
	}
	if metric.TotalClicked != 1 {
		t.Errorf("Expected TotalClicked 1, got %d", metric.TotalClicked)
	}

	if err := svc.RefreshJobMetrics(context.Background(), jobID, 1); err != nil {
		t.Fatalf("RefreshJobMetrics failed: %v", err)
	}
	metric, _ = svc.GetJobMetrics(context.Background(), jobID)
	if metric.OpenRate != 100.00 {
		t.Errorf("Expected OpenRate 100, got %f", metric.OpenRate)
	}
}
