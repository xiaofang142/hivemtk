// Package-level 说明：内存后端与 Redis 后端必须共用同一套键分域语义
// （tenant.RedisKey），否则同一个进程在 Redis 不可用时退回内存缓存，
// 租户隔离会静默失效。
package cache

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// ErrCacheMiss key 不存在或已过期时返回（两后端统一哨兵：RedisCache 的 Get/GetJSON/LPop
// 已在实现内把 redis.Nil 转换为本哨兵，调用方判未命中只看 errors.Is(err, ErrCacheMiss) 一支）
var ErrCacheMiss = errors.New("cache: key not found")

// DefaultMaxKeys 默认 LRU 上限（修复：限制内存使用）
const DefaultMaxKeys = 10_000

var maxKeysProvider = func() int { return DefaultMaxKeys }

// MaxKeys 返回当前 LRU 上限（DB 驱动优先，fallback 为 DefaultMaxKeys）。
// 非正值一律回落兜底——上限 0 会让缓存一条都留不下，每次读都穿透到源头，
// 而此时管理台上的「缓存命中率」看起来仍然正常，故障会一路藏到源头上。
func MaxKeys() int {
	if n := maxKeysProvider(); n > 0 {
		return n
	}
	return DefaultMaxKeys
}

// SetMaxKeysProvider 上层注入函数（ConfigParam 初始化后调用）
func SetMaxKeysProvider(fn func() int) {
	if fn != nil {
		maxKeysProvider = fn
	}
}

var (
	cacheRegistryMu sync.Mutex
	cacheRegistry   = make(map[*MemoryCache]struct{})
)

func registerCache(m *MemoryCache) {
	cacheRegistryMu.Lock()
	cacheRegistry[m] = struct{}{}
	cacheRegistryMu.Unlock()
}

func unregisterCache(m *MemoryCache) {
	cacheRegistryMu.Lock()
	delete(cacheRegistry, m)
	cacheRegistryMu.Unlock()
}

// ShutdownAll 关闭所有注册表中未关闭的 MemoryCache（用于测试进程退出清理 goroutine leak）
func ShutdownAll() {
	cacheRegistryMu.Lock()
	caches := make([]*MemoryCache, 0, len(cacheRegistry))
	for m := range cacheRegistry {
		caches = append(caches, m)
	}
	cacheRegistryMu.Unlock()
	for _, m := range caches {
		m.Close()
	}
}

// MemoryCache 内存缓存实现（带 LRU 上限）
type MemoryCache struct {
	data      map[string]*list.Element
	order     *list.List
	mu        sync.RWMutex
	done      chan struct{}
	closeOnce sync.Once
	maxKeys   int
}

type cacheItem struct {
	key        string
	value      any
	expiration time.Time
	listMode   bool
	listItems  []string
}

// NewMemoryCache 创建内存缓存（LRU 上限由 ConfigParam 动态驱动，fallback 10000）
func NewMemoryCache() *MemoryCache {
	return NewMemoryCacheWithLimit(MaxKeys())
}

// NewMemoryCacheWithLimit 创建带 LRU 上限的内存缓存
func NewMemoryCacheWithLimit(maxKeys int) *MemoryCache {
	if maxKeys <= 0 {
		maxKeys = MaxKeys()
	}
	cache := &MemoryCache{
		data:    make(map[string]*list.Element),
		order:   list.New(),
		done:    make(chan struct{}),
		maxKeys: maxKeys,
	}
	registerCache(cache)
	go cache.cleanup()
	return cache
}

// Close 关闭缓存清理 goroutine，幂等且并发安全
func (m *MemoryCache) Close() {
	m.closeOnce.Do(func() {
		unregisterCache(m)
		close(m.done)
	})
}

func (m *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.deleteExpired()
		case <-m.done:
			return
		}
	}
}

func (m *MemoryCache) deleteExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for ele := m.order.Back(); ele != nil; {
		prev := ele.Prev()
		item, ok := ele.Value.(*cacheItem)
		if !ok {
			ele = prev
			continue
		}
		if !item.expiration.IsZero() && item.expiration.Before(now) {
			m.order.Remove(ele)
			delete(m.data, item.key)
		}
		ele = prev
	}
}

func (m *MemoryCache) touch(ele *list.Element) {
	m.order.MoveToFront(ele)
}

func (m *MemoryCache) evictIfNeeded() {
	for m.order.Len() > m.maxKeys {
		back := m.order.Back()
		if back == nil {
			return
		}
		item := back.Value.(*cacheItem)
		m.order.Remove(back)
		delete(m.data, item.key)
	}
}

func (m *MemoryCache) peekItem(key string) (*cacheItem, bool) {
	m.mu.RLock()
	ele, ok := m.data[key]
	if !ok {
		m.mu.RUnlock()
		return nil, false
	}
	item := ele.Value.(*cacheItem)
	m.mu.RUnlock()
	m.mu.Lock()
	if ele2, ok2 := m.data[key]; ok2 && ele2 == ele {
		m.touch(ele)
		m.mu.Unlock()
		return item, true
	}
	m.mu.Unlock()
	return nil, false
}

// Get 获取缓存
// 未命中或已过期返回 ErrCacheMiss（两后端统一哨兵，见 RedisCache.Get 的转换）
func (m *MemoryCache) Get(ctx context.Context, key string) (string, error) {
	key = tenant.RedisKey(ctx, key)
	item, ok := m.peekItem(key)
	if !ok || (!item.expiration.IsZero() && item.expiration.Before(time.Now())) {
		return "", ErrCacheMiss
	}
	switch v := item.value.(type) {
	case string:
		return v, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// Set 设置缓存
func (m *MemoryCache) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()

	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}

	if ele, ok := m.data[key]; ok {
		ele.Value = &cacheItem{key: key, value: value, expiration: exp}
		m.touch(ele)
		return nil
	}
	ele := m.order.PushFront(&cacheItem{key: key, value: value, expiration: exp})
	m.data[key] = ele
	m.evictIfNeeded()
	return nil
}

// SetNX 仅在 key 不存在时设置；返回 true 表示本次设置成功
func (m *MemoryCache) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()

	if ele, exists := m.data[key]; exists {
		item := ele.Value.(*cacheItem)
		if item.expiration.IsZero() || item.expiration.After(time.Now()) {
			return false, nil
		}
	}

	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	ele := m.order.PushFront(&cacheItem{key: key, value: value, expiration: exp})
	m.data[key] = ele
	m.evictIfNeeded()
	return true, nil
}

// ReleaseLock 仅当 key 的值等于 token 时删除（安全释放 SetNX 锁）。返回 true 表示成功释放。
func (m *MemoryCache) ReleaseLock(ctx context.Context, key, token string) (bool, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	ele, ok := m.data[key]
	if !ok {
		return false, nil
	}
	item := ele.Value.(*cacheItem)
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		return false, nil
	}
	if s, ok := item.value.(string); ok && s == token {
		m.order.Remove(ele)
		delete(m.data, key)
		return true, nil
	}
	return false, nil
}

// LPush 向列表头部推入
// RefreshLock 仅在锁未过期且 token 匹配时延长 TTL。
func (m *MemoryCache) RefreshLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("锁续期 TTL 必须大于 0")
	}
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	ele, ok := m.data[key]
	if !ok {
		return false, nil
	}
	item := ele.Value.(*cacheItem)
	now := time.Now()
	if !item.expiration.IsZero() && !item.expiration.After(now) {
		return false, nil
	}
	if s, ok := item.value.(string); !ok || s != token {
		return false, nil
	}
	item.expiration = now.Add(ttl)
	m.touch(ele)
	return true, nil
}

func (m *MemoryCache) LPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()

	s := stringifyValue(value)
	var item *cacheItem
	if ele, ok := m.data[key]; ok {
		item = ele.Value.(*cacheItem)
		// 已过期的列表键对 Redis 而言等价于"不存在"：本次推入必须把键复活成
		// 新列表（旧元素作废），而不是往一条僵尸列表里追加后仍不可见。
		if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
			item.listMode = false
			item.listItems = nil
			item.expiration = time.Time{}
		}
		m.touch(ele)
	}
	if item == nil || !item.listMode {
		item = &cacheItem{key: key, listMode: true, listItems: []string{s}}
		ele := m.order.PushFront(item)
		m.data[key] = ele
	} else {
		item.listItems = append([]string{s}, item.listItems...)
	}
	if expiration > 0 {
		item.expiration = time.Now().Add(expiration)
	}
	m.evictIfNeeded()
	return nil
}

// RPush 向列表尾部推入
func (m *MemoryCache) RPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()

	s := stringifyValue(value)
	var item *cacheItem
	if ele, ok := m.data[key]; ok {
		item = ele.Value.(*cacheItem)
		if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
			item.listMode = false
			item.listItems = nil
			item.expiration = time.Time{}
		}
		m.touch(ele)
	}
	if item == nil || !item.listMode {
		item = &cacheItem{key: key, listMode: true, listItems: []string{s}}
		ele := m.order.PushFront(item)
		m.data[key] = ele
	} else {
		item.listItems = append(item.listItems, s)
	}
	if expiration > 0 {
		item.expiration = time.Now().Add(expiration)
	}
	m.evictIfNeeded()
	return nil
}

// LPop 从列表头部弹出
//
// E-6b 契约对齐（2026-10-23）：未命中 / 空列表返回 ErrCacheMiss（哨兵与 Get 统一），
// 不再返回 ("", nil)——RedisCache 腿 LPop 空时返回 redis.Nil，降级到 Memory 时
// 调用方（inbox_ingress / human_escalation 等）靠错误哨兵区分"没有"与"空串"，
// 两腿语义必须一致（规划 §4.6b：以 Redis 为准）。
func (m *MemoryCache) LPop(ctx context.Context, key string) (string, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()

	ele, ok := m.data[key]
	if !ok {
		return "", ErrCacheMiss
	}
	item := ele.Value.(*cacheItem)
	// 过期列表对 Redis 是"键不存在"：必须与未命中同哨兵，不能把死值弹给调用方。
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		m.order.Remove(ele)
		delete(m.data, key)
		return "", ErrCacheMiss
	}
	if !item.listMode || len(item.listItems) == 0 {
		return "", ErrCacheMiss
	}
	v := item.listItems[0]
	item.listItems = item.listItems[1:]
	if len(item.listItems) == 0 {
		m.order.Remove(ele)
		delete(m.data, key)
	} else {
		m.touch(ele)
	}
	return v, nil
}

// LRange 获取列表区间
// PopAll 原子取出并删除整个 list（写锁内交换，无丢消息窗口）。
func (m *MemoryCache) PopAll(ctx context.Context, key string) ([]string, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	ele, ok := m.data[key]
	if !ok {
		return []string{}, nil
	}
	item := ele.Value.(*cacheItem)
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		m.order.Remove(ele)
		delete(m.data, key)
		return []string{}, nil
	}
	if !item.listMode || len(item.listItems) == 0 {
		m.order.Remove(ele)
		delete(m.data, key)
		return []string{}, nil
	}
	items := item.listItems
	m.order.Remove(ele)
	delete(m.data, key)
	return items, nil
}

func (m *MemoryCache) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.RLock()
	defer m.mu.RUnlock()

	ele, ok := m.data[key]
	if !ok {
		return []string{}, nil
	}
	item := ele.Value.(*cacheItem)
	// 过期列表对 Redis 是"键不存在"：返回空区间（与 LPop 同一判据）。
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		return []string{}, nil
	}
	if !item.listMode || len(item.listItems) == 0 {
		return []string{}, nil
	}
	items := item.listItems
	n := int64(len(items))
	if start < 0 {
		start += n
	}
	if stop < 0 {
		stop += n
	}
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || start >= n {
		return []string{}, nil
	}
	return append([]string{}, items[start:stop+1]...), nil
}

// LLen 列表长度
func (m *MemoryCache) LLen(ctx context.Context, key string) (int64, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.RLock()
	defer m.mu.RUnlock()

	ele, ok := m.data[key]
	if !ok {
		return 0, nil
	}
	item := ele.Value.(*cacheItem)
	// 过期列表对 Redis 是"键不存在"：长度报 0（与 LPop/LRange 同一判据）。
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		return 0, nil
	}
	if !item.listMode {
		return 0, nil
	}
	return int64(len(item.listItems)), nil
}

func stringifyValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// Delete 删除缓存
func (m *MemoryCache) Delete(ctx context.Context, key string) error {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	if ele, ok := m.data[key]; ok {
		m.order.Remove(ele)
		delete(m.data, key)
	}
	return nil
}

// Incr 原子自增并返回新值（固定窗口计数）。
// 首次设置 value=1；expiration>0 时作为 TTL 应用（用于单实例下的 RPM/计数兜底）。
// 注：调用方应以「按时间窗口轮转的 key」（如带分钟时间戳）驱动窗口重置，TTL 仅用于清理旧 key。
func (m *MemoryCache) Incr(ctx context.Context, key string, expiration time.Duration) (int64, error) {
	key = tenant.RedisKey(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	if ele, ok := m.data[key]; ok {
		item := ele.Value.(*cacheItem)
		if item.expiration.IsZero() || item.expiration.After(time.Now()) {
			if n, ok := item.value.(int64); ok {
				n++
				item.value = n
				// E-6b 契约对齐（2026-10-23）：TTL 只在首次自增（n==1）设置，
				// 不再每次往后推——与 RedisCache 的 Expire-on-first 语义一致；
				// 旧实现下限流/去重键在高频写入时永不过期（规划 §4.6b 表第 3 行）。
				if expiration > 0 && n == 1 {
					item.expiration = exp
				}
				m.touch(ele)
				return n, nil
			}
		}
	}
	item := &cacheItem{key: key, value: int64(1), expiration: exp}
	ele := m.order.PushFront(item)
	m.data[key] = ele
	m.evictIfNeeded()
	return 1, nil
}

// Exists 检查缓存是否存在
func (m *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	key = tenant.RedisKey(ctx, key)
	item, ok := m.peekItem(key)
	if !ok {
		return false, nil
	}
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		return false, nil
	}
	return true, nil
}

// GetJSON 获取 JSON 缓存并反序列化
// 优化：SetJSON 写入时已序列化缓存，命中时直接反序列化，跳过 Marshal
func (m *MemoryCache) GetJSON(ctx context.Context, key string, dest any) error {
	key = tenant.RedisKey(ctx, key)
	item, ok := m.peekItem(key)
	if !ok {
		return ErrCacheMiss
	}
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		return ErrCacheMiss
	}
	switch v := item.value.(type) {
	case string:
		return json.Unmarshal([]byte(v), dest)
	case []byte:
		return json.Unmarshal(v, dest)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, dest)
	}
}

// SetJSON 设置 JSON 缓存
// 优化：写入时一次性序列化，避免 GetJSON 每次 Marshal 浪费 CPU
func (m *MemoryCache) SetJSON(ctx context.Context, key string, value any, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return m.Set(ctx, key, data, expiration)
}

// Clear 清空缓存
func (m *MemoryCache) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]*list.Element)
	m.order = list.New()
	return nil
}
