package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	geodto "hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
)

const ourDomain = "https://oursite.example.com/post"

type fakeProbe struct{}

func (f *fakeProbe) Name() string { return "fake" }
func (f *fakeProbe) Probe(_ context.Context, query string) (*ProbeResult, error) {
	return &ProbeResult{
		Engine: "fake", Query: query,
		Response:  "回答（含品牌）",
		Citations: []Citation{{URL: ourDomain}, {URL: "https://rival-source.example.com/a"}},
	}, nil
}

func TestNewDefaultSearchProbe_NoopFailClosed(t *testing.T) {
	t.Setenv("GEO_SEARCH_PROBE_URL", "")
	p := NewDefaultSearchProbe()
	if _, err := p.Probe(context.Background(), "q"); err == nil {
		t.Fatal("未配置探针必须显式报错（红队 F1：禁止 LLM 模拟冒充真实引擎）")
	}
}

type stubChainRepo struct{ rows []*model.GeoQueryChain }

func (r *stubChainRepo) Append(_ context.Context, c *model.GeoQueryChain) error {
	r.rows = append(r.rows, c)
	return nil
}
func (r *stubChainRepo) ListByChain(_ context.Context, _ string) ([]*model.GeoQueryChain, error) {
	return r.rows, nil
}
func (r *stubChainRepo) CountByChain(_ context.Context, _ string) (int64, error) {
	return int64(len(r.rows)), nil
}
func (r *stubChainRepo) ListByOneID(_ context.Context, _ string) ([]*model.GeoQueryChain, error) {
	return r.rows, nil
}
func (r *stubChainRepo) CountToday(_ context.Context) (int64, error) { return 0, nil }

type stubTaskRepo struct{ rows []*model.GeoContentTask }

func (r *stubTaskRepo) Create(_ context.Context, t *model.GeoContentTask) error {
	r.rows = append(r.rows, t)
	return nil
}
func (r *stubTaskRepo) ListPending(_ context.Context, _ int) ([]*model.GeoContentTask, error) {
	return r.rows, nil
}
func (r *stubTaskRepo) MarkDone(_ context.Context, _ string) error { return nil }
func (r *stubTaskRepo) CountByStatus(_ context.Context, status string) (int64, error) {
	var n int64
	for _, t := range r.rows {
		if t.Status == status {
			n++
		}
	}
	return n, nil
}

func newWFWithDecisionChain(t *testing.T, brand string) (*WorkflowService, *stubChainRepo, *stubTaskRepo) {
	t.Helper()
	s := &WorkflowService{executors: map[string]StepExecutor{}}
	chain := &stubChainRepo{}
	tasks := &stubTaskRepo{}
	s.RegisterDecisionChainExecutors(DecisionChainDeps{
		Probe: &fakeProbe{}, ChainRepo: chain, TaskRepo: tasks, Brand: brand,
	})
	return s, chain, tasks
}

func TestQueryProbeExecutor_EndToEnd(t *testing.T) {
	s, chain, _ := newWFWithDecisionChain(t, "ourbrand")
	exec := s.executors["query_probe"]
	if exec == nil {
		t.Fatal("query_probe 未注册")
	}
	out, err := exec(context.Background(), map[string]interface{}{
		"keyword": "CRM 选型",
		"intent":  "对比",
	})
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var parsed struct {
		ChainID      string         `json:"chain_id"`
		CitedDomains map[string]int `json:"cited_domains"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("输出非法 JSON: %v\n%s", err, out)
	}
	if len(chain.rows) == 0 {
		t.Fatal("探针结果未写入思维链")
	}
	row := chain.rows[0]
	if row.Source != "probe" || row.Intent != "对比" || row.ChainID == "" {
		t.Errorf("思维链行字段异常: %+v", row)
	}
	if len(parsed.CitedDomains) == 0 {
		t.Error("应聚合出被引域")
	}
	if !strings.Contains(row.CitedURLs, "oursite.example.com") &&
		!strings.Contains(row.CitedURLs, "rival-source.example.com") {
		t.Errorf("被引 URL 应落库: %q", row.CitedURLs)
	}
}

func TestAttributionAndGapFill_Pipeline(t *testing.T) {
	s, _, tasks := newWFWithDecisionChain(t, "ourbrand")

	probeJSON, _ := json.Marshal(map[string]any{
		"chain_id":      "c1",
		"cited_domains": map[string]int{"oursite.example.com": 3, "rival.example.com": 2},
	})
	attrExec := s.executors["source_attribution"]
	out, err := attrExec(context.Background(), map[string]interface{}{
		"keyword":     "CRM",
		"intent":      "对比",
		"our_domains": "oursite.example.com",
		"_step_results": map[string]*geodto.StepResult{
			"qp": {StepType: "query_probe", Result: string(probeJSON)},
		},
	})
	if err != nil {
		t.Fatalf("归因失败（upstream 契约断裂）: %v", err)
	}
	var attr struct {
		CoverageRate float64          `json:"coverage_rate"`
		CoverageGap  []map[string]any `json:"coverage_gap"`
	}
	if err := json.Unmarshal([]byte(out), &attr); err != nil {
		t.Fatalf("归因输出非法 JSON: %v", err)
	}
	if attr.CoverageRate != 0.5 {
		t.Errorf("期望覆盖率 0.5，得到 %v", attr.CoverageRate)
	}
	foundRival := false
	for _, g := range attr.CoverageGap {
		if g["domain"] == "rival.example.com" {
			foundRival = true
		}
	}
	if !foundRival {
		t.Fatalf("缺口应含 rival.example.com: %+v", attr.CoverageGap)
	}

	fillExec := s.executors["content_gap_fill"]
	gapJSON, _ := json.Marshal(attr)
	if _, err := fillExec(context.Background(), map[string]interface{}{
		"keyword": "CRM", "intent": "对比",
		"_step_results": map[string]*geodto.StepResult{
			"sa": {StepType: "source_attribution", Result: string(gapJSON)},
		},
	}); err != nil {
		t.Fatalf("补位失败: %v", err)
	}
	if len(tasks.rows) != 1 {
		t.Fatalf("应创建 1 条补位任务，实际 %d", len(tasks.rows))
	}
	if tasks.rows[0].GapType != "missing_domain" || tasks.rows[0].Status != "pending" {
		t.Errorf("任务字段异常: %+v", tasks.rows[0])
	}
	if !strings.Contains(tasks.rows[0].Detail, "rival.example.com") {
		t.Errorf("任务详情应包含缺口域: %q", tasks.rows[0].Detail)
	}
}

// TestCaptureLeadExecutor_PortInjection 端口注入与调用透传。
func TestCaptureLeadExecutor_PortInjection(t *testing.T) {
	called := false
	s := &WorkflowService{executors: map[string]StepExecutor{}}
	s.RegisterCaptureLeadExecutor(CaptureLeadFunc(func(_ context.Context, contact, contactType, chainID, intent string) (string, error) {
		called = true
		if contact != "13800138000" || contactType != "phone" || chainID != "c9" || intent != "推荐" {
			t.Errorf("参数透传异常: %s/%s/%s/%s", contact, contactType, chainID, intent)
		}
		return "clue-1", nil
	}))
	exec := s.executors["capture_lead"]
	if exec == nil {
		t.Fatal("capture_lead 未注册")
	}
	out, err := exec(context.Background(), map[string]interface{}{
		"contact": "13800138000", "contact_type": "phone",
		"chain_id": "c9", "intent": "推荐",
	})
	if err != nil || !called {
		t.Fatalf("执行异常: err=%v called=%v", err, called)
	}
	if !strings.Contains(out, "clue-1") {
		t.Errorf("应返回 clue_id: %s", out)
	}
}

// TestCaptureLead_MissingContact 无联系方式时报错
func TestCaptureLead_MissingContact(t *testing.T) {
	s := &WorkflowService{executors: map[string]StepExecutor{}}
	s.RegisterCaptureLeadExecutor(CaptureLeadFunc(func(context.Context, string, string, string, string) (string, error) {
		return "x", nil
	}))
	if _, err := s.executors["capture_lead"](context.Background(), map[string]interface{}{}); err == nil {
		t.Error("缺少 contact 应报错")
	}
}
