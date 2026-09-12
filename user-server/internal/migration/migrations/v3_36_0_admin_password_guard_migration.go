package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// AdminPasswordGuardMigration v3.36.0：超管账号密码保护
//
// 背景：admin（id=1 初始超管）密码多次被外部改为 admin123 导致无法登录。
// 排查结论：API 侧 operation_logs / api_interaction 日志均无针对 user id=1 的
// 改密记录，密码是被「绕过应用层直接 UPDATE system_users」的方式改掉的。
//
// 本迁移在数据库层加触发器：
//   - 禁止修改 id=1（初始超管 admin）的 password；
//   - 禁止删除 id=1；
//   - 禁止将 id=1 降级为非 admin 角色 / 停用（enabled=false 或 status!=1）。
//
// 触发器是最后一道防线，应用层四条改密入口仍保留各自的 service 级校验。
type AdminPasswordGuardMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*AdminPasswordGuardMigration)(nil)

func NewAdminPasswordGuardMigration(db *gorm.DB) *AdminPasswordGuardMigration {
	return &AdminPasswordGuardMigration{db: db}
}

func (m *AdminPasswordGuardMigration) Version() string { return "v3.36.0" }

func (m *AdminPasswordGuardMigration) Name() string {
	return "system_users 超管密码保护触发器"
}

func (m *AdminPasswordGuardMigration) Description() string {
	return "禁止修改/删除初始超管(id=1)的密码，禁止降权或停用初始超管"
}

func (m *AdminPasswordGuardMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		// 幂等：先删旧版本再建（触发器函数名固定，可重复执行）
		`CREATE OR REPLACE FUNCTION fn_guard_initial_admin_password() RETURNS trigger AS $$
		BEGIN
			-- 1) 初始超管（id=1）禁止改密码
			IF NEW.id = 1 AND TG_OP = 'UPDATE' AND NEW.password IS DISTINCT FROM OLD.password THEN
				RAISE EXCEPTION '初始超管账号(id=1)的密码不允许被修改，请使用平台提供的密码恢复流程';
			END IF;
			-- 2) 初始超管禁止降权/停用
			IF NEW.id = 1 AND TG_OP = 'UPDATE' THEN
				IF NEW.role IS DISTINCT FROM 'admin' THEN
					RAISE EXCEPTION '初始超管账号(id=1)的角色不允许变更';
				END IF;
				IF NEW.enabled = FALSE OR NEW.status <> 1 THEN
					RAISE EXCEPTION '初始超管账号(id=1)不允许被停用';
				END IF;
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS trg_guard_initial_admin_password ON system_users`,
		`CREATE TRIGGER trg_guard_initial_admin_password
			BEFORE UPDATE ON system_users
			FOR EACH ROW EXECUTE FUNCTION fn_guard_initial_admin_password()`,
		`DROP TRIGGER IF EXISTS trg_guard_initial_admin_delete ON system_users`,
		`CREATE TRIGGER trg_guard_initial_admin_delete
			BEFORE DELETE ON system_users
			FOR EACH ROW WHEN (OLD.id = 1)
			EXECUTE FUNCTION fn_guard_initial_admin_delete()`,
		`CREATE OR REPLACE FUNCTION fn_guard_initial_admin_delete() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION '初始超管账号(id=1)不允许被删除';
		END;
		$$ LANGUAGE plpgsql`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("执行语句失败: %w\nSQL: %s", err, s)
		}
	}
	return nil
}

func (m *AdminPasswordGuardMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`DROP TRIGGER IF EXISTS trg_guard_initial_admin_password ON system_users`,
		`DROP TRIGGER IF EXISTS trg_guard_initial_admin_delete ON system_users`,
		`DROP FUNCTION IF EXISTS fn_guard_initial_admin_password()`,
		`DROP FUNCTION IF EXISTS fn_guard_initial_admin_delete()`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
