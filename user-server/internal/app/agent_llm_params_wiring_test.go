package app

import (
	"context"
	"testing"
	"time"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantAgentLLMWired 是 agent_llm 组本轮接线的 8 条，逐条写死而不是从
// DefaultParamDefs() 过滤——理由同 wantMiscWired / wantConfidenceWired：
// 过滤只能钉住"接上的都在本组内"，拼错 key 时断言照样成立，真正漏接的那条没人发现。
var wantAgentLLMWired = []string{
	"agent_llm.default_health_check_interval",
	"agent_llm.default_failure_threshold",
	"agent_llm.default_circuit_open_duration",
	"agent_llm.default_health_check_timeout",
	"agent_llm.vote_agreement_threshold",
	"agent_llm.default_http_timeout",
	"agent_llm.db_sink_stop_deadline",
	"agent_llm.provider_failover_policy",
}

// resetAgentLLMProviders 把 8 个注入点摘回未装配状态。provider 是包级变量，
// 测试之间不还原会互相污染：上一格注入的值会让下一格"未注入"的断言假绿。
func resetAgentLLMProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		llmpkg.SetHealthCheckIntervalProvider(nil)
		llmpkg.SetFailureThresholdProvider(nil)
		llmpkg.SetCircuitOpenDurationProvider(nil)
		llmpkg.SetHealthCheckTimeoutProvider(nil)
		llmpkg.SetVoteAgreementThresholdProvider(nil)
		llmpkg.SetDefaultHTTPTimeoutProvider(nil)
		llmpkg.SetDBSinkStopDeadlineProvider(nil)
		llmpkg.SetFailoverPolicyProvider(nil)
	})
}

// newAgentLLMParamFixture 见 newConfidenceParamFixture；每组各建一个是因为
// t.Cleanup 还原全局单例，两个 fixture 不能并存于同一次运行。
func newAgentLLMParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetAgentLLMProviders(t)
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

// TestWireAgentLLMConfigParamsKeys 装配点声明接线的键，必须与上面那 8 条逐条相等。
func TestWireAgentLLMConfigParamsKeys(t *testing.T) {
	resetAgentLLMProviders(t)
	got := WireAgentLLMConfigParams()
	if len(got) != len(wantAgentLLMWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantAgentLLMWired), got, wantAgentLLMWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantAgentLLMWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantAgentLLMWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次：同一个点位注入两次，后一次覆盖前一次，读点会跟着错位", k)
		}
		seen[k] = true
	}
}

// TestWiredAgentLLMKeysExistInSeed 上面那 8 条必须在种子表里真实存在。
// 引用一个没登记的键 = 运行期永远读兜底值 + 管理台上看不见这一行，
// 和当初那 72 条僵尸一模一样的病。
func TestWiredAgentLLMKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantAgentLLMWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// TestWireAgentLLMConfigParamsReachesReaders 装配完之后，参数中心的值必须真的
// 出现在读取点上。只断言 provider 被 setter 收下没有意义——那证明不了
// 「运维在页面上改值真的改变了行为」。
func TestWireAgentLLMConfigParamsReachesReaders(t *testing.T) {
	svc := newAgentLLMParamFixture(t)
	ctx := context.Background()

	// 先记住"没装配"时各读取口的值：接上之后它们必须被参数中心的值顶掉。
	beforeInterval := llmpkg.HealthCheckInterval()
	beforeThreshold := llmpkg.FailureThreshold()
	beforeOpen := llmpkg.CircuitOpenDuration()
	beforeCheckTimeout := llmpkg.HealthCheckTimeout()
	beforeVote := llmpkg.VoteAgreementThreshold()
	beforeHTTP := llmpkg.DefaultHTTPTimeout()
	beforeSink := llmpkg.DBSinkStopDeadline()

	WireAgentLLMConfigParams()

	for _, tc := range []struct{ key, val string }{
		{"default_health_check_interval", "77"},
		{"default_failure_threshold", "9"},
		{"default_circuit_open_duration", "120"},
		{"default_health_check_timeout", "11"},
		{"vote_agreement_threshold", "0.62"},
		{"default_http_timeout", "95"},
		{"db_sink_stop_deadline", "6"},
	} {
		if err := svc.UpdateValue(ctx, "agent_llm", tc.key, tc.val, 1); err != nil {
			t.Fatalf("改 agent_llm.%s 失败：%v", tc.key, err)
		}
	}

	if got, want := llmpkg.HealthCheckInterval(), 77*time.Second; got != want {
		t.Errorf("default_health_check_interval 未被读走：got %v（原 %v）want %v", got, beforeInterval, want)
	}
	if got, want := llmpkg.FailureThreshold(), 9; got != want {
		t.Errorf("default_failure_threshold 未被读走：got %v（原 %v）want %v", got, beforeThreshold, want)
	}
	if got, want := llmpkg.CircuitOpenDuration(), 120*time.Second; got != want {
		t.Errorf("default_circuit_open_duration 未被读走：got %v（原 %v）want %v", got, beforeOpen, want)
	}
	if got, want := llmpkg.HealthCheckTimeout(), 11*time.Second; got != want {
		t.Errorf("default_health_check_timeout 未被读走：got %v（原 %v）want %v", got, beforeCheckTimeout, want)
	}
	if got, want := llmpkg.VoteAgreementThreshold(), 0.62; got != want {
		t.Errorf("vote_agreement_threshold 未被读走：got %v（原 %v）want %v", got, beforeVote, want)
	}
	if got, want := llmpkg.DefaultHTTPTimeout(), 95*time.Second; got != want {
		t.Errorf("default_http_timeout 未被读走：got %v（原 %v）want %v", got, beforeHTTP, want)
	}
	if got, want := llmpkg.DBSinkStopDeadline(), 6*time.Second; got != want {
		t.Errorf("db_sink_stop_deadline 未被读走：got %v（原 %v）want %v", got, beforeSink, want)
	}

	// 整份策略 JSON 也要真的被读走：这条连线是参数中心与 LoadPolicy 之间唯一的桥，
	// 少接一条的话运维在管理台改了候选表，运行期仍按内置默认跑，且不会有任何报错。
	const customPolicyJSON = `{"config":{"degraded_latency_ms":4321},"scenarios":{"sop_reply":["only-in-param-center"]}}`
	if err := svc.UpdateValue(ctx, "agent_llm", "provider_failover_policy", customPolicyJSON, 1); err != nil {
		t.Fatalf("改 agent_llm.provider_failover_policy 失败：%v", err)
	}
	policy := llmpkg.NewProviderFailover(nil).LoadPolicy(ctx)
	if got := policy.Scenarios["sop_reply"]; len(got) != 1 || got[0] != "only-in-param-center" {
		t.Errorf("provider_failover_policy 未被读走：scenarios[sop_reply] = %v，期望 [only-in-param-center]", got)
	}
	if got, want := policy.Config.DegradedLatencyMs, int64(4321); got != want {
		t.Errorf("provider_failover_policy 未被读走：degraded_latency_ms = %d，期望 %d", got, want)
	}
	// 单条旋钮在整份 JSON 缺字段时仍由常量/参数兜底：这里只改了两个字段，其余必须保持默认。
	if got, want := policy.Config.HealthCheckPath, "/health"; got != want {
		t.Errorf("未覆盖的 health_check_path 应回落默认：got %q want %q", got, want)
	}
}

// TestWireAgentLLMConfigParamsWithoutDB 参数表不存在时（没跑迁移、或装配早于建表），
// 接线必须仍把各点位托在代码兜底值上，而不是返回一个 nil 函数在首次请求时炸掉。
func TestWireAgentLLMConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetAgentLLMProviders(t)

	if got := WireAgentLLMConfigParams(); len(got) != len(wantAgentLLMWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantAgentLLMWired))
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"default_health_check_interval", llmpkg.HealthCheckInterval(), llmpkg.DefaultHealthCheckInterval},
		{"default_circuit_open_duration", llmpkg.CircuitOpenDuration(), llmpkg.DefaultCircuitOpenDuration},
		{"default_health_check_timeout", llmpkg.HealthCheckTimeout(), llmpkg.DefaultHealthCheckTimeout},
		{"default_http_timeout", llmpkg.DefaultHTTPTimeout(), llmpkg.DefaultLLMHTTPTimeout},
		{"db_sink_stop_deadline", llmpkg.DBSinkStopDeadline(), llmpkg.DefaultDBSinkStopDeadline},
	} {
		if tc.got != tc.want {
			t.Errorf("无库时 %s = %v，期望兜底 %v", tc.name, tc.got, tc.want)
		}
	}
	if got, want := llmpkg.FailureThreshold(), llmpkg.DefaultFailureThreshold; got != want {
		t.Errorf("无库时 default_failure_threshold = %v，期望兜底 %v", got, want)
	}
	if got, want := llmpkg.VoteAgreementThreshold(), llmpkg.DefaultVoteAgreementThreshold; got != want {
		t.Errorf("无库时 vote_agreement_threshold = %v，期望兜底 %v", got, want)
	}
}
