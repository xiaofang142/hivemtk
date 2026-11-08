package service

import (
	"context"
	"testing"
	"time"
)

func newRecallMonitor() *RagRecallMonitorService {
	return NewRagRecallMonitorService(nil, 0, 0)
}

// 1) 构造
func TestRagRecallMonitor_NewService(t *testing.T) {
	s := NewRagRecallMonitorService(nil, 10*time.Second, time.Hour)
	if s.interval != 10*time.Second {
		t.Errorf("Expected interval=10s, got %v", s.interval)
	}
	if s.window != time.Hour {
		t.Errorf("Expected window=1h, got %v", s.window)
	}

	s2 := newRecallMonitor()
	if s2.interval != RagRecallMonitorDefaultInterval {
		t.Errorf("Default interval mismatch")
	}
	if s2.window != RagRecallMonitorDefaultWindow {
		t.Errorf("Default window mismatch")
	}
}

// 2) Collect 空数据场景
func TestRagRecallMonitor_Collect_NilDB(t *testing.T) {
	s := newRecallMonitor()
	_, err := s.Collect(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Error("Expected error for nil db")
	}
}

func TestRagRecallMonitor_Collect_EndBeforeStart(t *testing.T) {
	s := newRecallMonitor()
	now := time.Now()
	_, err := s.Collect(context.Background(), now, now.Add(-time.Hour))
	if err == nil {
		t.Error("Expected error for end<start")
	}
}

func TestRagRecallMonitor_CollectAndStore_NilDB(t *testing.T) {
	s := newRecallMonitor()
	_, err := s.CollectAndStore(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Error("Expected error for nil db")
	}
	snap, _ := s.GetLatestSnapshot(context.Background())
	if snap == nil {
		_ = snap
	}
}

// 3) GetLatestSnapshot
func TestRagRecallMonitor_GetLatestSnapshot_Empty(t *testing.T) {
	s := newRecallMonitor()
	snap, ts := s.GetLatestSnapshot(context.Background())
	if snap != nil {
		t.Errorf("Expected nil snapshot, got %v", snap)
	}
	if !ts.IsZero() {
		t.Errorf("Expected zero time, got %v", ts)
	}
}

// 4) Start/Stop
func TestRagRecallMonitor_StartStop(t *testing.T) {
	s := NewRagRecallMonitorService(nil, 50*time.Millisecond, time.Hour)
	s.Start(context.Background())
	s.Start(context.Background())
	time.Sleep(80 * time.Millisecond)
	s.Stop(context.Background())
	s.Stop(context.Background())
}

// #791 回归：start→stop 后必须能再次 start。
//
// 原实现 stopCh 只在构造函数里建一次：Stop 后 channel 保持已关闭，
// 再 Start 时 goroutine 立即从 <-s.stopCh 退出（采集静默停摆），
// 再 Stop 对已关闭 channel 二次 close → panic → HTTP 500。
// 线上连续 3 轮 start→stop 每轮 stop 均 500 实证。
func TestRagRecallMonitor_RestartAfterStop(t *testing.T) {
	s := NewRagRecallMonitorService(nil, 50*time.Millisecond, time.Hour)
	for round := 1; round <= 3; round++ {
		s.Start(context.Background())
		s.Stop(context.Background()) // 修复前：第 2 轮起对已关闭 stopCh 二次 close panic
	}
	// 三轮后仍能再走完一轮完整周期。
	s.Start(context.Background())
	s.Stop(context.Background())
}

// #791 伴生断言：Stop 之后再 Start，后台 goroutine 必须真的在跑，
// 而不是拿着已关闭的 stopCh 立即退出（那会让采集静默停摆，比 panic 更隐蔽）。
func TestRagRecallMonitor_StartAfterStopKeepsGoroutineAlive(t *testing.T) {
	s := NewRagRecallMonitorService(nil, 20*time.Millisecond, time.Hour)
	s.Start(context.Background())
	s.Stop(context.Background())

	s.Start(context.Background())
	// 修复前：goroutine 第一轮 select 就命中已关闭 stopCh 退出；这里隔 3 个 tick
	// 再 Stop——若 goroutine 已死，started 仍为 true，Stop 会 close 已关闭的
	// channel panic，测试以 panic 失败暴露"停摆"。
	time.Sleep(70 * time.Millisecond)
	s.Stop(context.Background())
}

// 5) EnsureSchema
func TestRagRecallMonitor_EnsureSchema_NilDB(t *testing.T) {
	s := newRecallMonitor()
	if err := s.EnsureSchema(context.Background()); err == nil {
		t.Error("Expected error for nil db")
	}
}

// 6) ListSnapshots
func TestRagRecallMonitor_ListSnapshots_NilDB(t *testing.T) {
	s := newRecallMonitor()
	_, err := s.ListSnapshots(context.Background(), 10)
	if err == nil {
		t.Error("Expected error for nil db")
	}
}

// 7) 越界参数 fallback
func TestRagRecallMonitor_BoundaryParams(t *testing.T) {
	s := NewRagRecallMonitorService(nil, -1, -1)
	if s.interval != RagRecallMonitorDefaultInterval {
		t.Errorf("Expected default interval, got %v", s.interval)
	}
	if s.window != RagRecallMonitorDefaultWindow {
		t.Errorf("Expected default window, got %v", s.window)
	}
}
