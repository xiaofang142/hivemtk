// Package scope 提供 GORM 通用查询 scope（数据隔离维度）。
//
// 用法：
//
//	db.Scopes(scope.TenantScope(ctx, uid)).Find(&list)
//	db.Scopes(scope.OwnedBy(ctx, uid)).First(&m)
//
// 设计原则：
//   - scope 是纯函数（func(db *gorm.DB) *gorm.DB），与具体 model 解耦；
//   - 所有 scope 必须从 ctx 取 uid/admin 标记，禁止签名参数传入（避免调用方绕过 ctx）；
//   - 不在 scope 内记日志（保持纯函数语义，日志由调用方记录）。
package scope

import (
	"context"

	"gorm.io/gorm"
)

const ginUserKey = "user_id"

const roleAdminKey = "role"

func fromGinContext(ctx context.Context, key string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	// 写入侧统一用 ginCtxKey 类型键；同时兼容历史裸字符串键（如中间件直接
	// WithValue(ctx, "user_id", ...)），两种键都能命中。
	if v := ctx.Value(ginCtxKey(key)); v != nil {
		return v, true
	}
	if v := ctx.Value(key); v != nil {
		return v, true
	}
	return nil, false
}

// SetGinValuesToCtx 把 gin 上下文中的用户/角色信息注入到 ctx.Value，
// 供 service → scope 链路使用。
//
// 推荐在 controller 入口统一调用：
//
//	ctx = scope.SetGinValuesToCtx(c.Request.Context(), c)
//	db.Scopes(scope.TenantScope(ctx)).Find(&list)
//
// 已存在则跳过，避免覆盖 admin 强制提权标记。
func SetGinValuesToCtx(ctx context.Context, c interface {
	Get(string) (any, bool)
}) context.Context {
	if ctx == nil || c == nil {
		return ctx
	}
	if v, ok := c.Get(ginUserKey); ok && v != nil {
		if _, exists := fromGinContext(ctx, ginUserKey); !exists {
			ctx = context.WithValue(ctx, ginCtxKey(ginUserKey), v)
		}
	}
	if v, ok := c.Get(roleAdminKey); ok && v != nil {
		if _, exists := fromGinContext(ctx, roleAdminKey); !exists {
			ctx = context.WithValue(ctx, ginCtxKey(roleAdminKey), v)
		}
	}
	return ctx
}

type ginCtxKey string

func (k ginCtxKey) String() string { return "scope:" + string(k) }

// TenantScope 租户隔离 scope：owner_id = ? OR owner_id = 0。
//
// 语义：
//   - uid == 0（未登录）→ 仅返回 owner_id = 0 的系统级数据；
//   - role == "admin" → 不附加 uid 条件，返回全量；
//   - 其他用户 → 仅返回 owner_id = uid 的数据，同时允许 owner_id = 0 的系统数据可见。
//
// 注意：owner_id = 0 的数据默认对所有租户可见——若业务需「严格隔离」，
// 请改用 StrictTenantScope（不允许 owner_id = 0 漏出）。
func TenantScope(ctx context.Context) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if db == nil {
			return db
		}
		if isAdmin(ctx) {
			return db
		}
		uid := currentUID(ctx)
		if uid == 0 {
			return db.Where("owner_id = ?", 0)
		}
		return db.Where("owner_id = ? OR owner_id = ?", uid, 0)
	}
}

// StrictTenantScope 严格租户隔离：仅返回 owner_id = uid 的数据（不含 owner_id=0）。
//
// 适用：客户数据/账单/私密设置等不允许系统资源混入的场景。
func StrictTenantScope(ctx context.Context) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if db == nil {
			return db
		}
		if isAdmin(ctx) {
			return db
		}
		uid := currentUID(ctx)
		if uid == 0 {
			return db.Where("1 = 0")
		}
		return db.Where("owner_id = ?", uid)
	}
}

// OwnedBy 简易归属过滤：仅 owner_id = uid（不包含系统资源）。
//
// 与 StrictTenantScope 等价；提供更短别名便于 Repository 调用。
func OwnedBy(ctx context.Context) func(db *gorm.DB) *gorm.DB {
	return StrictTenantScope(ctx)
}

func currentUID(ctx context.Context) uint {
	v, ok := fromGinContext(ctx, ginUserKey)
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case uint:
		return x
	case uint64:
		return uint(x)
	case int:
		if x < 0 {
			return 0
		}
		return uint(x)
	case int64:
		if x < 0 {
			return 0
		}
		return uint(x)
	}
	return 0
}

func isAdmin(ctx context.Context) bool {
	v, ok := fromGinContext(ctx, roleAdminKey)
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && s == "admin"
}

// WithUID 显式把 uid 注入 ctx（service 层测试/批处理场景）。
//
// 不推荐生产代码使用——生产环境应通过 SetGinValuesToCtx 注入，避免
// 业务侧在调用栈深处「手动构造身份」。
func WithUID(ctx context.Context, uid uint) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, ginCtxKey(ginUserKey), uid)
}

// WithRole 显式注入 role（与 WithUID 同理）。
func WithRole(ctx context.Context, role string) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, ginCtxKey(roleAdminKey), role)
}
