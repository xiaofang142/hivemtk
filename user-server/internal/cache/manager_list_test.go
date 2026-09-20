package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCache_SetNXAndReleaseLock(t *testing.T) {
	m := NewMemoryCacheWithLimit(100)
	defer m.Close()
	ctx := context.Background()

	ok, err := m.SetNX(ctx, "lock1", "tok-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("首次 SetNX 应成功: %v %v", ok, err)
	}
	ok, err = m.SetNX(ctx, "lock1", "tok-b", time.Minute)
	if err != nil || ok {
		t.Fatalf("已持有者之外 SetNX 应失败: %v %v", ok, err)
	}
	// 值不符不得释放
	if released, _ := m.ReleaseLock(ctx, "lock1", "wrong"); released {
		t.Fatal("token 不符不应释放")
	}
	// 过期条目视作不存在，SetNX 可抢占
	if err := m.Set(ctx, "stale", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	expireKey(t, m, "stale")
	if ok, _ := m.SetNX(ctx, "stale", "new", time.Minute); !ok {
		t.Fatal("过期条目应可被 SetNX 抢占")
	}
	// 过期锁不可释放
	if released, _ := m.ReleaseLock(ctx, "stale-exp", "x"); released {
		t.Fatal("不存在的锁不应释放成功")
	}
	if err := m.Set(ctx, "lock2", "tok-c", time.Minute); err != nil {
		t.Fatal(err)
	}
	expireKey(t, m, "lock2")
	if released, _ := m.ReleaseLock(ctx, "lock2", "tok-c"); released {
		t.Fatal("已过期锁不应释放成功")
	}
	if released, _ := m.ReleaseLock(ctx, "lock1", "tok-a"); !released {
		t.Fatal("token 相符应释放成功")
	}
	if _, exists := m.peekItem("lock1"); exists {
		t.Fatal("释放后 key 应消失")
	}
}

func TestMemoryCache_ListOps(t *testing.T) {
	m := NewMemoryCacheWithLimit(100)
	defer m.Close()
	ctx := context.Background()

	if err := m.RPush(ctx, "q", "a", 0); err != nil {
		t.Fatal(err)
	}
	if err := m.RPush(ctx, "q", []byte("b"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := m.LPush(ctx, "q", map[string]int{"j": 1}, 0); err != nil {
		t.Fatal(err)
	}
	// LPush 头插 JSON、RPush 尾推 → [json, a, b]
	items, err := m.LRange(ctx, "q", 0, -1)
	if err != nil || len(items) != 3 || items[0] != `{"j":1}` || items[1] != "a" || items[2] != "b" {
		t.Fatalf("LRange 顺序错误: %v %v", items, err)
	}
	if n, _ := m.LLen(ctx, "q"); n != 3 {
		t.Fatalf("LLen=%d", n)
	}
	// 负区间
	if items, _ := m.LRange(ctx, "q", -2, -1); len(items) != 2 || items[0] != "a" {
		t.Fatalf("负区间错误: %v", items)
	}
	if items, _ := m.LRange(ctx, "q", 5, 9); len(items) != 0 {
		t.Fatalf("越界区间应为空: %v", items)
	}
	if items, _ := m.LRange(ctx, "q", 2, 1); len(items) != 0 {
		t.Fatalf("start>stop 应为空: %v", items)
	}
	if items, _ := m.LRange(ctx, "missing", 0, 5); len(items) != 0 {
		t.Fatal("missing key 应为空")
	}
	if n, _ := m.LLen(ctx, "missing"); n != 0 {
		t.Fatal("missing LLen 应为 0")
	}
	// 非 list 键
	if err := m.Set(ctx, "scalar", "v", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LRange(ctx, "scalar", 0, 1); err != nil {
		t.Fatalf("非 list LRange 应空结果不报错: %v", err)
	}
	if n, _ := m.LLen(ctx, "scalar"); n != 0 {
		t.Fatal("非 list LLen 应为 0")
	}

	// LPop 头弹至清空后键消失
	v, err := m.LPop(ctx, "q")
	if err != nil || v != `{"j":1}` {
		t.Fatalf("LPop 首元素错误: %q %v", v, err)
	}
	if _, err := m.LPop(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "q2", "notalist", 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.LPop(ctx, "q2"); v != "" {
		t.Fatal("非 list LPop 应返回空串")
	}
	if _, err := m.LPop(ctx, "q"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LPop(ctx, "q"); err != nil {
		t.Fatal(err)
	}
	if _, exists := m.peekItem("q"); exists {
		t.Fatal("list 弹空后键应消失")
	}
}

func TestMemoryCache_PopAllAndIncrAndEvict(t *testing.T) {
	m := NewMemoryCacheWithLimit(2)
	defer m.Close()
	ctx := context.Background()

	if err := m.RPush(ctx, "pipe", "1", 0); err != nil {
		t.Fatal(err)
	}
	if err := m.RPush(ctx, "pipe", "2", 0); err != nil {
		t.Fatal(err)
	}
	all, err := m.PopAll(ctx, "pipe")
	if err != nil || len(all) != 2 {
		t.Fatalf("PopAll 错误: %v %v", all, err)
	}
	if _, exists := m.peekItem("pipe"); exists {
		t.Fatal("PopAll 后键应消失")
	}
	if got, _ := m.PopAll(ctx, "pipe"); len(got) != 0 {
		t.Fatal("再次 PopAll 应为空")
	}
	if err := m.Set(ctx, "raw", "v", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.PopAll(ctx, "raw"); len(got) != 0 {
		t.Fatal("非 list PopAll 应为空并删键")
	}

	// Incr：首 1 后 2；非 int64 值重建为 1
	if n, err := m.Incr(ctx, "cnt", time.Minute); err != nil || n != 1 {
		t.Fatalf("Incr 首次: %d %v", n, err)
	}
	if n, _ := m.Incr(ctx, "cnt", 0); n != 2 {
		t.Fatalf("Incr 二次: %d", n)
	}
	if err := m.Set(ctx, "cnt", "notnum", 0); err != nil {
		t.Fatal(err)
	}
	if n, _ := m.Incr(ctx, "cnt", 0); n != 1 {
		t.Fatalf("非计数值 Incr 应重置为 1，得 %d", n)
	}
	// 过期计数视作可重置
	if err := m.Set(ctx, "stale-cnt", int64(9), time.Minute); err != nil {
		t.Fatal(err)
	}
	expireKey(t, m, "stale-cnt")
	if n, _ := m.Incr(ctx, "stale-cnt", time.Minute); n != 1 {
		t.Fatalf("过期计数应重置为 1，得 %d", n)
	}

	// LRU 上限=2：写 3 个键最旧被淘汰（maxKeys 生效路径）
	if err := m.Set(ctx, "e1", "1", 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "e2", "2", 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "e3", "3", 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Get(ctx, "e1"); v != "" {
		t.Fatal("e1 应被淘汰")
	}
	if v, _ := m.Get(ctx, "e3"); v != "3" {
		t.Fatalf("e3 应存活，得 %q", v)
	}
}

// expireKey 直改内部过期时间为过去（Set 的负 duration 语义是"永不过期"，无法造过期条目）
func expireKey(t *testing.T, m *MemoryCache, key string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	ele, ok := m.data[key]
	if !ok {
		t.Fatalf("expireKey: %s 不存在", key)
	}
	ele.Value.(*cacheItem).expiration = time.Now().Add(-time.Hour)
}

func TestSetMaxKeysProvider(t *testing.T) {
	orig := maxKeysProvider
	defer func() { maxKeysProvider = orig }()

	if MaxKeys() != DefaultMaxKeys {
		t.Fatalf("默认 MaxKeys=%d", MaxKeys())
	}
	SetMaxKeysProvider(func() int { return 42 })
	if MaxKeys() != 42 {
		t.Fatalf("注入后应为 42，得 %d", MaxKeys())
	}
	SetMaxKeysProvider(nil) // nil 应被忽略
	if MaxKeys() != 42 {
		t.Fatalf("nil 注入不应改变，得 %d", MaxKeys())
	}
	// limit<=0 回退到 MaxKeys()
	m := NewMemoryCacheWithLimit(0)
	defer m.Close()
	if m.maxKeys != 42 {
		t.Fatalf("NewMemoryCacheWithLimit(0) 应回退 42，得 %d", m.maxKeys)
	}
}

func TestCacheManager_ListAndLockDelegation(t *testing.T) {
	mgr, err := NewCacheManager(CacheConfig{Type: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if ok, _ := mgr.SetNX(ctx, "k", "tok", 0); !ok { // exp=0 走默认 TTL
		t.Fatal("manager.SetNX 应成功")
	}
	if ok, _ := mgr.SetNX(ctx, "k", "tok2", 0); ok {
		t.Fatal("manager.SetNX 二次应失败")
	}
	if err := mgr.LPush(ctx, "l", "x", 0); err != nil { // exp=0 默认 TTL
		t.Fatal(err)
	}
	if err := mgr.RPush(ctx, "l", "y", time.Minute); err != nil {
		t.Fatal(err)
	}
	if n, _ := mgr.LLen(ctx, "l"); n != 2 {
		t.Fatalf("manager.LLen=%d", n)
	}
	if items, _ := mgr.LRange(ctx, "l", 0, 100); len(items) != 2 || items[0] != "x" {
		t.Fatalf("manager.LRange: %v", items)
	}
	if v, _ := mgr.LPop(ctx, "l"); v != "x" {
		t.Fatalf("manager.LPop=%q", v)
	}
	if err := mgr.RPush(ctx, "l2", "z", time.Minute); err != nil {
		t.Fatal(err)
	}
	if all, _ := mgr.PopAll(ctx, "l2"); len(all) != 1 {
		t.Fatalf("manager.PopAll: %v", all)
	}
	if ok, _ := mgr.ReleaseLock(ctx, "k", "tok"); !ok {
		t.Fatal("manager.ReleaseLock 应成功")
	}
	if ok, _ := mgr.ReleaseLock(ctx, "k", "tok"); ok {
		t.Fatal("重复释放应失败")
	}
	// Close 对 memory 后端应为 nil
	if err := mgr.Close(); err != nil {
		t.Fatalf("memory 后端 Close: %v", err)
	}
}
