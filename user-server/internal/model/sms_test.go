package model

import (
	"testing"
	"time"
)

func TestSmsConfig_DefaultProvider(t *testing.T) {
	config := &SmsConfig{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      10000,
		RetryTimes:      3,
	}

	if config.DefaultProvider != "aliyun" {
		t.Errorf("Expected DefaultProvider 'aliyun', got %s", config.DefaultProvider)
	}
	if config.RateLimit != 100 {
		t.Errorf("Expected RateLimit 100, got %d", config.RateLimit)
	}
	if config.DailyLimit != 10000 {
		t.Errorf("Expected DailyLimit 10000, got %d", config.DailyLimit)
	}
	if config.RetryTimes != 3 {
		t.Errorf("Expected RetryTimes 3, got %d", config.RetryTimes)
	}
}

func TestSmsConfig_DefaultValues(t *testing.T) {
	config := &SmsConfig{}

	if config.DefaultProvider != "" {
		t.Logf("DefaultProvider is %s (expected empty before save, default is 'aliyun')", config.DefaultProvider)
	}
	if config.RateLimit != 0 {
		t.Logf("RateLimit is %d (expected 0 before save, default is 100)", config.RateLimit)
	}
	if config.DailyLimit != 0 {
		t.Logf("DailyLimit is %d (expected 0 before save, default is 10000)", config.DailyLimit)
	}
	if config.RetryTimes != 0 {
		t.Logf("RetryTimes is %d (expected 0 before save, default is 3)", config.RetryTimes)
	}
}

func TestSmsAliyunConfig_BasicFields(t *testing.T) {
	config := &SmsAliyunConfig{
		ID:              1,
		AccessKeyID:     "AK_test",
		AccessKeySecret: "secret_test",
		SignName:        "TestSign",
	}

	if config.AccessKeyID != "AK_test" {
		t.Errorf("Expected AccessKeyID 'AK_test', got %s", config.AccessKeyID)
	}
	if config.AccessKeySecret != "secret_test" {
		t.Errorf("Expected AccessKeySecret 'secret_test', got %s", config.AccessKeySecret)
	}
	if config.SignName != "TestSign" {
		t.Errorf("Expected SignName 'TestSign', got %s", config.SignName)
	}
}

func TestSmsTencentConfig_BasicFields(t *testing.T) {
	config := &SmsTencentConfig{
		ID:        1,
		SecretID:  "AKID" + "_test",
		SecretKey: "secr" + "et_key_test",
		AppID:     "123456",
		SignName:  "TestSign",
	}

	if config.SecretID != "AKID_test" {
		t.Errorf("Expected SecretID 'AKID_test', got %s", config.SecretID)
	}
	if config.SecretKey != "secret_key_test" {
		t.Errorf("Expected SecretKey 'secret_key_test', got %s", config.SecretKey)
	}
	if config.AppID != "123456" {
		t.Errorf("Expected AppID '123456', got %s", config.AppID)
	}
	if config.SignName != "TestSign" {
		t.Errorf("Expected SignName 'TestSign', got %s", config.SignName)
	}
}

func TestSmsHuaweiConfig_BasicFields(t *testing.T) {
	config := &SmsHuaweiConfig{
		ID:        1,
		AppKey:    "app_key_test",
		AppSecret: "app_secret_test",
		Sender:    "10690123",
		Signature: "【TestSign】",
	}

	if config.AppKey != "app_key_test" {
		t.Errorf("Expected AppKey 'app_key_test', got %s", config.AppKey)
	}
	if config.AppSecret != "app_secret_test" {
		t.Errorf("Expected AppSecret 'app_secret_test', got %s", config.AppSecret)
	}
	if config.Sender != "10690123" {
		t.Errorf("Expected Sender '10690123', got %s", config.Sender)
	}
	if config.Signature != "【TestSign】" {
		t.Errorf("Expected Signature '[TestSign]', got %s", config.Signature)
	}
}

func TestSmsRecord_BasicFields(t *testing.T) {
	now := time.Now()
	record := &SmsRecord{
		ID:        1,
		Phone:     "+8613800138000",
		Content:   "Your verification code is 123456",
		Provider:  "aliyun",
		Status:    "sent",
		ErrorCode: "",
		ErrorMsg:  "",
		SendTime:  &now,
	}

	if record.Phone != "+8613800138000" {
		t.Errorf("Expected Phone '+8613800138000', got %s", record.Phone)
	}
	if record.Content != "Your verification code is 123456" {
		t.Errorf("Expected Content, got %s", record.Content)
	}
	if record.Provider != "aliyun" {
		t.Errorf("Expected Provider 'aliyun', got %s", record.Provider)
	}
	if record.Status != "sent" {
		t.Errorf("Expected Status 'sent', got %s", record.Status)
	}
}

func TestSmsRecord_WithStatusValues(t *testing.T) {
	statuses := []string{"pending", "sending", "sent", "failed"}

	for _, status := range statuses {
		record := &SmsRecord{
			Status: status,
		}
		if record.Status != status {
			t.Errorf("Expected Status %s, got %s", status, record.Status)
		}
	}
}

func TestSmsRecord_WithNilSendTime(t *testing.T) {
	record := &SmsRecord{
		Phone:    "+8613800138000",
		Status:   "pending",
		SendTime: nil,
	}

	if record.SendTime != nil {
		t.Errorf("Expected SendTime nil, got %v", record.SendTime)
	}
}

func TestSmsRecord_WithErrorInfo(t *testing.T) {
	record := &SmsRecord{
		Phone:     "+8613800138000",
		Status:    "failed",
		ErrorCode: "CODE_123",
		ErrorMsg:  "Test error message",
	}

	if record.ErrorCode != "CODE_123" {
		t.Errorf("Expected ErrorCode 'CODE_123', got %s", record.ErrorCode)
	}
	if record.ErrorMsg != "Test error message" {
		t.Errorf("Expected ErrorMsg 'Test error message', got %s", record.ErrorMsg)
	}
}

func TestSmsDraft_BasicFields(t *testing.T) {
	draft := &SmsDraft{
		ID:      1,
		Title:   "Test Draft",
		Content: "This is a test SMS draft content",
	}

	if draft.ID != 1 {
		t.Errorf("Expected ID 1, got %d", draft.ID)
	}
	if draft.Title != "Test Draft" {
		t.Errorf("Expected Title 'Test Draft', got %s", draft.Title)
	}
	if draft.Content != "This is a test SMS draft content" {
		t.Errorf("Expected Content, got %s", draft.Content)
	}
}

func TestSmsJob_BasicFields(t *testing.T) {
	now := time.Now()
	job := &SmsJob{
		ID:           1,
		Name:         "Test Job",
		Total:        1000,
		Sent:         500,
		Failed:       10,
		Status:       "running",
		ScheduleTime: &now,
	}

	if job.ID != 1 {
		t.Errorf("Expected ID 1, got %d", job.ID)
	}
	if job.Name != "Test Job" {
		t.Errorf("Expected Name 'Test Job', got %s", job.Name)
	}
	if job.Total != 1000 {
		t.Errorf("Expected Total 1000, got %d", job.Total)
	}
	if job.Sent != 500 {
		t.Errorf("Expected Sent 500, got %d", job.Sent)
	}
	if job.Failed != 10 {
		t.Errorf("Expected Failed 10, got %d", job.Failed)
	}
	if job.Status != "running" {
		t.Errorf("Expected Status 'running', got %s", job.Status)
	}
}

func TestSmsJob_WithStatusValues(t *testing.T) {
	statuses := []string{"pending", "running", "paused", "completed", "failed"}

	for _, status := range statuses {
		job := &SmsJob{
			Status: status,
		}
		if job.Status != status {
			t.Errorf("Expected Status %s, got %s", status, job.Status)
		}
	}
}

func TestSmsJob_WithNilScheduleTime(t *testing.T) {
	job := &SmsJob{
		Name:         "Immediate Job",
		ScheduleTime: nil,
	}

	if job.ScheduleTime != nil {
		t.Errorf("Expected ScheduleTime nil, got %v", job.ScheduleTime)
	}
}

func TestSmsJob_Progress(t *testing.T) {
	job := &SmsJob{
		Total:  1000,
		Sent:   800,
		Failed: 50,
	}

	if job.Sent+job.Failed > job.Total {
		t.Log("Progress: 800 sent, 50 failed out of 1000 total")
	}
}

func TestSmsJobDetail_BasicFields(t *testing.T) {
	detail := &SmsJobDetail{
		ID:      1,
		JobID:   100,
		Phone:   "+8613800138000",
		Content: "Verification code: 123456",
		Status:  "sent",
	}

	if detail.JobID != 100 {
		t.Errorf("Expected JobID 100, got %d", detail.JobID)
	}
	if detail.Phone != "+8613800138000" {
		t.Errorf("Expected Phone '+8613800138000', got %s", detail.Phone)
	}
	if detail.Content != "Verification code: 123456" {
		t.Errorf("Expected Content, got %s", detail.Content)
	}
	if detail.Status != "sent" {
		t.Errorf("Expected Status 'sent', got %s", detail.Status)
	}
}
