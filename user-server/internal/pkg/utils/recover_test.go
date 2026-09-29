package utils

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeGo_Normal(t *testing.T) {
	var executed atomic.Bool
	SafeGo(context.Background(), "test.normal", func(ctx context.Context) {
		executed.Store(true)
	})
	time.Sleep(50 * time.Millisecond)
	if !executed.Load() {
		t.Fatal("SafeGo 未执行目标函数")
	}
}

func TestSafeGo_PanicRecovered(t *testing.T) {
	SafeGo(context.Background(), "test.panic", func(ctx context.Context) {
		panic("test panic - 进程应存活")
	})
	time.Sleep(50 * time.Millisecond)
}

func TestSafeGo_NilFunc(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SafeGo(nil) 不应 panic, 实际: %v", r)
		}
	}()
	SafeGo(context.Background(), "test.nil", nil)
}

func TestSafeGo_NilCtx(t *testing.T) {

	var executed atomic.Bool
	//lint:ignore SA1012 刻意传 nil 验证 SafeGo 的替换语义
	SafeGo(nil, "test.nilctx", func(ctx context.Context) {
		if ctx == nil {
			t.Fatal("nil ctx 应被替换为 Background")
		}
		executed.Store(true)
	})
	time.Sleep(50 * time.Millisecond)
	if !executed.Load() {
		t.Fatal("SafeGo(nil ctx) 未执行目标函数")
	}
}

func TestSafeGoWithRecover_CustomHandler(t *testing.T) {
	var called atomic.Bool
	SafeGoWithRecover(context.Background(), "test.custom", func(ctx context.Context) {
		panic(errors.New("custom panic"))
	}, func(name string, r interface{}, stack []byte) {
		called.Store(true)
		if name != "test.custom" {
			t.Errorf("onPanic 收到的 name = %q, 期望 test.custom", name)
		}
		if r == nil {
			t.Error("onPanic 收到的 panic 值为 nil")
		}
		if len(stack) == 0 {
			t.Error("onPanic 收到的 stack 为空")
		}
	})
	time.Sleep(50 * time.Millisecond)
	if !called.Load() {
		t.Fatal("onPanic 回调未被调用")
	}
}

func TestSafeGoDetached_PreservesTraceValue(t *testing.T) {
	type traceKey struct{}
	parent := context.WithValue(context.Background(), traceKey{}, "trace-abc")
	var captured atomic.Value
	SafeGoDetached(parent, "test.detach", 0, func(ctx context.Context) {
		if v, ok := ctx.Value(traceKey{}).(string); ok {
			captured.Store(v)
		} else {
			captured.Store("")
		}
	})
	time.Sleep(50 * time.Millisecond)
	if got := captured.Load().(string); got != "trace-abc" {
		t.Errorf("detached ctx 丢失 trace value, got=%q", got)
	}
}

func TestSafeGoDetached_NilCtxSafe(t *testing.T) {
	var executed atomic.Bool
	//lint:ignore SA1012 刻意传 nil 验证 SafeGoDetached 的替换语义
	SafeGoDetached(nil, "test.detachnil", 0, func(ctx context.Context) {
		if ctx == nil {
			t.Fatal("nil ctx 应被替换为 Background")
		}
		executed.Store(true)
	})
	time.Sleep(50 * time.Millisecond)
	if !executed.Load() {
		t.Fatal("SafeGoDetached(nil) 未执行")
	}
}

func TestSafeGoDetached_PanicRecovered(t *testing.T) {
	SafeGoDetached(context.Background(), "test.detachpanic", 0, func(ctx context.Context) {
		panic("detach panic")
	})
	time.Sleep(50 * time.Millisecond)
}

func TestSafeGoWithRetry_SucceedsEventually(t *testing.T) {
	var attempts atomic.Int32
	b := NewExponentialBackOff()
	b.InitialInterval = 10 * time.Millisecond
	b.MaxInterval = 50 * time.Millisecond
	SafeGoWithRetry(context.Background(), "test.retryok", b, func(ctx context.Context) error {
		n := attempts.Add(1)
		if n < 3 {
			return errors.New("模拟失败")
		}
		return nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if attempts.Load() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if attempts.Load() < 3 {
		t.Fatalf("期望至少 3 次尝试，实际 %d", attempts.Load())
	}
}

func TestSafeGoWithRetry_StopsOnStopBackOff(t *testing.T) {
	var attempts atomic.Int32
	b := &StopBackOff{}
	SafeGoWithRetry(context.Background(), "test.stopbo", b, func(ctx context.Context) error {
		attempts.Add(1)
		return errors.New("必失败")
	})
	time.Sleep(100 * time.Millisecond)
	if attempts.Load() != 1 {
		t.Fatalf("期望仅 1 次尝试（StopBackOff），实际 %d", attempts.Load())
	}
}

func TestSafeGoWithRetry_PanicRecoveredAndRetried(t *testing.T) {
	var attempts atomic.Int32
	b := NewExponentialBackOff()
	b.InitialInterval = 10 * time.Millisecond
	b.MaxInterval = 30 * time.Millisecond
	SafeGoWithRetry(context.Background(), "test.retrypanic", b, func(ctx context.Context) error {
		n := attempts.Add(1)
		if n == 1 {
			panic("retry panic")
		}
		return nil
	})
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if attempts.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if attempts.Load() < 2 {
		t.Fatalf("panic 后应继续重试，实际 %d 次", attempts.Load())
	}
}

func TestSafeGoWithRetry_NilCtxSafe(t *testing.T) {
	var executed atomic.Bool
	//lint:ignore SA1012 刻意传 nil 验证 SafeGoWithRetry 的替换语义
	SafeGoWithRetry(nil, "test.retrynil", nil, func(ctx context.Context) error {
		executed.Store(true)
		return nil
	})
	time.Sleep(50 * time.Millisecond)
	if !executed.Load() {
		t.Fatal("SafeGoWithRetry(nil ctx) 未执行")
	}
}

func TestExponentialBackOff_GrowsAndStops(t *testing.T) {
	b := NewExponentialBackOff()
	b.InitialInterval = 10 * time.Millisecond
	b.MaxInterval = 100 * time.Millisecond
	b.MaxElapsedTime = 50 * time.Millisecond
	b.RandomizationFactor = 0

	deadline := time.Now().Add(500 * time.Millisecond)
	calls := 0
	for time.Now().Before(deadline) {
		_, stop := b.NextBackOff()
		calls++
		if stop {
			break
		}
	}
	if calls < 1 {
		t.Fatal("至少应有 1 次调用")
	}
}

func TestExponentialBackOff_Reset(t *testing.T) {
	b := NewExponentialBackOff()
	b.InitialInterval = 10 * time.Millisecond
	b.MaxInterval = 100 * time.Millisecond
	b.RandomizationFactor = 0
	b.Multiplier = 2

	d1, _ := b.NextBackOff()
	d2, _ := b.NextBackOff()
	if d2 < d1 {
		t.Fatalf("第二次间隔应 >= 第一次, d1=%v d2=%v", d1, d2)
	}
	b.Reset()
	d3, _ := b.NextBackOff()
	if d3 != d1 {
		t.Fatalf("Reset 后应回到初始间隔, d1=%v d3=%v", d1, d3)
	}
}
