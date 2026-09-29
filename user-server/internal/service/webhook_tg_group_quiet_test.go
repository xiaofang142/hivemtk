package service

// 三人行社群「发了没人回」的两处静默面回归用例（Telegram 渠道）：
//
//	① 群消息触发判定：修复前是 handleJob 里一个只有两条 case、没有 default 也没有
//	   任何日志的 switch ⇒ 群里的普通发言在日志里连"收到了但没回"的痕迹都不留。
//	② 门控互锁：mute_unlock 管控群会把"台账里没有这一行"的人一并判为未验证，
//	   而门控装群之前就在线上的老成员永远不会产生入群事件 ⇒ 没人给他发过验证链接，
//	   互锁对他是终身的，且同样一声不响。
//	③ 轮询自投地址：polling 把 update 自投回自家 /api/webhook/telegram/:id，
//	   修复前基址写死 8204，而服务听的是 PORT ⇒ update 取到了、投给了别的进程，
//	   群里依旧零回复。这里用假 RoundTripper 断言真实出站地址（不发真请求，
//	   免得把垃圾 POST 打进同机其他会话的服务）。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// r49TGSetup 起一条带门控表的 TG 入站管线。账号 id 固定 1（NewTestDB 每次给全新库）。
func r49TGSetup(t *testing.T, token, botUsername string) (*WebhookService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.TelegramAccount{},
		&model.WebhookEvent{},
		&model.Customer{},
		&model.IntegrationAccount{},
		&model.UnifiedMessage{},
		&model.Clue{},
		&model.ClueScore{},
		&model.ClueEngagementEvent{},
		&model.TelegramGroupGate{},
		&model.TelegramGroupMember{},
	)
	acc := &model.TelegramAccount{AccountName: "门控号", BotToken: token, BotUsername: botUsername, Status: 1}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("种 TG 账号: %v", err)
	}
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })
	return ws, db
}

func r49GroupUpdate(updateID, msgID, userID int64, text string) []byte {
	return []byte(`{"update_id":` + strconv.FormatInt(updateID, 10) + `,"message":{"message_id":` + strconv.FormatInt(msgID, 10) +
		`,"from":{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"OldTimer","username":"oldtimer"},` +
		`"chat":{"id":-100,"type":"supergroup","title":"三人行 — 泛技术交流"},"date":1700000000,"text":"` + text + `"}}`)
}

// r49Ask 是真人群里已过审成员的一句问话（hub 926 @15:18:15 原文），用作"该回"的正对照；
// r49Noise 一组是同期真发过但不该占 AI 的正文（图片占位、/start 回调、成员间自我介绍）。
const r49Ask = "群主的微信是多少"

func TestTGGroupTriggerDecisionReasons(t *testing.T) {
	cases := []struct {
		mentioned, newOpp, allowed bool
		verified                   bool // 门控群里的已验证成员发言
		msgType, content           string
		wantFire                   bool
		wantReason                 string
	}{
		{true, false, false, false, "text", "今天天气不错", true, tgTriggerReasonMention},
		// @ 与商机各有各的分支，正文像不像问话都不该被内容判据二次拦掉
		{true, true, true, true, "image", "[图片]", true, tgTriggerReasonMention},
		{false, true, true, false, "text", "认识一下大家，请多指教", true, tgTriggerReasonOpportunity},
		{false, true, false, false, "text", r49Ask, false, tgTriggerReasonOpportunityCooldown},
		{false, false, true, false, "text", r49Ask, false, tgTriggerReasonNoTrigger},
		{false, false, false, false, "text", r49Ask, false, tgTriggerReasonNoTrigger},
		// 已验证成员的问话：不 @ 也算咨询，必须有人应答
		{false, false, false, true, "text", r49Ask, true, tgTriggerReasonVerifiedSpeaker},
		{false, false, true, true, "text", "这个产品多少钱", true, tgTriggerReasonVerifiedSpeaker},
		// 冷却只该挡住"主动触达"，不该让已验证成员的问话没人答
		{false, true, false, true, "text", "群主在吗  作者在吗", true, tgTriggerReasonVerifiedSpeaker},
		// 过了审但正文不是问话：图片/表情/贴纸占位、验证回调、成员之间的自我介绍都不该回
		{false, false, false, true, "image", "[图片]", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "[图片]", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "[表情]", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "/start 52ef8e6aa62482be156efa17406", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "认识一下大家，请多指教", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "？？？", false, tgTriggerReasonSpeechNoAsk},
		{false, false, false, true, "text", "", false, tgTriggerReasonSpeechNoAsk},
		// 现证的取舍：成员向全群发问（不是向机器人）目前仍会回——它确实是疑问句，
		// 判"问谁"要读语义。改动这条得先加语义判据，别只改期望值。
		{false, false, false, true, "text", "各位都是哪里的啊", true, tgTriggerReasonVerifiedSpeaker},
	}
	for _, c := range cases {
		fire, reason := telegramGroupTriggerDecision(c.mentioned, c.newOpp, c.allowed, c.verified, c.msgType, c.content)
		if fire != c.wantFire || reason != c.wantReason {
			t.Errorf("mentioned=%v newOpp=%v allowed=%v verified=%v msgType=%q content=%q: got=(%v,%q) want=(%v,%q)",
				c.mentioned, c.newOpp, c.allowed, c.verified, c.msgType, c.content, fire, reason, c.wantFire, c.wantReason)
		}
	}
}

// 未触发必须出声，且触发的两档必须不出声（否则日志行数无法当证据用）。
func TestTGGroupShouldTriggerAILogsWithReason(t *testing.T) {
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	// 正对照：只换 os.Stdout 不重建日志器会一行也抓不到（GetLogger 缓存实例）。
	logger.GetLogger().Info().Msg("tg-quiet-capture-probe")
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	tgGroupShouldTriggerAI(true, false, false, func() bool { return false }, "evt-mention", "-100", "777", "text", "今天天气不错")
	tgGroupShouldTriggerAI(false, false, false, func() bool { return true }, "evt-quiet", "-100", "777", "text", r49Ask)
	tgGroupShouldTriggerAI(false, true, false, func() bool { return false }, "evt-cooldown", "-100", "777", "text", r49Ask)
	// 已验证成员的发言走的是放行那条路：放行也要出声，否则"为什么回了"读不出来
	tgGroupShouldTriggerAI(false, false, true, func() bool { return true }, "evt-verified", "-100", "777", "text", r49Ask)
	// 过了审但正文是图片占位：判据要出声说"是没回，因为不像问话"，不能悄悄咽掉
	tgGroupShouldTriggerAI(false, false, true, func() bool { return true }, "evt-noask", "-100", "777", "image", "[图片]")

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	logged := string(captured)

	if n := len(linesWith(logged, "[Webhook] TG 群消息未触发 AI")); n != 3 {
		t.Errorf("未触发行应恰好 3 条，实际 %d 条，捕获日志：\n%s", n, logged)
	}
	noask := linesWith(logged, "reason=speech_not_question event=evt-noask ")
	if len(noask) != 1 {
		t.Errorf("过了审但正文不像问话的判定行应恰好 1 条，实际 %d 条：\n%s", len(noask), logged)
	}
	quiet := linesWith(logged, "reason=no_mention_no_opportunity event=evt-quiet ")
	if len(quiet) != 1 {
		t.Errorf("未 @ 未商机的判定行应恰好 1 条，实际 %d 条：\n%s", len(quiet), logged)
	}
	cool := linesWith(logged, "reason=opportunity_outreach_cooldown event=evt-cooldown ")
	if len(cool) != 1 {
		t.Errorf("冷却挡下的判定行应恰好 1 条，实际 %d 条：\n%s", len(cool), logged)
	}
	mention := linesWith(logged, "reason=mention event=evt-mention ")
	if len(mention) != 1 {
		t.Errorf("@机器人 放行行应恰好 1 条，实际 %d 条：\n%s", len(mention), logged)
	}
	verified := linesWith(logged, "reason=verified_member_speech event=evt-verified ")
	if len(verified) != 1 {
		t.Errorf("已验证成员放行行应恰好 1 条，实际 %d 条：\n%s", len(verified), logged)
	}
	// 两分支的串必须互不含子串，否则上面的按名计数会把放行读成拦截
	if n := len(linesWith(logged, "[Webhook] TG 群消息触发 AI")); n != 2 {
		t.Errorf("触发行应恰好 2 条（mention+verified），实际 %d 条：\n%s", n, logged)
	}
}

// 冷却位只能由真商机消耗：普通闲聊若也去抢 SetNX，等于替该发言人随后 30 分钟的
// 真商机主动触达预先点火。
func TestTGGroupShouldTriggerAIDoesNotBurnOutreachSlot(t *testing.T) {
	calls := 0
	if tgGroupShouldTriggerAI(false, false, false, func() bool { calls++; return true }, "evt-lazy", "-100", "777", "text", "今天天气不错") {
		t.Error("未 @ 未商机不应触发")
	}
	if calls != 0 {
		t.Errorf("普通闲聊调了 outreach 冷却判定 %d 次，应为 0（SetNX 一去不回）", calls)
	}
	calls = 0
	// 已验证成员触发时同样不许去抢冷却位：他回的是群内消息，不是主动触达
	if !tgGroupShouldTriggerAI(false, false, true, func() bool { calls++; return true }, "evt-lazy-verified", "-100", "777", "text", r49Ask) {
		t.Error("已验证成员的发言应触发")
	}
	if calls != 0 {
		t.Errorf("已验证成员闲聊调了 outreach 冷却判定 %d 次，应为 0", calls)
	}
	calls = 0
	if !tgGroupShouldTriggerAI(false, true, false, func() bool { calls++; return true }, "evt-opp", "-100", "777", "text", r49Ask) {
		t.Error("新商机 + 冷却放行时应触发")
	}
	if calls != 1 {
		t.Errorf("新商机应恰好调 1 次冷却判定，实际 %d 次", calls)
	}
}

// 门控群里的无台账老成员：互锁照旧（不能让他绕过验证），但他必须当场拿到验证入口，
// 且第二次发言不再重复骚扰。
func TestTGGroupUnledgeredMemberGetsVerificationInvite(t *testing.T) {
	const token = "555:AAA-gate-token"
	ws, db := r49TGSetup(t, token, "gatebot")
	if err := db.Create(&model.TelegramGroupGate{
		AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
		Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
	}).Error; err != nil {
		t.Fatalf("种门控: %v", err)
	}

	// 发言人身份显式给成普通成员：互锁 + 补发邀请这条路只对他成立。
	// 不设值时 stub 回的是 "result":true，getChatMember 解不进 map —— 那测的是"TG 返回异常"，
	// 而不是"普通人照样拦"。
	stub := &tgAPIStub{status: http.StatusOK, chatMemberStatus: "member"}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(srv.Close)
	ws.tgGate.SetAPIBase(srv.URL)

	hub, extra, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, r49GroupUpdate(9001, 11, 777, "@gatebot 有人在吗"))
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub == nil {
		t.Fatal("群消息应落 hub 行")
	}
	if extra == nil || !extra.GateHandled || !extra.GateMuted {
		t.Fatalf("门控群未验证成员应 GateHandled+GateMuted，实际 %+v", extra)
	}

	var m model.TelegramGroupMember
	if err := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", 1, "-100", "777").First(&m).Error; err != nil {
		t.Fatalf("老成员没被补出台账行 ⇒ 互锁对他仍是终身的: %v", err)
	}
	if m.VerifyToken == "" {
		t.Error("补出的台账没有 verify_token ⇒ 他没有可点的验证链接")
	}
	if m.Authorized {
		t.Error("补台账不等于放行：Authorized 必须仍是 false")
	}
	if !stub.called("restrictChatMember") {
		t.Errorf("补发邀请应同时执行禁言，实际调用：%v", stub.snapshot())
	}
	if !stub.called("sendMessage") {
		t.Errorf("补发邀请应在群里发出验证提示，实际调用：%v", stub.snapshot())
	}

	before := len(stub.snapshot())
	if _, again, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, r49GroupUpdate(9002, 12, 777, "第二次发言")); err != nil {
		t.Fatalf("第二次 dispatchTelegram: %v", err)
	} else if !again.GateMuted {
		t.Error("第二次仍应被互锁挡住")
	}
	if delta := len(stub.snapshot()) - before; delta != 0 {
		t.Errorf("已有台账还补发邀请 = 重复骚扰，新增 %d 次 TG 调用", delta)
	}
}

// 门控群的群主发言：不能走"先禁言再发邀请"（Bot 无权动群主 ⇒ 禁言必失败 ⇒ 邀请也发不出去 ⇒
// 互锁终身），必须就地认身份、直接放行，让消息回到正常的触发判定链上。
// 这就是"三人行群里说话外面零回复"的第三层根因：报障人本身是群主。
func TestTGGroupCreatorSpeakerIsNotInterlocked(t *testing.T) {
	const token = "556:AAA-owner-token"
	ws, db := r49TGSetup(t, token, "gatebot")
	if err := db.Create(&model.TelegramGroupGate{
		AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
		Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
	}).Error; err != nil {
		t.Fatalf("种门控: %v", err)
	}

	stub := &tgAPIStub{status: http.StatusOK, chatMemberStatus: "creator"}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(srv.Close)
	ws.tgGate.SetAPIBase(srv.URL)

	hub, extra, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, r49GroupUpdate(9101, 21, 8888, "群主在吗 有人在吗"))
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub == nil {
		t.Fatal("群消息应落 hub 行")
	}
	if extra == nil {
		t.Fatal("extra 不应为空")
	}
	if extra.GateHandled || extra.GateMuted {
		t.Errorf("群主被门控互锁吞掉了 ⇒ 外面不会有任何回复，实际 %+v（TG 调用：%v）", extra, stub.snapshot())
	}
	calls := stub.snapshot()
	if len(calls) != 1 || calls[0] != "getChatMember" {
		t.Errorf("群主这条消息只该核一次身份，实际调用：%v", calls)
	}

	var m model.TelegramGroupMember
	if err := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", 1, "-100", "8888").First(&m).Error; err != nil {
		t.Fatalf("群主没有授权台账 ⇒ 下一条消息还要再互锁一轮: %v", err)
	}
	if !m.Authorized || m.JoinStatus != model.TGMemberApproved {
		t.Errorf("群主台账应为已授权 approved，实际 authorized=%v join_status=%s", m.Authorized, m.JoinStatus)
	}

	// 第二条消息：判定读台账，不再打 TG，也不该再被吞
	before := len(stub.snapshot())
	if _, again, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, r49GroupUpdate(9102, 22, 8888, "第二条")); err != nil {
		t.Fatalf("第二次 dispatchTelegram: %v", err)
	} else if again.GateMuted {
		t.Error("已授权的群主第二次仍被互锁")
	}
	if delta := len(stub.snapshot()) - before; delta != 0 {
		t.Errorf("第二条消息又调了 %d 次 TG，应读台账", delta)
	}
}

// 非门控群 / 已授权成员都必须让 MemberLacksLedger 闭嘴，否则会给他们也补发验证。
func TestMemberLacksLedgerScopes(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupGate{}, &model.TelegramGroupMember{})
	svc := NewTelegramGateService(db)
	ctx := context.Background()

	if svc.MemberLacksLedger(ctx, 1, "-900", "1") {
		t.Error("无门控配置的群不应算作缺台账")
	}
	if err := db.Create(&model.TelegramGroupGate{AccountID: 1, ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: false, VerifyTTLMin: 10}).Error; err != nil {
		t.Fatalf("种停用门控: %v", err)
	}
	if svc.MemberLacksLedger(ctx, 1, "-100", "2") {
		t.Error("门控停用时无台账应不拦截")
	}
	if err := db.Create(&model.TelegramGroupGate{AccountID: 1, ChatID: "-101", Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10}).Error; err != nil {
		t.Fatalf("种启用门控: %v", err)
	}
	if !svc.MemberLacksLedger(ctx, 1, "-101", "3") {
		t.Error("启用门控 + 无台账 ⇒ 应判定为需要补发邀请")
	}
	if err := db.Create(&model.TelegramGroupMember{
		AccountID: 1, ChatID: "-101", UserID: "4",
		JoinStatus: model.TGMemberRestricted, JoinMode: TGGateModeMuteUnlock, VerifyToken: "tok",
	}).Error; err != nil {
		t.Fatalf("种待验证成员: %v", err)
	}
	if svc.MemberLacksLedger(ctx, 1, "-101", "4") {
		t.Error("已有待验证台账的成员不该再补发邀请（他自己没点链接）")
	}
}

// r49CaptureRT 只记录请求、不碰网络，返回 200 让 deliverTelegramUpdate 一次成功。
type r49CaptureRT struct {
	mu     sync.Mutex
	url    string
	header http.Header
}

func (c *r49CaptureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.url = r.URL.String()
	c.header = r.Header.Clone()
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     make(http.Header),
	}, nil
}

func TestTGPollingSelfPostFollowsPORT(t *testing.T) {
	cases := []struct {
		name  string
		port  string
		want  string
		scope string
	}{
		{"WithPORT", "8255", "http://localhost:8255/api/webhook/telegram/9", "设了 PORT 必须跟着 PORT"},
		{"WithoutPORT", "", "http://localhost:8204/api/webhook/telegram/9", "没设 PORT 才回落默认口"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PORT", c.port)
			rt := &r49CaptureRT{}
			raw := json.RawMessage(`{"update_id":1,"message":{"message_id":2}}`)
			if err := deliverTelegramUpdate(context.Background(), &http.Client{Transport: rt}, 9, "topsecret", raw); err != nil {
				t.Fatalf("deliverTelegramUpdate: %v", err)
			}
			if rt.url != c.want {
				t.Errorf("%s：自投地址 = %q，want %q", c.scope, rt.url, c.want)
			}
			// 自投必须带上 webhook secret，否则 controller 的密钥校验会把它判成伪造请求。
			if got := rt.header.Get("X-Telegram-Bot-Api-Secret-Token"); got != "topsecret" {
				t.Errorf("自投密钥头 = %q，want topsecret", got)
			}
		})
	}
}

// 门控群里已验证成员的不 @ 咨询，dispatch 必须把"已验证发言人"这个事实带到判定层：
// 现行词库是带货词库（多少钱/批发/采购），"群主在吗 作者在吗"这类问话意向分只有 8 分
// （阈值 40）⇒ 光靠 @ 或商机两条路，社群里绝大多数咨询永远不会有回复。
func TestTGGroupVerifiedSpeakerPlainChatterIsFlagged(t *testing.T) {
	const token = "557:AAA-verified-token"

	t.Run("门控群已放行成员的闲聊应标为已验证", func(t *testing.T) {
		ws, db := r49TGSetup(t, token, "gatebot")
		if err := db.Create(&model.TelegramGroupGate{
			AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
			Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
		}).Error; err != nil {
			t.Fatalf("种门控: %v", err)
		}
		if err := db.Create(&model.TelegramGroupMember{
			AccountID: 1, ChatID: "-100", UserID: "777", Username: "oldtimer", FullName: "OldTimer",
			JoinMode: TGGateModeMuteUnlock, JoinStatus: model.TGMemberApproved, Authorized: true,
		}).Error; err != nil {
			t.Fatalf("种台账: %v", err)
		}
		// 已验证成员的判定全落在台账上，不该再打 TG 侧
		stub := &tgAPIStub{status: http.StatusOK, chatMemberStatus: "member"}
		srv := httptest.NewServer(http.HandlerFunc(stub.handle))
		t.Cleanup(srv.Close)
		ws.tgGate.SetAPIBase(srv.URL)

		hub, extra, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{},
			r49GroupUpdate(9201, 31, 777, "群主在吗 作者在吗"))
		if err != nil {
			t.Fatalf("dispatchTelegram: %v", err)
		}
		if hub == nil {
			t.Fatal("群消息应落 hub 行")
		}
		if extra == nil {
			t.Fatal("extra 不应为空")
		}
		if extra.GateHandled || extra.GateMuted {
			t.Errorf("已放行成员仍被门控吞掉 ⇒ 群里问话不会有人答，实际 %+v", extra)
		}
		if !extra.SpeakerVerified {
			t.Errorf("已验证成员的发言没被标出来 ⇒ 判定层拿不到这条路，实际 %+v", extra)
		}
		if extra.Mentioned {
			t.Error("正文里没有 @机器人，Mentioned 不得为真（否则这条策略的覆盖面会被高估）")
		}
		if n := len(stub.snapshot()); n != 0 {
			t.Errorf("已验证成员判定只读台账，却调了 %d 次 TG：%v", n, stub.snapshot())
		}
	})

	t.Run("未验证成员不得被标为已验证", func(t *testing.T) {
		ws, db := r49TGSetup(t, token, "gatebot")
		if err := db.Create(&model.TelegramGroupGate{
			AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
			Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
		}).Error; err != nil {
			t.Fatalf("种门控: %v", err)
		}
		if err := db.Create(&model.TelegramGroupMember{
			AccountID: 1, ChatID: "-100", UserID: "888", Username: "newbie", FullName: "Newbie",
			JoinMode: TGGateModeMuteUnlock, JoinStatus: model.TGMemberPending, VerifyToken: "tok-888",
		}).Error; err != nil {
			t.Fatalf("种台账: %v", err)
		}
		hub, extra, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{},
			r49GroupUpdate(9202, 32, 888, "有人吗"))
		if err != nil {
			t.Fatalf("dispatchTelegram: %v", err)
		}
		if hub == nil {
			t.Fatal("群消息应落 hub 行")
		}
		if extra == nil || !extra.GateMuted {
			t.Fatalf("待激活成员应被互锁，实际 %+v", extra)
		}
		if extra.SpeakerVerified {
			t.Error("被互锁的人同时标为已验证 ⇒ 两处判据自相矛盾，放行分支会直接吃掉他")
		}
	})

	// 该腿的发言人**带着已放行台账**（approved + authorized），只是这个群没装门控：
	// 这才是"覆盖面被泼出去"的真实形状。若这里留一行空台账，门控判定被摘掉后
	// MemberVerified 仍因"台账没这一行"返回 false ⇒ 用例照样绿，覆盖面泄漏量不到。
	t.Run("没装门控的群不在本策略内", func(t *testing.T) {
		ws, db := r49TGSetup(t, token, "gatebot")
		if err := db.Create(&model.TelegramGroupMember{
			AccountID: 1, ChatID: "-100", UserID: "999", Username: "plain", FullName: "Plain",
			JoinStatus: model.TGMemberApproved, Authorized: true,
		}).Error; err != nil {
			t.Fatalf("种台账（该群没装门控，台账只是形状）: %v", err)
		}
		if _, extra, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{},
			r49GroupUpdate(9203, 33, 999, "群主在吗 作者在吗")); err != nil {
			t.Fatalf("dispatchTelegram: %v", err)
		} else if extra == nil {
			t.Fatal("extra 不应为空")
		} else if extra.SpeakerVerified {
			t.Error("未装门控的群不该享受已验证成员放行策略（覆盖面要按门控群算，不能顺手扩到全部群）")
		} else if extra.GateMuted {
			t.Error("未装门控的群也不该被互锁")
		}
	})
}

// r49SeverTGAPI 把门控服务的出站基址指向一台只回 401 的假服务器。
// 用例里的 token 本就是假的，但走 handleJob 的腿仍会经过门控的出站面
// （未验证者的补发邀请、禁言）⇒ 不断掉真 api.telegram.org，用例就带真网络、
// 且"0 次触发"的读数分不清是被判据挡下还是被网络拖住。
func r49SeverTGAPI(t *testing.T, ws *WebhookService) *tgAPIStub {
	t.Helper()
	stub := &tgAPIStub{status: http.StatusUnauthorized}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(srv.Close)
	ws.tgGate.SetAPIBase(srv.URL)
	return stub
}

// r49CaptureLogs 收回 fn 期间写出的日志原文（判定行为只有这一处可读证据）。
// 必须同时换 os.Stdout 并重建日志器：GetLogger 缓存实例，只换句柄会一行也抓不到。
func r49CaptureLogs(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("管道创建失败：%v", pipeErr)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	logger.GetLogger().Info().Msg("r49-capture-probe")
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})
	fn()
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	if len(linesWith(string(captured), "r49-capture-probe")) != 1 {
		t.Fatal("日志捕获是哑的（正对照没收到）：本用例读不到任何判定行")
	}
	return string(captured)
}

// handleJob 那一跳也要有用例：门控服务与判定函数都只是产出事实，真正决定"发不发"的是
// handleJob 把 tgExtra.SpeakerVerified 搬进 tgGroupShouldTriggerAI 这一行。少了这条搬运，
// 上游两处再对，群里依旧一声不响（三人行这次就是这么哑的）。
//
// 断言打在判定日志的 reason 上，而不是"引擎被调用了几次"：能过内容判据的正文必然非空，
// 而非空正文会让零值 SalesEngine 在它自己的第 9 步（sales_engine.go 里那处依赖装配好的
// repo/LLM 字段）踩空 panic ⇒ 用例只能把"该回"的正对照留在判定函数层（见
// TestTGGroupTriggerDecisionReasons），在 handleJob 这一跳钉"搬运 + 内容判据"两面。
//
// 每格的四条前提（少一条就是假绿，沿用同包批K 立下的口径）：
//  1. shouldTriggerAI 第一句是 `if s.salesEngine == nil { return false }` ⇒ 必须注入引擎，
//     否则 triggerAI 恒 false，搬运行删掉也全绿。
//  2. 计数器控制腿先跑一次 triggerSalesEngine，把"0 次"钉成"确实没触发"而不是"没接上"。
//  3. 已放行成员的两格（非问话句 / 空正文=生产里的图片贴纸）必须恰好 1 行
//     reason=speech_not_question：这行只有 SpeakerVerified=true 才会出现，搬运漏了就变成
//     no_mention_no_opportunity。
//  4. 未过审两格反过来：既不许有放行行，也不许有 speech_not_question 行（互锁在判定之前）。
func TestTGGroupSpeakerVerifiedTransportAtHandleJob(t *testing.T) {
	cases := []struct {
		name     string
		userID   int64
		ledger   bool // 是否给该发言人种已放行台账
		noLedger bool // 该发言人完全不留台账行（互锁那一格）
		content  string
		wantAsk  bool // 该腿本该走到判定并给出 speech_not_question
		// wantLocalAPI：该腿的门控出站本该发生（特权判定/禁言/补发邀请）。真打出去了且
		// 打在本地假服务器上才算"用例不碰 api.telegram.org"——假服务器不会留下计数的那种
		// 情况，只能说明这条腿压根没出站，与网络无关，不作断言。
		wantLocalAPI bool
	}{
		{"已放行成员·非问话句", 778, true, false, "认识一下大家，请多指教", true, false},
		{"已放行成员·空正文（生产里的图片/贴纸）", 777, true, false, "", true, false},
		{"待激活成员·问话", 888, false, false, "群主的微信是多少", false, false},
		{"无台账成员·问话（互锁）", 999, false, true, "群主的微信是多少", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws, db := r49TGSetup(t, "558:AAA-hjob-token", "gatebot")
			apiStub := r49SeverTGAPI(t, ws)
			// 账号必须同时开着 AI 开关，否则 shouldTriggerAI 恒 false ⇒ 搬运行没有牙齿
			if err := db.Model(&model.TelegramAccount{}).Where("id = 1").
				Update("ai_agent_enabled", true).Error; err != nil {
				t.Fatalf("开 AI 开关: %v", err)
			}
			if err := db.Create(&model.TelegramGroupGate{
				AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
				Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
			}).Error; err != nil {
				t.Fatalf("种门控: %v", err)
			}
			uid := strconv.FormatInt(c.userID, 10)
			if c.ledger {
				if err := db.Create(&model.TelegramGroupMember{
					AccountID: 1, ChatID: "-100", UserID: uid, Username: "speaker" + uid,
					JoinMode: TGGateModeMuteUnlock, JoinStatus: model.TGMemberApproved, Authorized: true,
				}).Error; err != nil {
					t.Fatalf("种台账: %v", err)
				}
			} else if !c.noLedger {
				if err := db.Create(&model.TelegramGroupMember{
					AccountID: 1, ChatID: "-100", UserID: uid, Username: "speaker" + uid,
					JoinMode: TGGateModeMuteUnlock, JoinStatus: model.TGMemberPending, VerifyToken: "tok-" + uid,
				}).Error; err != nil {
					t.Fatalf("种台账: %v", err)
				}
			}

			// 每格一套隔离缓存：ai_processing 排他标记按会话建键，共用会把后跑的腿静默挡下
			ing := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
			ws.SetIngressSvc(ing)
			ws.SetSalesEngine(context.Background(), &SalesEngine{})
			if !ws.shouldTriggerAI(context.Background(), ChannelTelegram, "1") {
				t.Fatal("前置不成立：TG 账号的 AI 开关没生效，后面的计数断言没有意义")
			}
			salesEntry := batchKSalesEngineEntryCounter(t)

			// 控制腿：先证明计数器活着，否则"0 次"分不清是被判据挡下还是根本没接上
			ws.triggerSalesEngine(context.Background(), ChannelTelegram, "1",
				&ParsedPayload{EventID: "ctl-" + uid},
				&model.MessageHub{Platform: "telegram", ConversationID: "ctl-conv-" + uid})
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && salesEntry() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if got := salesEntry(); got != 1 {
				t.Fatalf("计数器是哑的（got %d）：triggerSalesEngine 没在总线上留下计数，本用例的计数断言不成立", got)
			}
			base := salesEntry()

			evtID := "hjob-" + uid + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			raw := r49GroupUpdate(int64(len(evtID))+9300, 41, c.userID, c.content)
			evt := &model.WebhookEvent{
				Platform: string(ChannelTelegram), EventID: evtID, EventType: "message",
				AccountID: "1", RawData: string(raw), Processed: false,
			}
			if err := db.Create(evt).Error; err != nil {
				t.Fatalf("种事件: %v", err)
			}
			// 生产入口是 Receive → 队列 → handleJob；这里直调 handleJob，被测搬运行就在其体内
			logged := r49CaptureLogs(t, func() {
				ws.handleJob(context.Background(), &webhookJob{
					event: evt, raw: raw, channel: ChannelTelegram, account: "1",
				})
			})

			time.Sleep(300 * time.Millisecond)
			delta := salesEntry() - base
			if c.wantLocalAPI {
				if n := len(apiStub.snapshot()); n == 0 {
					t.Error("该腿本该经过门控出站面、却一次没打在本地假服务器上 ⇒ apiBase 没生效，用例其实在敲真 TG")
				}
			}
			// 四格都不该把消息送进 AI（放行格的正对照在判定函数层，见上面注释第 4 条前提）
			if delta != 0 {
				t.Errorf("该腿不该走到 AI（总线增量=%d，应为 0）：\n%s", delta, logged)
			}
			if n := len(linesWith(logged, "[Webhook] TG 群消息触发 AI")); n != 0 {
				t.Errorf("该腿不该有放行行，实际 %d 条：\n%s", n, logged)
			}
			ask := linesWith(logged, "reason="+tgTriggerReasonSpeechNoAsk+" event="+evtID+" ")
			want := 0
			if c.wantAsk {
				want = 1
			}
			if got := len(ask); got != want {
				t.Errorf("speech_not_question 行期望 %d 条、实际 %d 条。已放行成员那两格若读成 0，"+
					"说明 handleJob 没把 SpeakerVerified 搬进判据（未搬运的 reason 会是 no_mention_no_opportunity）；"+
					"未过审那两格若读成 1，说明互锁被绕过。\n%s", got, want, logged)
			}
			if n := len(linesWith(logged, "reason="+tgTriggerReasonVerifiedSpeaker+" event="+evtID+" ")); n != 0 {
				t.Errorf("放行行应为 0 条，实际 %d 条（正文都没过内容判据）：\n%s", n, logged)
			}
		})
	}

	// 没装门控的群：同一条空正文发言不得因为"没被互锁"就享受放行策略
	t.Run("未装门控的群不触发", func(t *testing.T) {
		ws, db := r49TGSetup(t, "559:AAA-nogate-token", "gatebot")
		r49SeverTGAPI(t, ws)
		if err := db.Model(&model.TelegramAccount{}).Where("id = 1").
			Update("ai_agent_enabled", true).Error; err != nil {
			t.Fatalf("开 AI 开关: %v", err)
		}
		if err := db.Create(&model.TelegramGroupMember{
			AccountID: 1, ChatID: "-100", UserID: "1777", Username: "plain",
			JoinStatus: model.TGMemberApproved, Authorized: true,
		}).Error; err != nil {
			t.Fatalf("种台账（该群没装门控，台账只是形状）: %v", err)
		}
		ing := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
		ws.SetIngressSvc(ing)
		ws.SetSalesEngine(context.Background(), &SalesEngine{})
		if !ws.shouldTriggerAI(context.Background(), ChannelTelegram, "1") {
			t.Fatal("前置不成立：TG 账号的 AI 开关没生效")
		}
		salesEntry := batchKSalesEngineEntryCounter(t)
		ws.triggerSalesEngine(context.Background(), ChannelTelegram, "1",
			&ParsedPayload{EventID: "ctl-nogate"},
			&model.MessageHub{Platform: "telegram", ConversationID: "ctl-nogate-conv"})
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && salesEntry() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if salesEntry() != 1 {
			t.Fatal("计数器是哑的，本用例的 0 次断言不成立")
		}
		base := salesEntry()

		evtID := "hjob-nogate-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		raw := r49GroupUpdate(9400, 42, 1777, "")
		evt := &model.WebhookEvent{
			Platform: string(ChannelTelegram), EventID: evtID, EventType: "message",
			AccountID: "1", RawData: string(raw), Processed: false,
		}
		if err := db.Create(evt).Error; err != nil {
			t.Fatalf("种事件: %v", err)
		}
		ws.handleJob(context.Background(), &webhookJob{event: evt, raw: raw, channel: ChannelTelegram, account: "1"})
		time.Sleep(300 * time.Millisecond)
		if delta := salesEntry() - base; delta != 0 {
			t.Errorf("未装门控的群被泼上了放行策略（总线增量=%d，应为 0）⇒ 覆盖面只能按门控群算", delta)
		}
	})
}

// 内容判据的用例全部用三人行社群 09-28 当天的真正文（hub 893/923/926/934/956 与入群事件），
// 而不是编的句子：这一条判据改的是"真人群里谁说话有人答"，夹具必须就是现场那批话。
func TestTGVerifiedSpeechWorthReply(t *testing.T) {
	cases := []struct {
		name    string
		msgType string
		content string
		want    bool
	}{
		// 该回：当天真被回过、且确实是在问话
		{"问群主在不在", "text", "群主在吗  作者在吗", true},
		{"口语疑问助词收尾（嘛＝吗）", "text", "你不是在测试机器人都嘛", true},
		{"问怎么联系", "text", "我想咨询下如何联系群主啊", true},
		{"问微信号", "text", "群主的微信是多少", true},
		{"要验证截图", "text", "可以方便截图看下你们如何 进来验证的吗", true},
		{"商机词库命中（无疑问词）", "text", "这个产品多少钱", true},
		{"只命中词库、不带任何疑问标记（走分数分支）", "text", "想了解一下你们的产品", true},
		{"现证的取舍：向全群发问目前仍算问话", "text", "各位都是哪里的啊", true},
		// 不该回：成员之间的自我介绍/应答/称呼，当天每句都换来过一条 AI 回复
		{"自我介绍", "text", "认识一下大家，请多指教", false},
		{"打招呼", "text", "大家好", false},
		{"裸称呼", "text", "Quinn", false},
		{"收到类应答", "text", "收到", false},
		{"纯符号", "text", "？？？", false},
		{"空正文", "text", "", false},
		// 不该回：非文本与占位正文（含 text 类型却带占位正文的那种兜底）
		{"图片类型", "image", "", false},
		{"图片带问话正文（AI 看不到图，回了是答非所问）", "image", "群主的微信是多少", false},
		{"视频带问话正文", "video", "这个产品多少钱", false},
		{"图片占位正文", "text", "[图片]", false},
		{"表情占位正文", "text", "[表情]", false},
		{"空正文兜底占位", "text", "[消息]", false},
		// 不该回：/start 验证回调（当天 5 条这类入站，正文既非问话也非商机词）
		{"start 回调", "text", "/start 52ef8e6aa62482be156efa17406", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tgVerifiedSpeechWorthReply(c.msgType, c.content); got != c.want {
				t.Errorf("msgType=%q content=%q: got %v want %v", c.msgType, c.content, got, c.want)
			}
		})
	}
}
