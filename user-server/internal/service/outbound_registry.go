package service

import (
	"context"
	"sort"
	"sync"

	"hivemtk-user/internal/model"
)

// OutboundRequest 是一条出站投递的全部输入。
//
// 设计要点（见 specs/024-im-seam-alignment D1）：
//   - Fail / FailUntracked 是 sendOutbound 里两个失败闭包的原件，由调用方注入、sender 在现场调用。
//     失败口径不是返回值而是副作用（写 sendErr + 落失败轨迹/告警），返回 error 会造出第二条真值来源；
//     飞书「正文失败仍继续发卡片」的「先记失败、后补成功」顺序也只有现场回调能保持。
//   - CardsFallbackCount 供 bridge sender 记进 Extra，说明「原本有几张卡被降级成文字」。
//   - Content 是清洗后的最终文本（bridge 族的 Markdown 清洗在 sendOutbound 横切段已完成），
//     这样失败重试入队拿到的也是清洗后文本，与搬迁前行为一致。
type OutboundRequest struct {
	Channel            WebhookChannel
	AccountID          string
	Payload            *ParsedPayload
	Content            string
	Cards              []model.RichCard
	Hub                *model.MessageHub
	CardsFallbackCount int

	Fail          func(error) // 带会话轨迹的失败（写终态失败行 / 发布授权告警）
	FailUntracked func(error) // 无可归属轨迹行时的失败（只留结构化日志）
}

// OutboundResult 是 sender 的投递事实：Sent 表示至少有一跳成功（不等于客户已收到）。
type OutboundResult struct {
	Sent          bool
	PlatformMsgID string
}

// channelSender 一个渠道一个实现；Send 只能通过 req.Fail / req.FailUntracked 报告失败。
type channelSender interface {
	Channel() WebhookChannel
	Send(ctx context.Context, req *OutboundRequest) *OutboundResult
}

// OutboundSenderFactory 构造某渠道的 sender；svc 用于取 integration/repo（不引入新单例）。
type OutboundSenderFactory func(svc *WebhookService) channelSender

var (
	outboundSenderFactoriesMu sync.RWMutex
	outboundSenderFactories   = map[WebhookChannel]OutboundSenderFactory{}
)

// RegisterChannelSender 登记一个渠道 sender 工厂。空渠道或重复登记直接 panic——
// 重复登记意味着两个实现争同一渠道，静默覆盖会让线上出站行为取决于 init 顺序。
func RegisterChannelSender(ch WebhookChannel, f OutboundSenderFactory) {
	if ch == "" {
		panic("service: RegisterChannelSender called with empty channel")
	}
	if f == nil {
		panic("service: RegisterChannelSender called with nil factory for channel " + string(ch))
	}
	outboundSenderFactoriesMu.Lock()
	defer outboundSenderFactoriesMu.Unlock()
	if _, exists := outboundSenderFactories[ch]; exists {
		panic("service: duplicate outbound sender registration for channel " + string(ch))
	}
	outboundSenderFactories[ch] = f
}

// RegisteredSenderChannels 返回已登记 sender 的渠道（升序），供文档与 parity 测试使用。
func RegisteredSenderChannels() []WebhookChannel {
	outboundSenderFactoriesMu.RLock()
	defer outboundSenderFactoriesMu.RUnlock()
	channels := make([]WebhookChannel, 0, len(outboundSenderFactories))
	for ch := range outboundSenderFactories {
		channels = append(channels, ch)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i] < channels[j] })
	return channels
}

func outboundSenderFactoryOf(ch WebhookChannel) (OutboundSenderFactory, bool) {
	outboundSenderFactoriesMu.RLock()
	defer outboundSenderFactoriesMu.RUnlock()
	f, ok := outboundSenderFactories[ch]
	return f, ok
}

// isBridgeOutboundChannel 是出站链路判断 bridge 能力的唯一 registry-owned 入口。
func isBridgeOutboundChannel(ch WebhookChannel) bool {
	return bridgeOutboxCapable(ch)
}

// resolveOutboundAccount 只负责把账号解析委托给 service；registry 文件不接触 DB/GORM。
func resolveOutboundAccount(ctx context.Context, tenantID uint, ch WebhookChannel, accountID string) (string, error) {
	return NewChannelDispatchService().ResolveExplicitOutboundAccount(ctx, tenantID, string(ch), accountID)
}

// 桥接集合仍由同一注册表查询驱动；提交裁决须在勿扰/重试队列前执行。
func authorizeOutboundSubmission(ctx context.Context, ch WebhookChannel, accountID string, hub *model.MessageHub) error {
	if isBridgeOutboundChannel(ch) {
		return authorizeBridgeReplySubmit(ctx, ch, accountID, hub)
	}
	return nil
}

// outboundSenderFor 取某渠道的 sender 实例（每 service 一份，惰性构造）。
//
// 桥接族（channelgw registry OutboundPath=bridge_outbox）不逐个登记渠道常量，
// 统一由 bridgeOutboxSender 承接；这条回退只对 bridgeOutboxCapable 成立的渠道生效，
// 其余未知渠道返回 false，由 sendOutbound 复现原来的「drift 渠道明确失败」。
func (s *WebhookService) outboundSenderFor(ch WebhookChannel) (channelSender, bool) {
	s.sendersMu.Lock()
	defer s.sendersMu.Unlock()
	if snd, ok := s.senders[ch]; ok {
		return snd, true
	}
	f, ok := outboundSenderFactoryOf(ch)
	if !ok {
		if !isBridgeOutboundChannel(ch) {
			return nil, false
		}
		snd := &bridgeOutboxSender{svc: s}
		s.ensureSendersLocked()
		s.senders[ch] = snd
		return snd, true
	}
	snd := f(s)
	if snd == nil {
		return nil, false
	}
	s.ensureSendersLocked()
	s.senders[ch] = snd
	return snd, true
}

// ensureSendersLocked 惰性初始化缓存表（调用方已持锁）。
func (s *WebhookService) ensureSendersLocked() {
	if s.senders == nil {
		s.senders = make(map[WebhookChannel]channelSender)
	}
}
