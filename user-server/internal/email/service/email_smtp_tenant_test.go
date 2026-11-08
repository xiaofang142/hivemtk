package email

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// #376 回归：PUT /api/email/smtp/{id} 在多租户 on 模式 100% 500。
//
// 根因：UpdateEmailSmtpDTO 构造 model.EmailSmtp 不带 TenantID（DTO 里也没有该字段），
// repo.Update 走 saveWholeRow Select("*") 整行回写，tenant_id=0 被隔离守卫
// guardUpdateTenantAssignments fail-closed 拒绝 → ErrorFromDB 500。
// 修复：回查旧行并携带 old.TenantID 回写。本用例以「DTO 更新后行的 tenant_id
// 保持不变、且更新真正落库」为判据（不 mock 守卫，直接对真库断言行归属）。
func TestEmailSmtpService_UpdateEmailSmtpDTO_KeepsTenantID(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	database := testutil.NewTestDB(t,
		&model.EmailSmtp{},
		&model.EmailList{},
		&model.EmailJobs{},
		&model.Clue{},
		&model.SystemConfig{},
	)
	db.SetTestDB(database)

	seed := &model.EmailSmtp{
		TenantID: 71,
		ID:       "smtp-tenant-71",
		Name:     "old@qq.com",
		Server:   "smtp.qq.com",
		Port:     465,
		Username: "old@qq.com",
		Password: "enc-old",
		Limit:    100,
	}
	if err := database.Create(seed).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	svc := NewEmailSmtpService()
	err := svc.UpdateEmailSmtpDTO(context.Background(), dto.UpdateEmailSmtpRequest{
		ID:   "smtp-tenant-71",
		Name: "new@qq.com",
	})
	if err != nil {
		t.Fatalf("DTO 更新不应报错（修复前 tenant_id=0 被守卫拒绝 500），实际: %v", err)
	}

	var updated model.EmailSmtp
	if err := database.Where("id = ?", "smtp-tenant-71").First(&updated).Error; err != nil {
		t.Fatalf("更新后行应存在: %v", err)
	}
	if updated.TenantID != 71 {
		t.Fatalf("tenant_id 必须保持 71（修复前落 0 触发守卫 500），实际 %d", updated.TenantID)
	}
	if updated.Name != "new@qq.com" {
		t.Fatalf("name 应更新为 new@qq.com，实际 %q", updated.Name)
	}
	// 未传字段保留原值（password/port/limit 的既有语义）
	if updated.Port != 465 || updated.Limit != 100 {
		t.Fatalf("未传的 port/limit 应保留原值 465/100，实际 %d/%d", updated.Port, updated.Limit)
	}
}
