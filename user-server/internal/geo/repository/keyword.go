package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// GeoKeywordFunnelField 漏斗聚合的分组字段（闭合枚举，列名不收外部字符串）
type GeoKeywordFunnelField int

const (
	// GeoKeywordFunnelLayer 按 layer 分组，NULL 归入 'seed'（沿 AV-04b 改前 COALESCE 缺省）
	GeoKeywordFunnelLayer GeoKeywordFunnelField = iota
	// GeoKeywordFunnelQueryIntent 按 query_intent 分组，NULL 归入 'unknown'
	GeoKeywordFunnelQueryIntent
	// GeoKeywordFunnelStage 按 funnel_stage 分组，NULL 归入 'unknown'
	GeoKeywordFunnelStage
)

// funnelFieldSQL 返回该枚举锁定的列名与 NULL 缺省标签。
// 只在这里展开列名：枚举闭合，调用方传不进任意字符串（无注入面）。
func (f GeoKeywordFunnelField) funnelFieldSQL() (column, nullLabel string, err error) {
	switch f {
	case GeoKeywordFunnelLayer:
		return "layer", "seed", nil
	case GeoKeywordFunnelQueryIntent:
		return "query_intent", "unknown", nil
	case GeoKeywordFunnelStage:
		return "funnel_stage", "unknown", nil
	default:
		return "", "", fmt.Errorf("geo keyword funnel: 未知分组字段 %d", int(f))
	}
}

// GeoKeywordFunnelCount 漏斗聚合一行的原始读数（分组标签 + 计数）。
// Value 的列别名固定为 layer（与下沉前的内联 struct 同形）。
type GeoKeywordFunnelCount struct {
	Value string `gorm:"column:layer"`
	Count int64  `gorm:"column:cnt"`
}

// GeoKeywordMiningPatch 挖掘结果合并回写已有词的白名单补丁。
// 只允许改这十个字段（与下沉前 SaveMiningResults 的 Updates map 逐键一致）；
// keyword/source/status/category 等身份字段不在白名单内，仓储层物理写不到。
type GeoKeywordMiningPatch struct {
	Layer          string
	ParentID       string
	ParentKeyword  string
	QueryIntent    string
	Intent         string
	FunnelStage    string
	SuggestEngines string
	SuggestCount   int
	LastMinedAt    *time.Time
	UpdatedAt      time.Time
}

// GeoKeywordRepository GEO 关键词仓储接口
type GeoKeywordRepository interface {
	Create(ctx context.Context, keyword *model.GeoKeyword) error
	BatchCreate(ctx context.Context, keywords []*model.GeoKeyword) error
	GetList(ctx context.Context, search, category, source, cluster, status, layer string, page, limit int) ([]*model.GeoKeyword, int64, error)
	Delete(ctx context.Context, id string) error
	// CountByLayerStatus 统计指定 layer + status 的关键词数
	CountByLayerStatus(ctx context.Context, layer, status string) (int64, error)

	// ── 关键词挖掘持久化组（AV-04b 自 KeywordMiningService 下沉）──
	// 语义冻结基准：geo/service/keyword_mining.go 改造前实现（2026-11-07 树）。

	// FindExistingByKeywords 按文本批查既有词。
	// 谓词仅 keyword IN (?)；无排序（与改前一致，调用方按文本建索引不依赖行序）；
	// 租户过滤与软删除由连接级 callback / gorm 约定生效。
	FindExistingByKeywords(ctx context.Context, keywords []string) ([]model.GeoKeyword, error)
	// TouchMinedAtByIDs 按 ID 刷新 last_mined_at（种子词复访）。
	// 白名单：只写 last_mined_at + updated_at，两值同源（调用方传入的 now）。
	TouchMinedAtByIDs(ctx context.Context, ids []string, now time.Time) error
	// UpdateMiningFields 把挖掘合并结果回写既有词，白名单见 GeoKeywordMiningPatch。
	// 按 id 单行更新；不校验 RowsAffected（改前即如此：id 来自刚查出的库内行）。
	UpdateMiningFields(ctx context.Context, id string, patch GeoKeywordMiningPatch) error
	// FunnelCounts 漏斗原始聚合：status='active'，按 COALESCE(列, 缺省标签) 分组计数。
	// NULL 归缺省标签；空字符串不归（COALESCE 只兜 NULL，'' 与 NULL 保持可分辨）。
	FunnelCounts(ctx context.Context, field GeoKeywordFunnelField) ([]GeoKeywordFunnelCount, error)
	// ListSuggestEnginePayloads 读 suggest 层 suggest_engines 原文（JSON 数组文本）。
	// 谓词 status='active' AND layer='suggest' AND suggest_engines <> ''（NULL 与 '' 一并排除，
	// 与改前一致）；坏 JSON 原样交回，解析与跳过属业务层。
	ListSuggestEnginePayloads(ctx context.Context) ([]string, error)
}

type geoKeywordRepo struct {
	db *gorm.DB
}

func NewGeoKeywordRepository() GeoKeywordRepository {
	return &geoKeywordRepo{db: _db.GetDB()}
}

// NewGeoKeywordRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoKeywordRepositoryWithDB(db *gorm.DB) GeoKeywordRepository {
	return &geoKeywordRepo{db: db}
}

func (r *geoKeywordRepo) Create(ctx context.Context, keyword *model.GeoKeyword) error {
	return r.db.WithContext(ctx).Create(keyword).Error
}

func (r *geoKeywordRepo) BatchCreate(ctx context.Context, keywords []*model.GeoKeyword) error {
	if len(keywords) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(keywords, 100).Error
}

func (r *geoKeywordRepo) GetList(ctx context.Context, search, category, source, cluster, status, layer string, page, limit int) ([]*model.GeoKeyword, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	var keywords []*model.GeoKeyword
	var total int64
	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&model.GeoKeyword{})

	if search != "" {
		query = query.Where("keyword LIKE ?", "%"+escapeLike(search)+"%")
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if cluster != "" {
		query = query.Where("cluster = ?", cluster)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if layer != "" {
		query = query.Where("layer = ?", layer)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&keywords).Error
	return keywords, total, err
}

func (r *geoKeywordRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoKeyword{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoKeywordRepo) CountByLayerStatus(ctx context.Context, layer, status string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Where("layer = ? AND status = ?", layer, status).
		Count(&n).Error
	return n, err
}

// ────────────────────────────────────────────
// 关键词挖掘持久化组（AV-04b 自 KeywordMiningService 下沉，语义逐项保持）
// ────────────────────────────────────────────

func (r *geoKeywordRepo) FindExistingByKeywords(ctx context.Context, keywords []string) ([]model.GeoKeyword, error) {
	var rows []model.GeoKeyword
	err := r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Where("keyword IN ?", keywords).
		Find(&rows).Error
	return rows, err
}

func (r *geoKeywordRepo) TouchMinedAtByIDs(ctx context.Context, ids []string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"last_mined_at": now, "updated_at": now}).Error
}

func (r *geoKeywordRepo) UpdateMiningFields(ctx context.Context, id string, patch GeoKeywordMiningPatch) error {
	return r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"layer":           patch.Layer,
			"parent_id":       patch.ParentID,
			"parent_keyword":  patch.ParentKeyword,
			"query_intent":    patch.QueryIntent,
			"intent":          patch.Intent,
			"funnel_stage":    patch.FunnelStage,
			"suggest_engines": patch.SuggestEngines,
			"suggest_count":   patch.SuggestCount,
			"last_mined_at":   patch.LastMinedAt,
			"updated_at":      patch.UpdatedAt,
		}).Error
}

func (r *geoKeywordRepo) FunnelCounts(ctx context.Context, field GeoKeywordFunnelField) ([]GeoKeywordFunnelCount, error) {
	column, nullLabel, err := field.funnelFieldSQL()
	if err != nil {
		return nil, err
	}
	coalesced := fmt.Sprintf("COALESCE(%s,'%s')", column, nullLabel)
	var rows []GeoKeywordFunnelCount
	err = r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Select(coalesced+" as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group(coalesced).
		Scan(&rows).Error
	return rows, err
}

func (r *geoKeywordRepo) ListSuggestEnginePayloads(ctx context.Context) ([]string, error) {
	var rows []struct {
		Engines string `gorm:"column:engines"`
	}
	err := r.db.WithContext(ctx).Model(&model.GeoKeyword{}).
		Select("suggest_engines as engines").
		Where("status = ? AND layer = ? AND suggest_engines <> ''", "active", "suggest").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Engines)
	}
	return out, nil
}
