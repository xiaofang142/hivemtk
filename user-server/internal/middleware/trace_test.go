package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/traceparent"
)

// collectSubscriber 把总线事件转成 channel，供测试按 trace_id 取回。
type collectSubscriber struct {
	ch chan llm.TraceEvent
}

func (s *collectSubscriber) OnEvent(event llm.TraceEvent) {
	select {
	case s.ch <- event:
	default:
	}
}

// TestTraceMiddlewarePerRequestEventFields 守卫每个请求发布的 trace 事件
// 只携带**自己那个请求**的 method/path/IP/UA。
//
// 坏形状（本轮 CI `-race` 首次真跑时命中）：SafeGo 闭包里直接读 `c.Request`、
// `c.Writer`、`c.ClientIP()`、`c.GetHeader()`。gin 的 *Context 来自 sync.Pool，
// 上一请求的闭包还在跑时，下一请求已经把这些字段重置成新值 ⇒ 既无同步（race），
// 也会把别人的 URL/IP 写进本条链路（跨请求串数据）。
// 因此本测试并发打 N 个各不相同的请求，再逐条比对事件字段与发起时的期望值。
func TestTraceMiddlewarePerRequestEventFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const n = 32

	bus := llm.InitGlobalTraceBus()
	events := make(chan llm.TraceEvent, 512)
	bus.Subscribe(&collectSubscriber{ch: events})

	engine := gin.New()
	engine.Use(TraceMiddleware())
	engine.Any("/probe/:n", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	type want struct {
		method string
		path   string
		ip     string
		ua     string
	}
	wants := make(map[string]want, n)

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		traceID := fmt.Sprintf("%032x", i)
		method := methods[i%len(methods)]
		path := fmt.Sprintf("/probe/%d", i)
		ip := fmt.Sprintf("10.%d.%d.%d", i/256, i%256, i)
		ua := fmt.Sprintf("agent-%d", i)

		wants[traceID] = want{method: method, path: path, ip: ip, ua: ua}

		req := httptest.NewRequest(method, path, nil)
		req.Header.Set(traceparent.HeaderName, fmt.Sprintf("00-%s-%016x-01", traceID, i))
		req.Header.Set("User-Agent", ua)
		req.RemoteAddr = ip + ":43210"

		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()

	got := make(map[string]llm.TraceEvent, n)
	deadline := time.After(3 * time.Second)
	for len(got) < n {
		select {
		case ev := <-events:
			if _, ok := wants[ev.TraceID]; ok && ev.Service == "http" {
				got[ev.TraceID] = ev
			}
		case <-deadline:
			t.Fatalf("超时：期望 %d 条 http 事件，实收 %d 条", n, len(got))
		}
	}

	for traceID, w := range wants {
		ev, ok := got[traceID]
		if !ok {
			t.Fatalf("trace %s 没有对应事件", traceID)
		}
		if ev.Operation != w.method+" "+w.path {
			t.Errorf("跨请求串数据：trace %s Operation=%q，期望 %q", traceID, ev.Operation, w.method+" "+w.path)
		}
		if metaPath, _ := ev.Metadata["path"].(string); metaPath != w.path {
			t.Errorf("跨请求串数据：trace %s metadata.path=%q，期望 %q", traceID, metaPath, w.path)
		}
		if metaMethod, _ := ev.Metadata["method"].(string); metaMethod != w.method {
			t.Errorf("跨请求串数据：trace %s metadata.method=%q，期望 %q", traceID, metaMethod, w.method)
		}
		if metaIP, _ := ev.Metadata["client_ip"].(string); metaIP != w.ip {
			t.Errorf("跨请求串数据：trace %s metadata.client_ip=%q，期望 %q", traceID, metaIP, w.ip)
		}
		if metaUA, _ := ev.Metadata["user_agent"].(string); metaUA != w.ua {
			t.Errorf("跨请求串数据：trace %s metadata.user_agent=%q，期望 %q", traceID, metaUA, w.ua)
		}
		if trace, _ := ev.Metadata["w3c_traceparent"].(bool); !trace {
			t.Errorf("trace %s 应记录 w3c_traceparent=true（本测试每个请求都带了该头）", traceID)
		}
	}
}
