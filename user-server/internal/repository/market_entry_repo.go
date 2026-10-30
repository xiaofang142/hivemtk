package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// MarketEntryRepository 模板/技能市场写侧闭环的存取口
// （market_catalog_entries 目录条目 / market_entry_orders 购买订单）。
//
// 为什么整域一个仓储而不是按表拆两个：发布-审核-支付是一条状态机链
// （提审 → 上架 → 下单 → 收款确认），拆开后「谁负责把状态机两边对齐」
// 就没有单一归属（对齐 asset_store 整域一仓的写法）。
type MarketEntryRepository struct {
	db *gorm.DB
}

// NewMarketEntryRepository 用调用方注入的连接构造（DI 装配点）。
func NewMarketEntryRepository(db *gorm.DB) *MarketEntryRepository {
	return &MarketEntryRepository{db: db}
}

// NewMarketEntryRepositoryDefault 默认构造（全局句柄）。
func NewMarketEntryRepositoryDefault() *MarketEntryRepository {
	return NewMarketEntryRepository(GetDB())
}

// ── 目录条目 ────────────────────────────────────────────────────────────────

// CreateEntry 写入目录条目（发布入口落点：status=pending）。
func (r *MarketEntryRepository) CreateEntry(ctx context.Context, e *model.MarketCatalogEntry) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// GetEntry 按 ID 取条目；查无返回 (nil, nil)（仓储层惯例）。
func (r *MarketEntryRepository) GetEntry(ctx context.Context, id string) (*model.MarketCatalogEntry, error) {
	var e model.MarketCatalogEntry
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&e).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// CountEntriesByID 计数同 ID 条目（ID 是目录契约主键，全库唯一，含各状态）。
func (r *MarketEntryRepository) CountEntriesByID(ctx context.Context, id string) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.MarketCatalogEntry{}).
		Where("id = ?", id).Count(&cnt).Error
	return cnt, err
}

// ListEntriesByStatus 按状态取条目（审核台队列），稳定序：created_at DESC（同刻按 ID）。
func (r *MarketEntryRepository) ListEntriesByStatus(ctx context.Context, status string, limit int) ([]*model.MarketCatalogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []*model.MarketCatalogEntry
	q := r.db.WithContext(ctx).Model(&model.MarketCatalogEntry{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListApprovedEntries 取全部已上架条目（公开橱窗合并数据源）。
// 目录是平台级共享橱窗：新上架的排前面（created_at DESC），量级可控不翻页。
func (r *MarketEntryRepository) ListApprovedEntries(ctx context.Context) ([]*model.MarketCatalogEntry, error) {
	var rows []*model.MarketCatalogEntry
	err := r.db.WithContext(ctx).Model(&model.MarketCatalogEntry{}).
		Where("status = ?", model.MarketEntryApproved).
		Order("created_at DESC, id ASC").
		Find(&rows).Error
	return rows, err
}

// ListEntriesBySubmitter 取某提交人的全部条目（我的发布）。
func (r *MarketEntryRepository) ListEntriesBySubmitter(ctx context.Context, submitterID int64) ([]*model.MarketCatalogEntry, error) {
	var rows []*model.MarketCatalogEntry
	err := r.db.WithContext(ctx).Model(&model.MarketCatalogEntry{}).
		Where("submitter_id = ?", submitterID).
		Order("created_at DESC, id ASC").
		Find(&rows).Error
	return rows, err
}

// UpdateEntryStatus 按**当前状态**做条件跃迁（乐观并发：两个人同时审核同一单，
// 只有一个能成——Where 里带上 from 状态，Update 影响行数为 0 即说明别人先动了）。
// 返回是否发生了跃迁。
func (r *MarketEntryRepository) UpdateEntryStatus(ctx context.Context, id, from, to string, patch map[string]any) (bool, error) {
	sets := map[string]any{"status": to, "reviewed_at": time.Now()}
	for k, v := range patch {
		sets[k] = v
	}
	res := r.db.WithContext(ctx).Model(&model.MarketCatalogEntry{}).
		Where("id = ? AND status = ?", id, from).Updates(sets)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ── 购买订单 ────────────────────────────────────────────────────────────────

// CreateOrder 写入购买订单。
func (r *MarketEntryRepository) CreateOrder(ctx context.Context, o *model.MarketEntryOrder) error {
	return r.db.WithContext(ctx).Create(o).Error
}

// GetOrderByNo 按单号取订单（租户隔离由 callback 按表裁决）；查无返回 (nil, nil)。
func (r *MarketEntryRepository) GetOrderByNo(ctx context.Context, orderNo string) (*model.MarketEntryOrder, error) {
	var o model.MarketEntryOrder
	if err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// CountOrdersByBuyerAndEntry 计数某买家某条目的**活跃**订单（待支付或已支付），
// 供「不重复下单 / 已拥有不再下单」判定。
func (r *MarketEntryRepository) CountOrdersByBuyerAndEntry(ctx context.Context, buyerID int64, entryID string) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.MarketEntryOrder{}).
		Where("buyer_id = ? AND entry_id = ? AND status IN ?", buyerID, entryID,
			[]string{model.MarketOrderPendingPayment, model.MarketOrderPaid}).
		Count(&cnt).Error
	return cnt, err
}

// ListOrdersByBuyer 买家订单列表（我的订单），稳定序：created_at DESC。
func (r *MarketEntryRepository) ListOrdersByBuyer(ctx context.Context, buyerID int64, limit int) ([]*model.MarketEntryOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []*model.MarketEntryOrder
	err := r.db.WithContext(ctx).Model(&model.MarketEntryOrder{}).
		Where("buyer_id = ?", buyerID).
		Order("created_at DESC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListPaidEntriesByBuyer 买家已支付的条目 ID 集（我的已获得）。
func (r *MarketEntryRepository) ListPaidEntriesByBuyer(ctx context.Context, buyerID int64) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.MarketEntryOrder{}).
		Where("buyer_id = ? AND status = ?", buyerID, model.MarketOrderPaid).
		Order("paid_at DESC").Pluck("entry_id", &ids).Error
	return ids, err
}

// ListAllOrders 管理端订单列表（可按状态过滤），稳定序：created_at DESC。
func (r *MarketEntryRepository) ListAllOrders(ctx context.Context, status string, limit int) ([]*model.MarketEntryOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []*model.MarketEntryOrder
	q := r.db.WithContext(ctx).Model(&model.MarketEntryOrder{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// UpdateOrderStatus 按**当前状态**做条件跃迁（与目录条目同一乐观并发口径）。
func (r *MarketEntryRepository) UpdateOrderStatus(ctx context.Context, orderNo, from, to string, patch map[string]any) (bool, error) {
	sets := map[string]any{"status": to}
	for k, v := range patch {
		sets[k] = v
	}
	res := r.db.WithContext(ctx).Model(&model.MarketEntryOrder{}).
		Where("order_no = ? AND status = ?", orderNo, from).Updates(sets)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
