package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

type mockProvider struct {
	name        string
	category    tooluse.ToolCategory
	description string
	tools       []tooluse.Tool
	provideErr  error
}

func (p *mockProvider) Name() string                   { return p.name }
func (p *mockProvider) Category() tooluse.ToolCategory { return p.category }
func (p *mockProvider) Description() string            { return p.description }
func (p *mockProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if p.provideErr != nil {
		return nil, p.provideErr
	}
	return p.tools, nil
}

func newMockProviderWithTools(name string, toolCount int) *mockProvider {
	tools := make([]tooluse.Tool, 0, toolCount)
	for i := 0; i < toolCount; i++ {
		tools = append(tools, newMockEchoTool(name+".tool"+string(rune('a'+i))))
	}
	return &mockProvider{
		name:        name,
		category:    tooluse.CategoryBusiness,
		description: "mock provider for testing",
		tools:       tools,
	}
}

func TestProviderRegistry_RegisterAndGet(t *testing.T) {
	registry := tooluse.NewProviderRegistry()
	p := newMockProviderWithTools("test1", 2)

	if err := registry.RegisterProvider(p); err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}
	if !registry.HasProvider("test1") {
		t.Error("HasProvider should return true")
	}
	if registry.Count() != 1 {
		t.Errorf("Count = %d, want 1", registry.Count())
	}

	got, err := registry.GetProvider("test1")
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}
	if got.Name() != "test1" {
		t.Errorf("got.Name = %s, want test1", got.Name())
	}

	if err := registry.RegisterProvider(p); err == nil {
		t.Error("duplicate RegisterProvider should fail")
	}

	if err := registry.UnregisterProvider("test1"); err != nil {
		t.Fatalf("UnregisterProvider failed: %v", err)
	}
	if registry.HasProvider("test1") {
		t.Error("HasProvider should return false after Unregister")
	}
}

func TestProviderRegistry_RegisterNil(t *testing.T) {
	registry := tooluse.NewProviderRegistry()
	if err := registry.RegisterProvider(nil); err == nil {
		t.Error("RegisterProvider(nil) should fail")
	}
}

func TestProviderRegistry_RegisterEmptyName(t *testing.T) {
	registry := tooluse.NewProviderRegistry()
	p := &mockProvider{name: "", category: tooluse.CategoryBusiness, description: "x"}
	if err := registry.RegisterProvider(p); err == nil {
		t.Error("RegisterProvider with empty name should fail")
	}
}

func TestProviderRegistry_ListProvidersOrder(t *testing.T) {
	registry := tooluse.NewProviderRegistry()
	_ = registry.RegisterProvider(&mockProvider{name: "c", category: tooluse.CategoryBusiness})
	_ = registry.RegisterProvider(&mockProvider{name: "a", category: tooluse.CategoryBusiness})
	_ = registry.RegisterProvider(&mockProvider{name: "b", category: tooluse.CategoryBusiness})

	providers := registry.ListProviders()
	if len(providers) != 3 {
		t.Fatalf("ListProviders len = %d, want 3", len(providers))
	}
	if providers[0].Name() != "c" || providers[1].Name() != "a" || providers[2].Name() != "b" {
		t.Errorf("order = %s,%s,%s; want c,a,b",
			providers[0].Name(), providers[1].Name(), providers[2].Name())
	}
}

func TestProviderRegistry_RegisterAll_Success(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	toolRegistry := tooluse.NewToolRegistry()

	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("p1", 3))
	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("p2", 2))

	results, err := providerRegistry.RegisterAll(
		tooluse.ProviderContext{DB: nil, Config: tooluse.ProviderConfig{Enabled: true}},
		toolRegistry,
	)
	if err != nil {
		t.Fatalf("RegisterAll err: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if toolRegistry.Count() != 5 {
		t.Errorf("tool registry Count = %d, want 5", toolRegistry.Count())
	}

	for _, r := range results {
		if r.ToolCount == 0 {
			t.Errorf("provider %s ToolCount = 0", r.ProviderName)
		}
		if r.Err != "" {
			t.Errorf("provider %s Err = %s", r.ProviderName, r.Err)
		}
	}
}

func TestProviderRegistry_RegisterAll_SkippedByConfig(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	toolRegistry := tooluse.NewToolRegistry()

	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("enabled", 2))
	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("disabled", 2))

	ctx := tooluse.ProviderContext{
		DB: nil,
		Config: tooluse.ProviderConfig{
			Enabled: false,
			Custom:  map[string]any{"test": true},
		},
	}
	results, err := providerRegistry.RegisterAll(ctx, toolRegistry)
	if err != nil {
		t.Logf("RegisterAll err (expected when all skipped): %v", err)
	}
	for _, r := range results {
		if !r.Skipped {
			t.Errorf("provider %s should be skipped", r.ProviderName)
		}
	}
	if toolRegistry.Count() != 0 {
		t.Errorf("tool registry should be empty, got %d", toolRegistry.Count())
	}
}

func TestProviderRegistry_RegisterAll_DisabledTools(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	toolRegistry := tooluse.NewToolRegistry()

	p := &mockProvider{
		name:     "p",
		category: tooluse.CategoryBusiness,
		tools: []tooluse.Tool{
			newMockEchoTool("p.keep1"),
			newMockEchoTool("p.skip"),
			newMockEchoTool("p.keep2"),
		},
	}
	_ = providerRegistry.RegisterProvider(p)

	ctx := tooluse.ProviderContext{
		DB: nil,
		Config: tooluse.ProviderConfig{
			Enabled:       true,
			DisabledTools: []string{"p.skip"},
		},
	}
	results, _ := providerRegistry.RegisterAll(ctx, toolRegistry)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ToolCount != 2 {
		t.Errorf("ToolCount = %d, want 2", results[0].ToolCount)
	}
	if len(results[0].SkippedTools) != 1 || results[0].SkippedTools[0] != "p.skip" {
		t.Errorf("SkippedTools = %v, want [p.skip]", results[0].SkippedTools)
	}
	if toolRegistry.Has("p.skip") {
		t.Error("p.skip should not be registered")
	}
	if !toolRegistry.Has("p.keep1") || !toolRegistry.Has("p.keep2") {
		t.Error("p.keep1 and p.keep2 should be registered")
	}
}

func TestProviderRegistry_RegisterAll_ProvideError(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	toolRegistry := tooluse.NewToolRegistry()

	pErr := errors.New("mock provide failure")
	_ = providerRegistry.RegisterProvider(&mockProvider{
		name:       "fail",
		category:   tooluse.CategoryBusiness,
		provideErr: pErr,
	})
	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("ok", 1))

	results, _ := providerRegistry.RegisterAll(
		tooluse.ProviderContext{Config: tooluse.ProviderConfig{Enabled: true}},
		toolRegistry,
	)
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}

	var failResult *tooluse.ProviderRegistrationResult
	for i := range results {
		if results[i].ProviderName == "fail" {
			failResult = &results[i]
			break
		}
	}
	if failResult == nil {
		t.Fatal("fail provider result not found")
	}
	if failResult.Err == "" {
		t.Error("fail provider Err should be non-empty")
	}
	if failResult.ToolCount != 0 {
		t.Errorf("fail provider ToolCount = %d, want 0", failResult.ToolCount)
	}
	if toolRegistry.Count() != 1 {
		t.Errorf("tool registry Count = %d, want 1 (only ok provider)", toolRegistry.Count())
	}
}

func TestProviderRegistry_RegisterAll_NilToolRegistry(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	_, err := providerRegistry.RegisterAll(
		tooluse.ProviderContext{Config: tooluse.ProviderConfig{Enabled: true}},
		nil,
	)
	if err == nil {
		t.Error("RegisterAll with nil toolRegistry should fail")
	}
}

func TestProviderRegistry_Results(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	toolRegistry := tooluse.NewToolRegistry()
	_ = providerRegistry.RegisterProvider(newMockProviderWithTools("p1", 1))

	if len(providerRegistry.Results()) != 0 {
		t.Error("Results should be empty before RegisterAll")
	}

	_, _ = providerRegistry.RegisterAll(
		tooluse.ProviderContext{Config: tooluse.ProviderConfig{Enabled: true}},
		toolRegistry,
	)
	if len(providerRegistry.Results()) != 1 {
		t.Errorf("Results len = %d, want 1", len(providerRegistry.Results()))
	}
}

func TestAutoRegister_BasicFlow(t *testing.T) {
	tooluse.ClearAutoRegisteredProviders()
	defer tooluse.ClearAutoRegisteredProviders()

	p1 := newMockProviderWithTools("auto1", 1)
	p2 := newMockProviderWithTools("auto2", 1)

	tooluse.RegisterToolProvider(p1)
	tooluse.RegisterToolProvider(p2)

	got := tooluse.GetAutoRegisteredProviders()
	if len(got) != 2 {
		t.Fatalf("auto providers len = %d, want 2", len(got))
	}
}

func TestAutoRegister_DuplicateName(t *testing.T) {
	tooluse.ClearAutoRegisteredProviders()
	defer tooluse.ClearAutoRegisteredProviders()

	p1 := newMockProviderWithTools("dup", 1)
	p2 := newMockProviderWithTools("dup", 2)

	tooluse.RegisterToolProvider(p1)
	tooluse.RegisterToolProvider(p2)

	got := tooluse.GetAutoRegisteredProviders()
	if len(got) != 1 {
		t.Errorf("duplicate name should be deduped, got len = %d", len(got))
	}
}

func TestAutoRegister_Nil(t *testing.T) {
	tooluse.ClearAutoRegisteredProviders()
	defer tooluse.ClearAutoRegisteredProviders()

	tooluse.RegisterToolProvider(nil)
	if len(tooluse.GetAutoRegisteredProviders()) != 0 {
		t.Error("nil provider should be ignored")
	}
}

func TestAutoRegister_GetReturnsCopy(t *testing.T) {
	tooluse.ClearAutoRegisteredProviders()
	defer tooluse.ClearAutoRegisteredProviders()

	tooluse.RegisterToolProvider(newMockProviderWithTools("p", 1))
	got := tooluse.GetAutoRegisteredProviders()
	got[0] = nil

	got2 := tooluse.GetAutoRegisteredProviders()
	if got2[0] == nil {
		t.Error("GetAutoRegisteredProviders should return a copy")
	}
}

func TestBuiltinProviders_ProvideToolCount(t *testing.T) {

	cases := []struct {
		name        string
		provider    tooluse.ToolProvider
		expectDBErr bool
		expectCount int
		requireDB   bool
	}{
		{
			name:        "ReachToolProvider",
			provider:    &ReachToolProvider{},
			expectDBErr: true,
			requireDB:   true,
		},
		{
			name:        "PrivateMessageToolProvider",
			provider:    &PrivateMessageToolProvider{},
			expectDBErr: true,
			requireDB:   true,
		},
		{
			name:        "CustomerToolProvider",
			provider:    &CustomerToolProvider{},
			expectDBErr: true,
			requireDB:   true,
		},
		{
			name:        "KnowledgeToolProvider",
			provider:    &KnowledgeToolProvider{},
			expectDBErr: true,
			requireDB:   true,
		},
		{
			name:        "BusinessToolProvider",
			provider:    &BusinessToolProvider{},
			expectDBErr: true,
			requireDB:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tools, err := c.provider.Provide(tooluse.ProviderContext{DB: nil})
			if c.expectDBErr {
				if err == nil {
					t.Errorf("%s.Provide(nil DB) should fail, got tools=%d", c.name, len(tools))
				}
				return
			}
			if err != nil {
				t.Errorf("%s.Provide err: %v", c.name, err)
			}
			if c.expectCount > 0 && len(tools) != c.expectCount {
				t.Errorf("%s tools len = %d, want %d", c.name, len(tools), c.expectCount)
			}
		})
	}
}

func TestBuiltinProviders_MetaData(t *testing.T) {
	cases := []struct {
		name               string
		provider           tooluse.ToolProvider
		expectName         string
		expectCategory     tooluse.ToolCategory
		expectDescNonEmpty bool
	}{
		{"ReachToolProvider", &ReachToolProvider{}, "reach", tooluse.CategoryReach, true},
		{"PrivateMessageToolProvider", &PrivateMessageToolProvider{}, "pm", tooluse.CategoryPrivateMessage, true},
		{"CustomerToolProvider", &CustomerToolProvider{}, "customer", tooluse.CategoryCustomer, true},
		{"KnowledgeToolProvider", &KnowledgeToolProvider{}, "knowledge", tooluse.CategoryKnowledge, true},
		{"BusinessToolProvider", &BusinessToolProvider{}, "business", tooluse.CategoryBusiness, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.provider.Name() != c.expectName {
				t.Errorf("Name = %s, want %s", c.provider.Name(), c.expectName)
			}
			if c.provider.Category() != c.expectCategory {
				t.Errorf("Category = %s, want %s", c.provider.Category(), c.expectCategory)
			}
			if c.expectDescNonEmpty && c.provider.Description() == "" {
				t.Error("Description should be non-empty")
			}
		})
	}
}

func TestAutoRegister_IntegrationWithProviderRegistry(t *testing.T) {
	tooluse.ClearAutoRegisteredProviders()
	defer tooluse.ClearAutoRegisteredProviders()

	thirdPartyProvider := newMockProviderWithTools("thirdparty", 2)
	tooluse.RegisterToolProvider(thirdPartyProvider)

	providerRegistry := tooluse.NewProviderRegistry()
	for _, p := range tooluse.GetAutoRegisteredProviders() {
		if err := providerRegistry.RegisterProvider(p); err != nil {
			t.Errorf("RegisterProvider %s failed: %v", p.Name(), err)
		}
	}

	toolRegistry := tooluse.NewToolRegistry()
	results, _ := providerRegistry.RegisterAll(
		tooluse.ProviderContext{Config: tooluse.ProviderConfig{Enabled: true}},
		toolRegistry,
	)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ProviderName != "thirdparty" {
		t.Errorf("ProviderName = %s, want thirdparty", results[0].ProviderName)
	}
	if results[0].ToolCount != 2 {
		t.Errorf("ToolCount = %d, want 2", results[0].ToolCount)
	}
	if toolRegistry.Count() != 2 {
		t.Errorf("tool registry Count = %d, want 2", toolRegistry.Count())
	}
}

func TestProviderRegistry_ConcurrentRegister(t *testing.T) {
	providerRegistry := tooluse.NewProviderRegistry()
	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := newMockProviderWithTools("p"+string(rune('a'+idx)), 1)
			if err := providerRegistry.RegisterProvider(p); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	errCount := 0
	for err := range errs {
		if err != nil {
			errCount++
			t.Logf("concurrent register err: %v", err)
		}
	}
	if errCount > 0 {
		t.Errorf("concurrent register err count = %d, want 0", errCount)
	}
}

func TestInitBuiltinToolProviders_RegistersAll(t *testing.T) {
	registry := tooluse.NewProviderRegistry()
	initBuiltinToolProviders(registry)

	// 6=reach/pm/customer/knowledge/business+connector_bridge（§3.12 刀1：
	// 连接器 preset 桥接器进内置 Provider 花名册）。
	if registry.Count() != 6 {
		t.Errorf("Count = %d, want 6", registry.Count())
	}
	expected := []string{"reach", "pm", "customer", "knowledge", "business", "connector_bridge"}
	for _, name := range expected {
		if !registry.HasProvider(name) {
			t.Errorf("provider %s not registered", name)
		}
	}
}

func TestProviderContext_ZeroValue(t *testing.T) {
	var ctx tooluse.ProviderContext
	if ctx.DB != nil {
		t.Error("zero value DB should be nil")
	}
	if ctx.Config.Enabled {
		t.Error("zero value Enabled should be false")
	}
}

var _ tooluse.ToolProvider = (*mockProvider)(nil)

var _ tooluse.ToolProvider = (*ReachToolProvider)(nil)
var _ tooluse.ToolProvider = (*PrivateMessageToolProvider)(nil)
var _ tooluse.ToolProvider = (*CustomerToolProvider)(nil)
var _ tooluse.ToolProvider = (*KnowledgeToolProvider)(nil)
var _ tooluse.ToolProvider = (*BusinessToolProvider)(nil)

var _ = context.Background
