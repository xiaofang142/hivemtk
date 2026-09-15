package llm

import (
	"context"

	"fmt"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/cache"

	"hash/fnv"
	"hivemtk-user/internal/pkg/utils/logger"
)

type rpmBucket struct {
	mu      sync.Mutex
	count   int
	resetAt time.Time
}

func (d *Dispatcher) getCache(ctx context.Context, key string) (string, bool) {
	if key == "" {
		return "", false
	}

	raw, err := cache.GetGlobalCache().Get(ctx, key)
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

func (d *Dispatcher) setCache(ctx context.Context, key string, ttl int, content string) {
	if ttl <= 0 || key == "" {
		return
	}
	// 空内容（截断/模型异常）不入缓存：否则同 prompt 在 TTL 内持续命中空回复
	if strings.TrimSpace(content) == "" {
		return
	}

	_ = cache.GetGlobalCache().Set(ctx, key, content, time.Duration(ttl)*time.Second)
}

func (d *Dispatcher) allowRequest(providerName string, maxRPM int) bool {
	if maxRPM <= 0 {
		return true
	}
	if !cache.GlobalIsRedis() {
		return d.allowRequestLocal(providerName, maxRPM)
	}
	c := cache.GetGlobalCache()
	now := time.Now()
	windowStart := now.Truncate(time.Minute).Unix()
	key := fmt.Sprintf("mtk:llm:rpm:%s:%d", providerName, windowStart)
	cur, err := c.Incr(context.Background(), key, time.Minute)
	if err != nil {
		logger.Warnf("[LLM] RPM 计数后端异常，放行 provider=%s: %v", providerName, err)
		return true
	}

	if cur > int64(maxRPM) {
		logger.Warnf("[LLM] RPM 限流触发 provider=%s cur=%d max=%d", providerName, cur, maxRPM)
		return false
	}
	return true
}

func (d *Dispatcher) allowRequestLocal(providerName string, maxRPM int) bool {
	d.mu.Lock()
	bucket, ok := d.rpmCounter[providerName]
	if !ok {
		bucket = &rpmBucket{resetAt: time.Now().Add(time.Minute)}
		d.rpmCounter[providerName] = bucket
	}
	d.mu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	if now.After(bucket.resetAt) {
		bucket.count = 0
		bucket.resetAt = now.Add(time.Minute)
	}
	if bucket.count >= maxRPM {
		return false
	}
	bucket.count++
	return true
}

// CacheKey 生成缓存 key（仅覆盖 scenario + user prompt）。
//
// ⚠️ 新代码请优先用 CacheKeyWithSystem：知识库召回内容注入在 system message
// （service.renderRAGReferenceBlock），若 key 不覆盖 system prompt，
// 同一句客户消息在不同知识库上下文下会命中同一条缓存，把「按 A 知识库生成的回复」
// 当作「B 知识库上下文」的结果返回。本函数保留仅为兼容既有调用与用例。
func CacheKey(scenario DispatchScenario, prompt string) string {
	h := fnv.New64a()
	h.Write([]byte(string(scenario)))
	h.Write([]byte(strings.TrimSpace(prompt)))
	return fmt.Sprintf("llm:dispatch:%s:%x", scenario, h.Sum64())
}

// CacheKeyWithSystem 生成缓存 key（覆盖 scenario + system prompt + user prompt）。
//
// system prompt 承载人设与知识库召回内容，二者都实质影响模型输出，
// 因此必须参与 key 计算，否则缓存会跨知识上下文串味。
func CacheKeyWithSystem(scenario DispatchScenario, systemPrompt, prompt string) string {
	h := fnv.New64a()
	h.Write([]byte(string(scenario)))
	h.Write([]byte(strings.TrimSpace(systemPrompt)))
	// 分隔符：避免 ("ab","c") 与 ("a","bc") 拼接后哈希相同
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(prompt)))
	return fmt.Sprintf("llm:dispatch:%s:%x", scenario, h.Sum64())
}
