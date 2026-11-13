package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/pkg/utils"
)

// GuardOwnerIDWithUID 是 service 层的归属断言工具（显式传 uid，不依赖 HTTP 框架类型）。
//
// 业务语义：uid 必须等于 resourceOwnerID，或者 resourceOwnerID == 0（系统级资源）才算合法；
// isAdmin=true 时豁免（与 middleware.RequireOwnerWithChecker 的策略对齐）。
// 不合法 → 返回 utils.ErrForbidden 哨兵（controller 层
// 经 response.ErrorFromDB 自动映射为 HTTP 403）。
//
// 用法：
//
//	if err := GuardOwnerIDWithUID(uid, ownerID, isAdmin); err != nil {
//	    return err
//	}
//
// 严格模式（无 admin 豁免，防「admin 偷用其他租户资源」越权路径）传 isAdmin=false，
// 适用内部服务调用、SOP outbox 重放等场景。
func GuardOwnerIDWithUID(uid, resourceOwnerID uint, isAdmin bool) error {
	if resourceOwnerID == 0 {
		return nil
	}
	if isAdmin {
		return nil
	}
	if uid == 0 {
		return fmt.Errorf("%w: anonymous user", utils.ErrForbidden)
	}
	if uid != resourceOwnerID {
		return fmt.Errorf("%w: uid=%d != owner=%d", utils.ErrForbidden, uid, resourceOwnerID)
	}
	return nil
}

// GuardOwnerIDFromContext 接受普通 context.Context 的兜底版本。
//
// 当前实现不支持从非 gin ctx 读取 uid（避免反射）——返回 ErrForbidden 由调用方处理。
//
// 保留入口仅为兼容未来从 context.Context 拿 uid 的中间链路（如异步任务）。
func GuardOwnerIDFromContext(ctx context.Context, resourceOwnerID uint) error {
	_ = ctx
	return fmt.Errorf("%w: GuardOwnerIDFromContext requires uid (use GuardOwnerIDWithUID)", utils.ErrForbidden)
}

// IsForbidden 便捷判断：errors.Is(err, utils.ErrForbidden)。
func IsForbidden(err error) bool {
	return errors.Is(err, utils.ErrForbidden)
}
