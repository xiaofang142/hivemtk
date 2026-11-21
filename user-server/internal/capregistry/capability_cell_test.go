package capregistry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResolveCell_RequiresShopEvidenceAndExactProductAction(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	key := CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: "api_adapter"}

	// Experimental 静态登记只定义候选格，不会单独授予店铺写权限。
	missing := ResolveCell(key, nil, now)
	if missing.ProductionAllowed || missing.ReasonCode != "shop_evidence_missing" || missing.ConnectionState != ConnectionUnconfigured {
		t.Fatalf("无店铺证据必须保持阻塞，实际 %+v", missing)
	}

	evidence := readyCellEvidence(key, now)
	resolved := ResolveCell(key, &evidence, now)
	if !resolved.ProductionAllowed || resolved.Status != "ready" {
		t.Fatalf("精确店铺动作证据齐全时应只放行本格，实际 %+v", resolved)
	}

	wrongShop := key
	wrongShop.ShopID = "shop-b"
	denied := ResolveCell(wrongShop, &evidence, now)
	if denied.ProductionAllowed || denied.ReasonCode != "shop_binding_mismatch" {
		t.Fatalf("错误店铺绑定必须拒绝，实际 %+v", denied)
	}

	wrongAction := key
	wrongAction.Action = "refund.write"
	denied = ResolveCell(wrongAction, &evidence, now)
	if denied.ProductionAllowed || denied.ReasonCode != "shop_binding_mismatch" {
		t.Fatalf("店铺地址变更授权不得继承到退款动作，实际 %+v", denied)
	}
}

func TestResolveCell_SimulatedRevokedAndStaleEvidenceFailClosed(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	key := CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: "api_adapter"}

	simulated := readyCellEvidence(key, now)
	simulated.Evidence[2].Simulated = true
	got := ResolveCell(key, &simulated, now)
	if got.ProductionAllowed || !got.SimulationAllowed || got.ReasonCode != "pending_live_validation" {
		t.Fatalf("模拟证据不得变成生产许可，实际 %+v", got)
	}

	revoked := readyCellEvidence(key, now)
	revoked.Revoked = true
	got = ResolveCell(key, &revoked, now)
	if got.ProductionAllowed || !got.PausePending || got.ConnectionState != ConnectionReauth || got.ReasonCode != "permission_revoked" {
		t.Fatalf("撤权后应需重连并暂停待发命令，实际 %+v", got)
	}

	stale := readyCellEvidence(key, now)
	stale.Version = "old-contract"
	got = ResolveCell(key, &stale, now)
	if got.ProductionAllowed || !got.PausePending || got.ConnectionState != ConnectionReauth || got.ReasonCode != "stale_contract_version" {
		t.Fatalf("接口契约版本过期后应降级并暂停待发命令，实际 %+v", got)
	}

	staleDocumentation := readyCellEvidence(key, now)
	staleDocumentation.DocumentationVersion = "old-documentation"
	got = ResolveCell(key, &staleDocumentation, now)
	if got.ProductionAllowed || !got.PausePending || got.ConnectionState != ConnectionReauth || got.ReasonCode != "stale_documentation_version" {
		t.Fatalf("文档证据版本过期后应降级并暂停待发命令，实际 %+v", got)
	}

	expired := readyCellEvidence(key, now)
	expiry := now
	expired.ExpiresAt = &expiry
	got = ResolveCell(key, &expired, now)
	if got.ProductionAllowed || !got.PausePending || got.ReasonCode != "evidence_expired" {
		t.Fatalf("证据到期后必须拒绝并暂停待发命令，实际 %+v", got)
	}
}

func TestResolveCell_SeparatesProductsScopesAndSecretExport(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	shopKey := CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "message.text.send", Carrier: "bridge"}
	socialKey := shopKey
	socialKey.Product = "douyin"

	evidence := readyCellEvidence(shopKey, now)
	evidence.Scopes = []string{"message.image.send"}
	got := ResolveCell(shopKey, &evidence, now)
	if got.ProductionAllowed || got.ReasonCode != "required_scope_missing" {
		t.Fatalf("缺少本格scope不得放行，实际 %+v", got)
	}
	if ResolveCell(socialKey, &evidence, now).ProductionAllowed {
		t.Fatal("douyin_shop 的证据不得继承给 douyin 产品")
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("序列化能力摘要失败: %v", err)
	}
	for _, secret := range []string{"Bearer secret", "private-token", "auth-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("能力摘要泄漏秘密 %q: %s", secret, encoded)
		}
	}
}

func readyCellEvidence(key CellKey, now time.Time) CellEvidence {
	expires := now.Add(time.Hour)
	contractVersion := "api-v1"
	if key.Carrier == ModeBridge {
		contractVersion = "bridge-v1"
	}
	stateLimits := []string(nil)
	preflightPassed := false
	if key.Action == "order.address.change" || key.Action == "aftersale.write" || key.Action == "refund.write" {
		stateLimits = []string{"unshipped"}
		preflightPassed = true
	}
	return CellEvidence{
		TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region,
		Action: key.Action, Carrier: key.Carrier, ConnectionState: ConnectionReady,
		Scopes: []string{"order.address.change", "message.text.send"}, Version: contractVersion,
		DocumentationVersion: "design-cell-v1", ExpiresAt: &expires,
		StateLimits: stateLimits, StatePreflightPassed: preflightPassed, Evidence: []EvidenceRecord{
			{Kind: EvidenceDocumentation}, {Kind: EvidenceImplementation}, {Kind: EvidenceRuntime},
		},
	}
}

// D/I/R 任一模拟、缺失或过期都不得被提升为生产许可。
func TestResolveCell_IncompleteOrSimulatedEvidenceDenied(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	key := CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: "api_adapter"}
	cases := []struct {
		name   string
		change func(*CellEvidence)
		reason string
	}{
		{"模拟文档", func(e *CellEvidence) { e.Evidence[0].Simulated = true }, "pending_live_validation"},
		{"模拟实现", func(e *CellEvidence) { e.Evidence[1].Simulated = true }, "pending_live_validation"},
		{"缺少运行证据", func(e *CellEvidence) { e.Evidence = e.Evidence[:2] }, "cell_evidence_incomplete"},
		{"缺少到期时间", func(e *CellEvidence) { e.ExpiresAt = nil }, "evidence_expiry_missing"},
		{"缺少平台状态预检", func(e *CellEvidence) { e.StatePreflightPassed = false }, "state_preflight_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := readyCellEvidence(key, now)
			tc.change(&evidence)
			got := ResolveCell(key, &evidence, now)
			if got.ProductionAllowed || got.ReasonCode != tc.reason {
				t.Fatalf("不完整或模拟证据必须阻塞，实际 %+v", got)
			}
		})
	}
}

// 每个维度独立匹配；未知格不能沿用其他格的授权。
func TestResolveCell_AllKeyDimensionsFailClosed(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	key := CellKey{TenantID: 7, ShopID: "shop-a", Product: "douyin_shop", Region: "CN", Action: "message.text.send", Carrier: "bridge"}
	evidence := readyCellEvidence(key, now)
	cases := []struct {
		name   string
		change func(*CellKey)
	}{
		{"租户", func(k *CellKey) { k.TenantID++ }},
		{"店铺", func(k *CellKey) { k.ShopID = "shop-b" }},
		{"产品", func(k *CellKey) { k.Product = "douyin" }},
		{"地区", func(k *CellKey) { k.Region = "US" }},
		{"图片动作", func(k *CellKey) { k.Action = "message.image.send" }},
		{"载体", func(k *CellKey) { k.Carrier = "official_ui" }},
		{"未知动作", func(k *CellKey) { k.Action = "unknown.submit" }},
		{"空租户", func(k *CellKey) { k.TenantID = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := key
			tc.change(&other)
			if got := ResolveCell(other, &evidence, now); got.ProductionAllowed || got.ReasonCode == "" {
				t.Fatalf("其他能力格必须拒绝并显示原因，实际 %+v", got)
			}
		})
	}
}

func TestCellCacheKey_SeparatesTupleAndVersion(t *testing.T) {
	first := CellKey{TenantID: 7, ShopID: "a/b", Product: "c", Region: "CN", Action: "x", Carrier: "bridge"}
	second := CellKey{TenantID: 7, ShopID: "a", Product: "b/c", Region: "CN", Action: "x", Carrier: "bridge"}
	if CellCacheKey(first, "v1") == CellCacheKey(second, "v1") {
		t.Fatal("包含分隔符的不同租户/店铺/产品元组不得共享缓存键")
	}
	if CellCacheKey(first, "v1") == CellCacheKey(first, "v2") {
		t.Fatal("接口版本变化必须生成不同缓存键")
	}
}
