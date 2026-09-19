package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// FunnelSourceStat 漏斗来源统计行
type FunnelSourceStat struct {
	Source string
	Count  int64
}

// ConversionFunnelRepository 转化漏斗分析仓储
type ConversionFunnelRepository struct {
	db *gorm.DB
}

// NewConversionFunnelRepository 创建转化漏斗仓储实例
func NewConversionFunnelRepository() *ConversionFunnelRepository {
	return &ConversionFunnelRepository{db: _db.GetDB()}
}

// CountCustomerEventsByTimeRange 统计时间范围内的客户事件数（访问阶段）
func (r *ConversionFunnelRepository) CountCustomerEventsByTimeRange(ctx context.Context, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.CustomerEvent{}).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Count(&count).Error
	return count, err
}

// CountCluesByUnixTimeRange 统计时间范围内的线索数（按 unix 时间戳过滤）
func (r *ConversionFunnelRepository) CountCluesByUnixTimeRange(ctx context.Context, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Clue{}).
		Where("create_time >= ? AND create_time <= ?", startTime.Unix(), endTime.Unix()).
		Count(&count).Error
	return count, err
}

// CountIntentRecords 统计时间范围内的意向记录数（按 intent_type 过滤）
func (r *ConversionFunnelRepository) CountIntentRecords(ctx context.Context, startTime, endTime time.Time, intentTypes []string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).
		Table("intent_records").
		Where("created_at BETWEEN ? AND ?", startTime, endTime)
	if len(intentTypes) > 0 {
		q = q.Where("intent_type IN ?", intentTypes)
	}
	err := q.Count(&count).Error
	return count, err
}

// CountCustomerSessionsByTimeRange 统计时间范围内的会话数
func (r *ConversionFunnelRepository) CountCustomerSessionsByTimeRange(ctx context.Context, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.CustomerSession{}).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Count(&count).Error
	return count, err
}

// GetClueSourceStats 线索来源分布（按 account 分组，取 Top 10）
func (r *ConversionFunnelRepository) GetClueSourceStats(ctx context.Context, startTime, endTime time.Time) ([]FunnelSourceStat, error) {
	var rows []FunnelSourceStat
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Clue{}).
		Select("account as source, COUNT(*) as count").
		Where("create_time >= ? AND create_time <= ?", startTime.Unix(), endTime.Unix()).
		Group("account").
		Order("count DESC").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

// ---------------------------------------------------------------------------
// 漏斗阶段词表 —— R-4「双源收口」后的唯一真源
// ---------------------------------------------------------------------------

// FunnelStageKey 漏斗阶段键，取值只能来自本文件下面的常量。
//
// 放在 repository 而不是 service，有两个理由：
//  1. service 已经依赖 repository，词表若放 service 就会逼 repository 反向 import，
//     形成包环；
//  2. 阶段键回答的是"取数层认哪些口径"，本质属于数据访问层，不属于响应渲染层。
//
// `conversion_funnels` 演示表里那套 exposure/click/consult/add_wecom/deal **不在**
// 本词表里，也不是同一批阶段的另一种叫法：那张表只有 cmd/seed 在写、全仓没有任何读
// 路径（判据见 docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md §4.11）。把它并进词表
// 等于把僵尸表扶正，因此 conversion_funnel_stage_test.go 有一条守门测试专门禁止。
type FunnelStageKey string

const (
	StageVisit       FunnelStageKey = "visit"
	StageClue        FunnelStageKey = "clue"
	StageIntent      FunnelStageKey = "intent"
	StageSession     FunnelStageKey = "session"
	StageOpportunity FunnelStageKey = "opportunity"
)

// liveFunnelStages 真实产出、构成 GET /conversion-funnel 响应 stages[] 的阶段清单。
// 顺序即对外契约：看板按下标绘制漏斗，调顺序等于改响应。
var liveFunnelStages = []FunnelStageKey{StageVisit, StageClue, StageIntent, StageSession}

// reservedFunnelStages 是「键名已定、数据源未接」的阶段，只登记不产出。
// 商机位登记在此，是为了让 T-P7-02 复用同一个字面值，而不是在本卡里凭空多画
// 一个恒为 0 的阶段——那是改变现网响应。
var reservedFunnelStages = []FunnelStageKey{StageOpportunity}

var funnelStageLabels = map[FunnelStageKey]string{
	StageVisit:       "访问",
	StageClue:        "线索",
	StageIntent:      "意向",
	StageSession:     "会话",
	StageOpportunity: "商机",
}

// LiveFunnelStages 返回产出阶段清单的副本。
// 必须是副本：调用方一次 sort 就会改掉全进程的词表。
func LiveFunnelStages() []FunnelStageKey {
	out := make([]FunnelStageKey, len(liveFunnelStages))
	copy(out, liveFunnelStages)
	return out
}

// ReservedFunnelStages 返回「已定名、未产出」阶段清单的副本。
func ReservedFunnelStages() []FunnelStageKey {
	out := make([]FunnelStageKey, len(reservedFunnelStages))
	copy(out, reservedFunnelStages)
	return out
}

// Label 返回阶段中文名；未登记的键返回空串（不编名字）——
// service 的阶段详情分支正依赖这个空串口径，对未登记阶段保持 200 + 空名 + 0 的现状。
func (k FunnelStageKey) Label() string { return funnelStageLabels[k] }

// IsLive 判断阶段是否属于当前产出清单。
func (k FunnelStageKey) IsLive() bool {
	for _, s := range liveFunnelStages {
		if s == k {
			return true
		}
	}
	return false
}

// IsReserved 判断阶段是否属于「已定名、未产出」清单。
func (k FunnelStageKey) IsReserved() bool {
	for _, s := range reservedFunnelStages {
		if s == k {
			return true
		}
	}
	return false
}
