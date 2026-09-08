package event

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEventBus_PublishSubscribe 基础发布订阅
func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := New(1, 16)
	defer bus.Stop()

	var received atomic.Int32
	bus.Subscribe("test.topic", func(evt Event) error {
		if evt.Topic != "test.topic" {
			t.Errorf("expected topic test.topic, got %s", evt.Topic)
		}
		received.Add(1)
		return nil
	})

	bus.Publish(Event{Topic: "test.topic", Payload: "hello"})

	waitForCondition(t, func() bool { return received.Load() == 1 }, 100*time.Millisecond)
	if received.Load() != 1 {
		t.Errorf("expected 1 event received, got %d", received.Load())
	}
}

// TestEventBus_MultipleSubscribers 多订阅者：单事件分发到所有订阅者
func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := New(1, 16)
	defer bus.Stop()

	var count atomic.Int32
	bus.Subscribe("topic.a", func(evt Event) error {
		count.Add(1)
		return nil
	})
	bus.Subscribe("topic.a", func(evt Event) error {
		count.Add(1)
		return nil
	})

	bus.Publish(Event{Topic: "topic.a"})

	waitForCondition(t, func() bool { return count.Load() == 2 }, 100*time.Millisecond)
	if count.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", count.Load())
	}
}

// TestEventBus_TopicIsolation 主题隔离：只有匹配的订阅者收到
func TestEventBus_TopicIsolation(t *testing.T) {
	bus := New(2, 16)
	defer bus.Stop()

	var aCount, bCount atomic.Int32
	bus.Subscribe("topic.a", func(evt Event) error {
		aCount.Add(1)
		return nil
	})
	bus.Subscribe("topic.b", func(evt Event) error {
		bCount.Add(1)
		return nil
	})

	bus.Publish(Event{Topic: "topic.a"})
	bus.Publish(Event{Topic: "topic.b"})

	waitForCondition(t, func() bool { return aCount.Load() == 1 && bCount.Load() == 1 }, 200*time.Millisecond)
	if aCount.Load() != 1 {
		t.Errorf("expected topic.a count 1, got %d", aCount.Load())
	}
	if bCount.Load() != 1 {
		t.Errorf("expected topic.b count 1, got %d", bCount.Load())
	}
}

// TestEventBus_HandlerError 不阻断：单个 handler 失败不影响后续 handler
func TestEventBus_HandlerError(t *testing.T) {
	bus := New(1, 16)
	defer bus.Stop()

	var executed atomic.Int32
	bus.Subscribe("topic.err", func(evt Event) error {
		return ErrTestHandlerFailure
	})
	bus.Subscribe("topic.err", func(evt Event) error {
		executed.Add(1)
		return nil
	})

	bus.Publish(Event{Topic: "topic.err"})

	waitForCondition(t, func() bool { return executed.Load() == 1 }, 2*time.Second)
	if executed.Load() != 1 {
		t.Errorf("expected second handler to execute despite first failure, got %d", executed.Load())
	}
}

// TestEventBus_QueueFull 队列满丢弃：不阻塞调用方
func TestEventBus_QueueFull(t *testing.T) {
	bus := New(1, 2)
	defer bus.Stop()

	var processed atomic.Int32
	workerStarted := make(chan struct{})
	blockCh := make(chan struct{})
	bus.Subscribe("topic.full", func(evt Event) error {
		select {
		case workerStarted <- struct{}{}:
		default:
		}
		<-blockCh
		processed.Add(1)
		return nil
	})

	bus.Publish(Event{Topic: "topic.full"})

	<-workerStarted

	for i := 0; i < 3; i++ {
		bus.Publish(Event{Topic: "topic.full"})
	}

	close(blockCh)

	waitForCondition(t, func() bool { return processed.Load() == 3 }, 500*time.Millisecond)
	if processed.Load() != 3 {
		t.Errorf("expected 3 processed (1 initial + 2 queued, 1 dropped), got %d", processed.Load())
	}
}

// TestEventBus_StopGraceful 优雅关闭：等待队列中事件处理完成
func TestEventBus_StopGraceful(t *testing.T) {
	bus := New(1, 64)

	var processed atomic.Int32
	bus.Subscribe("topic.stop", func(evt Event) error {
		processed.Add(1)
		return nil
	})

	for i := 0; i < 10; i++ {
		bus.Publish(Event{Topic: "topic.stop"})
	}

	bus.Stop()

	if processed.Load() != 10 {
		t.Errorf("expected all 10 events processed after Stop, got %d", processed.Load())
	}
}

// TestEventBus_StopIdempotent Stop 幂等：可重复调用
func TestEventBus_StopIdempotent(t *testing.T) {
	bus := New(1, 16)
	bus.Stop()
	bus.Stop()
	bus.Stop()
}

// TestEventBus_NilSafe nil 安全：nil 总线方法不 panic
func TestEventBus_NilSafe(t *testing.T) {
	var nilBus *EventBus
	nilBus.Publish(Event{Topic: "nil"})
	nilBus.Subscribe("nil", func(evt Event) error { return nil })
	nilBus.Stop()
	if nilBus.HasSubscribers("nil") {
		t.Error("nil bus should not have subscribers")
	}
}

// TestEventBus_HasSubscribers 检查订阅者
func TestEventBus_HasSubscribers(t *testing.T) {
	bus := New(1, 16)
	defer bus.Stop()

	if bus.HasSubscribers("topic.x") {
		t.Error("expected no subscribers initially")
	}

	bus.Subscribe("topic.x", func(evt Event) error { return nil })
	if !bus.HasSubscribers("topic.x") {
		t.Error("expected subscribers after Subscribe")
	}
}

// TestEventBus_AutoTimestamp 自动填充时间戳
func TestEventBus_AutoTimestamp(t *testing.T) {
	bus := New(1, 16)
	defer bus.Stop()

	var ts time.Time
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe("topic.ts", func(evt Event) error {
		ts = evt.Timestamp
		wg.Done()
		return nil
	})

	before := time.Now()
	bus.Publish(Event{Topic: "topic.ts", Timestamp: time.Time{}})
	wg.Wait()

	if ts.IsZero() {
		t.Error("expected timestamp to be auto-filled")
	}
	if ts.Before(before) {
		t.Error("timestamp should not be before publish time")
	}
}

// TestEventBus_ConcurrentPublish 并发发布
func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := New(4, 1024)
	defer bus.Stop()

	var received atomic.Int32
	bus.Subscribe("topic.concurrent", func(evt Event) error {
		received.Add(1)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(Event{Topic: "topic.concurrent"})
		}()
	}
	wg.Wait()

	waitForCondition(t, func() bool { return received.Load() == 100 }, 500*time.Millisecond)
	if received.Load() != 100 {
		t.Errorf("expected 100 events, got %d", received.Load())
	}
}

// TestGlobalBus 全局单例
func TestGlobalBus(t *testing.T) {
	SetGlobalBus(nil)

	if GetGlobalBus() != nil {
		t.Error("expected nil global bus after SetGlobalBus(nil)")
	}

	Publish("topic.nil", "payload")

	bus := New(1, 16)
	SetGlobalBus(bus)
	defer func() {
		SetGlobalBus(nil)
	}()

	if GetGlobalBus() != bus {
		t.Error("expected global bus to be set")
	}

	var received atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe("topic.global", func(evt Event) error {
		received.Add(1)
		wg.Done()
		return nil
	})

	Publish("topic.global", "test")
	wg.Wait()

	if received.Load() != 1 {
		t.Errorf("expected 1 event via global Publish, got %d", received.Load())
	}

	StopGlobal()
	if GetGlobalBus() != nil {
		t.Error("expected nil global bus after StopGlobal")
	}
}

var ErrTestHandlerFailure = newTestError("handler failure")

type testError string

func (e testError) Error() string { return string(e) }

func newTestError(s string) error { return testError(s) }

func waitForCondition(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
