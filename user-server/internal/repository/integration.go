package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"time"

	"gorm.io/gorm"
)

type IntegrationAccountRepository struct {
	db *gorm.DB
}

func NewIntegrationAccountRepository() *IntegrationAccountRepository {
	return &IntegrationAccountRepository{
		db: _db.GetDB(),
	}
}

func (r *IntegrationAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// SetIntegrationAccountRepoDB 工具函数
func SetIntegrationAccountRepoDB(r *IntegrationAccountRepository, db *gorm.DB) {
	r.SetDB(context.Background(), db)
}

func (r *IntegrationAccountRepository) Create(ctx context.Context, account *model.IntegrationAccount) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *IntegrationAccountRepository) GetByID(ctx context.Context, id uint) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	err := r.db.WithContext(ctx).First(&account, id).Error
	return &account, err
}

func (r *IntegrationAccountRepository) GetByPlatformAndAccount(ctx context.Context, platform, accountID string) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	q := r.db.WithContext(ctx).Where("platform = ?", platform)
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	err := q.First(&account).Error
	return &account, err
}

// TenantIDByPlatform 由平台名自证回调归属租户（公开 webhook 入口用）。
// 回调到达时租户尚未建立、平台 HMAC 密钥不能区分租户，只能看 integration_accounts：
// 该平台只被一个租户配置过才可自证，多租户共用同一平台名时返回 ok=false。
func (r *IntegrationAccountRepository) TenantIDByPlatform(ctx context.Context, platform string) (uint, bool, error) {
	if platform == "" {
		return 0, false, nil
	}
	var ids []uint
	err := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.IntegrationAccount{}).
		Where("platform = ?", platform).
		Distinct().Limit(2).Pluck("tenant_id", &ids).Error
	if err != nil {
		return 0, false, err
	}
	if len(ids) != 1 || ids[0] == 0 {
		return 0, false, nil
	}
	return ids[0], true, nil
}

// GetByPlatform 获取某平台的对接账号(单租户)
func (r *IntegrationAccountRepository) GetByPlatform(ctx context.Context, platform string) (*model.IntegrationAccount, error) {
	var account model.IntegrationAccount
	err := r.db.WithContext(ctx).Where("platform = ?", platform).First(&account).Error
	return &account, err
}

// GetAll 获取所有对接账号(单租户)
func (r *IntegrationAccountRepository) GetAll(ctx context.Context) ([]*model.IntegrationAccount, error) {
	var accounts []*model.IntegrationAccount
	err := r.db.WithContext(ctx).Find(&accounts).Error
	return accounts, err
}

func (r *IntegrationAccountRepository) Update(ctx context.Context, account *model.IntegrationAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), account)
}

func (r *IntegrationAccountRepository) UpdateToken(ctx context.Context, id uint, token string, expires *time.Time) error {
	return r.db.WithContext(ctx).Model(&model.IntegrationAccount{}).Where("id = ?", id).
		Updates(map[string]any{
			"access_token":  token,
			"token_expires": expires,
		}).Error
}

func (r *IntegrationAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.IntegrationAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *IntegrationAccountRepository) UpdateSyncTime(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.IntegrationAccount{}).Where("id = ?", id).
		Updates(map[string]any{
			"last_sync_at": now,
		}).Error
}

type SyncLogRepository struct {
	db *gorm.DB
}

func NewSyncLogRepository() *SyncLogRepository {
	return &SyncLogRepository{
		db: _db.GetDB(),
	}
}

func (r *SyncLogRepository) Create(ctx context.Context, log *model.SyncLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *SyncLogRepository) GetByID(ctx context.Context, id uint) (*model.SyncLog, error) {
	var log model.SyncLog
	err := r.db.WithContext(ctx).First(&log, id).Error
	return &log, err
}

func (r *SyncLogRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.SyncLog, int64, error) {
	var logs []*model.SyncLog
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.SyncLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&logs).Error

	return logs, total, err
}

func (r *SyncLogRepository) Update(ctx context.Context, log *model.SyncLog) error {
	return saveWholeRow(r.db.WithContext(ctx), log)
}

func (r *SyncLogRepository) UpdateStatus(ctx context.Context, id uint, status int, recordCount int, errorMessage string) error {
	updates := map[string]any{
		"status":        status,
		"record_count":  recordCount,
		"error_message": errorMessage,
	}
	now := time.Now()
	updates["end_time"] = now
	return r.db.WithContext(ctx).Model(&model.SyncLog{}).Where("id = ?", id).Updates(updates).Error
}

type ExternalCustomerRepository struct {
	db *gorm.DB
}

func NewExternalCustomerRepository() *ExternalCustomerRepository {
	return &ExternalCustomerRepository{
		db: _db.GetDB(),
	}
}

func (r *ExternalCustomerRepository) Create(ctx context.Context, customer *model.ExternalCustomer) error {
	return r.db.WithContext(ctx).Create(customer).Error
}

func (r *ExternalCustomerRepository) GetByID(ctx context.Context, id uint) (*model.ExternalCustomer, error) {
	var customer model.ExternalCustomer
	err := r.db.WithContext(ctx).First(&customer, id).Error
	return &customer, err
}

// GetByExternalID 根据外部 ID 获取外部客户
func (r *ExternalCustomerRepository) GetByExternalID(ctx context.Context, platform, externalID string) (*model.ExternalCustomer, error) {
	var customer model.ExternalCustomer
	err := r.db.WithContext(ctx).Where("platform = ? AND external_id = ?", platform, externalID).First(&customer).Error
	return &customer, err
}

// GetAll 获取所有外部客户列表(单租户)
func (r *ExternalCustomerRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ExternalCustomer, int64, error) {
	var customers []*model.ExternalCustomer
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.ExternalCustomer{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&customers).Error

	return customers, total, err
}

// GetByPlatform 获取某平台的外部客户列表(单租户)
func (r *ExternalCustomerRepository) GetByPlatform(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalCustomer, int64, error) {
	var customers []*model.ExternalCustomer
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.ExternalCustomer{}).Where("platform = ?", platform).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where("platform = ?", platform).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&customers).Error

	return customers, total, err
}

// Update 更新外部客户
func (r *ExternalCustomerRepository) Update(ctx context.Context, customer *model.ExternalCustomer) error {
	return saveWholeRow(r.db.WithContext(ctx), customer)
}

// Delete 删除外部客户
func (r *ExternalCustomerRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ExternalCustomer{}, id).Error
}

// ExternalOrderRepository reads/writes external_orders mirror (EC-07).
// Tenant isolation relies on GORM tenant callbacks/scope from ctx; callers must not pass a tenant-less ctx
// in multi-tenant request paths. Buyer ownership is not enforced here (enforce at service/tool layer).
type ExternalOrderRepository struct {
	db *gorm.DB
}

func NewExternalOrderRepository() *ExternalOrderRepository {
	return &ExternalOrderRepository{
		db: _db.GetDB(),
	}
}

func (r *ExternalOrderRepository) Create(ctx context.Context, order *model.ExternalOrder) error {
	return r.db.WithContext(ctx).Create(order).Error
}

func (r *ExternalOrderRepository) GetByID(ctx context.Context, id uint) (*model.ExternalOrder, error) {
	var order model.ExternalOrder
	err := r.db.WithContext(ctx).First(&order, id).Error
	return &order, err
}

// GetByOrderID 按 (platform, order_id) 复合键读一行镜像。
// 三种返回值互不重叠：命中 (行, nil)、未命中 (nil, nil)、读故障 (nil, err)。
// 未命中不得回非 nil 空结构体 + ErrRecordNotFound——调用方普遍只判 nil 丢错误，
// 「查不动库」与「第一次见这单」会被当成同一回事而走 Create，
// 轻则撞唯一键丢单，重则插出同一单的第二行。
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

	if err := r.db.WithContext(ctx).Model(&model.ExternalOrder{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error

	return orders, total, err
}

// GetByPlatform 获取某平台的外部订单列表(单租户)
func (r *ExternalOrderRepository) GetByPlatform(ctx context.Context, platform string, page, pageSize int) ([]*model.ExternalOrder, int64, error) {
	var orders []*model.ExternalOrder
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where("platform = ?", platform).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error

	return orders, total, err
}

// Update 更新外部订单
func (r *ExternalOrderRepository) Update(ctx context.Context, order *model.ExternalOrder) error {
	return saveWholeRow(r.db.WithContext(ctx), order)
}

// Delete 删除外部订单
func (r *ExternalOrderRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ExternalOrder{}, id).Error
}

// GetByCustomer 按客户手机/姓名查询近期外部订单（客服 360 视图 / 答单上下文用）。
// 订单是外部电商同步进来的只读镜像，此处只查询、不写。
func (r *ExternalOrderRepository) GetByCustomer(ctx context.Context, phone, name string) ([]*model.ExternalOrder, error) {
	var orders []*model.ExternalOrder
	if phone == "" && name == "" {
		return orders, nil
	}
	q := r.db.WithContext(ctx).Model(&model.ExternalOrder{})
	if phone != "" {
		q = q.Where("user_phone = ?", phone)
	}
	if name != "" {
		q = q.Where("user_name = ?", name)
	}
	err := q.Order("COALESCE(order_time, created_at) DESC").Limit(50).Find(&orders).Error
	return orders, err
}

type ExternalProductRepository struct {
	db *gorm.DB
}

func NewExternalProductRepository() *ExternalProductRepository {
	return &ExternalProductRepository{
		db: _db.GetDB(),
	}
}

func (r *ExternalProductRepository) Create(ctx context.Context, product *model.ExternalProduct) error {
	return r.db.WithContext(ctx).Create(product).Error
}

func (r *ExternalProductRepository) GetByID(ctx context.Context, id uint) (*model.ExternalProduct, error) {
	var product model.ExternalProduct
	err := r.db.WithContext(ctx).First(&product, id).Error
	return &product, err
}

// GetByProductID 根据外部商品 ID 获取外部商品
func (r *ExternalProductRepository) GetByProductID(ctx context.Context, platform, productID string) (*model.ExternalProduct, error) {
	var product model.ExternalProduct
	err := r.db.WithContext(ctx).Where("platform = ? AND product_id = ?", platform, productID).First(&product).Error
	return &product, err
}

// GetAll 获取所有外部商品列表(单租户)
func (r *ExternalProductRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.ExternalProduct, int64, error) {
	var products []*model.ExternalProduct
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.ExternalProduct{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&products).Error

	return products, total, err
}

// Update 更新外部商品
func (r *ExternalProductRepository) Update(ctx context.Context, product *model.ExternalProduct) error {
	return saveWholeRow(r.db.WithContext(ctx), product)
}

// Delete 删除外部商品
func (r *ExternalProductRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ExternalProduct{}, id).Error
}

// WebhookEventRepository Webhook 事件仓库
type WebhookEventRepository struct {
	db *gorm.DB
}

func NewWebhookEventRepository() *WebhookEventRepository {
	return &WebhookEventRepository{
		db: _db.GetDB(),
	}
}

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
	return r.db.WithContext(ctx).Create(event).Error
}

// GetByID 根据 ID 获取 Webhook 事件
func (r *WebhookEventRepository) GetByID(ctx context.Context, id uint) (*model.WebhookEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var event model.WebhookEvent
	err := r.db.WithContext(ctx).First(&event, id).Error
	return &event, err
}

// Update 更新 Webhook 事件
func (r *WebhookEventRepository) Update(ctx context.Context, event *model.WebhookEvent) error {
	if r == nil || r.db == nil {
		return nil
	}
	return saveWholeRow(r.db.WithContext(ctx), event)
}

// CountUnprocessed 统计未处理的 Webhook 事件数
func (r *WebhookEventRepository) CountUnprocessed(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var c int64
	err := r.db.WithContext(ctx).Model(&model.WebhookEvent{}).Where("processed = ?", false).Count(&c).Error
	return c, err
}

// ListStaleUnprocessedAfter 列出积压的未处理 Webhook 事件候选（id 游标分页）。
// webhook 主链路走进程内 chan（重启即丢），本方法供恢复扫描器兜底重放
// processed=false 的历史事件。仅返回 created_at < olderThan 的行（冷却期，
// 避免与内存队列 fast path 竞争刚落库的事件）。
// 跨实例互斥不在 SQL 层做：行锁覆盖不了处理窗口，由扫描器的 Redis SetNX
// 认领门闸承担；重放副作用幂等性由下游兜底（message_hub 唯一索引 + AI ClaimReply 防重）。
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

// Connected 判 DB 句柄是否已注入。service 层只允许问这一句是非题，
// 不许把 *gorm.DB 句柄拿回自己手里；也没有全局兜底——装配面必须显式注入。
func (r *WebhookEventRepository) Connected() bool {
	return r != nil && r.db != nil
}
