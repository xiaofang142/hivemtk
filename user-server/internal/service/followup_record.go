package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type FollowupRecordService struct {
	repo repository.FollowupRecordRepository
}

func NewFollowupRecordService() *FollowupRecordService {
	return &FollowupRecordService{repo: repository.NewFollowupRecordRepository()}
}

func (s *FollowupRecordService) Create(ctx context.Context, rec *model.FollowupRecord) error {
	if rec.RecordID == "" {
		rec.RecordID = fmt.Sprintf("fr_%d", time.Now().UnixNano())
	}
	return s.repo.Create(ctx, rec)
}

func (s *FollowupRecordService) Update(ctx context.Context, rec *model.FollowupRecord) error {
	return s.repo.Update(ctx, rec)
}

func (s *FollowupRecordService) Get(ctx context.Context, recordID string) (*model.FollowupRecord, error) {
	return s.repo.GetByRecordID(ctx, recordID)
}

func (s *FollowupRecordService) List(ctx context.Context, customerID, reminderID string, ownerID uint, page, pageSize int) ([]model.FollowupRecord, int64, error) {
	return s.repo.List(ctx, customerID, reminderID, ownerID, page, pageSize)
}
