package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type optionalEmailAdminRepo struct {
	repository.SystemUserRepository
	created      *model.SystemUser
	emailChecked bool
}

var errOptionalEmailCreate = errors.New("测试在写库前停止")

func (r *optionalEmailAdminRepo) UsernameExists(context.Context, string, uint) (bool, error) {
	return false, nil
}
func (r *optionalEmailAdminRepo) EmailExists(context.Context, string, uint) (bool, error) {
	r.emailChecked = true
	return false, nil
}
func (r *optionalEmailAdminRepo) Create(_ context.Context, user *model.SystemUser) error {
	r.created = user
	return errOptionalEmailCreate
}

func TestInitAdminOptionalEmail(t *testing.T) {
	for _, email := range []string{"", "   ", "admin@example.com"} {
		t.Run(email, func(t *testing.T) {
			repo := &optionalEmailAdminRepo{}
			svc := &AuthService{systemUserRepo: repo}
			err := svc.InitAdmin(context.Background(), "admin", "Password123!", email)
			if repo.created == nil {
				t.Fatalf("有效账号应进入创建流程，实际错误: %v", err)
			}
			wantCheck := email == "admin@example.com"
			if repo.emailChecked != wantCheck {
				t.Fatalf("邮箱唯一性检查=%v，期望=%v", repo.emailChecked, wantCheck)
			}
			if !wantCheck && repo.created.Email != "" {
				t.Fatalf("空白邮箱应保存为空串，实际=%q", repo.created.Email)
			}
		})
	}
}

func TestInitAdminRejectsInvalidOptionalEmail(t *testing.T) {
	repo := &optionalEmailAdminRepo{}
	svc := &AuthService{systemUserRepo: repo}
	if err := svc.InitAdmin(context.Background(), "admin", "Password123!", "invalid-email"); err == nil {
		t.Fatal("填写非法邮箱应拒绝")
	}
	if repo.created != nil || repo.emailChecked {
		t.Fatal("非法邮箱不应进入仓储操作")
	}
}
