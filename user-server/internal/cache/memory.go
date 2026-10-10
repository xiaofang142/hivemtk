package cache

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ErrCacheMiss key 不存在或已过期时返回（对齐 RedisCache 未命中返回 redis.Nil 的契约）
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
		item := ele.Value.(*cacheItem)
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
// 未命中或已过期返回 ErrCacheMiss（与 RedisCache 未命中返回 redis.Nil 的契约对齐）
func (m *MemoryCache) Get(ctx context.Context, key string) (string, error) {
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
func (m *MemoryCache) LPush(ctx context.Context, key string, value any, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := stringifyValue(value)
	var item *cacheItem
	if ele, ok := m.data[key]; ok {
		item = ele.Value.(*cacheItem)
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
	m.mu.Lock()
	defer m.mu.Unlock()

	s := stringifyValue(value)
	var item *cacheItem
	if ele, ok := m.data[key]; ok {
		item = ele.Value.(*cacheItem)
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
func (m *MemoryCache) LPop(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ele, ok := m.data[key]
	if !ok {
		return "", nil
	}
	item := ele.Value.(*cacheItem)
	if !item.listMode || len(item.listItems) == 0 {
		return "", nil
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
	m.mu.Lock()
	defer m.mu.Unlock()
	ele, ok := m.data[key]
	if !ok {
		return []string{}, nil
	}
	item := ele.Value.(*cacheItem)
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
	m.mu.RLock()
	defer m.mu.RUnlock()

	ele, ok := m.data[key]
	if !ok {
		return []string{}, nil
	}
	item := ele.Value.(*cacheItem)
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
	m.mu.RLock()
	defer m.mu.RUnlock()

	ele, ok := m.data[key]
	if !ok {
		return 0, nil
	}
	item := ele.Value.(*cacheItem)
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
				if expiration > 0 {
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
