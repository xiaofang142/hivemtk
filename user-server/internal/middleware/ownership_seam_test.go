package middleware

import (
	"testing"
	"time"
)

// TestOwnershipCacheTTLSeam 归属缓存 TTL 的接线三格。
func TestOwnershipCacheTTLSeam(t *testing.T) {
	restore := ownershipCacheTTLProvider
	t.Cleanup(func() { ownershipCacheTTLProvider = restore })

	if got, want := defaultOwnershipCacheTTL, 5*time.Second; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.ownership_cache_ttl 的 DefaultValue（5 秒 = %v）不一致", got, want)
	}

	SetOwnershipCacheTTLProvider(nil)
	if got := ownershipCacheTTL(); got != defaultOwnershipCacheTTL {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, defaultOwnershipCacheTTL)
	}

	SetOwnershipCacheTTLProvider(func() time.Duration { return 90 * time.Second })
	if got := ownershipCacheTTL(); got != 90*time.Second {
		t.Fatalf("注入后 = %v，期望 90s", got)
	}
	SetOwnershipCacheTTLProvider(func() time.Duration { return -time.Second })
	if got := ownershipCacheTTL(); got != defaultOwnershipCacheTTL {
		t.Fatalf("注入负数时 = %v，期望回落 %v", got, defaultOwnershipCacheTTL)
	}
}
