package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/pkg/tenant"
)

func TestDialectTool_WriteRequiresShopCellEvidence(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("解析 fake 请求体失败: %v", err)
		}
		if _, leaked := payload["shop_id"]; leaked {
			t.Errorf("权限匹配用 shop_id 不应透传至平台请求体: %v", payload)
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	endpoint := EndpointConfig{
		Name: "change_address", Method: http.MethodPost, Path: "/address",
		Capabilities: []string{"modify_order"},
		Cell:         &CapabilityCellRef{Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: capregistry.ModeAPIAdapter},
	}
	fixtureExecutor := NewExecutorWithValidator(func(string) error { return nil })
	fixtureExecutor.writeAuthorizer = authorizeCapabilityCell
	tool := &dialectTool{base: tooluse.BaseTool{NameVal: "connector.shop.change_address", RiskVal: tooluse.RiskHighWrite},
		preset: ConnectorPreset{Name: "shop", Config: tb06bCfg(srv.URL, "generic")}, endpoint: endpoint,
		exec: fixtureExecutor}
	if _, err := tool.exec.Call(tenant.WithTenantID(context.Background(), 7), tool.preset.Config,
		EndpointConfig{Name: "unscoped_write", Method: http.MethodPost, Path: "/unscoped", Capabilities: []string{"modify_order"}},
		map[string]any{"shop_id": "shop-a"}); err == nil {
		t.Fatal("直接 Executor 写调用缺少能力格时必须拒绝")
	}
	if hits.Load() != 0 {
		t.Fatalf("直接写权限拒绝必须发生在网络出站前，实际请求数 %d", hits.Load())
	}

	if _, err := tool.Execute(tenant.WithTenantID(context.Background(), 7), map[string]any{"shop_id": "shop-a"}); err == nil {
		t.Fatal("无店铺证据不得执行写端点")
	}
	if hits.Load() != 0 {
		t.Fatalf("权限拒绝必须发生在网络出站前，实际请求数 %d", hits.Load())
	}

	key := capregistry.CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: capregistry.ModeAPIAdapter}
	evidence := readyConnectorCellEvidence(key, time.Now())
	ctx := capregistry.WithCellEvidence(tenant.WithTenantID(context.Background(), 7), evidence)
	if _, err := tool.Execute(ctx, map[string]any{"shop_id": "shop-a"}); err != nil {
		t.Fatalf("匹配的本地证据应允许本地 fake 端点通过 gate: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("精确证据放行后预期一个 fake 请求，实际 %d", hits.Load())
	}

	if _, err := tool.Execute(ctx, map[string]any{"shop_id": "shop-b"}); err == nil {
		t.Fatal("请求 shop 与证据绑定不一致时必须拒绝")
	}
	if hits.Load() != 1 {
		t.Fatalf("错绑拒绝不得出站，实际请求数 %d", hits.Load())
	}
}

func TestBridge_SkipsWriteEndpointWithoutCellMetadata(t *testing.T) {
	tools, skipped, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }), ConnectorPreset{
		Name: "cell_metadata_missing", Config: tb06bCfg("https://example.invalid", "generic"),
		Endpoints: []EndpointConfig{{Name: "write", Method: http.MethodPost, Path: "/write", Capabilities: []string{"modify_order"}}},
	})
	if len(tools) != 0 || len(skipped) != 1 || skipped[0].Reason != "写端点缺少已登记店铺动作能力格，fail-closed 不桥接" {
		t.Fatalf("缺少能力格的写端点必须从工具目录 fail-closed 排除，tools=%d skipped=%+v", len(tools), skipped)
	}
}

// POST 即使带 query_ 标签也不能绕过写路径能力格检查。
func TestConnector_PostQueryLabelCannotBypassCellGate(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	_, err := NewExecutorWithValidator(func(string) error { return nil }).Call(
		tenant.WithTenantID(context.Background(), 7), tb06bCfg(srv.URL, "generic"),
		EndpointConfig{Name: "mislabelled", Method: http.MethodPost, Path: "/", Capabilities: []string{"query_order"}},
		map[string]any{"shop_id": "shop-a"},
	)
	if err == nil || hits.Load() != 0 {
		t.Fatalf("无明确只读声明和动作格的 POST 必须拒绝且不出站，err=%v hits=%d", err, hits.Load())
	}
}

// POST + query_ 未显式标记只读且没有店铺动作格时，不得注册成可执行只读工具。
func TestBridge_PostQueryLabelNeedsExplicitReadOnlyDeclaration(t *testing.T) {
	tools, skipped, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }), ConnectorPreset{
		Name: "post_query_without_readonly", Config: tb06bCfg("https://example.invalid", "generic"),
		Endpoints: []EndpointConfig{{Name: "query", Method: http.MethodPost, Path: "/query", Capabilities: []string{"query_order"}}},
	})
	if len(tools) != 0 || len(skipped) != 1 {
		t.Fatalf("未显式声明的 POST 查询不能默认视为只读，tools=%d skipped=%+v", len(tools), skipped)
	}
}

func TestBridge_SkipsWriteEndpointWithUnknownCell(t *testing.T) {
	unknown := &CapabilityCellRef{Product: "douyin_shop", Region: "CN", Action: "unknown.submit", Carrier: capregistry.ModeAPIAdapter}
	tools, skipped, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }), ConnectorPreset{
		Name: "unknown_action", Config: tb06bCfg("https://example.invalid", "generic"),
		Endpoints: []EndpointConfig{{Name: "write", Method: http.MethodPost, Path: "/write", Capabilities: []string{"modify_order"}, Cell: unknown}},
	})
	if len(tools) != 0 || len(skipped) != 1 {
		t.Fatalf("未登记的写动作格不得注册为工具，tools=%d skipped=%+v", len(tools), skipped)
	}
}

func readyConnectorCellEvidence(key capregistry.CellKey, now time.Time) capregistry.CellEvidence {
	expires := now.Add(time.Hour)
	return capregistry.CellEvidence{
		TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region,
		Action: key.Action, Carrier: key.Carrier, ConnectionState: capregistry.ConnectionReady,
		Scopes: []string{"order.address.change"}, Version: "api-v1",
		DocumentationVersion: "design-cell-v1", ExpiresAt: &expires, StatePreflightPassed: true,
		Evidence: []capregistry.EvidenceRecord{{Kind: capregistry.EvidenceDocumentation}, {Kind: capregistry.EvidenceImplementation}, {Kind: capregistry.EvidenceRuntime}},
	}
}

// 仅供本地 fake HTTP 夹具；此声明不代表平台授权或真实写入证据。
func localWriteCellRef() *CapabilityCellRef {
	return &CapabilityCellRef{Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: "api_adapter"}
}

type scopeCountingDialect struct{ builds atomic.Int32 }

func (*scopeCountingDialect) ID() string { return "ec01-system-scope" }
func (d *scopeCountingDialect) BuildRequest(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, _ map[string]any) (*http.Request, error) {
	d.builds.Add(1)
	return http.NewRequestWithContext(ctx, ep.Method, cfg.BaseURL+ep.Path, nil)
}
func (*scopeCountingDialect) NormalizeResponse(_ []byte, _ int) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

type scopeCountingTransport struct{ calls atomic.Int32 }

func (tr *scopeCountingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.calls.Add(1)
	return nil, errors.New("本地拒绝用例禁止网络传输")
}

// 系统范围即使带完全匹配的合成授权也不得构造写请求或尝试传输；只读路径保持原行为。
func TestConnector_SystemScopeCannotWriteWithMatchingEvidence(t *testing.T) {
	dialect := &scopeCountingDialect{}
	registryMu.Lock()
	previous, existed := dialects[dialect.ID()]
	dialects[dialect.ID()] = dialect
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		if existed {
			dialects[dialect.ID()] = previous
		} else {
			delete(dialects, dialect.ID())
		}
	})
	transport := &scopeCountingTransport{}
	exec := NewExecutorWithValidator(func(string) error { return nil })
	exec.http.Transport = transport
	key := capregistry.CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: capregistry.ModeAPIAdapter}
	ctx := capregistry.WithCellEvidence(tenant.WithSystemScope(tenant.WithTenantID(context.Background(), 7)), readyConnectorCellEvidence(key, time.Now()))
	_, err := exec.Call(ctx, tb06bCfg("https://example.invalid", dialect.ID()), EndpointConfig{
		Name: "write", Method: http.MethodPost, Path: "/write", Capabilities: []string{"modify_order"}, Cell: localWriteCellRef(),
	}, map[string]any{"shop_id": key.ShopID})
	if !errors.Is(err, ErrCapabilityCellDenied) || dialect.builds.Load() != 0 || transport.calls.Load() != 0 {
		t.Fatalf("系统范围写入必须在构造请求前拒绝，err=%v builds=%d transport=%d", err, dialect.builds.Load(), transport.calls.Load())
	}
	_, _ = exec.Call(ctx, tb06bCfg("https://example.invalid", dialect.ID()), EndpointConfig{
		Name: "read", Method: http.MethodGet, Path: "/read", Capabilities: []string{"query_order"},
	}, nil)
	if dialect.builds.Load() != 1 || transport.calls.Load() != 1 {
		t.Fatalf("系统范围只读调用应保留原路径，builds=%d transport=%d", dialect.builds.Load(), transport.calls.Load())
	}
}

func authorizeCapabilityCell(ctx context.Context, ref *CapabilityCellRef, args map[string]any) error {
	if !validCapabilityCellRef(ref) {
		return fmt.Errorf("%w: capability cell metadata missing", ErrCapabilityCellDenied)
	}
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return fmt.Errorf("%w: tenant context missing", ErrCapabilityCellDenied)
	}
	shopArgument := ref.ShopArgument
	if shopArgument == "" {
		shopArgument = "shop_id"
	}
	shopID, ok := args[shopArgument].(string)
	if !ok || strings.TrimSpace(shopID) == "" {
		return fmt.Errorf("%w: shop binding missing", ErrCapabilityCellDenied)
	}
	evidence, ok := capregistry.CellEvidenceFromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: shop evidence missing", ErrCapabilityCellDenied)
	}
	key := capregistry.CellKey{
		TenantID: tenantID, ShopID: shopID, Product: ref.Product, Region: ref.Region,
		Action: ref.Action, Carrier: ref.Carrier,
	}
	resolution := capregistry.ResolveCell(key, &evidence, time.Now().UTC())
	if !resolution.ProductionAllowed {
		return fmt.Errorf("%w: %s", ErrCapabilityCellDenied, resolution.ReasonCode)
	}
	return nil
}
