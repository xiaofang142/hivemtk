package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantAgentToolWired 是 agent_tool 组本轮接线的 4 条。result_cache_ttl 不在其中：
// ResultCache 的生产链路从未构造过它，没有构造点就没有可接的读取点。
var wantAgentToolWired = []string{
	"agent_tool.max_concurrent",
	"agent_tool.max_content_len",
	"agent_tool.fail_threshold",
	"agent_tool.cooldown_duration",
}

func resetAgentToolProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		tooluse.SetMaxConcurrentProvider(nil)
		tooluse.SetMaxContentLenProvider(nil)
		tooluse.SetRouterFailThresholdProvider(nil)
		tooluse.SetRouterCooldownDurationProvider(nil)
	})
}

func newAgentToolParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetAgentToolProviders(t)
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no DB")
	}
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	return svc
}

func TestWireAgentToolConfigParamsKeys(t *testing.T) {
	resetAgentToolProviders(t)
	got := WireAgentToolConfigParams()
	if len(got) != len(wantAgentToolWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantAgentToolWired), got, wantAgentToolWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantAgentToolWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantAgentToolWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次", k)
		}
		seen[k] = true
	}
}

func TestWiredAgentToolKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantAgentToolWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在", k)
		}
	}
	// 反向钉死：result_cache_ttl 必须仍在种子里，且必须仍带「未接线」标注。
	// 它没有生产构造点，接线只是给别人一个「改了会生效」的错觉。
	var found bool
	for _, d := range service.DefaultParamDefs() {
		if d.Group == "agent_tool" && d.Key == "result_cache_ttl" {
			found = true
		}
	}
	if !found {
		t.Error("agent_tool.result_cache_ttl 从种子里消失了：它没有生产构造点，应保留条目与「未接线」标注")
	}
}

func TestWireAgentToolConfigParamsReachesReaders(t *testing.T) {
	svc := newAgentToolParamFixture(t)
	ctx := context.Background()

	beforeConcurrent := tooluse.MaxConcurrent()
	beforeContentLen := tooluse.MaxContentLen()
	beforeThreshold := tooluse.RouterFailThreshold()
	beforeCooldown := tooluse.RouterCooldownDuration()

	WireAgentToolConfigParams()

	for _, tc := range []struct{ key, val string }{
		{"max_concurrent", "12"},
		{"max_content_len", "8000"},
		{"fail_threshold", "8"},
		{"cooldown_duration", "120"},
	} {
		if err := svc.UpdateValue(ctx, "agent_tool", tc.key, tc.val, 1); err != nil {
			t.Fatalf("改 agent_tool.%s 失败：%v", tc.key, err)
		}
	}

	if got, want := tooluse.MaxConcurrent(), 12; got != want {
		t.Errorf("max_concurrent 未被读走：got %v（原 %v）want %v", got, beforeConcurrent, want)
	}
	if got, want := tooluse.MaxContentLen(), 8000; got != want {
		t.Errorf("max_content_len 未被读走：got %v（原 %v）want %v", got, beforeContentLen, want)
	}
	if got, want := tooluse.RouterFailThreshold(), 8; got != want {
		t.Errorf("fail_threshold 未被读走：got %v（原 %v）want %v", got, beforeThreshold, want)
	}
	if got, want := tooluse.RouterCooldownDuration(), 120*time.Second; got != want {
		t.Errorf("cooldown_duration 未被读走：got %v（原 %v）want %v", got, beforeCooldown, want)
	}
}

func TestWireAgentToolConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetAgentToolProviders(t)

	if got := WireAgentToolConfigParams(); len(got) != len(wantAgentToolWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d", len(got), len(wantAgentToolWired))
	}
	if got, want := tooluse.MaxConcurrent(), tooluse.DefaultMaxConcurrent; got != want {
		t.Errorf("无库时 max_concurrent = %v，期望兜底 %v", got, want)
	}
	if got, want := tooluse.MaxContentLen(), tooluse.DefaultMaxContentLen; got != want {
		t.Errorf("无库时 max_content_len = %v，期望兜底 %v", got, want)
	}
	if got, want := tooluse.RouterFailThreshold(), tooluse.DefaultRouterFailThreshold; got != want {
		t.Errorf("无库时 fail_threshold = %v，期望兜底 %v", got, want)
	}
	if got, want := tooluse.RouterCooldownDuration(), tooluse.DefaultRouterCooldownDuration; got != want {
		t.Errorf("无库时 cooldown_duration = %v，期望兜底 %v", got, want)
	}
}
