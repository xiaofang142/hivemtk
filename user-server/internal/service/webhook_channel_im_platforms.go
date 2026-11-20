package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/onebot"
	"hivemtk-user/internal/channelbot/slack"
	"hivemtk-user/internal/channelbot/wecombot"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// webhook_channel_im_platforms.go 025 平台批（企业微信客服 / OneBot v11 /
// Slack / 企微群机器人 / 自定义 webhook）的通用 webhook 接缝。
//
// 五个平台的入站都走同一条通用路由 /api/webhook/:channel/:account_id：
//   Verify → ParsePayload → dispatchToChannel → ingress.HandleIngressMessage。
// 出站统一经 RegisterChannelSender 登记，sendOutbound 不再感知平台差异。

const (
	ChannelWeComCS   WebhookChannel = "wecomcs"
	ChannelOneBot    WebhookChannel = "onebot"
	ChannelSlack     WebhookChannel = "slack"
	ChannelWeCombBot WebhookChannel = "wecombot"
)

// ── 验签 ──────────────────────────────────────────────────────────────

// VerifySlack Events API v0 签名：v0=HMAC_SHA256(signingSecret, "v0:"+ts+":"+body)，
// 时间窗 5 分钟（slack.VerifySignature 内实现）。
func VerifySlack(secret string, body []byte, headers map[string]string) bool {
	if secret == "" {
		return false
	}
	ts := headerValue(headers, "X-Slack-Request-Timestamp")
	sig := headerValue(headers, "X-Slack-Signature")
	if ts == "" || sig == "" {
		return false
	}
	return slack.VerifySignature(secret, ts, body, sig, time.Now())
}

// VerifyOneBot 反向 HTTP 上报的 token 校验：Authorization: Bearer <token>，
// 也兼容 ?access_token=。token 为空表示账号未启用鉴权，交由调用方 fail-closed。
func VerifyOneBot(token string, headers map[string]string, query map[string]string) bool {
	if token == "" {
		return false
	}
	h := headerValue(headers, "Authorization")
	q := ""
	if query != nil {
		q = query["access_token"]
	}
	return onebot.VerifyToken(token, h, q)
}

// WeComCSCallbackEnvelope 客服回调信封（XML 或 JSON 均已压成 map 后取字段）。
type WeComCSCallbackEnvelope struct {
	ToUserName   string
	FromUserName string
	CreateTime   int64
	MsgType      string
	Event        string
	Token        string
	OpenKfID     string
	Encrypt      string
}

// ParseWeComCSEnvelope 解析客服回调（官方 XML；兼容 JSON 便于回放）。
func ParseWeComCSEnvelope(body []byte) WeComCSCallbackEnvelope {
	m := wecomEnvelopeMap(body)
	if m == nil {
		return WeComCSCallbackEnvelope{}
	}
	t, _ := strconv.ParseInt(getString(m, "CreateTime", "create_time"), 10, 64)
	return WeComCSCallbackEnvelope{
		ToUserName:   getString(m, "ToUserName", "to_user_name"),
		FromUserName: getString(m, "FromUserName", "from_user_name"),
		CreateTime:   t,
		MsgType:      getString(m, "MsgType", "msg_type"),
		Event:        getString(m, "Event", "event"),
		Token:        getString(m, "Token", "token"),
		OpenKfID:     getString(m, "OpenKfId", "OpenKfID", "open_kfid"),
		Encrypt:      getString(m, "Encrypt", "encrypt"),
	}
}

// VerifyWeComCS 客服回调验签：sha1(sort(token,timestamp,nonce,Encrypt))。
// Encrypt 缺失时退回 query.echostr（URL 验证场景）。
func VerifyWeComCS(token string, body []byte, query map[string]string) bool {
	if token == "" {
		return false
	}
	env := ParseWeComCSEnvelope(body)
	encrypt := env.Encrypt
	ts, nonce, sig := "", "", ""
	if query != nil {
		ts = query["timestamp"]
		nonce = query["nonce"]
		sig = query["msg_signature"]
		if sig == "" {
			sig = query["signature"]
		}
		if encrypt == "" {
			encrypt = query["echostr"]
		}
	}
	if sig == "" || ts == "" || nonce == "" || encrypt == "" {
		return false
	}
	return wecomcs.VerifySignature(token, sig, ts, nonce, encrypt)
}

// VerifyWeCombBot 智能机器人回调验签：sha1(sort(token,timestamp,nonce,encrypt))。
func VerifyWeCombBot(token string, body []byte, query map[string]string) bool {
	if token == "" {
		return false
	}
	var env struct {
		Encrypt string `json:"encrypt"`
	}
	_ = json.Unmarshal(body, &env)
	encrypt := env.Encrypt
	ts, nonce, sig := "", "", ""
	if query != nil {
		ts = query["timestamp"]
		nonce = query["nonce"]
		sig = query["msg_signature"]
	}
	if encrypt == "" || sig == "" || ts == "" || nonce == "" {
		return false
	}
	return wecombot.VerifySignature(token, sig, ts, nonce, encrypt)
}

// VerifyCustom 自定义渠道签名：
//
//	X-Hive-Signature: t=<unix>,v1=<hex hmac_sha256(secret, t+"."+body)>
//
// 5 分钟时间窗；缺字段/格式错/过期一律 false。
func VerifyCustom(secret string, body []byte, headers map[string]string, now time.Time) bool {
	if secret == "" {
		return false
	}
	raw := headerValue(headers, "X-Hive-Signature")
	if raw == "" {
		raw = headerValue(headers, "X-Hive-Signature")
	}
	ts, sig := parseHiveSignature(raw)
	if ts == "" || sig == "" {
		return false
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	drift := now.Sub(time.Unix(secs, 0))
	if drift > 5*time.Minute || drift < -5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(want)), []byte(strings.ToLower(sig)))
}

// SignCustom 生成自定义渠道出站签名头（与 VerifyCustom 同口径）。
func SignCustom(secret string, body []byte, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func parseHiveSignature(raw string) (ts, sig string) {
	for _, part := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sig = kv[1]
		}
	}
	return ts, sig
}

func headerValue(headers map[string]string, key string) string {
	if headers == nil {
		return ""
	}
	if v := headers[key]; v != "" {
		return v
	}
	if v := headers[strings.ToLower(key)]; v != "" {
		return v
	}
	// net/http 规范化为首字母大写、其余小写；这里再兜一层大小写不敏感匹配。
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// ── 解析 ──────────────────────────────────────────────────────────────

// parseSlackPayload Slack 事件信封 → 通用 ParsedPayload。
func parseSlackPayload(body []byte) (*ParsedPayload, error) {
	env, err := slack.ParseEnvelope(body)
	if err != nil {
		return nil, err
	}
	p := &ParsedPayload{Extra: map[string]any{
		"slack_event_id": env.EventID,
		"team_id":        env.TeamID,
		"channel":        env.Event.Channel,
		"ts":             env.Event.TS,
		"thread_ts":      env.Event.ThreadTS,
		"channel_type":   env.Event.ChannelType,
	}}
	p.EventID = env.EventID
	p.Sender = env.Event.User
	p.Content = env.Event.Text
	p.ChatID = env.Event.Channel
	switch {
	case env.Event.Subtype == "message_deleted":
		p.EventType = model.EventTypeMessageDeleted
	case env.Event.Subtype == "message_changed":
		p.EventType = model.EventTypeMessageEdited
	case env.Event.Type == "app_mention", env.Event.Type == "message":
		p.EventType = model.EventTypeMessageReceived
	case env.Type == "url_verification":
		p.EventType = "url_verification"
	default:
		p.EventType = env.Event.Type
	}
	if p.EventType == "" {
		p.EventType = model.EventTypeMessageReceived
	}
	return p, nil
}

// parseOneBotPayload OneBot 上报 → 通用 ParsedPayload。
func parseOneBotPayload(body []byte) (*ParsedPayload, error) {
	e, err := onebot.ParseEvent(body)
	if err != nil {
		return nil, err
	}
	in := e.ToInbound("")
	p := &ParsedPayload{Extra: map[string]any{
		"post_type":    e.PostType,
		"self_id":      e.SelfID,
		"message_type": e.MessageType,
		"message_id":   e.MessageID,
	}}
	if in != nil {
		p.EventID = in.MessageID
		p.EventType = in.EventType
		p.Sender = in.SenderID
		p.Content = in.Content
		p.ChatID = in.ConversationID
	} else {
		p.EventID = oneBotEventID(e)
		p.EventType = oneBotEventType(e)
	}
	if p.EventType == "" {
		p.EventType = model.EventTypeMessageReceived
	}
	return p, nil
}

// customInbound 自定义渠道固定 JSON 契约。
type customInbound struct {
	EventID        string `json:"event_id"`
	ChannelMsgID   string `json:"channel_msg_id"`
	ConversationID string `json:"conversation_id"`
	Sender         struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"sender"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
	MediaURL  string `json:"media_url"`
	Timestamp int64  `json:"timestamp"`
	IsGroup   bool   `json:"is_group"`
	GroupID   string `json:"group_id"`
}

func parseCustomPayload(body []byte) (*ParsedPayload, error) {
	var in customInbound
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("custom parse: %w", err)
	}
	if in.EventID == "" && in.ChannelMsgID == "" {
		return nil, errors.New("custom payload missing event_id/channel_msg_id")
	}
	id := in.EventID
	if id == "" {
		id = in.ChannelMsgID
	}
	mt := in.MsgType
	if mt == "" {
		mt = model.MsgTypeText
	}
	return &ParsedPayload{
		EventID:   id,
		EventType: model.EventTypeMessageReceived,
		Sender:    in.Sender.ID,
		Content:   in.Content,
		ChatID:    in.ConversationID,
		Extra: map[string]any{
			"channel_msg_id":  in.ChannelMsgID,
			"conversation_id": in.ConversationID,
			"sender_name":     in.Sender.Name,
			"msg_type":        mt,
			"media_url":       in.MediaURL,
			"is_group":        in.IsGroup,
			"group_id":        in.GroupID,
		},
	}, nil
}

// ── 账号密钥读取（025 平台批各表独立，不复用 integration_accounts） ──────

func (s *WebhookService) slackAccount(ctx context.Context, accountID string) *model.SlackAccount {
	if s == nil || s.slackRepo == nil {
		return nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	acc, _ := s.slackRepo.GetByID(ctx, uint(id))
	return acc
}

func (s *WebhookService) oneBotAccount(ctx context.Context, accountID string) *model.OneBotAccount {
	if s == nil || s.onebotRepo == nil {
		return nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	acc, _ := s.onebotRepo.GetByID(ctx, uint(id))
	return acc
}

func (s *WebhookService) wecomCSAccount(ctx context.Context, accountID string) *model.WeComCSAccount {
	if s == nil || s.wecomCSRepo == nil {
		return nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	acc, _ := s.wecomCSRepo.GetByID(ctx, uint(id))
	return acc
}

func (s *WebhookService) wecombotAccount(ctx context.Context, accountID string) *model.WeCombBotAccount {
	if s == nil || s.wecombotRepo == nil {
		return nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	acc, _ := s.wecombotRepo.GetByID(ctx, uint(id))
	return acc
}

func (s *WebhookService) customAccount(ctx context.Context, accountID string) *model.CustomAccount {
	if s == nil || s.customRepo == nil {
		return nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	acc, _ := s.customRepo.GetByID(ctx, uint(id))
	return acc
}

// ── 回调地址验证（需在入队前同步响应） ────────────────────────────────

// HandleSlackURLVerification Slack 回调地址验证：先验签，再原样回 challenge。
// 返回 handled=true 时控制器直接 JSON 响应。
func (s *WebhookService) HandleSlackURLVerification(ctx context.Context, accountID string, body []byte, headers map[string]string) (challenge string, handled bool, err error) {
	env, perr := slack.ParseEnvelope(body)
	if perr != nil || env.Type != "url_verification" {
		return "", false, nil
	}
	acc := s.slackAccount(ctx, accountID)
	if acc == nil || acc.SigningSecret == "" {
		return "", true, errors.New("slack signing_secret 未配置")
	}
	if !VerifySlack(acc.SigningSecret, body, headers) {
		return "", true, errors.New("slack signature mismatch")
	}
	return env.Challenge, true, nil
}

// HandleWeComCSVerify 企业微信客服回调 URL 验证（GET）：
// 验签通过后原样返回 echostr，否则返回空串。
func (s *WebhookService) HandleWeComCSVerify(ctx context.Context, accountID string, query map[string]string) (string, error) {
	acc := s.wecomCSAccount(ctx, accountID)
	if acc == nil || acc.Token == "" {
		return "", errors.New("wecomcs token 未配置")
	}
	ts, nonce, sig, echo := "", "", "", ""
	if query != nil {
		ts, nonce, sig, echo = query["timestamp"], query["nonce"], query["msg_signature"], query["echostr"]
	}
	if sig == "" && query != nil {
		sig = query["signature"]
	}
	if echo == "" {
		return "", errors.New("missing echostr")
	}
	if !wecomcs.VerifySignature(acc.Token, sig, ts, nonce, echo) {
		return "", errors.New("wecomcs signature mismatch")
	}
	return echo, nil
}

// ── 分发 ──────────────────────────────────────────────────────────────

// dispatchSlack Slack 入站：url_verification 由控制器短路；机器人自身消息忽略。
func (s *WebhookService) dispatchSlack(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if !s.connected() {
		return nil, nil
	}
	env, err := slack.ParseEnvelope(raw)
	if err != nil {
		return nil, err
	}
	if env.IsBotMessage() || env.Event.Subtype == "bot_message" {
		return nil, nil
	}
	if env.Type == "url_verification" {
		return nil, nil
	}
	et := model.EventTypeMessageReceived
	msgID := "slack_" + env.Event.TS
	switch env.Event.Subtype {
	case "message_deleted":
		et = model.EventTypeMessageDeleted
		msgID = "slack_del_" + env.Event.DeletedTS
	case "message_changed":
		et = model.EventTypeMessageEdited
	}
	if et == model.EventTypeMessageDeleted && env.Event.DeletedTS == "" {
		return nil, nil
	}
	in := &core.InboundMessage{
		Platform:       model.ChannelSlack,
		AccountID:      accountID,
		MessageID:      msgID,
		ConversationID: env.Event.Channel,
		SenderID:       env.Event.User,
		SenderName:     env.Event.User,
		Content:        env.Event.Text,
		MsgType:        model.MsgTypeText,
		IsGroup:        env.Event.ChannelType != "im",
		GroupID:        env.Event.Channel,
		Timestamp:      slackTS(env.Event.TS),
		EventType:      et,
		Extra:          map[string]any{"thread_ts": firstNonEmpty(env.Event.ThreadTS, env.Event.TS)},
	}
	return s.ingressPlatformInbound(ctx, in)
}

// dispatchOneBot OneBot 入站：message/notice/request/meta 四类由 ToInbound 归一。
func (s *WebhookService) dispatchOneBot(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if !s.connected() {
		return nil, nil
	}
	e, err := onebot.ParseEvent(raw)
	if err != nil {
		return nil, err
	}
	in := e.ToInbound(accountID)
	if in == nil {
		// 未知 post_type / notice：留一条结构化日志，不伪造消息。
		logger.Infof("[OneBot] skip unhandled event account=%s post_type=%s notice=%s", accountID, e.PostType, e.NoticeType)
		return nil, nil
	}
	in.Extra = mergeExtra(in.Extra, map[string]any{
		"channel_msg_id": fmt.Sprintf("%d", e.MessageID),
		"self_id":        fmt.Sprintf("%d", e.SelfID),
	})
	return s.ingressPlatformInbound(ctx, in)
}

// dispatchWeCombBot 智能机器人入站：回调体是密文，解密后按明文分发。
func (s *WebhookService) dispatchWeCombBot(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if !s.connected() {
		return nil, nil
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("wecombot account_id 非法: %q", accountID)
	}
	var enc struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &enc); err != nil {
		return nil, err
	}
	acc, err := s.wecombotRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return nil, fmt.Errorf("wecombot account %s 不存在: %w", accountID, err)
	}
	plain, err := wecombot.Decrypt(acc.AESKey, enc.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("wecombot decrypt: %w", err)
	}
	m, err := wecombot.ParseMessage(plain)
	if err != nil {
		return nil, err
	}
	if m.MsgType == "event" {
		// 事件只留审计证据（不进消息链路）。
		logger.Infof("[WeCombBot] event account=%s eventtype=%s msgid=%s", accountID, wecombotEventType(m), m.MsgID)
		return nil, nil
	}
	content := ""
	if m.Text != nil {
		content = m.Text.Content
	} else if m.Markdown != nil {
		content = m.Markdown.Content
	}
	in := &core.InboundMessage{
		Platform:       model.ChannelWeCombBot,
		AccountID:      accountID,
		MessageID:      model.ChannelWeCombBot + "_" + m.MsgID,
		ConversationID: m.ChatID,
		SenderID:       m.From.UserID,
		SenderName:     firstNonEmpty(m.From.Name, m.From.UserID),
		Content:        content,
		MsgType:        model.MsgTypeText,
		IsGroup:        true,
		GroupID:        m.ChatID,
		Timestamp:      m.CreateTime,
	}
	return s.ingressPlatformInbound(ctx, in)
}

func wecombotEventType(m *wecombot.Message) string {
	if m == nil || m.Event == nil {
		return ""
	}
	return m.Event.EventType
}

// dispatchCustom 自定义渠道入站。
func (s *WebhookService) dispatchCustom(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if !s.connected() {
		return nil, nil
	}
	var in customInbound
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	// 旧报文兼容：固定契约字段缺省时回退通用 ParsedPayload（event_id/content/from…）。
	if in.Sender.ID == "" && p != nil {
		in.Sender.ID = p.Sender
	}
	if in.Content == "" && p != nil {
		in.Content = p.Content
	}
	if in.ConversationID == "" && p != nil {
		in.ConversationID = p.ChatID
	}
	if in.EventID == "" && p != nil {
		in.EventID = p.EventID
	}
	mt := in.MsgType
	if mt == "" {
		mt = model.MsgTypeText
	}
	msgID := in.ChannelMsgID
	if msgID == "" {
		msgID = in.EventID
	}
	conv := in.ConversationID
	if conv == "" {
		conv = in.Sender.ID
	}
	tm := in.Timestamp
	if tm == 0 {
		tm = time.Now().Unix()
	}
	msg := &core.InboundMessage{
		Platform:       model.ChannelCustom,
		AccountID:      accountID,
		MessageID:      "custom_" + msgID,
		ConversationID: conv,
		SenderID:       in.Sender.ID,
		SenderName:     firstNonEmpty(in.Sender.Name, in.Sender.ID),
		Content:        in.Content,
		MsgType:        mt,
		IsGroup:        in.IsGroup,
		GroupID:        in.GroupID,
		Timestamp:      tm,
		Extra:          map[string]any{"channel_msg_id": msgID, "media_url": in.MediaURL},
	}
	return s.ingressPlatformInbound(ctx, msg)
}

// ingressPlatformInbound 将归一后的入站消息交给消息中台，并返回供 AI 触发的内存 hub 视图。
//
// 非 message.received 的平台事件由中台分流（只审计/总线），本函数返回 nil，避免被当成
// 一条客户消息去触发 AI。与 dispatchQQ 的既有口径一致。
func (s *WebhookService) ingressPlatformInbound(ctx context.Context, in *core.InboundMessage) (*model.MessageHub, error) {
	if in == nil {
		return nil, nil
	}
	if in.SenderID == "" {
		return nil, nil
	}
	ev := in.ToMessageEvent(in.AccountID)
	if s.ingressSvc != nil {
		if _, err := s.ingressSvc.HandleIngressMessage(ctx, ev); err != nil {
			return nil, err
		}
	}
	if in.EventType != "" && in.EventType != model.EventTypeMessageReceived {
		return nil, nil
	}
	if in.MsgType == "" {
		in.MsgType = model.MsgTypeText
	}
	hub := &model.MessageHub{
		Platform:       in.Platform,
		AccountID:      in.AccountID,
		MsgID:          in.MessageID,
		Direction:      "inbound",
		SenderID:       in.SenderID,
		SenderName:     in.SenderName,
		ConversationID: in.ConversationID,
		MsgType:        in.MsgType,
		Content:        in.Content,
		SentAt:         time.Now(),
		IsGroup:        in.IsGroup,
		GroupID:        in.GroupID,
		Extra:          ev.Extra,
	}
	return hub, nil
}

// ── 小工具 ────────────────────────────────────────────────────────────

func mergeExtra(base, add map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

// slackTS Slack 时间戳 "1700000000.000200" → unix 秒；解析失败返回 0。
func slackTS(ts string) int64 {
	if ts == "" {
		return 0
	}
	if i := strings.IndexByte(ts, '.'); i > 0 {
		ts = ts[:i]
	}
	n, _ := strconv.ParseInt(ts, 10, 64)
	return n
}

// oneBotEventID 非消息事件（request/notice）的幂等键。
func oneBotEventID(e *onebot.Event) string {
	return fmt.Sprintf("onebot_%d_%s_%s_%d_%d", e.Time, e.PostType, firstNonEmpty(e.NoticeType, e.RequestType), e.UserID, e.GroupID)
}

// oneBotEventType 非消息事件的统一事件类型。
func oneBotEventType(e *onebot.Event) string {
	switch e.PostType {
	case "notice":
		switch e.NoticeType {
		case "group_recall", "friend_recall":
			return model.EventTypeMessageDeleted
		case "group_increase":
			return model.EventTypeGroupMemberJoined
		case "group_decrease":
			return model.EventTypeGroupMemberLeft
		}
	case "request":
		// 好友/入群申请只入事件总线，绝不自动同意（避免被拉进任意群）。
		return "request.received"
	case "meta_event":
		return "meta_event"
	}
	return ""
}
