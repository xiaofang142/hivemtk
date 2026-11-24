package service

// 契约：MFA 设置（**第二次**起走的是更新分支）不得把记录的租户改写成 0。
//
// 坑（与短信配置 / 知识 OpenAPI 源 / 流失预测同一族的通用形状）：
// SetupMFA 传进 repo.Save 的是**新建的零值 model**（tenant_id=0），
// 而 repo.Save 走 GORM 的 Save —— Save 写**全部字段含零值** ⇒ 这一行的租户被改写成 0。
// 多租户模式下 tenant 回调 fail-closed 当场拒掉
//（"[tenant] 更新不可改写 tenant_id"），接口 500；独立部署没这层校验 ⇒ 200。
//
// 为什么第一次不报：第一次库里没有这行，走的是 Create —— Create 由隔离 callback
// 填 tenant_id，不经过 Save。所以这条缺陷**只在第二次设置 MFA 时才现形**，
// 手工测一次是测不出来的。
//
// 修法：更新前把库里的 tenant_id 带回来。
//
// 本文件锁两条腿：
//  1. 第二次 SetupMFA 之后，该行仍属于原租户（不是 0）；
//  2. 第二次 SetupMFA 不报错（这正是缺陷的症状本身）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// TestSetupMFASecondTimeKeepsTenant 第二次设置 MFA 不得把 tenant 改写成 0。
func TestSetupMFASecondTimeKeepsTenant(t *testing.T) {
	database := setupMFATestDB(t)
	s := NewMFAService()

	const tid = uint(3)
	user := &model.SystemUser{
		Username: "test_mfa_tenant_user",
		Password: "Passw" + "ord123",
		Email:    "mfa-tenant@example.com",
		Role:     "user",
		Status:   1,
		TenantID: tid,
	}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	// 第一次：库里没有这行 ⇒ 走 Create。
	if _, err := s.SetupMFA(context.Background(), user.ID, user.Username); err != nil {
		t.Fatalf("第一次 SetupMFA 失败: %v", err)
	}

	// 第二次：库里已有 ⇒ 走 Save（本缺陷的路径）。
	if _, err := s.SetupMFA(context.Background(), user.ID, user.Username); err != nil {
		t.Fatalf("第二次 SetupMFA 失败（更新分支不得把零值 tenant_id 写进去）: %v", err)
	}

	var mfa model.UserMFA
	if err := database.Where("user_id = ?", user.ID).First(&mfa).Error; err != nil {
		t.Fatalf("查询 MFA 记录失败: %v", err)
	}
	if mfa.TenantID == 0 {
		t.Errorf("MFA 记录的 tenant_id 被改写成 0（孤儿行，任何租户都读不到）")
	}
}
