package agent_runtime

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"hivemtk-user/internal/pkg/utils/logger"
)

const replyGuardTTL = 10 * time.Minute

const replyGuardKeyPrefix = "mtk:reply_guard:"

type replyGuardBackend interface {
	claim(eventID string) (bool, error)
	release(eventID string)
	has(eventID string) bool
}

type localReplyGuard struct {
	mu      sync.Mutex
	claimed map[string]time.Time
	ttl     time.Duration
}

func newLocalReplyGuard(ttl time.Duration) *localReplyGuard {
	return &localReplyGuard{claimed: make(map[string]time.Time), ttl: ttl}
}

func (g *localReplyGuard) claim(eventID string) (bool, error) {
	if eventID == "" {
		return true, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for k, t := range g.claimed {
		if now.Sub(t) > g.ttl {
			delete(g.claimed, k)
		}
	}
	if t, ok := g.claimed[eventID]; ok && now.Sub(t) < g.ttl {
		return false, nil
	}
	g.claimed[eventID] = now
	return true, nil
}

func (g *localReplyGuard) release(eventID string) {
	if eventID == "" {
		return
	}
	g.mu.Lock()
	delete(g.claimed, eventID)
	g.mu.Unlock()
}

func (g *localReplyGuard) has(eventID string) bool {
	if eventID == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.claimed[eventID]
	return ok && time.Since(t) < g.ttl
}

type redisReplyGuard struct {
	rdb   *redis.Client
	ttl   time.Duration
	local *localReplyGuard
}

func (g *redisReplyGuard) claim(eventID string) (bool, error) {
	if eventID == "" {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok, err := g.rdb.SetNX(ctx, replyGuardKeyPrefix+eventID, "1", g.ttl).Result()
	if err != nil {
		logger.Warnf("[reply_guard] redis claim error, degrade to local: %v", err)
		return g.local.claim(eventID)
	}
	return ok, nil
}

func (g *redisReplyGuard) release(eventID string) {
	if eventID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = g.rdb.Del(ctx, replyGuardKeyPrefix+eventID).Err()
	g.local.release(eventID)
}

func (g *redisReplyGuard) has(eventID string) bool {
	if eventID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := g.rdb.Exists(ctx, replyGuardKeyPrefix+eventID).Result()
	if err != nil {
		return g.local.has(eventID)
	}
	return n > 0
}

var (
	guardMu       sync.RWMutex
	activeBackend replyGuardBackend = newLocalReplyGuard(replyGuardTTL)
)

// SetReplyGuardRedis 启用 Redis 分布式幂等守卫（多实例水平扩展时调用）。
//
// rdb 为 nil 或 Ping 不可达时不启用，保持进程内守卫（单实例默认行为）。
// 调用时机：main.go 启动阶段，Redis 客户端就绪后。
func SetReplyGuardRedis(rdb *redis.Client) {
	if rdb == nil {
		return
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Warnf("[reply_guard] Redis 不可用，保持进程内守卫: %v", err)
		return
	}
	guardMu.Lock()
	activeBackend = &redisReplyGuard{rdb: rdb, ttl: replyGuardTTL, local: newLocalReplyGuard(replyGuardTTL)}
	guardMu.Unlock()
	logger.Infof("[reply_guard] 已切换为 Redis 分布式幂等守卫（支持多实例水平扩展）")
}

// ClaimReply 尝试认领指定 EventID 的回复权。
//   - 返回 true ：本调用方为第一个认领者，应当执行出站。
//   - 返回 false：已被其他链路/实例认领，调用方必须跳过出站（避免重复消息）。
//
// 当 EventID 为空时退化为「允许出站」但不做幂等保护，避免误拦截正常消息。
func ClaimReply(eventID string) bool {
	guardMu.RLock()
	b := activeBackend
	guardMu.RUnlock()
	ok, _ := b.claim(eventID)
	return ok
}

// ReleaseReply 释放某 EventID 的认领（出站失败后调用，允许平台重投重试）。
// 配合 webhook.sendOutbound 的 claim-before-confirm 机制使用。
func ReleaseReply(eventID string) {
	guardMu.RLock()
	b := activeBackend
	guardMu.RUnlock()
	b.release(eventID)
}

// HasReplied 只读判断某 EventID 是否已被认领回复（不修改状态）。
// 供异步事件总线订阅者使用：若同步主链路已认领，则总线直接跳过，退化为纯观察。
func HasReplied(eventID string) bool {
	guardMu.RLock()
	b := activeBackend
	guardMu.RUnlock()
	return b.has(eventID)
}

// MarkReplied 显式标记某 EventID 已回复（幂等置位，供同步出站前调用）。
// 与 ClaimReply 等价，保留以语义化调用点。
func MarkReplied(eventID string) {
	if eventID == "" {
		return
	}
	guardMu.RLock()
	b := activeBackend
	guardMu.RUnlock()
	// claim 仅用于置位：bool 结果对「标记已回复」无意义；后端实现不会返回非 nil error
	// （Redis 分支内部已记日志并降级到本地），故显式丢弃。
	_, _ = b.claim(eventID)
}
