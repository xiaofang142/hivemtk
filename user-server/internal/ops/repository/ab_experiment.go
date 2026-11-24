package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/ops/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ABExperimentRepository A/B 实验仓库
type ABExperimentRepository struct {
	db *gorm.DB
}

// NewABExperimentRepository 创建 A/B 实验仓库实例
func NewABExperimentRepository() *ABExperimentRepository {
	return &ABExperimentRepository{
		db: _db.GetDB(),
	}
}

// NewABExperimentRepositoryWithDB 创建指定数据库连接的实验仓库实例（用于测试）
func NewABExperimentRepositoryWithDB(db *gorm.DB) *ABExperimentRepository {
	return &ABExperimentRepository{db: db}
}

// Create 创建实验
func (r *ABExperimentRepository) Create(ctx context.Context, experiment *model.ABExperiment) error {
	return r.db.WithContext(ctx).Create(experiment).Error
}

// GetByID 根据 ID 获取实验
func (r *ABExperimentRepository) GetByID(ctx context.Context, id uint) (*model.ABExperiment, error) {
	var experiment model.ABExperiment
	err := r.db.WithContext(ctx).First(&experiment, id).Error
	return &experiment, err
}

// GetAll 获取所有实验列表（分页夹取 1..200：pageSize=0 ⇒ GORM Limit(0) 等于
// 不加 limit，page=0 ⇒ offset 为负 ⇒ PG 报错）。
func (r *ABExperimentRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ABExperiment, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	var experiments []*model.ABExperiment
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.ABExperiment{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&experiments).Error

	return experiments, total, err
}

// Update 整行回写实验。走 db.SaveWholeRow：裸 Save 命中 0 行会回退成
// `INSERT ... ON CONFLICT (id) DO UPDATE`（gorm@v1.31.2），把「详情页读出后、
// 保存前被删掉」的实验按原 id 插回。
func (r *ABExperimentRepository) Update(ctx context.Context, experiment *model.ABExperiment) error {
	if experiment == nil || experiment.ID == 0 {
		return errors.New("ab experiment: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), experiment)
}

// WithTransaction 在单个事务内执行 fn，并把事务句柄交给回调，供服务层把
// 实验主记录与变体的写入纳入同一原子提交（FA-AB-01：创建/更新不得留半套配置）。
// 回调内应使用 NewABExperimentRepositoryWithDB / NewABVariantRepositoryWithDB
// 基于传入的 tx 构造仓储，确保读改写都在同一事务。
func (r *ABExperimentRepository) WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if r == nil || r.db == nil {
		return errors.New("ab experiment: 数据库未装配，无法开启事务")
	}
	return r.db.WithContext(ctx).Transaction(fn)
}

// Delete 删除实验
func (r *ABExperimentRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.ABExperiment{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateStatus 更新实验状态
func (r *ABExperimentRepository) UpdateStatus(ctx context.Context, id uint, status string) error {
	res := r.db.WithContext(ctx).Model(&model.ABExperiment{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetLatestBySourceID 根据 sourceID 获取最新实验（不限状态，按 updated_at DESC）
func (r *ABExperimentRepository) GetLatestBySourceID(ctx context.Context, sourceID string) (*model.ABExperiment, error) {
	var experiment model.ABExperiment
	err := r.db.WithContext(ctx).Where("source_id = ?", sourceID).
		Order("updated_at DESC").
		First(&experiment).Error
	return &experiment, err
}

// ABVariantRepository A/B 变体仓库
type ABVariantRepository struct {
	db *gorm.DB
}

// NewABVariantRepository 创建 A/B 变体仓库实例
func NewABVariantRepository() *ABVariantRepository {
	return &ABVariantRepository{
		db: _db.GetDB(),
	}
}

// NewABVariantRepositoryWithDB 创建指定数据库连接的变体仓库实例（用于测试）
func NewABVariantRepositoryWithDB(db *gorm.DB) *ABVariantRepository {
	return &ABVariantRepository{db: db}
}

// Create 创建变体
func (r *ABVariantRepository) Create(ctx context.Context, variant *model.ABVariant) error {
	return r.db.WithContext(ctx).Create(variant).Error
}

// GetByExperiment 获取实验下的变体列表
func (r *ABVariantRepository) GetByExperiment(ctx context.Context, experimentID uint) ([]*model.ABVariant, error) {
	var variants []*model.ABVariant
	err := r.db.WithContext(ctx).Where("experiment_id = ?", experimentID).
		Order("id ASC").Find(&variants).Error
	return variants, err
}

// DeleteByExperiment 删除实验下所有变体
func (r *ABVariantRepository) DeleteByExperiment(ctx context.Context, experimentID uint) error {
	return r.db.WithContext(ctx).Where("experiment_id = ?", experimentID).Delete(&model.ABVariant{}).Error
}

// ABConversionEventRepository 转化事件仓库
//
// 只保留读取：应用内没有写入路径，写入方法一旦存在就是不可达代码。
type ABConversionEventRepository struct {
	db *gorm.DB
}

// NewABConversionEventRepository 创建转化事件仓库实例
func NewABConversionEventRepository() *ABConversionEventRepository {
	return &ABConversionEventRepository{
		db: _db.GetDB(),
	}
}

// GetByExperiment 获取实验下的转化事件列表
func (r *ABConversionEventRepository) GetByExperiment(ctx context.Context, experimentID uint, page, pageSize int) ([]*model.ABConversionEvent, int64, error) {
	var events []*model.ABConversionEvent
	var total int64

	// Count 的 error 必须透传（吞掉 ⇒ total=0 而列表有行，分页器错乱）+ 分页夹取。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if err := r.db.WithContext(ctx).Model(&model.ABConversionEvent{}).
		Where("experiment_id = ?", experimentID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where("experiment_id = ?", experimentID).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&events).Error

	return events, total, err
}

// ListAllByExperiment 清扫式读全量转化事件（统计聚合专用，区别于分页读
// GetByExperiment 的 200 夹取）：单实验事件量可控，上限 5000。
func (r *ABConversionEventRepository) ListAllByExperiment(ctx context.Context, experimentID uint, limit int) ([]*model.ABConversionEvent, error) {
	if limit < 1 {
		limit = 5000
	}
	if limit > 5000 {
		limit = 5000
	}
	var events []*model.ABConversionEvent
	err := r.db.WithContext(ctx).Where("experiment_id = ?", experimentID).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// ABExperimentResultRepository 实验结果仓库
type ABExperimentResultRepository struct {
	db *gorm.DB
}

// NewABExperimentResultRepository 创建实验结果仓库实例
func NewABExperimentResultRepository() *ABExperimentResultRepository {
	return &ABExperimentResultRepository{
		db: _db.GetDB(),
	}
}

// NewABExperimentResultRepositoryWithDB 创建指定数据库连接的实验结果仓库实例（用于测试）
func NewABExperimentResultRepositoryWithDB(db *gorm.DB) *ABExperimentResultRepository {
	return &ABExperimentResultRepository{db: db}
}

// Upsert 创建或更新结果
func (r *ABExperimentResultRepository) Upsert(ctx context.Context, result *model.ABExperimentResult) error {
	var existing model.ABExperimentResult
	err := r.db.WithContext(ctx).Where("experiment_id = ? AND variant_id = ?", result.ExperimentID, result.VariantID).First(&existing).Error
	if err == nil {
		result.ID = existing.ID
		// SaveWholeRow 同样展开为整行 SET，tenant_id 与 created_at 必然进 SET；
		// 租户归属与创建时间只能沿用库中已有行，不能由调用方传入的空值覆盖。
		result.TenantID = existing.TenantID
		result.CreatedAt = existing.CreatedAt
		// 整行回写而非裸 Save：命中 0 行（行被并发删掉）时裸 Save 会回退成
		// INSERT 复活已删行，这里翻成 ErrRecordNotFound。
		return _db.SaveWholeRow(r.db.WithContext(ctx), result)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.WithContext(ctx).Create(result).Error
}

// GetByExperiment 获取实验结果
func (r *ABExperimentResultRepository) GetByExperiment(ctx context.Context, experimentID uint) ([]*model.ABExperimentResult, error) {
	var results []*model.ABExperimentResult
	err := r.db.WithContext(ctx).Where("experiment_id = ?", experimentID).Find(&results).Error
	return results, err
}

// UpdateWinner 更新获胜者。清零旧 winner 的 UPDATE 错误必须透传：
// 吞掉的话「清零失败 + 置位成功」会留下两个 is_winner=true 的脏数据。
func (r *ABExperimentResultRepository) UpdateWinner(ctx context.Context, experimentID uint, winnerVariantID uint) error {
	if err := r.db.WithContext(ctx).Model(&model.ABExperimentResult{}).
		Where("experiment_id = ?", experimentID).
		Update("is_winner", false).Error; err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&model.ABExperimentResult{}).Where("experiment_id = ? AND variant_id = ?", experimentID, winnerVariantID).
		Update("is_winner", true).Error
}
