// system_user_email_test.go SystemUserRepository.GetEmailByID 的作用域契约（PG 定向，8217）。
//
// AV-04c 批次3 自 browser_automation/service/feedback.go notifyFailure 下沉。
// 必须钉住的三件事：
//  1. 谓词 id = ? AND email <> ”：空串邮箱视同未配置（返回 ""，不报错）；
//  2. SystemScope 全局解析：on 模式 + 无租户 ctx（原调用点 writeCtx 即无绑定）
//     仍能取到**任意租户**用户的邮箱 —— 原 Raw 绕过隔离 callback 的语义
//     （browser_tasks.user_id 的归属用户不一定在发起 ctx 的租户里）；
//  3. 查询形态保留 Raw：on 模式非 system scope 下预构建 SQL 守卫本会拒绝，
//     SystemScope 是其合法通道（形态保真的证据，见 GetEmailByID 注释）。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSystemUser_GetEmailByID(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SystemUser{})
	prev := dbUtil.GetDB()
	dbUtil.SetTestDB(db)
	defer dbUtil.SetTestDB(prev)

	repo := NewSystemUserRepository()
	ctx := context.Background()

	withEmail := &model.SystemUser{Username: "notify-alice", Email: "alice@example.com", Password: "x", Role: "staff"}
	noEmail := &model.SystemUser{Username: "notify-bob", Email: "", Password: "x", Role: "staff"}
	for _, u := range []*model.SystemUser{withEmail, noEmail} {
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("seed %s 失败: %v", u.Username, err)
		}
	}

	email, err := repo.GetEmailByID(ctx, withEmail.ID)
	if err != nil {
		t.Fatalf("GetEmailByID 失败: %v", err)
	}
	if email != "alice@example.com" {
		t.Fatalf("应返回配置邮箱，实际 %q", email)
	}

	email, err = repo.GetEmailByID(ctx, noEmail.ID)
	if err != nil {
		t.Fatalf("空邮箱应不报错（原 len==0 分支同形）: %v", err)
	}
	if email != "" {
		t.Fatalf("email <> '' 谓词应排除空串邮箱，实际 %q", email)
	}

	if email, err := repo.GetEmailByID(ctx, 999999); err != nil || email != "" {
		t.Fatalf("不存在用户应返回 (\"\", nil)，实际 (%q, %v)", email, err)
	}
}

func TestSystemUser_GetEmailByIDGlobalAcrossTenants(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SystemUser{})
	prev := dbUtil.GetDB()
	dbUtil.SetTestDB(db)
	defer dbUtil.SetTestDB(prev)

	repo := NewSystemUserRepository()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	const (
		ta = uint(361)
		tb = uint(362)
	)
	sysCtx := tenant.WithSystemScope(context.Background())

	// 两个租户各一名用户；归属租户 B 的用户是被通知对象（原场景：发起 ctx 的
	// 租户 ≠ 用户归属租户，Raw 全局解析才找得到他）。
	bUser := &model.SystemUser{Username: "notify-cross-b", Email: "b-owner@example.com", Password: "x", Role: "staff"}
	if err := db.WithContext(sysCtx).Create(bUser).Error; err != nil {
		t.Fatalf("seed 租户用户失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Exec(`UPDATE system_users SET tenant_id = ? WHERE id = ?`, tb, bUser.ID).Error; err != nil {
		t.Fatalf("钉租户 B 失败: %v", err)
	}
	aUser := &model.SystemUser{Username: "notify-cross-a", Email: "a-owner@example.com", Password: "x", Role: "staff"}
	if err := db.WithContext(sysCtx).Create(aUser).Error; err != nil {
		t.Fatalf("seed 租户 A 用户失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Exec(`UPDATE system_users SET tenant_id = ? WHERE id = ?`, ta, aUser.ID).Error; err != nil {
		t.Fatalf("钉租户 A 失败: %v", err)
	}

	// 无租户绑定的裸 ctx（与 notifyFailure 的 writeCtx 同形）：
	// SystemScope 通道放行，跨租户取到 B 的归属用户。
	email, err := repo.GetEmailByID(context.Background(), bUser.ID)
	if err != nil {
		t.Fatalf("SystemScope 全局解析失败（on 模式裸 ctx）: %v", err)
	}
	if email != "b-owner@example.com" {
		t.Fatalf("应跨租户取到归属用户邮箱，实际 %q", email)
	}
}
