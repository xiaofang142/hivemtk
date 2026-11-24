package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type BusinessAccountAuthService struct {
	repo repository.BusinessAccountAuthRepository
}

func NewBusinessAccountAuthService() *BusinessAccountAuthService {
	return &BusinessAccountAuthService{repo: repository.NewBusinessAccountAuthRepository()}
}

func (s *BusinessAccountAuthService) Upsert(ctx context.Context, a *model.BusinessAccountAuthorization) error {
	return s.repo.Upsert(ctx, a)
}
func (s *BusinessAccountAuthService) ListByUser(ctx context.Context, userID uint) ([]model.BusinessAccountAuthorization, error) {
	return s.repo.ListByUser(ctx, userID)
}
func (s *BusinessAccountAuthService) ListByTenant(ctx context.Context) ([]model.BusinessAccountAuthorization, error) {
	return s.repo.ListByTenant(ctx)
}
func (s *BusinessAccountAuthService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}
func (s *BusinessAccountAuthService) IsAuthorized(ctx context.Context, userID uint, channel, accountID string) (bool, error) {
	return s.repo.IsAuthorized(ctx, userID, channel, accountID)
}
