package service

// B5 单一 sender 能力位 + R5 Markdown 对齐（CH-37b刀3 批准决议）。
//
// 本文件钉住四格契约：
//  1. 出站识别纯查询驱动：sendOutbound 桥接分支的渠道集合 = registry
//     OutboundPath=bridge_outbox 能力投影（SetBridgeOutboxChannels 装配），分支里
//     不得有硬编码残留——注入什么认什么（registry 侧恒等由 channelgw
//     registry_parity_test.go TestBridgeOutboxParityWithWebhookOutbound 双向钉住）。
//  2. Sender 契约：桥接出站行 SenderID ≡ accountID（AI 与人工/触达两路径），
//     重放/重试不换 sender。
//  3. R5 Markdown 对齐（批准的行为变更）：AI 桥接出站入队前剥 Markdown，与人工/
//     触达路径（bridge_outbound.go DeliverBridgeOutbound）同源同函数，产出一致；
//     纯文本零变化（幂等），含 Markdown 文本 AI 路径行为有意变化＝与人工路径对齐。
//  4. 未知渠道（registry 无能力）出站拒绝路径不变。
//
// 单测装配说明：service 内测文件不能 import channelgw（channelgw 反向 import
// service，内测引它会成环），init 按 registry Default 的登记等价注入九渠道；
// 集合漂移由 channelgw 侧 parity 闸先红（那边钉的是同一份批准集合）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// b5RegistryOutboxChannels registry Default 九桥接渠道（批准决议口径）。
// 单测夹具等价注入；registry 侧的恒等由 TestBridgeOutboxParityWithWebhookOutbound 钉住。
var b5RegistryOutboxChannels = []string{
	"douyin", "xiaohongshu", "kuaishou", "xianyu", "tiktok",
	"jd", "bilibili", "weibo", "personal_wx",
}

func init() {
	SetBridgeOutboxChannels(b5RegistryOutboxChannels)
}

// setupBridgeOutboxForTest 换掉能力集合并在用例结束后还原（查询驱动的接缝替换）。
func setupBridgeOutboxForTest(t *testing.T, names ...string) {
	t.Helper()
	prev := BridgeOutboxChannels()
	SetBridgeOutboxChannels(names)
	t.Cleanup(func() { SetBridgeOutboxChannels(prev) })
}

// TestBridgeOutboxCapability_QueryDrivenSeam 出站识别必须纯查询驱动：
// 注入什么认什么，分支里不得有硬编码残留（B5「新增渠道只登记 registry 即自动
// 获得出站路径」的服务端等价证明——本用例模拟 registry 登记后的 init 装配结果）。
func TestBridgeOutboxCapability_QueryDrivenSeam(t *testing.T) {
	// 单渠道注入：其余桥接渠道不得命中（残留硬编码即红）。
	setupBridgeOutboxForTest(t, "douyin")
	if !bridgeOutboxCapable(ChannelDouyin) {
		t.Error("注入的渠道应命中能力查询")
	}
	for _, ch := range []WebhookChannel{ChannelXiaohongshu, ChannelKuaishou, ChannelPersonalWX, ChannelWeCom, ChannelTelegram} {
		if bridgeOutboxCapable(ch) {
			t.Errorf("渠道 %q 未注入却命中能力查询：分支存在硬编码残留，B5 单源被破坏", ch)
		}
	}

	// 合成新渠道（模拟 registry 新登记 → init 装配）：不改任何分支代码即应获得出站路径。
	setupBridgeOutboxForTest(t, "synthetic_new_ch")
	if !bridgeOutboxCapable("synthetic_new_ch") {
		t.Error("新登记渠道未获得出站路径：识别不是纯注入驱动，违反 B5 单源决议")
	}
	got := BridgeOutboxChannels()
	if len(got) != 1 || got[0] != "synthetic_new_ch" {
		t.Errorf("BridgeOutboxChannels 应恰为最新注入集合，got %v", got)
	}
}

// TestBridgeOutboxChannels_EmptyInjectionIgnored 空注入忽略（与 SetBridgeChannels 同一防呆）：
// 装配时序异常时宁可维持上一份集合，也不悄悄断掉 AI 出站。
func TestBridgeOutboxChannels_EmptyInjectionIgnored(t *testing.T) {
	setupBridgeOutboxForTest(t, "douyin", "xianyu")
	SetBridgeOutboxChannels(nil)
	got := BridgeOutboxChannels()
	if len(got) != 2 || got[0] != "douyin" || got[1] != "xianyu" {
		t.Errorf("空注入应被忽略，集合不应被清空，got %v", got)
	}
}

// TestSendOutbound_Bridge_UnknownChannelRejected registry 无能力的未知渠道：
// 出站拒绝路径不变（宁可明确拒绝，也不静默收下），不落任何出站行。
func TestSendOutbound_Bridge_UnknownChannelRejected(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	const (
		unknownCh = "pigeon_post_b5"
		account   = "acct-b5-unknown"
		conv      = "conv-b5-unknown"
	)
	releaseReplyClaimForTest(t, "evt-b5-unknown")
	p := &ParsedPayload{EventID: "evt-b5-unknown", Sender: "cust-b5-unknown", Content: "在吗", ChatID: conv}

	sent, sendErr := svc.sendOutbound(context.Background(), WebhookChannel(unknownCh), account, p, "AI 回复", nil, nil)
	if sent {
		t.Error("registry 无能力的渠道不得判出站成功")
	}
	if sendErr == nil {
		t.Fatal("未知渠道必须构造不可重试的渠道错误（拒绝要出声）")
	}
	if !strings.Contains(sendErr.Error(), "出站不支持该渠道") {
		t.Errorf("拒绝文案应保持原口径，got %q", sendErr.Error())
	}

	var n int64
	if err := db.Model(&model.MessageHub{}).
		Where("direction = ? AND platform = ?", "outbound", unknownCh).
		Count(&n).Error; err != nil {
		t.Fatalf("查出站行失败: %v", err)
	}
	if n != 0 {
		t.Errorf("未知渠道不得落出站行，实际 %d 行", n)
	}
}

// TestSendOutbound_Bridge_SenderIDEqualsAccountID_NineChannels 九渠道逐一回归：
//   - Sender 契约：桥接出站行 SenderID ≡ accountID（B5）；
//   - 纯文本内容零变化（StripMarkdownForDM 对纯文本幂等，R5 只对含 Markdown 文本有意变化）；
//   - 出站行形态不变（pending/text/AI 回复/会话号/收件人）。
func TestSendOutbound_Bridge_SenderIDEqualsAccountID_NineChannels(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	content := "好的，稍等，我核对一下库存再回复您。"
	for _, ch := range BridgeOutboxChannels() {
		account := "acct-b5-" + ch
		conv := "conv-b5-" + ch
		sender := "cust-b5-" + ch
		hub := &model.MessageHub{
			MsgID:          "mh:in-b5-" + ch,
			Platform:       ch,
			AccountID:      account,
			Direction:      "inbound",
			MsgType:        "text",
			SenderID:       sender,
			Content:        "有推荐吗",
			ConversationID: conv,
			SentAt:         time.Now(),
		}
		if err := db.Create(hub).Error; err != nil {
			t.Fatalf("[%s] seed inbound: %v", ch, err)
		}
		eventID := "evt-b5-sender-" + ch
		releaseReplyClaimForTest(t, eventID)
		p := &ParsedPayload{EventID: eventID, Sender: sender, Content: "有推荐吗", ChatID: conv}

		if _, err := svc.sendOutbound(context.Background(), WebhookChannel(ch), account, p, content, hub, nil); err != nil {
			t.Fatalf("[%s] 正常出库不应报错: %v", ch, err)
		}

		var out model.MessageHub
		if err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
			First(&out).Error; err != nil {
			t.Fatalf("[%s] 读回出站行: %v", ch, err)
		}
		if out.SenderID != account {
			t.Errorf("[%s] Sender 契约（B5）：出站行 SenderID = %q, want accountID %q", ch, out.SenderID, account)
		}
		if out.AccountID != account {
			t.Errorf("[%s] AccountID = %q, want %q", ch, out.AccountID, account)
		}
		if out.Platform != ch {
			t.Errorf("[%s] Platform = %q, want %q", ch, out.Platform, ch)
		}
		if out.Status != "pending" {
			t.Errorf("[%s] Status = %q, want pending（可被扩展领取）", ch, out.Status)
		}
		if out.MsgType != "text" {
			t.Errorf("[%s] MsgType = %q, want text", ch, out.MsgType)
		}
		if !out.IsAIReply {
			t.Errorf("[%s] IsAIReply 应为 true（AI 路径）", ch)
		}
		if out.ConversationID != conv {
			t.Errorf("[%s] ConversationID = %q, want %q", ch, out.ConversationID, conv)
		}
		if out.ReceiverID != sender {
			t.Errorf("[%s] ReceiverID = %q, want 入站发送者 %q", ch, out.ReceiverID, sender)
		}
		if out.Content != content {
			t.Errorf("[%s] 纯文本出站内容必须零变化（StripMarkdownForDM 幂等）：got %q, want %q", ch, out.Content, content)
		}
	}
}

// TestSendOutbound_Bridge_ReplayKeepsSender 重放/重试不换 sender：
// 重放 worker 的调用形状是 sendOutbound(DelayedReplayToContext(ctx), channel,
// rec.AccountID, ...)（replayDelayedOutbound），桥接分支落库的 SenderID 仍必须是
// accountID，且与首发行一致。
func TestSendOutbound_Bridge_ReplayKeepsSender(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	const (
		channel = ChannelDouyin
		account = "acct-b5-replay"
		conv    = "conv-b5-replay"
		sender  = "cust-b5-replay"
	)
	hub := &model.MessageHub{
		MsgID:          "mh:in-b5-replay",
		Platform:       "douyin",
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       sender,
		Content:        "在吗",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}

	// 首发（实时路径）
	releaseReplyClaimForTest(t, "evt-b5-replay-first")
	first := &ParsedPayload{EventID: "evt-b5-replay-first", Sender: sender, Content: "在吗", ChatID: conv}
	if sent, err := svc.sendOutbound(context.Background(), channel, account, first, "首发回复", hub, nil); err != nil || !sent {
		t.Fatalf("首发应成功: sent=%v err=%v", sent, err)
	}

	// 重放（重试/重领路径：DelayedReplayToContext 标记 + 独立 EventID）。
	// 内容用新回复：同内容重放会命中 message_hub 幂等键（platform+msg_id+conv+direction
	// ON CONFLICT DO NOTHING）不产生新行，属既有去重语义，与本契约无关。
	releaseReplyClaimForTest(t, "evt-b5-replay-second")
	second := &ParsedPayload{EventID: "evt-b5-replay-second", Sender: sender, Content: "在吗", ChatID: conv}
	replayCtx := DelayedReplayToContext(context.Background())
	if sent, err := svc.sendOutbound(replayCtx, channel, account, second, "重放回复", hub, nil); err != nil || !sent {
		t.Fatalf("重放应成功: sent=%v err=%v", sent, err)
	}

	var rows []model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
		Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读回出站行: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应落两行出站（首发+重放），实际 %d 行", len(rows))
	}
	for i, row := range rows {
		if row.SenderID != account {
			t.Errorf("第 %d 行 SenderID = %q, want accountID %q（重放不换 sender）", i+1, row.SenderID, account)
		}
	}
	if rows[0].SenderID != rows[1].SenderID {
		t.Errorf("首发与重放的 SenderID 必须一致：%q vs %q", rows[0].SenderID, rows[1].SenderID)
	}
}

// TestSendOutbound_Bridge_MarkdownAlignedWithManualPath R5 前后对照（CH-37b刀3 批准的
// 有意行为变更）：同一份含 Markdown 内容，AI 路径（sendOutbound 桥接分支）与人工/触达
// 路径（DeliverBridgeOutbound，bridge_outbound.go 入队前同一判定+同一函数）产出必须一致，
// 且都等于 StripMarkdownForDM 的剥离结果。变更前 AI 路径是原样入队（＝原始 Markdown），
// 断言「出站内容 == 原始内容」即红，本用例对剥/不剥敏感。
func TestSendOutbound_Bridge_MarkdownAlignedWithManualPath(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	setupBridgeWhitelistForTest(t, "douyin")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	ingress := NewInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	defer SetGlobalInboxIngressService(nil)

	md := "**限时优惠**：详情见 [产品页](https://example.com/p)\n- 第一步\n- 第二步\n```go\nfmt.Println(1)\n```"
	stripped := StripMarkdownForDM(md)
	if stripped == md {
		t.Fatal("对照失效：StripMarkdownForDM 对该样例无变化，本用例无法区分 R5 前后行为，请补样例")
	}

	const account = "acct-b5-md"

	// AI 路径：sendOutbound 桥接分支（douyin）
	const convAI = "conv-b5-md-ai"
	hub := &model.MessageHub{
		MsgID:          "mh:in-b5-md-ai",
		Platform:       "douyin",
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "cust-b5-md-ai",
		Content:        "有什么优惠",
		ConversationID: convAI,
		SentAt:         time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-b5-md-ai")
	p := &ParsedPayload{EventID: "evt-b5-md-ai", Sender: hub.SenderID, Content: "有什么优惠", ChatID: convAI}
	if sent, err := svc.sendOutbound(context.Background(), ChannelDouyin, account, p, md, hub, nil); err != nil || !sent {
		t.Fatalf("AI 路径出库应成功: sent=%v err=%v", sent, err)
	}
	var rowAI model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", convAI).
		First(&rowAI).Error; err != nil {
		t.Fatalf("读回 AI 路径出站行: %v", err)
	}

	// 人工/触达路径：DeliverBridgeOutbound（同一内容）
	const convManual = "conv-b5-md-manual"
	if err := DeliverBridgeOutbound(context.Background(), "douyin", account, convManual, "text", md, "evt-b5-md-manual"); err != nil {
		t.Fatalf("人工路径入队失败: %v", err)
	}
	var rowManual model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", convManual).
		First(&rowManual).Error; err != nil {
		t.Fatalf("读回人工路径出站行: %v", err)
	}

	if rowAI.Content != stripped {
		t.Errorf("R5 对齐：AI 路径出站内容应等于剥离结果。\n got  = %q\n want = %q", rowAI.Content, stripped)
	}
	if rowAI.Content == md {
		t.Error("AI 路径仍原样下发 Markdown —— R5 批准的行为变更被回退（变更前行为）")
	}
	if rowManual.Content != stripped {
		t.Errorf("人工路径基线漂移：出站内容应等于剥离结果。\n got  = %q\n want = %q", rowManual.Content, stripped)
	}
	if rowAI.Content != rowManual.Content {
		t.Errorf("AI 与人工路径剥离结果必须一致：\n AI     = %q\n manual = %q", rowAI.Content, rowManual.Content)
	}
	// Sender 契约在两路径上同口径。
	if rowAI.SenderID != account || rowManual.SenderID != account {
		t.Errorf("SenderID ≡ accountID（两路径）：AI=%q manual=%q, want %q", rowAI.SenderID, rowManual.SenderID, account)
	}
}

// TestDeliverBridgeOutbound_SenderIDEqualsAccountID 人工/触达路径的 Sender 契约（B5）：
// DeliverBridgeOutboundWithReceipt 落库行 SenderID ≡ accountID，重发同一会话不换 sender。
func TestDeliverBridgeOutbound_SenderIDEqualsAccountID(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	const (
		channel   = "douyin"
		accountID = "acc_b5_manual"
		convID    = "conv_b5_manual"
		content   = "主动触达消息"
	)

	hubID, msgID, err := DeliverBridgeOutboundWithReceipt(context.Background(),
		channel, accountID, convID, "text", content, "evt_b5_manual_1")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundWithReceipt 失败: %v", err)
	}
	if hubID == 0 || msgID == "" {
		t.Fatalf("回执键必须可用（操作者凭据）：hubID=%d msgID=%q", hubID, msgID)
	}

	var row model.MessageHub
	if err := db.Where("id = ?", hubID).First(&row).Error; err != nil {
		t.Fatalf("读回出站行: %v", err)
	}
	if row.SenderID != accountID {
		t.Errorf("Sender 契约（B5）：出站行 SenderID = %q, want accountID %q", row.SenderID, accountID)
	}
	if row.AccountID != accountID {
		t.Errorf("AccountID = %q, want %q", row.AccountID, accountID)
	}
	if row.ReceiverID != convID {
		t.Errorf("ReceiverID = %q, want conversationID %q", row.ReceiverID, convID)
	}
	if row.IsAIReply {
		t.Error("人工/触达路径 IsAIReply 应为 false")
	}

	// 同一会话再次出站（重发）：sender 不换。
	hubID2, _, err := DeliverBridgeOutboundWithReceipt(context.Background(),
		channel, accountID, convID, "text", "补充一条", "evt_b5_manual_2")
	if err != nil {
		t.Fatalf("重发失败: %v", err)
	}
	var row2 model.MessageHub
	if err := db.Where("id = ?", hubID2).First(&row2).Error; err != nil {
		t.Fatalf("读回重发行: %v", err)
	}
	if row2.SenderID != accountID || row2.SenderID != row.SenderID {
		t.Errorf("重发不换 sender：first=%q second=%q, want %q", row.SenderID, row2.SenderID, accountID)
	}
}
