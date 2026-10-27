package egolite

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 流程定时触发器仓储（egolite_flow_crons，specs/016 S10）。
// 与既有 egolite 域 repo 同纪律：读侧不做业务判定；写侧单语句；
// 归属校验由 service 做（repo 只回答"行是什么"）。

type EgoFlowCronRepository interface {
	// UpsertByFlow 按 flow_id 幂等写入/更新（表达式/时区/enabled 随行更新；
	// 租户面交给 tenancy ctx 钩子，与域内其他 repo 一致）。
	UpsertByFlow(ctx context.Context, tr *model.EgoliteFlowCron) (*model.EgoliteFlowCron, error)
	// GetByFlow 按 flow_id 读（未挂载返回 ErrFlowCronNotFound）。
	// 调度回调无请求 ctx：调用方以 tenant.WithSystemScope 的 ctx 进来（browser cron 同款）。
	GetByFlow(ctx context.Context, flowID uint) (*model.EgoliteFlowCron, error)
	// DeleteByFlow 归属校验删除（user_id 不匹配 ⇒ ErrFlowCronNotFound，同响应防枚举）。
	DeleteByFlow(ctx context.Context, flowID, userID uint) error
	// UpdateTimes 调度记账（next/last 一次写一对）。
	UpdateTimes(ctx context.Context, id uint, next *time.Time, last *time.Time) error
	// ListEnabled RestoreAll 恢复源（重启后重新注册）。
	ListEnabled(ctx context.Context) ([]*model.EgoliteFlowCron, error)
}

type egoFlowCronRepo struct{ db *gorm.DB }

func NewEgoFlowCronRepositoryWithDB(db *gorm.DB) EgoFlowCronRepository {
	return &egoFlowCronRepo{db: db}
}

var ErrFlowCronNotFound = errors.New("流程未挂载定时触发器")

func (r *egoFlowCronRepo) UpsertByFlow(ctx context.Context, tr *model.EgoliteFlowCron) (*model.EgoliteFlowCron, error) {
	// 冲突面只有 flow_id（租户面由 tenancy 钩子收窄）：换表达式/启停都是这一条路径
	err := r.db.WithContext(ctx).
		Where("flow_id = ?", tr.FlowID).
		Assign(map[string]any{
			"user_id": tr.UserID, "cron_expr": tr.CronExpr, "time_zone": tr.TimeZone,
			"enabled": tr.Enabled, "deleted_at": gorm.DeletedAt{},
		}).
		FirstOrCreate(tr).Error
	if err != nil {
		return nil, err
	}
	// FirstOrCreate 命中已有行时 Assign 的值不会自动回填结构体——重读拿最终行
	return r.GetByFlow(context.Background(), tr.FlowID)
}

func (r *egoFlowCronRepo) GetByFlow(ctx context.Context, flowID uint) (*model.EgoliteFlowCron, error) {
	var tr model.EgoliteFlowCron
	if err := r.db.WithContext(ctx).Where("flow_id = ?", flowID).Take(&tr).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFlowCronNotFound
		}
		return nil, err
	}
	return &tr, nil
}

func (r *egoFlowCronRepo) DeleteByFlow(ctx context.Context, flowID, userID uint) error {
	res := r.db.WithContext(ctx).
		Where("flow_id = ? AND user_id = ?", flowID, userID).
		Delete(&model.EgoliteFlowCron{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrFlowCronNotFound
	}
	return nil
}

func (r *egoFlowCronRepo) UpdateTimes(ctx context.Context, id uint, next *time.Time, last *time.Time) error {
	sets := map[string]any{}
	if next != nil {
		sets["next_run_at"] = *next
	} else {
		sets["next_run_at"] = nil
	}
	if last != nil {
		sets["last_run_at"] = *last
	}
	if len(sets) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.EgoliteFlowCron{}).Where("id = ?", id).Updates(sets).Error
}

func (r *egoFlowCronRepo) ListEnabled(ctx context.Context) ([]*model.EgoliteFlowCron, error) {
	var list []*model.EgoliteFlowCron
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&list).Error
	return list, err
}
