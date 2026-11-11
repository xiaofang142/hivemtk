package service

// connector_intent_e2e_test.go §3.12 刀3：preset→意图绑定层联通 E2E。
//
// 全链：意图 → 真实绑定层（IntentToolChainService+fakeChainRepo）→ 动态桥接工具
// （connector preset 桥接）→ 真实 ToolExecutor 装饰器链 → fake 上游（httptest，
// 连接器执行不真发外网）。双样本+负例三件套：
//   样本1 fakeMart.query_order（generic 方言，虚构平台零代码）；
//   样本2 courier_mirror.track（generic 方言，快递镜像轨迹）；
//   负例1 未注册方言的 preset 从未成为工具 → 绑定引用即 not found，零外发；
//   负例2 SSRF：strict Executor 桥接的工具对环回基址执行即拒，零外发；
//   负例3 能力标签缺失端点不桥接 → 绑定引用即 not found，零外发。
//
// preset 来源：E2E 用**显式 preset 参数**走同一桥接函数（全局 preset 注册表为
// 进程级单例，注册面本身由 connector 包 TestBridge_SanitizeAndDuplicatePreset 与
// 本文件 TestConnectorE2E_GlobalRegisteredPresetBecomesBindable 钉住）。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/connector"
	"hivemtk-user/internal/model"
)

// ---------- 夹具 ----------

type e2eUpstream struct {
	srv  *httptest.Server
	hits *atomic.Int32
}

func newE2EUpstream(t *testing.T, handler http.HandlerFunc) *e2eUpstream {
	t.Helper()
	up := &e2eUpstream{hits: &atomic.Int32{}}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(up.srv.Close)
	return up
}

func (u *e2eUpstream) assertHits(t *testing.T, want int32) {
	t.Helper()
	if got := u.hits.Load(); got != want {
		t.Fatalf("上游命中=%d want %d", got, want)
	}
}

func e2eFakeMartPreset(baseURL string) connector.ConnectorPreset {
	// 样本1：fakeMart——虚构平台，纯 preset 数据（零平台专属代码）。
	return connector.ConnectorPreset{
		Name:   "e2e_fakemart",
		Config: connector.ConnectorConfig{BaseURL: baseURL, Provider: "generic", Auth: connector.ConnectorAuth{Type: "bearer", Key: "k"}},
		Endpoints: []connector.EndpointConfig{{
			Name: "query_order", Method: "GET", Path: "/api/v1/orders",
			Capabilities: []string{"query_order"},
			ParamMap:     map[string]string{"order_id": "platform_order_id"},
		}},
	}
}

func e2eCourierMirrorPreset(baseURL string) connector.ConnectorPreset {
	// 样本2：快递镜像——GET /api/track?carrier=&no=。
	return connector.ConnectorPreset{
		Name:   "e2e_courier_mirror",
		Config: connector.ConnectorConfig{BaseURL: baseURL, Provider: "generic", Auth: connector.ConnectorAuth{Type: "bearer", Key: "k"}},
		Endpoints: []connector.EndpointConfig{{
			Name: "track", Method: "GET", Path: "/api/track",
			Capabilities: []string{"query_express"},
			ParamMap:     map[string]string{"carrier": "carrier", "tracking_no": "no"},
		}},
	}
}

func e2eGhostPreset(baseURL string) connector.ConnectorPreset {
	// 负例1：方言未注册的 preset（桥接期即被拒；上游永不命中）。
	return connector.ConnectorPreset{
		Name:      "e2e_ghost_shop",
		Config:    connector.ConnectorConfig{BaseURL: baseURL, Provider: "no_such_dialect_e2e"},
		Endpoints: []connector.EndpointConfig{{Name: "query_x", Method: "GET", Path: "/x", Capabilities: []string{"query_x"}}},
	}
}

func e2eNoCapPreset(baseURL string) connector.ConnectorPreset {
	// 负例3：能力标签缺失端点（不桥接；上游永不命中）。
	return connector.ConnectorPreset{
		Name:      "e2e_nocap_shop",
		Config:    connector.ConnectorConfig{BaseURL: baseURL, Provider: "generic"},
		Endpoints: []connector.EndpointConfig{{Name: "no_caps_ep", Method: "GET", Path: "/y", Capabilities: nil}},
	}
}

func e2eSSRFPreset(baseURL string) connector.ConnectorPreset {
	// 负例2：SSRF 环回基址 preset（桥接成功，执行期 strict 口径拒）。
	return connector.ConnectorPreset{
		Name:      "e2e_ssrf_shop",
		Config:    connector.ConnectorConfig{BaseURL: baseURL, Provider: "generic"},
		Endpoints: []connector.EndpointConfig{{Name: "query_leak", Method: "GET", Path: "/leak", Capabilities: []string{"query_leak"}}},
	}
}

// e2eFixtures 双样本 + 负例的整链装配：桥接→本地工具目录→真实 ToolExecutor
// （装饰器链同生产形态）→真实绑定层 service。
type e2eFixtures struct {
	fakeMart *e2eUpstream
	mirror   *e2eUpstream
	ssrf     *e2eUpstream
	ghost    *e2eUpstream // 未注册方言/能力缺失 preset 的上游：命中数恒 0 即负例证据
	nocap    *e2eUpstream

	svc      *IntentToolChainService
	registry *tooluse.ToolRegistry
	executor *tooluse.ToolExecutor
	audit    *tooluse.MemoryAuditLogger
}

func newE2EFixtures(t *testing.T) *e2eFixtures {
	t.Helper()
	fx := &e2eFixtures{}

	fx.fakeMart = newE2EUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/orders" && r.URL.Query().Get("platform_order_id") == "FM-1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"order_id":"FM-1","status":"shipped","tracking":"SF123"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	fx.mirror = newE2EUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/track" && r.URL.Query().Get("no") == "SF123" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tracks":[{"time":"09:00","status":"运输中","location":"上海","description":"已发出"}]}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})
	fx.ssrf = newE2EUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	fx.ghost = newE2EUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	fx.nocap = newE2EUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })

	// 桥接（同一生产桥接函数）：正向样本经测试 seam 放行环回；
	// SSRF 负例工具用生产 strict Executor 桥接（执行期才拒）。
	permissive := connector.NewExecutorWithValidator(func(string) error { return nil })
	tools, skipped, presetErrs := connector.BridgePresetsWithExecutor(permissive,
		e2eFakeMartPreset(fx.fakeMart.srv.URL),
		e2eCourierMirrorPreset(fx.mirror.srv.URL),
		e2eGhostPreset(fx.ghost.srv.URL),
		e2eNoCapPreset(fx.nocap.srv.URL),
	)
	// ghost（方言未注册）必须被拒；nocap（能力标签缺失）必须跳过。
	if len(presetErrs) != 1 || !e2eContains(presetErrs[0], "e2e_ghost_shop") {
		t.Fatalf("ghost preset 应因方言未注册被拒: %v", presetErrs)
	}
	if len(skipped) != 1 || skipped[0].Preset != "e2e_nocap_shop" {
		t.Fatalf("nocap 端点应被跳过: %v", skipped)
	}
	strictTools, _, _ := connector.BridgePresetsWithExecutor(connector.NewExecutor(), e2eSSRFPreset(fx.ssrf.srv.URL))
	if len(strictTools) != 1 {
		t.Fatalf("ssrf preset 应桥接出 1 个工具（执行期才拒）, got %d", len(strictTools))
	}

	// 注册进工具目录 + 真实 ToolExecutor（装饰器链同生产形态）。
	fx.registry = tooluse.NewToolRegistry()
	for _, tl := range append(tools, strictTools...) {
		if err := fx.registry.Register(tl); err != nil {
			t.Fatalf("注册桥接工具失败: %v", err)
		}
	}
	fx.audit = tooluse.NewMemoryAuditLogger(100)
	fx.executor = tooluse.NewToolExecutor(fx.registry, tooluse.ToolExecutorConfig{
		DefaultTimeout:    3 * time.Second,
		PermissionChecker: tooluse.NoOpPermissionChecker{},
		RateLimiter:       tooluse.NoOpRateLimiter{},
		AuditLogger:       fx.audit,
		CostTracker:       tooluse.NewMemoryCostTracker(),
	})

	// 真实绑定层（fake 仓储；写侧/读侧走同一 service 代码路径）。
	fx.svc = NewIntentToolChainService(newFakeChainRepo())
	return fx
}

func e2eContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// bindAndLookup 走真实绑定层写侧+读侧：Create 校验→落（fake repo）→LookupIntentChain 命中。
func (fx *e2eFixtures) bindAndLookup(t *testing.T, intent, name string, steps ...model.IntentChainStepConfig) *agent_runtime.IntentChainSpec {
	t.Helper()
	if _, err := fx.svc.Create(context.Background(), ChainUpsertInput{
		Name: name, IntentCode: intent, Steps: steps, Enabled: e2ePtr(true),
	}); err != nil {
		t.Fatalf("绑定层写侧拒绝（桥接工具名应可被绑定层消费）: %v", err)
	}
	spec, err := fx.svc.LookupIntentChain(context.Background(), intent)
	if err != nil || spec == nil {
		t.Fatalf("绑定层读侧未命中: spec=%v err=%v", spec, err)
	}
	return spec
}

func e2ePtr(b bool) *bool { return &b }

func (fx *e2eFixtures) runStep(t *testing.T, spec *agent_runtime.IntentChainSpec) tooluse.ExecuteResult {
	t.Helper()
	if len(spec.Steps) != 1 {
		t.Fatalf("绑定 spec 步骤数不符: %+v", spec.Steps)
	}
	return fx.executor.Execute(context.Background(), tooluse.ExecuteRequest{
		ToolName: spec.Steps[0].ToolName,
		Args:     spec.Steps[0].Args,
		ToolCtx:  &tooluse.ToolContext{CallerID: "e2e", AgentID: "e2e-agent"},
	})
}

// ---------- 样本1：fakeMart 意图→绑定→动态工具全链 ----------

func TestConnectorE2E_FakeMartQueryOrder(t *testing.T) {
	fx := newE2EFixtures(t)

	spec := fx.bindAndLookup(t, string(agent_runtime.IntentOrderStatus), "fakeMart 样本",
		model.IntentChainStepConfig{
			ToolName: "connector.e2e_fakemart.query_order",
			Args:     map[string]any{"order_id": "FM-1"},
			Priority: 1, Enabled: true,
		})
	res := fx.runStep(t, spec)
	if res.Err != nil || !res.Success {
		t.Fatalf("fakeMart 全链执行失败: err=%v result=%+v", res.Err, res)
	}
	fx.fakeMart.assertHits(t, 1)
	data, ok := res.Data.(map[string]any)
	if !ok || data["ok"] != true {
		t.Fatalf("统一信封应回填 ToolResult.Data: %+v", res.Data)
	}
	env, _ := data["data"].(map[string]any)
	if env == nil || env["status"] != "shipped" {
		t.Fatalf("平台原始字段应保留在信封 data: %+v", data["data"])
	}
	if fx.audit.Count() == 0 {
		t.Fatal("E2E 执行必须留下审计（治理链在全链上生效）")
	}
}

// ---------- 样本2：快递镜像 意图→绑定→动态工具全链 ----------

func TestConnectorE2E_CourierMirrorTrack(t *testing.T) {
	fx := newE2EFixtures(t)

	spec := fx.bindAndLookup(t, IntentAskService, "快递镜像样本",
		model.IntentChainStepConfig{
			ToolName: "connector.e2e_courier_mirror.track",
			Args:     map[string]any{"carrier": "SF", "tracking_no": "SF123"},
			Priority: 1, Enabled: true,
		})
	res := fx.runStep(t, spec)
	if res.Err != nil || !res.Success {
		t.Fatalf("快递镜像全链执行失败: err=%v result=%+v", res.Err, res)
	}
	fx.mirror.assertHits(t, 1)
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("信封应回填: %+v", res.Data)
	}
	env, _ := data["data"].(map[string]any)
	if env == nil {
		t.Fatalf("轨迹对象应进信封 data: %+v", data)
	}
	tracks, _ := env["tracks"].([]any)
	if len(tracks) != 1 {
		t.Fatalf("轨迹节点应原样透传, got %+v", env["tracks"])
	}
}

// ---------- 全局 preset 注册面 → 绑定层可消费（判据直验） ----------

func TestConnectorE2E_GlobalRegisteredPresetBecomesBindable(t *testing.T) {
	// 全局注册（生产注册面）→ BridgeTools()（遍历全局表）产出工具名，
	// 绑定层写侧接受该 tool_name（消费面契约）。
	name := fmt.Sprintf("e2e_global_%d", time.Now().UnixNano())
	connector.RegisterPreset(connector.ConnectorPreset{
		Name:      name,
		Config:    connector.ConnectorConfig{BaseURL: "https://global-e2e.example.com", Provider: "generic"},
		Endpoints: []connector.EndpointConfig{{Name: "query_thing", Method: "GET", Path: "/t", Capabilities: []string{"query_thing"}}},
	})
	tools, _, errs := connector.BridgeTools() // 生产口径（严格 Executor；仅验存在不执行）
	if len(errs) != 0 {
		t.Fatalf("全局桥接不应有错: %v", errs)
	}
	want := fmt.Sprintf("connector.%s.query_thing", name)
	found := false
	for _, tl := range tools {
		if tl.Name() == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("全局注册的 preset 端点应成为工具 %s", want)
	}

	// 绑定层写侧接受该工具名（意图绑定层消费面）。
	svc := NewIntentToolChainService(newFakeChainRepo())
	if _, err := svc.Create(context.Background(), ChainUpsertInput{
		Name: "全局注册样本", IntentCode: string(agent_runtime.IntentSalesLead),
		Steps:   []model.IntentChainStepConfig{{ToolName: want, Args: map[string]any{"thing": "T-1"}, Priority: 1, Enabled: true}},
		Enabled: e2ePtr(true),
	}); err != nil {
		t.Fatalf("绑定层应接受桥接工具名: %v", err)
	}
}

// ---------- 负例三件套（走真实绑定层） ----------

func TestConnectorE2E_Negative_UnregisteredDialectNoOutflow(t *testing.T) {
	fx := newE2EFixtures(t)

	// 未注册方言的 preset 从未成为工具；绑定层写侧不查注册表（契约如此），
	// 执行面 fail-closed：not found + 零外发。
	spec := fx.bindAndLookup(t, string(agent_runtime.IntentUnknown), "ghost 负例",
		model.IntentChainStepConfig{
			ToolName: "connector.e2e_ghost_shop.query_x",
			Args:     map[string]any{"x": "1"},
			Priority: 1, Enabled: true,
		})
	res := fx.runStep(t, spec)
	if res.Err == nil {
		t.Fatal("未注册方言的工具执行必须失败（不得静默成功）")
	}
	fx.ghost.assertHits(t, 0) // 零外发
}

func TestConnectorE2E_Negative_SSRFRefusedNoOutflow(t *testing.T) {
	fx := newE2EFixtures(t)

	spec := fx.bindAndLookup(t, string(agent_runtime.IntentInquiry), "ssrf 负例",
		model.IntentChainStepConfig{
			ToolName: "connector.e2e_ssrf_shop.query_leak",
			Args:     map[string]any{"q": "secret"},
			Priority: 1, Enabled: true,
		})
	res := fx.runStep(t, spec)
	if res.Err == nil || res.Success {
		t.Fatalf("SSRF 基址执行必须被拒: err=%v res=%+v", res.Err, res)
	}
	fx.ssrf.assertHits(t, 0) // 零外发（校验器在拨号前拦截）
}

func TestConnectorE2E_Negative_MissingCapabilitiesNoOutflow(t *testing.T) {
	fx := newE2EFixtures(t)

	spec := fx.bindAndLookup(t, string(agent_runtime.IntentChitchat), "nocap 负例",
		model.IntentChainStepConfig{
			ToolName: "connector.e2e_nocap_shop.no_caps_ep",
			Args:     map[string]any{"q": "1"},
			Priority: 1, Enabled: true,
		})
	res := fx.runStep(t, spec)
	if res.Err == nil {
		t.Fatal("能力标签缺失端点未桥接，执行必须 not found")
	}
	fx.nocap.assertHits(t, 0) // 零外发
}
