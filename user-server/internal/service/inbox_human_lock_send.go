package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/redis/go-redis/v9"
)

// ConfirmHumanLockBeforeSend 不复用入站的关键词降级：生成可以继续，抢答不能发生。
// 仅明确未命中或 false 允许外发；不可用与未知值不是“没有人工接管”。
func (s *InboxIngressService) ConfirmHumanLockBeforeSend(ctx context.Context, sessionID string) (bool, error) {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return false, errors.New("人工锁状态不可确认：租户上下文缺失")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return false, errors.New("人工锁状态不可确认：租户未绑定")
	}
	if s == nil || s.cache == nil || sessionID == "" {
		return false, errors.New("人工锁状态不可确认：缓存或会话身份缺失")
	}
	value, err := s.cache.Get(tenant.OnlyTenant(ctx, tenantID), InboxHumanLockKey+sessionID)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if errors.Is(err, redis.Nil) || errors.Is(err, cache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("发送前人工锁读取失败: %w", err)
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("人工锁值不可确认")
	}
}
