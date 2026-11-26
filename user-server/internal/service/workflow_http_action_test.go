package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// newCountingReceiver 本地计数接收器：记录每次请求的方法/路径/头/体并统计命中次数。
type countingReceiver struct {
	server *httptest.Server
	hits   atomic.Int64
	muBody chan string
	last   chan *http.Request
}

func newCountingReceiver(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *countingReceiver {
	t.Helper()
	rc := &countingReceiver{}
	rc.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc.hits.Add(1)
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(rc.server.Close)
	return rc
}

func httpActionCtx(execID uint, cfg model.JSONMap) (*WorkflowExecContext, *model.WorkflowExecution) {
	exec := &model.WorkflowExecution{ID: execID, Context: model.JSONMap{}}
	return &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "http-node",
		NodeType:   WorkflowNodeTypeAction,
		NodeConfig: cfg,
		Context:    exec.Context,
	}, exec
}

func runHTTPAction(t *testing.T, cfg model.JSONMap) *WorkflowNodeExecResult {
	t.Helper()
	wctx, _ := httpActionCtx(9001, cfg)
	e := &ActionNodeExecutor{}
	return e.executeHTTPAction(context.Background(), wctx, WorkflowSideEffectKey(wctx, "action_http"))
}

func TestActionHTTP_200CompletesSendsBodyAndCountsOnce(t *testing.T) {
	var gotMethod, gotPath, gotBody, gotCT string
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"done":true}`))
	})

	wctx, _ := httpActionCtx(9002, model.JSONMap{
		"url":     rc.server.URL + "/hook",
		"method":  "POST",
		"body":    `{"event":"hello"}`,
		"headers": map[string]any{"X-Trace": "tr-1"},
	})
	e := &ActionNodeExecutor{}
	res := e.executeHTTPAction(context.Background(), wctx, WorkflowSideEffectKey(wctx, "action_http"))

	if res.Status != NodeStatusCompleted {
		t.Fatalf("期望 completed，得到 %s（err=%s）", res.Status, res.ErrorMessage)
	}
	if rc.hits.Load() != 1 {
		t.Fatalf("期望接收器恰好命中 1 次，得到 %d", rc.hits.Load())
	}
	if gotMethod != "POST" || gotPath != "/hook" || gotBody != `{"event":"hello"}` {
		t.Fatalf("请求未被如实送达：method=%s path=%s body=%s", gotMethod, gotPath, gotBody)
	}
	if gotCT != "application/json" {
		t.Errorf("期望默认 Content-Type=application/json，得到 %s", gotCT)
	}
	if sc, _ := res.Output["status_code"].(int); sc != 200 {
		t.Errorf("期望输出 status_code=200，得到 %v", res.Output["status_code"])
	}
	if body, _ := res.Output["response_body"].(string); !strings.Contains(body, `"done"`) {
		t.Errorf("期望响应体回读，得到 %q", body)
	}
	found := false
	for _, ef := range res.SideEffects {
		if strings.HasPrefix(ef, "http_called:") {
			found = true
		}
	}
	if !found {
		t.Errorf("成功后应记录 http_called 副作用，得到 %v", res.SideEffects)
	}
}

func TestActionHTTP_Server500RetryableAndReplayResends(t *testing.T) {
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	wctx, _ := httpActionCtx(9003, model.JSONMap{"url": rc.server.URL})
	e := &ActionNodeExecutor{}
	res := e.executeHTTPAction(context.Background(), wctx, WorkflowSideEffectKey(wctx, "action_http"))
	if res.Status != NodeStatusFailed || !res.Retryable {
		t.Fatalf("期望 failed+retryable，得到 %s retryable=%v", res.Status, res.Retryable)
	}
	if rc.hits.Load() != 1 {
		t.Fatalf("期望 1 次请求，得到 %d", rc.hits.Load())
	}
	if sc, _ := res.Output["status_code"].(int); sc != 500 {
		t.Errorf("失败输出应含 status_code=500，得到 %v", res.Output["status_code"])
	}
	// dispatcher 重试=节点重新执行：失败未落副作用键，重放必须真实再发。
	res2 := e.executeHTTPAction(context.Background(), wctx, WorkflowSideEffectKey(wctx, "action_http"))
	if res2.Status != NodeStatusFailed || !res2.Retryable {
		t.Fatalf("重放期望 failed+retryable，得到 %s", res2.Status)
	}
	if rc.hits.Load() != 2 {
		t.Fatalf("重放后期望接收器命中 2 次，得到 %d", rc.hits.Load())
	}
}

func TestActionHTTP_Client400NonRetryable(t *testing.T) {
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	res := runHTTPAction(t, model.JSONMap{"url": rc.server.URL})
	if res.Status != NodeStatusFailed || res.Retryable {
		t.Fatalf("4xx 期望 failed+不可重试，得到 %s retryable=%v", res.Status, res.Retryable)
	}
}

func TestActionHTTP_MissingURLRejectedWithoutRequest(t *testing.T) {
	rc := newCountingReceiver(t, nil)
	res := runHTTPAction(t, model.JSONMap{})
	if res.Status != NodeStatusFailed || res.Retryable {
		t.Fatalf("缺 URL 期望 failed+不可重试，得到 %s", res.Status)
	}
	if u, _ := res.Output["url"].(string); strings.Contains(u, "http://placeholder") {
		t.Errorf("不允许回退 placeholder URL：%q", u)
	}
	if res.Output != nil && res.Output["_mock"] == true {
		t.Errorf("不得再以 _mock 作为完成证据：%v", res.Output)
	}
	if rc.hits.Load() != 0 {
		t.Fatalf("缺 URL 不得外发任何请求，命中=%d", rc.hits.Load())
	}
}

func TestActionHTTP_NonHTTPSchemeRejected(t *testing.T) {
	for _, u := range []string{"ftp://example.com/x", "file:///etc/passwd", "/relative/only", "http://"} {
		res := runHTTPAction(t, model.JSONMap{"url": u})
		if res.Status != NodeStatusFailed || res.Retryable {
			t.Fatalf("URL %q 期望 failed+不可重试，得到 %s", u, res.Status)
		}
	}
}

func TestActionHTTP_TimeoutRetryable(t *testing.T) {
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	res := runHTTPAction(t, model.JSONMap{"url": rc.server.URL, "timeout_ms": 50})
	if res.Status != NodeStatusFailed || !res.Retryable {
		t.Fatalf("超时期望 failed+retryable，得到 %s retryable=%v", res.Status, res.Retryable)
	}
}

func TestActionHTTP_CancelledNonRetryable(t *testing.T) {
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wctx, _ := httpActionCtx(9004, model.JSONMap{"url": rc.server.URL})
	e := &ActionNodeExecutor{}
	res := e.executeHTTPAction(ctx, wctx, WorkflowSideEffectKey(wctx, "action_http"))
	if res.Status != NodeStatusFailed || res.Retryable {
		t.Fatalf("取消期望 failed+不可重试，得到 %s retryable=%v", res.Status, res.Retryable)
	}
}

func TestActionHTTP_ResponseBodyCapped(t *testing.T) {
	big := fmt.Sprintf(`{"blob":"%s"}`, strings.Repeat("x", 200_000))
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	})
	res := runHTTPAction(t, model.JSONMap{"url": rc.server.URL})
	if res.Status != NodeStatusCompleted {
		t.Fatalf("期望 completed，得到 %s（err=%s）", res.Status, res.ErrorMessage)
	}
	if body, _ := res.Output["response_body"].(string); len(body) > httpActionOutputBodyLimit+20 {
		t.Fatalf("落库响应体未截断：len=%d", len(body))
	}
}

func TestActionHTTP_MethodAndBodyJSON(t *testing.T) {
	var gotMethod, gotBody string
	rc := newCountingReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		gotBody, _ = m["k"].(string)
		w.WriteHeader(204)
	})
	res := runHTTPAction(t, model.JSONMap{"url": rc.server.URL, "method": "PUT", "body_json": map[string]any{"k": "v"}})
	if res.Status != NodeStatusCompleted {
		t.Fatalf("期望 completed，得到 %s（err=%s）", res.Status, res.ErrorMessage)
	}
	if gotMethod != "PUT" || gotBody != "v" {
		t.Fatalf("PUT+body_json 未如实送达：method=%s body=%s", gotMethod, gotBody)
	}
}
