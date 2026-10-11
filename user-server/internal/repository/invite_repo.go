package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// invite_repo.go 邀请码仓储（SaaS P2 准入）。

type InviteCodeRepo struct{ db *gorm.DB }

func NewInviteCodeRepo() *InviteCodeRepo { return &InviteCodeRepo{db: db.GetDB()} }

func (r *InviteCodeRepo) Available() bool { return r != nil && r.db != nil }

// Generate 生成 12hex 邀请码并入账。
func (r *InviteCodeRepo) Generate(ctx context.Context, teamID uint, maxUses int, expiresAt *time.Time, createdBy *uint) (*model.InviteCode, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	row := &model.InviteCode{
		Code: hex.EncodeToString(b), TeamID: teamID, MaxUses: maxUses,
		ExpiresAt: expiresAt, Status: model.InviteStatusActive, CreatedBy: createdBy,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (r *InviteCodeRepo) ListByTeam(ctx context.Context, teamID uint) ([]model.InviteCode, error) {
	var rows []model.InviteCode
	err := r.db.WithContext(ctx).Where("team_id = ?", teamID).Order("id DESC").Limit(100).Find(&rows).Error
	return rows, err
}

func (r *InviteCodeRepo) GetByCode(ctx context.Context, code string) (*model.InviteCode, error) {
	var row model.InviteCode
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Consume 原子消耗一次使用次数（WHERE 带可用性三判，受影响行数=0 即不可用/已被抢完）。
// Disable 停用（幂等）。
func (r *InviteCodeRepo) Disable(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.InviteCode{}).Where("id = ?", id).
		Update("status", model.InviteStatusDisabled).Error
}

func (r *InviteCodeRepo) Consume(ctx context.Context, code string) (*model.InviteCode, error) {
	row, err := r.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	res := r.db.WithContext(ctx).Model(&model.InviteCode{}).
		Where("id = ? AND status = ? AND (max_uses = 0 OR used_count < max_uses) AND (expires_at IS NULL OR expires_at > ?)",
			row.ID, model.InviteStatusActive, time.Now()).
		Update("used_count", row.UsedCount+1)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errorsInviteUsed
	}
	return row, nil
}

var errorsInviteUsed = errInviteUsed{}

type errInviteUsed struct{}

func (errInviteUsed) Error() string { return "邀请码无效或已用完" }
