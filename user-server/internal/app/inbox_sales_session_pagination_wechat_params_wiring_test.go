package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/mcp"
	contentsvc "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/pagination"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	tracelearn "hivemtk-user/internal/service/trace_learning"
)

// wantInboxSalesSessionPaginationWired 是本批接线的 8 条。写死而不是从
// DefaultParamDefs 反推：反推的话，键名拼错也会被算进本组，漏接的那条就永远没人发现。
var wantInboxSalesSessionPaginationWired = []string{
	"inbox_sales.tg_lead_opportunity_threshold",
	"inbox_sales.unified_miner_lead_threshold",
	"inbox_sales.preview_sample_limit",
	"inbox_sales.geo_lead_preview_max_len",
	"session.active_ttl",
	"session.idle_ttl",
	"session.max_delay_seconds",
	"pagination.cursor_page_size",
}

func resetInboxSalesSessionPaginationProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		service.SetTgLeadOpportunityThresholdProvider(nil)
		service.SetUnifiedMinerOpportunityThresholdProvider(nil)
		service.SetPreviewSampleLimitProvider(nil)
		tracelearn.SetInsightMaxLenProvider(nil)
		repository.SetSessionActiveTTLProvider(nil)
		mcp.SetSessionIdleTTLProvider(nil)
		contentsvc.SetMaxFlowDelaySecondsProvider(nil)
		pagination.SetCursorPageSizeProvider(nil)
	})
}

func newInboxSalesSessionPaginationParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetInboxSalesSessionPaginationProviders(t)
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

func TestWireInboxSalesSessionPaginationConfigParamsKeys(t *testing.T) {
	resetInboxSalesSessionPaginationProviders(t)
	got := WireInboxSalesSessionPaginationConfigParams()
	if len(got) != len(wantInboxSalesSessionPaginationWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantInboxSalesSessionPaginationWired), got, wantInboxSalesSessionPaginationWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantInboxSalesSessionPaginationWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantInboxSalesSessionPaginationWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次", k)
		}
		seen[k] = true
	}
}

func TestWiredInboxSalesSessionPaginationKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantInboxSalesSessionPaginationWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在", k)
		}
	}
}

func TestWireInboxSalesSessionPaginationConfigParamsReachesReaders(t *testing.T) {
	svc := newInboxSalesSessionPaginationParamFixture(t)
	ctx := context.Background()

	beforeTg := service.ProbeTgLeadOpportunityThreshold()
	beforeUnified := service.ProbeUnifiedMinerOpportunityThreshold()
	beforePreview := service.ProbePreviewSampleLimit()
	beforeInsight := tracelearn.InsightMaxLen()
	beforeSession := repository.SessionActiveTTL()
	beforeIdle := mcp.SessionIdleTTL()
	beforeDelay := contentsvc.ProbeMaxFlowDelaySeconds()
	beforeCursor := pagination.CursorPageSize()

	WireInboxSalesSessionPaginationConfigParams()

	for _, tc := range []struct{ group, key, val string }{
		{"inbox_sales", "tg_lead_opportunity_threshold", "65"},
		{"inbox_sales", "unified_miner_lead_threshold", "66"},
		{"inbox_sales", "preview_sample_limit", "7"},
		{"inbox_sales", "geo_lead_preview_max_len", "333"},
		{"session", "active_ttl", "172800"},
		{"session", "idle_ttl", "600"},
		{"session", "max_delay_seconds", "45"},
		{"pagination", "cursor_page_size", "555"},
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
		{"tg_lead_opportunity_threshold", service.ProbeTgLeadOpportunityThreshold(), 65, beforeTg},
		{"unified_miner_lead_threshold", service.ProbeUnifiedMinerOpportunityThreshold(), 66, beforeUnified},
		{"preview_sample_limit", service.ProbePreviewSampleLimit(), 7, beforePreview},
		{"geo_lead_preview_max_len", tracelearn.InsightMaxLen(), 333, beforeInsight},
		{"active_ttl", repository.SessionActiveTTL(), 48 * time.Hour, beforeSession},
		{"idle_ttl", mcp.SessionIdleTTL(), 10 * time.Minute, beforeIdle},
		// 种子类型是 duration（秒），折算后应当是 45 秒。
		{"max_delay_seconds", contentsvc.ProbeMaxFlowDelaySeconds(), 45, beforeDelay},
		{"cursor_page_size", pagination.CursorPageSize(), 555, beforeCursor},
	} {
		if tc.got != tc.want {
			t.Errorf("%s 未被读走：got %v（原 %v）want %v", tc.name, tc.got, tc.was, tc.want)
		}
	}
}

func TestWireInboxSalesSessionPaginationConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetInboxSalesSessionPaginationProviders(t)

	if got := WireInboxSalesSessionPaginationConfigParams(); len(got) != len(wantInboxSalesSessionPaginationWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d", len(got), len(wantInboxSalesSessionPaginationWired))
	}
	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{"tg_lead_opportunity_threshold", service.ProbeTgLeadOpportunityThreshold(), service.DefaultTgLeadOpportunityThreshold},
		{"unified_miner_lead_threshold", service.ProbeUnifiedMinerOpportunityThreshold(), service.DefaultUnifiedMinerOpportunityThreshold},
		{"preview_sample_limit", service.ProbePreviewSampleLimit(), service.DefaultPreviewSampleLimit},
		{"geo_lead_preview_max_len", tracelearn.InsightMaxLen(), tracelearn.DefaultInsightMaxLen},
		{"active_ttl", repository.SessionActiveTTL(), repository.DefaultSessionActiveTTL},
		{"idle_ttl", mcp.SessionIdleTTL(), mcp.DefaultSessionIdleTTL},
		{"max_delay_seconds", contentsvc.ProbeMaxFlowDelaySeconds(), contentsvc.DefaultMaxFlowDelaySeconds},
		{"cursor_page_size", pagination.CursorPageSize(), pagination.DefaultCursorPageSize},
	} {
		if tc.got != tc.want {
			t.Errorf("无库时 %s = %v，期望兜底 %v", tc.name, tc.got, tc.want)
		}
	}
}
