package cron

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/service"
)

// fakeDomainHealth 内嵌接口只补 job 实际调用的 CheckAll；
// 其余 7 个方法保持 nil 接口值 —— 一旦被调用即 panic，越界依赖会被测试直接打红。
type fakeDomainHealth struct {
	service.DomainHealthService

	mu      sync.Mutex
	calls   int
	latest  context.Context
	results []*service.HealthCheckResult
	err     error
	panics  bool
	notify  chan struct{}
}

func (f *fakeDomainHealth) CheckAll(ctx context.Context) ([]*service.HealthCheckResult, error) {
	f.mu.Lock()
	f.calls++
	f.latest = ctx
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	if f.panics {
		panic("checkall boom")
	}
	return f.results, f.err
}

func (f *fakeDomainHealth) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestDomainHealthRunOnceCallsCheckAll(t *testing.T) {
	f := &fakeDomainHealth{results: []*service.HealthCheckResult{
		{Domain: "a.example.com", DNSOK: true, HTTPOk: true, HealthScore: 90},
		{Domain: "b.example.com", DNSOK: true, HTTPOk: false, HTTPStatus: 500},
		{Domain: "c.example.com", DNSOK: false, HealthScore: 0},
		{Domain: "d.example.com", DNSOK: true, HTTPOk: true, OnBlacklist: true, HealthScore: 70},
	}}
	NewDomainHealthCheckJob(f, nil).runOnce()

	if got := f.count(); got != 1 {
		t.Fatalf("CheckAll 调用次数=%d, want 1", got)
	}
	if f.latest == nil {
		t.Error("CheckAll 应收到非 nil ctx")
	}
}

func TestDomainHealthRunOnceErrorOnlyLogged(t *testing.T) {
	f := &fakeDomainHealth{err: errors.New("探测链路不可用")}
	NewDomainHealthCheckJob(f, nil).runOnce()
	if f.count() != 1 {
		t.Fatalf("CheckAll 调用次数=%d, want 1", f.count())
	}
}

func TestDomainHealthRunOnceRecoversPanic(t *testing.T) {
	f := &fakeDomainHealth{panics: true}
	j := NewDomainHealthCheckJob(f, nil)

	j.runOnce() // recover 生效则 panic 不外溢；否则本用例外层崩溃
	if f.count() != 1 {
		t.Fatalf("panic 前应先完成一次调用, got %d", f.count())
	}

	f.panics = false
	j.runOnce()
	if f.count() != 2 {
		t.Fatalf("上一次 panic 后任务应可继续, 累计调用=%d want 2", f.count())
	}
}

func TestDomainHealthStartProbesImmediately(t *testing.T) {
	notify := make(chan struct{}, 4)
	f := &fakeDomainHealth{notify: notify}
	go NewDomainHealthCheckJob(f, nil).Start()

	select {
	case <-notify:
	case <-time.After(3 * time.Second):
		t.Fatal("Start() 应在启动时立即探测一次，而不是等首个 ticker 周期")
	}
}

func TestDomainHealthStartTickerLoops(t *testing.T) {
	notify := make(chan struct{}, 16)
	f := &fakeDomainHealth{notify: notify}
	j := NewDomainHealthCheckJob(f, nil)
	j.interval = 20 * time.Millisecond // 覆盖生产 5min 周期，验证 ticker 分支确实在循环

	// Start() 内部是 for range ticker.C 死循环，没有停止通道，返回不了——
	// 本测试让其随测试进程结束一起回收。
	go func() { j.Start() }()

	deadline := time.After(2 * time.Second)
	for n := 0; n < 3; n++ {
		select {
		case <-notify:
		case <-deadline:
			t.Fatalf("ticker 循环未持续探测：累计 %d 次, want>=3", f.count())
		}
	}
	if f.count() < 3 {
		t.Fatalf("calls=%d want>=3", f.count())
	}
}

// fakeLiveCode 同上：只实现 rotate() 用到的 RotateLiveCodes。
type fakeLiveCode struct {
	service.LiveCodeService

	mu        sync.Mutex
	calls     int
	err       error
	panicWith any
	notify    chan struct{}
}

func (f *fakeLiveCode) RotateLiveCodes(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	if f.panicWith != nil {
		panic(f.panicWith)
	}
	return f.err
}

func (f *fakeLiveCode) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestLiveCodeRotateSuccess(t *testing.T) {
	f := &fakeLiveCode{}
	NewLiveCodeRotator(f).rotate()
	if f.count() != 1 {
		t.Fatalf("RotateLiveCodes 调用次数=%d, want 1", f.count())
	}
}

func TestLiveCodeRotateFailureDoesNotPanic(t *testing.T) {
	f := &fakeLiveCode{err: errors.New("轮询锁不可用")}
	NewLiveCodeRotator(f).rotate()
	if f.count() != 1 {
		t.Fatalf("RotateLiveCodes 调用次数=%d, want 1", f.count())
	}
}

// rotate() 由裸 goroutine 调用：未捕获的 panic 会终止整个进程（同包 domain_health_job.go
// 的 runOnce 早就有 recover，此处口径必须一致）。
func TestLiveCodeRotateServicePanicIsRecovered(t *testing.T) {
	f := &fakeLiveCode{panicWith: "活码数据不一致"}
	NewLiveCodeRotator(f).rotate()
	if f.count() != 1 {
		t.Fatalf("RotateLiveCodes 调用次数=%d, want 1", f.count())
	}
}

func TestLiveCodeStartRotatesImmediately(t *testing.T) {
	notify := make(chan struct{}, 4)
	f := &fakeLiveCode{notify: notify}
	go NewLiveCodeRotator(f).Start()

	select {
	case <-notify:
	case <-time.After(3 * time.Second):
		t.Fatal("Start() 应在启动时立即轮询一次（生产周期为 1h，否则测试窗口内永不执行）")
	}
}
