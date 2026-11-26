package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// 存量授权按业务 key 生效：移动后新祖先自动构造，不补授目录或重新 seed 全表。
func TestMenuNavigation_ExistingLeafGrantsSurviveMoves(t *testing.T) {
	previous := model.GetModuleEnabledFilter()
	model.SetModuleEnabledFilter(func(string) bool { return true })
	t.Cleanup(func() { model.SetModuleEnabledFilter(previous) })
	cases := []struct {
		name      string
		role      string
		keys      []string
		ancestors []string
	}{
		{"客服存量多叶子", model.SystemRoleCodeCustomerService,
			[]string{"faqKb", "knowledgeBase", "agentStatus", "customerSession"}, []string{"knowledge", "aiAgent", "agentWorkbench", "agentPassive"}},
		{"员工存量多叶子", model.SystemRoleCodeStaff,
			[]string{"faqKb", "knowledgeBase", "confidencePanel", "humanizePanel", "feedbackLoopPanel"}, []string{"knowledge", "aiAgent", "agentPassive"}},
		{"客服只授FAQ", model.SystemRoleCodeCustomerService, []string{"faqKb"}, []string{"knowledge"}},
		{"员工只授反馈闭环", model.SystemRoleCodeStaff, []string{"feedbackLoopPanel"}, []string{"aiAgent", "agentPassive"}},
		{"存量表中该角色无授权", model.SystemRoleCodeStaff, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 全表非空包括该角色零授权的情况；不能因该角色为空而重新 seed。
			repo := newFakeMenuRepo(map[string][]string{tc.role: tc.keys}, int64(len(tc.keys)+1))
			svc := NewMenuServiceWithRepo(repo)
			tree, err := svc.GetUserMenuTree(context.Background(), tc.role)
			if err != nil {
				t.Fatalf("读取存量菜单授权失败: %v", err)
			}
			visible := menuKeysIn(tree)
			grants, err := svc.GrantProvider(context.Background(), tc.role)
			if err != nil {
				t.Fatalf("读取能力面授予失败: %v", err)
			}
			for _, key := range tc.keys {
				mustHave(t, visible, key, "存量叶子应仍在侧栏")
				mustHave(t, grants, key, "存量叶子应仍在能力面授予")
			}
			for _, key := range tc.ancestors {
				mustHave(t, visible, key, "旧叶子授权自动带出新祖先")
			}
			if len(visible) != len(tc.keys)+len(tc.ancestors) {
				t.Fatalf("侧栏只能包含原授权叶子与必要祖先，实际 %v", visible)
			}
			if len(grants) != len(tc.keys) {
				t.Fatalf("迁移前后的有效授权集合应完全相同，实际 %v", grants)
			}
			echoed, err := svc.GetRoleMenuKeys(context.Background(), tc.role)
			if err != nil || len(echoed) != len(tc.keys) {
				t.Fatalf("分配回显必须只包含原授权叶子，实际 %v，错误 %v", echoed, err)
			}
			for _, key := range echoed {
				mustHave(t, grants, key, "分配回显与能力面授予一致")
			}
			for _, key := range []string{"workspace", "customer", "system", "reach", "inbox"} {
				mustLack(t, grants, key, "不得因目录搬迁自动扩权")
			}
			if repo.seeded != 0 || repo.replaceCnt != 0 {
				t.Fatalf("移动存量叶子不需改授权表，实际 seed=%d replace=%d", repo.seeded, repo.replaceCnt)
			}
		})
	}
}

// 即使老库有祖先受限叶子的遗留行，换目录后也不能复活为普通角色权限。
func TestMenuNavigation_MovedAdminLeavesStayDenied(t *testing.T) {
	for _, role := range []string{model.SystemRoleCodeCustomerService, model.SystemRoleCodeStaff} {
		repo := newFakeMenuRepo(map[string][]string{role: {"systemMaterialLibrary", "automationCenter", "messageHub", "businessAccountAuth", "wecomAccount"}}, 5)
		svc := NewMenuServiceWithRepo(repo)
		grants, err := svc.GrantProvider(context.Background(), role)
		if err != nil {
			t.Fatalf("读取 %s 能力面授予失败: %v", role, err)
		}
		if len(grants) != 0 {
			t.Fatalf("%s 不得因移动祖先而恢复 admin 叶子，实际 %v", role, grants)
		}
		if err := svc.UpdateRoleMenuKeys(context.Background(), role, []string{"systemMaterialLibrary"}); err == nil {
			t.Fatalf("%s 不应能重新授予 admin 素材库", role)
		}
	}
}
