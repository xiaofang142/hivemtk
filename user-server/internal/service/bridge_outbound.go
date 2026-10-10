package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

var bridgeChannels = map[string]struct{}{}

// SetBridgeChannels 用 channelgw.Default 的渠道名集合重建白名单（B-5 运行时单一来源）。
// 由 bridge 包初始化时调用一次；names 为空时忽略（防止误清空白名单）。
func SetBridgeChannels(names []string) {
	if len(names) == 0 {
		logger.Errorf("[B-5] SetBridgeChannels called with empty names; keep previous whitelist")
		return
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		m[n] = struct{}{}
	}
	if len(m) == 0 {
		return
	}
	bridgeChannels = m
}

var errBridgeOutboundNotReady = errors.New("bridge outbound not ready: InboxIngressService not registered")

var globalInboxIngressService *InboxIngressService

// GlobalSSEPublisher SSE 事件通知回调（由 bridge 包在初始化时设置）。
//
// 解耦：service 包不能 import bridge（避免循环依赖 bridge → service → bridge），
// 故通过全局函数回调通知 bridge 包的 SSEBus.Publish。回调在独立 goroutine 中执行，
// 不阻塞 DeliverBridgeOutbound 主路径。
//
// 参数：
//   - channel, accountID: 渠道和账号标识
//   - hubID: message_hub 表主键（用于 SSE id/Last-Event-ID）
//   - convID: 会话 ID（用于前端路由）
//   - msgType: 消息类型 (text/image/voice etc)
//   - receiverID: 接收者 ID
//   - content: 消息内容
//   - isAIReply: 是否为 AI 回复
//   - createdAt: 创建时间
var GlobalSSEPublisher func(channel, accountID string, hubID uint64, convID, msgType, receiverID, content string, isAIReply bool, createdAt time.Time)

// SetGlobalSSEPublisher 由 bridge 包在初始化时调用，注入 SSE 通知回调
func SetGlobalSSEPublisher(fn func(channel, accountID string, hubID uint64, convID, msgType, receiverID, content string, isAIReply bool, createdAt time.Time)) {
	GlobalSSEPublisher = fn
	logger.Info("[SSE] GlobalSSEPublisher registered successfully")
}

// SetGlobalInboxIngressService 由 router.Setup 在装配完成后调用一次。
func SetGlobalInboxIngressService(s *InboxIngressService) { globalInboxIngressService = s }

// GlobalInboxIngressService 读取桥接入站服务（装配前为 nil）
func GlobalInboxIngressService() *InboxIngressService { return globalInboxIngressService }

func DeliverBridgeOutbound(ctx context.Context, channel, accountID, conversationID, msgType, content, eventID string) error {
	if globalInboxIngressService == nil {
		return errBridgeOutboundNotReady
	}
	if channel == "" || accountID == "" || conversationID == "" {
		return fmt.Errorf("bridge outbound: channel/account_id/conversation_id required")
	}
	if _, ok := bridgeChannels[channel]; !ok {
		return fmt.Errorf("bridge: 不支持的桥接渠道 %q", channel)
	}
	if undeliverable, reason := bridgeOutboundUndeliverable(accountID, conversationID); undeliverable {
		logger.Ctx(ctx).Warn().
			Str("module", "bridge").
			Str("channel", channel).
			Str("account_id", accountID).
			Str("conversation_id", conversationID).
			Str("reason", reason).
			Msg("bridge outbound target undeliverable; rejected before enqueue")
		return fmt.Errorf("bridge outbound undeliverable: %s", reason)
	}
	// 网页私聊渠道（抖音/小红书等）输入框是纯文本：LLM 回复自带的 Markdown
	// 语法符号在入队前剥成纯文本，覆盖 AI 回复/SOP/触达/主动私信全部出站路径。
	if stripMarkdownForBridgeChannel(channel, msgType) {
		content = StripMarkdownForDM(content)
	}
	h := &model.MessageHub{
		MsgID:          ContentHashMsgID(channel, conversationID, content),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        msgType,
		SenderID:       accountID,
		ReceiverID:     conversationID,
		Content:        content,
		ConversationID: conversationID,
		IsAIReply:      false,
		IsRead:         true,
		SentAt:         time.Now(),
	}
	if eventID != "" {
		logger.Ctx(ctx).Debug().
			Str("module", "bridge").
			Str("event_id", eventID).
			Str("msg_id", h.MsgID).
			Msg("bridge outbound enqueue with upstream event_id (for tracing only)")
	}
	if err := globalInboxIngressService.DeliverOutbound(ctx, h); err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Str("module", "bridge").
			Str("channel", channel).
			Str("account_id", accountID).
			Str("conversation_id", conversationID).
			Msg("bridge outbound DeliverOutbound failed")
		return err
	}

	if GlobalSSEPublisher != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[SSE] GlobalSSEPublisher panic recovered: %v", r)
				}
			}()
			GlobalSSEPublisher(
				channel, accountID,
				uint64(h.ID),
				h.ConversationID,
				h.MsgType,
				h.ReceiverID,
				h.Content,
				h.IsAIReply,
				h.CreatedAt,
			)
		}()
	}

	return nil
}

func bridgeOutboundUndeliverable(accountID, conversationID string) (bool, string) {
	if accountID != "" && strings.HasSuffix(accountID, "-unknown") {
		return true, "placeholder-account"
	}
	return false, ""
}

func isBridgeChannel(channel string) bool {
	_, ok := bridgeChannels[channel]
	return ok
}
