package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/pkg/tenant"

	"github.com/redis/go-redis/v9"
)

// RedisCache Redis 缓存实现
//
// 与 MemoryCache 的已知差异（契约测试 contract_test.go 两腿对齐）：
//  1. 亚秒 TTL 不可表达——go-redis v9 formatSec 把 <1s 的 expiration 静默截成 1s
//     （commands.go:41），MemoryCache 真支持亚秒。短 TTL 锁/限流的调用方在 Redis
//     腿拿到的实际 TTL ≥1s；要对齐两腿，TTL 一律取 ≥1s。
//  2. 未命中哨兵 ErrCacheMiss 在两腿统一（redis.Nil 在实现内转换）。
type RedisCache struct {
	client       *redis.Client
	sharedClient bool
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	PoolSize int    `yaml:"pool_size"`
}

// NewRedisCache 创建 Redis 缓存
func NewRedisCache(config RedisConfig) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.DB,
		PoolSize: config.PoolSize,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &RedisCache{
		client: client,
	}, nil
}

// scopeKey 租户分域入口（T010）：off 模式键形不变；on 模式按 ctx 租户加前缀
func (r *RedisCache) scopeKey(ctx context.Context, key string) string {
	return tenant.RedisKey(ctx, key)
}

// Get 获取缓存
//
// E-6b 契约统一（规划 §4.6b 第 1 条）：未命中时返回包内哨兵 cache.ErrCacheMiss
// （redis.Nil 在本方法内部转换），与 MemoryCache.Get 同一哨兵——
// 调用方不再需要"两腿各判一个错误"的双哨兵分支（errors.Is 兼容保留：
// 已有调用方的 redis.Nil 判断仍绿，但新调用方只写 ErrCacheMiss 即可）。
func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	key = r.scopeKey(ctx, key)
	v, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	return v, err
}

// Set 设置缓存
func (r *RedisCache) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = r.scopeKey(ctx, key)
	return r.client.Set(ctx, key, value, expiration).Err()
}

// SetNX 仅在 key 不存在时设置
func (r *RedisCache) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	key = r.scopeKey(ctx, key)
	return r.client.SetNX(ctx, key, value, expiration).Result()
}

// ReleaseLock 仅当 key 的值等于 token 时原子删除（Lua 脚本），安全释放 SetNX 获取的锁，
// 避免误删其他持有者的锁。返回 true 表示成功释放。
func (r *RedisCache) ReleaseLock(ctx context.Context, key, token string) (bool, error) {
	key = r.scopeKey(ctx, key)
	script := redis.NewScript(`if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) else return 0 end`)
	n, err := script.Run(ctx, r.client, []string{key}, token).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// LPush 向列表头部推入
// RefreshLock 仅在 token 仍匹配时原子延长锁 TTL。
func (r *RedisCache) RefreshLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("锁续期 TTL 必须大于 0")
	}
	key = r.scopeKey(ctx, key)
	script := redis.NewScript(`if redis.call('get', KEYS[1]) == ARGV[1] then
  return redis.call('pexpire', KEYS[1], ARGV[2])
else
  return 0
end`)
	n, err := script.Run(ctx, r.client, []string{key}, token, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r *RedisCache) LPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = r.scopeKey(ctx, key)
	if err := r.client.LPush(ctx, key, value).Err(); err != nil {
		return err
	}
	if expiration > 0 {
		r.client.Expire(ctx, key, expiration)
	}
	return nil
}

// RPush 向列表尾部推入
func (r *RedisCache) RPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = r.scopeKey(ctx, key)
	if err := r.client.RPush(ctx, key, value).Err(); err != nil {
		return err
	}
	if expiration > 0 {
		r.client.Expire(ctx, key, expiration)
	}
	return nil
}

// LPop 从列表头部弹出
//
// E-6b 契约统一：未命中/空列表返回 ErrCacheMiss（redis.Nil 内部转换），
// 与 MemoryCache.LPop 同一哨兵。
func (r *RedisCache) LPop(ctx context.Context, key string) (string, error) {
	key = r.scopeKey(ctx, key)
	v, err := r.client.LPop(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	return v, err
}

// LRange 获取列表区间
func (r *RedisCache) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	key = r.scopeKey(ctx, key)
	return r.client.LRange(ctx, key, start, stop).Result()
}

// PopAll 原子取出并删除整个 list（Lua 脚本保证 LRange+DEL 之间无窗口）。
func (r *RedisCache) PopAll(ctx context.Context, key string) ([]string, error) {
	key = r.scopeKey(ctx, key)
	const popAllLua = `local v = redis.call('LRANGE', KEYS[1], 0, -1)
redis.call('DEL', KEYS[1])
return v`
	res, err := r.client.Eval(ctx, popAllLua, []string{key}).Result()
	if err != nil {
		return nil, err
	}
	vals, ok := res.([]any)
	if !ok {
		return nil, fmt.Errorf("cache: unexpected type %T for key %s", res, key)
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// LLen 列表长度
func (r *RedisCache) LLen(ctx context.Context, key string) (int64, error) {
	key = r.scopeKey(ctx, key)
	return r.client.LLen(ctx, key).Result()
}

// Delete 删除缓存
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	key = r.scopeKey(ctx, key)
	return r.client.Del(ctx, key).Err()
}

// Incr 原子自增并返回新值（首次 value=1，expiration>0 时作为 TTL 应用）
func (r *RedisCache) Incr(ctx context.Context, key string, expiration time.Duration) (int64, error) {
	key = r.scopeKey(ctx, key)
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 && expiration > 0 {
		if err := r.client.Expire(ctx, key, expiration).Err(); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Exists 检查缓存是否存在
func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	key = r.scopeKey(ctx, key)
	result, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// GetJSON 获取 JSON 缓存并反序列化
//
// E-6b 契约统一：未命中返回 ErrCacheMiss（与 Get 同一哨兵）。
func (r *RedisCache) GetJSON(ctx context.Context, key string, dest any) error {
	key = r.scopeKey(ctx, key)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrCacheMiss
		}
		return err
	}
	return json.Unmarshal(data, dest)
}

// SetJSON 设置 JSON 缓存
func (r *RedisCache) SetJSON(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = r.scopeKey(ctx, key)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, data, expiration).Err()
}

// Clear 清空缓存
func (r *RedisCache) Clear(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}

// NewRedisCacheWithClient 基于已存在的（共享）Redis 客户端创建缓存实现。
// 不拥有 client 生命周期：Close 为 no-op，由调用方（main）负责关闭连接。
// 用于全局缓存单例复用 main 构建的同一 *redis.Client，避免重复连接。
func NewRedisCacheWithClient(client *redis.Client) *RedisCache {
	return &RedisCache{client: client, sharedClient: true}
}

// Close 关闭连接
func (r *RedisCache) Close() error {
	if r.sharedClient {
		return nil
	}
	return r.client.Close()
}

// Client 暴露底层 go-redis client（供需要原生命令的组件：redis_rate GCRA 等）。
// 返回 nil 表示非 Redis 后端。
func (r *RedisCache) Client() *redis.Client {
	if r == nil {
		return nil
	}
	return r.client
}
