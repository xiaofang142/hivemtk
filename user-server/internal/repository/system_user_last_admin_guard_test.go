package repository

// system_user_last_admin_guard_test.go FA-AUTH-01：最后一个启用超管的原子守卫。
//
// 原缺陷：先计数后写的窗口内，两名启用超管可并发互相禁用（或删除），两边都读到
// count=2 而都放行 ⇒ 系统零启用超管。现守卫与写入收进同一事务 + 咨询锁，
// 并发夹具验证：冲突操作恰好一个成功、另一个被拒，非冲突操作不受锁牵连。
// 走真实 PG（咨询锁语义只在 PG 上成立）。

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func newLastAdminGuardDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.SystemUser{})
	db.SetTestDB(database)
	return database
}

func TestSetEnabledGuarded_ConcurrentDisableOfLastTwoAdmins(t *testing.T) {
	db := newLastAdminGuardDB(t)
	repo := NewSystemUserRepository()
	a := &model.SystemUser{Username: "fa01-admin-a", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	b := &model.SystemUser{Username: "fa01-admin-b", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("种 A 失败：%v", err)
	}
	if err := db.Create(b).Error; err != nil {
		t.Fatalf("种 B 失败：%v", err)
	}
	ctx := context.Background()

	var barrier, done sync.WaitGroup
	barrier.Add(2) // 各自 Done 后再 Wait，确保同时开赛
	errs := make([]error, 2)
	done.Add(2)
	go func() {
		defer done.Done()
		barrier.Done()
		barrier.Wait()
		errs[0] = repo.SetEnabledGuarded(ctx, a.ID, false)
	}()
	go func() {
		defer done.Done()
		barrier.Done()
		barrier.Wait()
		errs[1] = repo.SetEnabledGuarded(ctx, b.ID, false)
	}()
	done.Wait()

	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("并发禁用最后两名超管必须恰好一个成功一个被拒，实际 errs=%v", errs)
	}
	var n int64
	db.Model(&model.SystemUser{}).Where("role = ? AND enabled = ?", model.SystemUserRoleAdmin, true).Count(&n)
	if n != 1 {
		t.Fatalf("竞态后必须剩恰好一个启用超管，实际 %d", n)
	}
}

func TestDeleteSafe_ConcurrentDeleteOfLastTwoAdmins(t *testing.T) {
	db := newLastAdminGuardDB(t)
	repo := NewSystemUserRepository()
	a := &model.SystemUser{Username: "fa01-admin-a", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	b := &model.SystemUser{Username: "fa01-admin-b", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("种 A 失败：%v", err)
	}
	if err := db.Create(b).Error; err != nil {
		t.Fatalf("种 B 失败：%v", err)
	}
	ctx := context.Background()

	var barrier, done sync.WaitGroup
	barrier.Add(2) // 各自 Done 后再 Wait，确保同时开赛
	errs := make([]error, 2)
	done.Add(2)
	go func() {
		defer done.Done()
		barrier.Done()
		barrier.Wait()
		errs[0] = repo.DeleteSafe(ctx, a.ID)
	}()
	go func() {
		defer done.Done()
		barrier.Done()
		barrier.Wait()
		errs[1] = repo.DeleteSafe(ctx, b.ID)
	}()
	done.Wait()

	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("并发删除最后两名超管必须恰好一个成功一个被拒，实际 errs=%v", errs)
	}
	var n int64
	db.Model(&model.SystemUser{}).Where("role = ?", model.SystemUserRoleAdmin).Count(&n)
	if n != 1 {
		t.Fatalf("竞态后必须剩恰好一个超管，实际 %d", n)
	}
}

func TestUpdateGuarded_DemotionOfLastAdminRejected(t *testing.T) {
	db := newLastAdminGuardDB(t)
	repo := NewSystemUserRepository()
	a := &model.SystemUser{Username: "fa01-admin-a", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	b := &model.SystemUser{Username: "fa01-admin-b", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("种 A 失败：%v", err)
	}
	if err := db.Create(b).Error; err != nil {
		t.Fatalf("种 B 失败：%v", err)
	}
	ctx := context.Background()

	// 还有另一名启用超管：降级 A 应成功。
	if err := repo.UpdateGuarded(ctx, a.ID, func(current *model.SystemUser) error {
		current.Role = model.SystemUserRoleUser
		return nil
	}); err != nil {
		t.Fatalf("存在其他启用超管时降级应成功：%v", err)
	}
	// 只剩一名超管：降级必须被守卫拒绝。
	if err := repo.UpdateGuarded(ctx, b.ID, func(current *model.SystemUser) error {
		current.Role = model.SystemUserRoleUser
		return nil
	}); err == nil {
		t.Fatal("降级最后一名超管必须被拒绝")
	}
	var n int64
	db.Model(&model.SystemUser{}).Where("role = ? AND enabled = ?", model.SystemUserRoleAdmin, true).Count(&n)
	if n != 1 {
		t.Fatalf("守卫后必须仍剩一个启用超管，实际 %d", n)
	}
}

func TestSetEnabledGuarded_DisableNonAdminNotBlocked(t *testing.T) {
	db := newLastAdminGuardDB(t)
	repo := NewSystemUserRepository()
	a := &model.SystemUser{Username: "fa01-admin-a", Password: "x", Role: model.SystemUserRoleAdmin, Enabled: true, Status: 1}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("种超管失败：%v", err)
	}
	staff := &model.SystemUser{Username: "fa01-staff", Password: "x", Role: model.SystemUserRoleUser, Enabled: true, Status: 1}
	if err := db.Create(staff).Error; err != nil {
		t.Fatalf("种员工失败：%v", err)
	}

	start := time.Now()
	if err := repo.SetEnabledGuarded(context.Background(), staff.ID, false); err != nil {
		t.Fatalf("禁用非超管不受守卫影响：%v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("非冲突操作不应长时间持锁等待，实际 %v", elapsed)
	}
	var enabled bool
	db.Model(&model.SystemUser{}).Where("id = ?", staff.ID).Pluck("enabled", &enabled)
	if enabled {
		t.Fatal("员工应被禁用")
	}
}
