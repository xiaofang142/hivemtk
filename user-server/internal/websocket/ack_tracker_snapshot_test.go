package websocket

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
)

// captureCache 是一个只用于测试的 cache.Cache 桩：SetJSON 会阻塞在 start 通道上，
// 直到测试显式放行后才对传入 value 做 json.Marshal 并记录结果。通过这种"闸门"设计，
// 可以确定性地观测后台 goroutine 在读取快照那一刻看到的 map 内容，从而在不依赖
// -race / 调度时序的前提下，锁定 pendingRedisBacked.asyncSetJSON 是否把调用方的
// 实时 map 引用泄漏到了后台 goroutine。
type captureCache struct {
	cache.Cache // 嵌入以复用未实现方法的接口签名（测试仅触达 SetJSON/GetJSON）
	start       chan struct{}
	mu          sync.Mutex
	val         map[uint64]time.Time
	done        chan struct{}
	once        sync.Once
}

func (f *captureCache) SetJSON(_ context.Context, _ string, value any, _ time.Duration) error {
	<-f.start // 等待测试放行（放行前由测试改写原 map）
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var m map[uint64]time.Time
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	f.mu.Lock()
	f.val = m
	f.mu.Unlock()
	f.once.Do(func() { close(f.done) })
	return nil
}

// GetJSON 供 loadRemote（PendingSince 路径）安全触达：返回当前已捕获快照。
func (f *captureCache) GetJSON(_ context.Context, _ string, dest any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := json.Marshal(f.val)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

func withCaptureBackend(t *testing.T, cc *captureCache) {
	t.Helper()
	origEnabled, origBackend := pendingRedis.enabled, pendingRedis.backend
	pendingRedis.enabled = func() bool { return true }
	pendingRedis.backend = func() cache.Cache { return cc }
	t.Cleanup(func() {
		pendingRedis.enabled = origEnabled
		pendingRedis.backend = origBackend
	})
}

// TestPendingAck_asyncSetJSON_IsolatesSnapshot 复现并锁定 R77 修复：
// asyncSetJSON 早期直接把调用方（Track/Ack 在持 p.mu 时传入）的实时 map 引用交给
// 后台 goroutine 去 json.Marshal，主 goroutine 随后继续写该 map → 数据竞争，最坏
// fatal "concurrent map iteration and map write"（不可 recover，整进程崩溃）。修复
// 后必须在派生 goroutine 之前（仍在调用方锁内）做一次深拷贝，让后台只读私有副本。
//
// 确定性验证（不依赖 -race）：闸门前改写原 map，后台读到的快照不得包含改写结果；
// 修复前（直接透传引用）会包含 key 2 且缺失 key 1，用例必红；修复后必绿。
func TestPendingAck_asyncSetJSON_IsolatesSnapshot(t *testing.T) {
	cc := &captureCache{start: make(chan struct{}), done: make(chan struct{})}
	withCaptureBackend(t, cc)

	snap := map[uint64]time.Time{1: time.Now()}
	pendingRedis.asyncSetJSON("s1", snap)

	// 调用返回后改写原 map；修复前后台 goroutine 持有同一引用，放行后读取即见改写。
	snap[2] = time.Now()
	delete(snap, 1)

	close(cc.start) // 放行后台 SetJSON

	select {
	case <-cc.done:
	case <-time.After(3 * time.Second):
		t.Fatal("后台 SetJSON 未在超时内执行")
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()
	if _, leaked := cc.val[2]; leaked {
		t.Fatalf("快照未隔离：后台写入看到调用后的改写 key 2（%v）", cc.val)
	}
	if _, ok := cc.val[1]; !ok {
		t.Fatalf("快照丢失原值 key 1，期望捕获调用时刻内容，got %v", cc.val)
	}
	if len(cc.val) != 1 {
		t.Fatalf("快照内容应恰为 {1}，got %v", cc.val)
	}
}

// TestPendingAck_ConcurrentTrackAckIsRaceFree 并发 Track/Ack/Pending/PendingSince，
// 配合后台 SetJSON 写，专门在 -race 下验证 pending.items 实时 map 不再逃逸到后台
// marshaling（在无 CGO/-race 环境下仍作为无数据竞争的冒烟回归）。
func TestPendingAck_ConcurrentTrackAckIsRaceFree(t *testing.T) {
	cc := &captureCache{start: make(chan struct{}), done: make(chan struct{})}
	withCaptureBackend(t, cc)
	close(cc.start) // 不阻塞后台写，专注并发调用方与后台的共享 map 逃逸

	p := NewPendingAck()
	var wg sync.WaitGroup
	for k := 0; k < 8; k++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				seq := uint64(base*10000 + i)
				p.Track("conv", seq)
				p.Ack("conv", seq)
				_ = p.Pending("conv")
				_ = p.PendingSince("conv", seq)
			}
		}(k)
	}
	wg.Wait()
}
