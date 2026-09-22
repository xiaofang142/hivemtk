package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

// IntegrationAccountRepository 第三方对接账号仓库
type IntegrationAccountRepository struct {
	db *gorm.DB
}

// NewIntegrationAccountRepository 创建第三方对接账号仓库实例
func NewIntegrationAccountRepository() *IntegrationAccountRepository {
	return &IntegrationAccountRepository{
		db: _db.GetDB(),
	}
}

// SetDB 注入 db
//
// 五层架构 §三.5 + §七：仓库方法必须首参为 ctx context.Context。
func (r *IntegrationAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// SetIntegrationAccountRepoDB 工具函数
func SetIntegrationAccountRepoDB(r *IntegrationAccountRepository, db *gorm.DB) {
	r.SetDB(context.Background(), db)
}

// Create 创建对接账号
func (r *IntegrationAccountRepository) Create(ctx context.Context, account *model.IntegrationAccount) error {
	return r.db.Create(account).Error
}

// GetByID 根据 ID 获取对接账号
func (r *IntegrationAccountRepository) GetByID(ctx context.Context, id uint) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	err := r.db.First(&account, id).Error
	return &account, err
}

func (r *IntegrationAccountRepository) GetByPlatformAndAccount(ctx context.Context, platform, accountID string) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	q := r.db.Where("platform = ?", platform)
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	err := q.First(&account).Error
	return &account, err
}

// GetByPlatform 获取某平台的对接账号(单租户)
func (r *IntegrationAccountRepository) GetByPlatform(ctx context.Context, platform string) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	err := r.db.Where("platform = ?", platform).First(&account).Error
	return &account, err
}

// GetAll 获取所有对接账号(单租户)
func (r *IntegrationAccountRepository) GetAll(ctx context.Context) ([]*model.IntegrationAccount, error) {
	var accounts []*model.IntegrationAccount
	err := r.db.Find(&accounts).Error
	return accounts, err
}

// Update 更新对接账号
func (r *IntegrationAccountRepository) Update(ctx context.Context, account *model.IntegrationAccount) error {
	return r.db.Save(account).Error
}

// UpdateToken 更新访问令牌
func (r *IntegrationAccountRepository) UpdateToken(ctx context.Context, id uint, token string, expires *time.Time) error {
	return r.db.Model(&model.IntegrationAccount{}).Where("id = ?", id).
		Updates(map[string]any{
			"access_token":  token,
			"token_expires": expires,
		}).Error
}

// Delete 删除对接账号
func (r *IntegrationAccountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.IntegrationAccount{}, id).Error
}

// UpdateSyncTime 更新同步时间
func (r *IntegrationAccountRepository) UpdateSyncTime(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.Model(&model.IntegrationAccount{}).Where("id = ?", id).
		Updates(map[string]any{
			"last_sync_at": now,
		}).Error
}

// SyncLogRepository 同步日志仓库
type SyncLogRepository struct {
	db *gorm.DB
}

// NewSyncLogRepository 创建同步日志仓库实例
func NewSyncLogRepository() *SyncLogRepository {
	return &SyncLogRepository{
		db: _db.GetDB(),
	}
}

// Create 创建同步日志
func (r *SyncLogRepository) Create(ctx context.Context, log *model.SyncLog) error {
	return r.db.Create(log).Error
}

// GetByID 根据 ID 获取同步日志
func (r *SyncLogRepository) GetByID(ctx context.Context, id uint) (*model.SyncLog, error) {
	var log model.SyncLog
	err := r.db.First(&log, id).Error
	return &log, err
}

// GetAll 获取所有同步日志(单租户)
func (r *SyncLogRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.SyncLog, int64, error) {
	var logs []*model.SyncLog
	var total int64

	if err := r.db.Model(&model.SyncLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&logs).Error

	return logs, total, err
}

// Update 更新同步日志
func (r *SyncLogRepository) Update(ctx context.Context, log *model.SyncLog) error {
	return r.db.Save(log).Error
}

// UpdateStatus 更新同步状态
func (r *SyncLogRepository) UpdateStatus(ctx context.Context, id uint, status int, recordCount int, errorMessage string) error {
	updates := map[string]any{
		"status":        status,
		"record_count":  recordCount,
		"error_message": errorMessage,
	}
	now := time.Now()
	updates["end_time"] = now
	return r.db.Model(&model.SyncLog{}).Where("id = ?", id).Updates(updates).Error
}

// ExternalCustomerRepository 外部客户仓库
type ExternalCustomerRepository struct {
	db *gorm.DB
}

// NewExternalCustomerRepository 创建外部客户仓库实例
func NewExternalCustomerRepository() *ExternalCustomerRepository {
	return &ExternalCustomerRepository{
		db: _db.GetDB(),
	}
}

// Create 创建外部客户
func (r *ExternalCustomerRepository) Create(ctx context.Context, customer *model.ExternalCustomer) error {
	return r.db.Create(customer).Error
}

// GetByID 根据 ID 获取外部客户
func (r *ExternalCustomerRepository) GetByID(ctx context.Context, id uint) (*model.ExternalCustomer, error) {
	var customer model.ExternalCustomer
	err := r.db.First(&customer, id).Error
	return &customer, err
}

// GetByExternalID 根据外部 ID 获取外部客户
func (r *ExternalCustomerRepository) GetByExternalID(ctx context.Context, platform, externalID string) (*model.ExternalCustomer, error) {
	var customer model.ExternalCustomer
	err := r.db.Where("platform = ? AND external_id = ?", platform, externalID).First(&customer).Error
	return &customer, err
}

// GetAll 获取所有外部客户列表(单租户)
func (r *ExternalCustomerRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ExternalCustomer, int64, error) {
	var customers []*model.ExternalCustomer
	var total int64

	if err := r.db.Model(&model.ExternalCustomer{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&customers).Error

	return customers, total, err
}

// GetByPlatform 获取某平台的外部客户列表(单租户)
func (r *ExternalCustomerRepository) GetByPlatform(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalCustomer, int64, error) {
	var customers []*model.ExternalCustomer
	var total int64

	if err := r.db.Model(&model.ExternalCustomer{}).Where("platform = ?", platform).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Where("platform = ?", platform).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&customers).Error

	return customers, total, err
}

// Update 更新外部客户
func (r *ExternalCustomerRepository) Update(ctx context.Context, customer *model.ExternalCustomer) error {
	return r.db.Save(customer).Error
}

// Delete 删除外部客户
func (r *ExternalCustomerRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.ExternalCustomer{}, id).Error
}

// ExternalOrderRepository 外部订单仓库
type ExternalOrderRepository struct {
	db *gorm.DB
}

// NewExternalOrderRepository 创建外部订单仓库实例
func NewExternalOrderRepository() *ExternalOrderRepository {
	return &ExternalOrderRepository{
		db: _db.GetDB(),
	}
}

// Create 创建外部订单
func (r *ExternalOrderRepository) Create(ctx context.Context, order *model.ExternalOrder) error {
	return r.db.Create(order).Error
}

// GetByID 根据 ID 获取外部订单
func (r *ExternalOrderRepository) GetByID(ctx context.Context, id uint) (*model.ExternalOrder, error) {
	var order model.ExternalOrder
	err := r.db.First(&order, id).Error
	return &order, err
}

// GetByOrderID 按 (platform, order_id) 复合键读一行镜像。
//
// 三种返回值互不重叠：命中 (行, nil)、未命中 (nil, nil)、读故障 (nil, err)。
// 老形状是"未命中也回非 nil 空结构体 + ErrRecordNotFound"，而三个调用点全都写成
// `existing, _ :=`（把错误丢掉、只判 nil），于是**查不动库**与**第一次见这单**
// 在判据上长成同一个样子 —— 后果是走 Create：轻则撞唯一键丢单，重则在改成
// 复合键之后插出同一单的第二行。T-P7-02 / G15 第④条。
func (r *ExternalOrderRepository) GetByOrderID(ctx context.Context, platform, orderID string) (*model.ExternalOrder, error) {
	var order model.ExternalOrder
	err := r.db.WithContext(ctx).Where("platform = ? AND order_id = ?", platform, orderID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetAll 获取所有外部订单列表(单租户)
func (r *ExternalOrderRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ExternalOrder, int64, error) {
	var orders []*model.ExternalOrder
	var total int64

	if err := r.db.Model(&model.ExternalOrder{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error

	return orders, total, err
}

// GetByPlatform 获取某平台的外部订单列表(单租户)
func (r *ExternalOrderRepository) GetByPlatform(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalOrder, int64, error) {
	var orders []*model.ExternalOrder
	var total int64

	if err := r.db.Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Where("platform = ?", platform).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error

	return orders, total, err
}

// Update 更新外部订单
func (r *ExternalOrderRepository) Update(ctx context.Context, order *model.ExternalOrder) error {
	return r.db.Save(order).Error
}

// Delete 删除外部订单
func (r *ExternalOrderRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.ExternalOrder{}, id).Error
}

// GetByCustomer 按客户手机/姓名查询近期外部订单（客服 360 视图 / 答单上下文用）。
// 订单是外部电商同步进来的只读镜像，此处只查询、不写。
func (r *ExternalOrderRepository) GetByCustomer(ctx context.Context, phone, name string) ([]*model.ExternalOrder, error) {
	var orders []*model.ExternalOrder
	if phone == "" && name == "" {
		return orders, nil
	}
	q := r.db.Model(&model.ExternalOrder{})
	if phone != "" {
		q = q.Where("user_phone = ?", phone)
	}
	if name != "" {
		q = q.Where("user_name = ?", name)
	}
	err := q.Order("COALESCE(order_time, created_at) DESC").Limit(50).Find(&orders).Error
	return orders, err
}

// ExternalProductRepository 外部商品仓库
type ExternalProductRepository struct {
	db *gorm.DB
}

// NewExternalProductRepository 创建外部商品仓库实例
func NewExternalProductRepository() *ExternalProductRepository {
	return &ExternalProductRepository{
		db: _db.GetDB(),
	}
}

// Create 创建外部商品
func (r *ExternalProductRepository) Create(ctx context.Context, product *model.ExternalProduct) error {
	return r.db.Create(product).Error
}

// GetByID 根据 ID 获取外部商品
func (r *ExternalProductRepository) GetByID(ctx context.Context, id uint) (*model.ExternalProduct, error) {
	var product model.ExternalProduct
	err := r.db.First(&product, id).Error
	return &product, err
}

// GetByProductID 根据外部商品 ID 获取外部商品
func (r *ExternalProductRepository) GetByProductID(ctx context.Context, platform, productID string) (*model.ExternalProduct, error) {
	var product model.ExternalProduct
	err := r.db.Where("platform = ? AND product_id = ?", platform, productID).First(&product).Error
	return &product, err
}

// GetAll 获取所有外部商品列表(单租户)
func (r *ExternalProductRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ExternalProduct, int64, error) {
	var products []*model.ExternalProduct
	var total int64

	if err := r.db.Model(&model.ExternalProduct{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&products).Error

	return products, total, err
}

// Update 更新外部商品
func (r *ExternalProductRepository) Update(ctx context.Context, product *model.ExternalProduct) error {
	return r.db.Save(product).Error
}

// Delete 删除外部商品
func (r *ExternalProductRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.ExternalProduct{}, id).Error
}

// WebhookEventRepository Webhook 事件仓库
type WebhookEventRepository struct {
	db *gorm.DB
}

// NewWebhookEventRepository 创建 Webhook 事件仓库实例
func NewWebhookEventRepository() *WebhookEventRepository {
	return &WebhookEventRepository{
		db: _db.GetDB(),
	}
}

// SetDB 注入 db（用于测试）
//
// 五层架构 §三.5 + §七：仓库方法必须首参为 ctx context.Context。
func (r *WebhookEventRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// SetWebhookEventRepoDB 工具函数（service 层使用）
func SetWebhookEventRepoDB(r *WebhookEventRepository, db *gorm.DB) {
	r.SetDB(context.Background(), db)
}

// Create 创建 Webhook 事件
func (r *WebhookEventRepository) Create(ctx context.Context, event *model.WebhookEvent) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(event).Error
}

// GetByID 根据 ID 获取 Webhook 事件
func (r *WebhookEventRepository) GetByID(ctx context.Context, id uint) (*model.WebhookEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var event model.WebhookEvent
	err := r.db.First(&event, id).Error
	return &event, err
}

// GetByEventID 根据事件 ID 获取 Webhook 事件
func (r *WebhookEventRepository) GetByEventID(ctx context.Context, eventID string) (*model.WebhookEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var event model.WebhookEvent
	err := r.db.Where("event_id = ?", eventID).First(&event).Error
	return &event, err
}

// GetUnprocessed 获取未处理的 Webhook 事件
func (r *WebhookEventRepository) GetUnprocessed(ctx context.Context, platform string, limit int) ([]*model.WebhookEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var events []*model.WebhookEvent
	err := r.db.Where("platform = ? AND processed = ?", platform, false).
		Order("created_at ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// Update 更新 Webhook 事件
func (r *WebhookEventRepository) Update(ctx context.Context, event *model.WebhookEvent) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Save(event).Error
}

// MarkProcessed 标记 Webhook 事件为已处理
func (r *WebhookEventRepository) MarkProcessed(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return nil
	}
	now := time.Now()
	return r.db.Model(&model.WebhookEvent{}).Where("id = ?", id).
		Updates(map[string]any{
			"processed":    true,
			"processed_at": now,
		}).Error
}

// CountUnprocessed 统计未处理的 Webhook 事件数
func (r *WebhookEventRepository) CountUnprocessed(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var c int64
	err := r.db.Model(&model.WebhookEvent{}).Where("processed = ?", false).Count(&c).Error
	return c, err
}

// ListStaleUnprocessed 列出积压的未处理 Webhook 事件候选。
//
// 业务背景：webhook 主链路走进程内 chan（重启即丢），本方法供恢复扫描器
// 兜底重放 processed=false 的历史事件。仅返回 created_at < olderThan 的行
// （冷却期，避免与内存队列 fast path 竞争刚落库的事件，fast path 处理成功
// 即会翻转 processed）。
//
// 跨实例互斥不在 SQL 层做：行锁（FOR UPDATE）在事务提交即释放，覆盖不了
// 处理窗口；真正的互斥由扫描器的 Redis SetNX 认领门闸承担，重放的副作用
// 幂等性由下游兜底（message_hub 唯一索引 + AI ClaimReply 事件级防重）。
func (r *WebhookEventRepository) ListStaleUnprocessed(ctx context.Context, olderThan time.Time, limit int) ([]*model.WebhookEvent, error) {
	return r.ListStaleUnprocessedAfter(ctx, olderThan, 0, limit)
}

// ListStaleUnprocessedAfter 在 ListStaleUnprocessed 基础上支持 id 游标
// （afterID 之后），防止头部反复失败的事件占满批次窗口饿死后续积压。
func (r *WebhookEventRepository) ListStaleUnprocessedAfter(ctx context.Context, olderThan time.Time, afterID uint64, limit int) ([]*model.WebhookEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	q := r.db.WithContext(ctx).
		Where("processed = ? AND created_at < ?", false, olderThan)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	var events []*model.WebhookEvent
	err := q.Order("id ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// GetDB 暴露底层连接（测试辅助：回拨 created_at 等数据构造）。
func (r *WebhookEventRepository) GetDB() *gorm.DB {
	if r == nil {
		return nil
	}
	return r.db
}
