package ragretrieval

import (
	"testing"
	"time"
)

// TestTranslationCacheMaxEntriesSeam 阶段一 cache 组接线。
// 非正值会让统计接口把缓存报成 0 条，掩盖"缓存其实一直在命中"这个事实，
// 所以和其余几个 seam 一样，非法值一律回落兜底而不是照单全收。
func TestTranslationCacheMaxEntriesSeam(t *testing.T) {
	t.Cleanup(func() { SetTranslationCacheMaxEntriesProvider(nil) })
	SetTranslationCacheMaxEntriesProvider(nil)

	if got := TranslationCacheMaxEntries(); got != TranslationCacheMaxEntriesDefault {
		t.Fatalf("未注入时 = %d，期望代码兜底 %d", got, TranslationCacheMaxEntriesDefault)
	}
	SetTranslationCacheMaxEntriesProvider(func() int { return 777 })
	if got := TranslationCacheMaxEntries(); got != 777 {
		t.Errorf("注入后 = %d，期望 777", got)
	}
	SetTranslationCacheMaxEntriesProvider(func() int { return 0 })
	if got := TranslationCacheMaxEntries(); got != TranslationCacheMaxEntriesDefault {
		t.Errorf("注入 0 后 = %d，期望回落 %d", got, TranslationCacheMaxEntriesDefault)
	}
	SetTranslationCacheMaxEntriesProvider(nil)
	if got := TranslationCacheMaxEntries(); got != TranslationCacheMaxEntriesDefault {
		t.Errorf("传 nil 撤销注入后 = %d，期望回到兜底 %d", got, TranslationCacheMaxEntriesDefault)
	}
}

// TestNewTranslationCachePicksUpMaxEntriesSeam 钉住构造器读的是 seam 而不是常量。
// 构造期只读一次的读取口最容易被重构绕回常量。
func TestNewTranslationCachePicksUpMaxEntriesSeam(t *testing.T) {
	t.Cleanup(func() { SetTranslationCacheMaxEntriesProvider(nil) })
	c := NewTranslationCache(nil, 0, "test")
	if c.maxEntries != TranslationCacheMaxEntriesDefault {
		t.Fatalf("未注入时构造出的 maxEntries = %d，期望 %d", c.maxEntries, TranslationCacheMaxEntriesDefault)
	}
	SetTranslationCacheMaxEntriesProvider(func() int { return 321 })
	c2 := NewTranslationCache(nil, 0, "test")
	if c2.maxEntries != 321 {
		t.Errorf("注入后构造出的 maxEntries = %d，期望 321", c2.maxEntries)
	}
}

var _ = time.Second

// TestTranslationCacheTTLStillDefaults 顺带钉住：显式传 TTL 时构造器不得覆盖它。
func TestTranslationCacheTTLStillDefaults(t *testing.T) {
	c := NewTranslationCache(nil, 7*time.Second, "test")
	if c.ttl != 7*time.Second {
		t.Errorf("显式 TTL 被覆盖：got %v，期望 7s", c.ttl)
	}
}
