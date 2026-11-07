package repository

import (
	"context"
	"errors"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// DashboardScreenRepository 数据大屏仓库
type DashboardScreenRepository struct {
	db *gorm.DB
}

// NewDashboardScreenRepository 创建数据大屏仓库实例
func NewDashboardScreenRepository() *DashboardScreenRepository {
	return &DashboardScreenRepository{
		db: _db.GetDB(),
	}
}

// Create 创建大屏
func (r *DashboardScreenRepository) Create(ctx context.Context, screen *model.DashboardScreen) error {
	return r.db.WithContext(ctx).Create(screen).Error
}

// GetByID 根据 ID 获取大屏
func (r *DashboardScreenRepository) GetByID(ctx context.Context, id uint) (*model.DashboardScreen, error) {
	var screen model.DashboardScreen
	err := r.db.WithContext(ctx).First(&screen, id).Error
	return &screen, err
}

// GetAll 获取所有大屏列表。
//
// Count 与 Find 必须各起一条链：同一 *gorm.DB 上先 Count 再追加分页子句，
// GORM 会把分页子句叠进 Count 语句（判例 #133/#150）。分页夹取 1..200。
func (r *DashboardScreenRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.DashboardScreen, int64, error) {
	page, pageSize = clampPage(page, pageSize)
	var screens []*model.DashboardScreen
	var total int64

	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.DashboardScreen{})
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := base().
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&screens).Error

	return screens, total, err
}

// GetPublicByCodeCtx 匿名公开访问：code 由分享链接携带，先系统作用域反查
// 唯一租户，再钉死在该租户内读取并自增浏览量。同码多租户时拒猜（返回未找到）。
func (r *DashboardScreenRepository) GetPublicByCodeCtx(ctx context.Context, code string) (*model.DashboardScreen, error) {
	var ids []uint
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Model(&model.DashboardScreen{}).
		Distinct("tenant_id").
		Where("code = ? AND is_public = ?", code, true).
		Limit(2).
		Pluck("tenant_id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) != 1 || ids[0] == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	tctx := tenant.OnlyTenant(ctx, ids[0])
	var screen model.DashboardScreen
	if err := r.db.WithContext(tctx).
		Where("code = ? AND is_public = ?", code, true).
		First(&screen).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(tctx).Model(&model.DashboardScreen{}).
		Where("id = ?", screen.ID).
		Update("view_count", gorm.Expr("view_count + 1")).Error; err != nil {
		return nil, err
	}
	return &screen, nil
}

// Update 整行回写大屏。走 db.SaveWholeRow：裸 Save 命中 0 行会回退成
// `INSERT ... ON CONFLICT (id) DO UPDATE`（gorm@v1.31.2），把「编辑页读出后、
// 保存前被删掉」的大屏按原 id 插回。
func (r *DashboardScreenRepository) Update(ctx context.Context, screen *model.DashboardScreen) error {
	if screen == nil || screen.ID == 0 {
		return errors.New("dashboard screen: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), screen)
}

// Delete 删除大屏。0 行 ⇒ ErrRecordNotFound。
func (r *DashboardScreenRepository) Delete(ctx context.Context, id uint) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Delete(&model.DashboardScreen{}, id))
}

// DashboardWidgetRepository 大屏 Widget 仓库
type DashboardWidgetRepository struct {
	db *gorm.DB
}

// NewDashboardWidgetRepository 创建大屏 Widget 仓库实例
func NewDashboardWidgetRepository() *DashboardWidgetRepository {
	return &DashboardWidgetRepository{
		db: _db.GetDB(),
	}
}

// Create 创建 Widget
func (r *DashboardWidgetRepository) Create(ctx context.Context, widget *model.DashboardWidget) error {
	return r.db.WithContext(ctx).Create(widget).Error
}

// GetByScreenIDCtx 公开访问路径的 widgets 读取（必须带已绑定租户的 ctx）
func (r *DashboardWidgetRepository) GetByScreenIDCtx(ctx context.Context, screenID uint) ([]*model.DashboardWidget, error) {
	var widgets []*model.DashboardWidget
	err := r.db.WithContext(ctx).Where("screen_id = ?", screenID).Order("sort_order, y, x").Find(&widgets).Error
	return widgets, err
}

// DeleteByScreenID 删除大屏下所有 Widgets
func (r *DashboardWidgetRepository) DeleteByScreenID(ctx context.Context, screenID uint) error {
	return r.db.WithContext(ctx).Where("screen_id = ?", screenID).Delete(&model.DashboardWidget{}).Error
}

// ----------------------------------------------------------------------------
// 大屏聚合只读查询（AggregateDashboardData / FetchRealtimeActivities 的查询组下沉）
//
// 迁移自 ops/service/dashboard_screen.go（AV-04c 批次5 刀1）：service 原先经
// sysrepo.GetDB() 直查 clues / customers / "order"，此处把同一组查询按原谓词、
// 原投影、原排序逐字下沉。全部只读；租户过滤语义不变 —— 仓储与原 service 直查
// 用的是同一条全局句柄（repository.GetDB 与 pkgdb.GetDB 同源 dbstate）加调用方
// ctx，租户隔离 callback（on 模式 fail-closed）作用于同一会话，行为逐项不变。
// HasTable 探测与原实现一致：表不存在时跳过查询、计数按 0 处理（不是错误）。
// ----------------------------------------------------------------------------

// DashboardDayCount 按天计数行（to_char 日期投影 + 计数）。
type DashboardDayCount struct {
	Day string
	Cnt int64
}

// DashboardNameValue 名称-数值行（渠道/地区分布投影）。
type DashboardNameValue struct {
	Name  string
	Value int64
}

// CountClues 统计线索总数。
func (r *DashboardScreenRepository) CountClues(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&sysmodel.Clue{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountCluesCreatedSince 统计 create_time >= from 的线索数。
func (r *DashboardScreenRepository) CountCluesCreatedSince(ctx context.Context, from time.Time) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&sysmodel.Clue{}).
		Where("create_time >= ?", from.Unix()).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountCluesCreatedBetween 统计 from <= create_time < to 的线索数。
func (r *DashboardScreenRepository) CountCluesCreatedBetween(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&sysmodel.Clue{}).
		Where("create_time >= ? AND create_time < ?", from.Unix(), to.Unix()).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountVerifiedClues 统计已验证线索数（is_verify = 1）。
func (r *DashboardScreenRepository) CountVerifiedClues(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&sysmodel.Clue{}).
		Where("is_verify = ?", 1).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountCustomersIfTableExists 统计客户总数；customers 表不存在时返回 0（非错误），
// 与原 service 直查的 HasTable 守卫一致。
func (r *DashboardScreenRepository) CountCustomersIfTableExists(ctx context.Context) (int64, error) {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(&sysmodel.Customer{}) {
		return 0, nil
	}
	var n int64
	if err := db.Model(&sysmodel.Customer{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountOrdersIfTableExists 统计订单总数；"order" 表不存在时返回 0（非错误）。
func (r *DashboardScreenRepository) CountOrdersIfTableExists(ctx context.Context) (int64, error) {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(&sysmodel.Order{}) {
		return 0, nil
	}
	var n int64
	if err := db.Model(&sysmodel.Order{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountOrdersCreatedSinceIfTableExists 统计 create_time >= from 的订单数；表不存在返回 0。
func (r *DashboardScreenRepository) CountOrdersCreatedSinceIfTableExists(ctx context.Context, from time.Time) (int64, error) {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(&sysmodel.Order{}) {
		return 0, nil
	}
	var n int64
	if err := db.Model(&sysmodel.Order{}).
		Where("create_time >= ?", from.Unix()).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountOrdersCreatedBetweenIfTableExists 统计 from <= create_time < to 的订单数；表不存在返回 0。
func (r *DashboardScreenRepository) CountOrdersCreatedBetweenIfTableExists(ctx context.Context, from, to time.Time) (int64, error) {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(&sysmodel.Order{}) {
		return 0, nil
	}
	var n int64
	if err := db.Model(&sysmodel.Order{}).
		Where("create_time >= ? AND create_time < ?", from.Unix(), to.Unix()).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// ClueTrendDaily 近 N 天线索趋势（to_char 日期分组，按 day 升序）。
// GROUP BY 写表达式别名 day（原实现逐字保留）。
func (r *DashboardScreenRepository) ClueTrendDaily(ctx context.Context, from time.Time) ([]DashboardDayCount, error) {
	var rows []DashboardDayCount
	if err := r.db.WithContext(ctx).Table("clues").
		Select("to_char(to_timestamp(create_time), 'MM-DD') AS day, COUNT(*) AS cnt").
		Where("create_time >= ?", from.Unix()).
		Group("day").Order("day").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// OrderTrendDaily 近 N 天成单趋势；"order" 表不存在时返回 nil（非错误，与原守卫一致）。
func (r *DashboardScreenRepository) OrderTrendDaily(ctx context.Context, from time.Time) ([]DashboardDayCount, error) {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(&sysmodel.Order{}) {
		return nil, nil
	}
	var rows []DashboardDayCount
	if err := db.Table("\"order\"").
		Select("to_char(to_timestamp(create_time), 'MM-DD') AS day, COUNT(*) AS cnt").
		Where("create_time >= ?", from.Unix()).
		Group("day").Order("day").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ClueChannelDistribution 渠道分布（source_id 空串归「未知」，按计数降序）。
// GROUP BY 写表达式本身（GORM 的 Group 走 WriteQuoted，序号会被引用成标识符）。
func (r *DashboardScreenRepository) ClueChannelDistribution(ctx context.Context) ([]DashboardNameValue, error) {
	var rows []DashboardNameValue
	if err := r.db.WithContext(ctx).Table("clues").
		Select("COALESCE(NULLIF(source_id, ''), '未知') AS name, COUNT(*) AS value").
		Group("COALESCE(NULLIF(source_id, ''), '未知')").Order("value DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ClueRegionDistribution 地区分布（city 空串归「未知」，按计数降序）。
func (r *DashboardScreenRepository) ClueRegionDistribution(ctx context.Context) ([]DashboardNameValue, error) {
	var rows []DashboardNameValue
	if err := r.db.WithContext(ctx).Table("clues").
		Select("COALESCE(NULLIF(city, ''), '未知') AS name, COUNT(*) AS value").
		Group("COALESCE(NULLIF(city, ''), '未知')").Order("value DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListRecentClues 按创建时间倒序取最近 limit 条线索（实时活动流）。
func (r *DashboardScreenRepository) ListRecentClues(ctx context.Context, limit int) ([]sysmodel.Clue, error) {
	var clues []sysmodel.Clue
	if err := r.db.WithContext(ctx).Order("create_time DESC").Limit(limit).Find(&clues).Error; err != nil {
		return nil, err
	}
	return clues, nil
}
