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

// TestOneBotWSManagerReconnectReplaces 同 self_id 重连必须顶掉旧连接，
// 且 Unregister 只能移除表内当前连接（旧连接断开不能误删新连接）。
func TestOneBotWSManagerReconnectReplaces(t *testing.T) {
	mgr := newOneBotWSManager()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registered := make(chan *OneBotWSConn, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		var id uint
		if strings.Contains(r.URL.Path, "2") {
			id = 2
		} else {
			id = 1
		}
		registered <- mgr.Register(id, "10001", conn)
	}))
	defer srv.Close()

	dial := func(id string) *websocket.Conn {
		u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/" + id
		c, _, err := websocket.DefaultDialer.Dial(u, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}

	first := dial("1")
	defer first.Close()
	c1 := <-registered
	if !mgr.Connected(1) {
		t.Fatal("首个连接未登记")
	}

	second := dial("2")
	defer second.Close()
	<-registered
	if mgr.Connected(1) {
		t.Fatal("同 self_id 重连后旧账号连接应被顶掉")
	}
	if !mgr.Connected(2) {
		t.Fatal("新连接未登记")
	}
	// 旧连接应已被服务端关闭：读会很快出错。
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("旧连接未被关闭")
	}
	// 旧连接迟到注销不得删掉新连接。
	mgr.Unregister(1, c1)
	if !mgr.Connected(2) {
		t.Fatal("旧连接注销误删了新连接")
	}
}

func TestOneBotWSSendUnconnected(t *testing.T) {
	mgr := newOneBotWSManager()
	if err := mgr.Send(context.Background(), 9, "send_private_msg", map[string]any{"user_id": 1}); err == nil {
		t.Fatal("未连接时应返回错误以便回落 HTTP")
	}
}
