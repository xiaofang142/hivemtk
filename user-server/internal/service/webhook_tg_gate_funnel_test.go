package service

// 门控互锁必须管住 AI 触发的每一个漏斗，不只管住入站主路径：
//
//	① RecheckUnrepliedAndTrigger（补触发）读"会话里最后一条入站行"再问一次 AI。
//	   入群/退群这类 msg_type='event' 的系统行同样是入站行，且入站主路径按设计不回它
//	   ⇒ 修复前它被当成"没回的客户消息"喂给 AI，刚被禁言的新人在群里收到一条欢迎语
//	   （三人行 hub 939→941，15:24:23 事件行 → 15:24:28 出站）。
//	② TriggerInboundAI 是补触发/桥接共用的漏斗：门控群里未过验证的人从这里进去，
//	   等于绕过网关拿到回复。

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// r55SeedGate 给会话 -100 装一条启用中的禁言解锁门控。
func r55SeedGate(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.TelegramGroupGate{
		AccountID: 1, ChatID: "-100", ChatTitle: "三人行 — 泛技术交流",
		Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
	}).Error; err != nil {
		t.Fatalf("种门控: %v", err)
	}
}

func r55SeedMember(t *testing.T, db *gorm.DB, userID string, approved bool) {
	t.Helper()
	m := &model.TelegramGroupMember{
		AccountID: 1, ChatID: "-100", UserID: userID, Username: "u" + userID, FullName: "U" + userID,
		JoinMode: TGGateModeMuteUnlock, VerifyToken: "tok-" + userID,
	}
	if approved {
		m.JoinStatus = model.TGMemberApproved
		m.Authorized = true
		m.VerifyToken = ""
	} else {
		m.JoinStatus = model.TGMemberRestricted
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("种台账 %s: %v", userID, err)
	}
}

// r55SeedPrivateShapedGate 在一个"会话 id 像私聊"的 id 上装门控 + 待验证台账。
// 生产里门控只装群，造它只为钉住守卫的 IsGroup 覆盖面：删掉 IsGroup 判定后，
// 这条腿会被按台账挡下 ⇒ 用例转红；只靠③（正数用户 id、查不到门控）量不到这个条件。
func r55SeedPrivateShapedGate(t *testing.T, db *gorm.DB, chatID string) {
	t.Helper()
	if err := db.Create(&model.TelegramGroupGate{
		AccountID: 1, ChatID: chatID, ChatTitle: "私聊形状的门控",
		Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
	}).Error; err != nil {
		t.Fatalf("种私聊形状门控: %v", err)
	}
	if err := db.Create(&model.TelegramGroupMember{
		AccountID: 1, ChatID: chatID, UserID: chatID, Username: "p" + chatID, FullName: "P",
		JoinMode: TGGateModeMuteUnlock, JoinStatus: model.TGMemberRestricted, VerifyToken: "tok-" + chatID,
	}).Error; err != nil {
		t.Fatalf("种私聊形状台账: %v", err)
	}
}

func TestRecheck_SystemEventRowDoesNotTriggerAI(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	now := time.Now()

	// 只有入群事件行的会话（生产形状：hub 939）
	convEventOnly := "r55-conv-event-only"
	// 老问话已超窗 + 之后来了事件行（生产形状：事件行不该把旧问话拖回窗口内）
	convStalePlusEvent := "r55-conv-stale-event"
	// 正对照：真有一条窗口内的客户问话 ⇒ 补触发必须照常发生
	convLiveText := "r55-conv-live-text"

	mustCreate := func(h *model.MessageHub) {
		t.Helper()
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("种 hub 行 %s: %v", h.MsgID, err)
		}
	}
	mustCreate(&model.MessageHub{
		MsgID: "r55-ev1", Platform: "telegram", AccountID: "9", Direction: "inbound",
		MsgType: "event", SenderID: "683825379", ConversationID: convEventOnly,
		Content: "[入群事件] 用户 Dark (@) 加入群组 三人行", IsGroup: true, SentAt: now.Add(-2 * time.Second),
	})
	mustCreate(&model.MessageHub{
		MsgID: "r55-old", Platform: "telegram", AccountID: "9", Direction: "inbound",
		MsgType: "text", SenderID: "8948222221", ConversationID: convStalePlusEvent,
		Content: "六分钟前的问话", IsGroup: true, SentAt: now.Add(-6 * time.Minute),
	})
	mustCreate(&model.MessageHub{
		MsgID: "r55-ev2", Platform: "telegram", AccountID: "9", Direction: "inbound",
		MsgType: "event", SenderID: "7803919906", ConversationID: convStalePlusEvent,
		Content: "[入群事件] 用户 虾 (@) 加入群组 三人行", IsGroup: true, SentAt: now.Add(-2 * time.Second),
	})
	mustCreate(&model.MessageHub{
		MsgID: "r55-txt", Platform: "telegram", AccountID: "9", Direction: "inbound",
		MsgType: "text", SenderID: "8948222221", ConversationID: convLiveText,
		Content: "群主在吗 作者在吗", IsGroup: true, SentAt: now.Add(-2 * time.Second),
	})

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), convEventOnly, "")
	if tr.called != 0 {
		t.Errorf("会话里只有系统事件行时不得补触发 AI，实际调用 %d 次（内容=%q）", tr.called, tr.lastContent)
	}

	svc.RecheckUnrepliedAndTrigger(context.Background(), convStalePlusEvent, "")
	if tr.called != 0 {
		t.Errorf("事件行不得把超窗的旧问话拖回窗口内，实际调用 %d 次（内容=%q）", tr.called, tr.lastContent)
	}

	svc.RecheckUnrepliedAndTrigger(context.Background(), convLiveText, "")
	if tr.called != 1 {
		t.Fatalf("正对照失效：窗口内的真实客户问话应补触发 1 次，实际 %d 次 ⇒ 上面两个 0 次没有判据意义", tr.called)
	}
	if tr.lastContent != "群主在吗 作者在吗" {
		t.Errorf("补触发内容应为文本问话，实际 %q", tr.lastContent)
	}
}

// TriggerInboundAI 的门控守卫：门控群里未过验证的人不得从这个漏斗拿到回复；
// 已放行的人和私聊必须照常进漏斗（否则"0 次"可能只是漏斗本身是哑的）。
func TestTriggerInboundAIGateGuardForTelegramGroups(t *testing.T) {
	const token = "560:AAA-funnel-token"
	ws, db := r49TGSetup(t, token, "gatebot")
	r49SeverTGAPI(t, ws)
	r55SeedGate(t, db)
	r55SeedMember(t, db, "701", false) // 待验证：进来了也该被挡
	r55SeedMember(t, db, "702", true)  // 已放行：该进漏斗
	// shouldTriggerAI 第一句是 `if s.salesEngine == nil { return false }` ⇒ 不注入引擎，
	// AI 开关守卫会先把三条腿全挡下，"1 条挡下"的读数就成了假绿。
	ws.SetSalesEngine(context.Background(), &SalesEngine{})
	if err := db.Model(&model.TelegramAccount{}).Where("id = 1").
		Update("ai_agent_enabled", true).Error; err != nil {
		t.Fatalf("开 AI 开关: %v", err)
	}
	if !ws.shouldTriggerAI(context.Background(), ChannelTelegram, "1") {
		t.Fatal("前置不成立：账号 AI 开关没生效，本用例的 0 次读数分不清被谁挡下")
	}

	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	logger.GetLogger().Info().Msg("r55-funnel-capture-probe")
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	ctx := context.Background()
	// ① 门控群 + 未验证发言人
	ws.TriggerInboundAI(ctx, string(ChannelTelegram), "1", "-100", "701", "有人吗", "r55-f-unverified",
		WithGroup("-100", "三人行 — 泛技术交流"))
	// ② 门控群 + 已放行发言人（正对照：漏斗是活的）
	ws.TriggerInboundAI(ctx, string(ChannelTelegram), "1", "-100", "702", "群主在吗", "r55-f-verified",
		WithGroup("-100", "三人行 — 泛技术交流"))
	// ③ 同一个未验证的人改在私聊说话：互锁只管群，不该把私聊一起封掉
	ws.TriggerInboundAI(ctx, string(ChannelTelegram), "1", "701", "701", "验证码呢", "r55-f-private")
	// ④ 私聊会话 id 上"恰好也有门控+待验证台账"的形状：守卫的覆盖面按 IsGroup 划，
	//    不看会话 id 长得像什么。删掉 IsGroup 判定只会在这条腿上露馅（③ 的会话 id 是
	//    正数用户 id， MemberUnverified 查不到门控照样放行 ⇒ ③ 单独不足以钉住这个条件）。
	r55SeedPrivateShapedGate(t, db, "7031")
	ws.TriggerInboundAI(ctx, string(ChannelTelegram), "1", "7031", "7031", "在吗", "r55-f-private-gated")

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	logged := string(captured)

	blocked := linesWith(logged, "TriggerInboundAI skipped: 门控群未验证成员")
	if len(blocked) != 1 {
		t.Errorf("门控挡下行应恰好 1 条（只有①），实际 %d 条：\n%s", len(blocked), logged)
	}
	// 未注入智能体 = 走到了漏斗末端，是「放行」的证据
	reached := linesWith(logged, "smartOrchestrator 未注入")
	if len(reached) != 3 {
		t.Errorf("走到漏斗末端的应有 3 条（②已放行+③④私聊），实际 %d 条：\n%s", len(reached), logged)
	}
}

// 内容闸必须管住补触发漏斗：主路径判为「不该回」的群消息，在下一条出站收尾时会被
// RecheckUnrepliedAndTrigger 按「会话里存在未回复入站行」再灌一次 AI ⇒ 群里照旧收到
// 一条对闲聊的回复，闸等于白装。
//
// 五条腿各钉一个条件，删掉任何一个都会露馅：
//
//	① 群 + 不像问话 → 0（钉住内容判据本身）
//	② 群 + 非文本正文 → 0（钉住只认 text 那一面）
//	③ 私聊 + 同样的闲聊正文 → 1（钉住 IsGroup：漏了它私聊客户也会被挡）
//	⑤ 别的渠道的群 + 同样的闲聊正文 → 1（钉住平台判定：漏了它会把抖音/飞书的群一起切掉）
//	④ 群 + 真问话 → 1（正对照：漏斗是活的，①② 的 0 次才有意义）
func TestRecheck_TGGroupChitchatDoesNotLeakThroughFunnel(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	now := time.Now()
	conv := func(name string) string { return "r60-conv-" + name }

	mustCreate := func(h *model.MessageHub) {
		t.Helper()
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("种 hub 行 %s: %v", h.MsgID, err)
		}
	}
	seed := func(id, convID, msgType, content string, isGroup bool) {
		mustCreate(&model.MessageHub{
			MsgID: id, Platform: "telegram", AccountID: "9", Direction: "inbound",
			MsgType: msgType, SenderID: "88001", ConversationID: convID,
			Content: content, IsGroup: isGroup, SentAt: now.Add(-2 * time.Second),
		})
	}
	seed("r60-chitchat", conv("chitchat"), "text", "Quinn", true)
	seed("r60-image", conv("image"), "image", "[图片]", true)
	seed("r60-private", conv("private"), "text", "Quinn", false)
	seed("r60-question", conv("question"), "text", "群主在吗 作者在吗", true)
	// 别的渠道的群（抖音）：闸按平台划面，只切 telegram ⇒ 同样不像问话的正文在这里照旧补触发
	seedDy := &model.MessageHub{
		MsgID: "r60-dy", Platform: "douyin", AccountID: "9", Direction: "inbound",
		MsgType: "text", SenderID: "88002", ConversationID: conv("dy-group"),
		Content: "Quinn", IsGroup: true, SentAt: now.Add(-2 * time.Second),
	}
	mustCreate(seedDy)

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	ctx := context.Background()
	svc.RecheckUnrepliedAndTrigger(ctx, conv("chitchat"), "")
	if tr.called != 0 {
		t.Errorf("群里被判为闲聊的正文不得从补触发漏斗回锅，实际调用 %d 次（内容=%q）", tr.called, tr.lastContent)
	}
	svc.RecheckUnrepliedAndTrigger(ctx, conv("image"), "")
	if tr.called != 0 {
		t.Errorf("群里的图片行不得补触发 AI，实际调用 %d 次（内容=%q）", tr.called, tr.lastContent)
	}
	svc.RecheckUnrepliedAndTrigger(ctx, conv("private"), "")
	if tr.called != 1 {
		t.Fatalf("私聊客户的未回复补触发不该被群闸挡掉：期望 1 次，实际 %d 次 ⇒ ①② 的 0 次分不清被谁挡下", tr.called)
	}
	if tr.lastConv != conv("private") {
		t.Errorf("第 ③ 条腿应补触发私聊会话，实际 conv=%q", tr.lastConv)
	}
	// ⑤ 抖音群里的同样闲聊：平台判定被摘掉时这条腿会少一次增量 ⇒ 用例转红
	svc.RecheckUnrepliedAndTrigger(ctx, conv("dy-group"), "")
	if tr.called != 2 {
		t.Fatalf("非 TG 的群消息不该被这道只切 telegram 的闸挡掉：期望累计 2 次，实际 %d 次（conv=%q）", tr.called, tr.lastConv)
	}
	if tr.lastConv != conv("dy-group") {
		t.Errorf("第 ⑤ 条腿应补触发抖音群会话，实际 conv=%q", tr.lastConv)
	}
	svc.RecheckUnrepliedAndTrigger(ctx, conv("question"), "")
	if tr.called != 3 {
		t.Fatalf("正对照失效：窗口内的真实群问话应补触发，累计期望 3 次，实际 %d 次", tr.called)
	}
	if tr.lastContent != "群主在吗 作者在吗" {
		t.Errorf("第 ④ 条腿补触发内容应为群问话，实际 %q", tr.lastContent)
	}
}
