package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 所有合成证据仅用于本地测试；生产中没有任何代码装载这些夹具。
func localSubmitFixture(t *testing.T, source, product, resource, action, carrier string) (capregistry.SubmissionBinding, capregistry.CellEvidence) {
	t.Helper()
	key := capregistry.CellKey{TenantID: 501, ShopID: "local-fixture-shop", Product: product, Region: "CN", Action: action, Carrier: carrier}
	definition, found := capregistry.FindCellDefinition(product, key.Region, action, carrier)
	if !found {
		t.Fatalf("本地正例必须使用已登记的精确能力格，实际 %+v", key)
	}
	expires := time.Now().Add(time.Hour)
	return capregistry.SubmissionBinding{Source: source, Platform: product, ResourceID: resource, Key: key}, capregistry.CellEvidence{
		TenantID: key.TenantID, ShopID: key.ShopID, Product: product, Region: key.Region, Action: action, Carrier: carrier,
		ConnectionState: capregistry.ConnectionReady, Scopes: definition.RequiredScopes,
		Version: definition.ContractVersion, DocumentationVersion: definition.DocumentationVersion,
		ExpiresAt: &expires, StatePreflightPassed: true,
		Evidence: []capregistry.EvidenceRecord{{Kind: capregistry.EvidenceDocumentation}, {Kind: capregistry.EvidenceImplementation}, {Kind: capregistry.EvidenceRuntime}},
	}
}

func localSubmitContext(binding capregistry.SubmissionBinding, evidence capregistry.CellEvidence) context.Context {
	ctx := tenant.WithTenantID(context.Background(), binding.Key.TenantID)
	return capregistry.WithCellEvidence(capregistry.WithSubmissionBinding(ctx, binding), evidence)
}

// 旧出站行为用例须显式提供精确本地授权；只允许已登记格，不改全局策略或生产装配。
// Install an explicit authorization adapter on the test's own ingress instance.
// This preserves the old algorithm cases without making context proof a runtime source.
func useLegacyBridgeFixture(t *testing.T) {
	t.Helper()
	prior := GlobalInboxIngressService()
	var fixture InboxIngressService
	if prior != nil {
		fixture.hubRepo = prior.hubRepo
	}
	fixture.outboundAuthorizer = capregistry.AuthorizeBridgeSubmission
	SetGlobalInboxIngressService(&fixture)
	t.Cleanup(func() {
		if GlobalInboxIngressService() == &fixture {
			SetGlobalInboxIngressService(prior)
		}
	})
}

func localBridgeSubmitContext(t *testing.T, ctx context.Context, product, account, msgType string) context.Context {
	t.Helper()
	action := "message.text.send"
	if msgType == "image" {
		action = "message.image.send"
	}
	binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, product, account, action, capregistry.ModeBridge)
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		tenantID = tenant.DefaultTenantID
		ctx = tenant.WithTenantID(ctx, tenantID)
	}
	binding.Key.TenantID, evidence.TenantID = tenantID, tenantID
	return capregistry.WithCellEvidence(capregistry.WithSubmissionBinding(ctx, binding), evidence)
}

type submitDenialCase struct {
	name, reason string
	change       func(*capregistry.SubmissionBinding, *capregistry.CellEvidence)
	omitBinding  bool
	omitEvidence bool
}

func submitDenialCases() []submitDenialCase {
	return []submitDenialCase{
		{name: "无可信映射", reason: "capability_mapping_missing", omitBinding: true},
		{name: "无授权证据", reason: "capability_evidence_missing", omitEvidence: true},
		{name: "来源错配", reason: "request_binding_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Source = "untrusted-source" }},
		{name: "请求平台错配", reason: "request_binding_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Platform = "other-platform" }},
		{name: "账号或订单错配", reason: "request_binding_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.ResourceID = "another-resource" }},
		{name: "映射租户错配", reason: "tenant_binding_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Key.TenantID++ }},
		{name: "映射店铺缺失", reason: "capability_mapping_incomplete", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Key.ShopID = "" }},
		{name: "映射产品错配", reason: "product_binding_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Key.Product = "douyin" }},
		{name: "未知地区格", reason: "unknown_capability", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) { b.Key.Region = "US" }},
		{name: "请求动作错配", reason: "request_action_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) {
			b.Key.Action = "order.address.change"
		}},
		{name: "请求载体错配", reason: "request_action_mismatch", change: func(b *capregistry.SubmissionBinding, _ *capregistry.CellEvidence) {
			b.Key.Carrier = capregistry.ModeOfficialUI
		}},
		{name: "证据租户错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.TenantID++ }},
		{name: "证据店铺错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.ShopID = "other-shop" }},
		{name: "证据产品错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Product = "douyin" }},
		{name: "证据地区错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Region = "US" }},
		{name: "证据动作错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Action = "order.address.change" }},
		{name: "证据载体错配", reason: "shop_binding_mismatch", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			e.Carrier = capregistry.ModeOfficialUI
		}},
		{name: "撤权", reason: "permission_revoked", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Revoked = true }},
		{name: "过期", reason: "evidence_expired", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			expired := time.Now().Add(-time.Second)
			e.ExpiresAt = &expired
		}},
		{name: "无到期时间", reason: "evidence_expiry_missing", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.ExpiresAt = nil }},
		{name: "接口版本改变", reason: "stale_contract_version", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Version = "old-contract" }},
		{name: "文档版本改变", reason: "stale_documentation_version", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			e.DocumentationVersion = "old-documentation"
		}},
		{name: "scope缺失", reason: "required_scope_missing", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Scopes = nil }},
		{name: "D缺失", reason: "cell_evidence_incomplete", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Evidence = e.Evidence[1:] }},
		{name: "I缺失", reason: "cell_evidence_incomplete", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			e.Evidence = append(e.Evidence[:1], e.Evidence[2:]...)
		}},
		{name: "R缺失", reason: "cell_evidence_incomplete", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Evidence = e.Evidence[:2] }},
		{name: "模拟D", reason: "pending_live_validation", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Evidence[0].Simulated = true }},
		{name: "模拟I", reason: "pending_live_validation", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Evidence[1].Simulated = true }},
		{name: "模拟R", reason: "pending_live_validation", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) { e.Evidence[2].Simulated = true }},
		{name: "状态预检未通过", reason: "state_preflight_required", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			e.StateLimits = []string{"platform_preflight_required"}
			e.StatePreflightPassed = false
		}},
		{name: "连接停用", reason: "connection_disabled", change: func(_ *capregistry.SubmissionBinding, e *capregistry.CellEvidence) {
			e.ConnectionState = capregistry.ConnectionDisabled
		}},
	}
}

func (tc submitDenialCase) context(binding capregistry.SubmissionBinding, evidence capregistry.CellEvidence) context.Context {
	if tc.change != nil {
		tc.change(&binding, &evidence)
	}
	ctx := tenant.WithTenantID(context.Background(), 501)
	if !tc.omitBinding {
		ctx = capregistry.WithSubmissionBinding(ctx, binding)
	}
	if !tc.omitEvidence {
		ctx = capregistry.WithCellEvidence(ctx, evidence)
	}
	return ctx
}

func requireSubmitDenial(t *testing.T, err error, expected string) {
	t.Helper()
	if reason, ok := capabilityGateReason(err); !ok || reason != expected {
		t.Fatalf("能力拒绝 reason 期望 %s，实际 err=%v", expected, err)
	}
}

type submitCountingTransport struct {
	attempts atomic.Int32
	next     http.RoundTripper
}

func (tr *submitCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.attempts.Add(1)
	return tr.next.RoundTrip(req)
}

// 仓储已真实装配；统计 Create 尝试与最终记录，不能靠 nil 仓储制造假绿。
func TestBridgeSubmit_CapabilityDenialsHaveNoOutboxOrRetry(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.DelayedOutboundReply{})
	setupBridgeWhitelistForTest(t, "douyin_shop")
	setupBridgeOutboxForTest(t, "douyin_shop")
	previous := GlobalInboxIngressService()
	SetGlobalInboxIngressService(&InboxIngressService{hubRepo: repository.NewMessageHubRepositoryWithDB(db)})
	t.Cleanup(func() { SetGlobalInboxIngressService(previous) })
	var writes atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("ec01-count-writes", func(tx *gorm.DB) {
		writes.Add(1)
		tx.AddError(errors.New("本地负例禁止任何持久化尝试"))
	}); err != nil {
		t.Fatal(err)
	}
	svc := &WebhookService{delayedRepo: repository.NewDelayedOutboundRepository(db)}
	sender, _ := svc.outboundSenderFor("douyin_shop")
	useLegacyBridgeFixture(t)
	for _, tc := range submitDenialCases() {
		t.Run(tc.name, func(t *testing.T) {
			binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "local-account", "message.text.send", capregistry.ModeBridge)
			ctx := tc.context(binding, evidence)
			_, _, err := DeliverBridgeOutboundMedia(ctx, "douyin_shop", "local-account", "local-conversation", "text", "blocked", "", "")
			requireSubmitDenial(t, err, tc.reason)
			for _, quiet := range []bool{false, true} {
				previousQuiet := loadAIReplyQuietHoursFn()
				storeAIReplyQuietHoursFn(func(time.Time) bool { return quiet })
				sent, sendErr := svc.sendOutbound(ctx, "douyin_shop", "local-account", &ParsedPayload{EventID: "ec01-" + tc.name}, "blocked", &model.MessageHub{TenantID: 501, ConversationID: "local-conversation", SenderID: "buyer"}, nil)
				storeAIReplyQuietHoursFn(previousQuiet)
				if sent {
					t.Fatal("拒绝请求不得标记发送成功")
				}
				requireSubmitDenial(t, sendErr, tc.reason)
				if ce := AsChannelError(sendErr); ce == nil || ce.Retryable {
					t.Errorf("能力拒绝必须不可重试，实际 %+v", ce)
				}
				// 复用 worker 的错误分类检查：拒绝也不能被调用方再次放入重试队列。
				svc.enqueueSendRetry(ctx, "douyin_shop", "local-account", &ParsedPayload{}, "blocked", &model.MessageHub{ConversationID: "local-conversation"}, nil, sendErr)
			}
			var directErr error
			result := sender.Send(ctx, &OutboundRequest{Channel: "douyin_shop", AccountID: "local-account", Content: "blocked",
				Hub:           &model.MessageHub{TenantID: 501, ConversationID: "local-conversation", SenderID: "buyer"},
				FailUntracked: func(err error) { directErr = err },
			})
			requireSubmitDenial(t, directErr, tc.reason)
			if result.Sent || AsChannelError(directErr).Retryable {
				t.Fatal("直接 sender 拒绝不得成功或可重试")
			}
			if writes.Load() != 0 {
				t.Fatalf("拒绝必须零 outbox/quiet-hours/retry 写入尝试，实际 %d", writes.Load())
			}
		})
	}
	for _, row := range []any{&model.MessageHub{}, &model.DelayedOutboundReply{}} {
		var count int64
		if err := db.WithContext(tenant.WithTenantID(context.Background(), 501)).Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("拒绝后的队列表必须为空，count=%d err=%v", count, err)
		}
	}
}

// HTTP 客户端和上层售后编排都必须在本地落库、HTTP Do 之前拒绝。
func TestAfterSaleSubmit_CapabilityDenialsHaveNoPersistenceOrHTTP(t *testing.T) {
	db := testutil.NewTestDB(t, &model.AfterSale{}, &model.CapabilitySourceBinding{})
	var hits, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"external_id":"unexpected"}`))
	}))
	defer server.Close()
	client := NewAfterSaleExternalClientFromConfig(AfterSaleIntegration{BaseURL: server.URL})
	transport := &submitCountingTransport{next: server.Client().Transport}
	client.http.Transport = transport
	client.authorizer = legacyAfterSaleFixture
	svc := NewAfterSaleServiceWithClient(repository.NewAfterSaleRepositoryWithDB(db), client)
	if err := db.Callback().Create().Before("gorm:create").Register("ec01-count-aftersale", func(tx *gorm.DB) {
		writes.Add(1)
		tx.AddError(errors.New("本地负例禁止售后持久化尝试"))
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{portcontract.AfterSaleRefund, "return", "exchange"} {
		t.Run(kind, func(t *testing.T) {
			action := "aftersale.write"
			if kind == portcontract.AfterSaleRefund {
				action = "refund.write"
			}
			for _, tc := range submitDenialCases() {
				t.Run(tc.name, func(t *testing.T) {
					binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceAfterSale, "douyin_shop", "local-order", action, capregistry.ModeAPIAdapter)
					ctx := tc.context(binding, evidence)
					req := &portcontract.AfterSaleRequest{Platform: "douyin_shop", OrderID: "local-order", Type: kind}
					_, err := client.Create(ctx, req)
					requireSubmitDenial(t, err, tc.reason)
					_, err = svc.Create(ctx, req)
					requireSubmitDenial(t, err, "capability_mapping_missing")
					if hits.Load() != 0 || writes.Load() != 0 || transport.attempts.Load() != 0 {
						t.Fatalf("拒绝须在本地记录和 HTTP 传输前，writes=%d attempts=%d hits=%d", writes.Load(), transport.attempts.Load(), hits.Load())
					}
				})
			}
		})
	}
}

// 已识别的任何桥接渠道（包括未来新增渠道）都不因缺少 cell 而走旧放行路径。
func TestBridgeSubmit_UnregisteredCellsDenyAtProductionBoundaries(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.DelayedOutboundReply{}, &model.CapabilitySourceBinding{}, &model.MerchantConnection{}, &model.CapabilityEvidence{})
	var writes atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("ec01-count-unknown-cell", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || (tx.Statement.Schema.Table != "message_hub" && tx.Statement.Schema.Table != (&model.DelayedOutboundReply{}).TableName()) {
			return
		}
		writes.Add(1)
		tx.AddError(errors.New("未知格/证据缺失不得持久化"))
	}); err != nil {
		t.Fatal(err)
	}
	previousIngress := GlobalInboxIngressService()
	SetGlobalInboxIngressService(&InboxIngressService{hubRepo: repository.NewMessageHubRepositoryWithDB(db)})
	t.Cleanup(func() { SetGlobalInboxIngressService(previousIngress) })
	previousQuiet := loadAIReplyQuietHoursFn()
	t.Cleanup(func() { storeAIReplyQuietHoursFn(previousQuiet) })
	channels := []string{"future_bridge_channel"}
	for _, entry := range capregistry.All() {
		if entry.Cap == capregistry.CapDMSend && entry.Via == capregistry.ModeBridge {
			channels = append(channels, entry.Platform)
		}
	}
	setupBridgeWhitelistForTest(t, channels...)
	setupBridgeOutboxForTest(t, channels...)
	for _, channel := range channels {
		t.Run(channel, func(t *testing.T) {
			binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "local-account", "message.text.send", capregistry.ModeBridge)
			binding.Platform, binding.Key.Product, evidence.Product = channel, channel, channel
			ctx := localSubmitContext(binding, evidence)
			expires := time.Now().Add(time.Hour)
			conn := model.MerchantConnection{TenantID: 501, Provider: channel, ShopID: binding.Key.ShopID, Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "local-contract", ExpiresAt: &expires}
			if err := db.WithContext(ctx).Create(&conn).Error; err != nil {
				t.Fatal(err)
			}
			source := model.CapabilitySourceBinding{TenantID: 501, Source: binding.Source, Product: channel, ResourceID: binding.ResourceID, ShopID: conn.ShopID, Region: "CN", ConnectionID: conn.ID, ConsentVersion: 1}
			if err := db.WithContext(ctx).Create(&source).Error; err != nil {
				t.Fatal(err)
			}
			reason := "unknown_capability"
			if _, known := capregistry.FindCellDefinition(channel, "CN", "message.text.send", capregistry.ModeBridge); known {
				reason = "shop_evidence_missing"
			}
			_, _, err := DeliverBridgeOutboundMedia(ctx, channel, "local-account", "local-conversation", "text", "blocked", "", "")
			requireSubmitDenial(t, err, reason)
			svc := &WebhookService{delayedRepo: repository.NewDelayedOutboundRepository(db)}
			for _, quiet := range []bool{false, true} {
				storeAIReplyQuietHoursFn(func(time.Time) bool { return quiet })
				sent, err := svc.sendOutbound(ctx, WebhookChannel(channel), "local-account", &ParsedPayload{}, "blocked", &model.MessageHub{TenantID: 501, ConversationID: "local-conversation"}, nil)
				requireSubmitDenial(t, err, reason)
				if sent || AsChannelError(err).Retryable {
					t.Fatal("缺失能力格或证据不得发送或重试")
				}
				svc.enqueueSendRetry(ctx, WebhookChannel(channel), "local-account", &ParsedPayload{}, "blocked", &model.MessageHub{ConversationID: "local-conversation"}, nil, err)
			}
			sender, ok := svc.outboundSenderFor(WebhookChannel(channel))
			if !ok {
				t.Fatal("桥接能力集合中渠道必须走真实 bridge sender")
			}
			var directErr error
			result := sender.Send(ctx, &OutboundRequest{Channel: WebhookChannel(channel), AccountID: "local-account", Hub: &model.MessageHub{TenantID: 501, ConversationID: "local-conversation"}, FailUntracked: func(err error) { directErr = err }})
			requireSubmitDenial(t, directErr, reason)
			if result.Sent || writes.Load() != 0 {
				t.Fatalf("任何桥接格拒绝须零 outbox/quiet-hours/retry 写入，sent=%v writes=%d", result.Sent, writes.Load())
			}
		})
	}
	for _, row := range []any{&model.MessageHub{}, &model.DelayedOutboundReply{}} {
		var count int64
		if err := db.WithContext(tenant.WithTenantID(context.Background(), 501)).Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("所有桥接渠道拒绝后的队列表必须为空，count=%d err=%v", count, err)
		}
	}
}

// 上下文授权不可被复用来给另一个租户的入站消息入队；正常工厂与直接 sender 都拒绝。
func TestWebhookBridgeSubmit_HubTenantMismatchHasNoPersistence(t *testing.T) {
	setupBridgeOutboxForTest(t, "douyin_shop")
	binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "local-account", "message.text.send", capregistry.ModeBridge)
	ctx := localSubmitContext(binding, evidence)
	previous := loadAIReplyQuietHoursFn()
	storeAIReplyQuietHoursFn(func(time.Time) bool { t.Fatal("跨租户请求必须在延迟策略前拒绝"); return false })
	t.Cleanup(func() { storeAIReplyQuietHoursFn(previous) })
	svc := &WebhookService{}
	_, err := svc.sendOutbound(ctx, "douyin_shop", "local-account", &ParsedPayload{}, "blocked", &model.MessageHub{TenantID: 502}, nil)
	requireSubmitDenial(t, err, "tenant_binding_mismatch")
	sender, _ := svc.outboundSenderFor("douyin_shop")
	var directErr error
	sender.Send(ctx, &OutboundRequest{Channel: "douyin_shop", AccountID: "local-account", Hub: &model.MessageHub{TenantID: 502}, FailUntracked: func(err error) { directErr = err }})
	requireSubmitDenial(t, directErr, "tenant_binding_mismatch")
}

// 正例仅打到本机 httptest，验证 exact 证据可经过裁决；不代表生产店铺已授权。
func TestHTTPAfterSaleSubmit_ExactLocalFixtureRechecksRevocation(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/refund" {
			t.Errorf("请求方法或路径错误：%s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"external_id":"local-only"}`)
	}))
	defer server.Close()
	binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceAfterSale, "douyin_shop", "local-order", "refund.write", capregistry.ModeAPIAdapter)
	client := NewAfterSaleExternalClientFromConfig(AfterSaleIntegration{BaseURL: server.URL})
	req := &portcontract.AfterSaleRequest{Platform: "douyin_shop", OrderID: "local-order", Type: portcontract.AfterSaleRefund}
	client.authorizer = legacyAfterSaleFixture
	result, err := client.Create(localSubmitContext(binding, evidence), req)
	if err != nil || result == nil || result.ExternalID != "local-only" || hits.Load() != 1 {
		t.Fatalf("精确本地夹具应只发送一次，result=%+v err=%v hits=%d", result, err, hits.Load())
	}
	evidence.Revoked = true
	_, err = client.Create(localSubmitContext(binding, evidence), req)
	requireSubmitDenial(t, err, "permission_revoked")
	if hits.Load() != 1 {
		t.Fatalf("撤权不能沿用上次裁决，实际 HTTP 次数 %d", hits.Load())
	}
}

func legacyAfterSaleFixture(ctx context.Context, req *portcontract.AfterSaleRequest) error {
	action := "aftersale.write"
	if req.Type == portcontract.AfterSaleRefund {
		action = "refund.write"
	}
	return capregistry.AuthorizeAfterSaleSubmission(ctx, req.Platform, req.OrderID, action, time.Now().UTC())
}
