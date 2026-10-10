package llm

import (
	"testing"
	"time"
)

// 阶段一 agent_llm 组的 seam 测试。每个读取口都逐格钉死四件事：
//   未注入 == 代码兜底 / 注入生效 / 非法值回落兜底 / setter 传 nil 等于未注入。
//
// 最后一格最容易被忽略却最要命：装配层在某些顺序下会拿到一个还没接上的全局单例，
// 传 nil 进去如果被当成"注入一个空 provider"，读点就永久返回零值而不是兜底值。

func TestDurationSeamsFallBackAndInject(t *testing.T) {
	resetFailoverSeams(t)
	resetSinkSeam(t)
	resetHTTPTimeoutSeam(t)

	for _, tc := range []struct {
		name     string
		set      func(func() time.Duration)
		read     func() time.Duration
		def      time.Duration
		injected time.Duration
	}{
		{"HealthCheckInterval", SetHealthCheckIntervalProvider, HealthCheckInterval, DefaultHealthCheckInterval, 90 * time.Second},
		{"CircuitOpenDuration", SetCircuitOpenDurationProvider, CircuitOpenDuration, DefaultCircuitOpenDuration, 150 * time.Second},
		{"HealthCheckTimeout", SetHealthCheckTimeoutProvider, HealthCheckTimeout, DefaultHealthCheckTimeout, 12 * time.Second},
		{"DBSinkStopDeadline", SetDBSinkStopDeadlineProvider, DBSinkStopDeadline, DefaultDBSinkStopDeadline, 8 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.read(); got != tc.def {
				t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, tc.def)
			}
			tc.set(func() time.Duration { return tc.injected })
			if got := tc.read(); got != tc.injected {
				t.Errorf("注入后 = %v，期望 %v", got, tc.injected)
			}
			// 非法值：0 会让健康检查退化/熔断失效/尾段事件被丢，负数更糟，一律回落。
			tc.set(func() time.Duration { return 0 })
			if got := tc.read(); got != tc.def {
				t.Errorf("注入 0 后 = %v，期望回落兜底 %v", got, tc.def)
			}
			tc.set(func() time.Duration { return -5 * time.Second })
			if got := tc.read(); got != tc.def {
				t.Errorf("注入负数后 = %v，期望回落兜底 %v", got, tc.def)
			}
			// setter 传 nil = 撤销注入，不是"置一个空 provider"。
			tc.set(nil)
			if got := tc.read(); got != tc.def {
				t.Errorf("传 nil 撤销注入后 = %v，期望回到兜底 %v", got, tc.def)
			}
		})
	}
}

func TestFailureThresholdSeam(t *testing.T) {
	resetFailoverSeams(t)
	if got := FailureThreshold(); got != DefaultFailureThreshold {
		t.Fatalf("未注入 = %d，期望 %d", got, DefaultFailureThreshold)
	}
	SetFailureThresholdProvider(func() int { return 11 })
	if got := FailureThreshold(); got != 11 {
		t.Errorf("注入后 = %d，期望 11", got)
	}
	SetFailureThresholdProvider(func() int { return 0 })
	if got := FailureThreshold(); got != DefaultFailureThreshold {
		t.Errorf("注入 0 后 = %d，期望回落 %d（阈值 0 会让一次网络抖动就熔断整个 provider）", got, DefaultFailureThreshold)
	}
	SetFailureThresholdProvider(nil)
	if got := FailureThreshold(); got != DefaultFailureThreshold {
		t.Errorf("传 nil 后 = %d，期望 %d", got, DefaultFailureThreshold)
	}
}

func TestVoteAgreementThresholdSeam(t *testing.T) {
	resetVoteSeam(t)
	if got := VoteAgreementThreshold(); got != DefaultVoteAgreementThreshold {
		t.Fatalf("未注入 = %v，期望 %v", got, DefaultVoteAgreementThreshold)
	}
	SetVoteAgreementThresholdProvider(func() float64 { return 0.55 })
	if got := VoteAgreementThreshold(); got != 0.55 {
		t.Errorf("注入后 = %v，期望 0.55", got)
	}
	// 0 或负数：任何一对答案都算"一致"，投票退化成随便选一个。
	for _, bad := range []float64{0, -0.1} {
		SetVoteAgreementThresholdProvider(func() float64 { return bad })
		if got := VoteAgreementThreshold(); got != DefaultVoteAgreementThreshold {
			t.Errorf("注入 %v 后 = %v，期望回落 %v", bad, got, DefaultVoteAgreementThreshold)
		}
	}
	// 大于 1：永远选不出共识，退化成每次都报"无一致答案"。
	SetVoteAgreementThresholdProvider(func() float64 { return 1.5 })
	if got := VoteAgreementThreshold(); got != DefaultVoteAgreementThreshold {
		t.Errorf("注入 1.5 后 = %v，期望回落 %v", got, DefaultVoteAgreementThreshold)
	}
	// 1.0 是合法上界：要求完全一致才算一致，是有意义的配置，不能被当成非法值回落。
	SetVoteAgreementThresholdProvider(func() float64 { return 1.0 })
	if got := VoteAgreementThreshold(); got != 1.0 {
		t.Errorf("注入 1.0 后 = %v，期望 1.0（上界必须被接受）", got)
	}
	SetVoteAgreementThresholdProvider(nil)
}

// TestHealthCheckerAndClientPickUpTimeoutSeam 钉住"构造期读一次"的两个读取口。
// 构造器读一次是这类超时的固有形态，最容易在重构时被绕回常量。
func TestHealthCheckerAndClientPickUpTimeoutSeam(t *testing.T) {
	resetFailoverSeams(t)
	resetHTTPTimeoutSeam(t)

	if got := NewHTTPHealthChecker().httpClient.Timeout; got != DefaultHealthCheckTimeout {
		t.Fatalf("未注入时健康检查器超时 = %v，期望 %v", got, DefaultHealthCheckTimeout)
	}
	SetHealthCheckTimeoutProvider(func() time.Duration { return 17 * time.Second })
	if got := NewHTTPHealthChecker().httpClient.Timeout; got != 17*time.Second {
		t.Errorf("注入后健康检查器超时 = %v，期望 17s", got)
	}

	if got := NewLLMService().httpClient.Timeout; got != DefaultLLMHTTPTimeout {
		t.Fatalf("未注入时 LLM 客户端超时 = %v，期望 %v", got, DefaultLLMHTTPTimeout)
	}
	SetDefaultHTTPTimeoutProvider(func() time.Duration { return 42 * time.Second })
	if got := NewLLMService().httpClient.Timeout; got != 42*time.Second {
		t.Errorf("注入后 LLM 客户端超时 = %v，期望 42s", got)
	}
}

// TestDefaultFailoverConfigReadsParams 熔断四件套里有三个的兜底值流进
// DefaultFailoverConfig()，而它又是 system_config_kv 的 llm_provider_failover
// 缺失时的回退。这条断言证明"参数中心的值确实进了降级策略默认值"，
// 而不只是停在 seam 上没人用。
func TestDefaultFailoverConfigReadsParams(t *testing.T) {
	resetFailoverSeams(t)
	SetHealthCheckIntervalProvider(func() time.Duration { return 45 * time.Second })
	SetFailureThresholdProvider(func() int { return 8 })
	SetCircuitOpenDurationProvider(func() time.Duration { return 200 * time.Second })

	cfg := DefaultFailoverConfig()
	if cfg.HealthCheckInterval != 45 {
		t.Errorf("DefaultFailoverConfig().HealthCheckInterval = %d，期望 45", cfg.HealthCheckInterval)
	}
	if cfg.FailureThreshold != 8 {
		t.Errorf("DefaultFailoverConfig().FailureThreshold = %d，期望 8", cfg.FailureThreshold)
	}
	if cfg.CircuitOpenDuration != 200 {
		t.Errorf("DefaultFailoverConfig().CircuitOpenDuration = %d，期望 200", cfg.CircuitOpenDuration)
	}
}

// TestDBSinkStopDeadlineIsUsedByStop 确认 sink 的读取口确实被 Stop() 用上。
func TestDBSinkStopDeadlineIsUsedByStop(t *testing.T) {
	resetSinkSeam(t)
	sink := NewDBTraceSink(nil)
	SetDBSinkStopDeadlineProvider(func() time.Duration { return 50 * time.Millisecond })
	if got := DBSinkStopDeadline(); got != 50*time.Millisecond {
		t.Fatalf("seam 未生效：got %v", got)
	}
	// Stop 幂等；这里只要求它不挂死、不 panic。
	done := make(chan struct{})
	go func() { sink.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() 超过 3s 未返回")
	}
}

// 下面四个 helper 把包级 provider 摘回未装配状态，供各用例在开头统一调用。
func resetFailoverSeams(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetHealthCheckIntervalProvider(nil)
		SetFailureThresholdProvider(nil)
		SetCircuitOpenDurationProvider(nil)
		SetHealthCheckTimeoutProvider(nil)
	})
}

func resetVoteSeam(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { SetVoteAgreementThresholdProvider(nil) })
}

func resetSinkSeam(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { SetDBSinkStopDeadlineProvider(nil) })
}

func resetHTTPTimeoutSeam(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { SetDefaultHTTPTimeoutProvider(nil) })
}
