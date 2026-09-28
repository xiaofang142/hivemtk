// 同一用户出现第二条 Host 连接时的替换语义（真机只读执行腿暴露）。
//
// 现场：一个用户先后起了两个 Host 进程（扩展自拉的那个 + 手工/第二个浏览器起的那个），
// 旧实现只 `go old.close()`——被顶掉的那个 Host 进程不退出，它的 WS 退避在「拨号成功」
// 时立刻归零，于是毫秒级重连重注册、再把对方顶掉，两边互逐不停：
// host/status 永远 count=1 而 servable=false，日志被刷满，用户点「执行」随机拿到「Host 离线」。
//
// 三条不变量（缺一条就退回上面那个现场）：
//  1. 被顶掉的一方要在关闭前收到一条退出控制帧——host 进程退出后由 Chrome 重拉，
//     而不是揣着一条已让位的 WS 继续抢；
//  2. 让位的那次关闭不得触发「该用户已无 Host」清理钩子——在场的那个还在跑会话；
//  3. 短时间内反复互逐时保留在场连接、拒绝新来的，把抢占从「谁都不可用」收敛成「一个可用」。
package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// hostTakeoverPair 起一条真 WS（客户端↔服务端）并登记进同一个注册表，
// 返回**客户端**侧连接：测试从这一侧读服务端写下来的控制帧。
func hostTakeoverPair(t *testing.T, reg *HostRegistry, srvURL string, wired <-chan *websocket.Conn, userID uint, pid int) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(srvURL, "http")+"/host-ws", nil)
	if err != nil {
		t.Fatalf("pid=%d 客户端连接失败: %v", pid, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	serverConn := <-wired
	reg.Register(userID, "1.5.0-takeover", pid, serverConn)
	return client
}

// newTakeoverServer 起一个只负责升级 WS 的服务端，升级成功的连接塞进 wired 供测试登记。
func newTakeoverServer(t *testing.T, capacity int) (string, chan *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wired := make(chan *websocket.Conn, capacity)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("升级失败: %v", err)
			return
		}
		wired <- c
	}))
	return srv.URL, wired, srv.Close
}

// readShutdownFrame 在读到的帧里找退出控制帧，其余帧（注册探针等）跳过。
// 返回 reason；没读到就带上已经跳过了哪些 action，免得归因时只剩一句「超时」。
func readShutdownFrame(t *testing.T, conn *websocket.Conn, within time.Duration) (string, error) {
	t.Helper()
	deadline := time.Now().Add(within)
	var skipped []string
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return "", &timeoutSkippedError{skipped: skipped}
		}
		_ = conn.SetReadDeadline(time.Now().Add(left))
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			return "", err
		}
		if action, _ := frame["action"].(string); action == "__host_shutdown__" {
			reason, _ := frame["reason"].(string)
			return reason, nil
		}
		action, _ := frame["action"].(string)
		skipped = append(skipped, action)
	}
}

type timeoutSkippedError struct{ skipped []string }

func (e *timeoutSkippedError) Error() string {
	return "没等到 __host_shutdown__ 控制帧，期间跳过的 action=" + strings.Join(e.skipped, ",")
}

// 不变量 1：后注册者顶掉前者时，前者必须先收到退出帧再被断开。
func TestEvictedHostReceivesExitFrameBeforeClose(t *testing.T) {
	srvURL, wired, closeSrv := newTakeoverServer(t, 8)
	defer closeSrv()

	reg := NewHostRegistry()
	const uid = uint(9101)
	first := hostTakeoverPair(t, reg, srvURL, wired, uid, 111)
	second := hostTakeoverPair(t, reg, srvURL, wired, uid, 222)
	_ = second

	reason, err := readShutdownFrame(t, first, 5*time.Second)
	if err != nil {
		t.Fatalf("被顶掉的连接应在关闭前收到退出帧: %v", err)
	}
	if reason != hostEvictReplaced {
		t.Errorf("退出原因应说明「被新注册顶掉」，got %q", reason)
	}
	if cur, ok := reg.GetConn(uid); !ok || cur.PID != 222 {
		t.Errorf("注册表应指向后注册者 pid=222，got %+v ok=%t", cur, ok)
	}
}

// 不变量 2：让位不等于「该用户没 Host 了」——断连清理钩子只能由真正失去连接那一次触发。
// 钩子会把该用户所有 running session 置 failed，误触发等于把还在跑的执行判死。
func TestEvictedConnDoesNotTriggerNoHostHook(t *testing.T) {
	srvURL, wired, closeSrv := newTakeoverServer(t, 8)
	defer closeSrv()

	reg := NewHostRegistry()
	hooked := make(chan uint, 8)
	reg.SetDisconnectHook(func(_ context.Context, userID uint) { hooked <- userID })
	const uid = uint(9102)
	first := hostTakeoverPair(t, reg, srvURL, wired, uid, 111)
	hostTakeoverPair(t, reg, srvURL, wired, uid, 222)

	if _, err := readShutdownFrame(t, first, 5*time.Second); err != nil {
		t.Fatalf("夹具失效：前者没收到退出帧: %v", err)
	}
	select {
	case uid := <-hooked:
		t.Errorf("前者只是让位，场上仍有 Host，不该触发「无 Host」清理钩子（user=%d）", uid)
	case <-time.After(700 * time.Millisecond):
	}

	// 反向对照：在场的那个真断了，钩子必须火——否则 running session 永久挂在 active。
	if cur, ok := reg.GetConn(uid); ok {
		cur.close()
	}
	select {
	case got := <-hooked:
		if got != uid {
			t.Errorf("钩子 userID=%d want %d", got, uid)
		}
	case <-time.After(2 * time.Second):
		t.Error("在场连接断开后应触发「无 Host」清理钩子")
	}
}

// 不变量 3：抢占抖动时保留在场连接、拒绝新来的。
// 阈值内（正常「换浏览器/重启」）仍是后注册者赢，两格都要开火，否则一条判据就能同时放行两种行为。
func TestTakeoverThrashKeepsIncumbent(t *testing.T) {
	srvURL, wired, closeSrv := newTakeoverServer(t, 16)
	defer closeSrv()

	reg := NewHostRegistry()
	const uid = uint(9103)

	// 前 hostRegThrashCount-1 次注册都按「新者赢」处理（未达抖动阈值不得提前改判）。
	clients := map[int]*websocket.Conn{}
	clients[1] = hostTakeoverPair(t, reg, srvURL, wired, uid, 1)
	for pid := 2; pid < hostRegThrashCount; pid++ {
		clients[pid] = hostTakeoverPair(t, reg, srvURL, wired, uid, pid)
		if _, err := readShutdownFrame(t, clients[pid-1], 5*time.Second); err != nil {
			t.Fatalf("第 %d 次注册未达抖动阈值，应顶掉前者并通知其退出: %v", pid, err)
		}
	}

	// 达到阈值的那一次：在场者保留，新来者被拒。
	incumbentPID := hostRegThrashCount - 1
	newcomer := hostTakeoverPair(t, reg, srvURL, wired, uid, 100+incumbentPID)
	if cur, ok := reg.GetConn(uid); !ok || cur.PID != incumbentPID {
		t.Errorf("抖动窗口内应保留在场连接 pid=%d，got %+v ok=%t", incumbentPID, cur, ok)
	}
	if _, err := readShutdownFrame(t, clients[incumbentPID], 300*time.Millisecond); err == nil {
		t.Error("在场连接被误逐：抖动判定没守住「保留在场者」这一半")
	}
	reason, err := readShutdownFrame(t, newcomer, 5*time.Second)
	if err != nil {
		t.Fatalf("被拒的新连接应收到退出帧: %v", err)
	}
	if reason != hostRefuseTakeoverThrash {
		t.Errorf("退出原因应说明「抢占抖动被拒」，got %q", reason)
	}
}
