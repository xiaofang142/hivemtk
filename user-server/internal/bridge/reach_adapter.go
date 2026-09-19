package bridge

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/service"
)

type BridgeReachAdapter struct {
	inner   tooluse.ReachAdapter
	ingress *service.InboxIngressService
}

// NewBridgeReachAdapter 构造桥接触达适配器。
//
// ingress 可选：nil 时 EnqueueManualReply 返回 error。Send* 等旧方法已删除，构造时
// 不再注入 httpReplyBuffer。
func NewBridgeReachAdapter(inner tooluse.ReachAdapter, ingress ...*service.InboxIngressService) *BridgeReachAdapter {
	a := &BridgeReachAdapter{inner: inner}
	if len(ingress) > 0 {
		a.ingress = ingress[0]
	}
	return a
}

// SetIngress 后期注入 ingress（避免装配阶段循环依赖；服务启动时由 router 调用一次）
func (a *BridgeReachAdapter) SetIngress(ingress *service.InboxIngressService) {
	a.ingress = ingress
}

func (a *BridgeReachAdapter) EnqueueManualReply(ctx context.Context, channel, accountID, conversationID, content, senderID string) error {
	if a == nil {
		return errors.New("bridge adapter not initialized")
	}
	if a.ingress == nil {
		return errors.New("bridge ingress not initialized")
	}
	return service.DeliverBridgeOutbound(ctx, channel, accountID, conversationID, "text", content, "")
}

func (a *BridgeReachAdapter) SendDouyin(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return a.deliverToOutbox(ctx, "douyin", accountID, openID, msgType, content)
}

func (a *BridgeReachAdapter) SendKuaishou(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return a.deliverToOutbox(ctx, "kuaishou", accountID, openID, msgType, content)
}

func (a *BridgeReachAdapter) SendXHS(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return a.deliverToOutbox(ctx, "xiaohongshu", accountID, openID, msgType, content)
}

func (a *BridgeReachAdapter) SendTikTok(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return a.deliverToOutbox(ctx, "tiktok", accountID, openID, msgType, content)
}

func (a *BridgeReachAdapter) SendXianyu(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return a.deliverToOutbox(ctx, "xianyu", accountID, openID, msgType, content)
}

func (a *BridgeReachAdapter) deliverToOutbox(ctx context.Context, channel, accountID, openID, msgType, content string) (string, error) {
	if err := service.DeliverBridgeOutbound(ctx, channel, accountID, openID, msgType, content, ""); err != nil {
		return "", err
	}
	return "bridge:" + channel + ":" + accountID + ":" + openID, nil
}

func (a *BridgeReachAdapter) SendSMS(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
	return a.inner.SendSMS(ctx, phone, content, templateID, params)
}

func (a *BridgeReachAdapter) SendEmail(ctx context.Context, to, subject, content string, attachments []string) (string, error) {
	return a.inner.SendEmail(ctx, to, subject, content, attachments)
}

func (a *BridgeReachAdapter) SendWeCom(ctx context.Context, accountID, externalUserID, msgType, content string) (string, error) {
	return a.inner.SendWeCom(ctx, accountID, externalUserID, msgType, content)
}

func (a *BridgeReachAdapter) SendWeixin(ctx context.Context, openID, msgType, content string) (string, error) {
	return a.inner.SendWeixin(ctx, openID, msgType, content)
}

func (a *BridgeReachAdapter) SendDingTalk(ctx context.Context, chatID, msgType, content string) (string, error) {
	return a.inner.SendDingTalk(ctx, chatID, msgType, content)
}

func (a *BridgeReachAdapter) SendTelegram(ctx context.Context, accountID, chatID, content string) (string, error) {
	return a.inner.SendTelegram(ctx, accountID, chatID, content)
}

func (a *BridgeReachAdapter) SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (string, error) {
	return a.inner.SendWhatsApp(ctx, accountID, toPhone, content)
}

func (a *BridgeReachAdapter) SendFeishu(ctx context.Context, accountID, openID, content string) (string, error) {
	return a.inner.SendFeishu(ctx, accountID, openID, content)
}

func (a *BridgeReachAdapter) SendWeb(ctx context.Context, sessionID, content string) (string, error) {
	return a.inner.SendWeb(ctx, sessionID, content)
}

func (a *BridgeReachAdapter) SendCard(ctx context.Context, channel, accountID, externalUserID, cardID string) (string, error) {
	return a.inner.SendCard(ctx, channel, accountID, externalUserID, cardID)
}

// Recall B8（2026-09-19）：Bridge 渠道的 msgID 是 deliverToOutbox 生成的合成键
// （bridge:channel:account:openid），不是平台真实消息标识——透传给 inner 会拿假 ID
// 调撤回 API（静默失败或误撤）。且下行本无平台侧撤回通道，显式 not_supported。
func (a *BridgeReachAdapter) Recall(ctx context.Context, channel, msgID string) error {
	if IsBridgeChannel(channel) {
		return fmt.Errorf("recall: bridge 渠道 %s 不支持撤回（msgID 为合成键，非平台消息标识）", channel)
	}
	return a.inner.Recall(ctx, channel, msgID)
}

func (a *BridgeReachAdapter) AccountHealth(ctx context.Context, channel, accountID string) (*tooluse.AccountHealthInfo, error) {
	return a.inner.AccountHealth(ctx, channel, accountID)
}

// GlobalBridgeReachAdapter 占位全局（reach_sender_wiring.go 引用）
var GlobalBridgeReachAdapter *BridgeReachAdapter

func (a *BridgeReachAdapter) ListAccounts(ctx context.Context, channel string) ([]tooluse.AccountInfo, error) {
	return a.inner.ListAccounts(ctx, channel)
}
