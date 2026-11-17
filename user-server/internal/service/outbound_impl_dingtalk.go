package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outboundDingTalk 是渠道 DingTalk 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundDingTalk(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {

	webhookURL := ""
	var expiredAt int64
	if hubMsg != nil && hubMsg.Extra != nil {
		if v, ok := hubMsg.Extra["session_webhook"].(string); ok {
			webhookURL = v
		}
		switch t := hubMsg.Extra["session_webhook_expired_at"].(type) {
		case int64:
			expiredAt = t
		case float64:
			expiredAt = int64(t)
		}
	}
	if webhookURL == "" {
		logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
			Str("conv_id", convIDOrEmpty(hubMsg)).
			Msg("[DingTalk] 缺少 sessionWebhook（回调未携带或非机器人消息），AI 回复无法送达")
		fail(preSendFailure(channel, CategoryWindowExpired,
			"钉钉回调未携带 sessionWebhook（非机器人消息或平台未下发），无可用的回复地址"))
		return
	}

	if expiredAt > 1_000_000_000_000 {
		expiredAt /= 1000
	}
	if expiredAt > 0 && time.Now().Unix() > expiredAt {
		logger.Ctx(ctx).Warn().Str("channel", "dingtalk").Str("account_id", accountID).
			Int64("expired_at", expiredAt).
			Msg("[DingTalk] sessionWebhook 已过期，AI 回复无法送达（用户重新发言可恢复）")
		fail(preSendFailure(channel, CategoryWindowExpired,
			fmt.Sprintf("钉钉 sessionWebhook 已过期(expired_at=%d)，被动回复窗口已关闭", expiredAt)))
		return
	}

	u, perr := url.Parse(webhookURL)
	if perr != nil || !loadDingtalkWebhookHostAllowed()(u) {
		logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
			Msg("[DingTalk] sessionWebhook 域名非法（仅允许 *.dingtalk.com），拒绝发送")
		fail(preSendFailure(channel, CategoryBadRequest,
			"钉钉 sessionWebhook 域名不在允许清单(*.dingtalk.com)内，拒绝向外部地址投递"))
		return
	}
	payload := map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": content},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("build dingtalk reply request failed")
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("钉钉回复请求构造失败: %v", err)))
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("dingtalk reply send failed")
		fail(err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	var dtResult struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &dtResult); err != nil || dtResult.Errcode != 0 {
		logger.Ctx(ctx).Error().Int("http_status", resp.StatusCode).Int("errcode", dtResult.Errcode).
			Str("errmsg", dtResult.Errmsg).
			Msg("[DingTalk] AI 回复出站被平台拒绝")
		if err != nil {
			fail(fmt.Errorf("dingtalk sessionWebhook 响应不可解析(status=%d): %w", resp.StatusCode, err))
		} else {
			fail(fmt.Errorf("dingtalk sessionWebhook errcode=%d errmsg=%s", dtResult.Errcode, dtResult.Errmsg))
		}
		return
	}
	sent = true
	return
}
