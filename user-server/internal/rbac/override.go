package rbac

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
)

// RolePermission 角色→权限点覆盖行（rbac 包自带定义：本包不能 import model——
// testutil→rbac→model 会与 model 包的测试成环；表名与 repository 写路径同表）。
type RolePermission struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// RoleCode 矩阵角色（super_admin/team_admin/member）
	RoleCode string `gorm:"column:role_code;type:varchar(32);not null;index" json:"role_code"`
	// PermissionCode 权限点（资源.动作，目录来自 registry.yaml）
	PermissionCode string    `gorm:"column:permission_code;type:varchar(64);not null;index" json:"permission_code"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (RolePermission) TableName() string { return "role_permissions" }

// override.go 角色-权限覆盖层的执法缓存（2026-10-11）。
//
// 放在 rbac 包而不是 middleware/service：执法层（middleware）与管理层（service）
// 都要用，且 service 不能 import middleware——rbac 是两者的公共下游。
// 语义：DB 优先（角色有覆盖行则以 DB 为准）、生成为兜底、super_admin 恒全量；
// 缓存 60s，管理端变更走 InvalidateOverrides 立即生效。

var (
	overrideMu     sync.RWMutex
	overrideCache  = map[string]overrideEntry{}
	overrideTTL    = 60 * time.Second
	overrideDBFunc func(ctx context.Context) *gorm.DB // 装配方注入（InitDB/testutil），nil=无库回落生成矩阵
)

type overrideEntry struct {
	perms    []string
	override bool
	expires  time.Time
}

// SetOverrideDB 注入覆盖层查询句柄（InitDB / testutil 装配时调用一次）。
func SetOverrideDB(fn func(ctx context.Context) *gorm.DB) {
	overrideDBFunc = fn
}

// loadOverride 读覆盖层（不经 repository：rbac 不能反向依赖 repository/db，
// 用注入的 gorm 句柄直查 model.RolePermission——与 repository 的写路径同表同语义）。
func loadOverride(rbacRole string) ([]string, bool) {
	if overrideDBFunc == nil {
		return PermissionsFor(rbacRole), false
	}
	g := overrideDBFunc(context.Background())
	if g == nil {
		return PermissionsFor(rbacRole), false
	}
	var cnt int64
	if err := g.Model(&RolePermission{}).Where("role_code = ?", rbacRole).Limit(1).Count(&cnt).Error; err != nil || cnt == 0 {
		return PermissionsFor(rbacRole), false
	}
	var rows []RolePermission
	if err := g.Where("role_code = ?", rbacRole).Find(&rows).Error; err != nil {
		return PermissionsFor(rbacRole), false
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.PermissionCode)
	}
	return out, true
}

// EffectiveHas 判定矩阵角色是否持有权限点（缓存 60s）。
func EffectiveHas(rbacRole, permission string) bool {
	if rbacRole == "super_admin" {
		return true // 平台超管恒全量
	}
	overrideMu.RLock()
	entry, cached := overrideCache[rbacRole]
	overrideMu.RUnlock()
	if !cached || time.Now().After(entry.expires) {
		perms, override := loadOverride(rbacRole)
		overrideMu.Lock()
		overrideCache[rbacRole] = overrideEntry{perms: perms, override: override, expires: time.Now().Add(overrideTTL)}
		overrideMu.Unlock()
		entry = overrideEntry{perms: perms, override: override}
	}
	for _, p := range entry.perms {
		if p == permission {
			return true
		}
	}
	return false
}

// InvalidateOverrides 管理端改覆盖层后调用（service 层 SetRolePermissions 收口）。
func InvalidateOverrides() {
	overrideMu.Lock()
	overrideCache = map[string]overrideEntry{}
	overrideMu.Unlock()
}
