package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
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
//   - ctx: **必须携带租户归属**。SSE 推送前的"出站认领"（ClaimOutboundForPush）
//     要 UPDATE message_hub 这张租户表，on 模式下 ctx 不带租户即被隔离守卫
//     fail-closed 拒绝 ⇒ 现场是 "[SSE] 出站认领报错，放弃本次推送（行仍待办，
//     超时后重投）"——推送看着通了，其实每条都靠超时重投兜着。
//     调用方在起 goroutine 前用 tenant.BackgroundWithTenant(ctx) 派生
//     （保留租户归属、丢掉请求的取消信号），见本仓既有用法
//     （account_domain.go / customer_session.go）。
//   - channel, accountID: 渠道和账号标识
//   - hubID: message_hub 表主键（用于 SSE id/Last-Event-ID）
//   - convID: 会话 ID（用于前端路由）
//   - msgType: 消息类型 (text/image/voice etc)
//   - receiverID: 接收者 ID
//   - content: 消息内容
//   - isAIReply: 是否为 AI 回复
//   - createdAt: 创建时间
var GlobalSSEPublisher func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time)

// SetGlobalSSEPublisher 由 bridge 包在初始化时调用，注入 SSE 通知回调
func SetGlobalSSEPublisher(fn func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time)) {
	GlobalSSEPublisher = fn
	logger.Info("[SSE] GlobalSSEPublisher registered successfully")
}

// SetGlobalInboxIngressService 由 router.Setup 在装配完成后调用一次。
func SetGlobalInboxIngressService(s *InboxIngressService) { globalInboxIngressService = s }

// GlobalInboxIngressService 读取桥接入站服务（装配前为 nil）
func GlobalInboxIngressService() *InboxIngressService { return globalInboxIngressService }

// DeliverBridgeOutbound 出站入队（旧签名，等价于 DeliverBridgeOutboundWithReceipt 丢回执）。
func DeliverBridgeOutbound(ctx context.Context, channel, accountID, conversationID, msgType, content, eventID string) error {
	_, _, err := DeliverBridgeOutboundWithReceipt(ctx, channel, accountID, conversationID, msgType, content, eventID)
	return err
}

// DeliverBridgeOutboundWithReceipt 出站入队 + 回执（D-9，2026-10-23）。
// 返回：hubID = message_hub 行号；msgID = hub 行 MsgID（bridge 渠道的回执键，
// 发送时是占位内容哈希，平台号回写后由 UpdateMsgID 覆盖）。调用方应把这对值
// 透给上层响应（ProactiveReachResponse.HubID / MessageID），操作者才有可反查的凭据——
// Recall（IntegrationReachAdapter.Recall）正是按 msg_id 反查 hub 行的。
func DeliverBridgeOutboundWithReceipt(ctx context.Context, channel, accountID, conversationID, msgType, content, eventID string) (hubID uint64, msgID string, err error) {
	return DeliverBridgeOutboundMedia(ctx, channel, accountID, conversationID, msgType, content, "", eventID)
}

// DeliverBridgeOutboundMedia 带媒体出站入队（002-multi-type-messages 下行通路的服务端生产者出口）。
//
// 2026-11-10 审计 F6：下行媒体协议三端早已就绪（OutboxMessage.MediaURL、SSE media_url、
// 扩展 sendOutbound 的 msgType/mediaUrl 分派），但服务端所有 AI/触达/重放入口都是
// text-only 入队——扩展端永远收不到带 media_url 的下行行。本函数补上生产者出口：
//   - msgType 非 text 且 mediaURL 非空时，msg_id 用 typed 口径（ContentHashMsgIDTyped，
//     与扩展 typedContentHash 同构）：outbound 行的 content 也可能是「[图片]」占位符，
//     旧口径会让同会话两张图的 pending 行 msg_id 碰撞（ack 误翻、去重互吞）；
//   - 文本消息（msgType 空/text 或 mediaURL 空）与旧口径逐字节一致；
//   - mediaURL 统一 TruncateMediaURL（F2 列宽收口）。
func DeliverBridgeOutboundMedia(ctx context.Context, channel, accountID, conversationID, msgType, content, mediaURL, eventID string) (hubID uint64, msgID string, err error) {
	if globalInboxIngressService == nil {
		return 0, "", errBridgeOutboundNotReady
	}
	if channel == "" || accountID == "" || conversationID == "" {
		return 0, "", fmt.Errorf("bridge outbound: channel/account_id/conversation_id required")
	}
	if _, ok := bridgeChannels[channel]; !ok {
		return 0, "", fmt.Errorf("bridge: 不支持的桥接渠道 %q", channel)
	}
	if undeliverable, reason := bridgeOutboundUndeliverable(accountID, conversationID); undeliverable {
		logger.Ctx(ctx).Warn().
			Str("module", "bridge").
			Str("channel", channel).
			Str("account_id", accountID).
			Str("conversation_id", conversationID).
			Str("reason", reason).
			Msg("bridge outbound target undeliverable; rejected before enqueue")
		return 0, "", fmt.Errorf("bridge outbound undeliverable: %s", reason)
	}
	// 网页私聊渠道（抖音/小红书等）输入框是纯文本：LLM 回复自带的 Markdown
	// 语法符号在入队前剥成纯文本，覆盖 AI 回复/SOP/触达/主动私信全部出站路径。
	if stripMarkdownForBridgeChannel(channel, msgType) {
		content = StripMarkdownForDM(content)
	}
	mediaURL = model.TruncateMediaURL(strings.TrimSpace(mediaURL))
	if msgType == "" || strings.EqualFold(strings.TrimSpace(msgType), "text") {
		mediaURL = ""
	}
	// msgID 是命名返回值（回执键），此处用 = 赋值而非 :=。
	msgID = ContentHashMsgIDTyped(channel, conversationID, msgType, mediaURL, content)
	h := &model.MessageHub{
		MsgID:          msgID,
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        msgType,
		SenderID:       accountID,
		ReceiverID:     conversationID,
		Content:        content,
		MediaURL:       mediaURL,
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
		return 0, "", err
	}

	if GlobalSSEPublisher != nil {
		// 进协程前把包级注入点快照成本地（check-async-global-read.py 同口径，reach 族
		// 4176e599 同款）：体内直读全局会与测试 Cleanup 的 SetGlobalSSEPublisher 写回
		// 构成 DATA RACE（-race 实证），生产语义不变（该 seam 运行期从不改）。
		pub := GlobalSSEPublisher
		// 派生带租户的 ctx：认领那一步要 UPDATE message_hub（租户表），
		// on 模式下 ctx 不带租户会被隔离守卫 fail-closed 拒绝。
		// BackgroundWithTenant 保留租户归属、丢掉请求的取消信号（请求早结束、
		// 推送还没发出去时不能把推送一起取消）。
		pubCtx := tenant.BackgroundWithTenant(ctx)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[SSE] GlobalSSEPublisher panic recovered: %v", r)
				}
			}()
			pub(
				pubCtx,
				channel, accountID,
				uint64(h.ID),
				h.ConversationID,
				h.MsgType,
				h.ReceiverID,
				h.Content,
				h.MediaURL,
				h.IsAIReply,
				h.CreatedAt,
			)
		}()
	}

	return uint64(h.ID), h.MsgID, nil
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
