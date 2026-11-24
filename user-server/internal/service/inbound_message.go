package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ErrInboundMessageInvalid 归一化消息缺少必要字段（事件 ID / 来源）。
var ErrInboundMessageInvalid = errors.New("入站消息缺少事件 ID 或来源")

// InboundMessageService 统一入站投递口（导出 interface + 小写 struct 实现口径）。
// 三条入口（bridge HTTP / channelgw WS / webhook）只做归一化投递；自他识别统一走
// ChannelResolver，线索/自动回复触发由既有 InboxIngressService 负责，避免重复触发。
type InboundMessageService interface {
	Deliver(ctx context.Context, msg *model.InboundMessage) error
}

// InboundDispatcher 是 InboundMessageService 的语义别名（投递口口径），装配点用。
type InboundDispatcher = InboundMessageService

type inboundMessageService struct {
	resolver *ChannelResolver
}

// NewInboundMessageService 创建归一化投递服务。
func NewInboundMessageService() InboundDispatcher {
	return &inboundMessageService{}
}

// SetChannelResolver 注入渠道解析器（装配点/测试用）：投递时按识别码补全租户/账号归属。
func (s *inboundMessageService) SetChannelResolver(r *ChannelResolver) {
	if s != nil && r != nil {
		s.resolver = r
	}
}

// AttachInboundResolver 给归一化投递器装配渠道解析器（router 装配点用）。
// 走类型断言而非扩接口，避免破坏仓内多个 InboundDispatcher fake 实现。
func AttachInboundResolver(d InboundDispatcher, r *ChannelResolver) bool {
	impl, ok := d.(*inboundMessageService)
	if !ok || r == nil {
		return false
	}
	impl.SetChannelResolver(r)
	return true
}

// Deliver 校验信封后用 ChannelResolver 补全识别码/租户归属，再留痕。
//
// 刻意不在这里触发任何线索/自动回复业务：真实落库仍由既有 inbox/message_hub 负责，
// 本服务只做三路归一化收口（FR-031），避免重复收件箱与重复触发。
// 解析失败不丢消息：留 warn 日志，由既有入口链路按原口径处理。
func (s *inboundMessageService) Deliver(ctx context.Context, msg *model.InboundMessage) error {
	if msg == nil || msg.EventID == "" || msg.Source == "" {
		return ErrInboundMessageInvalid
	}
	if s != nil && s.resolver != nil {
		code := strings.TrimSpace(msg.AccountRef)
		if code == "" {
			code = strings.TrimSpace(msg.IdentifyCode)
		}
		if code != "" {
			if resolved, err := s.resolver.ResolveByIdentifyCode(ctx, code); err == nil && resolved != nil {
				msg.IdentifyCode = resolved.IdentifyCode
				if msg.TenantID == 0 {
					msg.TenantID = resolved.TenantID
				}
			} else {
				logger.Ctx(ctx).Warn().Err(err).Str("module", "inbound").
					Str("event", "inbound_identify_unresolved").Str("code", code).
					Msg("[Inbound] 识别码解析失败，按既有入口链路处理")
			}
		}
	}
	logger.Ctx(ctx).Debug().
		Str("module", "inbound").
		Str("event", "inbound_normalized").
		Str("source", msg.Source).
		Str("channel", msg.Channel).
		Str("identify_code", msg.IdentifyCode).
		Str("event_id", msg.EventID).
		Msg("[Inbound] 三路入口归一化收口（统一投递到既有收件箱链路）")
	return nil
}
