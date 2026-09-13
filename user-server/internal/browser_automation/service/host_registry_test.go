package service

import (
	"context"
	"testing"
	"time"
)

// D5 补测（G8 优先级①）：HostRegistry 无连接路径 + pending 关联机制单测。
// 完整 WS 往返（含心跳）需真机/集成环境，这里覆盖纯逻辑分支。

func TestRegistryOfflinePaths(t *testing.T) {
	r := NewHostRegistry()
	if err := r.EnsureOnline(7); err != ErrHostOffline {
		t.Errorf("无连接应 ErrHostOffline, got %v", err)
	}
	if _, err := r.Request(context.Background(), 7, time.Second, map[string]any{"action": "wait"}); err != ErrHostOffline {
		t.Errorf("Request 无连接应 ErrHostOffline, got %v", err)
	}
	st := r.MyStatus(7)
	if st["online"] != false {
		t.Error("MyStatus 离线应 online=false")
	}
	if len(r.Status()) != 0 {
		t.Error("Status 应为空")
	}
}

func TestPendingRegistration(t *testing.T) {
	c := newHostConn(1, "1.3.0", 0, nil, nil)
	ch := c.registerPending("req-a")
	if _, ok := c.pending["req-a"]; !ok {
		t.Fatal("registerPending 未登记")
	}
	ch <- &CommandResult{OK: true} // buffered 1
	c.removePending("req-a")
	if len(c.pending) != 0 {
		t.Error("removePending 未清理")
	}
}

func TestDisconnectHookCalled(t *testing.T) {
	r := NewHostRegistry()
	var got uint
	fired := make(chan uint, 1)
	r.SetDisconnectHook(func(_ context.Context, userID uint) { fired <- userID })
	// 钩子语义直测（完整 conn.close 路径需真 WS 连接，留集成轮）
	r.onDisconnect(42)
	got = <-fired
	if got != 42 {
		t.Errorf("钩子 userID=%d want 42", got)
	}
}
