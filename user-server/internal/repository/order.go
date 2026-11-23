package repository

import (
	"context"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	_type "hivemtk-user/internal/pkg/utils/type"
	"time"

	"gorm.io/gorm"
)

type OrderRepository interface {
	Create(ctx context.Context, order *model.Order) error
	GetByStringID(ctx context.Context, id string) (*model.Order, error)
	GetOrderList(ctx context.Context, page int, limit int) ([]*model.Order, int64, error)
	Delete(ctx context.Context, id string) error
	UpdateOrderStatusById(ctx context.Context, id string, status _type.OrderStatusType) error
	GetRecentOrderList(ctx context.Context) ([]*model.Order, error)
	GetByTgID(ctx context.Context, tgID int64) ([]*model.Order, error)
	Update(ctx context.Context, order *model.Order) error
	ListByAccountIDs(ctx context.Context, accountIDs []string) ([]*model.Order, error)
}

type orderRepo struct {
	db *gorm.DB
}

func NewOrderRepository() OrderRepository {
	return &orderRepo{db: _db.GetDB()}
}

func NewOrderRepositoryWithDB(db *gorm.DB) OrderRepository {
	return &orderRepo{db: db}
}

// scopeQuery 返回带租户边界的查询。EC-07「跨租户/买家越权读单拒绝」：
// 带租户 ctx 且非系统 scope 时只查该租户；无租户/系统 scope 回落默认租户 1，
// 兼容历史用 context.Background() 与默认 TenantID=1 的调用与数据。
func (r *orderRepo) scopeQuery(ctx context.Context) *gorm.DB {
	if id, ok := tenant.FromContext(ctx); ok && !tenant.IsSystemScope(ctx) && id > 0 {
		return r.db.WithContext(ctx).Where("tenant_id = ?", id)
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", uint(1))
}

func (r *orderRepo) Create(ctx context.Context, order *model.Order) error {
	return r.db.WithContext(ctx).Create(order).Error
}

func (r *orderRepo) GetByStringID(ctx context.Context, id string) (*model.Order, error) {
	var order model.Order
	err := r.scopeQuery(ctx).Where("id = ?", id).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (r *orderRepo) GetOrderList(ctx context.Context, page int, limit int) ([]*model.Order, int64, error) {
	// 夹取分页：page ≤ 0 时 `(page-1)*limit` 是负 offset（PG 直接报错），limit 无上界会整表拖进内存。
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var orders []*model.Order
	var total int64
	q := r.scopeQuery(ctx).Model(&model.Order{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Offset((page - 1) * limit).Limit(limit).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// Delete 0 行 ⇒ 该订单不存在（db.EnsureAffected），不把删错 id 当删除成功。
func (r *orderRepo) Delete(ctx context.Context, id string) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Order{}))
}

func (r *orderRepo) UpdateOrderStatusById(ctx context.Context, id string, status _type.OrderStatusType) error {
	var order model.Order
	err := r.db.WithContext(ctx).First(&order, "id = ?", id).Error
	if err != nil {
		return err
	}
	order.Status = _type.OrderStatusType(status)
	// 读—改—整行回写。0 行只能是这行在两句之间被删（裸 Save 会回退成 INSERT，把删掉的订单复活）。
	return _db.SaveWholeRow(r.db.WithContext(ctx), &order)
}

func (r *orderRepo) GetRecentOrderList(ctx context.Context) ([]*model.Order, error) {
	var orders []*model.Order

	var start_time = time.Now().Add(-time.Minute * 5).Unix()
	var end_time = time.Now().Unix()
	err := r.scopeQuery(ctx).Where("status = ? and create_time > ? and create_time < ?", _type.OrderStatusPending, start_time, end_time).Order("create_time desc").Find(&orders).Error
	return orders, err
}

func (r *orderRepo) GetByTgID(ctx context.Context, tgID int64) ([]*model.Order, error) {
	var orders []*model.Order
	err := r.scopeQuery(ctx).Where("tg_id = ?", tgID).Order("create_time desc").Find(&orders).Error
	return orders, err
}

func (r *orderRepo) Update(ctx context.Context, order *model.Order) error {
	return _db.SaveWholeRow(r.db.WithContext(ctx), order)
}

func (r *orderRepo) ListByAccountIDs(ctx context.Context, accountIDs []string) ([]*model.Order, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	unique := make([]string, 0, len(accountIDs))
	seen := make(map[string]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	var orders []*model.Order
	err := r.db.WithContext(ctx).
		Where("account_id IN ?", unique).
		Order("create_time DESC").
		Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}
