package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealProviderContextCaching 是显式 opt-in 的真实供应商验证；缺凭据时 skip 并记录 not-run。
// 运行方式见 scripts/verify-llm-real-provider.sh；本测试不模拟缓存命中，必须读真实 cached tokens。
func TestRealProviderContextCaching(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("REAL_LLM_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("REAL_LLM_API_KEY"))
	model := strings.TrimSpace(os.Getenv("REAL_LLM_MODEL"))
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("not-run: set REAL_LLM_BASE_URL/REAL_LLM_API_KEY/REAL_LLM_MODEL to enable real-provider verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stablePrefix := strings.Repeat("稳定的系统说明：你是严谨的客服助手，必须依据事实回答。", 120)
	run := func(dynamicTail string) (ProviderResponse, time.Duration) {
		t.Helper()
		body := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": stablePrefix},
				{"role": "user", "content": dynamicTail},
			},
			"max_tokens":     64,
			"stream":         true,
			"stream_options": map[string]bool{"include_usage": true},
		}
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("请求序列化失败: %v", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("请求构造失败: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("真实供应商请求失败: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("真实供应商返回状态码 %d", resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		var firstToken time.Duration
		var content strings.Builder
		var usage ProviderUsage
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 && strings.HasPrefix(line, "data:") {
				chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if chunk != "" && chunk != "[DONE]" {
					var payload map[string]json.RawMessage
					if json.Unmarshal([]byte(chunk), &payload) == nil {
						if raw := payload["choices"]; len(raw) > 0 {
							var choices []struct {
								Delta struct {
									Content string `json:"content"`
								} `json:"delta"`
								FinishReason string `json:"finish_reason"`
							}
							if json.Unmarshal(raw, &choices) == nil && len(choices) > 0 {
								if choices[0].Delta.Content != "" && firstToken == 0 {
									firstToken = time.Since(start)
								}
								content.WriteString(choices[0].Delta.Content)
							}
						}
						if normalized, err := NormalizeProviderUsage([]byte(chunk)); err == nil && normalized.NormalizedTotal() > 0 {
							usage = normalized
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
		if content.Len() == 0 {
			t.Fatal("真实供应商未返回任何内容")
		}
		return ProviderResponse{Content: content.String(), Usage: usage}, firstToken
	}

	cold, coldTTFT := run("第一轮：请只回答\"收到\"。")
	hot, hotTTFT := run("第二轮：请只回答\"收到\"。")
	t.Logf("real provider cold TTFT=%s cached=%d total=%d", coldTTFT, cold.Usage.CachedInputTokens, cold.Usage.NormalizedTotal())
	t.Logf("real provider hot TTFT=%s cached=%d total=%d", hotTTFT, hot.Usage.CachedInputTokens, hot.Usage.NormalizedTotal())
	if hot.Usage.CachedInputTokens <= cold.Usage.CachedInputTokens {
		t.Fatalf("真实缓存未命中：cold cached=%d hot cached=%d", cold.Usage.CachedInputTokens, hot.Usage.CachedInputTokens)
	}
	if hotTTFT > 0 && coldTTFT > 0 && hotTTFT >= coldTTFT {
		t.Fatalf("热调用 TTFT 未下降：cold=%s hot=%s", coldTTFT, hotTTFT)
	}

	if inputPrice := os.Getenv("REAL_LLM_INPUT_PRICE_PER_1K"); inputPrice != "" {
		t.Logf("cost check requested via %s; compare billed tokens with cached tokens from usage above", inputPrice)
	} else {
		t.Log("cost check skipped: REAL_LLM_INPUT_PRICE_PER_1K not set")
	}
	if os.Getenv("REAL_LLM_CONTINUATION_MODE") == "native" {
		t.Log("continuation expiry check requires provider-specific TTL; run scripts/verify-llm-real-provider.sh with REAL_LLM_EXPECTED_TTL")
	} else {
		t.Log("continuation expiry check skipped: REAL_LLM_CONTINUATION_MODE != native")
	}
}
