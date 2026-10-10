package db

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// migrate_backfill_tenant_test.go 多租户 P0 回填钩子的落库用例（PG，CI 强制）。
//
// 三条腿：
//  1. 默认团队幂等创建（重跑不重复建）；
//  2. 非 admin 用户与业务行归入默认团队、admin 保持 team_id=0（平台侧）；
//  3. role CHECK 放宽钩子：带历史 CHECK 的库也能落 team_admin（Go 侧为唯一口径）。
func TestStartupBackfillDefaultTeam(t *testing.T) {
	gdb := testutil.NewTestDB(t, &model.Team{}, &model.SystemUser{}, &model.Customer{})
	if gdb == nil {
		t.Skip("测试库不可达")
	}

	// 铺存量：一个 admin（保持 0）、两个普通用户（应归默认团队）、两条客户行
	admin := &model.SystemUser{Username: "t-admin", Role: model.SystemUserRoleAdmin, Enabled: true}
	u1 := &model.SystemUser{Username: "t-user-1", Role: model.SystemUserRoleUser, Enabled: true}
	u2 := &model.SystemUser{Username: "t-user-2", Role: model.SystemUserRoleUser, Enabled: true}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatalf("铺 admin 失败: %v", err)
	}
	if err := gdb.Create(u1).Error; err != nil {
		t.Fatalf("铺 user 失败: %v", err)
	}
	if err := gdb.Create(u2).Error; err != nil {
		t.Fatalf("铺 user 失败: %v", err)
	}
	c1 := &model.Customer{ID: "t-cust-1", Name: "客户1"}
	c2 := &model.Customer{ID: "t-cust-2", Name: "客户2"}
	if err := gdb.Create(c1).Error; err != nil {
		t.Fatalf("铺客户失败: %v", err)
	}
	if err := gdb.Create(c2).Error; err != nil {
		t.Fatalf("铺客户失败: %v", err)
	}

	postMigrateBackfillDefaultTeam(gdb)

	var team model.Team
	if err := gdb.Where("name = ?", model.DefaultTeamName).First(&team).Error; err != nil {
		t.Fatalf("默认团队应存在: %v", err)
	}
	// 幂等：重跑不重复建
	postMigrateBackfillDefaultTeam(gdb)
	var teamCnt int64
	gdb.Model(&model.Team{}).Where("name = ?", model.DefaultTeamName).Count(&teamCnt)
	if teamCnt != 1 {
		t.Fatalf("默认团队必须唯一，got %d", teamCnt)
	}

	for _, u := range []*model.SystemUser{u1, u2} {
		var after model.SystemUser
		if err := gdb.First(&after, u.ID).Error; err != nil {
			t.Fatalf("回读用户失败: %v", err)
		}
		if after.TeamID != team.ID {
			t.Errorf("普通用户 %s 应归默认团队，got team_id=%d", after.Username, after.TeamID)
		}
	}
	var afterAdmin model.SystemUser
	if err := gdb.First(&afterAdmin, admin.ID).Error; err != nil {
		t.Fatalf("回读 admin 失败: %v", err)
	}
	if afterAdmin.TeamID != 0 {
		t.Errorf("平台管理员应保持 team_id=0（平台侧），got %d", afterAdmin.TeamID)
	}
	for _, c := range []*model.Customer{c1, c2} {
		var after model.Customer
		if err := gdb.First(&after, "id = ?", c.ID).Error; err != nil {
			t.Fatalf("回读客户失败: %v", err)
		}
		if after.TeamID != team.ID {
			t.Errorf("客户 %s 应归默认团队，got team_id=%d", after.ID, after.TeamID)
		}
	}

	// 角色 CHECK 放宽：team_admin 应能落库（带历史 CHECK 的库上没有这一步会直接被 DB 拒）
	postMigrateRelaxSystemUserRoleCheck(gdb)
	ta := &model.SystemUser{Username: "t-team-admin", Role: model.SystemUserRoleTeamAdmin, Enabled: true, TeamID: team.ID}
	if err := gdb.Create(ta).Error; err != nil {
		t.Errorf("team_admin 应能落库（CHECK 已放宽）: %v", err)
	}
}
