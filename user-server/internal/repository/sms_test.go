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

func setupSmsTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SmsConfig{},
		&model.SmsAliyunConfig{},
		&model.SmsTencentConfig{},
		&model.SmsHuaweiConfig{},
		&model.SmsRecord{},
		&model.SmsDraft{},
		&model.SmsJob{},
		&model.SmsJobDetail{},
	)
	db.SetTestDB(database)
	return database
}

func setupSmsRepository(t *testing.T) SmsRepository {
	setupSmsTestDB(t)
	return NewSmsRepository()
}

// TestSmsRepository_GetConfig 测试获取短信配置
func TestSmsRepository_GetConfig(t *testing.T) {
	repo := setupSmsRepository(t)

	config, err := repo.GetConfig(context.Background())
	if err != nil {
		t.Errorf("GetConfig() error = %v", err)
	}

	if config.DefaultProvider != "aliyun" {
		t.Errorf("Expected default provider 'aliyun', got '%s'", config.DefaultProvider)
	}
	if config.RateLimit != 100 {
		t.Errorf("Expected rate limit 100, got %d", config.RateLimit)
	}

	newConfig := &model.SmsConfig{
		DefaultProvider: "tencent",
		RateLimit:       200,
		DailyLimit:      20000,
		RetryTimes:      5,
	}
	err = repo.SaveConfig(context.Background(), newConfig)
	if err != nil {
		t.Errorf("SaveConfig() error = %v", err)
	}

	config, err = repo.GetConfig(context.Background())
	if err != nil {
		t.Errorf("GetConfig() error = %v", err)
	}

	if config.DefaultProvider != "tencent" {
		t.Errorf("Expected provider 'tencent', got '%s'", config.DefaultProvider)
	}
	if config.RateLimit != 200 {
		t.Errorf("Expected rate limit 200, got %d", config.RateLimit)
	}
}

// TestSmsRepository_SaveConfig 测试保存短信配置
func TestSmsRepository_SaveConfig(t *testing.T) {
	repo := setupSmsRepository(t)

	tests := []struct {
		name    string
		config  *model.SmsConfig
		wantErr bool
	}{
		{
			name: "save config success",
			config: &model.SmsConfig{
				DefaultProvider: "aliyun",
				RateLimit:       100,
			},
			wantErr: false,
		},
		{
			name: "update existing config",
			config: &model.SmsConfig{
				DefaultProvider: "huawei",
				RateLimit:       150,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.SaveConfig(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("SaveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSmsRepository_AliyunConfig 测试阿里云短信配置
func TestSmsRepository_AliyunConfig(t *testing.T) {
	repo := setupSmsRepository(t)

	config, err := repo.GetAliyunConfig(context.Background())
	if err != nil {
		t.Errorf("GetAliyunConfig() error = %v", err)
	}
	if config == nil {
		t.Error("Expected config, got nil")
	}

	newConfig := &model.SmsAliyunConfig{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-secret",
		SignName:        "Test Sign",
	}
	err = repo.SaveAliyunConfig(context.Background(), newConfig)
	if err != nil {
		t.Errorf("SaveAliyunConfig() error = %v", err)
	}

	config, err = repo.GetAliyunConfig(context.Background())
	if err != nil {
		t.Errorf("GetAliyunConfig() error = %v", err)
	}
	if config.AccessKeyID != "test-key-id" {
		t.Errorf("Expected AccessKeyID 'test-key-id', got '%s'", config.AccessKeyID)
	}

	newConfig.AccessKeyID = "updated-key-id"
	err = repo.SaveAliyunConfig(context.Background(), newConfig)
	if err != nil {
		t.Errorf("SaveAliyunConfig() update error = %v", err)
	}

	config, _ = repo.GetAliyunConfig(context.Background())
	if config.AccessKeyID != "updated-key-id" {
		t.Errorf("Expected updated AccessKeyID 'updated-key-id', got '%s'", config.AccessKeyID)
	}
}

// TestSmsRepository_TencentConfig 测试腾讯云短信配置
func TestSmsRepository_TencentConfig(t *testing.T) {
	repo := setupSmsRepository(t)

	config := &model.SmsTencentConfig{
		SecretID:  "test" + "-secret-id",
		SecretKey: "test" + "-secret-key",
		AppID:     "123456",
		SignName:  "Test Sign",
	}

	err := repo.SaveTencentConfig(context.Background(), config)
	if err != nil {
		t.Errorf("SaveTencentConfig() error = %v", err)
	}

	retrieved, err := repo.GetTencentConfig(context.Background())
	if err != nil {
		t.Errorf("GetTencentConfig() error = %v", err)
	}
	if retrieved.SecretID != "test-secret-id" {
		t.Errorf("Expected SecretID 'test-secret-id', got '%s'", retrieved.SecretID)
	}
}

// TestSmsRepository_HuaweiConfig 测试华为云短信配置
func TestSmsRepository_HuaweiConfig(t *testing.T) {
	repo := setupSmsRepository(t)

	config := &model.SmsHuaweiConfig{
		AppKey:    "test-app-key",
		AppSecret: "test-app-secret",
		Sender:    "+8612345678901",
		Signature: "Test Signature",
	}

	err := repo.SaveHuaweiConfig(context.Background(), config)
	if err != nil {
		t.Errorf("SaveHuaweiConfig() error = %v", err)
	}

	retrieved, err := repo.GetHuaweiConfig(context.Background())
	if err != nil {
		t.Errorf("GetHuaweiConfig() error = %v", err)
	}
	if retrieved.AppKey != "test-app-key" {
		t.Errorf("Expected AppKey 'test-app-key', got '%s'", retrieved.AppKey)
	}
}

// TestSmsRepository_CreateSmsRecord 测试创建短信记录
func TestSmsRepository_CreateSmsRecord(t *testing.T) {
	repo := setupSmsRepository(t)

	tests := []struct {
		name    string
		record  *model.SmsRecord
		wantErr bool
	}{
		{
			name: "create sms record success",
			record: &model.SmsRecord{
				Phone:    "13800138000",
				Content:  "Test message",
				Provider: "aliyun",
				Status:   "pending",
			},
			wantErr: false,
		},
		{
			name: "create sms record with error",
			record: &model.SmsRecord{
				Phone:     "13900139000",
				Content:   "Failed message",
				Provider:  "tencent",
				Status:    "failed",
				ErrorCode: "ERROR_001",
				ErrorMsg:  "Test error",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateSmsRecord(context.Background(), tt.record)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateSmsRecord() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.record.ID == 0 {
				t.Error("Expected record ID to be set after creation")
			}
		})
	}
}

// TestSmsRepository_GetSmsByID 测试根据 ID 获取短信
func TestSmsRepository_GetSmsByID(t *testing.T) {
	repo := setupSmsRepository(t)

	record := &model.SmsRecord{
		Phone:    "13800138000",
		Content:  "GetByID Test",
		Provider: "aliyun",
		Status:   "sent",
	}
	repo.CreateSmsRecord(context.Background(), record)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing record",
			id:      record.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing record",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetSmsByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetSmsByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.Content != "GetByID Test" {
					t.Errorf("Expected content 'GetByID Test', got '%s'", result.Content)
				}
			}
		})
	}
}

// TestSmsRepository_GetSmsList 测试获取短信列表
func TestSmsRepository_GetSmsList(t *testing.T) {
	repo := setupSmsRepository(t)

	for i := 1; i <= 5; i++ {
		repo.CreateSmsRecord(context.Background(), &model.SmsRecord{
			Phone:    "13800138000",
			Content:  string(rune('A' + i - 1)),
			Provider: "aliyun",
			Status:   "sent",
		})
	}
	repo.CreateSmsRecord(context.Background(), &model.SmsRecord{
		Phone:    "13900139000",
		Content:  "Other phone",
		Provider: "tencent",
		Status:   "failed",
	})

	tests := []struct {
		name      string
		page      int
		limit     int
		phone     string
		status    string
		startDate string
		endDate   string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "get first page",
			page:      1,
			limit:     3,
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "get second page",
			page:      2,
			limit:     3,
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "filter by phone",
			page:      1,
			limit:     10,
			phone:     "13800138000",
			wantCount: 5,
			wantErr:   false,
		},
		{
			name:      "filter by status",
			page:      1,
			limit:     10,
			status:    "sent",
			wantCount: 5,
			wantErr:   false,
		},
		{
			name:      "filter by failed status",
			page:      1,
			limit:     10,
			status:    "failed",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetSmsList(context.Background(), tt.page, tt.limit, tt.phone, tt.status, tt.startDate, tt.endDate)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetSmsList() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.page == 1 && tt.phone == "" && tt.status == "" && int(total) != 6 {
				t.Errorf("Expected total 6, got %d", total)
			}
		})
	}
}

// TestSmsRepository_UpdateSmsRecord 测试更新短信记录
func TestSmsRepository_UpdateSmsRecord(t *testing.T) {
	repo := setupSmsRepository(t)

	record := &model.SmsRecord{
		Phone:    "13800138000",
		Content:  "Original content",
		Provider: "aliyun",
		Status:   "pending",
	}
	repo.CreateSmsRecord(context.Background(), record)

	record.Status = "sent"
	record.SendTime = &time.Time{}

	err := repo.UpdateSmsRecord(context.Background(), record)
	if err != nil {
		t.Errorf("UpdateSmsRecord() error = %v", err)
	}

	updated, _ := repo.GetSmsByID(context.Background(), record.ID)
	if updated.Status != "sent" {
		t.Errorf("Expected status 'sent', got '%s'", updated.Status)
	}
}

// TestSmsRepository_CreateDraft 测试创建草稿
func TestSmsRepository_CreateDraft(t *testing.T) {
	repo := setupSmsRepository(t)

	draft := &model.SmsDraft{
		Title:   "Test Draft",
		Content: "Test content",
	}

	err := repo.CreateDraft(context.Background(), draft)
	if err != nil {
		t.Errorf("CreateDraft() error = %v", err)
	}

	if draft.ID == 0 {
		t.Error("Expected draft ID to be set after creation")
	}
}

// TestSmsRepository_GetDraftByID 测试根据 ID 获取草稿
func TestSmsRepository_GetDraftByID(t *testing.T) {
	repo := setupSmsRepository(t)

	draft := &model.SmsDraft{
		Title:   "GetByID Draft",
		Content: "GetByID content",
	}
	repo.CreateDraft(context.Background(), draft)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing draft",
			id:      draft.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing draft",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetDraftByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetDraftByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.Title != "GetByID Draft" {
					t.Errorf("Expected title 'GetByID Draft', got '%s'", result.Title)
				}
			}
		})
	}
}

// TestSmsRepository_GetDraftList 测试获取草稿列表
func TestSmsRepository_GetDraftList(t *testing.T) {
	repo := setupSmsRepository(t)

	repo.CreateDraft(context.Background(), &model.SmsDraft{Title: "Draft A", Content: "Content A"})
	repo.CreateDraft(context.Background(), &model.SmsDraft{Title: "Draft B", Content: "Content B"})
	repo.CreateDraft(context.Background(), &model.SmsDraft{Title: "Draft C", Content: "Content C"})

	tests := []struct {
		name      string
		page      int
		limit     int
		title     string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "get first page",
			page:      1,
			limit:     2,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "get second page",
			page:      2,
			limit:     2,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "filter by title",
			page:      1,
			limit:     10,
			title:     "Draft A",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetDraftList(context.Background(), tt.page, tt.limit, tt.title)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetDraftList() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.page == 1 && tt.title == "" && int(total) != 3 {
				t.Errorf("Expected total 3, got %d", total)
			}
		})
	}
}

// TestSmsRepository_UpdateDraft 测试更新草稿
func TestSmsRepository_UpdateDraft(t *testing.T) {
	repo := setupSmsRepository(t)

	draft := &model.SmsDraft{
		Title:   "Original Title",
		Content: "Original content",
	}
	repo.CreateDraft(context.Background(), draft)

	draft.Title = "Updated Title"
	err := repo.UpdateDraft(context.Background(), draft)
	if err != nil {
		t.Errorf("UpdateDraft() error = %v", err)
	}

	updated, _ := repo.GetDraftByID(context.Background(), draft.ID)
	if updated.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got '%s'", updated.Title)
	}
}

// TestSmsRepository_DeleteDraft 测试删除草稿
func TestSmsRepository_DeleteDraft(t *testing.T) {
	repo := setupSmsRepository(t)

	draft := &model.SmsDraft{
		Title:   "To Delete",
		Content: "Delete content",
	}
	repo.CreateDraft(context.Background(), draft)

	err := repo.DeleteDraft(context.Background(), draft.ID)
	if err != nil {
		t.Errorf("DeleteDraft() error = %v", err)
	}

	_, err = repo.GetDraftByID(context.Background(), draft.ID)
	if err == nil {
		t.Error("Expected draft to be deleted")
	}
}

// TestSmsRepository_CreateJob 测试创建任务
func TestSmsRepository_CreateJob(t *testing.T) {
	repo := setupSmsRepository(t)

	now := time.Now()
	job := &model.SmsJob{
		Name:         "Test Job",
		Total:        100,
		Status:       "pending",
		ScheduleTime: &now,
	}

	err := repo.CreateJob(context.Background(), job)
	if err != nil {
		t.Errorf("CreateJob() error = %v", err)
	}

	if job.ID == 0 {
		t.Error("Expected job ID to be set after creation")
	}
}

// TestSmsRepository_GetJobByID 测试根据 ID 获取任务
func TestSmsRepository_GetJobByID(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{
		Name:   "GetByID Job",
		Total:  50,
		Status: "running",
	}
	repo.CreateJob(context.Background(), job)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing job",
			id:      job.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing job",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetJobByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetJobByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.Name != "GetByID Job" {
					t.Errorf("Expected name 'GetByID Job', got '%s'", result.Name)
				}
			}
		})
	}
}

// TestSmsRepository_GetJobList 测试获取任务列表
func TestSmsRepository_GetJobList(t *testing.T) {
	repo := setupSmsRepository(t)

	repo.CreateJob(context.Background(), &model.SmsJob{Name: "Job A", Total: 100, Status: "pending"})
	repo.CreateJob(context.Background(), &model.SmsJob{Name: "Job B", Total: 200, Status: "running"})
	repo.CreateJob(context.Background(), &model.SmsJob{Name: "Job C", Total: 300, Status: "completed"})

	tests := []struct {
		name       string
		page       int
		limit      int
		status     string
		filterName string
		wantCount  int
		wantErr    bool
	}{
		{
			name:      "get first page",
			page:      1,
			limit:     2,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "get second page",
			page:      2,
			limit:     2,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "filter by status",
			page:      1,
			limit:     10,
			status:    "running",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetJobList(context.Background(), tt.page, tt.limit, tt.status, tt.filterName)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetJobList() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.page == 1 && tt.status == "" && int(total) != 3 {
				t.Errorf("Expected total 3, got %d", total)
			}
		})
	}
}

// TestSmsRepository_UpdateJob 测试更新任务
func TestSmsRepository_UpdateJob(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{
		Name:   "Original Name",
		Total:  100,
		Status: "pending",
	}
	repo.CreateJob(context.Background(), job)

	job.Status = "running"
	job.Sent = 50

	err := repo.UpdateJob(context.Background(), job)
	if err != nil {
		t.Errorf("UpdateJob() error = %v", err)
	}

	updated, _ := repo.GetJobByID(context.Background(), job.ID)
	if updated.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", updated.Status)
	}
	if updated.Sent != 50 {
		t.Errorf("Expected sent 50, got %d", updated.Sent)
	}
}

// TestSmsRepository_DeleteJob 测试删除任务
func TestSmsRepository_DeleteJob(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{
		Name:   "To Delete",
		Total:  100,
		Status: "pending",
	}
	repo.CreateJob(context.Background(), job)

	err := repo.DeleteJob(context.Background(), job.ID)
	if err != nil {
		t.Errorf("DeleteJob() error = %v", err)
	}

	_, err = repo.GetJobByID(context.Background(), job.ID)
	if err == nil {
		t.Error("Expected job to be deleted")
	}
}

// TestSmsRepository_CreateJobDetails 测试创建任务详情
func TestSmsRepository_CreateJobDetails(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{Name: "Test Job", Total: 3}
	repo.CreateJob(context.Background(), job)

	details := []*model.SmsJobDetail{
		{JobID: job.ID, Phone: "13800138001", Content: "Message 1", Status: "pending"},
		{JobID: job.ID, Phone: "13800138002", Content: "Message 2", Status: "pending"},
		{JobID: job.ID, Phone: "13800138003", Content: "Message 3", Status: "pending"},
	}

	err := repo.CreateJobDetails(context.Background(), details)
	if err != nil {
		t.Errorf("CreateJobDetails() error = %v", err)
	}

	for _, d := range details {
		if d.ID == 0 {
			t.Error("Expected detail ID to be set after creation")
		}
	}
}

// TestSmsRepository_GetJobDetails 测试获取任务详情列表
func TestSmsRepository_GetJobDetails(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{Name: "Test Job", Total: 5}
	repo.CreateJob(context.Background(), job)

	details := []*model.SmsJobDetail{
		{JobID: job.ID, Phone: "13800138001", Status: "pending"},
		{JobID: job.ID, Phone: "13800138002", Status: "sent"},
		{JobID: job.ID, Phone: "13800138003", Status: "sent"},
		{JobID: job.ID, Phone: "13800138004", Status: "failed"},
		{JobID: job.ID, Phone: "13800138005", Status: "pending"},
	}
	repo.CreateJobDetails(context.Background(), details)

	tests := []struct {
		name      string
		jobID     uint
		page      int
		limit     int
		wantCount int
		wantErr   bool
	}{
		{
			name:      "get first page",
			jobID:     job.ID,
			page:      1,
			limit:     2,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "get second page",
			jobID:     job.ID,
			page:      2,
			limit:     2,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "get all details",
			jobID:     job.ID,
			page:      1,
			limit:     10,
			wantCount: 5,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetJobDetails(context.Background(), tt.jobID, tt.page, tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetJobDetails() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.page == 1 && int(total) != 5 {
				t.Errorf("Expected total 5, got %d", total)
			}
		})
	}
}

// TestSmsRepository_DeleteJobDetails 测试删除任务详情
func TestSmsRepository_DeleteJobDetails(t *testing.T) {
	repo := setupSmsRepository(t)

	job := &model.SmsJob{Name: "Test Job", Total: 3}
	repo.CreateJob(context.Background(), job)

	details := []*model.SmsJobDetail{
		{JobID: job.ID, Phone: "13800138001", Status: "pending"},
		{JobID: job.ID, Phone: "13800138002", Status: "pending"},
		{JobID: job.ID, Phone: "13800138003", Status: "pending"},
	}
	repo.CreateJobDetails(context.Background(), details)

	err := repo.DeleteJobDetails(context.Background(), job.ID)
	if err != nil {
		t.Errorf("DeleteJobDetails() error = %v", err)
	}

	results, _, _ := repo.GetJobDetails(context.Background(), job.ID, 1, 10)
	if len(results) != 0 {
		t.Errorf("Expected 0 details after deletion, got %d", len(results))
	}
}
