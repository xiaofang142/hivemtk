package egolite

// EgoGovernanceService 治理接缝（2026-11-05 二次审计）：
// 产物配额 / 会话审计导出 / 观测留存清理原先由 router 直接传 *gorm.DB 调用，
// 现收进服务层；controller 不持有数据库句柄。

import (
	"context"

	"gorm.io/gorm"
)

type EgoGovernanceService struct {
	db *gorm.DB
}

func NewEgoGovernanceServiceWithDB(db *gorm.DB) *EgoGovernanceService {
	return &EgoGovernanceService{db: db}
}

// CheckArtifactQuota 产物日配额（T21）。
func (s *EgoGovernanceService) CheckArtifactQuota(ctx context.Context, userID uint) error {
	return CheckArtifactQuota(ctx, s.db, userID)
}

func (s *EgoGovernanceService) ExportSession(ctx context.Context, userID, sessionID uint, isAdmin bool) (*SessionExport, error) {
	return ExportSession(ctx, s.db, userID, sessionID, isAdmin)
}

func (s *EgoGovernanceService) ExportSessionCSV(ctx context.Context, userID, sessionID uint, isAdmin bool) ([]byte, error) {
	return ExportSessionCSV(ctx, s.db, userID, sessionID, isAdmin)
}

// SweepRetention 观测留存清理（窗口 ≤0 = 永不清理，由 ObserveRetentionDays 判定）。
func (s *EgoGovernanceService) SweepRetention(ctx context.Context) (int64, error) {
	return RetentionSweep(ctx, s.db, ObserveRetentionDays(ctx, s.db))
}
