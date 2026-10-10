package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	geosvc "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/monitor"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantTelemetryWorkflowWired 是本批接线的 8 条。写死而不是从 DefaultParamDefs 反推：
// 反推的话，键名拼错也会被算进本组，漏接的那条就永远没人发现。
var wantTelemetryWorkflowWired = []string{
	"telemetry.node_health_window",
	"telemetry.trace_sink_buffer",
	"telemetry.geo_position_window",
	"telemetry.feature_flag_poll_interval",
	"workflow.max_subflow_depth",
	"workflow.max_workflow_steps",
	"workflow.max_running_per_sop",
	"workflow.max_executed_node_trace",
}

func resetTelemetryWorkflowProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		monitor.SetNodeHealthWindowProvider(nil)
		llm.SetDBSinkBufferSizeProvider(nil)
		geosvc.SetPositionDedupWindowProvider(nil)
		featureflag.SetPollIntervalProvider(nil)
		service.SetMaxSubflowDepthProvider(nil)
		service.SetMaxWorkflowStepsProvider(nil)
		service.SetMaxRunningPerSOPProvider(nil)
		service.SetMaxExecutedNodeTraceProvider(nil)
	})
}

func newTelemetryWorkflowParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetTelemetryWorkflowProviders(t)
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

func TestWireTelemetryWorkflowConfigParamsKeys(t *testing.T) {
	resetTelemetryWorkflowProviders(t)
	got := WireTelemetryWorkflowConfigParams()
	if len(got) != len(wantTelemetryWorkflowWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantTelemetryWorkflowWired), got, wantTelemetryWorkflowWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantTelemetryWorkflowWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantTelemetryWorkflowWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次", k)
		}
		seen[k] = true
	}
}

func TestWiredTelemetryWorkflowKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantTelemetryWorkflowWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在", k)
		}
	}
}

func TestWireTelemetryWorkflowConfigParamsReachesReaders(t *testing.T) {
	svc := newTelemetryWorkflowParamFixture(t)
	ctx := context.Background()

	beforeWindow := monitor.NodeHealthWindow()
	beforeBuffer := llm.DBSinkBufferSize()
	beforeDedup := geosvc.ProbePositionDedupWindow()
	beforePoll := featureflag.PollInterval()
	beforeDepth := service.MaxSubflowDepth()
	beforeSteps := service.ProbeMaxWorkflowSteps()
	beforeRunning := service.ProbeMaxRunningPerSOP()
	beforeTrace := service.ProbeMaxExecutedNodeTrace()

	WireTelemetryWorkflowConfigParams()

	for _, tc := range []struct{ group, key, val string }{
		{"telemetry", "node_health_window", "172800"},
		{"telemetry", "trace_sink_buffer", "4096"},
		{"telemetry", "geo_position_window", "40"},
		{"telemetry", "feature_flag_poll_interval", "15"},
		{"workflow", "max_subflow_depth", "9"},
		{"workflow", "max_workflow_steps", "2000"},
		{"workflow", "max_running_per_sop", "77"},
		{"workflow", "max_executed_node_trace", "333"},
	} {
		if err := svc.UpdateValue(ctx, tc.group, tc.key, tc.val, 1); err != nil {
			t.Fatalf("改 %s.%s 失败：%v", tc.group, tc.key, err)
		}
	}

	for _, tc := range []struct {
		name      string
		got, want any
		was       any
	}{
		{"node_health_window", monitor.NodeHealthWindow(), 48 * time.Hour, beforeWindow},
		{"trace_sink_buffer", llm.DBSinkBufferSize(), 4096, beforeBuffer},
		{"geo_position_window", geosvc.ProbePositionDedupWindow(), 40, beforeDedup},
		{"feature_flag_poll_interval", featureflag.PollInterval(), 15 * time.Second, beforePoll},
		{"max_subflow_depth", service.MaxSubflowDepth(), 9, beforeDepth},
		{"max_workflow_steps", service.ProbeMaxWorkflowSteps(), 2000, beforeSteps},
		{"max_running_per_sop", service.ProbeMaxRunningPerSOP(), 77, beforeRunning},
		{"max_executed_node_trace", service.ProbeMaxExecutedNodeTrace(), 333, beforeTrace},
	} {
		if tc.got != tc.want {
			t.Errorf("%s 未被读走：got %v（原 %v）want %v", tc.name, tc.got, tc.was, tc.want)
		}
	}
}

func TestWireTelemetryWorkflowConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetTelemetryWorkflowProviders(t)

	if got := WireTelemetryWorkflowConfigParams(); len(got) != len(wantTelemetryWorkflowWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d", len(got), len(wantTelemetryWorkflowWired))
	}
	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{"node_health_window", monitor.NodeHealthWindow(), monitor.DefaultNodeHealthWindow},
		{"trace_sink_buffer", llm.DBSinkBufferSize(), llm.DefaultDBSinkBufferSize},
		{"geo_position_window", geosvc.ProbePositionDedupWindow(), geosvc.DefaultPositionDedupWindow},
		{"feature_flag_poll_interval", featureflag.PollInterval(), featureflag.DefaultPollInterval},
		{"max_subflow_depth", service.MaxSubflowDepth(), service.DefaultMaxSubflowDepth},
		{"max_workflow_steps", service.ProbeMaxWorkflowSteps(), service.DefaultMaxWorkflowSteps},
		{"max_running_per_sop", service.ProbeMaxRunningPerSOP(), service.DefaultMaxRunningPerSOP},
		{"max_executed_node_trace", service.ProbeMaxExecutedNodeTrace(), service.DefaultMaxExecutedNodeTrace},
	} {
		if tc.got != tc.want {
			t.Errorf("无库时 %s = %v，期望兜底 %v", tc.name, tc.got, tc.want)
		}
	}
}
