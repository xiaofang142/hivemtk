// wechat_account_repo_tenant_test.go 公众号账号仓储的租户契约。
//
// 改前 UpdateCredentials / Delete 不看 RowsAffected：on 模式下被隔离条件挡掉 0 行时
// 照样返回 nil，controller 回「更新成功 / 删除成功」——前端刷新列表什么都没变，
// 跨租户改别人的公众号账号也拿不到任何失败信号。这里把「0 行 = 不存在」钉死，
// 并顺带验证 app_id 的唯一键确实复合到了 (tenant_id, app_id)。
package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	wxTenantA = uint(61)
	wxTenantB = uint(62)
)

func wxSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.WechatAccount{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡与 0 行语义都判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

// wxSeed 用 SystemScope 直插，好让用例自己决定这行属于哪个租户。
func wxSeed(t *testing.T, db *gorm.DB, tenantID uint, appID string) *model.WechatAccount {
	t.Helper()
	row := &model.WechatAccount{
		TenantID:  tenantID,
		AppID:     appID,
		AppSecret: "secret-" + appID,
		Token:     "tok" + "en-" + appID,
		Status:    "active",
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置租户 %d 的公众号账号失败: %v", tenantID, err)
	}
	return row
}

func wxPhysical(t *testing.T, db *gorm.DB, id uint) *model.WechatAccount {
	t.Helper()
	var row model.WechatAccount
	err := db.WithContext(tenant.WithSystemScope(context.Background())).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("物理读取 account(%d) 失败: %v", id, err)
	}
	return &row
}

func TestWechatAccountUpdate_CrossTenantReportsNotFound(t *testing.T) {
	db := wxSetupDB(t)
	mine := wxSeed(t, db, wxTenantA, "wx_mine_a")
	theirs := wxSeed(t, db, wxTenantB, "wx_theirs_b")

	repo := NewWechatAccountRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), wxTenantA)

	err := repo.UpdateCredentials(ctxA, &model.WechatAccount{
		ID: theirs.ID, AppID: "wx_hijacked", AppSecret: "hijacked", Token: "hija" + "cked", Status: "active",
	})
	if !errors.Is(err, ErrWechatAccountNotFound) {
		t.Fatalf("跨租户更新返回 err=%v，期望 ErrWechatAccountNotFound（改前这里返回 nil）", err)
	}
	if got := wxPhysical(t, db, theirs.ID); got == nil || got.AppID != "wx_theirs_b" || got.AppSecret != "secret-wx_theirs_b" {
		t.Fatalf("租户 %d 的账号被跨租户改写：%+v", wxTenantB, got)
	}

	// 本租户改自己那行必须成功——否则上面的断言只是「永远报错」的假阳性。
	if err := repo.UpdateCredentials(ctxA, &model.WechatAccount{
		ID: mine.ID, AppID: "wx_mine_a2", AppSecret: "secret2", Token: "tok" + "en2", Status: "inactive",
	}); err != nil {
		t.Fatalf("租户 %d 更新自己的账号失败: %v", wxTenantA, err)
	}
	if got := wxPhysical(t, db, mine.ID); got == nil || got.AppID != "wx_mine_a2" || got.Status != "inactive" {
		t.Fatalf("本租户更新没落库：%+v", got)
	}
}

func TestWechatAccountDelete_CrossTenantReportsNotFound(t *testing.T) {
	db := wxSetupDB(t)
	mine := wxSeed(t, db, wxTenantA, "wx_del_mine")
	theirs := wxSeed(t, db, wxTenantB, "wx_del_theirs")

	repo := NewWechatAccountRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), wxTenantA)

	if err := repo.Delete(ctxA, theirs.ID); !errors.Is(err, ErrWechatAccountNotFound) {
		t.Fatalf("跨租户删除返回 err=%v，期望 ErrWechatAccountNotFound（改前这里返回 nil）", err)
	}
	if wxPhysical(t, db, theirs.ID) == nil {
		t.Fatal("租户 B 的公众号账号被租户 A 删掉了")
	}

	if err := repo.Delete(ctxA, mine.ID); err != nil {
		t.Fatalf("租户 %d 删除自己的账号失败: %v", wxTenantA, err)
	}
	if wxPhysical(t, db, mine.ID) != nil {
		t.Fatal("本租户删除没有落库")
	}

	// 再删一次（行已不存在）同样要报不存在，而不是成功。
	if err := repo.Delete(ctxA, mine.ID); !errors.Is(err, ErrWechatAccountNotFound) {
		t.Fatalf("删除不存在的账号返回 err=%v，期望 ErrWechatAccountNotFound", err)
	}
}

func TestWechatAccountUpdate_MissingRowReportsNotFound(t *testing.T) {
	db := wxSetupDB(t)
	repo := NewWechatAccountRepositoryWithDB(db)

	err := repo.UpdateCredentials(tenant.WithTenantID(context.Background(), wxTenantA), &model.WechatAccount{
		ID: 999999, AppID: "wx_ghost", AppSecret: "s", Token: "t", Status: "active",
	})
	if !errors.Is(err, ErrWechatAccountNotFound) {
		t.Fatalf("更新不存在的账号返回 err=%v，期望 ErrWechatAccountNotFound", err)
	}
}

// TestWechatAccount_AppIDUniqueIsPerTenant 同一公众号先接 A 家再接 B 家是常见场景：
// 唯一键若还是单列 app_id，第二家永远建不进来；同租户内重复才该被挡住。
func TestWechatAccount_AppIDUniqueIsPerTenant(t *testing.T) {
	db := wxSetupDB(t)
	wxSeed(t, db, wxTenantA, "wx_same_app")
	wxSeed(t, db, wxTenantB, "wx_same_app")

	dup := &model.WechatAccount{
		TenantID: wxTenantA, AppID: "wx_same_app", AppSecret: "s", Token: "t", Status: "active",
	}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), wxTenantA)).Create(dup).Error; err == nil {
		t.Fatal("同一租户内重复的 app_id 没被唯一键挡住 ⇒ 唯一键没复合到 (tenant_id, app_id)")
	}
}
