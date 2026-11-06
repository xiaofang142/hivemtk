package service

// llm_routing_test.go AV-04a 验收：租户 provider 名单查询下沉后的 Service 行为。
//
// 本文件是 fake 依赖单测（不连库）：Service 只认 TenantProviderLookup 接口，
// fake 负责统计传入的 tenant/ctx 与调用次数，把「单租户不查依赖、多租户只
// 查自己、一切异常 fail-closed」钉在接口边界上。真实 SQL 作用域与唯一约束
// 见 repository 包 llm_provider_tenant_test.go 与本包 PG 端到端用例。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/tenant"
)

// fakeProviderLookup 记录每次调用的 ctx 与 tenantID，返回预置名单/错误。
type fakeProviderLookup struct {
	mu         sync.Mutex
	calls      int
	lastCtx    context.Context
	lastTenant uint
	names      []string
	err        error
}

func (f *fakeProviderLookup) ListProviderNamesByTenant(ctx context.Context, tenantID uint) ([]string, error) {
	f.mu.Lock()
	f.calls++
	f.lastCtx = ctx
	f.lastTenant = tenantID
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	// 尊重 ctx 取消：取消的请求等同读库失败（由调用方 fail-closed）。
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]string(nil), f.names...), nil
}

func (f *fakeProviderLookup) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newRoutingTestDispatcher 组一个带默认 provider 的 dispatcher，再叠加
// A/B 两租户各自的 provider（Name 全局唯一，DisplayName/Model/BaseURL
// 故意做成相同——模拟易混淆对照）。
func newRoutingTestDispatcher(t *testing.T) *llm.Dispatcher {
	t.Helper()
	d := llm.NewDispatcher(llm.NewLLMService())
	d.AddProvider(llm.ProviderConfig{
		Name: "av04a-a1", DisplayName: "Shared Display", BaseURL: "https://shared.example.com/v1",
		Model: "shared-model", APIKey: "sk-secret-a1", Enabled: true,
	})
	d.AddProvider(llm.ProviderConfig{
		Name: "av04a-z2", DisplayName: "Shared Display", BaseURL: "https://shared.example.com/v1",
		Model: "shared-model", APIKey: "", Enabled: true,
	})
	d.AddProvider(llm.ProviderConfig{
		Name: "av04a-b1", DisplayName: "Shared Display", BaseURL: "https://shared.example.com/v1",
		Model: "shared-model", APIKey: "sk-secret-b1", Enabled: true,
	})
	return d
}

// setMultiTenantForTest 打开多租户模式并在测试结束时还原（沿包内既有模式）。
func setMultiTenantForTest(t *testing.T) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
}

func namesOfModels(in []LLMProviderInfo) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		out = append(out, m.Name)
	}
	return out
}

func assertModelNames(t *testing.T, got []LLMProviderInfo, want ...string) {
	t.Helper()
	names := namesOfModels(got)
	if len(names) != len(want) {
		t.Fatalf("名单不符: got %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("名单不符（应为按 Name 升序）: got %v, want %v", names, want)
		}
	}
}

// ① 多租户 A/B 各自只看到自己的名单；进程内 dispatcher 的其余行
// （含默认 provider 与别租户的 av04a-b1）一律被过滤。
func TestLLMRouting_ListModels_MultiTenantScopedToCaller(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	lookupA := &fakeProviderLookup{names: []string{"av04a-a1", "av04a-z2"}} // 故意乱序
	svcA := NewLLMRoutingService(d, lookupA)
	got := svcA.ListModels(tenant.WithTenantID(context.Background(), 901))
	assertModelNames(t, got, "av04a-a1", "av04a-z2")
	if lookupA.lastTenant != 901 {
		t.Errorf("依赖收到的 tenantID=%d，期望 901", lookupA.lastTenant)
	}
	if lookupA.lastCtx == nil {
		t.Error("依赖未收到 ctx")
	}

	lookupB := &fakeProviderLookup{names: []string{"av04a-b1"}}
	svcB := NewLLMRoutingService(d, lookupB)
	gotB := svcB.ListModels(tenant.WithTenantID(context.Background(), 902))
	assertModelNames(t, gotB, "av04a-b1")
	if lookupB.lastTenant != 902 {
		t.Errorf("B 依赖收到的 tenantID=%d，期望 902", lookupB.lastTenant)
	}
}

// ② 单租户模式既有语义不变：短路，不调依赖，不过滤。
func TestLLMRouting_ListModels_SingleTenantShortCircuits(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	d := newRoutingTestDispatcher(t)

	lookup := &fakeProviderLookup{names: []string{"av04a-a1"}} // 即便依赖给出名单也不该被用
	svc := NewLLMRoutingService(d, lookup)
	got := svc.ListModels(context.Background())

	if lookup.callCount() != 0 {
		t.Fatalf("单租户模式不应查询依赖，实际调用 %d 次", lookup.callCount())
	}
	found := false
	for _, m := range got {
		if m.Name == "av04a-a1" {
			found = true
		}
	}
	if !found {
		t.Errorf("单租户模式应看到全量名单（含新增 provider）: %v", namesOfModels(got))
	}
}

// ③ 缺租户上下文（含 tenant=0，WithTenantID 对 0 视为无效绑定）：
// fail-closed 空名单，且不触发依赖调用。
func TestLLMRouting_ListModels_MissingTenantFailClosed(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	for name, ctx := range map[string]context.Context{
		"无租户绑定":    context.Background(),
		"tenant=0": tenant.WithTenantID(context.Background(), 0),
	} {
		lookup := &fakeProviderLookup{names: []string{"av04a-a1", "av04a-b1"}}
		svc := NewLLMRoutingService(d, lookup)
		got := svc.ListModels(ctx)
		if len(got) != 0 {
			t.Errorf("[%s] 缺租户上下文应 fail-closed 空名单，实际 %v", name, namesOfModels(got))
		}
		if lookup.callCount() != 0 {
			t.Errorf("[%s] 不应触发依赖调用（租户短路在先），实际 %d 次", name, lookup.callCount())
		}
	}
}

// ④ 依赖空名单：当前租户没有任何 provider ⇒ 空列表，绝不退回全平台。
func TestLLMRouting_ListModels_EmptyTenantListStaysEmpty(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	lookup := &fakeProviderLookup{names: nil}
	svc := NewLLMRoutingService(d, lookup)
	got := svc.ListModels(tenant.WithTenantID(context.Background(), 901))
	if len(got) != 0 {
		t.Fatalf("空名单应得空列表（不得退回全平台 %d 个 provider）", len(got))
	}
}

// ⑤ 依赖报错 / ctx 取消：fail-closed 空名单。
func TestLLMRouting_ListModels_LookupErrorFailClosed(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	t.Run("查询错误", func(t *testing.T) {
		lookup := &fakeProviderLookup{err: errors.New("db down")}
		svc := NewLLMRoutingService(d, lookup)
		got := svc.ListModels(tenant.WithTenantID(context.Background(), 901))
		if len(got) != 0 {
			t.Errorf("读库失败应 fail-closed 空名单，实际 %v", namesOfModels(got))
		}
	})

	t.Run("ctx取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(tenant.WithTenantID(context.Background(), 901))
		cancel()
		lookup := &fakeProviderLookup{}
		svc := NewLLMRoutingService(d, lookup)
		got := svc.ListModels(ctx)
		if len(got) != 0 {
			t.Errorf("ctx 取消应 fail-closed 空名单，实际 %v", namesOfModels(got))
		}
	})
}

// ⑥ 缺查询依赖（装配未注入）：多租户下空名单取严，不退回全平台列表。
func TestLLMRouting_ListModels_NilLookupFailClosed(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	svc := NewLLMRoutingService(d, nil)
	got := svc.ListModels(tenant.WithTenantID(context.Background(), 901))
	if len(got) != 0 {
		t.Fatalf("缺查询依赖应得空名单（不得退回全平台 %d 个）", len(got))
	}
}

// ⑦ dispatcher=nil 保留既有空结果语义。
func TestLLMRouting_ListModels_NilDispatcherReturnsNil(t *testing.T) {
	setMultiTenantForTest(t)
	svc := NewLLMRoutingService(nil, &fakeProviderLookup{names: []string{"av04a-a1"}})
	if got := svc.ListModels(tenant.WithTenantID(context.Background(), 901)); got != nil {
		t.Fatalf("dispatcher=nil 应返回 nil，实际 %v", namesOfModels(got))
	}
}

// ⑧ 出口契约：按 Name 升序稳定排序；APIKey 绝不出现在响应；APIKeySet
// 保持布尔语义（配了 Key 为 true，没配为 false）。
func TestLLMRouting_ListModels_ExportContract(t *testing.T) {
	setMultiTenantForTest(t)
	d := newRoutingTestDispatcher(t)

	lookup := &fakeProviderLookup{names: []string{"av04a-z2", "av04a-a1"}} // 乱序进
	svc := NewLLMRoutingService(d, lookup)
	got := svc.ListModels(tenant.WithTenantID(context.Background(), 901))
	assertModelNames(t, got, "av04a-a1", "av04a-z2") // 出口按 Name 升序

	byName := map[string]LLMProviderInfo{}
	for _, m := range got {
		byName[m.Name] = m
		if m.APIKey != "" {
			t.Errorf("provider %s 的 APIKey 泄漏进响应: %q", m.Name, m.APIKey)
		}
	}
	if !byName["av04a-a1"].APIKeySet {
		t.Error("配了 Key 的 provider APIKeySet 应为 true")
	}
	if byName["av04a-z2"].APIKeySet {
		t.Error("没配 Key 的 provider APIKeySet 应为 false")
	}
}

// ⑨ 单租户 + 缺查询依赖：既有行为不变（短路在先，依赖永远不被触碰）。
func TestLLMRouting_ListModels_SingleTenantNilLookupUnchanged(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	d := newRoutingTestDispatcher(t)

	svc := NewLLMRoutingService(d, nil) // 装配未注入依赖也不 panic
	got := svc.ListModels(context.Background())
	found := false
	for _, m := range got {
		if m.Name == "av04a-a1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("单租户模式名单应完整（含新增 provider）: %v", namesOfModels(got))
	}
}
