package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// 账号 A 只授权行业甲，账号 B 只授权行业乙：交叉组合必须拒绝。
func TestAccountRulePairing_CrossAccountIndustryRejected(t *testing.T) {
	scope := CustomerDataScope{
		UserID: 7, RestrictedAccounts: true,
		AccountIDs: []string{"acct-a", "acct-b"},
		Industries: []string{"甲", "乙"},
		AccountRules: []CustomerAccountRule{
			{AccountID: "acct-a", Industries: []string{"甲"}, Role: "viewer"},
			{AccountID: "acct-b", Industries: []string{"乙"}, Role: "viewer"},
		},
	}
	svc := &CustomerPermissionService{}
	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-a", Industry: "甲"}) {
		t.Fatal("A/甲 应允许")
	}
	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-b", Industry: "乙"}) {
		t.Fatal("B/乙 应允许")
	}
	if svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-a", Industry: "乙"}) {
		t.Fatal("A/乙 交叉组合必须拒绝")
	}
	if svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-b", Industry: "甲"}) {
		t.Fatal("B/甲 交叉组合必须拒绝")
	}
}

// owner 与 viewer 混合授权：owner 规则只对它自己的账号生效，不能把 viewer 账号也变成 OnlyOwned。
func TestAccountRulePairing_MixedRoleDoesNotWidenOrNarrow(t *testing.T) {
	scope := CustomerDataScope{
		UserID: 7, RestrictedAccounts: true,
		AccountIDs: []string{"acct-a", "acct-b"},
		AccountRules: []CustomerAccountRule{
			{AccountID: "acct-a", Role: "owner", OwnerUserID: 7},
			{AccountID: "acct-b", Role: "viewer"},
		},
	}
	svc := &CustomerPermissionService{}
	owned := uint(7)
	other := uint(8)
	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-a", OwnerUserID: &owned}) {
		t.Fatal("owner 规则的账号、本人负责应允许")
	}
	if svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-a", OwnerUserID: &other}) {
		t.Fatal("owner 规则的账号、他人负责应拒绝")
	}
	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "acct-b", OwnerUserID: &other}) {
		t.Fatal("viewer 规则账号不应被 owner 规则错误收窄")
	}
}

// 管理员的全局范围不受逐账号配对影响。
func TestAccountRulePairing_AdminUnaffected(t *testing.T) {
	scope := CustomerDataScope{UserID: 9, AllAccounts: true}
	svc := &CustomerPermissionService{}
	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "any", Industry: "any"}) {
		t.Fatal("管理员应全量可读")
	}
}
