package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// CH-09 真实归档适配器：把 ChannelInboundOrchestrator 的 InboundArchiveRepository
// 端口接到既有 MessageHub 仓储，不新建第二个收件箱模型。
//
// 刻意只声明 narrow store 口（repository.MessageHubRepository 隐式实现），
// Service 层不持有 *gorm.DB；查重/乱序/落库全部经仓储方法。
type channelArchiveStore interface {
	GetByMsgID(ctx context.Context, msgID string) (*model.MessageHub, error)
	GetLastInboundByConversation(ctx context.Context, conversationID string) (*model.MessageHub, error)
	Create(ctx context.Context, hub *model.MessageHub) error
}

type messageHubInboundArchive struct {
	store channelArchiveStore
}

// NewProductionInboundArchive 生产归档器（全局仓储装配）。
func NewProductionInboundArchive() InboundArchiveRepository {
	return NewInboundArchiveWithStore(repository.NewMessageHubRepository())
}

// NewInboundArchiveWithStore 显式仓储构造（装配点/测试用）。
func NewInboundArchiveWithStore(store channelArchiveStore) InboundArchiveRepository {
	return &messageHubInboundArchive{store: store}
}

func (a *messageHubInboundArchive) Available() bool {
	return a != nil && a.store != nil
}

// scopedCtx 强制带上事件自带租户：多租户模式下缺租户的仓储调用由隔离 callback fail-closed，
// 绝不用「公共池」读到别的租户行（CH-07/09 同一身份口径）。
func (a *messageHubInboundArchive) scopedCtx(ctx context.Context, tenantID uint) context.Context {
	return tenant.WithTenantID(ctx, tenantID)
}

func (a *messageHubInboundArchive) Exists(ctx context.Context, tenantID uint, eventID string) (bool, error) {
	if !a.Available() || eventID == "" {
		return false, repository.ErrChannelStoreUnavailable
	}
	hub, err := a.store.GetByMsgID(a.scopedCtx(ctx, tenantID), eventID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return hub != nil, nil
}

func (a *messageHubInboundArchive) LastTimestamp(ctx context.Context, tenantID uint, conversationID string) (int64, error) {
	if !a.Available() {
		return 0, repository.ErrChannelStoreUnavailable
	}
	if conversationID == "" {
		return 0, nil
	}
	hub, err := a.store.GetLastInboundByConversation(a.scopedCtx(ctx, tenantID), conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if hub == nil {
		return 0, nil
	}
	return hub.SentAt.UnixMilli(), nil
}

func (a *messageHubInboundArchive) Archive(ctx context.Context, hub *model.MessageHub) error {
	if !a.Available() || hub == nil {
		return repository.ErrChannelStoreUnavailable
	}
	return a.store.Create(a.scopedCtx(ctx, hub.TenantID), hub)
}

// InboundBusinessTriggerFunc 让业务触发以函数接缝注入：既有 InboxIngressService 的
// 双 Agent 调度由装配层包装成此函数，编排器不反向依赖 ingress 包（避免改其文件）。
type InboundBusinessTriggerFunc func(ctx context.Context, msg *model.MessageHub, identity *ChannelIdentity) (string, error)

func (f InboundBusinessTriggerFunc) Trigger(ctx context.Context, msg *model.MessageHub, identity *ChannelIdentity) (string, error) {
	if f == nil {
		return "not_started", nil
	}
	return f(ctx, msg, identity)
}

// NewProductionInboundOrchestrator 生产装配口：真实归档 + 统一自他识别 + 可注入触发。
// trigger 为 nil 时仅归档不触发业务——入口迁移期保持既有 ingress 单独触发，
// 不发生双写双触发；待各协议入口切换到本编排器时再注入触发接缝。
func NewProductionInboundOrchestrator(trigger InboundBusinessTrigger) *ChannelInboundOrchestrator {
	return NewChannelInboundOrchestrator(
		NewProductionInboundArchive(),
		NewChannelIdentityResolver(NewChannelResolver()),
		trigger,
	)
}
