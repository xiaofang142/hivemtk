package channelgw

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/pkg/tenant"
)

// 精确合成证据仅限本地管道测试；必须显式绑定租户，且不为未知格造授权。
func localBridgeSubmitContext(t *testing.T, ctx context.Context, product, account string) context.Context {
	t.Helper()
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		t.Fatal("本地正例必须显式提供普通租户上下文")
	}
	key := capregistry.CellKey{
		TenantID: tenantID, ShopID: "local-fixture-shop", Product: product,
		Region: "CN", Action: "message.text.send", Carrier: capregistry.ModeBridge,
	}
	definition, found := capregistry.FindCellDefinition(product, key.Region, key.Action, key.Carrier)
	if !found {
		t.Fatalf("本地正例必须使用已登记的精确格，实际 %+v", key)
	}
	expires := time.Now().Add(time.Hour)
	binding := capregistry.SubmissionBinding{
		Source: capregistry.SubmissionSourceBridge, Platform: product, ResourceID: account, Key: key,
	}
	evidence := capregistry.CellEvidence{
		TenantID: tenantID, ShopID: key.ShopID, Product: product, Region: key.Region, Action: key.Action, Carrier: key.Carrier,
		ConnectionState: capregistry.ConnectionReady, Scopes: definition.RequiredScopes,
		Version: definition.ContractVersion, DocumentationVersion: definition.DocumentationVersion,
		ExpiresAt: &expires, StatePreflightPassed: true,
		Evidence: []capregistry.EvidenceRecord{{Kind: capregistry.EvidenceDocumentation}, {Kind: capregistry.EvidenceImplementation}, {Kind: capregistry.EvidenceRuntime}},
	}
	return capregistry.WithCellEvidence(capregistry.WithSubmissionBinding(ctx, binding), evidence)
}
