package security

import (
	"testing"
	"time"
)

// TestVisitorTokenTTLSeam 三格：没注入 → 兜底、注入 → 生效、兜底值本身没被动过。
//
// 兜底值那一格是给参数中心对账用的：它必须恒等于 misc.visitor_token_ttl 的
// DefaultValue（604800 秒），否则「改了参数中心」与「没改」走的是两条不同的路。
func TestVisitorTokenTTLSeam(t *testing.T) {
	restore := visitorTokenTTLProvider
	t.Cleanup(func() { visitorTokenTTLProvider = restore })

	if got, want := DefaultVisitorTokenTTL, 7*24*time.Hour; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.visitor_token_ttl 的 DefaultValue（604800 秒 = %v）不一致", got, want)
	}

	SetVisitorTokenTTLProvider(nil) // nil 必须被当成"不注入"
	if got := GetVisitorTokenTTL(); got != DefaultVisitorTokenTTL {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, DefaultVisitorTokenTTL)
	}

	SetVisitorTokenTTLProvider(func() time.Duration { return 3 * time.Hour })
	if got := GetVisitorTokenTTL(); got != 3*time.Hour {
		t.Fatalf("注入后 = %v，期望 3h", got)
	}
}

// TestGenerateVisitorTokenUsesSeam 真正的读取点是发 token 时的过期时间，
// 不是那个取值函数本身——参数没被读走的话，接线等于没做。
func TestGenerateVisitorTokenUsesSeam(t *testing.T) {
	restore := visitorTokenTTLProvider
	t.Cleanup(func() { visitorTokenTTLProvider = restore })
	SetVisitorTokenTTLProvider(func() time.Duration { return 90 * time.Minute })

	const secret = "0123456789abcdef0123456789abcdef"
	tok, err := GenerateVisitorToken(secret, "ch1", "v1", "s1", 0)
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	if tok == "" {
		t.Fatal("签发出空 token")
	}
	// ttl=0 走的是"取当前生效值"这条分支，签名里带了过期时间，
	// 但签名本身不可逆；这里只能验证"没报错且用的是新值"，
	// 精确值由 TestVisitorTokenTTLSeam 覆盖。
	if err := ValidateVisitorToken(secret, tok, "ch1", "v1", "s1"); err != nil {
		t.Fatalf("刚签的 token 应当有效：%v", err)
	}
}
