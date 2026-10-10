package middleware

import (
	"os"
	"testing"
)

func resetMaxJSONBodyMB(t *testing.T) {
	t.Helper()
	old := maxJSONBodyMBProvider
	t.Cleanup(func() { maxJSONBodyMBProvider = old })
	maxJSONBodyMBProvider = func() int { return DefaultMaxJSONBodyMB }
}

func TestMaxJSONBodyMBSeam(t *testing.T) {
	const mb = 1024 * 1024

	cases := []struct {
		name     string
		inject   int
		expected int64
	}{
		{"未注入时等于代码兜底", 0, int64(DefaultMaxJSONBodyMB) * mb},
		{"注入生效", 24, 24 * mb},
		{"注入 0 回落兜底", 0, int64(DefaultMaxJSONBodyMB) * mb},
		{"注入负数回落兜底", -5, int64(DefaultMaxJSONBodyMB) * mb},
		{"注入 1 是合法下界", 1, 1 * mb},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetMaxJSONBodyMB(t)
			if c.inject != 0 {
				SetMaxJSONBodyMBProvider(func() int { return c.inject })
			}
			t.Setenv("MAX_JSON_BODY_MB", "")
			if got := BodyLimitFromEnv(); got != c.expected {
				t.Fatalf("BodyLimitFromEnv() = %d, want %d", got, c.expected)
			}
		})
	}
}

// setter 传 nil 是**空操作**而不是复位：与本仓其余 seam 同一语义，
// 防止装配顺序出错时把兜底值顶掉。这里先注入一个已知非兜底值再传 nil。
func TestSetMaxJSONBodyMBProviderNilIsNoop(t *testing.T) {
	resetMaxJSONBodyMB(t)
	SetMaxJSONBodyMBProvider(func() int { return 24 })
	SetMaxJSONBodyMBProvider(nil)
	t.Setenv("MAX_JSON_BODY_MB", "")
	if got := maxJSONBodyMB(); got != 24 {
		t.Fatalf("传 nil 后 = %d, want 24（nil 应是空操作而非复位）", got)
	}
}

// env 排第一，且 env 设 0 仍是「不限制」的既有语义 —— 那是部署层排障开关，
// 参数中心不提供这个口子（种子 Min 卡在 1）。
func TestEnvStillWinsOverSeam(t *testing.T) {
	const mb = 1024 * 1024
	resetMaxJSONBodyMB(t)
	SetMaxJSONBodyMBProvider(func() int { return 24 })

	t.Setenv("MAX_JSON_BODY_MB", "16")
	if got := BodyLimitFromEnv(); got != 16*mb {
		t.Fatalf("env=16 时 = %d, want %d", got, 16*mb)
	}

	t.Setenv("MAX_JSON_BODY_MB", "0")
	if got := BodyLimitFromEnv(); got != 0 {
		t.Fatalf("env=0 应为不限制（0），got %d", got)
	}

	// 非法值不短路：回落参数中心，而不是兜底常量。
	t.Setenv("MAX_JSON_BODY_MB", "abc")
	if got := BodyLimitFromEnv(); got != 24*mb {
		t.Fatalf("env 非法时应回落参数中心 24MB，got %d", got)
	}
}

// 门禁反向钉死：参数默认值不能低于既有按端点上界，否则全局默认值会变成新的天花板。
func TestSeedDefaultStaysAboveExistingCaps(t *testing.T) {
	resetMaxJSONBodyMB(t)
	t.Setenv("MAX_JSON_BODY_MB", "")
	got := BodyLimitFromEnv()
	if got < maxWebhookMaxBody {
		t.Fatalf("全局默认 %d 低于 webhook 上界 %d，会把已放行的高段请求截断", got, maxWebhookMaxBody)
	}
	if want := int64(DefaultMaxJSONBodyMB) * 1024 * 1024; got != want {
		t.Fatalf("默认 %d, want %d", got, want)
	}
}

func TestMaxMultipartMemoryIndependentOfBodyLimit(t *testing.T) {
	resetMaxJSONBodyMB(t)
	SetMaxJSONBodyMBProvider(func() int { return 200 })
	// multipart 内存缓冲是主动把 gin 默认 32MB 调小的独立优化，不跟随 body 上限联动 ——
	// 否则「把 body 上限调到 200MB」会顺带把每个并发上传的内存放大 4 倍。
	if got, want := MaxMultipartMemoryBytes(), int64(8)*1024*1024; got != want {
		t.Fatalf("MaxMultipartMemoryBytes() = %d, want %d（不应跟随 body 上限）", got, want)
	}
}

func TestMaxJSONBodyMBProviderDefaultIsConst(t *testing.T) {
	// 未经注入时 provider 的返回值必须等于常量本身，这样「参数中心没装配」与
	// 「参数中心配成默认值」两条路径不可区分 —— 上线时不该有行为跳变。
	old := os.Getenv("MAX_JSON_BODY_MB")
	t.Setenv("MAX_JSON_BODY_MB", "")
	defer func() { _ = os.Setenv("MAX_JSON_BODY_MB", old) }()
	if got := maxJSONBodyMBProvider(); got != DefaultMaxJSONBodyMB {
		t.Fatalf("默认 provider = %d, want %d", got, DefaultMaxJSONBodyMB)
	}
}
