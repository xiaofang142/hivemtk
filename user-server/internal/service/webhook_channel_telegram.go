package service

import (
	"context"

	"fmt"

	"regexp"

	"strconv"

	"strings"

	"time"

	"hivemtk-user/internal/channelbot/telegram"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/cache"
)

type tgDispatchExtra struct {
	Mentioned      bool
	NewOpportunity bool
	GateHandled    bool // /start 网关验证已消费，不再触发销售智能体
	// GateMuted 区分 GateHandled 的两种来源：群门控把发言人挡下（可观测/可归因），
	// 而私聊 /start 只是流程已消费。二者都置 GateHandled，日志口径不能混。
	GateMuted bool

	// SpeakerVerified 发言人是启用中的禁言解锁门控群里已验证的成员 ⇒ 他的问话即使
	// 没 @ 机器人也应交给智能体（社群咨询话术打不进带货词库的意向分阈值）。
	SpeakerVerified bool

	// 群回复 @mention 原发言人 + reply-to 消息所需
	FromUsername string // Telegram @username（可能为空）
	FromName     string // 真实姓名（fallback）
	FromUserID   int64  // 原发言人 user_id（用于 tg://user?id= 链接）
	ReplyToMsgID int64  // 原消息 msgID（回复引用，Telegram int64）

	// 触发原因（传递到 AI 上下文 + 回复行为决策）
	TriggerReason string // "mention" | "opportunity" | "private" | "start" | "verified_member_speech" | ""
}

const (
	ChannelDouyin WebhookChannel = "douyin"

	ChannelKuaishou WebhookChannel = "kuaishou"

	ChannelXiaohongshu WebhookChannel = "xiaohongshu"

	ChannelXianyu WebhookChannel = "xianyu"

	ChannelTiktok WebhookChannel = "tiktok"

	ChannelWechat WebhookChannel = "wechat"

	ChannelWeCom WebhookChannel = "wecom"

	ChannelDingTalk WebhookChannel = "dingtalk"

	ChannelWhatsapp WebhookChannel = "whatsapp"

	ChannelTelegram WebhookChannel = "telegram"

	ChannelFeishu WebhookChannel = "feishu"

	ChannelCustom WebhookChannel = "custom"
)

func (s *WebhookService) dispatchTelegram(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if s.lazyDB() == nil {
		return nil, nil, nil
	}
	s.ensureReposFromDB(ctx)

	tgPayload, err := telegram.ParseUpdate(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("telegram parse: %w", err)
	}

	// M-01：媒体消息的正文在 caption 里，且落库类型必须是 image/video/audio/file，
	// 不能一律 text + 空正文（p.Content 与 hub.Content 必须同源，否则 AI 看到的与工作台上显示的不是同一条）。
	if m := tgPayload.AnyMessage(); m != nil {
		inb := m.Inbound()
		if inb.Content != "" {
			p.Content = inb.Content
		}
		if m.From != nil {
			p.Sender = strconv.FormatInt(m.From.ID, 10)
		}
		if m.Chat != nil {
			p.ChatID = strconv.FormatInt(m.Chat.ID, 10)
		}
	}

	botUsername := s.getTelegramBotUsername(ctx, accountID)

	// 方案 A：入群申请（群开启 "申请加入" 后 Telegram 推送 chat_join_request）
	if tgPayload.ChatJoinRequest != nil && tgPayload.ChatJoinRequest.From != nil {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID > 0 {
			s.tgGate.HandleJoinRequest(ctx, uint(accID), tgPayload.ChatJoinRequest)
		}
		return nil, nil, nil
	}

	// 成员状态变更（chat_member / my_chat_member）暂无业务，静默消费
	if tgPayload.ChatMember != nil || tgPayload.MyChatMember != nil {
		return nil, nil, nil
	}

	if tgPayload.Message != nil && len(tgPayload.Message.NewChatMembers) > 0 && tgPayload.Message.Chat != nil {
		chatID := tgPayload.Message.Chat.ID
		chatType := tgPayload.Message.Chat.Type
		chatTitle := tgPayload.Message.Chat.Title
		if chatType == "" {
			chatType = "group"
		}
		chatIDStr := fmt.Sprintf("%d", chatID)
		isGroup := chatType == "group" || chatType == "supergroup"

		var newMember *telegram.TGUser
		for i := range tgPayload.Message.NewChatMembers {
			if !tgPayload.Message.NewChatMembers[i].IsBot {

				member := tgPayload.Message.NewChatMembers[i]
				newMember = &telegram.TGUser{
					ID:        member.ID,
					FirstName: member.FirstName,
					Username:  member.Username,
					IsBot:     member.IsBot,
				}
				break
			}
		}
		if newMember != nil {
			// 方案 B：管控群先禁言（HandleNewMembers 内部判断网关配置，未配置直接跳过）
			accID, _ := strconv.ParseUint(accountID, 10, 64)
			gateActive := false
			if accID > 0 {
				gateActive = s.tgGate.HandleNewMembers(ctx, uint(accID), chatID, []telegram.TGUser{*newMember})
			}

			senderIDStr := fmt.Sprintf("%d", newMember.ID)
			fromName := newMember.FirstName
			if newMember.Username != "" {
				fromName = newMember.Username
			}

			groupLabel := chatTitle
			if groupLabel == "" {
				groupLabel = chatIDStr
			}
			eventContent := fmt.Sprintf("[入群事件] 用户 %s (@%s) 加入群组 %s", newMember.FirstName, newMember.Username, groupLabel)
			hub := &model.MessageHub{
				Platform:       "telegram",
				AccountID:      accountID,
				MsgID:          fmt.Sprintf("tg_join_%d_%d", chatID, newMember.ID),
				Direction:      "inbound",
				SenderID:       senderIDStr,
				ConversationID: chatIDStr,
				MsgType:        "event",
				Content:        eventContent,
				SentAt:         time.Now(),
				IsGroup:        isGroup,
				GroupID:        chatIDStr,
			}
			if err := s.messageHubRepo.Create(ctx, hub); err != nil {
				if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
					return nil, nil, err
				}
			}
			s.upsertInboxFromHub(ctx, hub, fromName)

			// 门控群（方案 B 禁言中）：验证提示已由 HandleNewMembers 发出，
			// 不再叠加 AI 欢迎语，避免污染验证引导 / 诱导禁言用户误以为可发言
			if gateActive {
				return hub, nil, nil
			}

			triggerMsg := fmt.Sprintf("新用户 %s (@%s) 刚加入群组「%s」。请以销售助手身份主动发起欢迎+销售开场白，引导用户了解我们的产品。",
				newMember.FirstName, newMember.Username, groupLabel)
			s.triggerTelegramJoinSales(ctx, accountID, chatIDStr, senderIDStr, triggerMsg)
			return hub, nil, nil
		}
	}

	if tgPayload.Message != nil && tgPayload.Message.LeftChatMember != nil && tgPayload.Message.Chat != nil {
		chatID := tgPayload.Message.Chat.ID
		chatIDStr := fmt.Sprintf("%d", chatID)
		left := tgPayload.Message.LeftChatMember
		senderIDStr := fmt.Sprintf("%d", left.ID)
		fromName := left.FirstName
		if left.Username != "" {
			fromName = left.Username
		}
		eventContent := fmt.Sprintf("[退群事件] 用户 %s (@%s) 离开群组", left.FirstName, left.Username)
		hub := &model.MessageHub{
			Platform:       "telegram",
			AccountID:      accountID,
			MsgID:          fmt.Sprintf("tg_left_%d_%d", chatID, left.ID),
			Direction:      "inbound",
			SenderID:       senderIDStr,
			ConversationID: chatIDStr,
			MsgType:        "event",
			Content:        eventContent,
			SentAt:         time.Now(),
			IsGroup:        true,
			GroupID:        chatIDStr,
		}
		if err := s.messageHubRepo.Create(ctx, hub); err != nil {
			if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
				return nil, nil, err
			}
		}
		s.upsertInboxFromHub(ctx, hub, fromName)
		return hub, nil, nil
	}

	type tgMsg struct {
		msgID     int64
		chatID    int64
		chatType  string
		fromID    int64
		fromName  string
		username  string
		fromIsBot bool
		text      string
		msgType   string
		media     []telegram.TGMediaRef
	}
	var picked *tgMsg
	if tgPayload.Message != nil && tgPayload.Message.From != nil && tgPayload.Message.Chat != nil {

		if tgPayload.Message.Text == "" && len(tgPayload.Message.NewChatMembers) == 0 && tgPayload.Message.LeftChatMember == nil && tgPayload.Message.NewChatTitle != "" {
			return nil, nil, nil
		}
		tm := &tgMsg{
			msgID:     tgPayload.Message.MessageID,
			chatID:    tgPayload.Message.Chat.ID,
			chatType:  tgPayload.Message.Chat.Type,
			fromID:    tgPayload.Message.From.ID,
			fromName:  tgPayload.Message.From.FirstName,
			username:  tgPayload.Message.From.Username,
			fromIsBot: tgPayload.Message.From.IsBot,
		}
		inb := tgPayload.Message.Inbound()
		tm.text, tm.msgType, tm.media = inb.Content, inb.MsgType, inb.Media
		if tm.chatType == "" {
			tm.chatType = "private"
		}
		picked = tm
	} else if tgPayload.EditedMessage != nil && tgPayload.EditedMessage.Chat != nil {
		tm := &tgMsg{
			msgID:    tgPayload.EditedMessage.MessageID,
			chatID:   tgPayload.EditedMessage.Chat.ID,
			chatType: tgPayload.EditedMessage.Chat.Type,
			fromID: func() int64 {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.ID
				}
				return 0
			}(),
			fromName: func() string {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.FirstName
				}
				return ""
			}(),
			username: func() string {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.Username
				}
				return ""
			}(),
			fromIsBot: func() bool {
				if tgPayload.EditedMessage.From != nil {
					return tgPayload.EditedMessage.From.IsBot
				}
				return false
			}(),
		}
		// edited_message 与 message 同构：媒体改动也走同一归一，否则编辑过的图片消息又回到空正文
		inb := tgPayload.EditedMessage.Inbound()
		tm.text, tm.msgType, tm.media = inb.Content, inb.MsgType, inb.Media
		if tm.chatType == "" {
			tm.chatType = "private"
		}
		picked = tm
	} else if tgPayload.CallbackQuery != nil && tgPayload.CallbackQuery.From != nil {
		chatID := int64(0)
		chatType := "private"
		if tgPayload.CallbackQuery.Message != nil {
			chatID = tgPayload.CallbackQuery.Message.Chat.ID
			chatType = tgPayload.CallbackQuery.Message.Chat.Type
		}
		picked = &tgMsg{
			msgID:     0,
			chatID:    chatID,
			chatType:  chatType,
			fromID:    tgPayload.CallbackQuery.From.ID,
			fromName:  tgPayload.CallbackQuery.From.FirstName,
			fromIsBot: tgPayload.CallbackQuery.From.IsBot,
			text:      "/callback " + tgPayload.CallbackQuery.Data,
			msgType:   model.MsgTypeText,
		}
	}
	if picked == nil {
		return nil, nil, nil
	}
	chatIDStr := fmt.Sprintf("%d", picked.chatID)
	senderIDStr := fmt.Sprintf("%d", picked.fromID)
	// 与 Ingress 落库的 EventID 同一个键：媒体回填按它找行，两处各算一套就是一个键写、另一个键读。
	hubMsgID := tgPayload.HubMsgID(accountID)
	if hubMsgID == "" {
		hubMsgID = fmt.Sprintf("tg_%s_%d", accountID, picked.msgID)
	}
	hub := &model.MessageHub{
		Platform:       "telegram",
		AccountID:      accountID,
		MsgID:          hubMsgID,
		Direction:      "inbound",
		SenderID:       senderIDStr,
		ConversationID: chatIDStr,
		MsgType:        picked.msgType,
		Content:        picked.text,
		SentAt:         time.Now(),
		IsGroup:        picked.chatType == "group" || picked.chatType == "supergroup",
		GroupID:        chatIDStr,
	}
	if hub.MsgType == "" {
		hub.MsgType = model.MsgTypeText
	}
	if hub.Content == "" {
		hub.Content = "[消息]"
	}

	// 入站经中台落库（message_hub / 去重钩子），但 AI 触发留在本 dispatch：
	// 下面 handleJob 才会按群门控（@mention/商机）、/start 网关、账号 AI 开关决定是否 triggerSalesEngine。
	if err := tgPayload.Ingress(WithChannelOwnedAITrigger(ctx), s.ingressHandler(ctx), accountID); err != nil {
		return nil, nil, err
	}
	// 富媒体当场转存：官方 getFile 返回的 file_path 是「至少 1 小时有效」的临时链接，
	// 且下载要带 bot token（不能像 QQ 那样把链接丢给工作台），故必须在入站侧完成。
	// 放在 Ingress 之后：回填按 hubMsgID 找行，先落库再异步补，省掉一次「行还没建好」的竞态。
	s.persistTelegramMediaAsync(ctx, accountID, hubMsgID, picked.media)
	s.upsertInboxFromHub(ctx, hub, picked.fromName)

	newOpportunity := false
	if picked.fromID != 0 && !picked.fromIsBot {
		groupTitle := ""
		if tgPayload.Message != nil && tgPayload.Message.Chat != nil {
			groupTitle = tgPayload.Message.Chat.Title
		}
		// 门控互锁前置：未验证成员在门控群的发言不挖掘线索/商机（先于 mining，避免拦截前已入库）
		interlocked := false
		if picked.chatType == "group" || picked.chatType == "supergroup" {
			accID, _ := strconv.ParseUint(accountID, 10, 64)
			if accID > 0 && s.tgGate.MemberUnverified(ctx, uint(accID), chatIDStr, senderIDStr) {
				interlocked = true
			}
		}
		if !interlocked {
			newOpportunity = s.mineTelegramGroupLead(ctx, hub, accountID, chatIDStr, groupTitle, senderIDStr, picked.username, picked.fromName, picked.text)
		}
	}

	mentioned := isTelegramBotMentioned(picked.text, botUsername)
	if !mentioned && tgPayload.Message != nil && tgPayload.Message.ReplyToMessage != nil &&
		tgPayload.Message.ReplyToMessage.From != nil && tgPayload.Message.ReplyToMessage.From.IsBot {
		mentioned = true
	}

	// 网关验证：私聊 /start（带或不带 token）走激活流程，命中后不再触发销售智能体
	gateHandled := false
	if !picked.fromIsBot && picked.chatType == "private" && strings.HasPrefix(strings.TrimSpace(picked.text), "/start") {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID > 0 {
			gateHandled = s.tgGate.HandleStartCommand(ctx, uint(accID), tgUserFromMessage(tgPayload), picked.text, picked.chatID)
		}
	}

	// 门控群 AI 互锁（后置兜底）：未验证成员即使 @机器人 也不触发销售 AI
	if picked.chatType == "group" || picked.chatType == "supergroup" {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID > 0 && s.tgGate.MemberUnverified(ctx, uint(accID), chatIDStr, senderIDStr) {
			// 台账里根本没有这个人 = 门控装群之前就在线上的老成员：他的验证入口永远没发过，
			// 互锁因此是终身的、且原先一声不响。按新人流程就地补发一次（写台账+禁言+群内提示），
			// 补过之后他就有 pending 行，后续发言不再重复补发。
			if s.tgGate.MemberLacksLedger(ctx, uint(accID), chatIDStr, senderIDStr) {
				invited := s.tgGate.HandleNewMembers(ctx, uint(accID), picked.chatID, []telegram.TGUser{{
					ID:        picked.fromID,
					FirstName: picked.fromName,
					Username:  picked.username,
					IsBot:     picked.fromIsBot,
				}})
				logger.Infof("[TG-Gate] 门控群无台账老成员：就地补发验证邀请 account=%s chat=%s sender=%s invited=%v",
					accountID, chatIDStr, senderIDStr, invited)
			}
			return hub, &tgDispatchExtra{
				Mentioned: false, NewOpportunity: false, GateHandled: true, GateMuted: true,
				FromUsername: picked.username, FromName: picked.fromName, FromUserID: picked.fromID,
				ReplyToMsgID:  picked.msgID,
				TriggerReason: "",
			}, nil
		}
	}

	// 已验证发言人：门控群里过了审的人，一句"群主在吗"也是来咨询的。
	// 必须放在两处互锁判定之后（走到这里说明他没被挡下），且用正面判据而非取反
	// MemberUnverified —— 后者对"没装门控的群/已退群的人/服务未装配"同样返回 false。
	speakerVerified := false
	if picked.chatType == "group" || picked.chatType == "supergroup" {
		if accID, cerr := strconv.ParseUint(accountID, 10, 64); cerr == nil && accID > 0 {
			speakerVerified = s.tgGate.MemberVerified(ctx, uint(accID), chatIDStr, senderIDStr)
		}
	}

	// 推断触发原因（handleJob 会最终决定用哪个触发 AI）
	reason := ""
	switch {
	case gateHandled:
		reason = "start"
	case mentioned:
		reason = "mention"
	case newOpportunity:
		reason = "opportunity"
	case speakerVerified:
		reason = tgTriggerReasonVerifiedSpeaker
	case picked.chatType == "private":
		reason = "private"
	}

	return hub, &tgDispatchExtra{
		Mentioned:       mentioned,
		NewOpportunity:  newOpportunity,
		GateHandled:     gateHandled,
		SpeakerVerified: speakerVerified,
		FromUsername:    picked.username,
		FromName:        picked.fromName,
		FromUserID:      picked.fromID,
		ReplyToMsgID:    picked.msgID,
		TriggerReason:   reason,
	}, nil
}

// tgUserFromMessage 提取 /start 命令发送者
func tgUserFromMessage(tgPayload *telegram.Update) *telegram.TGUser {
	if tgPayload.Message != nil && tgPayload.Message.From != nil {
		return tgPayload.Message.From
	}
	if tgPayload.EditedMessage != nil && tgPayload.EditedMessage.From != nil {
		return tgPayload.EditedMessage.From
	}
	if tgPayload.CallbackQuery != nil && tgPayload.CallbackQuery.From != nil {
		return tgPayload.CallbackQuery.From
	}
	return nil
}

const tgLeadOutreachCooldown = 30 * time.Minute

func (s *WebhookService) getTelegramBotUsername(ctx context.Context, accountID string) string {
	if s.telegramRepo == nil {
		return ""
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return ""
	}
	acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
	if err != nil || acc == nil {
		return ""
	}
	return strings.TrimSpace(acc.BotUsername)
}

// tgCommunityAskRe 社群问话/咨询的最小信号集。带货商机词由 DetectUnifiedIntent 的词库负责
// （见 tgVerifiedSpeechWorthReply 里的分数下界），这里只补"在问话但没在采购"那一类。
// 集合里刻意不列"什么/想/要/谢谢/老师"：前三个在纯问候句里高频出现（"我是做什么的"
// "认识一下大家，请多指教"），后两个是礼貌用语，放进放行集等于把闲聊全放回来。
// "嘛"与"吗"同音、社群口语里同样收尾表疑问（"你不是在测试机器人都嘛"），漏了它＝把真问话当闲聊。
var tgCommunityAskRe = regexp.MustCompile(`(?:吗|嘛|怎样|怎么|如何|多少|几个|哪些|哪里|哪儿|为什么|为啥|能否|能不能|可不可以|是不是|有没有|是否|请问|麻烦|求|支持|？|\?|[Ww]ho|[Ww]hat|[Ww]hen|[Ww]here|[Ww]hich|[Ww]hy|[Hh]ow|[Mm]uch|[Pp]rice|[Cc]ost|[Cc]ontact|[Ss]upport)`)

// tgVerifiedSpeechWorthReply 已验证成员的不 @ 发言值不值得占一次 AI 回复。
//
// 上一轮为救"群里问话没人回"，这条路开成了"过了审的成员说话就进 AI"，代价是图片、
// 表情、成员之间的自我介绍与寒暄也各触发一次 AI 回复（现证：09-28 当天群里两张图片、
// "各位都是哪里的啊"这类成员互聊各换来一条回复）。这里把内容判据加回来，三道各挡一类，
// 每道都有能杀掉它的用例（少一道判据就少一面牙）：
//  1. 只认 text ⇒ 带说明文字的图片/文件（正文是问话也不回，AI 看不到图，回了是答非所问）；
//  2. 含字母/数字 ⇒ 空正文、纯标点、纯表情（判据本身就把空串挡下，不另设判空）；
//  3. 像问话（tgCommunityAskRe）或命中商机词库（分数高于起点 = 至少命中一词）⇒ 挡掉
//     "大家好""收到""Quinn"这类成员之间的应答。
//
// @机器人 与"新商机"走各自分支、不经本函数 ⇒ 真问话不会因为词库没命中而丢掉。
func tgVerifiedSpeechWorthReply(msgType, content string) bool {
	if msgType != "text" {
		return false
	}
	t := strings.TrimSpace(content)
	if !unifiedMeaningfulRe.MatchString(t) {
		return false
	}
	if tgCommunityAskRe.MatchString(t) {
		return true
	}
	score, _, _ := DetectUnifiedIntent(t, nil, nil)
	return score > unifiedIntentBaseScore
}

// telegramGroupTriggerDecision Telegram 群消息是否触发销售 AI，以及判定的原因文本。
// 原先 handleJob 里的 switch 只有两条 case、既无 default 也无日志 ⇒ 群里"发了没人回"
// 在日志里查不到任何痕迹。outreachAllowed 必须由调用方在 newOpp 为真时才计算
// （它会占一个 30 分钟的 Redis SetNX 槽）。
//
// speakerVerified 排在 cooldown 之前：那把冷却锁的是"主动触达"，不该顺手把
// 已验证成员在群里的问话也一起挡掉。
//
// msgType/content 只用于给"已验证成员"这条无门槛通路加内容判据；不参与别的分支。
func telegramGroupTriggerDecision(mentioned, newOpp, outreachAllowed, speakerVerified bool, msgType, content string) (bool, string) {
	switch {
	case mentioned:
		return true, tgTriggerReasonMention
	case newOpp && outreachAllowed:
		return true, tgTriggerReasonOpportunity
	case speakerVerified:
		// 单独一条 not-worth 的 reason：否则日志里会把"内容被判为不值得回"和
		// "根本没过了审"混成同一句 no_mention_no_opportunity，排查时分不开。
		if tgVerifiedSpeechWorthReply(msgType, content) {
			return true, tgTriggerReasonVerifiedSpeaker
		}
		return false, tgTriggerReasonSpeechNoAsk
	case newOpp:
		return false, tgTriggerReasonOpportunityCooldown
	default:
		return false, tgTriggerReasonNoTrigger
	}
}

const (
	tgTriggerReasonMention             = "mention"
	tgTriggerReasonOpportunity         = "opportunity"
	tgTriggerReasonOpportunityCooldown = "opportunity_outreach_cooldown"
	tgTriggerReasonVerifiedSpeaker     = "verified_member_speech"
	// tgTriggerReasonSpeechNoAsk 过了审、但正文不像在问话（图片/表情/命令/纯寒暄）。
	// 取值刻意不含 "member_speech" 子串：日志按 reason 名计数时两条判据要能分开。
	tgTriggerReasonSpeechNoAsk = "speech_not_question"
	tgTriggerReasonNoTrigger   = "no_mention_no_opportunity"
)

// tgGroupShouldTriggerAI 判定并出声：群消息到底为什么不触发 AI。
//
// outreachAllowed 是惰性的，只有判定到"新商机"才被调用 —— 它会占一个 30 分钟的
// Redis SetNX 槽（tgLeadOutreachCooldown），普通闲聊也调一次就等于替该发言人
// 随后半小时的真商机触达预先点火。已验证成员的问话同样不去抢它（抢了也不影响放行）。
//
// 日志不打消息正文：正文可含换行，一条发言就能把日志劈成两行伪造出别的事件。
func tgGroupShouldTriggerAI(mentioned, newOpp, speakerVerified bool, outreachAllowed func() bool, eventID, chatID, sender, msgType, content string) bool {
	allowed := false
	if newOpp {
		allowed = outreachAllowed()
	}
	fire, reason := telegramGroupTriggerDecision(mentioned, newOpp, allowed, speakerVerified, msgType, content)
	if fire {
		logger.Infof("[Webhook] TG 群消息触发 AI reason=%s event=%s chat=%s sender=%s", reason, eventID, chatID, sender)
		return true
	}
	logger.Infof("[Webhook] TG 群消息未触发 AI reason=%s event=%s chat=%s sender=%s", reason, eventID, chatID, sender)
	return false
}

func isTelegramBotMentioned(text, botUsername string) bool {
	uname := strings.TrimSpace(botUsername)
	if uname == "" {
		return false
	}
	needle := "@" + strings.ToLower(uname)
	lower := strings.ToLower(text)
	idx := strings.Index(lower, needle)
	if idx < 0 {
		return false
	}
	end := idx + len(needle)
	if end < len(lower) {
		next := lower[end]
		if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') || next == '_' {
			return false
		}
	}
	return true
}

func (s *WebhookService) tgLeadOutreachAllowed(ctx context.Context, accountID, chatID, senderID string) bool {
	key := "mtk:tg:outreach:" + accountID + ":" + chatID + ":" + senderID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", tgLeadOutreachCooldown)
	if err != nil {

		return true
	}
	return set
}

func (s *WebhookService) triggerTelegramJoinSales(ctx context.Context, accountID, chatID, senderID, triggerMsg string) {
	if s.salesEngine == nil {
		return
	}
	if !s.shouldTriggerAI(ctx, ChannelTelegram, accountID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
	defer cancel()

	req := &SalesRequest{
		SessionID:   "telegram:" + chatID,
		CustomerID:  senderID,
		OneID:       "telegram:" + senderID,
		UserMessage: triggerMsg,
		Platform:    "telegram",
		Config:      DefaultSalesEngineConfig(),
	}

	req.Config.Persona = "你是 Telegram 群组里的销售助手。新用户加入群组时，主动发起一段简洁、亲切的欢迎+销售开场白，引导用户了解产品。回复不超过 80 字。"

	resp, err := s.salesEngine.Handle(ctx, req)
	if err != nil {
		logger.Errorf("[Webhook] TG 入群触发 智能体失败 account=%s chat=%s: %v", accountID, chatID, err)
		return
	}
	if resp == nil || resp.Reply == "" {
		return
	}
	if resp.TransferredToHuman {
		logger.Infof("[Webhook] TG 入群触发转人工: %s", resp.TransferReason)
		return
	}

	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	if chatIDInt == 0 {
		return
	}
	if s.tgIntegration == nil {
		s.tgIntegration = NewTelegramIntegrationService(s.lazyDB())
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return
	}
	if err := s.tgIntegration.SendMessage(ctx, uint(accID), chatIDInt, resp.Reply); err != nil {
		logger.Errorf("[Webhook] TG 入群欢迎消息发送失败 account=%s chat=%s: %v", accountID, chatID, err)
	}
}

func (s *WebhookService) getTelegramWebhookSecret(ctx context.Context, accountID string) string {
	if s.telegramRepo == nil {
		return ""
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return ""
	}
	acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
	if err != nil {
		return ""
	}
	return acc.WebhookSecret
}
