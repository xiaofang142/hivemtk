package router

import (
	"context"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

// operationLogSink 审计 sink：经 OperationLogService 落库（Router 不直连 Repository）
type operationLogSink struct {
	svc *service.OperationLogService
}

func (s operationLogSink) Save(ctx context.Context, entry *middleware.AuditEntry) error {
	return s.svc.Create(ctx, &model.OperationLog{
		UserID:     entry.UserID,
		Username:   entry.Username,
		Action:     entry.Action,
		Module:     entry.Module,
		Resource:   entry.Resource,
		ResourceID: entry.ResourceID,
		Detail:     entry.Detail,
		IP:         entry.IP,
		UserAgent:  entry.UserAgent,
	})
}

type chatChannelResolver struct {
	svc *service.ChatChannelService
}

func (r chatChannelResolver) ResolveByAppKey(ctx context.Context, appKey string) (*middleware.ChatChannelView, error) {
	channel, err := r.svc.GetByAppKey(ctx, appKey)
	if err != nil {
		return nil, err
	}
	return toChatChannelView(channel), nil
}

func (r chatChannelResolver) ResolveByChannelID(ctx context.Context, channelID string) (*middleware.ChatChannelView, error) {
	channel, err := r.svc.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return toChatChannelView(channel), nil
}

func toChatChannelView(c *model.ChatChannel) *middleware.ChatChannelView {
	return &middleware.ChatChannelView{
		ChannelID:      c.ChannelID,
		ChannelName:    c.ChannelName,
		Status:         string(c.Status),
		Active:         service.ChatChannelIsActive(c),
		AllowedOrigins: service.ChatChannelAllowedOriginsList(c),
	}
}

func injectMiddlewarePorts() {
	middleware.SetPermChecker(service.NewPermissionService())
	middleware.SetAuditSink(operationLogSink{svc: service.NewOperationLogService()})
}
