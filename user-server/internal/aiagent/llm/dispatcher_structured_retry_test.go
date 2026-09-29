package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type leadJudgeSchema struct {
	IsLead      bool  `json:"is_lead"`
	IntentScore int64 `json:"intent_score"`
}

// openAIChatResponse 拼一个 OpenAI 兼容的最小响应体。
func openAIChatResponse(t *testing.T, content string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"},
		},
		"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10},
	})
	if err != nil {
		t.Fatalf("构造 mock 响应失败: %v", err)
	}
	return string(body)
}

// respond 收到「第几次调用」，由用例决定回散文还是回 JSON。
type respond func(w http.ResponseWriter, call int32)

type structuredProbe struct {
	calls  atomic.Int32
	bodies []map[string]any
}

func newStructuredDispatcher(t *testing.T, fn respond) (*Dispatcher, *structuredProbe) {
	t.Helper()
	probe := &structuredProbe{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := probe.calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		probe.bodies = append(probe.bodies, parsed)
		w.Header().Set("Content-Type", "application/json")
		fn(w, n)
	}))
	t.Cleanup(srv.Close)

	d := newDispatcherBase(NewLLMService())
	p := &ProviderConfig{
		Name:    "jsontest",
		BaseURL: srv.URL,
		APIType: "openai",
		Model:   "test-model",
		Enabled: true,
		APIKey:  "x",
	}
	d.providers[p.Name] = p
	d.routes[ScenarioLowCost] = &ScenarioRoute{Scenario: ScenarioLowCost, Provider: p.Name, MaxLatency: 10000}
	return d, probe
}

func writeChat(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	if _, err := io.WriteString(w, openAIChatResponse(t, content)); err != nil {
		t.Fatalf("写 mock 响应失败: %v", err)
	}
}

// 兼容网关收下 response_format=json_object 却回散文时，DispatchStructured 必须补一次
// 「只输出 JSON」的重试，而不是直接把结构化调用判死（桥接侧 lead-mining 实测 27 次全灭）。
func TestDispatchStructuredRetriesWhenProviderReturnsProse(t *testing.T) {
	d, probe := newStructuredDispatcher(t, func(w http.ResponseWriter, n int32) {
		if n == 1 {
			writeChat(t, w, "您好！关于您的问题，以下是通用分析思路：\n1. 检查日志\n2. 性能分析")
			return
		}
		writeChat(t, w, `{"is_lead":true,"intent_score":82}`)
	})

	var out leadJudgeSchema
	res, err := d.DispatchStructured(context.Background(), DispatchRequest{
		Scenario:     ScenarioLowCost,
		SystemPrompt: "你是线索发掘助手，只输出 JSON。",
		Messages:     []ChatMessage{{Role: "user", Content: "你好，咨询产品价格与优惠活动"}},
	}, &out)
	if err != nil {
		t.Fatalf("重试后应成功，实际 err=%v", err)
	}
	if got := probe.calls.Load(); got != 2 {
		t.Fatalf("应打两次上游，实际 %d", got)
	}
	if !out.IsLead || out.IntentScore != 82 {
		t.Fatalf("schema 未回填: %+v", out)
	}
	if res.Content == "" || !strings.Contains(res.Content, "is_lead") {
		t.Fatalf("result.Content 应为抽出的 JSON: %q", res.Content)
	}

	if len(probe.bodies) != 2 {
		t.Fatalf("期望捕获 2 个请求体，实际 %d", len(probe.bodies))
	}
	for i, b := range probe.bodies {
		if b["response_format"] == nil {
			t.Fatalf("第 %d 次请求缺 response_format", i+1)
		}
	}
	first := probe.bodies[0]["messages"].([]any)
	second := probe.bodies[1]["messages"].([]any)
	if len(second) != len(first)+1 {
		t.Fatalf("重试应只在末尾追加一条约束消息: %d → %d", len(first), len(second))
	}
	last := second[len(second)-1].(map[string]any)
	if last["role"] != "user" || !strings.Contains(last["content"].(string), "只输出目标 JSON") {
		t.Fatalf("重试消息形状不对: %+v", last)
	}
	// 原对话不许被丢掉（丢掉等于换了个问题问）
	if first[0].(map[string]any)["content"] != "你好，咨询产品价格与优惠活动" {
		t.Fatalf("原消息被改写: %+v", first)
	}
}

// 只走 Prompt（不带 Messages）的请求同样要被重试覆盖。
func TestDispatchStructuredRetryFallsBackToPrompt(t *testing.T) {
	d, probe := newStructuredDispatcher(t, func(w http.ResponseWriter, n int32) {
		if n == 1 {
			writeChat(t, w, "纯散文回复")
			return
		}
		writeChat(t, w, `{"is_lead":false,"intent_score":3}`)
	})

	var out leadJudgeSchema
	if _, err := d.DispatchStructured(context.Background(), DispatchRequest{
		Scenario: ScenarioLowCost,
		Prompt:   "判断这段对话是否是销售线索",
	}, &out); err != nil {
		t.Fatalf("Prompt-only 重试后应成功，实际 err=%v", err)
	}
	if got := probe.calls.Load(); got != 2 || out.IsLead {
		t.Fatalf("calls=%d out=%+v", got, out)
	}
	msgs := probe.bodies[1]["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("Prompt-only 重试应只有一条消息，实际 %d", len(msgs))
	}
	content := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, "判断这段对话是否是销售线索") || !strings.Contains(content, "只输出目标 JSON") {
		t.Fatalf("重试消息应同时含原 Prompt 与硬约束: %q", content)
	}
}

// 两次都拿不到 JSON 时才认失败，且错误串里的模型原文必须被截断（客服对话正文可达数 KB）。
func TestDispatchStructuredGivesUpAfterRetryAndClipsContent(t *testing.T) {
	long := "您好！" + strings.Repeat("详", 4000)
	d, probe := newStructuredDispatcher(t, func(w http.ResponseWriter, _ int32) {
		writeChat(t, w, long)
	})

	var out leadJudgeSchema
	_, err := d.DispatchStructured(context.Background(), DispatchRequest{
		Scenario: ScenarioLowCost,
		Messages: []ChatMessage{{Role: "user", Content: "你好"}},
	}, &out)
	if err == nil {
		t.Fatal("两次都是散文时应判失败")
	}
	if got := probe.calls.Load(); got != 2 {
		t.Fatalf("应重试一次，实际 %d 次", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "…") {
		t.Fatalf("错误串应带截断省略号，实际前 200 字: %s", msg)
	}
	if strings.Contains(msg, long) {
		t.Fatal("错误串不得整段带上模型原文")
	}
	if len([]rune(msg)) > 240 {
		t.Fatalf("错误串过长: %d runes", len([]rune(msg)))
	}
}
