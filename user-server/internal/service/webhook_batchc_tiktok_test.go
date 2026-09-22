package service

// 批C D-01：TikTok 复用抖音入站解析，但落库时 `Platform` 写死 "douyin" ——
// 同一条 TikTok 客户消息在 message_hub / 线索表里被记成抖音，出站按平台回原渠道
// 时也会走错链路。这里要求：解析逻辑可以共用，**平台标记与幂等键必须按渠道分开**。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestDispatchDouyin_TikTokMustNotBeLabelledDouyin(t *testing.T) {
	svc, ctx := newDouyinGenericTestService(t)
	t.Cleanup(func() { svc.Stop(ctx) })

	body := []byte(`{"event":"im_receive_msg","client_key":"ck_c","from_user_id":"u_1","to_user_id":"bot_1",` +
		`"content":{"conversation_short_id":"@c_1","server_message_id":"m_5001","create_time":1681303285997,` +
		`"message_type":"text","text":"多少钱","user_infos":[{"open_id":"u_1","nick_name":"客户甲"}]}}`)

	dyHub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "7", &ParsedPayload{}, body)
	if err != nil {
		t.Fatalf("douyin dispatch: %v", err)
	}
	if dyHub == nil {
		t.Fatal("douyin dispatch 应产出 hub 行")
	}
	ttHub, _, err := svc.dispatchDouyin(ctx, ChannelTiktok, "7", &ParsedPayload{}, body)
	if err != nil {
		t.Fatalf("tiktok dispatch: %v", err)
	}
	if ttHub == nil {
		t.Fatal("tiktok dispatch 应产出 hub 行")
	}

	if dyHub.Platform != "douyin" {
		t.Errorf("抖音入站 Platform 应为 douyin，got %q", dyHub.Platform)
	}
	if ttHub.Platform != "tiktok" {
		t.Errorf("同一份报文经 tiktok 渠道入站，Platform 必须是 tiktok（D-01），got %q", ttHub.Platform)
	}
	if dyHub.MsgID == ttHub.MsgID {
		t.Errorf("两个平台的同一 message_id 不得共用幂等键（会互相吞消息）：%q", ttHub.MsgID)
	}
	if !strings.HasPrefix(ttHub.MsgID, "tt_") {
		t.Errorf("tiktok 幂等键前缀应为 tt_，got %q", ttHub.MsgID)
	}

	var cnt int64
	if err := svc.lazyDB().Model(&model.MessageHub{}).
		Where("conversation_id = ?", "@c_1").Count(&cnt).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if cnt != 2 {
		t.Errorf("两个平台各入库一行，实际 %d", cnt)
	}
}

func TestDispatchDouyinGeneric_TikTokKeyScopedByPlatform(t *testing.T) {
	svc, ctx := newDouyinGenericTestService(t)
	t.Cleanup(func() { svc.Stop(ctx) })

	p := func() *ParsedPayload { return &ParsedPayload{Sender: "u_9", Content: "同一内容"} }

	dy, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "8", p(), []byte("{not-json"))
	if err != nil || dy == nil {
		t.Fatalf("douyin generic dispatch: hub=%+v err=%v", dy, err)
	}
	tt, _, err := svc.dispatchDouyin(ctx, ChannelTiktok, "8", p(), []byte("{not-json"))
	if err != nil || tt == nil {
		t.Fatalf("tiktok generic dispatch: hub=%+v err=%v", tt, err)
	}

	if dy.MsgID == tt.MsgID {
		t.Errorf("内容哈希必须带上平台，否则两平台同内容会互相判重复：%q", tt.MsgID)
	}
	// 批G 起通用键也带账号段（N-16 同一结论：不带账号的键在第二家账号上撞唯一键后被吞掉）。
	if tt.Platform != "tiktok" || !strings.HasPrefix(tt.MsgID, "tt_8_generic_") {
		t.Errorf("tiktok 通用分支应为 tiktok + tt_<account>_generic_*，got platform=%q msgID=%q", tt.Platform, tt.MsgID)
	}
	if want := "tt_8_generic_" + ContentHashMsgID("tiktok", "u_9", "同一内容"); tt.MsgID != want {
		t.Errorf("tiktok 幂等键 expected %s, got %s", want, tt.MsgID)
	}
}

func TestLeadAdapterForTikTokIsNotDouyin(t *testing.T) {
	// 线索表平台标记：tiktok 必须是自己的 ClueType，不能记成抖音。
	dy := LeadAdapterForPlatform("douyin")
	tt := LeadAdapterForPlatform("tiktok")
	if dy.Channel() != "douyin" || tt.Channel() != "tiktok" {
		t.Fatalf("adapter channel 标记错误：%q / %q", dy.Channel(), tt.Channel())
	}
	if dy.ClueType() == tt.ClueType() {
		t.Fatalf("tiktok 与 douyin 的线索类型不得相同（%d），否则线索统计混成一家", tt.ClueType())
	}
}

func TestDouyinLeadAdapterForHub_SelectsByHubPlatform(t *testing.T) {
	// mineDouyinGroupLead 的适配器必须由 hub.Platform 决定：
	// 只测 LeadAdapterForPlatform 工厂不够，写死 DouyinLeadAdapter{} 一样能过工厂测试。
	if got := douyinLeadAdapterForHub(&model.MessageHub{Platform: "tiktok"}); got.Channel() != "tiktok" {
		t.Errorf("tiktok hub 必须用 tiktok 适配器（D-01），got %q", got.Channel())
	}
	if got := douyinLeadAdapterForHub(&model.MessageHub{Platform: "douyin"}); got.Channel() != "douyin" {
		t.Errorf("douyin hub 必须保持 douyin 适配器，got %q", got.Channel())
	}
	if got := douyinLeadAdapterForHub(nil); got.Channel() != "douyin" {
		t.Errorf("hub 为 nil 时应回退 douyin 适配器，got %q", got.Channel())
	}
	if douyinLeadAdapterForHub(&model.MessageHub{Platform: "tiktok"}).ClueType() ==
		douyinLeadAdapterForHub(&model.MessageHub{Platform: "douyin"}).ClueType() {
		t.Error("两平台线索类型不得相同")
	}
}

// tikTokGroupBody 官方**群**消息事件（im_group_receive_msg）。
//
// 批C 这三条用例盯的是「两家平台标记/幂等键必须分开」（D-01），与外壳形态无关，
// 所以批G 只把夹具从旧实现自带的飞书式外壳换成官方信封，断言一条不动。
// 官方形态要点：群/单聊的区分**全在事件名**上（信封里没有 group 字段），
// 昵称只出现在 content.user_infos[]（信封上没有）。
func tikTokGroupBody(msgID, userID, nick, text string) []byte {
	return []byte(`{"event":"im_group_receive_msg","client_key":"ck_c","from_user_id":"` + userID +
		`","to_user_id":"bot_1","log_id":"lg_` + msgID + `",` +
		`"content":{"conversation_short_id":"@c_group_1","server_message_id":"` + msgID +
		`","conversation_type":1,"create_time":1681303285997,"message_type":"text","text":"` + text +
		`","user_infos":[{"open_id":"` + userID + `","nick_name":"` + nick + `"}]}}`)
}

// TestDispatchDouyin_LeadRowKeepsPlatform 端到端：同一个客户名分别经抖音、TikTok 入站，
// 线索表必须各落一行、类型分别为 douyin / tiktok。写死 DouyinLeadAdapter 时两行会并成一个类型。
func TestDispatchDouyin_LeadRowKeepsPlatform(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.Clue{},
	)
	svc := NewWebhookService(db)
	ctx := context.Background()
	t.Cleanup(func() { svc.Stop(ctx) })

	if _, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "71001", &ParsedPayload{},
		tikTokGroupBody("m_lead_dy", "u_lead_dy", "客户同步", "这个产品多少钱")); err != nil {
		t.Fatalf("douyin dispatch: %v", err)
	}
	if _, _, err := svc.dispatchDouyin(ctx, ChannelTiktok, "71001", &ParsedPayload{},
		tikTokGroupBody("m_lead_tt", "u_lead_tt", "客户同步", "这个产品多少钱")); err != nil {
		t.Fatalf("tiktok dispatch: %v", err)
	}

	var clues []model.Clue
	if err := db.Where("account = ?", "@客户同步").Order("type asc").Find(&clues).Error; err != nil {
		t.Fatalf("读线索: %v", err)
	}
	if len(clues) != 2 {
		t.Fatalf("两平台应各出一条线索，实际 %d 条（%+v）", len(clues), clues)
	}
	got := map[int64]bool{}
	for _, c := range clues {
		got[c.Type] = true
	}
	if !got[ClueTypeDouyin] || !got[ClueTypeTikTok] {
		t.Errorf("线索类型必须覆盖 douyin(%d)+tiktok(%d)，实际 %+v", ClueTypeDouyin, ClueTypeTikTok, got)
	}
}

// TestTriggerBridgeDMOutreach_EnqueuesUnderOwnChannel 商机票的私信必须进自己渠道的出站队列：
// 原先写死 douyin，TikTok/小红书的私信会被塞进抖音队列（且抖音侧根本无该账号）。
func TestTriggerBridgeDMOutreach_EnqueuesUnderOwnChannel(t *testing.T) {
	setupBridgeWhitelistForTest(t, string(ChannelDouyin), string(ChannelTiktok))
	db := testutil.NewTestDB(t, &model.MessageHub{})
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(nil) })

	svc := NewWebhookService(db)
	ctx := context.Background()
	t.Cleanup(func() { svc.Stop(ctx) })

	// 走适配器入口：适配器把哪个渠道传给私信逻辑也是本用例要守的（否则写死 douyin 一样能过）。
	bridgeLeadAdapterForChannel(string(ChannelTiktok)).
		TriggerOutreach(ctx, svc, "acc_tt_dm", "u_dm_1", "g_dm", "TikTok 群", 80, "多少钱")
	ttPending, err := ingress.ListPendingOutbound(ctx, string(ChannelTiktok), "acc_tt_dm")
	if err != nil {
		t.Fatalf("list tiktok pending: %v", err)
	}
	if len(ttPending) != 1 {
		t.Fatalf("tiktok 私信应入自己的队列，实际 %d 条", len(ttPending))
	}
	if ttPending[0].ConversationID != "tt_dm_u_dm_1" {
		t.Errorf("tiktok 私信会话键应为 tt_dm_*，got %q", ttPending[0].ConversationID)
	}

	dyPending, err := ingress.ListPendingOutbound(ctx, string(ChannelDouyin), "acc_tt_dm")
	if err != nil {
		t.Fatalf("list douyin pending: %v", err)
	}
	if len(dyPending) != 0 {
		t.Errorf("tiktok 商机不得出现在抖音出站队列，实际 %d 条", len(dyPending))
	}

	var rows []model.MessageHub
	if err := db.Where("conversation_id = ?", "tt_dm_u_dm_1").Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读私信行: %v", err)
	}
	// 入队行 + 归因回写只许有一行：插第二行会让 outbox 重发、会话流出现重复气泡。
	if len(rows) != 1 {
		t.Fatalf("私信在 message_hub 只应有一行，实际 %d 行：%+v", len(rows), rows)
	}
	event := rows[0]
	if event.Platform != "tiktok" {
		t.Errorf("私信行 Platform 必须是 tiktok，got %q", event.Platform)
	}
	if event.Status != "pending" {
		t.Errorf("私信行必须保持 pending 等待桥接领取，got %q", event.Status)
	}
	if !event.IsAIReply || event.AIAgent != "lead_outreach" {
		t.Errorf("归因信息必须回写到同一行，got isAIReply=%v agent=%q", event.IsAIReply, event.AIAgent)
	}
	if got := event.Extra["scenario"]; got != "group_to_dm" {
		t.Errorf("Extra.scenario 应为 group_to_dm，got %+v", event.Extra)
	}

	// 抖音侧行为必须与改造前一致：短名仍是 dy、渠道仍是 douyin。
	DouyinLeadAdapter{}.TriggerOutreach(ctx, svc, "acc_dy_dm", "u_dm_2", "g_dy", "抖音群", 80, "多少钱")
	dyOwn, err := ingress.ListPendingOutbound(ctx, string(ChannelDouyin), "acc_dy_dm")
	if err != nil {
		t.Fatalf("list douyin pending(own): %v", err)
	}
	if len(dyOwn) != 1 || dyOwn[0].ConversationID != "dy_dm_u_dm_2" {
		t.Errorf("抖音私信行为回归：期望 1 条 dy_dm_u_dm_2，实际 %+v", dyOwn)
	}
}

// TestDispatchToChannel_DouyinFamilyKeepsChannel 覆盖「分发处 → dispatchDouyin」这一跳：
// 只测 dispatchDouyin 自身不够，分发处若把 channel 写死成 ChannelDouyin，
// TikTok 入站仍会被标成抖音，而上面的单元用例照样是绿的。
func TestDispatchToChannel_DouyinFamilyKeepsChannel(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.Clue{},
	)
	svc := NewWebhookService(db)
	ctx := context.Background()
	t.Cleanup(func() { svc.Stop(ctx) })

	ttHub, _, err := svc.dispatchToChannel(ctx, ChannelTiktok, "71009", &ParsedPayload{},
		tikTokGroupBody("m_wire_tt", "u_wire_tt", "客户甲", "这个产品多少钱"), nil)
	if err != nil {
		t.Fatalf("tiktok dispatchToChannel: %v", err)
	}
	if ttHub == nil || ttHub.Platform != "tiktok" {
		t.Fatalf("分发处丢失了 tiktok 渠道（D-01），got %+v", ttHub)
	}

	dyHub, _, err := svc.dispatchToChannel(ctx, ChannelDouyin, "71009", &ParsedPayload{},
		tikTokGroupBody("m_wire_dy", "u_wire_dy", "客户乙", "这个产品多少钱"), nil)
	if err != nil {
		t.Fatalf("douyin dispatchToChannel: %v", err)
	}
	if dyHub == nil || dyHub.Platform != "douyin" {
		t.Fatalf("抖音分发平台标记错误，got %+v", dyHub)
	}

	var ttClue, dyClue model.Clue
	if err := db.Where("account = ?", "@客户甲").First(&ttClue).Error; err != nil {
		t.Fatalf("tiktok 线索未入库: %v", err)
	}
	if ttClue.Type != ClueTypeTikTok {
		t.Errorf("经分发链路的 tiktok 线索类型应为 %d，got %d", ClueTypeTikTok, ttClue.Type)
	}
	if err := db.Where("account = ?", "@客户乙").First(&dyClue).Error; err != nil {
		t.Fatalf("douyin 线索未入库: %v", err)
	}
	if dyClue.Type != ClueTypeDouyin {
		t.Errorf("经分发链路的抖音线索类型应为 %d，got %d", ClueTypeDouyin, dyClue.Type)
	}
}
