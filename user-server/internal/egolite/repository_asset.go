package egolite

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 资产目录仓储（egolite_assets）。
//
// 与既有四域 repo 同一纪律：
//   - 读侧不做业务判定（published/scan_passed 的判定在 service，repo 只回答"行是什么"）；
//   - 写侧单语句，幂等靠 ON CONFLICT（asset_code 是租户内业务键）；
//   - 归属校验用"同响应防枚举"（ErrExecutionNotFound 复用，不暴露"存在但不是你的"）。
type EgoAssetRepository interface {
	// Upsert 按 (tenant_id, asset_code) 幂等写入；返回最终行（含自增 ID）。
	Upsert(ctx context.Context, a *model.EgoliteAsset) (*model.EgoliteAsset, error)
	// GetByCode 按业务键读（执行侧 asset_id 即 asset_code）。
	GetByCode(ctx context.Context, code string) (*model.EgoliteAsset, error)
	// GetByID 管理端按主键读。
	GetByID(ctx context.Context, id uint) (*model.EgoliteAsset, error)
	// ListPublished 用户端可见面：只有 published（offline 不出门）。
	ListPublished(ctx context.Context) ([]*model.EgoliteAsset, error)
	// ListAll 管理端全量（含 offline），分页。
	ListAll(ctx context.Context, page, pageSize int) ([]*model.EgoliteAsset, int64, error)
	// SetStatus 上下线（单语句；不存在返回 ErrExecutionNotFound）。
	SetStatus(ctx context.Context, id uint, status string) error
	// IncInstall 安装/引用计数 +1（弱计数，失败不影响主流程）。
	IncInstall(ctx context.Context, code string) error
}

type egoAssetRepo struct{ db *gorm.DB }

func NewEgoAssetRepositoryWithDB(db *gorm.DB) EgoAssetRepository {
	return &egoAssetRepo{db: db}
}

func (r *egoAssetRepo) Upsert(ctx context.Context, a *model.EgoliteAsset) (*model.EgoliteAsset, error) {
	// 冲突时只更新"内容 + 状态 + 扫描结论"，不动 install_count / published_at 之外的统计口径；
	// 关键一点：重新发布同一 code 必须把状态拉回 published（否则"重新上架"要额外一次调用）。
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "asset_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "version", "domains", "writes", "params_schema", "entry", "script",
			"script_hash", "status", "scan_passed", "scan_report", "source", "source_flow_id",
			"publisher_user_id", "published_at", "updated_at",
		}),
	}).Create(a).Error
	if err != nil {
		return nil, err
	}
	return r.GetByCode(ctx, a.AssetCode)
}

func (r *egoAssetRepo) GetByCode(ctx context.Context, code string) (*model.EgoliteAsset, error) {
	var a model.EgoliteAsset
	err := r.db.WithContext(ctx).Where("asset_code = ?", code).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *egoAssetRepo) GetByID(ctx context.Context, id uint) (*model.EgoliteAsset, error) {
	var a model.EgoliteAsset
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *egoAssetRepo) ListPublished(ctx context.Context) ([]*model.EgoliteAsset, error) {
	var list []*model.EgoliteAsset
	err := r.db.WithContext(ctx).
		Where("status = ? AND scan_passed = ?", model.EgoAssetPublished, true).
		Order("id DESC").Limit(200).Find(&list).Error
	return list, err
}

func (r *egoAssetRepo) ListAll(ctx context.Context, page, pageSize int) ([]*model.EgoliteAsset, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.EgoliteAsset{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.EgoliteAsset
	err := r.db.WithContext(ctx).Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *egoAssetRepo) SetStatus(ctx context.Context, id uint, status string) error {
	res := r.db.WithContext(ctx).Model(&model.EgoliteAsset{}).Where("id = ?", id).
		Updates(map[string]any{"status": status})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

func (r *egoAssetRepo) IncInstall(ctx context.Context, code string) error {
	return r.db.WithContext(ctx).Model(&model.EgoliteAsset{}).
		Where("asset_code = ?", code).
		UpdateColumn("install_count", gorm.Expr("install_count + 1")).Error
}
