package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type LeadMiningNotifyRouteService struct {
	repo *repository.LeadMiningNotifyRouteRepository
}

func NewLeadMiningNotifyRouteService() *LeadMiningNotifyRouteService {
	return &LeadMiningNotifyRouteService{repo: repository.NewLeadMiningNotifyRouteRepository()}
}

func (s *LeadMiningNotifyRouteService) Create(ctx context.Context, m *model.LeadMiningNotifyRoute) error {
	if m.RouteID == "" { m.RouteID = fmt.Sprintf("lnr_%d", time.Now().UnixNano()) }
	return s.repo.Create(ctx, m)
}

func (s *LeadMiningNotifyRouteService) List(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	return s.repo.List(ctx, channel, accountID)
}

func (s *LeadMiningNotifyRouteService) Delete(ctx context.Context, routeID string) error {
	return s.repo.Delete(ctx, routeID)
}

func (s *LeadMiningNotifyRouteService) ListEnabledByAccount(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	return s.repo.ListEnabledByAccount(ctx, channel, accountID)
}
