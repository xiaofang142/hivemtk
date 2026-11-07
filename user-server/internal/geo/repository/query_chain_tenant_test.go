// query_chain_tenant_test.go 思维链与补位任务的租户契约。
//
// geo_query_chains / geo_content_tasks 都是租户表：读写只能看到本租户；
// MarkDone 命中 0 行（跨租户/已删）必须报不存在，不能当作更新成功。
package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	gqcTenantA = uint(367)
	gqcTenantB = uint(368)
)

func gqcSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoQueryChain{}, &model.GeoContentTask{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoQueryChainRepository_TenantScoped(t *testing.T) {
	db := gqcSetupDB(t)
	repo := NewGeoQueryChainRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gqcTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gqcTenantB)
	sysCtx := tenant.WithSystemScope(context.Background())

	chainA := &model.GeoQueryChain{ChainID: "chain-a", Seq: 1, Query: "A 查询"}
	if err := repo.Append(ctxA, chainA); err != nil {
		t.Fatalf("租户 A 写入失败: %v", err)
	}
	if chainA.TenantID != gqcTenantA {
		t.Fatalf("Append 未填租户: %d", chainA.TenantID)
	}
	// 同一 chain_id 在租户 B 是另一条独立会话。
	if err := repo.Append(ctxB, &model.GeoQueryChain{ChainID: "chain-a", Seq: 1, Query: "B 查询"}); err != nil {
		t.Fatalf("租户 B 写入失败: %v", err)
	}

	rowsA, err := repo.ListByChain(ctxA, "chain-a")
	if err != nil {
		t.Fatalf("租户 A 按链查询失败: %v", err)
	}
	if len(rowsA) != 1 || rowsA[0].TenantID != gqcTenantA {
		t.Fatalf("租户 A 按链查询越界: %+v", rowsA)
	}
	nA, err := repo.CountByChain(ctxA, "chain-a")
	if err != nil || nA != 1 {
		t.Fatalf("租户 A 链计数 = %d, err=%v，期望 1", nA, err)
	}
	todayA, err := repo.CountToday(ctxA)
	if err != nil || todayA != 1 {
		t.Fatalf("租户 A 当日计数 = %d, err=%v，期望 1", todayA, err)
	}

	// OneID 回填只动本租户的链行。
	affected, err := repo.BindOneIDToChain(ctxA, "chain-a", "one-1")
	if err != nil {
		t.Fatalf("回填 OneID 失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("回填行数 = %d，期望 1", affected)
	}
	byOne, err := repo.ListByOneID(ctxA, "one-1")
	if err != nil || len(byOne) != 1 {
		t.Fatalf("租户 A 按 OneID 查询应命中 1 行，实际 %d, err=%v", len(byOne), err)
	}
	var bRow model.GeoQueryChain
	if err := db.WithContext(sysCtx).Where("tenant_id = ?", gqcTenantB).First(&bRow).Error; err != nil {
		t.Fatalf("回查租户 B 链行失败: %v", err)
	}
	if bRow.OneID != "" {
		t.Fatalf("回填越界到租户 B: one_id=%q", bRow.OneID)
	}

	// fail-closed：无租户 ctx 不允许写租户表。
	if err := repo.Append(context.Background(), &model.GeoQueryChain{ChainID: "chain-x"}); err == nil {
		t.Fatal("无租户 ctx 写入应报错，实际成功")
	}
}

func TestGeoContentTaskRepository_TenantScoped(t *testing.T) {
	db := gqcSetupDB(t)
	repo := NewGeoContentTaskRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gqcTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gqcTenantB)

	taskA := &model.GeoContentTask{Keyword: "kw-a", Status: "pending"}
	if err := repo.Create(ctxA, taskA); err != nil {
		t.Fatalf("租户 A 创建任务失败: %v", err)
	}
	if taskA.TenantID != gqcTenantA {
		t.Fatalf("Create 未填租户: %d", taskA.TenantID)
	}
	if err := repo.Create(ctxB, &model.GeoContentTask{Keyword: "kw-b", Status: "pending"}); err != nil {
		t.Fatalf("租户 B 创建任务失败: %v", err)
	}

	pendingA, err := repo.ListPending(ctxA, 50)
	if err != nil {
		t.Fatalf("租户 A 待办失败: %v", err)
	}
	if len(pendingA) != 1 || pendingA[0].TenantID != gqcTenantA {
		t.Fatalf("租户 A 待办越界: %+v", pendingA)
	}
	pendingN, err := repo.CountByStatus(ctxA, "pending")
	if err != nil || pendingN != 1 {
		t.Fatalf("租户 A 待办计数 = %d, err=%v，期望 1", pendingN, err)
	}

	// 跨租户 MarkDone：报不存在，且不改租户 A 的状态。
	if err := repo.MarkDone(ctxB, taskA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 MarkDone 应报不存在，实际 err=%v", err)
	}
	var after model.GeoContentTask
	if err := db.WithContext(ctxA).Where("id = ?", taskA.ID).First(&after).Error; err != nil {
		t.Fatalf("回查租户 A 任务失败: %v", err)
	}
	if after.Status != "pending" {
		t.Fatalf("跨租户 MarkDone 改了租户 A 的任务: status=%s", after.Status)
	}

	// 本租户 MarkDone 成功；不存在的 id 报不存在（旧实现恒 nil = 假成功）。
	if err := repo.MarkDone(ctxA, taskA.ID); err != nil {
		t.Fatalf("本租户 MarkDone 失败: %v", err)
	}
	if err := repo.MarkDone(ctxA, "no-such-task"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不存在的任务应报不存在，实际 err=%v", err)
	}
	if doneN, err := repo.CountByStatus(ctxA, "done"); err != nil || doneN != 1 {
		t.Fatalf("租户 A 完成计数 = %d, err=%v，期望 1", doneN, err)
	}
}
