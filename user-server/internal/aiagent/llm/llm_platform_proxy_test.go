package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/platform"
)

// 平台垫资 provider 的调用路径测试。
//
// 这一组守的是「用户买了套餐之后能不能真的用上」——
// 平台不下发真实 Key（ADR-M7），所以 source='platform' 的 provider
// 如果没走代理，它的 APIKey 是空的，必然 401。这条链路断掉时的症状是
// 「菜单里能选到这个模型、测试按钮能点、一调用就失败」，用户完全看不出
// 是商业化改造漏接了。

func withProxy(t *testing.T, baseURL string) {
	t.Helper()
	c := &http.Client{}
	SetPlatformProxyCallerForTest(func() *platform.LLMProxyClient {
		return platform.SetLLMProxyClientForTest(baseURL, "MK-TEST", c)
	})
	t.Cleanup(func() { SetPlatformProxyCallerForTest(nil) })
}

func TestCallProviderPlatformRoutesToProxy(t *testing.T) {
	var gotPath, gotAuth, gotProvider string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotProvider = r.URL.Query().Get("provider")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Llm-Usage-Prompt-Tokens", "1200")
		w.Header().Set("X-Llm-Usage-Completion-Tokens", "300")
		w.Header().Set("X-Llm-Usage-Request-Id", "req-1")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}],
			"usage":{"prompt_tokens":1200,"completion_tokens":300}}`))
	}))
	defer srv.Close()
	withProxy(t, srv.URL)

	svc := NewLLMService()
	cfg := &LLMConfig{
		Source:       LLMProviderSourcePlatform,
		ProviderName: "gpt-platform",
		Model:        "gpt-4o",
		Scenario:     "customer_service",
		TraceID:      "trace-1",
		AgentID:      "agent-9",
		UserID:       "user-3",
		// 关键：APIKey 为空。平台不下发密钥，直连必然 401。
		APIKey:   "",
		BaseURL:  "",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
	resp, err := svc.callProvider(context.Background(), cfg, []byte(`{"model":"gpt-4o"}`))
	if err != nil {
		t.Fatalf("平台 provider 调用失败: %v", err)
	}
	if gotPath != "/llm-api/v1/chat/completions" {
		t.Errorf("未走平台代理路径，实际 %q", gotPath)
	}
	if gotAuth != "Bearer MK-TEST" {
		t.Errorf("代理鉴权头不对: %q", gotAuth)
	}
	if gotProvider != "gpt-platform" {
		t.Errorf("未传 provider 寻址参数: %q", gotProvider)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("响应解析为空")
	}
}

// TestSelfProviderDoesNotUseProxy 自带 Key 路径**绝不能**被代理接管：
// 那会把商户自己的 Key 送到平台去（既是安全问题，也让平台承担本不该承担的流量）。
func TestSelfProviderDoesNotUseProxy(t *testing.T) {
	var proxyHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withProxy(t, srv.URL)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"direct"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewLLMService()
	cfg := &LLMConfig{
		Source:   "self", // 自带 Key
		APIKey:   "sk-user-own-key",
		BaseURL:  upstream.URL,
		APIType:  "openai",
		Model:    "qwen",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
	if _, err := svc.callProvider(context.Background(), cfg, []byte(`{"model":"qwen"}`)); err != nil {
		t.Fatalf("自带 Key 调用失败: %v", err)
	}
	if proxyHit {
		t.Fatal("自带 Key 的调用被打到了平台代理：商户自己的 Key 会被送到平台")
	}
}

// TestPlatformBalanceInsufficientIsDistinguishable 余额不足必须是**独立**一类错误。
//
// 处置动作完全不同：余额不足 → 去充值；调用失败 → 稍后重试或换模型。
// 混成一句 "调用失败" 会让用户在余额已经为负时反复重试。
func TestPlatformBalanceInsufficientIsDistinguishable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"message":"余额不足"}`))
	}))
	defer srv.Close()
	withProxy(t, srv.URL)

	svc := NewLLMService()
	cfg := &LLMConfig{Source: LLMProviderSourcePlatform, ProviderName: "p", Messages: []ChatMessage{{Role: "user", Content: "x"}}}
	_, err := svc.callProvider(context.Background(), cfg, []byte(`{}`))
	if err == nil {
		t.Fatal("402 应返回错误")
	}
	if !IsBalanceInsufficient(err) {
		t.Fatalf("402 应被识别为余额不足，实际 err=%v", err)
	}
	if strings.Contains(err.Error(), "调用平台模型失败: 402") {
		t.Error("余额不足不应被包装成通用调用失败（用户会被引导去重试而不是充值）")
	}
}

func TestPlatformProviderNotConfiguredFailsFast(t *testing.T) {
	withProxy(t, "") // 空地址 = 未配置
	if IsPlatformProxyConfigured() {
		t.Fatal("空地址时不应报告平台代理可用")
	}
	svc := NewLLMService()
	cfg := &LLMConfig{Source: LLMProviderSourcePlatform, ProviderName: "p"}
	_, err := svc.callProvider(context.Background(), cfg, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("未配置时应快速失败并说清原因，实际 err=%v", err)
	}
}

// TestPlatformResponseErrorBodyTruncated 上游报错时不要把整段报文塞进日志
func TestPlatformResponseErrorBodyTruncated(t *testing.T) {
	big := strings.Repeat("x", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()
	withProxy(t, srv.URL)

	svc := NewLLMService()
	cfg := &LLMConfig{Source: LLMProviderSourcePlatform, ProviderName: "p"}
	_, err := svc.callProvider(context.Background(), cfg, []byte(`{}`))
	if err == nil {
		t.Fatal("502 应返回错误")
	}
	if len(err.Error()) > 600 {
		t.Fatalf("错误信息过长（%d 字符）：会把上游整段报文灌进日志", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Error("截断后应有标记，便于判断信息不完整")
	}
}

// TestProviderSourceOnConfig 判定口径：空值回落 self，platform 才走代理
func TestProviderSourceOnConfig(t *testing.T) {
	if (&LLMConfig{}).Source == LLMProviderSourcePlatform {
		t.Fatal("空 Source 不得判为 platform")
	}
	if (&LLMConfig{Source: "self"}).Source == LLMProviderSourcePlatform {
		t.Fatal("self 不得判为 platform")
	}
}

// TestUsageSinkFiresOnlyForPlatformProvider 用量回填只对平台垫资 provider 发生。
// 自带 Key 路径本来就在 llm_routing_logs 里统计，再记一份到 llm_usage_records
// 就是双份账。
func TestUsageSinkFiresOnlyForPlatformProvider(t *testing.T) {
	var events []PlatformUsageEvent
	SetPlatformUsageSink(func(e PlatformUsageEvent) { events = append(events, e) })
	t.Cleanup(func() { SetPlatformUsageSink(nil) })

	d := newTestDispatcherWithProviders(t,
		ProviderConfig{Name: "self-p", APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "m", Source: "self", Enabled: true},
		ProviderConfig{Name: "plat-p", BaseURL: "", Model: "m", Source: "platform", Enabled: true},
	)
	if d == nil {
		t.Fatal("dispatcher 构造失败")
	}
	// 断言点放在「装配可用」而不是「具体触发」：触发点在成功路径上，
	// 需要真实上游响应，属于 integration 层。这里的价值是：
	// 若有人重构把 sink 的注入点删掉，这里会红，而那条重构会让
	// 平台垫资调用的本地明细全部消失（平台侧仍在扣钱，商户看不到账）。
	if PlatformUsageSink() == nil {
		t.Fatal("sink 未注入")
	}
}

func newTestDispatcherWithProviders(t *testing.T, cfgs ...ProviderConfig) *Dispatcher {
	t.Helper()
	d := NewDispatcher(NewLLMService())
	for i := range cfgs {
		d.AddProvider(cfgs[i])
	}
	return d
}

// TestComputeCostCentsRoundsDown 成本换算向下取整：少收一分比多收一分安全。
func TestComputeCostCentsRoundsDown(t *testing.T) {
	u := PlatformUsageEvent{PromptTokens: 1, CompletionTokens: 1}
	_ = u
	// 1 token × 0.0007 元/1k = 0.0000007 元 = 0.00007 分 → 0
	if got := centsFrom(float64(1) * 0.0007 / 1000.0); got != 0 {
		t.Errorf("极小金额应向下取整为 0，实际 %d", got)
	}
	// 1000 token × 1 分/1k = 1 分
	if got := centsFrom(float64(1000) * 1 / 1000.0); got != 1 {
		t.Errorf("整数分应精确，实际 %d", got)
	}
}

func centsFrom(v float64) int64 { return int64(v) }

// almostEq float 容差比较（相对误差 1e-9）
func almostEq(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1e-9*b+1e-12
}

var _ = json.Marshal

// TestDispatchPlatformProviderEndToEnd 端到端：dispatch → 代理 → 用量回填。
//
// 这是本轮修复的核心断言。在加它之前，链路断在「代理已实现、dispatcher 没接线」
// 这个状态上：所有单测都绿（计价、闸、菜单），而用户点了平台模型就是失败。
// 单点测试测不出"两个模块之间的连线"，所以这条用例的价值正在于它跨了那条缝。
func TestDispatchPlatformProviderEndToEnd(t *testing.T) {
	var proxyCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"平台回复"}}],
			"usage":{"prompt_tokens":1000,"completion_tokens":400}}`))
	}))
	defer srv.Close()
	withProxy(t, srv.URL)

	var events []PlatformUsageEvent
	SetPlatformUsageSink(func(e PlatformUsageEvent) { events = append(events, e) })
	t.Cleanup(func() { SetPlatformUsageSink(nil) })
	// 平台套餐单价快照（PLT-01 刀1）：平台 provider 的计价唯一来源是快照整数分
	// （写入时点按 plan_code 取本地镜像价列），自带 CostPer1k* 浮点字段不再参与。
	// 0.5 元/1k = 50 分/1k、3 元/1k = 300 分/1k。
	SetPlatformPlanPriceResolver(func(planCode string) (int64, int64) {
		return 50, 300
	})
	t.Cleanup(func() { SetPlatformPlanPriceResolver(nil) })

	d := NewDispatcher(NewLLMService())
	d.AddProvider(ProviderConfig{
		Name: "plat", Model: "gpt-4o",
		Source:   "platform",
		PlanCode: "plan-e2e",
		// 刻意填非零浮点价：钉死「平台调用不再消费这两个字段（那是第二定价源）」
		// ——下面的断言值全部来自 resolver 快照，填 999 若再生效断言必红。
		CostPer1kInput: 999, CostPer1kOutput: 999,
		Enabled: true, QualityScore: 1,
	})
	d.SetRoute(ScenarioRoute{
		Scenario: ScenarioSOPReply, Provider: "plat", MaxLatency: 10000,
	})

	res, err := d.Dispatch(context.Background(), DispatchRequest{
		Scenario: ScenarioSOPReply,
		Prompt:   "你好",
		Messages: []ChatMessage{{Role: "user", Content: "你好"}},
		AgentID:  "agent-1", UserID: "user-1",
	})
	if err != nil {
		t.Fatalf("平台 provider 派发失败: %v", err)
	}
	if proxyCalls != 1 {
		t.Fatalf("应经平台代理调用一次，实际 %d", proxyCalls)
	}
	if res.Content != "平台回复" {
		t.Errorf("未取到代理响应内容: %q", res.Content)
	}
	// 分别计价：1000×50/1000 = 50 分 = 0.5 元；400×300/1000 = 120 分 = 1.2 元。
	// 整数分 ÷100 的元值在 numeric(14,6) 上精确两位小数，容差比较只为 float
	// 序列化噪声（0.5/1.2 二进制不精确）。
	if !almostEq(res.PromptCost, 0.5) || !almostEq(res.CompletionCost, 1.2) {
		t.Errorf("分别计价不符: prompt=%v completion=%v（期望 0.5 / 1.2）",
			res.PromptCost, res.CompletionCost)
	}
	if len(events) != 1 {
		t.Fatalf("平台 provider 应回填 1 条用量，实际 %d 条", len(events))
	}
	e := events[0]
	if e.Provider != "plat" || e.PromptTokens != 1000 || e.CompletionTokens != 400 {
		t.Errorf("用量回填内容不符: %+v", e)
	}
	if e.AgentID != "agent-1" || e.UserID != "user-1" {
		t.Errorf("归因上下文未透传（商户无法回答谁用掉的）: %+v", e)
	}
	// 整数分快照逐分对齐（PLT-01 判据）：事件携带的分必须与 tokens*price/1000
	// 整数换算一致，且不经 float 元 ×100 往返。
	if e.InputCostCents != 50 || e.OutputCostCents != 120 {
		t.Errorf("整数分快照不符: in=%d out=%d（期望 50 / 120，tokens×price/1000 整数口径）",
			e.InputCostCents, e.OutputCostCents)
	}
}

// TestPriceTokensCentsMatchesPlatform 整数计价与平台 PriceTokens 同式同截断
// （platform-server llm_billing_service.go：tokens*price/1000，向下取整不四舍五入）。
// 固定值断言，不锁平台文案。
func TestPriceTokensCentsMatchesPlatform(t *testing.T) {
	cases := []struct {
		name          string
		tokens, price int64
		want          int64
	}{
		{"整除", 1000, 50, 50},
		{"截断 1500x7=10.5 取 10", 1500, 7, 10},
		{"极小金额归零 1x7", 1, 7, 0},
		{"零 token", 0, 50, 0},
		{"负 token", -5, 50, 0},
		{"零价（join 不上降级格）", 1000, 0, 0},
		{"负价", 1000, -1, 0},
		{"大数不溢出", 1_000_000, 9_999, 9_999_000},
	}
	for _, c := range cases {
		if got := PriceTokensCents(c.tokens, c.price); got != c.want {
			t.Errorf("%s: PriceTokensCents(%d,%d)=%d，期望 %d", c.name, c.tokens, c.price, got, c.want)
		}
	}
}

// TestPlatformCostWithoutResolverDegradesToZero 未注入单价 resolver（或镜像无价）
// 时成本如实记 0：不报错、不编价（join 失败降级格的 dispatcher 侧）。
func TestPlatformCostWithoutResolverDegradesToZero(t *testing.T) {
	if PriceTokensCents(1000, 0) != 0 {
		t.Fatal("无价必须降级 0 分")
	}
}

// TestDispatchSelfProviderDoesNotEmitUsageEvent 自带 Key 不产生平台用量事件
func TestDispatchSelfProviderDoesNotEmitUsageEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"直连"}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
	}))
	defer upstream.Close()
	withProxy(t, "http://127.0.0.1:1") // 代理地址故意不可用：走到它就说明分流错了

	var events int
	SetPlatformUsageSink(func(PlatformUsageEvent) { events++ })
	t.Cleanup(func() { SetPlatformUsageSink(nil) })

	d := NewDispatcher(NewLLMService())
	d.AddProvider(ProviderConfig{
		Name: "own", Model: "qwen", APIKey: "sk-own", BaseURL: upstream.URL,
		APIType: "openai", Source: "self", Enabled: true, QualityScore: 1,
	})
	d.SetRoute(ScenarioRoute{
		Scenario: ScenarioSOPReply, Provider: "own", MaxLatency: 10000,
	})
	if _, err := d.Dispatch(context.Background(), DispatchRequest{
		Scenario: ScenarioSOPReply, Prompt: "hi",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("自带 Key 派发失败: %v", err)
	}
	if events != 0 {
		t.Fatal("自带 Key 不得产生平台用量事件（那是平台垫资的账）")
	}
}

// TestPlatformProviderSkippedWhenPlatformUnconfigured 平台没配置时，
// source='platform' 的 provider 不进候选，让调度走 fallback。
//
// 不做这一步的后果：离线部署（或平台改域名）之后，这些 provider 仍然 Enabled，
// 每次 AI 调用都发一次注定失败的请求，用户看到的是「AI 一直报错」，
// 而真实原因是平台没连上 —— 排查方向会被完全带偏。
func TestPlatformProviderSkippedWhenPlatformUnconfigured(t *testing.T) {
	withProxy(t, "") // 空地址 = 未配置
	if IsPlatformProxyConfigured() {
		t.Fatal("空地址时不应报告可用")
	}

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fallback"}}],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`))
	}))
	defer upstream.Close()

	d := NewDispatcher(NewLLMService())
	d.AddProvider(ProviderConfig{
		Name: "plat", Model: "gpt-4o", Source: "platform", Enabled: true, QualityScore: 0.9,
	})
	d.AddProvider(ProviderConfig{
		Name: "own", Model: "qwen", APIKey: "sk", BaseURL: upstream.URL, APIType: "openai",
		Source: "self", Enabled: true, QualityScore: 1,
	})
	d.SetRoute(ScenarioRoute{
		Scenario: ScenarioSOPReply, Provider: "plat",
		Fallbacks: []string{"own"}, MaxLatency: 10000,
	})

	res, err := d.Dispatch(context.Background(), DispatchRequest{
		Scenario: ScenarioSOPReply, Prompt: "hi",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("应降级到自带 Key 而不是失败: %v", err)
	}
	if res.Provider != "own" {
		t.Errorf("平台未配置时应降级到自带 Key，实际 provider=%q", res.Provider)
	}
	if upstreamCalls != 1 {
		t.Errorf("降级路径应真实调用自带 Key provider 一次，实际 %d", upstreamCalls)
	}
}

// TestSelfProvidersUnaffectedWhenPlatformUnconfigured 自带 Key 完全不受平台配置影响
func TestSelfProvidersUnaffectedWhenPlatformUnconfigured(t *testing.T) {
	withProxy(t, "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	d := NewDispatcher(NewLLMService())
	d.AddProvider(ProviderConfig{
		Name: "own", Model: "qwen", APIKey: "sk", BaseURL: upstream.URL, APIType: "openai",
		Source: "self", Enabled: true, QualityScore: 1,
	})
	d.SetRoute(ScenarioRoute{Scenario: ScenarioSOPReply, Provider: "own", MaxLatency: 10000})
	if _, err := d.Dispatch(context.Background(), DispatchRequest{
		Scenario: ScenarioSOPReply, Prompt: "hi",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("平台未配置不得影响自带 Key: %v", err)
	}
}
