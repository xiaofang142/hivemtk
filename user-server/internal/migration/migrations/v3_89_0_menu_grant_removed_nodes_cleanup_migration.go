package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// MenuGrantRemovedNodesCleanupMigration v3.89.0（2026-11-10 内容重复菜单收口轮）：
// 从 role_menu_permissions 里摘掉两个已从注册表（model.SystemMenuTree）除名的
// 菜单 key 的存量分配行 —— backupEnhanced（备份高级版，页面并入「备份恢复」）
// 与 unifiedMessage（统一消息，用户视角与「统一收件箱」重复，数据面由
// 客户 360 消息轨迹与收件箱继续承载）。
//
// 为什么要在库里摘：注册表删行后，读侧（EligibleMenuKeys 交集）已经让这些
// 分配行不生效，但行还躺在表里——授权管理界面按注册表回显时它们不可见、
// 却仍在表里占位；等哪天有人新建同名 key（或注册表误恢复同名节点），
// 这些没人点过授权的存量行会静默复权。与 v3.55.0 收敛迁移同一取舍：
// 摘掉之后，重新放开必须走管理台显式授权（有操作日志的那条路）。
//
// 不分角色一把摘：admin 恒全量不落库，表里出现这两行只可能是
// customer_service / staff 的历史分配（或遗留角色码的孤儿行）——
// 两类都随节点除名一起失去意义，不存在"留着等复权"的正当场景。
//
// 幂等可重入：按 key 删除，重复执行删 0 行。
type MenuGrantRemovedNodesCleanupMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*MenuGrantRemovedNodesCleanupMigration)(nil)

// NewMenuGrantRemovedNodesCleanupMigration 构造
func NewMenuGrantRemovedNodesCleanupMigration(db *gorm.DB) *MenuGrantRemovedNodesCleanupMigration {
	return &MenuGrantRemovedNodesCleanupMigration{db: db}
}

func (m *MenuGrantRemovedNodesCleanupMigration) Version() string { return "v3.89.0" }

func (m *MenuGrantRemovedNodesCleanupMigration) Name() string {
	return "摘除已除名菜单节点（backupEnhanced/unifiedMessage）的存量授权行"
}

func (m *MenuGrantRemovedNodesCleanupMigration) Description() string {
	return "删除 role_menu_permissions 中 menu_key 属于 backupEnhanced/unifiedMessage 的行；" +
		"这两节点已从 SystemMenuTree 除名（备份高级版并入备份恢复、统一消息并入收件箱/客户360），" +
		"存量分配行只会在未来同名节点出现时静默复权"
}

// removedMenuKeys 本迁移摘除的菜单 key（与 SystemMenuTree 的除名保持同步）。
var removedMenuKeys = []string{"backupEnhanced", "unifiedMessage"}

func (m *MenuGrantRemovedNodesCleanupMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable("role_menu_permissions") {
		return nil // 首启动：表还没建，自然没有存量分配
	}
	for _, key := range removedMenuKeys {
		res := db.Exec(`DELETE FROM role_menu_permissions WHERE menu_key = ?`, key)
		if res.Error != nil {
			return fmt.Errorf("摘除 %s 的存量授权行失败: %w", key, res.Error)
		}
		if res.RowsAffected > 0 {
			fmt.Printf("[migration v3.89.0] 摘除菜单 %s 的存量授权行 %d 条\n", key, res.RowsAffected)
		}
	}
	return nil
}

func (m *MenuGrantRemovedNodesCleanupMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 授权行删除不可自动重建：哪些角色曾经被分过这两个节点只有删除前知道，
	// 盲目回插等于把没点过授权的复权写进库。降级后如需恢复，
	// 走管理台显式授权（UpdateRoleMenuKeys，有操作日志）。
	declineColumnDrop(m.Version(), "role_menu_permissions(menu_key in backupEnhanced/unifiedMessage)")
	return nil
}
