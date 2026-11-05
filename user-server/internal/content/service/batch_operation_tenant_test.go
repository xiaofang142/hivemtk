package service

// 契约锁（实体审查 #15，服务层，TENANT_MODE=on）：缺陷住在这里，不住在仓储层。
//
// 仓储的三个历史口本来就收 ctx，但服务层把它们全部喂成 `context.Background()`
// （batch_operation.go:638/643/648），`BatchUpdateClues` 连 ctx 参数都没有（:628 直接 Background），
// `exportUsers` 也漏了一个 ctx 形参（:461）。实测改前形状撞闸：
//   [tenant] 表 batch_operation_histories 需要租户上下文但 ctx 未绑定（query|update）
// ⇒ saas 形态下「历史列表/详情/取消」「批量更新线索」「导出用户」四条链路全废，
//   而 off 形态（本地独立部署）逐条全绿——handler 传下来的 `ctx.Request.Context()` 在服务层被丢掉了。
// 这一层必须证明的是「调用方的 ctx 真的交到了仓储」，仓储层的锁看不见这个取舍。

import (
	"context"
	"strings"
	"testing"
	"time"

	contentmodel "hivemtk-user/internal/content/model"
	datamodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	bopSvcTenantA = uint(61)
	bopSvcTenantB = uint(62)
)

func bopSvcDB(t *testing.T) (*gorm.DB, *BatchOperationService, context.Context, context.Context) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	db := testutil.NewTestDB(t,
		&contentmodel.BatchOperationHistory{},
		&datamodel.Clue{},
		&datamodel.User{},
	)
	if db == nil {
		t.Fatal("测试库不可达：ctx 是否贯通全都判不了（不 Skip，跳过等于没锁）")
	}
	return db, NewBatchOperationService(NewBatchOperationDeps(db)),
		tenant.WithTenantID(context.Background(), bopSvcTenantA),
		tenant.WithTenantID(context.Background(), bopSvcTenantB)
}

func bopSvcSeedHistory(t *testing.T, db *gorm.DB, tenantID uint, status string) *contentmodel.BatchOperationHistory {
	t.Helper()
	row := &contentmodel.BatchOperationHistory{
		TenantID:      tenantID,
		OperationType: "import",
		DataType:      "clue",
		Status:        status,
		TotalCount:    3,
		StartedAt:     time.Now(),
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置历史记录失败: %v", err)
	}
	return row
}

// bopSvcPhysicalClueCity 物理读：判断「是谁的行被改了」只能用这一条。
func bopSvcPhysicalClueCity(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var row datamodel.Clue
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("物理读取 clue(%s) 失败: %v", id, err)
	}
	return row.City
}

// TestBatchServiceHistoriesUseCallerContext 列表/详情只认调用方带来的租户，不再自造裸 ctx。
func TestBatchServiceHistoriesUseCallerContext(t *testing.T) {
	db, svc, ctxA, ctxB := bopSvcDB(t)
	bopSvcSeedHistory(t, db, bopSvcTenantA, "success")
	bopSvcSeedHistory(t, db, bopSvcTenantA, "running")
	shared := bopSvcSeedHistory(t, db, bopSvcTenantB, "success")

	list, total, err := svc.GetHistories(ctxA, 1, 20)
	if err != nil {
		t.Fatalf("带租户 ctx 的历史列表应可查（改前这里就是 [tenant] 未绑定）: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("A 租户应看到 2 条，实得 total=%d len=%d", total, len(list))
	}

	got, err := svc.GetHistoryByID(ctxA, shared.ID)
	if err == nil || got != nil {
		t.Errorf("A 读 B 的历史应遮挡为 nil+err，实得 got=%v err=%v", got, err)
	}
	if _, err := svc.GetHistoryByID(ctxB, shared.ID); err != nil {
		t.Errorf("B 读本租户历史应命中，实得 err=%v", err)
	}
}

// TestBatchServiceCancelUsesCallerContext 取消口同样不能用裸 ctx，且只能取消本租户。
func TestBatchServiceCancelUsesCallerContext(t *testing.T) {
	db, svc, ctxA, ctxB := bopSvcDB(t)
	running := bopSvcSeedHistory(t, db, bopSvcTenantA, "running")

	if err := svc.CancelHistory(ctxB, running.ID); err == nil {
		t.Fatal("跨租户取消必须失败")
	}

	if err := svc.CancelHistory(ctxA, running.ID); err != nil {
		t.Fatalf("本租户取消应成功（改前这里被 context.Background() 撞闸）: %v", err)
	}
	var row contentmodel.BatchOperationHistory
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).First(&row, running.ID).Error; err != nil {
		t.Fatalf("物理回读失败: %v", err)
	}
	if row.Status != "cancelled" {
		t.Errorf("取消后应为 cancelled，实得 %q", row.Status)
	}
}

// TestBatchServiceUpdateCluesIsTenantFactored 批量更新的 ctx 必须一路到 SQL：
// 混进别租户的 id 也只能命中 0 行，不能改到别人的线索。
func TestBatchServiceUpdateCluesIsTenantFactored(t *testing.T) {
	db, svc, ctxA, _ := bopSvcDB(t)
	scope := db.WithContext(tenant.WithSystemScope(context.Background()))
	clueA := &datamodel.Clue{TenantID: bopSvcTenantA, Name: "A 的线索", City: "北京"}
	clueB := &datamodel.Clue{TenantID: bopSvcTenantB, Name: "B 的线索", City: "上海"}
	for _, c := range []*datamodel.Clue{clueA, clueB} {
		if err := scope.Create(c).Error; err != nil {
			t.Fatalf("预置线索失败: %v", err)
		}
	}

	count, err := svc.BatchUpdateClues(ctxA, &BatchUpdateRequest{
		IDs:    []string{clueA.ID, clueB.ID},
		Fields: map[string]string{"city": "杭州"},
	})
	if err != nil {
		t.Fatalf("批量更新应成功（改前 context.Background() 撞 update 闸）: %v", err)
	}
	if count != 1 {
		t.Errorf("只应更新本租户那 1 条，实得 count=%d", count)
	}
	if got := bopSvcPhysicalClueCity(t, db, clueA.ID); got != "杭州" {
		t.Errorf("本租户线索应改为杭州，实得 %q", got)
	}
	if got := bopSvcPhysicalClueCity(t, db, clueB.ID); got != "上海" {
		t.Errorf("跨租户 id 不得改动别人的线索，实得 %q", got)
	}
}

// TestBatchServiceExportUsersIsTenantFactored 导出用户的分支漏了 ctx 形参：
// 改前 on 模式下整条导出直接报错，而导出线索那条（已带 ctx）正常——两条现在必须都可用。
func TestBatchServiceExportUsersIsTenantFactored(t *testing.T) {
	db, svc, ctxA, _ := bopSvcDB(t)
	scope := db.WithContext(tenant.WithSystemScope(context.Background()))
	userA := &datamodel.User{TenantID: bopSvcTenantA, Username: "bop_svc_user_a", Email: "a@bop.test"}
	userB := &datamodel.User{TenantID: bopSvcTenantB, Username: "bop_svc_user_b", Email: "b@bop.test"}
	for _, u := range []*datamodel.User{userA, userB} {
		if err := scope.Create(u).Error; err != nil {
			t.Fatalf("预置用户失败: %v", err)
		}
	}
	clueA := &datamodel.Clue{TenantID: bopSvcTenantA, Name: "A 的线索"}
	if err := scope.Create(clueA).Error; err != nil {
		t.Fatalf("预置线索失败: %v", err)
	}

	buf, err := svc.GenerateCSV(ctxA, ExportTypeUser, []string{userA.ID})
	if err != nil {
		t.Fatalf("导出用户应成功（改前 context.Background() 撞 query 闸）: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "bop_svc_user_a") {
		t.Errorf("导出内容应含本租户用户，实得 %q", out)
	}
	if strings.Contains(out, "bop_svc_user_b") {
		t.Errorf("导出内容不得含别租户用户，实得 %q", out)
	}

	clueBuf, err := svc.GenerateCSV(ctxA, ExportTypeClue, []string{clueA.ID})
	if err != nil {
		t.Fatalf("导出线索同样应可用: %v", err)
	}
	if !strings.Contains(clueBuf.String(), "A 的线索") {
		t.Errorf("导出线索内容缺失，实得 %q", clueBuf.String())
	}
}
