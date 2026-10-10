package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 两个已启用的候选 provider 指向同一个计数端点：fan-out 至少要 2 个候选才会进入分派。
func newFanOutTestDispatcher(srv *httptest.Server) *Dispatcher {
	d := newDispatcherBase(NewLLMService())
	for _, name := range []string{"alpha", "beta"} {
		n := name
		d.providers[n] = &ProviderConfig{
			Name:    n,
			BaseURL: srv.URL,
			APIType: "openai",
			Model:   "fanout-test-model",
			Enabled: true,
			APIKey:  "x",
		}
	}
	return d
}

func newHitCountingServer(hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
}

// 非法 strategy 必须「零次 provider 调用」就报错：校验排在并发发起之前。
// 修好前这段是真金白银的浪费——两路请求打完（计费 + 吃限流额度 + 等 timeout），
// 结果全部丢弃后才报 not implemented。
func TestFanOutIllegalStrategyRejectedBeforeDispatch(t *testing.T) {
	var hits int32
	srv := newHitCountingServer(&hits)
	defer srv.Close()

	d := newFanOutTestDispatcher(srv)
	route := &ScenarioRoute{Scenario: ScenarioHighQuality, Provider: "alpha", MaxLatency: 10000}
	req := DispatchRequest{
		Scenario:    ScenarioHighQuality,
		Messages:    []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens:   16,
		Temperature: 0.3,
		FanOut:      &FanOutConfig{Enable: true, Strategy: "weighted-majority", Timeout: 2 * time.Second},
	}

	start := time.Now()
	res, err := d.dispatchFanOut(context.Background(), req, route, []string{"alpha", "beta"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("非法 strategy 应返回错误，实际 res=%v err=<nil>", res)
	}
	if res != nil {
		t.Errorf("非法 strategy 不应产出结果，实际=%+v", res)
	}
	// 错误信息要同时点名「传进来的值」和「可用名单」，否则调用方拿到一句无从下手的话。
	if !strings.Contains(err.Error(), "weighted-majority") {
		t.Errorf("错误信息应含非法策略名，实际=%q", err.Error())
	}
	for _, want := range fanOutStrategies {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应列出已实现策略 %q，实际=%q", want, err.Error())
		}
	}
	// 报错不该等满 Timeout；等满说明走的是"两路打完再丢弃"的旧路径。
	if elapsed >= 2*time.Second {
		t.Errorf("非法策略耗时 %v，接近 Timeout，疑似校验后置于并发发起", elapsed)
	}
	// 端点计数只作次级信号：校验缺失时 fanCtx 会在返回时立即取消，
	// 在途请求多半连不上端点，所以 hits 为 0 并不单独证明"没发起"——
	// "排在发起之前"由下面的源码顺序锁判定。
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("准入校验未挡住发起：provider 端点被打了 %d 次，期望 0", got)
	}
}

// 顺序锁：准入校验必须在并发发起的 go 语句之前。这条判据是源码级的，
// 因为运行期无法稳定区分"没发起"与"发起了但被 fanCtx 取消"。
func TestFanOutAdmissionPrecedesDispatch(t *testing.T) {
	src, err := os.ReadFile("dispatcher.go")
	if err != nil {
		t.Fatalf("读取 dispatcher.go 失败: %v", err)
	}
	body := string(src)
	admission := strings.Index(body, "if !fanOutStrategyImplemented(strategy) {")
	firstGo := strings.Index(body, "go func(idx int, prov *ProviderConfig) {")
	if admission < 0 {
		t.Fatal("找不到准入校验语句：fan-out 非法策略的门不见了")
	}
	if firstGo < 0 {
		t.Fatal("找不到并发发起语句：dispatchFanOut 的形态变了，顺序锁需要一并更新")
	}
	if admission > firstGo {
		t.Errorf("准入校验(偏移 %d)排在并发发起(偏移 %d)之后：非法策略会先产生真实调用与费用", admission, firstGo)
	}
}

// 反向对账：合法策略仍然照常发起，准入校验没有把门堵死。
func TestFanOutLegalStrategiesStillDispatch(t *testing.T) {
	for _, strategy := range fanOutStrategies {
		t.Run(strategy, func(t *testing.T) {
			var hits int32
			srv := newHitCountingServer(&hits)
			defer srv.Close()

			d := newFanOutTestDispatcher(srv)
			route := &ScenarioRoute{Scenario: ScenarioHighQuality, Provider: "alpha", MaxLatency: 10000}
			req := DispatchRequest{
				Scenario:    ScenarioHighQuality,
				Messages:    []ChatMessage{{Role: "user", Content: "hi"}},
				MaxTokens:   16,
				Temperature: 0.3,
				FanOut:      &FanOutConfig{Enable: true, Strategy: strategy, Timeout: 5 * time.Second},
			}
			res, err := d.dispatchFanOut(context.Background(), req, route, []string{"alpha", "beta"})
			if err != nil {
				t.Fatalf("strategy=%s 期望成功，实际 err=%v", strategy, err)
			}
			if res.Content != "ok" {
				t.Errorf("strategy=%s 回复内容=%q，期望来自 mock 端点的 ok", strategy, res.Content)
			}
			if got := atomic.LoadInt32(&hits); got == 0 {
				t.Errorf("strategy=%s 一次 provider 都没打，准入校验疑似把合法值也挡了", strategy)
			}
		})
	}
}

// 未填 strategy 时走默认值 fastest，不能被准入校验当成非法值。
func TestFanOutEmptyStrategyDefaultsToFastest(t *testing.T) {
	var hits int32
	srv := newHitCountingServer(&hits)
	defer srv.Close()

	d := newFanOutTestDispatcher(srv)
	route := &ScenarioRoute{Scenario: ScenarioHighQuality, Provider: "alpha", MaxLatency: 10000}
	req := DispatchRequest{
		Scenario:    ScenarioHighQuality,
		Messages:    []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens:   16,
		Temperature: 0.3,
		FanOut:      &FanOutConfig{Enable: true, Timeout: 5 * time.Second},
	}
	if _, err := d.dispatchFanOut(context.Background(), req, route, []string{"alpha", "beta"}); err != nil {
		t.Fatalf("空 strategy 应回落 fastest，实际 err=%v", err)
	}
	if got := atomic.LoadInt32(&hits); got == 0 {
		t.Error("空 strategy 未发起任何 provider 调用")
	}
}

// fanOutStrategies 名单本身的两条不变量：名单里没有空串（否则默认值会被自己判非法），
// 且每个名单项都能在 dispatchFanOut 里走到实现分支（新加策略忘了实现时这里先红）。
func TestFanOutStrategyListShape(t *testing.T) {
	if len(fanOutStrategies) == 0 {
		t.Fatal("策略名单为空")
	}
	seen := make(map[string]bool, len(fanOutStrategies))
	for _, s := range fanOutStrategies {
		if s == "" {
			t.Error("策略名单含空串：空 strategy 的默认值回落会被自己的准入校验判非法")
		}
		if seen[s] {
			t.Errorf("策略名单含重复项 %q", s)
		}
		seen[s] = true
		if !fanOutStrategyImplemented(s) {
			t.Errorf("%q 在名单里但 implemented 判定为 false", s)
		}
	}
}

// DispatchRequest 的 FanOut 是 JSON 可反序列化的（strategy 属外部可传字段），
// 锁住解码后的非法值同样被准入校验挡住——这条走 Dispatch 全链路。
func TestFanOutStrategyJSONDecodeRejected(t *testing.T) {
	var req DispatchRequest
	body := []byte(`{"fanout":{"enable":true,"strategy":"quorum","timeout":1000000000}}`)
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("FanOutConfig JSON 解码失败: %v", err)
	}
	if req.FanOut == nil || req.FanOut.Strategy != "quorum" {
		t.Fatalf("JSON 解码结果不符合预期: %+v", req.FanOut)
	}
	if fanOutStrategyImplemented(req.FanOut.Strategy) {
		t.Errorf("外部可传字段解码出的 %q 必须在名单之外", req.FanOut.Strategy)
	}
}
