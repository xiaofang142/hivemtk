package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 未提供可信店铺映射/动作证据时，bridge 提交必须在 outbox 落库前拒绝。
func TestDeliverBridgeOutboundMedia_RejectsWithoutCapabilityEvidenceBeforeOutbox(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	previous := GlobalInboxIngressService()
	SetGlobalInboxIngressService(nil)
	t.Cleanup(func() { SetGlobalInboxIngressService(previous) })

	ctx := tenant.WithTenantID(context.Background(), 501)
	_, _, err := DeliverBridgeOutboundMedia(ctx, "douyin", "acc_without_shop_mapping", "conv_gate", "text", "must not enqueue", "", "")
	if err == nil {
		t.Fatal("缺少可信店铺动作证据时必须拒绝 bridge submit")
	}
	if reason, ok := capabilityGateReason(err); !ok || reason != "capability_storage_unavailable" {
		t.Fatalf("拒绝应带机器可读 reason capability_storage_unavailable，err=%v", err)
	}
	// ingress 未装配，测试不触碰数据库；必须在到达 enqueue 前由能力门给出拒绝。
}

// 配置售后 HTTP 客户端没有可信的 tenant/shop/action 证据时，Do 前返回机器可读拒绝。
func TestHTTPAfterSaleClient_RejectsWithoutCapabilityEvidenceBeforeHTTPDo(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"external_id":"unexpected"}`))
	}))
	defer server.Close()

	client := NewAfterSaleExternalClientFromConfig(AfterSaleIntegration{BaseURL: server.URL})
	_, err := client.Create(tenant.WithTenantID(context.Background(), 501), &portcontract.AfterSaleRequest{
		Platform: "douyin_shop", OrderID: "order-local-test", Type: portcontract.AfterSaleRefund,
	})
	if err == nil {
		t.Fatalf("缺少可信店铺动作证据时必须在 HTTP Do 前拒绝售后 submit，实际 HTTP 命中 %d 次", hits)
	}
	if reason, ok := capabilityGateReason(err); !ok || reason != "capability_storage_unavailable" {
		t.Fatalf("拒绝应带机器可读 reason capability_storage_unavailable，err=%v", err)
	}
	if hits != 0 {
		t.Fatalf("拒绝前不得调用外部 HTTP，实际命中 %d 次", hits)
	}
}

// WebhookService 的生产 sender 选择到 bridgeOutboxSender，但该实现绕过
// DeliverBridgeOutboundMedia 并直接写 message_hub；必须在该独立边界裁决。
func TestWebhookBridgeSender_RejectsBeforeDirectOutboxWrite(t *testing.T) {
	const channel WebhookChannel = "douyin_shop"
	testutil.NewTestDB(t, &model.CapabilitySourceBinding{})
	setupBridgeOutboxForTest(t, string(channel))
	svc := &WebhookService{}
	sender, ok := svc.outboundSenderFor(channel)
	if !ok {
		t.Fatal("douyin_shop 应由生产 bridge outbox sender 接管")
	}
	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		sent, sendErr := svc.sendOutbound(tenant.WithTenantID(context.Background(), 501), channel,
			"acct-not-a-shop-id", &ParsedPayload{EventID: "ec01-no-binding"}, "must not enqueue",
			&model.MessageHub{TenantID: 501, ConversationID: "conv_gate", SenderID: "buyer"}, nil)
		if sent {
			t.Fatal("缺少可信店铺证据时不得报告发送成功")
		}
		if reason, ok := capabilityGateReason(sendErr); !ok || reason != "capability_mapping_missing" {
			t.Fatalf("sendOutbound 必须在延迟队列/发送器前返回机器可读拒绝，err=%v", sendErr)
		}
	}()
	if panicValue != nil {
		t.Fatalf("生产 sendOutbound 应在访问 outbox/retry repository 前拒绝，实际 panic: %v", panicValue)
	}

	// 直接调用 sender 也须在 message_hub 落库前拒绝，作为最终副作用边界保护。
	var submitErr error
	func() {
		defer func() { panicValue = recover() }()
		sender.Send(tenant.WithTenantID(context.Background(), 501), &OutboundRequest{
			Channel: channel, AccountID: "acct-not-a-shop-id",
			Hub:     &model.MessageHub{TenantID: 501, ConversationID: "conv_gate", SenderID: "buyer"},
			Content: "must not enqueue", FailUntracked: func(err error) { submitErr = err },
		})
	}()
	if panicValue != nil {
		t.Fatalf("bridge sender 应在访问 message_hub repository 前拒绝，实际 panic: %v", panicValue)
	}
	if reason, ok := capabilityGateReason(submitErr); !ok || reason != "capability_mapping_missing" {
		t.Fatalf("bridge sender 缺少映射应返回机器可读拒绝，err=%v", submitErr)
	}
}

func capabilityGateReason(err error) (string, bool) {
	var gateErr interface{ CapabilityReasonCode() string }
	if !errors.As(err, &gateErr) {
		return "", false
	}
	return gateErr.CapabilityReasonCode(), true
}

// 2026-11-10 审计 F6：DeliverBridgeOutboundMedia 补上下行媒体的服务端生产者出口。
// 下行媒体协议三端（OutboxMessage.MediaURL / SSE media_url / 扩展 sendOutbound 分派）
// 早已就绪，此前所有出站入口都 text-only 入队。本文件钉三个口径：
//  1. 带媒体出站：pending 行带 media_url + msg_type，msg_id 用 typed 口径（同占位符
//     两张图的行 msg_id 不碰撞）；
//  2. 文本出站（旧签名）：msg_id 与 ContentHashMsgID 逐字节一致（存量不变）；
//  3. media_url 列宽收口（F2）与 text 类型强制清空 media_url。
func TestDeliverBridgeOutboundMedia_TwoImagesDistinctMsgIDs(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	ctx := tenant.WithTenantID(context.Background(), uint(501))
	const (
		channel = "douyin"
		account = "acc_media_out"
		conv    = "conv_media_out"
	)
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContext(t, ctx, channel, account, "image")

	id1, msg1, err := DeliverBridgeOutboundMedia(ctx, channel, account, conv, "image", "[图片]", "https://f/x1.png", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(图1) 失败: %v", err)
	}
	id2, msg2, err := DeliverBridgeOutboundMedia(ctx, channel, account, conv, "image", "[图片]", "https://f/x2.png", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(图2) 失败: %v", err)
	}
	if msg1 == msg2 {
		t.Fatalf("同占位符两张图的 msg_id 必须不同（typed 口径）: %s", msg1)
	}
	if id1 == id2 {
		t.Fatalf("两行 hub id 相同: %d", id1)
	}

	pending, err := svc.ListPendingOutbound(ctx, channel, account)
	if err != nil {
		t.Fatalf("ListPendingOutbound 失败: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("期望 2 条 pending，实际 %d", len(pending))
	}
	byMsg := map[string]*model.MessageHub{}
	for i := range pending {
		byMsg[pending[i].MsgID] = pending[i]
	}
	row1 := byMsg[msg1]
	if row1 == nil {
		t.Fatalf("回执 msg_id %s 未命中 pending 行", msg1)
	}
	if row1.MediaURL != "https://f/x1.png" {
		t.Errorf("MediaURL = %q, want https://f/x1.png", row1.MediaURL)
	}
	if row1.MsgType != "image" {
		t.Errorf("MsgType = %q, want image", row1.MsgType)
	}
	if row1.Direction != "outbound" {
		t.Errorf("Direction = %q, want outbound", row1.Direction)
	}
}

func TestDeliverBridgeOutboundMedia_TextLegacyByteIdentical(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	const (
		channel = "douyin"
		account = "acc_media_out_txt"
		conv    = "conv_media_out_txt"
		content = "纯文本回复"
	)
	useLegacyBridgeFixture(t)
	ctx := localBridgeSubmitContext(t, ctx2(), channel, account, "text")
	_, msgID, err := DeliverBridgeOutboundMedia(ctx, channel, account, conv, "text", content, "", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(text) 失败: %v", err)
	}
	if msgID != ContentHashMsgID(channel, conv, content) {
		t.Fatalf("文本出站 msg_id 必须与旧口径逐字节一致: got %s want %s", msgID, ContentHashMsgID(channel, conv, content))
	}
	pending, err := svc.ListPendingOutbound(ctx2(), channel, account)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending 查询失败或条数不符: %v %d", err, len(pending))
	}
	if pending[0].MediaURL != "" {
		t.Errorf("文本出站 MediaURL 必须为空: %q", pending[0].MediaURL)
	}
}

func TestDeliverBridgeOutboundMedia_TextTypeForcesEmptyMediaURLAndClamps(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	// text 类型强制清空 media_url
	useLegacyBridgeFixture(t)
	ctx := localBridgeSubmitContext(t, ctx2(), "douyin", "acc_a", "text")
	_, msgID, err := DeliverBridgeOutboundMedia(ctx, "douyin", "acc_a", "conv_a", "text", "hi", "https://f/should-be-dropped.png", "")
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	hub, err := svc.hubRepo.GetByMsgID(ctx2(), msgID)
	if err != nil || hub == nil {
		t.Fatalf("行未落库: %v", err)
	}
	if hub.MediaURL != "" {
		t.Errorf("text 类型 MediaURL 必须清空: %q", hub.MediaURL)
	}

	// 媒体类型超长 URL 截到列宽（F2）
	long := "https://f/" + strings.Repeat("z", 600)
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContext(t, ctx, "douyin", "acc_a", "image")
	_, msgID2, err := DeliverBridgeOutboundMedia(ctx, "douyin", "acc_a", "conv_a", "image", "[图片]", long, "")
	if err != nil {
		t.Fatalf("超长媒体出站失败: %v", err)
	}
	hub2, err := svc.hubRepo.GetByMsgID(ctx2(), msgID2)
	if err != nil || hub2 == nil {
		t.Fatalf("行未落库: %v", err)
	}
	if len([]rune(hub2.MediaURL)) > model.MaxMediaURLLen {
		t.Errorf("MediaURL 未截断: %d 字符", len([]rune(hub2.MediaURL)))
	}
}

func ctx2() context.Context { return context.Background() }
