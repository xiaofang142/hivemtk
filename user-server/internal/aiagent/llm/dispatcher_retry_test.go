package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newRetryDispatcher 装一个只指向 mock 端点的 dispatcher（与 maxtokens 基线用例同一装配口）。
func newRetryDispatcher(t *testing.T, baseURL string) (*Dispatcher, *ProviderConfig) {
	t.Helper()
	d := newDispatcherBase(NewLLMService())
	p := &ProviderConfig{
		Name: "retrytest", BaseURL: baseURL, APIType: "openai",
		Model: "test-model", Enabled: true, APIKey: "x",
	}
	d.providers[p.Name] = p
	return d, p
}

// TestDispatcherRetriesTransientServerError 守护"云端网关瞬时 5xx 不该一句话就把客户晾着"。
// 实测 sensenova 回过一次 522，因为 dispatcher 把 MaxRetries 写死成 1（同包
// GetDefaultConfig 用的是 3），一次抖动就直接降级成"抱歉，AI 服务暂时不可用"落到客户脸上。
func TestDispatcherRetriesTransientServerError(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"正常回复"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer srv.Close()

	d, p := newRetryDispatcher(t, srv.URL)
	result, err := d.CallProviderForTest(context.Background(), p, DispatchRequest{
		Scenario: ScenarioSOPReply, Prompt: "hello",
	}, &ScenarioRoute{Scenario: ScenarioSOPReply, Provider: p.Name, MaxLatency: 20000})
	if err != nil {
		t.Fatalf("瞬时 5xx 应通过重试自愈，实际 err=%v", err)
	}
	if result == nil || result.Content != "正常回复" {
		t.Fatalf("重试后应拿到正常回复, 实际 %+v", result)
	}
	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 2 {
		t.Fatalf("期望 1 次失败 + 1 次重试成功 = 2 次请求, 实际 %d", got)
	}
}

// TestDispatcherUnreachableFailsFast 是上一例的边界：重试是给瞬时故障的，
// 端口拒绝连接（本地推理栈没起）不能也退避三次，否则每个请求白等 3s。
func TestDispatcherUnreachableFailsFast(t *testing.T) {
	d, p := newRetryDispatcher(t, "http://"+refusedAddr(t)+"/v1")

	start := time.Now()
	_, err := d.CallProviderForTest(context.Background(), p, DispatchRequest{
		Scenario: ScenarioSOPReply, Prompt: "hello",
	}, &ScenarioRoute{Scenario: ScenarioSOPReply, Provider: p.Name, MaxLatency: 20000})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("端点不可达时应返回错误")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("端口拒绝连接却仍在退避重试：耗时 %.1fs（应 <3s 快速失败）", elapsed.Seconds())
	}
}
