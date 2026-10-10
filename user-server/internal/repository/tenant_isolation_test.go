package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository/scope"

	dbutil "hivemtk-user/internal/pkg/db"
)

// tenant_isolation_test.go 多租户隔离的仓库层落库用例（PG，CI 强制）。
//
// 核心判据（对应总目标验收标准）：
//   - 团队 A 的普通用户看不到团队 B 的客户（列表与按 id 直读都是"不存在"）；
//   - 平台管理员（Platform=true）跨团队全量可见（需求 3）；
//   - 系统上下文（无身份）直通（ingest/导入等平台管线不被误伤）；
//   - 普通用户 Create 自动回填 team_id（GORM 租户回调）；
//   - 系统上下文 Create 落默认团队（不是谁也看不见的 team 0）。
func TestTenantIsolation_CustomerScoping(t *testing.T) {
	gdb := testutil.NewTestDB(t, &model.Team{}, &model.SystemUser{}, &model.Customer{})
	if gdb == nil {
		t.Skip("测试库不可达")
	}
	dbutil.SetTestDB(gdb)
	defer dbutil.SetTestDB(nil)

	restore := scope.SetTenantIsolationForTest(true)
	defer restore()

	ctx := context.Background()

	teamA := &model.Team{Name: "团队A", Plan: model.TeamPlanFree, Status: model.TeamStatusActive}
	teamB := &model.Team{Name: "团队B", Plan: model.TeamPlanFree, Status: model.TeamStatusActive}
	if err := gdb.Create(teamA).Error; err != nil {
		t.Fatalf("铺团队A失败: %v", err)
	}
	if err := gdb.Create(teamB).Error; err != nil {
		t.Fatalf("铺团队B失败: %v", err)
	}

	// 常量同步钉：scope.DefaultTeamName 与 model.DefaultTeamName 必须同值
	//（scope 不能 import model，见 tenant_callback.go 注释）。
	if model.DefaultTeamName != scope.DefaultTeamName {
		t.Fatalf("默认团队名漂移：model=%q scope=%q", model.DefaultTeamName, scope.DefaultTeamName)
	}

	// 默认团队先建（生产由回填钩子建；回调的系统上下文兜底写依赖它）
	defTeam := &model.Team{Name: model.DefaultTeamName, Plan: model.TeamPlanFree, Status: model.TeamStatusActive}
	if err := gdb.Create(defTeam).Error; err != nil {
		t.Fatalf("铺默认团队失败: %v", err)
	}

	// 铺三个客户：A 一条、B 一条、平台侧（team 0）一条——归属显式声明
	//（裸 Create 的系统上下文会兜底写默认团队，见 GORM 租户回调语义）
	cA := &model.Customer{ID: "iso-a-1", Name: "A 的客户", TeamID: teamA.ID}
	cB := &model.Customer{ID: "iso-b-1", Name: "B 的客户", TeamID: teamB.ID}
	cP := &model.Customer{ID: "iso-p-1", Name: "平台侧客户", TeamID: 0}
	if err := gdb.Create(cA).Error; err != nil {
		t.Fatalf("铺客户失败: %v", err)
	}
	if err := gdb.Create(cB).Error; err != nil {
		t.Fatalf("铺客户失败: %v", err)
	}
	if err := gdb.Create(cP).Error; err != nil {
		t.Fatalf("铺客户失败: %v", err)
	}

	userCtxA := scope.WithTeamInfo(ctx, scope.TeamInfo{TeamID: teamA.ID})
	userCtxB := scope.WithTeamInfo(ctx, scope.TeamInfo{TeamID: teamB.ID})
	adminCtx := scope.WithTeamInfo(ctx, scope.TeamInfo{Platform: true})

	r := NewCustomerRepository()

	// ① 团队 A 列表：只见 A 的客户
	rowsA, totalA, err := r.List(userCtxA, 1, 50, "")
	if err != nil {
		t.Fatalf("团队A列表失败: %v", err)
	}
	if totalA != 1 || len(rowsA) != 1 || rowsA[0].ID != cA.ID {
		t.Errorf("团队A应只见自己的客户，got total=%d rows=%v", totalA, rowsA)
	}

	// ② 团队 B 直读 A 的客户 id → 不存在（404 语义）
	gotA, err := r.GetByID(userCtxB, cA.ID)
	if err != nil {
		t.Fatalf("跨团队 GetByID 不应报错（应返回 nil）: %v", err)
	}
	if gotA != nil {
		t.Errorf("团队B 不得读到团队A 的客户，got %+v", gotA)
	}

	// ③ 平台管理员全量可见（需求 3）
	_, totalAll, err := r.List(adminCtx, 1, 50, "")
	if err != nil {
		t.Fatalf("平台管理员列表失败: %v", err)
	}
	if totalAll != 3 {
		t.Errorf("平台管理员应见全量 3 条，got %d", totalAll)
	}

	// ④ 系统上下文（无身份）直通（ingest/导入等平台管线）
	rowsSys, totalSys, err := r.List(ctx, 1, 50, "")
	if err != nil {
		t.Fatalf("系统上下文列表失败: %v", err)
	}
	if totalSys != 3 || len(rowsSys) != 3 {
		t.Errorf("系统上下文应见全量，got total=%d rows=%d", totalSys, len(rowsSys))
	}

	// ⑤ 普通用户 Create 自动回填 team_id（GORM 租户回调）
	nc := &model.Customer{ID: "iso-a-2", Name: "A 新建客户"}
	if err := r.Create(userCtxA, nc); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	var after model.Customer
	if err := gdb.First(&after, "id = ?", nc.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if after.TeamID != teamA.ID {
		t.Errorf("普通用户创建的行应自动归属团队A，got team_id=%d", after.TeamID)
	}

	// ⑥ 系统上下文 Create 落默认团队（可被团队成员看见，而不是沉进 team 0）
	sc := &model.Customer{ID: "iso-sys-1", Name: "系统管线客户"}
	if err := r.Create(ctx, sc); err != nil {
		t.Fatalf("系统上下文创建失败: %v", err)
	}
	var afterSys model.Customer
	if err := gdb.First(&afterSys, "id = ?", sc.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if afterSys.TeamID != defTeam.ID {
		t.Errorf("系统管线创建的行应落默认团队，got team_id=%d want %d", afterSys.TeamID, defTeam.ID)
	}
}
