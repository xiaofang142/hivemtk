package service

import (
	"testing"

	"hivemtk-user/internal/repository"
)

// TestCustomerPermissionToFilter_EmptyIntersectionRemainsEmptyAfterReapplication
// 行业 A 与请求行业 B 无交集时，重复应用权限不得把空交集重新解释为“未指定行业”。
func TestCustomerPermissionToFilter_EmptyIntersectionRemainsEmptyAfterReapplication(t *testing.T) {
	permission := &CustomerPermissionService{}
	scope := CustomerDataScope{
		UserID:             7,
		RestrictedAccounts: true,
		AccountIDs:         []string{"acct-1"},
		Industries:         []string{"A"},
	}

	first := permission.ToFilter(scope, repository.CustomerScopeFilter{Industries: []string{"B"}})
	if len(first.Industries) != 0 || !first.DenyAll {
		t.Fatalf("A→B 首次交集应为空且显式拒绝，实际 industries=%v,deny_all=%v", first.Industries, first.DenyAll)
	}
	second := permission.ToFilter(scope, first)
	if len(second.Industries) != 0 || !second.DenyAll {
		t.Fatalf("A→B 重复应用权限后仍应为空且显式拒绝，实际 industries=%v,deny_all=%v", second.Industries, second.DenyAll)
	}
}

// TestCustomerPermissionToFilter_PartialIntersectionKeepsAllowedIndustry
// 部分交集只保留双方都允许的行业，不能误判为拒绝全部。
func TestCustomerPermissionToFilter_PartialIntersectionKeepsAllowedIndustry(t *testing.T) {
	permission := &CustomerPermissionService{}
	scope := CustomerDataScope{Industries: []string{"A", "C"}}

	got := permission.ToFilter(scope, repository.CustomerScopeFilter{Industries: []string{"B", "C"}})
	if len(got.Industries) != 1 || got.Industries[0] != "C" {
		t.Fatalf("部分交集应仅保留 C，实际 %v", got.Industries)
	}
}

// TestCustomerPermissionToFilter_AdminBypassesIndustryGrant
// 管理员全量作用域不被员工行业授权反向收敛。
func TestCustomerPermissionToFilter_AdminBypassesIndustryGrant(t *testing.T) {
	permission := &CustomerPermissionService{}
	scope := CustomerDataScope{AllAccounts: true, Industries: []string{"A"}}

	got := permission.ToFilter(scope, repository.CustomerScopeFilter{Industries: []string{"B"}})
	if len(got.Industries) != 1 || got.Industries[0] != "B" {
		t.Fatalf("管理员请求 B 应保留 B，实际 %v", got.Industries)
	}
}

// TestCustomerPermissionToFilter_UnrestrictedIndustryScopePreservesRequest
// 授权行业为空表示行业不受限，仍按调用方请求行业过滤。
func TestCustomerPermissionToFilter_UnrestrictedIndustryScopePreservesRequest(t *testing.T) {
	permission := &CustomerPermissionService{}
	scope := CustomerDataScope{RestrictedAccounts: true, AccountIDs: []string{"acct-1"}}

	got := permission.ToFilter(scope, repository.CustomerScopeFilter{Industries: []string{"B"}})
	if len(got.Industries) != 1 || got.Industries[0] != "B" {
		t.Fatalf("行业不受限时应保留请求 B，实际 %v", got.Industries)
	}
}

// TestCustomerPermissionToFilter_EmptyRequestInheritsAuthorizedIndustries
// 未指定行业时仍继承员工授权行业，不能退化为全行业。
func TestCustomerPermissionToFilter_EmptyRequestInheritsAuthorizedIndustries(t *testing.T) {
	permission := &CustomerPermissionService{}
	scope := CustomerDataScope{Industries: []string{"A"}}

	got := permission.ToFilter(scope, repository.CustomerScopeFilter{})
	if len(got.Industries) != 1 || got.Industries[0] != "A" {
		t.Fatalf("未指定行业时应继承授权 A，实际 %v", got.Industries)
	}
}
