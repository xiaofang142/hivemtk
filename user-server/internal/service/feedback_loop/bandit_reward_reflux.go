package feedbackloop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

var refluxSignalSet = map[string]bool{
	string(dtoSignalConversion):   true,
	string(dtoSignalComplaint):    true,
	string(dtoSignalTransfer):     true,
	string(dtoSignalChampionMark): true,
}

// BanditRewardReflux 回流 worker 纯逻辑（可独立单测）
type BanditRewardReflux struct {
	db     *gorm.DB
	repo   *repository.FeedbackLoopRepository
	bandit BanditUpdater
}

// BanditUpdater UpdateReward 最小接口（便于测试注入）
type BanditUpdater interface {
	UpdateReward(ctx context.Context, experimentID, armKey string, success bool, reward float64) error
}

// NewBanditRewardReflux 构造
//
// 参数 db 仅为构造签名兼容保留，内部用 db 构造 repository，不存 gorm 直连写路径
func NewBanditRewardReflux(db *gorm.DB, bandit BanditUpdater) *BanditRewardReflux {
	return &BanditRewardReflux{
		db:     db,
		repo:   repository.NewFeedbackLoopRepositoryWithDB(db),
		bandit: bandit,
	}
}

// RefluxStats 单次回流统计
type RefluxStats struct {
	Scanned  int
	Refluxed int
	Skipped  int
	Duped    int
	Failed   int
}

// RefluxOnce 扫描 (since, until] 窗口内事件并回流。返回统计。
func (r *BanditRewardReflux) RefluxOnce(ctx context.Context, since, until time.Time) (RefluxStats, error) {
	var stats RefluxStats
	if r.repo == nil || r.bandit == nil {
		return stats, fmt.Errorf("reflux not initialized")
	}

	events, err := r.repo.ListRewardRefluxEvents(ctx, since, until)
	if err != nil {
		return stats, fmt.Errorf("load events: %w", err)
	}

	for _, ev := range events {
		if !refluxSignalSet[ev.SignalKey] {
			continue
		}
		stats.Scanned++

		cnt, err := r.repo.CountRefluxLogsByEventID(ctx, ev.EventID)
		if err != nil {
			stats.Failed++
			continue
		}
		if cnt > 0 {
			stats.Duped++
			continue
		}

		target := r.resolveArm(ctx, ev)
		if target == nil {
			stats.Skipped++
			continue
		}

		success := ev.Reward > 0
		reward := ev.Reward
		if reward < 0 {
			reward = -reward
		}
		if err := r.bandit.UpdateReward(ctx, target.ExperimentID, target.ArmKey, success, reward); err != nil {
			stats.Failed++
			continue
		}

		log := model.BanditRefluxLog{
			EventID:      ev.EventID,
			ExperimentID: target.ExperimentID,
			ArmKey:       target.ArmKey,
			SignalKey:    ev.SignalKey,
			SessionID:    ev.SessionID,
			SOPID:        ev.SOPID,
			Reward:       reward,
			Success:      success,
		}
		if err := r.repo.CreateRefluxLog(ctx, &log); err != nil {

			stats.Failed++
			continue
		}
		if !success || ev.SignalKey == string(dtoSignalConversion) {

			stats.Duped++
		}
		stats.Refluxed++
	}
	return stats, nil
}

type refluxTarget struct {
	ExperimentID string
	ArmKey       string
}

func (r *BanditRewardReflux) resolveArm(ctx context.Context, ev model.FeedbackEvent) *refluxTarget {

	if ev.PromptCandidateID > 0 {

		arm, err := r.repo.ResolveArmForPromptCandidate(ctx, ev.PromptCandidateID)
		if err == nil {
			return &refluxTarget{ExperimentID: arm.ExperimentID, ArmKey: arm.ArmKey}
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}

	}

	if ev.SOPID > 0 {
		tests, err := r.repo.ListRunningABTestsBySOP(ctx, ev.SOPID)
		if err != nil || len(tests) == 0 {
			return nil
		}

		arm, err := r.repo.GetBanditArmByExperimentAndSOP(ctx, tests[0].ExperimentID, ev.SOPID)
		if err != nil {
			return nil
		}
		return &refluxTarget{ExperimentID: arm.ExperimentID, ArmKey: arm.ArmKey}
	}
	return nil
}

type dtoSignalKey string

const (
	dtoSignalConversion   dtoSignalKey = "conversion"
	dtoSignalComplaint    dtoSignalKey = "complaint"
	dtoSignalTransfer     dtoSignalKey = "transfer"
	dtoSignalChampionMark dtoSignalKey = "champion_mark"
)
