package cache

import (
	"context"
	"errors"
	"time"
)

// ErrLockRefreshUnsupported 后端不支持原子续期；调用方必须 fail-closed，不能退化为无主锁。
var ErrLockRefreshUnsupported = errors.New("cache backend does not support lock refresh")

// Cache 缓存接口
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key, token string) (bool, error)
	Incr(ctx context.Context, key string, expiration time.Duration) (int64, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	GetJSON(ctx context.Context, key string, dest any) error
	SetJSON(ctx context.Context, key string, value any, expiration time.Duration) error
	LPush(ctx context.Context, key string, value any, expiration time.Duration) error
	RPush(ctx context.Context, key string, value any, expiration time.Duration) error
	LPop(ctx context.Context, key string) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)

	PopAll(ctx context.Context, key string) ([]string, error)
	LLen(ctx context.Context, key string) (int64, error)
	Clear(ctx context.Context) error
}
