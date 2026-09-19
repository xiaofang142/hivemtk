package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 批9 生产路径锁（Go 侧）：tab 作用域原语必须把 tab_id 发到线上。
// 扩展侧的 ref→selector 映射从批9 起按 tab 分桶，tab_id 缺失/错位不再是「无所谓的一个字段」，
// 而是会让解析器查错桶、把 A 页定位用到 B 页上——所以这一层要锁在帧格式上，
// 而不是只锁在 JS 单测里（JS 侧看到的 tab_id 是 Go 发出来的，两边各锁一段才闭环）。

type capturedFrame struct {
	action string
	frame  map[string]any
}

// wireCapture 只起 WS 一层的假扩展（不接 DB）：帧格式锁不该被测试库可用性挡住。
func wireCapture(t *testing.T, reply func(f capturedFrame) map[string]any) (*Hand, func() []capturedFrame) {
	t.Helper()

	var mu sync.Mutex
	var got []capturedFrame

	reg := NewHostRegistry()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wired := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("升级失败: %v", err)
			return
		}
		wired <- c
	}))
	t.Cleanup(srv.Close)

	dialer := websocket.Dialer{}
	clientConn, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/host-ws", nil)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	serverConn := <-wired
	const user = uint(9501)
	reg.Register(user, "1.5.0-wire", os.Getpid(), serverConn)

	go func() {
		var writeMu sync.Mutex
		for {
			var frame map[string]any
			if err := clientConn.ReadJSON(&frame); err != nil {
				return
			}
			action, _ := frame["action"].(string)
			mu.Lock()
			got = append(got, capturedFrame{action: action, frame: frame})
			mu.Unlock()

			var data map[string]any
			if reply != nil {
				data = reply(capturedFrame{action: action, frame: frame})
			}
			reqID, _ := frame["req_id"].(string)
			writeMu.Lock()
			err := clientConn.WriteJSON(map[string]any{"req_id": reqID, "ok": true, "data": data})
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	return NewHand(reg), func() []capturedFrame {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedFrame(nil), got...)
	}
}

func lastFrame(frames []capturedFrame, action string) (map[string]any, bool) {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].action == action {
			return frames[i].frame, true
		}
	}
	return nil, false
}

// tab_id 在 JSON 里过一遍无线，回到 Go 侧必须是数值型且等于调用方传入的 tab。
func assertTabID(t *testing.T, frame map[string]any, want int) {
	t.Helper()
	v, ok := frame["tab_id"]
	if !ok {
		t.Fatalf("帧缺少 tab_id：%v（扩展侧 ref 按 tab 分桶，缺 tab_id 会查错桶）", frame)
	}
	n, ok2 := toInt(v)
	if !ok2 || n != want {
		t.Fatalf("tab_id=%#v 期望 %d（帧=%v）", v, want, frame)
	}
}

func TestHandResolveRefFrameCarriesTabID(t *testing.T) {
	hand, frames := wireCapture(t, func(f capturedFrame) map[string]any {
		if f.action == "resolve_ref" {
			return map[string]any{"selector": "#real-btn"}
		}
		return map[string]any{}
	})

	sel, err := hand.resolveRef(context.Background(), 9501, 4242, "@e7")
	if err != nil {
		t.Fatalf("resolveRef 失败: %v", err)
	}
	if sel != "#real-btn" {
		t.Fatalf("selector=%q 期望 #real-btn", sel)
	}
	frame, ok := lastFrame(frames(), "resolve_ref")
	if !ok {
		t.Fatal("扩展侧没收到 resolve_ref 帧")
	}
	assertTabID(t, frame, 4242)
	if got, _ := frame["ref"].(string); got != "@e7" {
		t.Fatalf("ref=%v 期望 @e7", frame["ref"])
	}
}

func TestHandTabScopedPrimitivesCarryTabID(t *testing.T) {
	hand, frames := wireCapture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const tab = 777

	if _, err := hand.click(ctx, 9501, tab, "#send"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if err := hand.typeText(ctx, 9501, tab, "#input", "hi", true, false); err != nil {
		t.Fatalf("typeText: %v", err)
	}
	if _, _, err := hand.snapshot(ctx, 9501, tab); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	got := frames()
	for _, action := range []string{"click", "type", "snapshot"} {
		frame, ok := lastFrame(got, action)
		if !ok {
			t.Fatalf("扩展侧没收到 %s 帧（实收=%s）", action, actionsOf(got))
		}
		assertTabID(t, frame, tab)
	}
}

func actionsOf(frames []capturedFrame) string {
	parts := make([]string, 0, len(frames))
	for _, f := range frames {
		parts = append(parts, f.action)
	}
	return strings.Join(parts, ",")
}
