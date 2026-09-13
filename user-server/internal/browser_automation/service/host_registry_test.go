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
	c := newHostConn(1, "1.4.1", 0, nil, nil)
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

// R4 命令级假死探针：连续超时达阈值→主动 close（触发断连清理+从注册表摘除）；
// 任何回包到达清零计数，防止「偶发慢命令」误杀健康连接。
func TestCmdTimeoutSickProbe(t *testing.T) {
	r := NewHostRegistry()
	fired := make(chan uint, 1)
	r.SetDisconnectHook(func(_ context.Context, userID uint) { fired <- userID })
	c := newHostConn(7, "1.4.1", 0, nil, r)
	r.mu.Lock()
	r.conns[7] = c
	r.mu.Unlock()

	// 1 条超时不判死（单条慢命令属页面异常）
	c.noteCmdTimeout("screenshot")
	select {
	case <-c.closed:
		t.Fatal("1 条超时不应判死")
	default:
	}

	// 第 2 条连续超时 → 判假死：closed 触发 + 钩子收到 + 注册表摘除
	c.noteCmdTimeout("wait")
	select {
	case <-c.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("达阈值应主动 close")
	}
	select {
	case got := <-fired:
		if got != 7 {
			t.Errorf("钩子 userID=%d want 7", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("断连清理钩子未触发")
	}
	if _, ok := r.GetConn(7); ok {
		t.Error("判死后连接应从注册表摘除")
	}

	// 计数清零：回包后重新计数（新连接场景=重连后不误杀）
	c2 := newHostConn(8, "1.4.1", 0, nil, nil)
	c2.noteCmdTimeout("click")
	c2.noteCmdAlive()
	c2.noteCmdTimeout("click")
	select {
	case <-c2.closed:
		t.Error("noteCmdAlive 后不应累积判死")
	default:
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
