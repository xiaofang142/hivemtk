package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

type ScriptLibraryRepository struct {
	db *gorm.DB
}

func NewScriptLibraryRepository(db *gorm.DB) *ScriptLibraryRepository {
	return &ScriptLibraryRepository{db: db}
}

// NewScriptLibraryRepositoryFromGlobal 便捷构造（内部调用 db.GetDB()）
func NewScriptLibraryRepositoryFromGlobal() *ScriptLibraryRepository {
	return NewScriptLibraryRepository(_db.GetDB())
}

// ListObjectionTemplates 查询异议处理模板：category=objection 或 subcategory=objectionCategory，
// usage_count DESC，limit<=0 不限制；仅取 active 且未过期的。
func (r *ScriptLibraryRepository) ListObjectionTemplates(ctx context.Context, objectionCategory string, limit int) ([]model.ScriptLibrary, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("script library repository not initialized")
	}
	q := r.db.WithContext(ctx).
		Where("(category = ? OR subcategory = ?)", "objection", objectionCategory).
		Where("(status IN ('', 'active'))").
		Where("(expires_at IS NULL OR expires_at > NOW())").
		Order("usage_count DESC")
	q = applyListLimit(q, limit)
	var scripts []model.ScriptLibrary
	if err := q.Find(&scripts).Error; err != nil {
		return nil, err
	}
	return scripts, nil
}

// IncrementUsageStats 更新话术使用统计：始终 +usage_count；success=true 时同时
// +success_count 并按 success_count / GREATEST(usage_count, 1) 重算 conversion_rate。
func (r *ScriptLibraryRepository) IncrementUsageStats(ctx context.Context, templateID uint, success bool) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	updates := map[string]any{
		"usage_count": gorm.Expr("usage_count + 1"),
	}
	if success {
		updates["success_count"] = gorm.Expr("success_count + 1")
		updates["conversion_rate"] = gorm.Expr("success_count::float / GREATEST(usage_count, 1)")
	}
	return r.db.WithContext(ctx).
		Model(&model.ScriptLibrary{}).
		Where("id = ?", templateID).
		Updates(updates).Error
}

// scriptVersionListLimit 版本历史上限：版本号只增不减，封顶防止无界读。
const scriptVersionListLimit = 200

// ListScriptVersions 查询话术版本历史（version DESC，最多 scriptVersionListLimit 条）
func (r *ScriptLibraryRepository) ListScriptVersions(ctx context.Context, scriptID uint) ([]model.ScriptVersion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("script library repository not initialized")
	}
	var versions []model.ScriptVersion
	err := r.db.WithContext(ctx).
		Where("script_id = ?", scriptID).
		Order("version DESC").
		Limit(scriptVersionListLimit).
		Find(&versions).Error
	return versions, err
}

// MaxScriptVersion 查询当前最大版本号（无历史返回 0）
func (r *ScriptLibraryRepository) MaxScriptVersion(ctx context.Context, scriptID uint) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("script library repository not initialized")
	}
	var maxVer *int
	err := r.db.WithContext(ctx).
		Model(&model.ScriptVersion{}).
		Select("MAX(version)").
		Where("script_id = ?", scriptID).
		Scan(&maxVer).Error
	if err != nil {
		return 0, err
	}
	if maxVer == nil {
		return 0, nil
	}
	return *maxVer, nil
}

// CreateScriptVersion 写入不可变版本快照
func (r *ScriptLibraryRepository) CreateScriptVersion(ctx context.Context, v *model.ScriptVersion) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	return r.db.WithContext(ctx).Create(v).Error
}

// UpdateScriptActivation 更新话术当前生效版本与状态（激活/过期）。
func (r *ScriptLibraryRepository) UpdateScriptActivation(ctx context.Context, scriptID uint, version int, status string, expiresAt *time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	updates := map[string]any{}
	if version > 0 {
		updates["version"] = version
	}
	if status != "" {
		updates["status"] = status
	}
	updates["expires_at"] = expiresAt
	res := r.db.WithContext(ctx).
		Model(&model.ScriptLibrary{}).
		Where("id = ?", scriptID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	// 命中 0 行 = 话术不存在：不能当成功——expire 对不存在 id 回过
	// 200「话术已过期下线」的假成功（#896，D-35 同型）。
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateScriptExposure 写入曝光记录（fire-and-forget 调用方负责降级）
func (r *ScriptLibraryRepository) CreateScriptExposure(ctx context.Context, e *model.ScriptExposureLog) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *ScriptLibraryRepository) MarkScriptExposuresConverted(ctx context.Context, oneID, conversationID, outcome string, at time.Time, since time.Time, scriptIDs ...uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("script library repository not initialized")
	}
	q := r.db.WithContext(ctx).
		Model(&model.ScriptExposureLog{}).
		Where("converted = ?", false).
		Where("exposed_at >= ?", since)
	if oneID != "" {
		q = q.Where("one_id = ?", oneID)
	}
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	if len(scriptIDs) == 1 && scriptIDs[0] > 0 {
		q = q.Where("script_id = ?", scriptIDs[0])
	}
	res := q.Updates(map[string]any{
		"converted":    true,
		"converted_at": at,
		"outcome":      outcome,
	})
	return res.RowsAffected, res.Error
}

func (r *ScriptLibraryRepository) ListScriptIDsByExposureAnchor(ctx context.Context, oneID, conversationID string) ([]uint, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("script library repository not initialized")
	}
	q := r.db.WithContext(ctx).Model(&model.ScriptExposureLog{})
	if oneID != "" {
		q = q.Where("one_id = ?", oneID)
	}
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	var ids []uint
	err := q.Distinct().Pluck("script_id", &ids).Error
	return ids, err
}

type ScriptVersionStats struct {
	ScriptID       uint    `json:"script_id"`
	Version        int     `json:"version"`
	Bucket         string  `json:"bucket"`
	Exposures      int64   `json:"exposures"`
	Conversions    int64   `json:"conversions"`
	ConversionRate float64 `json:"conversion_rate"`
}

// ScriptExposureStats 聚合各版本×分桶曝光与转化
func (r *ScriptLibraryRepository) ScriptExposureStats(ctx context.Context, scriptID uint) ([]ScriptVersionStats, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("script library repository not initialized")
	}
	var rows []ScriptVersionStats
	err := r.db.WithContext(ctx).
		Model(&model.ScriptExposureLog{}).
		Select("script_id, version, bucket, "+
			"COUNT(*) AS exposures, "+
			"SUM(CASE WHEN converted THEN 1 ELSE 0 END) AS conversions, "+
			"COALESCE(SUM(CASE WHEN converted THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0), 0) AS conversion_rate").
		Where("script_id = ?", scriptID).
		Group("script_id, version, bucket").
		Order("version ASC, bucket ASC").
		Scan(&rows).Error
	return rows, err
}

func (r *ScriptLibraryRepository) FirstScriptByID(ctx context.Context, id uint, out *model.ScriptLibrary) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	return r.db.WithContext(ctx).First(out, id).Error
}

func (r *ScriptLibraryRepository) FirstVersionByID(ctx context.Context, scriptID uint, version int, out *model.ScriptVersion) error {
	if r == nil || r.db == nil {
		return errors.New("script library repository not initialized")
	}
	return r.db.WithContext(ctx).
		Where("script_id = ? AND version = ?", scriptID, version).
		First(out).Error
}
