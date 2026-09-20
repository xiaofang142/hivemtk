package migrations

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.36.0 的删除保护此前从未在任何库上建立成功：stmts 里 CREATE TRIGGER
// trg_guard_initial_admin_delete 引用了后面一条才 CREATE 的函数，PG 在建触发器时
// 就要解析函数签名 ⇒ 首错即 return，前面的改密触发器留下、后面的删除触发器与函数永远缺位。

func pgFunctionExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
		WHERE n.nspname='public' AND p.proname=?)`, name).Scan(&exists).Error; err != nil {
		t.Fatalf("函数存在性查询失败 %s: %v", name, err)
	}
	return exists
}

func pgTriggerExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
		WHERE c.relname='system_users' AND t.tgname=? AND NOT t.tgisinternal)`, name).
		Scan(&exists).Error; err != nil {
		t.Fatalf("触发器存在性查询失败 %s: %v", name, err)
	}
	return exists
}

// TestAdminPasswordGuardMigration_GuardBehavior 逐条验证触发器的拦截范围。
//
// 必须显式恢复 session_replication_role：testutil.NewTestDB 在建表前执行过
// `SET session_replication_role = 'replica'`，该会话级设置会让 PG 跳过用户触发器——
// 沿用同一连接跑下面的 DELETE 断言，「没被拦」与「保护没生效」根本分不开。
func TestAdminPasswordGuardMigration_GuardBehavior(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SystemUser{})
	ctx := context.Background()
	if err := NewAdminPasswordGuardMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	seed := []model.SystemUser{
		{ID: 1, Username: "guard-root", Password: "p", Role: "admin", Status: 1, Enabled: true},
		{ID: 2, Username: "guard-staff", Password: "p", Role: "staff", Status: 1, Enabled: true},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("预置账号失败 id=%d: %v", seed[i].ID, err)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("取独占连接失败: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET session_replication_role = 'origin'`); err != nil {
		t.Fatalf("恢复触发器开关失败: %v", err)
	}

	cases := []struct {
		name       string
		sql        string
		wantDenied bool
	}{
		{"改初始超管密码被拒", `UPDATE system_users SET password = 'evil' WHERE id = 1`, true},
		{"改其他账号密码放行", `UPDATE system_users SET password = 'ok' WHERE id = 2`, false},
		{"初始超管降权被拒", `UPDATE system_users SET role = 'staff' WHERE id = 1`, true},
		{"初始超管停用被拒", `UPDATE system_users SET enabled = FALSE WHERE id = 1`, true},
		{"初始超管置灰被拒", `UPDATE system_users SET status = 0 WHERE id = 1`, true},
		// 保护只针对凭据/角色/启停；误伤日常资料写入的守卫会在下次运维改昵称时把整条链路打断
		{"初始超管改昵称放行", `UPDATE system_users SET real_name = '新昵称' WHERE id = 1`, false},
		{"删除其他账号放行", `DELETE FROM system_users WHERE id = 2`, false},
		{"删除初始超管被拒", `DELETE FROM system_users WHERE id = 1`, true},
	}
	for _, c := range cases {
		_, execErr := conn.ExecContext(ctx, c.sql)
		if !c.wantDenied {
			if execErr != nil {
				t.Errorf("%s：应放行却报错 %v", c.name, execErr)
			}
			continue
		}
		if execErr == nil {
			t.Errorf("%s：应被触发器拒绝，却执行成功", c.name)
			continue
		}
		if !strings.Contains(execErr.Error(), "初始超管") {
			t.Errorf("%s：报错信息未含守卫说明，got %v", c.name, execErr)
		}
	}
}

func TestAdminPasswordGuardMigration_Meta(t *testing.T) {
	m := NewAdminPasswordGuardMigration(nil)
	if m.Version() != "v3.36.0" {
		t.Errorf("Version()=%q want=v3.36.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestAdminPasswordGuardMigration_NilDB(t *testing.T) {
	m := NewAdminPasswordGuardMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

// TestAdminPasswordGuardMigration_UpBuildsBothGuards 证明「Up 成功」不等于「两道保护都在」：
// 只有函数与触发器同时存在，删除保护才算真的落地。
func TestAdminPasswordGuardMigration_UpBuildsBothGuards(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SystemUser{})
	ctx := context.Background()
	m := NewAdminPasswordGuardMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	for _, fn := range []string{"fn_guard_initial_admin_password", "fn_guard_initial_admin_delete"} {
		if !pgFunctionExists(t, db, fn) {
			t.Errorf("Up 后函数 %s 应存在", fn)
		}
	}
	for _, trg := range []string{"trg_guard_initial_admin_password", "trg_guard_initial_admin_delete"} {
		if !pgTriggerExists(t, db, trg) {
			t.Errorf("Up 后触发器 %s 应存在", trg)
		}
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("二次 Down() 应幂等: %v", err)
	}
	for _, fn := range []string{"fn_guard_initial_admin_password", "fn_guard_initial_admin_delete"} {
		if pgFunctionExists(t, db, fn) {
			t.Errorf("Down 后函数 %s 应已删除", fn)
		}
	}
	for _, trg := range []string{"trg_guard_initial_admin_password", "trg_guard_initial_admin_delete"} {
		if pgTriggerExists(t, db, trg) {
			t.Errorf("Down 后触发器 %s 应已删除", trg)
		}
	}
}
