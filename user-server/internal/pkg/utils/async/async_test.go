package async

import (
	"context"
	"testing"
	"time"
)

type ctxKey string

func TestDetachContext_KeepsValueStopsCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey("trace"), "t1"))
	detached := DetachContext(parent)
	cancel() // 取消父 ctx，detached 不应受影响

	if got := detached.Value(ctxKey("trace")); got != "t1" {
		t.Fatalf("value should survive detach, got %v", got)
	}
	select {
	case <-detached.Done():
		t.Fatal("detached ctx should not be cancelled by parent")
	default:
	}
}

func TestRunWithTimeout_TimeoutFires(t *testing.T) {
	done := make(chan struct{})
	RunWithTimeout(context.Background(), 30*time.Millisecond, func(ctx context.Context) {
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("task did not observe timeout in time")
	}
}

func TestSafeGo_RecoversPanic(t *testing.T) {
	done := make(chan struct{})
	SafeGo(func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("panic in SafeGo should be recovered, not crash")
	}
}
