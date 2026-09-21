package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupEmailSendTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailSend{},
	)
	db.SetTestDB(database)
	return database
}

func setupEmailSendRepository(t *testing.T) EmailSendRepository {
	setupEmailSendTestDB(t)
	return NewEmailSendRepository()
}

// TestEmailSendRepository_Create 测试创建邮件发送记录
func TestEmailSendRepository_Create(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		email   *model.EmailSend
		wantErr bool
	}{
		{
			name: "create email success",
			email: &model.EmailSend{
				To:      "user@example.com",
				Subject: "Test Email",
				Content: "This is a test email content",
				Status:  0,
				SendTime: func() *time.Time {
					t := time.Now().Add(time.Hour)
					return &t
				}(),
				SmtpID: "smtp-1",
			},
			wantErr: false,
		},
		{
			name: "create email with attachments",
			email: &model.EmailSend{
				To:          "user@example.com",
				Subject:     "Email with attachments",
				Content:     "Email content",
				Attachments: "[\"file1.pdf\", \"file2.jpg\"]",
				Status:      0,
				SendTime: func() *time.Time {
					t := time.Now()
					return &t
				}(),
			},
			wantErr: false,
		},
		{
			name: "create sent email",
			email: &model.EmailSend{
				To:       "user@example.com",
				Subject:  "Already sent",
				Content:  "Content",
				Status:   1,
				SendTime: func() *time.Time { t := time.Now(); return &t }(),
				SmtpID:   "smtp-2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.email)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.email.ID == "" {
				t.Error("Expected email ID to be set after creation")
			}
		})
	}
}

// TestEmailSendRepository_GetByID 测试根据 ID 获取邮件发送记录
func TestEmailSendRepository_GetByID(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	email := &model.EmailSend{
		To:      "getbyid@example.com",
		Subject: "GetByID Test",
		Content: "Test content",
		Status:  0,
		SendTime: func() *time.Time {
			t := time.Now().Add(time.Hour)
			return &t
		}(),
	}
	repo.Create(ctx, email)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "get existing email",
			id:      email.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing email",
			id:      uuid.New().String(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := uuid.MustParse(tt.id)
			result, err := repo.GetByID(ctx, id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.To != "getbyid@example.com" {
					t.Errorf("Expected to 'getbyid@example.com', got '%s'", result.To)
				}
			}
		})
	}
}

// TestEmailSendRepository_List 测试获取邮件发送列表
func TestEmailSendRepository_List(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		repo.Create(ctx, &model.EmailSend{
			To:       "user" + string(rune('0'+i)) + "@example.com",
			Subject:  "Email " + string(rune('0'+i)),
			Content:  "Content " + string(rune('0'+i)),
			Status:   0,
			SendTime: func() *time.Time { t := time.Now(); return &t }(),
		})
	}

	results, err := repo.List(context.Background())
	if err != nil {
		t.Errorf("List() error = %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 emails, got %d", len(results))
	}
}

// TestEmailSendRepository_Delete 测试删除邮件发送记录
func TestEmailSendRepository_Delete(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	email := &model.EmailSend{
		To:      "delete@example.com",
		Subject: "To Delete",
		Content: "Delete content",
		Status:  0,
	}
	repo.Create(ctx, email)

	id := uuid.MustParse(email.ID)
	err := repo.Delete(ctx, id)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, id)
	if err == nil {
		t.Error("Expected email to be deleted")
	}
}

// TestEmailSendRepository_UpdateStatus 测试更新邮件状态
func TestEmailSendRepository_UpdateStatus(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	email := &model.EmailSend{
		To:      "update@example.com",
		Subject: "Status Update Test",
		Content: "Content",
		Status:  0,
	}
	repo.Create(ctx, email)

	id := uuid.MustParse(email.ID)

	err := repo.UpdateStatus(context.Background(), id, 1)
	if err != nil {
		t.Errorf("UpdateStatus() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, id)
	if updated.Status != 1 {
		t.Errorf("Expected status 1, got %d", updated.Status)
	}

	err = repo.UpdateStatus(context.Background(), id, 2)
	if err != nil {
		t.Errorf("UpdateStatus() error = %v", err)
	}

	updated2, _ := repo.GetByID(ctx, id)
	if updated2.Status != 2 {
		t.Errorf("Expected status 2, got %d", updated2.Status)
	}
}

// GetPendingEmails 那两条旧用例随该谓词一并退役：`status = 0 AND send_time <= now`
// 既没有认领（多副本会双投）、也没有龄上限（停机一周后重启会把老邮件一次性群发），
// 更漏掉 send_time IS NULL 的行。替代判据在 email_drain_test.go。

// TestEmailSendRepository_GetByID_NotFound 测试获取不存在的邮件
func TestEmailSendRepository_GetByID_NotFound(t *testing.T) {
	repo := setupEmailSendRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())
	if err == nil {
		t.Error("Expected error when getting non-existing email")
	}
}

// TestEmailSendRepository_List_EmptyResult 测试获取空列表
func TestEmailSendRepository_List_EmptyResult(t *testing.T) {
	repo := setupEmailSendRepository(t)

	results, err := repo.List(context.Background())
	if err != nil {
		t.Errorf("List() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 emails, got %d", len(results))
	}
}
