package translation

import (
	"testing"
	"time"
)

// TestDeeplTimeoutSeam DeepL 超时的接线三格。
func TestDeeplTimeoutSeam(t *testing.T) {
	restore := deeplTimeoutProvider
	t.Cleanup(func() { deeplTimeoutProvider = restore })

	if got, want := deeplTimeout, 30*time.Second; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.deepl_timeout 的 DefaultValue（30 秒 = %v）不一致", got, want)
	}

	SetDeeplTimeoutProvider(nil)
	if got := NewDeepLTranslator("k", "").httpClient.Timeout; got != deeplTimeout {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, deeplTimeout)
	}

	SetDeeplTimeoutProvider(func() time.Duration { return 8 * time.Second })
	if got := NewDeepLTranslator("k", "").httpClient.Timeout; got != 8*time.Second {
		t.Fatalf("注入后 = %v，期望 8s（超时是构造期读一次，所以改完要重建翻译器）", got)
	}
}
