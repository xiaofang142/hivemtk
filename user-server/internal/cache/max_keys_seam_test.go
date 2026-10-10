package cache

import "testing"

// TestMaxKeysSeam 阶段一 cache 组接线：逐格钉死「未注入==代码兜底 / 注入生效 /
// 非法值回落 / setter 传 nil 等于撤销注入」。
//
// 最后一格最容易被忽略：cache.SetMaxKeysProvider 本来就有 `if fn != nil` 的守卫，
// 但如果哪天有人为了统一风格改成"传 nil 就注入一个空函数"，MaxKeys() 会永久返回 0，
// 缓存一条都留不下——而命中率面板看起来仍然正常。
func TestMaxKeysSeam(t *testing.T) {
	t.Cleanup(func() { SetMaxKeysProvider(nil) })
	SetMaxKeysProvider(nil)

	if got := MaxKeys(); got != DefaultMaxKeys {
		t.Fatalf("未注入时 = %d，期望代码兜底 %d", got, DefaultMaxKeys)
	}
	SetMaxKeysProvider(func() int { return 4242 })
	if got := MaxKeys(); got != 4242 {
		t.Errorf("注入后 = %d，期望 4242", got)
	}
	SetMaxKeysProvider(func() int { return 0 })
	if got := MaxKeys(); got != DefaultMaxKeys {
		t.Errorf("注入 0 后 = %d，期望回落 %d（上限 0 会让缓存一条都留不下）", got, DefaultMaxKeys)
	}
	SetMaxKeysProvider(func() int { return -1 })
	if got := MaxKeys(); got != DefaultMaxKeys {
		t.Errorf("注入 -1 后 = %d，期望回落 %d", got, DefaultMaxKeys)
	}
	SetMaxKeysProvider(nil)
	if got := MaxKeys(); got != DefaultMaxKeys {
		t.Errorf("传 nil 撤销注入后 = %d，期望回到兜底 %d", got, DefaultMaxKeys)
	}
}
