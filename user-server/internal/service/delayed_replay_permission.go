package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type delayedReplayPermission struct {
	sessions *repository.CustomerSessionRepository
	messages *repository.MessageHubRepository
	ingress  *InboxIngressService
}

func newDelayedReplayPermission(repo *repository.DelayedOutboundRepository, ingress *InboxIngressService) delayedReplayPermission {
	sessions, messages := repo.ReplayRepositories()
	return delayedReplayPermission{sessions: sessions, messages: messages, ingress: ingress}
}

// confirm 返回真实会话及旧答案作废事实；未知状态不能被当作允许投递。
func (p delayedReplayPermission) confirm(ctx context.Context, rec *model.DelayedOutboundReply) (*model.CustomerSession, bool, error) {
	if ctx == nil || tenant.IsSystemScope(ctx) || rec == nil || p.ingress == nil {
		return nil, false, errors.New("delayed replay permission unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 || tenantID != rec.TenantID {
		return nil, false, errors.New("delayed replay tenant unconfirmed")
	}
	session, err := p.sessions.ReplaySession(ctx, rec.Platform, rec.AccountID, rec.SenderID)
	if err != nil {
		return nil, false, err
	}
	if session.HandlerType != model.HandlerTypeAI || session.DNCBlocked {
		return nil, false, errors.New("delayed replay session not AI controlled")
	}
	locked, err := p.ingress.ConfirmHumanLockBeforeSend(ctx, session.SessionID)
	if err != nil || locked {
		return nil, false, errors.New("delayed replay human state unconfirmed or held")
	}
	answered, err := p.messages.HasDeliveredReplySince(ctx, rec.Platform, rec.AccountID, rec.ConversationID, rec.CreatedAt)
	if err != nil {
		return nil, false, err
	}
	return session, answered, ctx.Err()
}
