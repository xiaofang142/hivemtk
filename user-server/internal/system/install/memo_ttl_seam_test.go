package install

import (
	"testing"
	"time"
)

// TestMemoTTLSeam 阶段一 cache 组接线：memo 有效期非法值必须回落兜底。
// 非正值让 memo 永不过期，首次探测出的 admin 用户名会被一直缓存住，
// 事后往库里换超管也探测不出来。
func TestMemoTTLSeam(t *testing.T) {
	t.Cleanup(func() { SetMemoTTLProvider(nil) })
	SetMemoTTLProvider(nil)

	if got := MemoTTL(); got != DefaultMemoTTL {
		t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, DefaultMemoTTL)
	}
	SetMemoTTLProvider(func() time.Duration { return 45 * time.Second })
	if got := MemoTTL(); got != 45*time.Second {
		t.Errorf("注入后 = %v，期望 45s", got)
	}
	for _, bad := range []time.Duration{0, -time.Second} {
		SetMemoTTLProvider(func() time.Duration { return bad })
		if got := MemoTTL(); got != DefaultMemoTTL {
			t.Errorf("注入 %v 后 = %v，期望回落 %v", bad, got, DefaultMemoTTL)
		}
	}
	SetMemoTTLProvider(nil)
	if got := MemoTTL(); got != DefaultMemoTTL {
		t.Errorf("传 nil 撤销注入后 = %v，期望回到兜底 %v", got, DefaultMemoTTL)
	}
}
