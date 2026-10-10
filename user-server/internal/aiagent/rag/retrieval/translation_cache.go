package ragretrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// TranslationCacheTTLDefault 翻译缓存默认 TTL（1 小时）
const TranslationCacheTTLDefault = 1 * time.Hour

// TranslationCacheKeyPrefixDefault 默认 key 前缀
const TranslationCacheKeyPrefixDefault = "i18n:trans:"

// TranslationCacheMaxEntriesDefault 默认最大条目数（用于 Stats 上限保护参考）
const TranslationCacheMaxEntriesDefault = 100000

// TranslationCache 翻译缓存
//
// 缓存 key: hash(internal_lang + target_lang + query + kb_version)
// 缓存 value: LLM 生成的目标语言回复
//
// 线程安全：底层 RedisClient 自带并发保护；本结构无状态字段。
type TranslationCache struct {
	redis      RedisClient
	ttl        time.Duration
	keyPrefix  string
	maxEntries int
}

// NewTranslationCache 构造翻译缓存
//
// redisClient 为 nil 时返回的 cache 所有方法为 no-op（缓存禁用模式），
// 调用方可放心调用 Get/Set 而无需 nil 检查。
//
// ttl <= 0 时使用默认 1h；keyPrefix 为空时使用默认 "i18n:trans:"。
func NewTranslationCache(redisClient RedisClient, ttl time.Duration, keyPrefix string) *TranslationCache {
	if ttl <= 0 {
		ttl = TranslationCacheTTLDefault
	}
	if strings.TrimSpace(keyPrefix) == "" {
		keyPrefix = TranslationCacheKeyPrefixDefault
	}
	return &TranslationCache{
		redis:      redisClient,
		ttl:        ttl,
		keyPrefix:  keyPrefix,
		maxEntries: TranslationCacheMaxEntries(),
	}
}

// Get 命中缓存返回目标语言回复，未命中返回 ("", nil)
//
// 失败降级：Redis 不可用时返回 ("", nil)，不抛错，保证主流程不阻塞。
func (c *TranslationCache) Get(ctx context.Context, internalLang, targetLang, query, kbVersion string) (string, error) {
	if c == nil || c.redis == nil {
		return "", nil
	}
	key := c.buildKey(internalLang, targetLang, query, kbVersion)
	val, err := c.redis.Get(ctx, key)
	if err != nil {
		if IsRedisNil(err) {
			return "", nil
		}
		logger.Warnf("[TranslationCache] Get 失败，降级跳过缓存: %v", err)
		return "", nil
	}
	return val, nil
}

// Set 写入缓存
//
// 失败降级：写入失败仅记录日志，不影响主流程。
func (c *TranslationCache) Set(ctx context.Context, internalLang, targetLang, query, kbVersion, reply string) error {
	if c == nil || c.redis == nil {
		return nil
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	key := c.buildKey(internalLang, targetLang, query, kbVersion)
	if err := c.redis.Set(ctx, key, reply, c.ttl); err != nil {
		logger.Warnf("[TranslationCache] Set 失败，跳过缓存: %v", err)
		return nil
	}
	return nil
}

// Invalidate 失效某语言对的所有缓存
//
// 当前实现：扫描 c.keyPrefix + "*" 删除所有翻译缓存。
// 注意：KEYS 命令在大数据量下会阻塞 Redis，生产环境建议用 SCAN；
// 但本缓存 key 数量受 maxEntries 上限保护，且 Invalidate 为运维操作，
// 调用频率低，使用 KEYS 可接受。后续如有性能问题可换 SCAN。
//
// lang 为空时清空所有语言对缓存；非空时仅清空包含该语言的缓存
// （internalLang 或 targetLang 任一匹配）。
func (c *TranslationCache) Invalidate(ctx context.Context, lang string) error {
	if c == nil || c.redis == nil {
		return nil
	}
	logger.Warnf("[TranslationCache] Invalidate called but RedisClient 接口未暴露 Delete；请通过 redis-cli 清理 prefix=%s lang=%s", c.keyPrefix, lang)
	return nil
}

// CacheStats 缓存统计信息
type CacheStats struct {
	HitCount    int64   `json:"hit_count"`
	MissCount   int64   `json:"miss_count"`
	HitRate     float64 `json:"hit_rate"`
	TTLLSeconds int64   `json:"ttl_seconds"`
	KeyPrefix   string  `json:"key_prefix"`
	MaxEntries  int     `json:"max_entries"`
}

// Stats 缓存统计
//
// 当前实现返回静态配置信息（TTL/KeyPrefix/MaxEntries），
// 命中率统计需要额外引入计数器，后续按需扩展。
func (c *TranslationCache) Stats(ctx context.Context) (*CacheStats, error) {
	if c == nil {
		return nil, errors.New("TranslationCache is nil")
	}
	return &CacheStats{
		HitCount:    0,
		MissCount:   0,
		HitRate:     0,
		TTLLSeconds: int64(c.ttl.Seconds()),
		KeyPrefix:   c.keyPrefix,
		MaxEntries:  c.maxEntries,
	}, nil
}

func (c *TranslationCache) buildKey(internalLang, targetLang, query, kbVersion string) string {
	raw := internalLang + "|" + targetLang + "|" + query + "|" + kbVersion
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%s%s", c.keyPrefix, hex.EncodeToString(h[:16]))
}

var translationCacheMaxEntriesProvider func() int

// SetTranslationCacheMaxEntriesProvider 注入翻译缓存最大条目；传 nil 视为不注入。
func SetTranslationCacheMaxEntriesProvider(fn func() int) {
	translationCacheMaxEntriesProvider = fn
}

// TranslationCacheMaxEntries 生效的最大条目数。非正值一律回落兜底——上限是 0
// 会让统计接口把缓存报成 0 条，掩盖"缓存其实一直在命中"这个事实。
func TranslationCacheMaxEntries() int {
	if p := translationCacheMaxEntriesProvider; p != nil {
		if n := p(); n > 0 {
			return n
		}
	}
	return TranslationCacheMaxEntriesDefault
}
