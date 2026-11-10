// platform_llm_usage.go 平台垫资模型的本地镜像与用量落库仓储
// （2026-10-25 自 service/llm_platform_usage.go 下沉，L3 铁律）。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrDBNotReady 全局句柄尚未装配（早期启动/单测）。调用方（用量 sink）把它
// 当「本轮跳过」处理，不是缺陷。
var ErrDBNotReady = errors.New("db handle not ready")

// PlatformLLMUsageRepository 平台用量明细 + provider 镜像仓储。
type PlatformLLMUsageRepository struct {
	db *gorm.DB
}

// NewPlatformLLMUsageRepository 默认构造（全局句柄，仓库既有惯例）。
func NewPlatformLLMUsageRepository() *PlatformLLMUsageRepository {
	return &PlatformLLMUsageRepository{db: GetDB()}
}

// NewPlatformLLMUsageRepositoryWithDB 用测试/装配注入的连接构造。
func NewPlatformLLMUsageRepositoryWithDB(db *gorm.DB) *PlatformLLMUsageRepository {
	return &PlatformLLMUsageRepository{db: db}
}

// InsertUsageRecord 幂等落一条用量明细：trace_id 撞唯一索引就跳过
// （重试/重放不重复记账——扣费权威在平台侧，这里只做镜像）。
func (r *PlatformLLMUsageRepository) InsertUsageRecord(ctx context.Context, rec *model.PlatformLLMUsageRecord) error {
	if r.db == nil {
		return ErrDBNotReady
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rec).Error
}

// UpsertProviders 按 (tenant_id, name) upsert 平台下发的 provider 行。
//
// 冲突目标必须与库里实际索引形状一致：(tenant_id, name)——tenancyidx.Normalize
// 在框架层把租户表的唯一索引原位复合化，写 ON CONFLICT (name) 会 42P10 报
// 「no unique or exclusion constraint matching the ON CONFLICT specification」。
// 只更新平台下发的元信息与 source，**不动 api_key**——商户自己填的密钥要留着，
// 否则 provider 从 platform 转回 self 就废了。
// 单价快照两列（input/output_price_per_1k_cents）是平台 sync 的专属写权限：
// 服务层已按 plan_code 关联套餐价（join 不上 = 0 = 如实降级），这里原样落列。
func (r *PlatformLLMUsageRepository) UpsertProviders(ctx context.Context, rows []model.LLMProvider) (int, error) {
	if r.db == nil {
		return 0, ErrDBNotReady
	}
	now := time.Now()
	n := 0
	var firstErr error
	for _, row := range rows {
		row.UpdatedAt = now
		row.CreatedAt = now
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"display_name", "model", "api_type", "vendor",
				"source", "plan_code", "enabled", "updated_at",
				"input_price_per_1k_cents", "output_price_per_1k_cents",
			}),
		}).Create(&row).Error; err != nil {
			// 逐行容错（原 savePlatformProviders 语义）：单行失败不阻断整批，
			// 逐行留痕（观测口径与原实现一致），另返回首个错误，n 只计成功行。
			logger.Warnf("[llm-provider] 平台 provider 落库失败 %s: %v", row.Name, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// GetPlatformPlanPriceByCode 按 plan_code 读本地 provider 镜像上的套餐单价快照
// （分/1k token）。
//
// 快照由 sync 落列（Providers()×Plans() 按 plan_code 关联）；同一 plan_code 挂在
// 多个 provider 行上时取最近同步的一行——sync 在同一趟里对同 plan_code 写同价，
// 正常无歧义，排序只为把「provider 已从下发列表消失、行还残留旧价」的窗口收窄。
//
// 查不到（未同步/套餐下架/plan_code 为空）返回 (0,0,nil)：调用侧如实降级 cost 0，
// 不报错、不编价。读走 system scope（调用方负责）：平台价是商户级配置，镜像行
// 归属默认租户，租户 ctx 的隔离过滤会让非默认租户永远读不到价。
func (r *PlatformLLMUsageRepository) GetPlatformPlanPriceByCode(ctx context.Context, planCode string) (inputCents, outputCents int64, err error) {
	if r.db == nil {
		return 0, 0, ErrDBNotReady
	}
	code := strings.TrimSpace(planCode)
	if code == "" {
		return 0, 0, nil
	}
	var row model.LLMProvider
	qerr := r.db.WithContext(ctx).
		Where("source = ? AND plan_code = ?", model.LLMSourcePlatform, code).
		Order("updated_at DESC, id DESC").First(&row).Error
	if errors.Is(qerr, gorm.ErrRecordNotFound) {
		return 0, 0, nil
	}
	if qerr != nil {
		return 0, 0, qerr
	}
	return row.InputPricePer1kCents, row.OutputPricePer1kCents, nil
}

// ListUsageRecords 分页读平台垫资用量明细（PLT-01 刀2 最小读面）。
//
// 租户作用域不在这里手拼 WHERE：platform_llm_usage_records 是租户表，隔离
// callback 按 ctx 绑定的租户注入过滤（off 模式退化为默认租户全量）——与
// ListTxns 同一口径。字段=既有记录列，读侧不做任何重算/回填。
func (r *PlatformLLMUsageRepository) ListUsageRecords(ctx context.Context, page, pageSize int) ([]*model.PlatformLLMUsageRecord, int64, error) {
	if r.db == nil {
		return nil, 0, ErrDBNotReady
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	// Count 与 Find 必须各起一条 query：同一条链上先 Count 再 Find 会就地改 Statement。
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.PlatformLLMUsageRecord{})
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*model.PlatformLLMUsageRecord
	err := base().Order("occurred_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}
