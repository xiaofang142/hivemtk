package cron

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	robcron "github.com/robfig/cron/v3"
)

// 调度器的防重入必须覆盖真实任务体。
func TestGoTask_SkipCoversBody(t *testing.T) {
	mgr := &TaskManager{}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	returned := make(chan struct{})
	var bodies sync.WaitGroup
	job := robcron.NewChain(robcron.SkipIfStillRunning(robcron.DefaultLogger)).Then(robcron.FuncJob(mgr.goTask("blocked", func(context.Context) {
		bodies.Add(1)
		defer bodies.Done()
		started <- struct{}{}
		<-release
	})))
	defer func() { close(release); <-returned; bodies.Wait() }()
	go func() { job.Run(); close(returned) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("首轮任务没有启动")
	}
	select {
	case <-returned:
		t.Fatal("任务体尚未结束，cron 包装器已经返回，防重入失效")
	default:
	}
	job.Run()
	select {
	case <-started:
		t.Fatal("首轮未结束时第二轮任务体重叠启动")
	default:
	}
}

// panic 必须被恢复且释放防重入锁，下一轮仍能正常执行。
func TestGoTask_PanicReleasesSkip(t *testing.T) {
	mgr := newTaskManager()
	defer mgr.cancel()
	calls := 0
	id, err := mgr.AddTask("* * * * * *", mgr.goTask("panic", func(context.Context) {
		calls++
		if calls == 1 {
			panic("受控测试异常")
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	job := mgr.cron.Entry(id).WrappedJob
	job.Run()
	job.Run()
	if calls != 2 {
		t.Fatalf("异常后任务没有恢复执行：%d", calls)
	}
}

// 使用真实调度器确认 Stop 取消正在执行的任务并禁止新任务登记。
func TestTaskManager_StopCancelsAndDrains(t *testing.T) {
	mgr := newTaskManager()
	started := make(chan struct{})
	finished := make(chan struct{})
	_, err := mgr.AddTask("* * * * * *", mgr.goTask("cancel", func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}))
	if err != nil {
		t.Fatal(err)
	}
	mgr.cron.Start()
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("任务未被真实调度器触发")
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("取消任务后排空失败：%v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Stop 在任务结束前返回")
	}
	if _, err := mgr.AddTask("* * * * * *", func() { t.Error("停机后启动新任务") }); err == nil {
		t.Fatal("停机后仍允许登记任务")
	}
	called := false
	mgr.goTask("after-stop", func(context.Context) { called = true })()
	if called {
		t.Fatal("停机后包装器仍执行任务")
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("重复停止失败：%v", err)
	}
}

// 不配合取消的任务必须报告超时，释放后允许再次等待排空。
func TestTaskManager_StopReportsDeadline(t *testing.T) {
	mgr := newTaskManager()
	started, release := make(chan struct{}), make(chan struct{})
	_, err := mgr.AddTask("* * * * * *", mgr.goTask("blocked", func(context.Context) {
		close(started)
		<-release
	}))
	if err != nil {
		t.Fatal(err)
	}
	mgr.cron.Start()
	t.Cleanup(func() { close(release); _ = mgr.Stop(context.Background()) })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("阻塞任务未启动")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("应报告等待取消，实际：%v", err)
	}
}
