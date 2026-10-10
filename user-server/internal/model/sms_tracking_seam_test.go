package model

import "testing"

// TestSmsMaxRetrySeam 短信重试上限的接线三格。
//
// DefaultSmsMaxRetry 与参数中心 misc.sms_max_retry 的 DefaultValue（3）必须相等：
// 两者不等时，"库里有这条参数"和"库里没这条参数"会走出两条不同的重试次数。
func TestSmsMaxRetrySeam(t *testing.T) {
	restore := smsMaxRetryProvider
	t.Cleanup(func() { smsMaxRetryProvider = restore })

	if got, want := DefaultSmsMaxRetry, 3; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.sms_max_retry 的 DefaultValue（%d）不一致", got, want)
	}

	SetSmsMaxRetryProvider(nil)
	if got := GetSmsMaxRetry(); got != DefaultSmsMaxRetry {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, DefaultSmsMaxRetry)
	}

	SetSmsMaxRetryProvider(func() int { return 7 })
	if got := GetSmsMaxRetry(); got != 7 {
		t.Fatalf("注入后 = %v，期望 7", got)
	}
	SetSmsMaxRetryProvider(func() int { return -1 })
	if got := GetSmsMaxRetry(); got != DefaultSmsMaxRetry {
		t.Fatalf("注入负数时 = %v，期望回落 %v", got, DefaultSmsMaxRetry)
	}
}
