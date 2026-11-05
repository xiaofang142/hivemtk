package egolite

// EgoTelemetryService 遥测接缝（2026-11-05 二次审计）：controller 不再直接持有遥测仓储。

import "context"

type EgoTelemetryService struct {
	repo EgoTelemetryRepository
}

func NewEgoTelemetryService(repo EgoTelemetryRepository) *EgoTelemetryService {
	return &EgoTelemetryService{repo: repo}
}

func (s *EgoTelemetryService) Ingest(ctx context.Context, userID uint, rawEvents []map[string]any) (accepted, droppedKeys, truncated int, err error) {
	return IngestTelemetry(ctx, userID, s.repo, rawEvents)
}

func (s *EgoTelemetryService) Summary(ctx context.Context, hours int) (buckets []TelemetryBucket, total int, truncated bool, err error) {
	return TelemetrySummary(ctx, s.repo, hours)
}
