package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/onebot"
	"hivemtk-user/internal/channelbot/slack"
	"hivemtk-user/internal/channelbot/wecombot"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
)

// outbound_impl_im_platforms.go 025 平台批发送器（text 为主；OneBot 做 CQ 转义与切块）。
//
// 失败一律经 req.Fail 上报（写终态失败行/告警），不返回 error —— 与 specs/024 D1
// 的 sender 契约一致。

// ── Slack ─────────────────────────────────────────────────────────────

type SlackOutboundSender struct{ svc *WebhookService }

func (x *SlackOutboundSender) Channel() WebhookChannel { return ChannelSlack }

func (x *SlackOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return x.svc.outboundSlack(ctx, req)
}

func (s *WebhookService) outboundSlack(ctx context.Context, req *OutboundRequest) *OutboundResult {
	acc := s.slackAccount(ctx, req.AccountID)
	if acc == nil || acc.BotToken == "" {
		req.Fail(preSendFailure(req.Channel, CategoryAuth, "Slack 账号不存在或 BotToken 未配置"))
		return &OutboundResult{}
	}
	channel := ""
	threadTS := ""
	if req.Hub != nil {
		channel = req.Hub.ConversationID
		if req.Hub.Extra != nil {
			threadTS, _ = req.Hub.Extra["thread_ts"].(string)
		}
	}
	if channel == "" && req.Payload != nil {
		channel = req.Payload.ChatID
	}
	if channel == "" {
		req.Fail(preSendFailure(req.Channel, CategoryBadRequest, "Slack 出站缺少会话 channel"))
		return &OutboundResult{}
	}
	c := slack.NewClient(acc.BotToken)
	if s.slackAPIBase != "" {
		c = slack.NewClient(acc.BotToken, core.WithBaseURL(s.slackAPIBase))
	}
	ts, err := c.PostMessage(ctx, channel, req.Content, threadTS)
	if err != nil {
		req.Fail(err)
		return &OutboundResult{}
	}
	return sentOutboundResult("text", ts)
}

// ── OneBot v11 ────────────────────────────────────────────────────────

type OneBotOutboundSender struct{ svc *WebhookService }

func (x *OneBotOutboundSender) Channel() WebhookChannel { return ChannelOneBot }

func (x *OneBotOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return x.svc.outboundOneBot(ctx, req)
}

func (s *WebhookService) outboundOneBot(ctx context.Context, req *OutboundRequest) *OutboundResult {
	acc := s.oneBotAccount(ctx, req.AccountID)
	if acc == nil {
		req.Fail(preSendFailure(req.Channel, CategoryAuth, "OneBot 账号不存在"))
		return &OutboundResult{}
	}
	if acc.HTTPBaseURL == "" && (s.onebotWS == nil || !s.onebotWS.Connected(acc.ID)) {
		req.Fail(preSendFailure(req.Channel, CategoryNetwork, "OneBot 既无反向 WS 连接也未配置 HTTPBaseURL"))
		return &OutboundResult{}
	}
	groupID := ""
	var sender string
	if req.Hub != nil {
		groupID = req.Hub.GroupID
		if req.Hub.Direction == "outbound" {
			sender = req.Hub.ReceiverID
		} else {
			sender = req.Hub.SenderID
		}
	}
	if groupID == "" && req.Payload != nil {
		sender = req.Payload.Sender
	}
	text := onebot.EscapeCQ(req.Content)
	chunks := onebot.SplitText(text, 2000)
	if len(chunks) == 0 {
		req.Fail(preSendFailure(req.Channel, CategoryBadRequest, "OneBot 出站正文为空"))
		return &OutboundResult{}
	}
	result := &OutboundResult{Receipts: []OutboundArtifactReceipt{}}
	for index, chunk := range chunks {
		artifactID := fmt.Sprintf("chunk-%d", index+1)
		var messageID int64
		var err error
		if gid, perr := strconv.ParseInt(groupID, 10, 64); perr == nil && gid > 0 {
			messageID, err = s.oneBotSend(ctx, acc, "send_group_msg", map[string]any{"group_id": gid, "message": chunk})
		} else {
			uid, perr := strconv.ParseInt(sender, 10, 64)
			if perr != nil || uid == 0 {
				req.Fail(preSendFailure(req.Channel, CategoryBadRequest,
					fmt.Sprintf("OneBot 出站目标非法（group_id=%q user_id=%q）", groupID, sender)))
				return result
			}
			messageID, err = s.oneBotSend(ctx, acc, "send_private_msg", map[string]any{"user_id": uid, "message": chunk})
		}
		if err != nil {
			result.Receipts = append(result.Receipts, OutboundArtifactReceipt{
				ArtifactID: artifactID, Status: OutboundArtifactFailed, Error: err.Error(),
			})
			if len(result.Receipts) > 1 {
				result.Sent = true
				result.Partial = true
				result.Unknown = true
			}
			req.Fail(err)
			return result
		}
		platformMsgID := ""
		if messageID > 0 {
			platformMsgID = strconv.FormatInt(messageID, 10)
		}
		result.Receipts = append(result.Receipts, OutboundArtifactReceipt{
			ArtifactID: artifactID, PlatformMsgID: platformMsgID, Status: OutboundArtifactSent,
		})
	}
	result.Sent = true
	if result.PlatformMsgID == "" && len(result.Receipts) > 0 {
		result.PlatformMsgID = result.Receipts[0].PlatformMsgID
	}
	return result
}

func (s *WebhookService) oneBotSend(ctx context.Context, acc *model.OneBotAccount, action string, params map[string]any) (int64, error) {
	if s.onebotWS != nil && s.onebotWS.Connected(acc.ID) {
		if err := s.onebotWS.Send(ctx, acc.ID, action, params); err == nil {
			return 0, nil
		}
		// WS 发失败回落 HTTP（协议端可能刚断线，连接表尚未清理）。
	}
	c := onebot.NewClient(acc.HTTPBaseURL, acc.AccessToken)
	switch action {
	case "send_group_msg":
		gid, _ := params["group_id"].(int64)
		msg, _ := params["message"].(string)
		return c.SendGroupMsg(ctx, gid, msg)
	case "send_private_msg":
		uid, _ := params["user_id"].(int64)
		msg, _ := params["message"].(string)
		return c.SendPrivateMsg(ctx, uid, msg)
	}
	return 0, fmt.Errorf("onebot unsupported action %s", action)
}

// ── 企业微信客服 ──────────────────────────────────────────────────────

type WeComCSOutboundSender struct{ svc *WebhookService }

func (x *WeComCSOutboundSender) Channel() WebhookChannel { return ChannelWeComCS }

func (x *WeComCSOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return x.svc.outboundWeComCS(ctx, req)
}

func (s *WebhookService) outboundWeComCS(ctx context.Context, req *OutboundRequest) *OutboundResult {
	acc := s.wecomCSAccount(ctx, req.AccountID)
	if acc == nil || acc.CorpID == "" || acc.Secret == "" {
		req.Fail(preSendFailure(req.Channel, CategoryAuth, "企业微信客服账号不存在或 Secret 未配置"))
		return &OutboundResult{}
	}
	touser := ""
	if req.Hub != nil {
		if req.Hub.Direction == "outbound" {
			touser = req.Hub.ReceiverID
		} else {
			touser = req.Hub.SenderID
		}
	}
	if touser == "" && req.Payload != nil {
		touser = req.Payload.Sender
	}
	if touser == "" {
		req.Fail(preSendFailure(req.Channel, CategoryBadRequest, "企业微信客服出站缺少 external_userid"))
		return &OutboundResult{}
	}
	w := s.ensureWeComCS()
	client := w.clientFor(acc.ID)
	token, err := client.GetToken(ctx, acc.CorpID, acc.Secret)
	if err != nil {
		req.Fail(err)
		return &OutboundResult{}
	}
	msgID, err := client.SendMsg(ctx, token, &wecomcs.SendMsgReq{
		Touser:   touser,
		OpenKfID: acc.OpenKfID,
		MsgType:  "text",
		Text:     &wecomcs.TextBody{Content: req.Content},
	})
	if err != nil {
		var ce *wecomcs.Error
		if asWeComCSError(err, &ce) && ce.IsWindowExpired() {
			req.Fail(preSendFailure(req.Channel, CategoryWindowExpired,
				"企业微信客服 48 小时会话窗口已关闭："+ce.Error()))
			return &OutboundResult{}
		}
		req.Fail(err)
		return &OutboundResult{}
	}
	return sentOutboundResult("text", msgID)
}

func asWeComCSError(err error, target **wecomcs.Error) bool {
	for err != nil {
		if e, ok := err.(*wecomcs.Error); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ── 企业微信智能机器人（群机器人） ────────────────────────────────────

type WeCombBotOutboundSender struct{ svc *WebhookService }

func (x *WeCombBotOutboundSender) Channel() WebhookChannel { return ChannelWeCombBot }

func (x *WeCombBotOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return x.svc.outboundWeCombBot(ctx, req)
}

func (s *WebhookService) outboundWeCombBot(ctx context.Context, req *OutboundRequest) *OutboundResult {
	acc := s.wecombotAccount(ctx, req.AccountID)
	if acc == nil || acc.WebhookURL == "" {
		req.Fail(preSendFailure(req.Channel, CategoryAuth, "企微群机器人账号不存在或 WebhookURL 未配置"))
		return &OutboundResult{}
	}
	if err := wecombot.NewClient().PushMessage(ctx, acc.WebhookURL, "markdown", req.Content); err != nil {
		req.Fail(err)
		return &OutboundResult{}
	}
	return sentOutboundResult("text", "")
}

// ── 自定义 webhook ────────────────────────────────────────────────────

type CustomOutboundSender struct{ svc *WebhookService }

func (x *CustomOutboundSender) Channel() WebhookChannel { return ChannelCustom }

func (x *CustomOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return x.svc.outboundCustom(ctx, req)
}

func (s *WebhookService) outboundCustom(ctx context.Context, req *OutboundRequest) *OutboundResult {
	acc := s.customAccount(ctx, req.AccountID)
	if acc == nil || acc.CallbackURL == "" || acc.Secret == "" {
		req.Fail(preSendFailure(req.Channel, CategoryAuth, "custom 账号不存在或 callback_url/secret 未配置"))
		return &OutboundResult{}
	}
	conv := ""
	if req.Hub != nil {
		conv = req.Hub.ConversationID
	}
	body, _ := json.Marshal(map[string]any{
		"event_id":        fmt.Sprintf("out_%d", time.Now().UnixNano()),
		"conversation_id": conv,
		"msg_type":        model.MsgTypeText,
		"content":         req.Content,
		"timestamp":       time.Now().Unix(),
	})
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(cctx, http.MethodPost, acc.CallbackURL, bytes.NewReader(body))
	if err != nil {
		req.Fail(preSendFailure(req.Channel, CategoryBadRequest, err.Error()))
		return &OutboundResult{}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hive-Signature", SignCustom(acc.Secret, body, time.Now()))
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		req.Fail(preSendFailure(req.Channel, CategoryNetwork, "custom 回调请求失败: "+err.Error()))
		return &OutboundResult{}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return sentOutboundResult("text", "")
	}
	cat := CategoryBadRequest
	if resp.StatusCode >= 500 {
		cat = CategoryNetwork
	}
	req.Fail(preSendFailure(req.Channel, cat, fmt.Sprintf("custom 回调返回 HTTP %d", resp.StatusCode)))
	return &OutboundResult{}
}

// SetSlackAPIBaseForTest 把 Slack Web API 基址指向假平台（仅测试用）。
//
// 为什么需要钩子：channelbot/core.BaseClient 自建 http.Transport，测试里替换
// http.DefaultClient.Transport 拦不到它；不注入就只能真打 api.slack.com。
func (s *WebhookService) SetSlackAPIBaseForTest(base string) { s.slackAPIBase = base }
