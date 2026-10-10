package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// LeadMiningConfigRepository 线索发掘配置仓储（单例行）
type LeadMiningConfigRepository interface {
	GetSingleton(ctx context.Context) (*model.LeadMiningConfig, error)
	Save(ctx context.Context, cfg *model.LeadMiningConfig) error
}

type leadMiningConfigRepo struct {
	db *gorm.DB
}

func NewLeadMiningConfigRepository() LeadMiningConfigRepository {
	return &leadMiningConfigRepo{db: _db.GetDB()}
}

func (r *leadMiningConfigRepo) GetSingleton(ctx context.Context) (*model.LeadMiningConfig, error) {
	var c model.LeadMiningConfig
	err := r.db.WithContext(ctx).First(&c, "id = ?", 1).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.LeadMiningConfig{ID: 1, MinIntentScore: 50}, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *leadMiningConfigRepo) Save(ctx context.Context, cfg *model.LeadMiningConfig) error {
	if cfg == nil {
		return errors.New("配置为空")
	}
	cfg.ID = 1
	var existing model.LeadMiningConfig
	err := r.db.WithContext(ctx).First(&existing, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(cfg).Error
	}
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.LeadMiningConfig{}).Where("id = ?", 1).Updates(map[string]any{
		"enabled":          cfg.Enabled,
		"keywords":         cfg.Keywords,
		"tags":             cfg.Tags,
		"requirement":      cfg.Requirement,
		"channels":         cfg.Channels,
		"min_intent_score": cfg.MinIntentScore,
		"model":            cfg.Model,
		"updated_at":       time.Now().Unix(),
	}).Error
}
