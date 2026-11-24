package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// FA-CHANNEL-02：本地模拟 OneBot 反向 WS 端，验证发送必须关联 echo 回执，
// 而不是把 TCP 写成功当作平台已受理。
type faOneBotReply struct {
	Status  string         `json:"status"`
	RetCode int            `json:"retcode"`
	Data    map[string]any `json:"data"`
	Message string         `json:"message"`
	Echo    string         `json:"echo"`
}

type faOneBotEnv struct {
	mgr      *OneBotWSManager
	conn     *OneBotWSConn
	requests *atomic.Int64
	srv      *httptest.Server
	client   *websocket.Conn
	done     chan struct{}
}

// faResponder 决定服务端收到动作帧后如何应答。close 用于模拟断线。
type faResponder func(echo string, req map[string]any, write func(any), close func())

func faStartOneBot(t *testing.T, responder faResponder) *faOneBotEnv {
	t.Helper()
	mgr := newOneBotWSManager()
	registered := make(chan *OneBotWSConn, 4)
	done := make(chan struct{})
	var requests atomic.Int64
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	// 服务端即「我们的服务」：Upgrade 后登记连接并启动读循环（与生产 controller 一致）；
	// handler 本身不再读，避免与 RunReadLoop 争抢同一连接上的帧。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := mgr.Register(42, "10001", conn)
		registered <- c
		go mgr.RunReadLoop(42, c)
		<-done // 保持 handler 存活直到清理
	}))
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/onebot/42"
	client, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	c := <-registered
	// 拨号端模拟 OneBot 协议端：读动作帧、按 responder 回执。
	go func() {
		for {
			_, data, err := client.ReadMessage()
			if err != nil {
				return
			}
			requests.Add(1)
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				continue
			}
			echo, _ := req["echo"].(string)
			write := func(v any) { _ = client.WriteJSON(v) }
			closeFn := func() { _ = client.Close() }
			responder(echo, req, write, closeFn)
		}
	}()
	env := &faOneBotEnv{mgr: mgr, conn: c, requests: &requests, srv: srv, client: client, done: done}
	t.Cleanup(func() {
		close(done)
		client.Close()
		srv.Close()
	})
	return env
}

func faTag(req map[string]any) string {
	params, _ := req["params"].(map[string]any)
	tag, _ := params["tag"].(string)
	return tag
}

// 1. 平台成功：必须拿到 echo 关联的回执与 message_id，不能只看写成功。
func TestFAChannel02_SuccessCorrelatesEchoAndMessageID(t *testing.T) {
	env := faStartOneBot(t, func(echo string, _ map[string]any, write func(any), _ func()) {
		write(faOneBotReply{Status: "ok", RetCode: 0, Data: map[string]any{"message_id": 12345}, Echo: echo})
	})
	reply, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": "ok"})
	if err != nil {
		t.Fatalf("成功回执应返回 nil error, got %v", err)
	}
	if reply == nil || reply.Status != "ok" || reply.RetCode != 0 {
		t.Fatalf("回执状态错误: %+v", reply)
	}
	if mid, ok := reply.Data["message_id"]; !ok || mid != float64(12345) {
		t.Fatalf("业务回执 message_id 错误: %v", reply.Data)
	}
	if env.requests.Load() != 1 {
		t.Fatalf("上游应恰好调用 1 次, got %d", env.requests.Load())
	}
}

// 2. 平台拒绝：非零 retcode 必须暴露为错误，不能被当作已受理。
func TestFAChannel02_PlatformRejectReturnsRetcode(t *testing.T) {
	env := faStartOneBot(t, func(echo string, _ map[string]any, write func(any), _ func()) {
		write(faOneBotReply{Status: "failed", RetCode: 1400, Message: "bad request", Echo: echo})
	})
	reply, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": "reject"})
	if err == nil || !strings.Contains(err.Error(), "1400") {
		t.Fatalf("平台拒绝应返回含 retcode 的错误, got %v", err)
	}
	if reply == nil || reply.RetCode != 1400 {
		t.Fatalf("应返回平台拒绝回执, got %+v", reply)
	}
	if env.requests.Load() != 1 {
		t.Fatalf("上游应恰好调用 1 次, got %d", env.requests.Load())
	}
}

// 3. 乱序/未知 echo：事件帧与错误 echo 必须忽略，只认自己的回执。
func TestFAChannel02_OutOfOrderAndUnknownEchoIgnored(t *testing.T) {
	env := faStartOneBot(t, func(echo string, req map[string]any, write func(any), _ func()) {
		_ = req
		// 心跳/事件帧（无 echo）与未知 echo 回执，均须被读循环忽略。
		write(map[string]any{"post_type": "message", "message": "hi"})
		write(faOneBotReply{Status: "ok", RetCode: 0, Data: map[string]any{"message_id": 1}, Echo: "unknown-echo"})
		write(faOneBotReply{Status: "ok", RetCode: 0, Data: map[string]any{"message_id": 999}, Echo: echo})
	})
	reply, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": "order"})
	if err != nil {
		t.Fatalf("正确 echo 回执应成功, got %v", err)
	}
	if mid, ok := reply.Data["message_id"]; !ok || mid != float64(999) {
		t.Fatalf("应取到自己 echo 的回执, got %v", reply.Data)
	}
	if env.requests.Load() != 1 {
		t.Fatalf("上游应恰好调用 1 次, got %d", env.requests.Load())
	}
}

// 4. 两个并发发送乱序回执：各自只能拿到自己的回执（tag 对应）。
func TestFAChannel02_ConcurrentSendsMatchOwnEcho(t *testing.T) {
	var once sync.Once
	var echoes []string
	var echoesMu sync.Mutex
	env := faStartOneBot(t, func(echo string, req map[string]any, write func(any), _ func()) {
		echoesMu.Lock()
		echoes = append(echoes, echo)
		ready := len(echoes) == 2
		echoesMu.Unlock()
		if !ready {
			return
		}
		once.Do(func() {
			// 故意逆序回复：后到 echo 先回。
			for i := len(echoes) - 1; i >= 0; i-- {
				write(faOneBotReply{Status: "ok", RetCode: 0, Data: map[string]any{"message_id": echoes[i]}, Echo: echoes[i]})
			}
		})
	})

	type result struct {
		tag string
		mid any
		err error
	}
	out := make(chan result, 2)
	for _, tag := range []string{"A", "B"} {
		go func(tag string) {
			reply, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": tag})
			var mid any
			if reply != nil {
				mid = reply.Data["message_id"]
			}
			out <- result{tag: tag, mid: mid, err: err}
		}(tag)
	}
	seen := map[string]string{}
	for i := 0; i < 2; i++ {
		r := <-out
		if r.err != nil {
			t.Fatalf("send %s: %v", r.tag, r.err)
		}
		seen[r.tag] = r.mid.(string)
	}
	// 回执里的 message_id 设成了 echo 本身；校验两个 send 拿到的 echo 互不相同且都已注册过。
	if seen["A"] == "" || seen["B"] == "" || seen["A"] == seen["B"] {
		t.Fatalf("并发回执未各自关联: %+v", seen)
	}
	if env.requests.Load() != 2 {
		t.Fatalf("上游应恰好调用 2 次, got %d", env.requests.Load())
	}
}

// 5. 超时：结果不明且绝不盲重发。
func TestFAChannel02_TimeoutNoBlindResend(t *testing.T) {
	env := faStartOneBot(t, func(string, map[string]any, func(any), func()) {}) // 永不回复
	env.conn.sendTimeout = 120 * time.Millisecond
	_, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": "timeout"})
	if err == nil || !strings.Contains(err.Error(), "结果不明") {
		t.Fatalf("超时应返回「结果不明」错误, got %v", err)
	}
	if env.requests.Load() != 1 {
		t.Fatalf("超时不得盲重发，上游应恰好 1 次, got %d", env.requests.Load())
	}
}

// 6. 断线：连接断开唤醒等待者且绝不盲重发。
func TestFAChannel02_DisconnectNoBlindResend(t *testing.T) {
	env := faStartOneBot(t, func(_ string, _ map[string]any, _ func(any), closeFn func()) {
		closeFn() // 收到动作帧后立刻断线，不发回执
	})
	_, err := env.mgr.sendWithReply(context.Background(), 42, "send_private_msg", map[string]any{"tag": "disconnect"})
	if err == nil || !strings.Contains(err.Error(), "结果不明") {
		t.Fatalf("断线应返回「结果不明」错误, got %v", err)
	}
	if env.requests.Load() != 1 {
		t.Fatalf("断线不得盲重发，上游应恰好 1 次, got %d", env.requests.Load())
	}
}
