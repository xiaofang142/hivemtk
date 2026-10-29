package egolite

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// quota.go —— T21 成本与配额（M3 治理：市场/多租户前置）。
//
// 遥测回答"花了多少"，配额回答"还能不能花"。三道闸（全部按**自然日**滚动）：
//   - 会话数：单用户当日新开会话 ≤ SessionQuotaPerDay；
//   - 轮次数：单用户当日派发轮次 ≤ RoundQuotaPerDay；
//   - 截图数：单用户当日上传产物 ≤ ArtifactQuotaPerDay。
// 超限统一 ErrQuotaExceeded（路由层映射 429），错误文案带"哪个维度超了"——
// 可操作拒绝码纪律：用户要能知道是今天别跑了还是等下一轮。
//
// 为什么按日而不是按会话：会话内轮次已有 max_rounds/max_actions 预算
// （Policy T18），日配额防的是"开很多个会话"的放大面。并发闸（≤2 在途）
// 在 session_service，与这里正交——并发限"同时"，配额限"总量"。

var ErrQuotaExceeded = errors.New("egolite: 当日配额已用尽")

// 配额缺省（保守起步：单用户单日）。硬编码而非 config_params——
// 首版口径要可被测试钉死；放开到参数中心是后续轮的事（届时测试改读参数）。
const (
	SessionQuotaPerDay = 20
	RoundQuotaPerDay   = 200
	ArtifactQuotaPerDay = 100
)

// quotaUsage 当日已用量（会话数按 opened，轮次按 created，产物按 created）。
type quotaUsage struct {
	Sessions  int64
	Rounds    int64
	Artifacts int64
}

func dayStartUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// currentUsage 统计单用户当日三维度用量。
func currentUsage(ctx context.Context, db *gorm.DB, userID uint) (quotaUsage, error) {
	since := dayStartUTC()
	var u quotaUsage
	if err := db.WithContext(ctx).Model(&model.EgoliteSession{}).
		Where("user_id = ? AND created_at >= ?", userID, since).
		Count(&u.Sessions).Error; err != nil {
		return u, err
	}
	if err := db.WithContext(ctx).Model(&model.EgoliteRound{}).
		Where("user_id = ? AND created_at >= ?", userID, since).
		Count(&u.Rounds).Error; err != nil {
		return u, err
	}
	// 产物计数：截图片段挂在事件行 artifact 类型上——事件行是唯一逐条账本。
	// ego_event_log 没有 user_id 列（execution 归属），按执行归属人 join 统计。
	if err := db.WithContext(ctx).Model(&model.EgoEventLog{}).
		Joins("JOIN egolite_executions ON egolite_executions.id = ego_event_log.execution_id").
		Where("egolite_executions.user_id = ? AND ego_event_log.type = ? AND ego_event_log.created_at >= ?",
			userID, "artifact", since).
		Count(&u.Artifacts).Error; err != nil {
		return u, err
	}
	return u, nil
}

// CheckSessionQuota 开会话前的配额裁决。
func CheckSessionQuota(ctx context.Context, db *gorm.DB, userID uint) error {
	u, err := currentUsage(ctx, db, userID)
	if err != nil {
		return err
	}
	if u.Sessions >= SessionQuotaPerDay {
		return ErrQuotaExceeded
	}
	return nil
}

// CheckRoundQuota 派发轮次前的配额裁决。
func CheckRoundQuota(ctx context.Context, db *gorm.DB, userID uint) error {
	u, err := currentUsage(ctx, db, userID)
	if err != nil {
		return err
	}
	if u.Rounds >= RoundQuotaPerDay {
		return ErrQuotaExceeded
	}
	return nil
}

// CheckArtifactQuota 上传产物前的配额裁决。
func CheckArtifactQuota(ctx context.Context, db *gorm.DB, userID uint) error {
	u, err := currentUsage(ctx, db, userID)
	if err != nil {
		return err
	}
	if u.Artifacts >= ArtifactQuotaPerDay {
		return ErrQuotaExceeded
	}
	return nil
}
