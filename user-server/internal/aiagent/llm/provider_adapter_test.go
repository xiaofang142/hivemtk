package llm

import (
	"errors"
	"testing"
)

func TestBuildProviderRequestShape(t *testing.T) {
	t.Run("native session allowed", func(t *testing.T) {
		cfg := LLMConfig{
			Model: "gpt-4o", MaxTokens: 99999,
			LLMSessionRef: "llmsess_1", RequestIdempotencyKey: "idem-1", PromptPrefixHash: "prefix-1",
			Messages: []ChatMessage{{Role: "user", Content: "你好"}},
		}
		caps := ResolveProviderCapabilities(ProviderConfig{Model: "gpt-4o"})
		caps.SupportsIdempotencyKey = true
		shape, err := BuildProviderRequestShape(cfg, caps)
		if err != nil {
			t.Fatalf("支持原生 session 时不应失败: %v", err)
		}
		if !shape.HasSession || !shape.HasIdempotencyKey || len(shape.UnsupportedFields) != 0 {
			t.Fatalf("请求形状错误: %+v", shape)
		}
		if shape.MaxTokens != caps.MaxOutputTokens {
			t.Fatalf("输出预算必须夹取到 provider 上限: got %d want %d", shape.MaxTokens, caps.MaxOutputTokens)
		}
		if len(shape.Messages) != 1 || shape.Messages[0].Content != "你好" {
			t.Fatalf("请求消息丢失: %+v", shape.Messages)
		}
	})

	t.Run("stateless provider rejects session", func(t *testing.T) {
		cfg := LLMConfig{Model: "deepseek-v4-pro", LLMSessionRef: "llmsess_2"}
		caps := ResolveProviderCapabilities(ProviderConfig{Model: "deepseek-v4-pro"})
		_, err := BuildProviderRequestShape(cfg, caps)
		if !errors.Is(err, ErrUnsupportedSession) {
			t.Fatalf("stateless 协议必须拒绝 session ref, got %v", err)
		}
	})

	t.Run("unsupported fields reported", func(t *testing.T) {
		cfg := LLMConfig{
			Model: "unknown-model", RequestIdempotencyKey: "idem-2", PromptPrefixHash: "prefix-2",
		}
		caps := ResolveProviderCapabilities(ProviderConfig{Model: "unknown-model"})
		shape, err := BuildProviderRequestShape(cfg, caps)
		if err != nil {
			t.Fatalf("不支持字段不应阻断无状态请求: %v", err)
		}
		if len(shape.UnsupportedFields) != 2 {
			t.Fatalf("应报告两个不支持字段: %+v", shape.UnsupportedFields)
		}
	})

	t.Run("empty model rejected", func(t *testing.T) {
		_, err := BuildProviderRequestShape(LLMConfig{}, ProviderCapabilities{})
		if !errors.Is(err, ErrUnsupportedProviderField) {
			t.Fatalf("空模型必须拒绝, got %v", err)
		}
	})
}

func TestNormalizeProviderUsage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ProviderUsage
	}{
		{
			name: "openai cached details",
			raw:  `{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":40}}}`,
			want: ProviderUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CachedInputTokens: 40},
		},
		{
			name: "anthropic cache tokens",
			raw:  `{"usage":{"input_tokens":90,"output_tokens":30,"cache_read_input_tokens":8,"cache_creation_input_tokens":4}}`,
			want: ProviderUsage{PromptTokens: 90, CompletionTokens: 30, CachedInputTokens: 8, CacheCreationTokens: 4},
		},
		{
			name: "gemini usage metadata",
			raw:  `{"usageMetadata":{"promptTokenCount":70,"candidatesTokenCount":15,"totalTokenCount":85,"cached_content_token_count":5}}`,
			want: ProviderUsage{PromptTokens: 70, CompletionTokens: 15, TotalTokens: 85, CachedInputTokens: 5},
		},
		{name: "missing usage", raw: `{"choices":[]}`, want: ProviderUsage{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeProviderUsage([]byte(tt.raw))
			if err != nil {
				t.Fatalf("usage 归一化失败: %v", err)
			}
			if got != tt.want {
				t.Fatalf("usage 不符: got %+v want %+v", got, tt.want)
			}
			if tt.want.TotalTokens == 0 && got.NormalizedTotal() != tt.want.PromptTokens+tt.want.CompletionTokens {
				t.Fatalf("total 退回计算错误: %d", got.NormalizedTotal())
			}
		})
	}
}

func TestNormalizeFinishReason(t *testing.T) {
	tests := map[string]string{
		"stop": "stop", "end_turn": "stop", "": "stop",
		"length": "length", "max_tokens": "length", "MAX_TOKENS": "length",
		"tool_calls": "tool_calls", "tool_use": "tool_calls",
		"content_filter": "content_filter", "SAFETY": "content_filter",
	}
	for raw, want := range tests {
		if got := NormalizeFinishReason(raw); got != want {
			t.Fatalf("finish reason %q: got %q want %q", raw, got, want)
		}
	}
}

func TestParseProviderResponse(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		content  string
		finish   string
		cached   int
		response string
	}{
		{
			name:     "openai",
			raw:      `{"id":"resp-1","choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":6}}}`,
			content:  "你好",
			finish:   "stop",
			cached:   6,
			response: "resp-1",
		},
		{
			name:    "anthropic",
			raw:     `{"id":"msg-1","content":[{"type":"text","text":"在"}],"stop_reason":"max_tokens","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":4}}`,
			content: "在",
			finish:  "length",
			cached:  4,
		},
		{
			name:    "gemini",
			raw:     `{"candidates":[{"content":{"parts":[{"text":"收到"}]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2,"cached_content_token_count":3}}`,
			content: "收到",
			finish:  "length",
			cached:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProviderResponse([]byte(tt.raw))
			if err != nil {
				t.Fatalf("响应解析失败: %v", err)
			}
			if got.Content != tt.content || got.FinishReason != tt.finish || got.Usage.CachedInputTokens != tt.cached {
				t.Fatalf("响应归一化不符: %+v", got)
			}
			if tt.response != "" && got.ContinuationRef != tt.response {
				t.Fatalf("continuation ref 丢失: %+v", got)
			}
		})
	}
	if _, err := ParseProviderResponse([]byte(`{not json`)); !errors.Is(err, ErrProviderResponseMalformed) {
		t.Fatalf("非法 JSON 必须拒绝, got %v", err)
	}
}
