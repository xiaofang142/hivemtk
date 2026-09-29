package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/cache"

	"github.com/redis/go-redis/v9"
)

// redisNilCache 模拟生产 RedisCache：key 不存在时 Get 返回 redis.Nil（不是故障）。
type redisNilCache struct {
	cache.Cache
}

func (r *redisNilCache) Get(_ context.Context, _ string) (string, error) {
	return "", redis.Nil
}

// TestInboxIngress_HumanLock_RedisNilIsMiss 未命中＝该会话无人工锁：即使最近消息命中转人工关键词，
// 也必须走「未命中」分支返回 false，不能被当成「Redis 故障」落进 fail-closed/fail-open 降级。
func TestInboxIngress_HumanLock_RedisNilIsMiss(t *testing.T) {
	ctx := context.Background()
	svc := NewInboxIngressServiceWithDB(nil, &redisNilCache{})

	locked, err := svc.IsSessionHumanLocked(ctx, "sess_nil_miss", "这个问题我要转人工处理")
	if err != nil {
		t.Fatalf("key 未命中不应返回错误，实际: %v", err)
	}
	if locked {
		t.Fatal("redis.Nil 是未命中不是故障，不该因关键词命中判定为人工接管")
	}
}

// TestInboxIngress_HumanLock_RealFailureStillFailClosed 真故障分支不能被改松：
// Redis 读取失败且命中转人工关键词时仍要 fail-closed 判定为人工接管。
func TestInboxIngress_HumanLock_RealFailureStillFailClosed(t *testing.T) {
	ctx := context.Background()
	svc := NewInboxIngressServiceWithDB(nil, &brokenCache{})

	locked, err := svc.IsSessionHumanLocked(ctx, "sess_broken_closed", "这个问题我要转人工处理")
	if err != nil {
		t.Fatalf("降级路径不应返回错误，实际: %v", err)
	}
	if !locked {
		t.Fatal("Redis 真故障且命中关键词应 fail-closed 返回 true")
	}
}

// TestInboxIngress_HumanLock_RealFailureNoKeywordFailOpen 真故障且未命中关键词 → 放行 AI。
func TestInboxIngress_HumanLock_RealFailureNoKeywordFailOpen(t *testing.T) {
	ctx := context.Background()
	svc := NewInboxIngressServiceWithDB(nil, &brokenCache{})

	locked, err := svc.IsSessionHumanLocked(ctx, "sess_broken_open", "你好，这个产品多少钱")
	if err != nil {
		t.Fatalf("降级路径不应返回错误，实际: %v", err)
	}
	if locked {
		t.Fatal("Redis 真故障但未命中关键词应放行 AI 返回 false")
	}
}

// TestInboxIngress_HumanLock_MemoryCacheMissIsMiss MemoryCache 的 ErrCacheMiss 同样按未命中处理。
func TestInboxIngress_HumanLock_MemoryCacheMissIsMiss(t *testing.T) {
	ctx := context.Background()
	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(nil, mc)

	locked, err := svc.IsSessionHumanLocked(ctx, "sess_mem_miss", "这个问题我要转人工处理")
	if err != nil {
		t.Fatalf("未命中不应返回错误，实际: %v", err)
	}
	if locked {
		t.Fatal("cache.ErrCacheMiss 是未命中，不该判定为人工接管")
	}
}
