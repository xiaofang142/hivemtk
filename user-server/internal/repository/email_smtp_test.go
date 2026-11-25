package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupEmailSmtpTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailSmtp{},
	)
	db.SetTestDB(database)
	return database
}

func setupEmailSmtpRepository(t *testing.T) EmailSmtpRepository {
	setupEmailSmtpTestDB(t)
	return NewEmailSmtpRepository()
}

// TestEmailSmtpRepository_Create 测试创建 SMTP 配置
func TestEmailSmtpRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	tests := []struct {
		name    string
		smtp    *model.EmailSmtp
		wantErr bool
	}{
		{
			name: "create smtp success",
			smtp: &model.EmailSmtp{
				Name:     "Example SMTP",
				Server:   "smtp.example.com",
				Port:     587,
				Username: "user@example.com",
				Password: "pass" + "word123",
				Limit:    100,
			},
			wantErr: false,
		},
		{
			name: "create smtp with gmail",
			smtp: &model.EmailSmtp{
				Name:     "Gmail SMTP",
				Server:   "smtp.gmail.com",
				Port:     465,
				Username: "user@gmail.com",
				Password: "secu" + "re_password",
				Limit:    500,
			},
			wantErr: false,
		},
		{
			name: "create smtp with low limit",
			smtp: &model.EmailSmtp{
				Name:     "Old SMTP",
				Server:   "smtp.old.com",
				Port:     25,
				Username: "old@example.com",
				Password: "old_" + "password",
				Limit:    10,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.smtp)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.smtp.ID == "" {
				t.Error("Expected SMTP ID to be set after creation")
			}
		})
	}
}

// TestEmailSmtpRepository_GetByID 测试根据 ID 获取 SMTP 配置
func TestEmailSmtpRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	smtp := &model.EmailSmtp{
		Name:     "GetByID SMTP",
		Server:   "smtp.getbyid.com",
		Port:     587,
		Username: "getbyid@example.com",
		Password: "pass" + "word",
		Limit:    100,
	}
	repo.Create(ctx, smtp)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "get existing smtp",
			id:      smtp.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing smtp",
			id:      "non-existing-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Server != "smtp.getbyid.com" {
					t.Errorf("Expected server 'smtp.getbyid.com', got '%s'", result.Server)
				}
				if result.Port != 587 {
					t.Errorf("Expected port 587, got %d", result.Port)
				}
			}
		})
	}
}

// TestEmailSmtpRepository_GetEmailSmtpList 测试获取 SMTP 列表
func TestEmailSmtpRepository_GetEmailSmtpList(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	for i := 1; i <= 5; i++ {
		repo.Create(ctx, &model.EmailSmtp{
			Name:     "SMTP " + string(rune('0'+i)),
			Server:   "smtp" + string(rune('0'+i)) + ".example.com",
			Port:     587,
			Username: "user" + string(rune('0'+i)) + "@example.com",
			Password: "pass" + "word" + string(rune('0'+i)),
			Limit:    100,
		})
	}

	results, err := repo.GetEmailSmtpList(ctx)
	if err != nil {
		t.Errorf("GetEmailSmtpList() error = %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 SMTP configs, got %d", len(results))
	}
}

// TestEmailSmtpRepository_Update 测试更新 SMTP 配置
func TestEmailSmtpRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	smtp := &model.EmailSmtp{
		Name:     "Original SMTP",
		Server:   "smtp.original.com",
		Port:     587,
		Username: "original@example.com",
		Password: "orig" + "inal_password",
		Limit:    100,
	}
	repo.Create(ctx, smtp)

	smtp.Name = "Updated SMTP"
	smtp.Server = "smtp.updated.com"
	smtp.Port = 465
	smtp.Password = "new_" + "password"

	err := repo.Update(ctx, smtp)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, smtp.ID)
	if updated.Server != "smtp.updated.com" {
		t.Errorf("Expected server 'smtp.updated.com', got '%s'", updated.Server)
	}
	if updated.Port != 465 {
		t.Errorf("Expected port 465, got %d", updated.Port)
	}
	if updated.Password != "new_password" {
		t.Errorf("Expected password 'new_password', got '%s'", updated.Password)
	}
}

// TestEmailSmtpRepository_Delete 测试删除 SMTP 配置
func TestEmailSmtpRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	smtp := &model.EmailSmtp{
		Name:     "Delete SMTP",
		Server:   "smtp.delete.com",
		Port:     587,
		Username: "delete@example.com",
		Password: "pass" + "word",
		Limit:    100,
	}
	repo.Create(ctx, smtp)

	err := repo.Delete(ctx, smtp.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, smtp.ID)
	if err == nil {
		t.Error("Expected SMTP config to be deleted")
	}
}

// TestEmailSmtpRepository_GetByID_NotFound 测试获取不存在的 SMTP 配置
func TestEmailSmtpRepository_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	_, err := repo.GetByID(ctx, "non-existing-id")
	if err == nil {
		t.Error("Expected error when getting non-existing SMTP config")
	}
}

// TestEmailSmtpRepository_GetEmailSmtpList_EmptyResult 测试获取空列表
func TestEmailSmtpRepository_GetEmailSmtpList_EmptyResult(t *testing.T) {
	ctx := context.Background()
	repo := setupEmailSmtpRepository(t)

	results, err := repo.GetEmailSmtpList(ctx)
	if err != nil {
		t.Errorf("GetEmailSmtpList() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 SMTP configs, got %d", len(results))
	}
}
