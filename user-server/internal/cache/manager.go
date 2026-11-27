package cache

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits   int64
	Misses int64
}

// HitRate 计算命中率
func (s *CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}

// Total 返回总查询次数
func (s *CacheStats) Total() int64 {
	return s.Hits + s.Misses
}

// CacheManager 缓存管理器
type CacheManager struct {
	cache Cache
	stats CacheStats
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Type       string        `yaml:"type"`
	Redis      RedisConfig   `yaml:"redis"`
	DefaultTTL time.Duration `yaml:"default_ttl"`
}

// NewCacheManager 创建缓存管理器
func NewCacheManager(config CacheConfig) (*CacheManager, error) {
	var cache Cache
	var err error

	switch config.Type {
	case "redis":
		cache, err = NewRedisCache(config.Redis)
		if err != nil {
			logger.Errorf("Redis 连接失败，使用内存缓存：%v", err)
			cache = NewMemoryCache()
		}
	case "memory", "":
		cache = NewMemoryCache()
	default:
		cache = NewMemoryCache()
	}

	return &CacheManager{
		cache: cache,
	}, nil
}

// GetStats 返回缓存统计信息（副本）
func (m *CacheManager) GetStats() CacheStats {
	return CacheStats{
		Hits:   atomic.LoadInt64(&m.stats.Hits),
		Misses: atomic.LoadInt64(&m.stats.Misses),
	}
}

// ResetStats 重置缓存统计
func (m *CacheManager) ResetStats() {
	atomic.StoreInt64(&m.stats.Hits, 0)
	atomic.StoreInt64(&m.stats.Misses, 0)
}

// Get 获取缓存（带统计）
func (m *CacheManager) Get(ctx context.Context, key string) (string, error) {
	val, err := m.cache.Get(ctx, key)
	if err == nil && val != "" {
		atomic.AddInt64(&m.stats.Hits, 1)
	} else {
		atomic.AddInt64(&m.stats.Misses, 1)
	}
	return val, err
}

// Set 设置缓存
func (m *CacheManager) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	if expiration == 0 {
		expiration = m.getDefaultTTL()
	}
	return m.cache.Set(ctx, key, value, expiration)
}

// SetNX 仅在 key 不存在时设置
func (m *CacheManager) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	if expiration == 0 {
		expiration = m.getDefaultTTL()
	}
	return m.cache.SetNX(ctx, key, value, expiration)
}

// ReleaseLock 安全释放分布式锁（仅删除持有者自己的锁）
// PopAll 原子取出并删除整个 list（委托底层实现）。
func (m *CacheManager) PopAll(ctx context.Context, key string) ([]string, error) {
	return m.cache.PopAll(ctx, key)
}

func (m *CacheManager) ReleaseLock(ctx context.Context, key, token string) (bool, error) {
	return m.cache.ReleaseLock(ctx, key, token)
}

// RefreshLock 仅在 token 仍匹配时原子延长锁 TTL；不支持续期的后端返回明确错误。
func (m *CacheManager) RefreshLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("锁续期 TTL 必须大于 0")
	}
	refresher, ok := m.cache.(interface {
		RefreshLock(context.Context, string, string, time.Duration) (bool, error)
	})
	if !ok {
		return false, ErrLockRefreshUnsupported
	}
	return refresher.RefreshLock(ctx, key, token, ttl)
}

// LPush 向列表头部推入
func (m *CacheManager) LPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	if expiration == 0 {
		expiration = m.getDefaultTTL()
	}
	return m.cache.LPush(ctx, key, value, expiration)
}

// RPush 向列表尾部推入
func (m *CacheManager) RPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	if expiration == 0 {
		expiration = m.getDefaultTTL()
	}
	return m.cache.RPush(ctx, key, value, expiration)
}

// LPop 从列表头部弹出
func (m *CacheManager) LPop(ctx context.Context, key string) (string, error) {
	return m.cache.LPop(ctx, key)
}

// LRange 获取列表区间
func (m *CacheManager) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return m.cache.LRange(ctx, key, start, stop)
}

// LLen 列表长度
func (m *CacheManager) LLen(ctx context.Context, key string) (int64, error) {
	return m.cache.LLen(ctx, key)
}

// Delete 删除缓存
func (m *CacheManager) Delete(ctx context.Context, key string) error {
	return m.cache.Delete(ctx, key)
}

// Exists 检查缓存是否存在
func (m *CacheManager) Exists(ctx context.Context, key string) (bool, error) {
	return m.cache.Exists(ctx, key)
}

// GetJSON 获取 JSON 缓存并反序列化（带统计）
func (m *CacheManager) GetJSON(ctx context.Context, key string, dest any) error {
	err := m.cache.GetJSON(ctx, key, dest)
	if err == nil {
		atomic.AddInt64(&m.stats.Hits, 1)
	} else {
		atomic.AddInt64(&m.stats.Misses, 1)
	}
	return err
}

// SetJSON 设置 JSON 缓存
func (m *CacheManager) SetJSON(ctx context.Context, key string, value any, expiration time.Duration) error {
	if expiration == 0 {
		expiration = m.getDefaultTTL()
	}
	return m.cache.SetJSON(ctx, key, value, expiration)
}

// Clear 清空缓存
func (m *CacheManager) Clear(ctx context.Context) error {
	return m.cache.Clear(ctx)
}

func (m *CacheManager) getDefaultTTL() time.Duration {
	return 30 * time.Minute
}

// Close 关闭缓存连接
func (m *CacheManager) Close() error {
	if redisCache, ok := m.cache.(*RedisCache); ok {
		return redisCache.Close()
	}
	return nil
}
