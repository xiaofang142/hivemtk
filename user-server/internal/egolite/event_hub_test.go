package egolite

// event_hub_test.go —— S-13 广播语义（纯内存，不依赖 PG）。

import (
	"sync"
	"testing"
	"time"
)

func waitSignal(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatalf("通道被关闭，不该在有订阅时关")
		}
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

// 1. 订阅者收到广播；不同执行的信号互不串
func TestEgoEventHub_PublishScopedByExecution(t *testing.T) {
	hub := NewEgoEventHub()
	chA, cancelA := hub.Subscribe(1)
	defer cancelA()
	chB, cancelB := hub.Subscribe(2)
	defer cancelB()

	hub.Publish(1)
	if !waitSignal(t, chA) {
		t.Fatalf("执行 1 的订阅者没收到信号")
	}
	select {
	case <-chB:
		t.Fatalf("执行 2 的订阅者收到了别的执行的信号")
	case <-time.After(50 * time.Millisecond):
	}
}

// 2. 未消费的信号合并成一拍（慢订阅者不排队、不阻塞发布方）
func TestEgoEventHub_CoalescesSignals(t *testing.T) {
	hub := NewEgoEventHub()
	ch, cancel := hub.Subscribe(3)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			hub.Publish(3)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Publish 被慢订阅者阻塞了（必须非阻塞）")
	}
	if !waitSignal(t, ch) {
		t.Fatalf("合并后至少要有 1 拍信号")
	}
	select {
	case <-ch:
		t.Fatalf("容量 1 且未消费时不该攒出第二拍")
	case <-time.After(50 * time.Millisecond):
	}
}

// 3. 退订后不再收到信号，且通道被关闭；重复退订安全
func TestEgoEventHub_Unsubscribe(t *testing.T) {
	hub := NewEgoEventHub()
	ch, cancel := hub.Subscribe(4)
	cancel()
	cancel() // 幂等

	if n := hub.SubscriberCount(4); n != 0 {
		t.Fatalf("退订后订阅数应为 0，got %d", n)
	}
	hub.Publish(4)
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("退订后不该再有信号")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("退订应关闭通道")
	}
}

// 4. 并发订阅/广播/退订不 panic（多 runner 事件与多 SSE 连接并发是常态）
func TestEgoEventHub_ConcurrentSafety(t *testing.T) {
	hub := NewEgoEventHub()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cancel := hub.Subscribe(5)
			defer cancel()
			go hub.Publish(5)
			select {
			case <-ch:
			case <-time.After(50 * time.Millisecond):
			}
		}()
	}
	wg.Wait()
	if n := hub.SubscriberCount(5); n != 0 {
		t.Fatalf("全部退订后订阅数应为 0，got %d", n)
	}
}

// 5. nil hub 安全（未装配广播时服务层不该因此 panic）
func TestEgoEventHub_NilSafe(t *testing.T) {
	var hub *EgoEventHub
	ch, cancel := hub.Subscribe(6)
	if ch == nil {
		t.Fatalf("nil hub 也要返回可用通道")
	}
	cancel()
	hub.Publish(6)
	if n := hub.SubscriberCount(6); n != 0 {
		t.Fatalf("nil hub 订阅数应为 0")
	}
}
