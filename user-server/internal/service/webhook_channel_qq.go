package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ChannelQQ webhook 渠道常量（与 model.ChannelQQ 对齐）
const ChannelQQ WebhookChannel = "qq"

// VerifyQQ Ed25519 webhook 验签（secret 为官方 BotSecret 派生种子）
//
// QQ 官方无 secret-token 头模式，必须 Ed25519；secret 缺失直接拒绝。
// 开发环境放行走全局 ALLOW_INSECURE_WEBHOOK（在 WebhookService.Verify 入口统一处理）。
func VerifyQQ(secret, sigHex, timestamp string, body []byte) bool {
	return qq.VerifySignature(secret, sigHex, timestamp, body)
}

// HandleQQCallbackChallenge Op13 回调地址验证短路处理。
// 返回 handled=true 时调用方应直接以 JSON 应答（不走 webhook 入队流程）。
//
// 安全缓解（P1-2 签名预言机）：op13 必须在验签前处理（协议先有鸡还是先有蛋），
// 但对 challenge 参数做严格校验——plain_token 长度约束 + event_ts 新鲜度（±5 分钟），
// 使该端点无法被滥用为任意消息的签名预言机。
func (s *WebhookService) HandleQQCallbackChallenge(ctx context.Context, accountID string, raw []byte) (handled bool, payload any) {
	e, err := qq.ParseEvent(raw)
	if err != nil || !e.IsCallbackVerify() {
		return false, nil
	}
	if !qq.IsValidChallenge(e.PlainToken, e.EventTS) {
		logger.Errorf("[QQ] op13 challenge rejected: invalid plain_token/event_ts account=%s", accountID)
		return true, map[string]any{"plain_token": "", "signature": ""}
	}
	qqSvc := NewQQService(s.db)
	plainToken, signature, ok := qqSvc.VerifyQQCallbackChallenge(ctx, accountID, e)
	if !ok {
		logger.Errorf("[QQ] op13 challenge verify failed account=%s", accountID)
		return true, map[string]any{"plain_token": "", "signature": ""}
	}
	logger.Infof("[QQ] op13 callback verify ok account=%s", accountID)
	return true, map[string]any{"plain_token": plainToken, "signature": signature}
}

// dispatchQQ QQ 入站消息分发（与 dispatchTelegram 同构）
//
// 流程：解析事件 → Ingress 进消息中台（幂等 EventID）→ upsert inbox → 返回 hub 消息供 AI 触发判定。
func (s *WebhookService) dispatchQQ(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if s.db == nil {
		return nil, nil
	}
	s.ensureReposFromDB(ctx)

	e, err := qq.ParseEvent(raw)
	if err != nil {
		return nil, fmt.Errorf("qq parse event: %w", err)
	}
	if e.IsCallbackVerify() {
		// Op13 验证请求已在 Verify 阶段短路，正常不应到达 dispatch
		return nil, nil
	}

	// 回填 ParsedPayload 通用字段（供 ToUnifiedMessage / 触发 AI 使用）
	inbound := e.ToInbound(accountID)
	if inbound != nil {
		p.Content = inbound.Content
		p.Sender = inbound.SenderID
		p.ChatID = inbound.ConversationID
	}

	// 经消息中台 Ingress（幂等 + AI 串行锁 + message_hub 落库 + 触发 AgentRuntime 由中台统一负责）
	if err := e.Ingress(ctx, s.ingressHandler(ctx), accountID); err != nil {
		return nil, err
	}

	if inbound == nil {
		return nil, nil
	}

	// 同步构造 hub 记录供 sendOutbound / AI 触发使用（Ingress 内部已落库，此处为内存视图）。
	// MsgID/Extra 与 Ingress 落库口径对齐（MsgID=qq_evt_{事件id}，Extra.channel_msg_id
	// 供 QQOutboundMsgID 取被动回复关联 ID——P2-2 修复）。
	hub := &model.MessageHub{
		Platform:       model.ChannelQQ,
		AccountID:      accountID,
		MsgID:          inbound.MessageID,
		Direction:      "inbound",
		SenderID:       inbound.SenderID,
		ConversationID: inbound.ConversationID,
		MsgType:        model.MsgTypeText,
		Content:        inbound.Content,
		SentAt:         time.Now(),
		IsGroup:        inbound.IsGroup,
		GroupID:        inbound.GroupID,
		Extra:          map[string]any{"channel_msg_id": inbound.MessageID},
	}
	if hub.Content == "" {
		hub.Content = "[qq]"
	}
	return hub, nil
}

// getQQWebhookSecret 取 QQ 账号验签 secret
func (s *WebhookService) getQQWebhookSecret(ctx context.Context, accountID string) string {
	if s.qqRepo == nil {
		return ""
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	acc, err := s.qqRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return ""
	}
	return acc.WebhookSecret
}

// triggerQQSalesEngine QQ AI 触发（群消息必须有内容；单聊直接触发）
func (s *WebhookService) triggerQQSalesEngine(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, hubMsg *model.MessageHub) {
	if hubMsg == nil || strings.TrimSpace(p.Content) == "" {
		return
	}
	s.triggerSalesEngine(ctx, channel, accountID, p, hubMsg)
}

// QQOutboundMsgID 出站回复关联的原消息 ID（从 hub Extra 取 channel_msg_id）
func QQOutboundMsgID(hubMsg *model.MessageHub) string {
	if hubMsg == nil || hubMsg.Extra == nil {
		return ""
	}
	if v, ok := hubMsg.Extra["channel_msg_id"].(string); ok {
		return v
	}
	return ""
}
