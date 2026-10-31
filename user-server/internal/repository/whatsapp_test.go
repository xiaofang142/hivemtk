package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupWhatsappTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.WhatsappAccount{},
		&model.WhatsappSession{},
		&model.WhatsappDraft{},
		&model.WhatsappJob{},
		&model.WhatsappJobDetail{},
	)
	db.SetTestDB(database)
	return database
}

func setupWhatsappRepository(t *testing.T) WhatsappRepository {
	setupWhatsappTestDB(t)
	return NewWhatsappRepository()
}

// TestWhatsappRepository_CreateAccount 测试创建 WhatsApp 账号
func TestWhatsappRepository_CreateAccount(t *testing.T) {
	repo := setupWhatsappRepository(t)

	tests := []struct {
		name    string
		account *model.WhatsappAccount
		wantErr bool
	}{
		{
			name: "create account success",
			account: &model.WhatsappAccount{
				Name:   "Test Account",
				Remark: "Test remark",
				Status: model.WhatsappStatusOnline,
			},
			wantErr: false,
		},
		{
			name: "create account with minimal fields",
			account: &model.WhatsappAccount{
				Name: "Minimal Account",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateAccount(context.Background(), tt.account)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateAccount() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.account.ID == (uuid.UUID{}) {
				t.Error("Expected account ID to be set after creation")
			}
		})
	}
}

// TestWhatsappRepository_GetAccount 测试根据 ID 获取账号
func TestWhatsappRepository_GetAccount(t *testing.T) {
	repo := setupWhatsappRepository(t)

	account := &model.WhatsappAccount{
		Name: "GetAccount Test",
	}
	repo.CreateAccount(context.Background(), account)

	tests := []struct {
		name    string
		id      uuid.UUID
		wantNil bool
	}{
		{
			name:    "get existing account",
			id:      account.ID,
			wantNil: false,
		},
		{
			name:    "get non-existing account",
			id:      uuid.New(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetAccount(context.Background(), tt.id)

			if err != nil {
				t.Errorf("GetAccount() error = %v", err)
			}

			if tt.wantNil && result != nil {
				t.Error("Expected nil for non-existing account")
			}

			if !tt.wantNil && result == nil {
				t.Error("Expected result to not be nil")
			}

			if !tt.wantNil && result.Name != "GetAccount Test" {
				t.Errorf("Expected name 'GetAccount Test', got '%s'", result.Name)
			}
		})
	}
}

func TestWhatsappRepository_ListAccounts(t *testing.T) {
	repo := setupWhatsappRepository(t)

	for i := 1; i <= 3; i++ {
		repo.CreateAccount(context.Background(), &model.WhatsappAccount{
			Name: "Account " + string(rune('0'+i)),
		})
	}
	repo.CreateAccount(context.Background(), &model.WhatsappAccount{
		Name: "Other Account",
	})

	accounts, err := repo.ListAccounts(context.Background())
	if err != nil {
		t.Errorf("ListAccounts() error = %v", err)
	}

	if len(accounts) != 4 {
		t.Errorf("Expected 4 accounts, got %d", len(accounts))
	}
}

// TestWhatsappRepository_UpdateAccount 测试更新账号
func TestWhatsappRepository_UpdateAccount(t *testing.T) {
	repo := setupWhatsappRepository(t)

	account := &model.WhatsappAccount{
		Name:   "Original Name",
		Status: model.WhatsappStatusPending,
	}
	repo.CreateAccount(context.Background(), account)

	account.Remark = "Updated remark"
	account.Status = model.WhatsappStatusOnline

	err := repo.UpdateAccount(context.Background(), account)
	if err != nil {
		t.Errorf("UpdateAccount() error = %v", err)
	}

	updated, _ := repo.GetAccount(context.Background(), account.ID)
	if updated.Remark != "Updated remark" {
		t.Errorf("Expected remark 'Updated remark', got '%s'", updated.Remark)
	}
	if updated.Status != model.WhatsappStatusOnline {
		t.Errorf("Expected status Online, got %v", updated.Status)
	}
}

// TestWhatsappRepository_DeleteAccount 测试删除账号
func TestWhatsappRepository_DeleteAccount(t *testing.T) {
	repo := setupWhatsappRepository(t)

	account := &model.WhatsappAccount{
		Name: "To Delete",
	}
	repo.CreateAccount(context.Background(), account)

	err := repo.DeleteAccount(context.Background(), account.ID)
	if err != nil {
		t.Errorf("DeleteAccount() error = %v", err)
	}

	result, _ := repo.GetAccount(context.Background(), account.ID)
	if result != nil {
		t.Error("Expected account to be deleted")
	}
}

// TestWhatsappRepository_UpsertSession 测试创建/更新会话
func TestWhatsappRepository_UpsertSession(t *testing.T) {
	repo := setupWhatsappRepository(t)

	account := &model.WhatsappAccount{
		Name: "Session Test Account",
	}
	repo.CreateAccount(context.Background(), account)

	session := &model.WhatsappSession{
		AccountID:   account.ID.String(),
		SessionJSON: "session_json_123",
	}

	err := repo.UpsertSession(context.Background(), session)
	if err != nil {
		t.Errorf("UpsertSession() error = %v", err)
	}

	if session.ID == (uuid.UUID{}) {
		t.Error("Expected session ID to be set")
	}

	session.SessionJSON = "updated_session_json"
	err = repo.UpsertSession(context.Background(), session)
	if err != nil {
		t.Errorf("UpsertSession() update error = %v", err)
	}

	updated, _ := repo.GetSession(context.Background(), account.ID)
	if updated.SessionJSON != "updated_session_json" {
		t.Errorf("Expected session JSON 'updated_session_json', got '%s'", updated.SessionJSON)
	}
}

// TestWhatsappRepository_GetSession 测试获取会话
func TestWhatsappRepository_GetSession(t *testing.T) {
	repo := setupWhatsappRepository(t)

	account := &model.WhatsappAccount{
		Name: "GetSession Test Account",
	}
	repo.CreateAccount(context.Background(), account)

	expectedSession := &model.WhatsappSession{
		AccountID:   account.ID.String(),
		SessionJSON: "test_session",
	}
	repo.UpsertSession(context.Background(), expectedSession)

	tests := []struct {
		name       string
		accountID  uuid.UUID
		merchantID string
		wantNil    bool
	}{
		{
			name:      "get existing session",
			accountID: account.ID,

			wantNil: false,
		},
		{
			name:      "get non-existing session",
			accountID: uuid.New(),

			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetSession(context.Background(), tt.accountID)

			if err != nil {
				t.Errorf("GetSession() error = %v", err)
			}

			if tt.wantNil && result != nil {
				t.Error("Expected nil for non-existing session")
			}

			if !tt.wantNil && result == nil {
				t.Error("Expected result to not be nil")
			}

			if !tt.wantNil && result.SessionJSON != "test_session" {
				t.Errorf("Expected session JSON 'test_session', got '%s'", result.SessionJSON)
			}
		})
	}
}

// TestWhatsappRepository_CreateDraft 测试创建草稿
func TestWhatsappRepository_CreateDraft(t *testing.T) {
	repo := setupWhatsappRepository(t)

	tests := []struct {
		name    string
		draft   *model.WhatsappDraft
		wantErr bool
	}{
		{
			name: "create draft success",
			draft: &model.WhatsappDraft{
				Title:   "Test Draft",
				Content: "Test content",
			},
			wantErr: false,
		},
		{
			name: "create draft with minimal fields",
			draft: &model.WhatsappDraft{
				Title: "Minimal Draft",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateDraft(context.Background(), tt.draft)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateDraft() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.draft.ID == (uuid.UUID{}) {
				t.Error("Expected draft ID to be set after creation")
			}
		})
	}
}

// TestWhatsappRepository_GetDraft 测试获取草稿
func TestWhatsappRepository_GetDraft(t *testing.T) {
	repo := setupWhatsappRepository(t)

	draft := &model.WhatsappDraft{
		Title:   "GetDraft Test",
		Content: "Test content",
	}
	repo.CreateDraft(context.Background(), draft)

	tests := []struct {
		name    string
		id      uuid.UUID
		wantNil bool
	}{
		{
			name:    "get existing draft",
			id:      draft.ID,
			wantNil: false,
		},
		{
			name:    "get non-existing draft",
			id:      uuid.New(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetDraft(context.Background(), tt.id)

			if err != nil {
				t.Errorf("GetDraft() error = %v", err)
			}

			if tt.wantNil && result != nil {
				t.Error("Expected nil for non-existing draft")
			}

			if !tt.wantNil {
				if result.Title != "GetDraft Test" {
					t.Errorf("Expected title 'GetDraft Test', got '%s'", result.Title)
				}
			}
		})
	}
}

// TestWhatsappRepository_ListDrafts 测试获取草稿列表
func TestWhatsappRepository_ListDrafts(t *testing.T) {
	repo := setupWhatsappRepository(t)

	for i := 1; i <= 3; i++ {
		repo.CreateDraft(context.Background(), &model.WhatsappDraft{
			Title: "Draft " + string(rune('0'+i)),
		})
	}
	repo.CreateDraft(context.Background(), &model.WhatsappDraft{
		Title: "Other Draft",
	})

	drafts, total, err := repo.ListDrafts(context.Background(), 1, 50)
	if err != nil {
		t.Errorf("ListDrafts() error = %v", err)
	}

	if len(drafts) != 4 || total != 4 {
		t.Errorf("Expected 4 drafts/4 total, got %d/%d", len(drafts), total)
	}

	// S-31：分页语义钉住——第 2 页 pageSize=3 只剩 1 条，total 仍为全量。
	page2, total2, err := repo.ListDrafts(context.Background(), 2, 3)
	if err != nil {
		t.Errorf("ListDrafts(page=2) error = %v", err)
	}
	if len(page2) != 1 || total2 != 4 {
		t.Errorf("Expected 1 draft/4 total on page 2, got %d/%d", len(page2), total2)
	}
}

// TestWhatsappRepository_UpdateDraft 测试更新草稿
func TestWhatsappRepository_UpdateDraft(t *testing.T) {
	repo := setupWhatsappRepository(t)

	draft := &model.WhatsappDraft{
		Title:   "Original Name",
		Content: "Original content",
	}
	repo.CreateDraft(context.Background(), draft)

	draft.Content = "Updated content"

	err := repo.UpdateDraft(context.Background(), draft)
	if err != nil {
		t.Errorf("UpdateDraft() error = %v", err)
	}

	updated, _ := repo.GetDraft(context.Background(), draft.ID)
	if updated.Content != "Updated content" {
		t.Errorf("Expected content 'Updated content', got '%s'", updated.Content)
	}
}

// TestWhatsappRepository_DeleteDraft 测试删除草稿
func TestWhatsappRepository_DeleteDraft(t *testing.T) {
	repo := setupWhatsappRepository(t)

	draft := &model.WhatsappDraft{
		Title: "To Delete",
	}
	repo.CreateDraft(context.Background(), draft)

	err := repo.DeleteDraft(context.Background(), draft.ID)
	if err != nil {
		t.Errorf("DeleteDraft() error = %v", err)
	}

	result, _ := repo.GetDraft(context.Background(), draft.ID)
	if result != nil {
		t.Error("Expected draft to be deleted")
	}
}

// TestWhatsappRepository_CreateJob 测试创建任务
func TestWhatsappRepository_CreateJob(t *testing.T) {
	repo := setupWhatsappRepository(t)

	tests := []struct {
		name    string
		job     *model.WhatsappJob
		wantErr bool
	}{
		{
			name: "create job success",
			job: &model.WhatsappJob{
				DraftID: uuid.New(),
				Status:  model.WhatsappJobPending,
			},
			wantErr: false,
		},
		{
			name: "create job with minimal fields",
			job: &model.WhatsappJob{
				DraftID: uuid.New(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateJob(context.Background(), tt.job)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateJob() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.job.ID == (uuid.UUID{}) {
				t.Error("Expected job ID to be set after creation")
			}
		})
	}
}

// TestWhatsappRepository_GetJob 测试获取任务
func TestWhatsappRepository_GetJob(t *testing.T) {
	repo := setupWhatsappRepository(t)

	job := &model.WhatsappJob{
		DraftID: uuid.New(),
		Status:  model.WhatsappJobPending,
	}
	repo.CreateJob(context.Background(), job)

	tests := []struct {
		name    string
		id      uuid.UUID
		wantNil bool
	}{
		{
			name:    "get existing job",
			id:      job.ID,
			wantNil: false,
		},
		{
			name:    "get non-existing job",
			id:      uuid.New(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetJob(context.Background(), tt.id)

			if err != nil {
				t.Errorf("GetJob() error = %v", err)
			}

			if tt.wantNil && result != nil {
				t.Error("Expected nil for non-existing job")
			}

			if !tt.wantNil && result == nil {
				t.Error("Expected result to not be nil")
			}

			if !tt.wantNil && result.Status != model.WhatsappJobPending {
				t.Errorf("Expected status Pending, got %v", result.Status)
			}
		})
	}
}

// TestWhatsappRepository_ListJobs 测试获取任务列表
func TestWhatsappRepository_ListJobs(t *testing.T) {
	repo := setupWhatsappRepository(t)

	for i := 1; i <= 3; i++ {
		repo.CreateJob(context.Background(), &model.WhatsappJob{
			DraftID: uuid.New(),
		})
	}
	repo.CreateJob(context.Background(), &model.WhatsappJob{
		DraftID: uuid.New(),
	})

	jobs, total, err := repo.ListJobs(context.Background(), 1, 50)
	if err != nil {
		t.Errorf("ListJobs() error = %v", err)
	}

	if len(jobs) != 4 || total != 4 {
		t.Errorf("Expected 4 jobs/4 total, got %d/%d", len(jobs), total)
	}
}

// TestWhatsappRepository_UpdateJob 测试更新任务
func TestWhatsappRepository_UpdateJob(t *testing.T) {
	repo := setupWhatsappRepository(t)

	job := &model.WhatsappJob{
		DraftID: uuid.New(),
		Status:  model.WhatsappJobPending,
	}
	repo.CreateJob(context.Background(), job)

	job.Status = model.WhatsappJobFinished

	err := repo.UpdateJob(context.Background(), job)
	if err != nil {
		t.Errorf("UpdateJob() error = %v", err)
	}

	updated, _ := repo.GetJob(context.Background(), job.ID)
	if updated.Status != model.WhatsappJobFinished {
		t.Errorf("Expected status Finished, got %v", updated.Status)
	}
}

// TestWhatsappRepository_CreateJobDetail 测试创建任务详情
func TestWhatsappRepository_CreateJobDetail(t *testing.T) {
	repo := setupWhatsappRepository(t)

	job := &model.WhatsappJob{
		DraftID: uuid.New(),
	}
	repo.CreateJob(context.Background(), job)

	tests := []struct {
		name    string
		detail  *model.WhatsappJobDetail
		wantErr bool
	}{
		{
			name: "create job detail success",
			detail: &model.WhatsappJobDetail{
				JobID:     job.ID,
				AccountID: uuid.New(),
				ToJid:     "1234567890",
				Status:    model.WhatsappJobDetailPending,
			},
			wantErr: false,
		},
		{
			name: "create job detail with minimal fields",
			detail: &model.WhatsappJobDetail{
				JobID:     job.ID,
				AccountID: uuid.New(),
				ToJid:     "0987654321",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateJobDetail(context.Background(), tt.detail)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateJobDetail() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.detail.ID == (uuid.UUID{}) {
				t.Error("Expected job detail ID to be set after creation")
			}
		})
	}
}

// TestWhatsappRepository_UpdateJobDetail 测试更新任务详情
func TestWhatsappRepository_UpdateJobDetail(t *testing.T) {
	repo := setupWhatsappRepository(t)

	job := &model.WhatsappJob{
		DraftID: uuid.New(),
	}
	repo.CreateJob(context.Background(), job)

	detail := &model.WhatsappJobDetail{
		JobID:     job.ID,
		AccountID: uuid.New(),
		ToJid:     "1234567890",
		Status:    model.WhatsappJobDetailPending,
	}
	repo.CreateJobDetail(context.Background(), detail)

	detail.Status = model.WhatsappJobDetailSuccess
	detail.ErrorMsg = ""

	// UpdateJobDetail 是定向 Updates，无返回读口（原 ListJobDetails 已删）；
	// 断言降级为「不报错」——明细读取端点本就不存在（progress 走 queue status）。
	if err := repo.UpdateJobDetail(context.Background(), detail); err != nil {
		t.Errorf("UpdateJobDetail() error = %v", err)
	}
}
