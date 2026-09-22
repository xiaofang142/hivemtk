package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 抖音/TikTok 入站回调（dop webhooks）。
//
// 官方契约取证（A 档原文与字节数登记在审计文档 §16.1）：
//   - developer.open-douyin.com/docs/resource/zh-CN/dop/develop/webhooks/summarize
//     签名 = hex(sha1(client_secret ‖ 原始 body))，放在 X-Douyin-Signature 头；
//     重投口径「连接超过 5s 会自动断开，共重试 3 次」「可通过请求头中的 Msg-Id 去重」。
//   - .../mini-app/develop/server/reach-marketing/instant-message/private-message/private-msg-webhook
//     信封 {event, client_key, from_user_id, to_user_id, log_id, content}；
//     content {conversation_short_id, server_message_id, conversation_type, message_type,
//              text|resource_url|item_id|card_*, create_time(13 位毫秒), source, index, user_infos[]}
//   - .../dop/develop/webhooks/event-list
//     群/单聊**只由事件名区分**（im_receive_msg / im_group_receive_msg / im_send_msg /
//     im_group_send_msg），信封与 content 里都没有 group 字段。
//
// 为什么这里的解析这么"瘦"：只声明上面确认过、且这里要消费的字段。encoding/json 遇到
// 类型不符（官方表格标 int 而示例给字符串的 index、conversation_type）会让**整包** Unmarshal
// 失败，而一次失败在这条通道上等于"消息蒸发"（见 dispatchDouyin 的兜底路径）。不消费的字段
// 不声明，就永远不会因为它的形态意外而毁掉整条消息。

// douyinEnvelope 官方事件信封。content 用 RawMessage：小程序页标它是 struct，
// 而同类 dop 页的 content 出现过 JSON 字符串形态，两种都得接（douyinContentObject）。
type douyinEnvelope struct {
	Event      string          `json:"event"`
	ClientKey  string          `json:"client_key"`
	FromUserID string          `json:"from_user_id"`
	ToUserID   string          `json:"to_user_id"`
	LogID      string          `json:"log_id"`
	Content    json.RawMessage `json:"content"`
}

type douyinUserInfo struct {
	OpenID   string `json:"open_id"`
	NickName string `json:"nick_name"`
	Avatar   string `json:"avatar"`
}

// douyinIMContent content 里被消费的部分。create_time 保留 RawMessage 以容忍
// 数字/字符串两种形态（见上面的"瘦解析"说明）。
type douyinIMContent struct {
	ConversationShortID string           `json:"conversation_short_id"`
	ServerMessageID     string           `json:"server_message_id"`
	MessageType         string           `json:"message_type"`
	Text                string           `json:"text"`
	ResourceURL         string           `json:"resource_url"`
	ItemID              string           `json:"item_id"`
	CardID              string           `json:"card_id"`
	Source              string           `json:"source"`
	CreateTime          json.RawMessage  `json:"create_time"`
	UserInfos           []douyinUserInfo `json:"user_infos"`
}

// 官方事件分类。已知集合之外的名字（文档还会新增）一律按非会话事件处理：
// 宁可漏一行，也不能凭 [平台 message] 占位符造出一条永远无法回复的假会话（D-04 同一结论）。
const (
	dyEventVerifyWebhook = "verify_webhook"
	dyEventReceiveMsg    = "im_receive_msg"
	dyEventGroupRecvMsg  = "im_group_receive_msg"
	dyEventSendMsg       = "im_send_msg"
	dyEventGroupSendMsg  = "im_group_send_msg"
)

// douyinMessageEvent 报告事件名是不是会话消息，以及它是收还是发。
//
// im_send_msg 是「用户发送私信触发」——即我方发出的回声。它必须落**出站**行：
// 若按入站处理，客服工作台会看到自己说的话等着回复，AI 还会去回复自己。
func douyinMessageEvent(event string) (inbound bool, ok bool) {
	switch event {
	case dyEventReceiveMsg, dyEventGroupRecvMsg:
		return true, true
	case dyEventSendMsg, dyEventGroupSendMsg:
		return false, true
	}
	return false, false
}

func douyinIsGroupEvent(event string) bool {
	return strings.HasPrefix(event, "im_group_")
}

// douyinContentObject 把 content 归一成可继续解的 JSON 对象字节。
// 官方两种外壳都给过：对象，以及"对象序列化成的字符串"。
func douyinContentObject(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false
	}
	if strings.HasPrefix(trimmed, "{") {
		return raw, true
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
			return nil, false
		}
		inner := strings.TrimSpace(s)
		if !strings.HasPrefix(inner, "{") {
			return nil, false
		}
		return json.RawMessage(inner), true
	}
	return nil, false
}

// douyinEventTime 解官方 create_time（13 位毫秒）。取不到时退回当前时间——
// 落 0001-01-01 会把收件箱排序整个打乱（新消息沉底）。
func douyinEventTime(raw json.RawMessage, now time.Time) time.Time {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return now
	}
	s = strings.Trim(s, `"`)
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil || ms <= 0 {
		return now
	}
	return time.UnixMilli(ms)
}

// douyinNickName 昵称只出现在 content.user_infos[]（信封上没有），按 open_id 匹配。
func (c *douyinIMContent) nickName(openID string) string {
	if c == nil || openID == "" {
		return ""
	}
	for _, u := range c.UserInfos {
		if u.OpenID == openID {
			return strings.TrimSpace(u.NickName)
		}
	}
	return ""
}

// douyinPlaceholder 官方报文里非文本类型大多**没有正文字段**（image/user_local_* 只给 ID），
// 占位符与 WhatsApp 同一套中文词，未知类型保留官方原值：新增类型要看得出是哪种。
func douyinPlaceholder(messageType string) string {
	switch messageType {
	case "image", "user_local_image":
		return "[图片]"
	case "emoji":
		return "[表情]"
	case "video", "user_local_video":
		return "[视频]"
	case "retain_consult_card":
		return "[留资卡片]"
	case "":
		return "[消息]"
	}
	return "[" + messageType + "]"
}

func douyinBody(messageType, text string) string {
	switch messageType {
	case "text", "other":
		if t := strings.TrimSpace(text); t != "" {
			return t
		}
	}
	return douyinPlaceholder(messageType)
}

// douyinMsgKey 幂等键的消息段。
//
// 官方 server_message_id 是 88 字符上下的 base64（含 + / =），直接拼会顶到
// message_hub.msg_id 的 varchar(100)，而这里的 Create 错误是被吞掉的（只有 UNIQUE 之外的
// 才打日志）⇒ 超列宽不是报错，是这条消息静静消失。取 sha1 前 16 位：定长、稳定、
// 不与官方 ID 的字符集较劲；原文进 Extra.server_message_id 供按抖音侧 ID 回查。
func douyinMsgKey(serverMessageID string) string {
	if serverMessageID == "" {
		return ""
	}
	sum := sha1.Sum([]byte(serverMessageID))
	return hex.EncodeToString(sum[:])[:16]
}

// HandleDouyinURLVerification 官方「保存回调地址」握手：解析出 challenge 并**原样**回显。
//
// 官方原文（.../dop/develop/webhooks/summarize）：
//
//	{"event":"verify_webhook","client_key":"","content":{"challenge":12345}}
//	「当你收到开放平台 POST 验证请求时，你需要解析出 challenge 值，并立即返回该 challenge 值作为响应」
//	响应体 {"challenge":12345}
//
// 三个不显眼但会整条卡死的点：
//  1. 类型必须原样保留。官方示例是**数字** 12345，而别处/将来可能是字符串或 19 位大数。
//     解成 Go 的 string 会把数字加引号，解成 interface{} 再编回去会把 168130328599700001
//     变成 1.681303285997e+17 —— 两种都让注册失败，而失败发生在抖音控制台里，
//     我们这边只看到一个 200。所以这里全程用 json.RawMessage 搬运原文。
//  2. 验签必须在回显之前。challenge 的意义是「这个地址确实收到了这条**带签名**的请求」；
//     先回显再验签等于把它做成一个无门槛回声弹。
//  3. client_key 官方示例里就是空串，不得当成校验失败。
//
// 返回的 handled=false 表示「这不是 verify_webhook」，调用方继续走通用漏斗。
func (s *WebhookService) HandleDouyinURLVerification(ctx context.Context, accountID string, raw []byte, headers map[string]string) (challenge json.RawMessage, handled bool, err error) {
	var env douyinEnvelope
	if uerr := json.Unmarshal(raw, &env); uerr != nil || env.Event != dyEventVerifyWebhook {
		return nil, false, nil
	}
	secret, serr := s.getAccountSecret(ctx, string(ChannelDouyin), accountID)
	if secret == "" {
		if !insecureWebhookAllowed() {
			return nil, true, fmt.Errorf("douyin webhook secret 未配置(account=%s)，无法验签；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true", accountID)
		}
		logInsecureWebhookBypass(string(ChannelDouyin), accountID, serr)
	} else {
		ok, verr := verifyDouyinWebhook(secret, raw, headers)
		if verr != nil {
			return nil, true, verr
		}
		if !ok {
			return nil, true, errors.New("X-Douyin-Signature 与 hex(sha1(client_secret+body)) 不匹配")
		}
	}
	contentRaw, ok := douyinContentObject(env.Content)
	if !ok {
		return nil, true, errors.New("verify_webhook 缺少 content")
	}
	var payload struct {
		Challenge json.RawMessage `json:"challenge"`
	}
	if uerr := json.Unmarshal(contentRaw, &payload); uerr != nil || len(payload.Challenge) == 0 {
		return nil, true, errors.New("verify_webhook 的 content 里没有 challenge")
	}
	// 手工拼而不是 map[string]any 再 Marshal：后者会把原文再解一遍、类型就跑掉了。
	return json.RawMessage(`{"challenge":` + string(payload.Challenge) + `}`), true, nil
}

// dispatchDouyin 处理抖音/TikTok 入站回调。
//
// 两家的报文结构同源（字节跳动 IM 事件），解析可以共用，但**平台标记必须按
// 渠道分开**：原先写死 "douyin"，TikTok 的客户消息在 message_hub、线索表里全被
// 记成抖音，且两平台的同一 message_id 会共用幂等键、互相吞消息（审计 D-01）。
func (s *WebhookService) dispatchDouyin(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if s.lazyDB() == nil {
		return nil, nil, nil
	}
	s.ensureReposFromDB(ctx)

	platform, keyPrefix := douyinPlatformKeyPrefix(channel)

	var env douyinEnvelope
	contentRaw, haveContent := func() (json.RawMessage, bool) {
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, false
		}
		return douyinContentObject(env.Content)
	}()
	if env.Event == "" || !haveContent {
		// 不是官方信封（旧事件行的重放、渠道侧怪形态）：交给通用兜底，别在这一步丢。
		return s.dispatchDouyinGeneric(ctx, channel, accountID, p, raw)
	}

	inbound, isMessage := douyinMessageEvent(env.Event)
	if !isMessage {
		// verify_webhook 由控制器同步回显；进入会话、加群审核、授权等事件同样不是消息，
		// 它们带 from_user_id 却没有会话正文（审计 D-04）。
		return nil, nil, nil
	}

	var content douyinIMContent
	if err := json.Unmarshal(contentRaw, &content); err != nil {
		logger.Debugf("[DouyinWebhook] content 解析失败 event=%s: %v", env.Event, err)
	}

	sender := env.FromUserID
	if sender == "" {
		sender = p.Sender
	}
	if sender == "" {
		// 连发送人都没有的消息无法回复，也不该在收件箱里长出假会话（D-04 保留项）。
		return nil, nil, nil
	}

	if p.Content == "" {
		p.Content = douyinBody(content.MessageType, content.Text)
	}
	msgType := InboundHubMsgType(content.MessageType)

	isGroup := douyinIsGroupEvent(env.Event)
	groupID := ""
	groupTitle := ""
	if isGroup {
		// 官方群事件没有单独的 group_id（群/单聊的区分全在事件名上），
		// 会话 ID 就是这条群会话的短 ID。
		groupID = content.ConversationShortID
	}

	convID := content.ConversationShortID
	if convID == "" {
		if isGroup {
			convID = keyPrefix + "_group_" + sender
		} else {
			convID = keyPrefix + "_dm_" + sender
		}
	}

	direction := "inbound"
	if !inbound {
		direction = "outbound"
	}

	msgID := keyPrefix + "_" + accountID + "_"
	if key := douyinMsgKey(content.ServerMessageID); key != "" {
		msgID += key
	} else {
		// 官方 ID 缺失时退回内容哈希：它至少稳定（重投同体同键），
		// 但绝不能不带账号（N-16：跨账号共用键会让第二家撞唯一键后被静默吞掉）。
		msgID += ContentHashMsgID(platform, sender, p.Content)
	}

	senderName := content.nickName(sender)
	if senderName == "" {
		senderName = sender
	}

	hub := &model.MessageHub{
		Platform:       platform,
		AccountID:      accountID,
		MsgID:          msgID,
		Direction:      direction,
		SenderID:       sender,
		SenderName:     senderName,
		ReceiverID:     env.ToUserID,
		ConversationID: convID,
		MsgType:        msgType,
		Content:        p.Content,
		SentAt:         douyinEventTime(content.CreateTime, time.Now()),
		IsGroup:        isGroup,
		GroupID:        groupID,
		Extra: model.JSONMap{
			"event":                 env.Event,
			"log_id":                env.LogID,
			"client_key":            env.ClientKey,
			"conversation_short_id": content.ConversationShortID,
			"server_message_id":     content.ServerMessageID,
			"dy_message_type":       content.MessageType,
		},
	}
	// 官方留的另外几手：source 非空=这条是接口发出（空=端上主动发），
	// resource_url/item_id/card_id 分别是表情直链、视频分享 ID、留资卡片 ID。
	if content.Source != "" {
		hub.Extra["source"] = content.Source
	}
	for k, v := range map[string]string{"resource_url": content.ResourceURL, "item_id": content.ItemID, "card_id": content.CardID} {
		if v != "" {
			hub.Extra[k] = v
		}
	}

	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			logger.Warnf("[DouyinWebhook] 写入 MessageHub 失败: %v", err)
		}
	}

	s.upsertInboxFromHub(ctx, hub, senderName)

	// 官方对这两类只给 ID、不给正文也不给直链：占位符之上必须当场换一次资源链接并转存，
	// 否则工作台看到的就是一个永远点不开的图片。
	if inbound && douyinNeedsMedia(content.MessageType) {
		s.persistDouyinMediaAsync(ctx, channel, accountID, hub.MsgID, douyinMediaRef{
			OpenID:         sender,
			ConversationID: content.ConversationShortID,
			MessageID:      content.ServerMessageID,
		})
	}

	newOpportunity := false
	if inbound && !strings.HasPrefix(sender, "bot_") {
		newOpportunity = s.mineDouyinGroupLead(ctx, hub, accountID, groupID, groupTitle, sender, senderName, p.Content)
		if newOpportunity {
			logger.Infof("[DouyinWebhook] 线索触发 account=%s user=%s content=%s", accountID, sender, p.Content)
		}
	}

	return hub, &tgDispatchExtra{NewOpportunity: newOpportunity}, nil
}

// douyinPlatformKeyPrefix 返回 (落库平台名, 幂等键前缀)。
func douyinPlatformKeyPrefix(channel WebhookChannel) (string, string) {
	if channel == ChannelTiktok {
		return string(ChannelTiktok), "tt"
	}
	return string(ChannelDouyin), "dy"
}

func (s *WebhookService) dispatchDouyinGeneric(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, *tgDispatchExtra, error) {
	if p.Sender == "" {
		return nil, nil, nil
	}
	platform, keyPrefix := douyinPlatformKeyPrefix(channel)
	isGroup := p.ChatID != "" && strings.HasPrefix(p.ChatID, "group_")
	groupID := p.ChatID

	content := p.Content
	if content == "" {
		content = "[" + platform + " generic event]"
	}

	hub := &model.MessageHub{
		Platform:       platform,
		AccountID:      accountID,
		MsgID:          keyPrefix + "_" + accountID + "_generic_" + ContentHashMsgID(platform, p.Sender, content),
		Direction:      "inbound",
		SenderID:       p.Sender,
		ConversationID: p.ChatID,
		MsgType:        "text",
		Content:        content,
		SentAt:         time.Now(),
		IsGroup:        isGroup,
		GroupID:        groupID,
	}

	if hub.Content == "" {
		hub.Content = "[" + platform + " generic event]"
	}

	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			logger.Warnf("[DouyinWebhook] 通用解析写入失败: %v", err)
		}
	}
	s.upsertInboxFromHub(ctx, hub, p.Sender)

	newOpportunity := false
	if p.Sender != "" {
		newOpportunity = s.mineDouyinGroupLead(ctx, hub, accountID, groupID, "", p.Sender, p.Sender, p.Content)
	}

	return hub, &tgDispatchExtra{NewOpportunity: newOpportunity}, nil
}
