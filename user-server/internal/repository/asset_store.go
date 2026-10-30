package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AssetStoreRepository 资产市场商店化域的存取口（asset_listings / asset_versions /
// local_assets 购买校验 / asset_ratings / asset_reports / extension_usage_monthly）。
//
// 为什么整域一个仓储而不是按表拆四五个：这一域的查询全部落在同一套状态机
// （listing 的当前版本、评分冗余列、举报联动下架）上，拆开后"谁负责把
// rating_avg 重算回 listing"就没有单一归属。service 只留判定与文案。
type AssetStoreRepository struct {
	db *gorm.DB
}

// NewAssetStoreRepository 用调用方注入的连接构造（DI 装配点）。
// NewAssetStoreRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
func NewAssetStoreRepositoryDefault() *AssetStoreRepository {
	return NewAssetStoreRepository(GetDB())
}

func NewAssetStoreRepository(db *gorm.DB) *AssetStoreRepository {
	return &AssetStoreRepository{db: db}
}

// CountListingsByCode 计数同 asset_code 的上架单（全库唯一，不按租户）。
func (r *AssetStoreRepository) CountListingsByCode(ctx context.Context, assetCode string) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.AssetListing{}).Where("asset_code = ?", assetCode).Count(&cnt).Error
	return cnt, err
}

// CreateListing 写入上架单。
func (r *AssetStoreRepository) CreateListing(ctx context.Context, l *model.AssetListing) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// GetListing 按 (id, tenant_id) 取上架单。
func (r *AssetStoreRepository) GetListing(ctx context.Context, tenantID uint, listingID int64) (*model.AssetListing, error) {
	var l model.AssetListing
	if err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", listingID, tenantID).First(&l).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

// CountVersionsSince 计数某贡献者自 since 起的提审版本行。
func (r *AssetStoreRepository) CountVersionsSince(ctx context.Context, tenantID uint, contributorID int64, since time.Time) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.AssetVersion{}).
		Where("tenant_id = ? AND created_at >= ?", tenantID, since).
		Where("listing_id IN (?)", r.db.WithContext(ctx).Model(&model.AssetListing{}).Select("id").Where("contributor_id = ?", contributorID)).
		Count(&cnt).Error
	return cnt, err
}

// CreateVersion 写入版本行。
func (r *AssetStoreRepository) CreateVersion(ctx context.Context, v *model.AssetVersion) error {
	return r.db.WithContext(ctx).Create(v).Error
}

// UpdateListingByID 按主键更新上架单字段。
func (r *AssetStoreRepository) UpdateListingByID(ctx context.Context, l *model.AssetListing, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(l).Updates(updates).Error
}

// UpdateListing 按 (id, tenant_id) 更新上架单字段。
func (r *AssetStoreRepository) UpdateListing(ctx context.Context, tenantID uint, listingID int64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.AssetListing{}).
		Where("id = ? AND tenant_id = ?", listingID, tenantID).Updates(updates).Error
}

// PublishVersion 把版本行置为已发布（版本级同步发布）。
func (r *AssetStoreRepository) PublishVersion(ctx context.Context, tenantID uint, versionID int64, reviewerID int64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssetVersion{}).
		Where("id = ? AND tenant_id = ?", versionID, tenantID).
		Updates(map[string]any{"status": model.AssetListingPublished, "published_at": at, "reviewed_by": reviewerID}).Error
}

// ReviewQueue 审核队列：submitted/auto_check/manual_review，按 updated_at 升序。
func (r *AssetStoreRepository) ReviewQueue(ctx context.Context, assetType string, page, pageSize int) ([]model.AssetListing, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AssetListing{}).
		Where("status IN ?", []string{model.AssetListingSubmitted, model.AssetListingAutoCheck, model.AssetListingManualReview})
	if assetType != "" {
		q = q.Where("asset_type = ?", assetType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.AssetListing
	if err := q.Order("updated_at ASC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// CountPurchasedLocalAsset 评分门槛：该 purchase_id 是否属于该租户的已购记录。
func (r *AssetStoreRepository) CountPurchasedLocalAsset(ctx context.Context, tenantID uint, purchaseID int64) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.LocalAsset{}).
		Where("tenant_id = ? AND purchase_id = ? AND source = ?", tenantID, purchaseID, model.AssetSourcePurchased).
		Count(&cnt).Error
	return cnt, err
}

// FindOrCreateRating 一人一评：按 (listing_id, user_id, tenant_id) 取既有行，无则插入。
func (r *AssetStoreRepository) FindOrCreateRating(ctx context.Context, rating *model.AssetRating) error {
	return r.db.WithContext(ctx).
		Where("listing_id = ? AND user_id = ? AND tenant_id = ?", rating.ListingID, rating.UserID, rating.TenantID).
		FirstOrCreate(rating).Error
}

// UpdateRating 覆盖评分行内容。
func (r *AssetStoreRepository) UpdateRating(ctx context.Context, rating *model.AssetRating, score int, comment string) error {
	return r.db.WithContext(ctx).Model(rating).Updates(map[string]any{"score": score, "comment": comment}).Error
}

// RatingAggregate 汇总某上架单的可见评分（均值与条数）。
func (r *AssetStoreRepository) RatingAggregate(ctx context.Context, tenantID uint, listingID int64) (avg float64, cnt int64, err error) {
	var agg struct {
		Avg float64
		Cnt int64
	}
	err = r.db.WithContext(ctx).Model(&model.AssetRating{}).
		Select("COALESCE(AVG(score),0) AS avg, COUNT(*) AS cnt").
		Where("listing_id = ? AND tenant_id = ? AND status = ?", listingID, tenantID, "visible").
		Scan(&agg).Error
	return agg.Avg, agg.Cnt, err
}

// CountOpenReports 同类型未结单举报计数（去重判据）。
func (r *AssetStoreRepository) CountOpenReports(ctx context.Context, tenantID uint, listingID, reporterID int64, category string) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.AssetReport{}).
		Where("listing_id = ? AND reporter_id = ? AND category = ? AND status = ? AND tenant_id = ?",
			listingID, reporterID, category, model.AssetReportOpen, tenantID).
		Count(&cnt).Error
	return cnt, err
}

// CreateReport 写入举报行。
func (r *AssetStoreRepository) CreateReport(ctx context.Context, rep *model.AssetReport) error {
	return r.db.WithContext(ctx).Create(rep).Error
}

// GetReport 按 (id, tenant_id) 取举报行。
func (r *AssetStoreRepository) GetReport(ctx context.Context, tenantID uint, reportID int64) (*model.AssetReport, error) {
	var rep model.AssetReport
	if err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", reportID, tenantID).First(&rep).Error; err != nil {
		return nil, err
	}
	return &rep, nil
}

// UpdateReport 按主键更新举报行字段。
func (r *AssetStoreRepository) UpdateReport(ctx context.Context, rep *model.AssetReport, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(rep).Updates(updates).Error
}

// FindUsageMonthly 取一条月度用量行；found=false 表示还没有这一格的记录。
func (r *AssetStoreRepository) FindUsageMonthly(ctx context.Context, tenantID uint, listingID int64, extensionKey, period string) (*model.ExtensionUsageMonthly, bool, error) {
	var row model.ExtensionUsageMonthly
	err := r.db.WithContext(ctx).
		Where("listing_id = ? AND extension_key = ? AND period = ? AND tenant_id = ?", listingID, extensionKey, period, tenantID).
		First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &row, true, nil
}

// CreateUsageMonthly 写入月度用量行。
func (r *AssetStoreRepository) CreateUsageMonthly(ctx context.Context, row *model.ExtensionUsageMonthly) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// IncrUsageMonthly 原子累加月度用量。
func (r *AssetStoreRepository) IncrUsageMonthly(ctx context.Context, row *model.ExtensionUsageMonthly, delta int64) error {
	return r.db.WithContext(ctx).Model(row).UpdateColumn("usage_count", gorm.Expr("usage_count + ?", delta)).Error
}

// ListUsage 月账分页（按 usage_count 倒序）。
func (r *AssetStoreRepository) ListUsage(ctx context.Context, period string, page, pageSize int) ([]model.ExtensionUsageMonthly, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.ExtensionUsageMonthly{})
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.ExtensionUsageMonthly
	if err := q.Order("usage_count DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListReports 举报分页（按 id 倒序）。
func (r *AssetStoreRepository) ListReports(ctx context.Context, status string, page, pageSize int) ([]model.AssetReport, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AssetReport{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.AssetReport
	if err := q.Order("id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListVersions 版本列表（按 id 倒序）。
func (r *AssetStoreRepository) ListVersions(ctx context.Context, tenantID uint, listingID int64) ([]model.AssetVersion, error) {
	var out []model.AssetVersion
	err := r.db.WithContext(ctx).Where("listing_id = ? AND tenant_id = ?", listingID, tenantID).
		Order("id DESC").Find(&out).Error
	return out, err
}
