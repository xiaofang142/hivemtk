package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
)

func TestDispatchCacheScope_ConfigurationAndExecutionAreIsolated(t *testing.T) {
	for _, name := range []string{"模型改变", "输出参数改变", "工具请求", "租户改变"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": body.Model}, "finish_reason": "stop",
				}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
			}))
			defer srv.Close()
			d := newDispatcherBase(NewLLMService())
			provider := ProviderConfig{Name: "scope-local", BaseURL: srv.URL, APIType: "openai", Model: "model-a", APIKey: "fixture", Enabled: true, QualityScore: 1}
			d.AddProvider(provider)
			d.SetRoute(ScenarioRoute{Scenario: ScenarioSOPReply, Provider: provider.Name, MinQuality: 0.9})
			ctx := tenant.OnlyTenant(context.Background(), 18)
			req := DispatchRequest{Scenario: ScenarioSOPReply, Prompt: "合成问题", CacheKey: "scope-test:" + srv.URL, CacheTTL: 60, MaxTokens: 128}
			for i := 0; i < 2; i++ {
				res, err := d.Dispatch(ctx, req)
				if err != nil || res == nil || res.Content != "model-a" {
					t.Fatalf("正常模型回答失败：%v", err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("同配置正常请求必须真的命中缓存")
			}
			switch name {
			case "模型改变":
				provider.Model = "model-b"
				d.AddProvider(provider)
			case "输出参数改变":
				req.MaxTokens++
			case "工具请求":
				req.ToolChoice = "required"
			case "租户改变":
				ctx = tenant.OnlyTenant(context.Background(), 19)
			}
			res, err := d.Dispatch(ctx, req)
			if err != nil || res == nil {
				t.Fatalf("正常回源不得丢失回答：%v", err)
			}
			if calls.Load() != 2 {
				t.Fatalf("变化后必须执行新的模型请求，实际调用=%s", strconv.Itoa(int(calls.Load())))
			}
		})
	}
}

func TestDispatchCacheScope_ImplicitVoteCannotBorrowSingleAnswer(t *testing.T) {
	previous := fanoutVoteEnabledGetter
	t.Cleanup(func() { fanoutVoteEnabledGetter = previous })
	SetFanoutVoteEnabledGetter(func() bool { return false })
	var calls int32
	srv := newHitCountingServer(&calls)
	defer srv.Close()
	d := newFanOutTestDispatcher(srv)
	d.SetRoute(ScenarioRoute{Scenario: ScenarioHighQuality, Provider: "alpha", Fallbacks: []string{"beta"}})
	ctx := tenant.OnlyTenant(context.Background(), 18)
	req := DispatchRequest{Scenario: ScenarioHighQuality, Prompt: "合成投票问题", CacheKey: "vote-scope:" + srv.URL, CacheTTL: 60}
	for i := 0; i < 2; i++ {
		if _, err := d.Dispatch(ctx, req); err != nil {
			t.Fatalf("普通请求失败：%v", err)
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatal("普通同配置请求应真正命中缓存")
	}
	SetFanoutVoteEnabledGetter(func() bool { return true })
	if _, err := d.Dispatch(ctx, req); err != nil {
		t.Fatalf("投票请求失败：%v", err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("启用投票后应调用两个候选而非借用普通答案，实际总调用=%d", atomic.LoadInt32(&calls))
	}
}

func TestDispatchCacheScope_InFlightChangeDoesNotFillOldConfiguration(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": body.Model}, "finish_reason": "stop",
		}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(tenant.OnlyTenant(context.Background(), 18), 5*time.Second)
	defer cancel()
	d := newDispatcherBase(NewLLMService())
	p := ProviderConfig{Name: "inflight-local", BaseURL: srv.URL, APIType: "openai", Model: "model-a", APIKey: "fixture", Enabled: true, QualityScore: 1}
	d.AddProvider(p)
	d.SetRoute(ScenarioRoute{Scenario: ScenarioSOPReply, Provider: p.Name})
	req := DispatchRequest{Scenario: ScenarioSOPReply, Prompt: "合成在途问题", CacheKey: "inflight-scope:" + srv.URL, CacheTTL: 60}
	done := make(chan error, 1)
	utils.SafeGo(ctx, "test.dispatch-cache-inflight", func(ctx context.Context) {
		_, err := d.Dispatch(ctx, req)
		done <- err
	})
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("模型请求没有开始")
	}
	p.Model = "model-b"
	d.AddProvider(p)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("在途请求未结束")
	}
	p.Model = "model-a"
	d.AddProvider(p)
	res, err := d.Dispatch(ctx, req)
	if err != nil || res == nil || res.Content != "model-a" || calls.Load() != 2 {
		t.Fatalf("在途配置变化后不得回填旧键：调用=%d，错误=%v", calls.Load(), err)
	}
}
