package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// StarterKitRepository 开户赠送 provisioning 的读写口（站点配置 / 起步智能体 / 起步资产包）。
//
// 为什么单开一个仓储而不是往 SystemConfig/AIAgent/AssetBundle 三个仓储各加两个方法：
//   - 这是**一个**流程（新租户开户赠送），不是三张表各自的日常 CRUD；把它们拆到三个
//     接口上，读的人要翻三处才知道"开户时到底碰了哪几张表"；
//   - 那三个接口各自还有 mock/fake（`AssetBundleRepository`/`SystemConfigRepository`
//     是 interface），为一次开户各加两三个方法，会让每个 fake 都要跟着补空实现。
//
// 归属口径：调用方一律用 `tenant.WithSystemScope(ctx)` 进入（注册流程的 ctx 没有租户
// 绑定，裸写会撞 fillCreateForTenant 的 fail-closed），行归属靠**行上显式 tenant_id**
// —— 所以这里的每个方法都把 tenantID 作为显式条件，不依赖隔离 callback 注入。SystemScope
// 下 callback 不填列，一旦哪个方法漏了 tenant_id，写出来的行归属就是错的且没人报错。
type StarterKitRepository struct {
	db *gorm.DB
}

func NewStarterKitRepository(db *gorm.DB) *StarterKitRepository {
	return &StarterKitRepository{db: db}
}

// NewStarterKitRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
//
// 供 service 层装配用：调用方不再自行取 `NewTenantRepository().GetDB()` 拼仓储
// （那是 service 直取 DB 句柄的宽口径）。历史调用方 auth.go 开户赠送原先经
// NewTenantRepository() 拿句柄——NewTenantRepository 构造即捕获全局连接、GetDB()
// 原样返回该连接，因此本构造与其同连接、同解析时刻，行为不变。
func NewStarterKitRepositoryDefault() *StarterKitRepository {
	return NewStarterKitRepository(GetDB())
}

// CountSiteConfig 该租户是否已有站点配置（开户赠送的幂等检查）。
func (r *StarterKitRepository) CountSiteConfig(ctx context.Context, tenantID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.SystemConfig{}).
		Where("tenant_id = ?", tenantID).Count(&n).Error
	return n, err
}

// CreateSiteConfig 建一行租户站点配置。
func (r *StarterKitRepository) CreateSiteConfig(ctx context.Context, cfg *model.SystemConfig) error {
	return r.db.WithContext(ctx).Create(cfg).Error
}

// CountAgentByCode 该租户是否已有指定 agent_code 的智能体（幂等键 tenant_id + agent_code）。
func (r *StarterKitRepository) CountAgentByCode(ctx context.Context, tenantID uint, code string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AIAgent{}).
		Where("tenant_id = ? AND agent_code = ?", tenantID, code).Count(&n).Error
	return n, err
}

// FindAgentByCode 取指定租户下 agent_code 对应的智能体；不存在返回 (nil, nil)。
//
// 「不存在」不返回 ErrRecordNotFound：调用方对"种子行缺失"的处理是 Warn 后跳过
// （种子可以后补，开户不能被它拖死），把 not-found 混进 err 会让那段分支写成
// `if err != nil` 而分不清"没种子"和"查挂了"。
func (r *StarterKitRepository) FindAgentByCode(ctx context.Context, tenantID uint, code string) (*model.AIAgent, error) {
	var a model.AIAgent
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND agent_code = ?", tenantID, code).
		First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateAgent 落一条智能体（克隆副本的写入口）。
func (r *StarterKitRepository) CreateAgent(ctx context.Context, a *model.AIAgent) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// CountBundleByAssetID 该租户是否已有指定 asset_id 的资产包（幂等键 tenant_id + asset_id）。
func (r *StarterKitRepository) CountBundleByAssetID(ctx context.Context, tenantID uint, assetID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("tenant_id = ? AND asset_id = ?", tenantID, assetID).Count(&n).Error
	return n, err
}

// CreateBundle 落一条资产包。
func (r *StarterKitRepository) CreateBundle(ctx context.Context, b *model.AssetBundle) error {
	return r.db.WithContext(ctx).Create(b).Error
}
