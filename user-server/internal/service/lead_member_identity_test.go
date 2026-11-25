package service

import "testing"

// LM-11 正例：群成员身份与业务角色分离；普通发言不把已有业务角色抹回 member。
func TestResolveGroupMemberRole_PreservesBusinessRole(t *testing.T) {
	if got := resolveGroupMemberRole("甲", "member", "demand"); got != "demand" {
		t.Fatalf("转述者普通发言不得覆盖已证实的业务角色，实际 %q", got)
	}
	if got := resolveGroupMemberRole("甲", "member", "supply"); got != "supply" {
		t.Fatalf("供货角色应保留，实际 %q", got)
	}
	if got := resolveGroupMemberRole("甲", "member", ""); got != "member" {
		t.Fatalf("无业务证据时应写群成员身份，实际 %q", got)
	}
}

// LM-11 反例：无稳定成员标识时保留 unknown，不凭昵称强并；群管理员不自动成为采购负责人。
func TestResolveGroupMemberRole_UnknownAndAdmin(t *testing.T) {
	if got := resolveGroupMemberRole("", "admin", "unknown"); got != "unknown" {
		t.Fatalf("空成员标识不得归为管理员/买家，实际 %q", got)
	}
	if got := resolveGroupMemberRole("甲", "admin", "broker"); got != "broker" {
		t.Fatalf("群管理员身份不等于业务角色，实际 %q", got)
	}
}
