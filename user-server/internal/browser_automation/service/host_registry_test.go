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

// F7：host/status 响应形状必须与角色无关。扩展 popup 点亮判据是 count>0，
// 普通用户分支（只读自己）历史上只返回 online/servable 不含 count → 已连接的
// 普通用户永久看到「Host 离线」。同时锁 admin 分支的 hosts 来自一次快照
// （旧实现 Status() 调两遍，count 与 hosts 之间可插入注册/摘除而自相矛盾）。
func TestStatusForShapeIsRoleIndependent(t *testing.T) {
	r := NewHostRegistry()

	// 离线：count/hosts 仍要在（缺字段=前端判不了，而不是判成 0）
	off := r.StatusFor(7, false)
	if off["online"] != false || off["count"] != 0 {
		t.Errorf("离线应 online=false/count=0，got %v", off)
	}
	if h, ok := off["hosts"].([]map[string]any); !ok || h == nil {
		t.Error("离线 hosts 应为空切片而非 nil（JSON 要出 []）")
	}

	c := newHostConn(7, "1.4.2", 4242, nil, r)
	r.mu.Lock()
	r.conns[7] = c
	r.mu.Unlock()

	for _, all := range []bool{false, true} {
		st := r.StatusFor(7, all)
		if st["online"] != true || st["count"] != 1 {
			t.Fatalf("all=%t 应 online=true/count=1，got %v", all, st)
		}
		if hosts, ok := st["hosts"].([]map[string]any); !ok || len(hosts) != 1 || hosts[0]["pid"] != 4242 {
			t.Fatalf("all=%t hosts 形状异常，got %v", all, st["hosts"])
		}
		if st["servable"] != false || st["last_cmd_ok_at"] != nil {
			t.Errorf("all=%t 刚注册无回包证据，servable 应为假且无时间戳，got %v / %v", all, st["servable"], st["last_cmd_ok_at"])
		}
	}

	// 有回包证据后：self 与 admin 两个视角都携带 servable + 时间戳
	c.noteCmdAlive()
	self, admin := r.StatusFor(7, false), r.StatusFor(7, true)
	for name, st := range map[string]map[string]any{"self": self, "admin": admin} {
		if st["servable"] != true {
			t.Errorf("%s 视角 servable 应为真", name)
		}
		if v, ok := st["last_cmd_ok_at"].(string); !ok || v == "" {
			t.Errorf("%s 视角缺 last_cmd_ok_at，got %v", name, st["last_cmd_ok_at"])
		}
	}

	// 越权隔离：普通用户视角看不到别人的 Host
	if n := r.StatusFor(8, false)["count"]; n != 0 {
		t.Errorf("user=8 无连接应 count=0，got %v", n)
	}
	if n := r.StatusFor(8, true)["count"]; n != 1 {
		t.Errorf("admin 视角应看到 user=7 的 1 台，got %v", n)
	}
}
