package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// WeComAccountRepository 企业微信账号仓库
type WeComAccountRepository struct {
	db *gorm.DB
}

// NewWeComAccountRepository 创建企业微信账号仓库
func NewWeComAccountRepository() *WeComAccountRepository {
	return &WeComAccountRepository{
		db: _db.GetDB(),
	}
}

// SetDB 注入 db（用于测试）
func (r *WeComAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建账号
func (r *WeComAccountRepository) Create(ctx context.Context, account *model.WeComAccount) error {
	return r.db.WithContext(ctx).Create(account).Error
}

// GetByID 根据 ID 获取账号
func (r *WeComAccountRepository) GetByID(ctx context.Context, id uint) (*model.WeComAccount, error) {
	var account model.WeComAccount
	err := r.db.WithContext(ctx).First(&account, id).Error
	return &account, err
}

func (r *WeComAccountRepository) GetByMerchant(ctx context.Context) ([]*model.WeComAccount, error) {
	var accounts []*model.WeComAccount
	err := r.db.WithContext(ctx).Find(&accounts).Error
	return accounts, err
}

// Update 整行回写账号。
//
// 走 db.SaveWholeRow：裸 Save 命中 0 行会回退成 INSERT。唯一调用方 UpdateAccount
// 是 Get 后改字段再回写，若账号在窗口期被删，裸 Save 会把它按原 id 插回——
// 而其回调配置（CallbackToken/EncodingAESKey）可能已被新账号占用。
func (r *WeComAccountRepository) Update(ctx context.Context, account *model.WeComAccount) error {
	if account == nil || account.ID == 0 {
		return errors.New("wecom account: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), account)
}

// UpdateToken 更新访问令牌。0 行 ⇒ ErrRecordNotFound（给已删账号刷令牌不该静默成功，
// 否则同步循环拿着失效 token 每 5 分钟失败一次却查不到原因）。
func (r *WeComAccountRepository) UpdateToken(ctx context.Context, id uint, token string, expires time.Time) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.WeComAccount{}).Where("id = ?", id).
		Updates(map[string]any{
			"access_token":  token,
			"token_expires": expires,
		}))
}

// Delete 删除账号。0 行 ⇒ ErrRecordNotFound。
func (r *WeComAccountRepository) Delete(ctx context.Context, id uint) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Delete(&model.WeComAccount{}, id))
}

// UpdateSyncTime 更新同步时间。0 行 ⇒ ErrRecordNotFound（同 UpdateToken）。
func (r *WeComAccountRepository) UpdateSyncTime(ctx context.Context, id uint) error {
	now := make(map[string]any)
	now["last_sync_at"] = gorm.Expr("NOW()")
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.WeComAccount{}).Where("id = ?", id).Updates(now))
}

// WeComCustomerRepository 企业微信客户仓库
type WeComCustomerRepository struct {
	db *gorm.DB
}

// NewWeComCustomerRepository 创建企业微信客户仓库
func NewWeComCustomerRepository() *WeComCustomerRepository {
	return &WeComCustomerRepository{
		db: _db.GetDB(),
	}
}

// Create 创建客户
func (r *WeComCustomerRepository) Create(ctx context.Context, customer *model.WeComCustomer) error {
	return r.db.WithContext(ctx).Create(customer).Error
}

// GetByID 根据 ID 获取客户
func (r *WeComCustomerRepository) GetByID(ctx context.Context, id uint) (*model.WeComCustomer, error) {
	var customer model.WeComCustomer
	err := r.db.WithContext(ctx).First(&customer, id).Error
	return &customer, err
}

// GetByExternalUserID 根据外部用户 ID 获取客户（独立部署：单租户）
func (r *WeComCustomerRepository) GetByExternalUserID(ctx context.Context, externalUserID string) (*model.WeComCustomer, error) {
	var customer model.WeComCustomer
	err := r.db.WithContext(ctx).Where("external_user_id = ?", externalUserID).First(&customer).Error
	return &customer, err
}

func (r *WeComCustomerRepository) GetByMerchant(ctx context.Context, page, pageSize int) ([]*model.WeComCustomer, int64, error) {
	// 分页夹取：pageSize = 0 ⇒ GORM 的 Limit(0) 等于**不加 limit**，
	// page = 0 ⇒ offset 为负 ⇒ PG 报 "OFFSET must not be negative"。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var customers []*model.WeComCustomer
	var total int64

	// Count 的 error 必须透传：吞掉的话 total=0 而列表有行，分页器当场错乱。
	if err := r.db.WithContext(ctx).Model(&model.WeComCustomer{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&customers).Error

	return customers, total, err
}

// Update 整行回写客户。走 db.SaveWholeRow（理由同账号：SyncCustomers 是
// read-then-Save，行被并发删掉时裸 Save 会把客户按原 id 插回）。
func (r *WeComCustomerRepository) Update(ctx context.Context, customer *model.WeComCustomer) error {
	if customer == nil || customer.ID == 0 {
		return errors.New("wecom customer: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), customer)
}

// WeComGroupRepository 企业微信客户群仓库
type WeComGroupRepository struct {
	db *gorm.DB
}

// NewWeComGroupRepository 创建企业微信客户群仓库
func NewWeComGroupRepository() *WeComGroupRepository {
	return &WeComGroupRepository{
		db: _db.GetDB(),
	}
}

// Create 创建群
func (r *WeComGroupRepository) Create(ctx context.Context, group *model.WeComGroup) error {
	return r.db.WithContext(ctx).Create(group).Error
}

// GetByChatID 根据群 ID 获取群（独立部署：单租户）
func (r *WeComGroupRepository) GetByChatID(ctx context.Context, chatID string) (*model.WeComGroup, error) {
	var group model.WeComGroup
	err := r.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&group).Error
	return &group, err
}

func (r *WeComGroupRepository) GetByMerchant(ctx context.Context, page, pageSize int) ([]*model.WeComGroup, int64, error) {
	// 分页夹取 + Count 错误透传（理由同 WeComCustomerRepository.GetByMerchant）。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var groups []*model.WeComGroup
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.WeComGroup{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&groups).Error

	return groups, total, err
}

// Update 整行回写群。走 db.SaveWholeRow（理由同账号：SyncGroups 是
// read-then-Save，行被并发删掉时裸 Save 会把群按原 id 插回）。
func (r *WeComGroupRepository) Update(ctx context.Context, group *model.WeComGroup) error {
	if group == nil || group.ID == 0 {
		return errors.New("wecom group: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), group)
}

// WeComMessageRepository 企业微信消息仓库
type WeComMessageRepository struct {
	db *gorm.DB
}

// NewWeComMessageRepository 创建企业微信消息仓库
func NewWeComMessageRepository() *WeComMessageRepository {
	return &WeComMessageRepository{
		db: _db.GetDB(),
	}
}

// Create 创建消息
func (r *WeComMessageRepository) Create(ctx context.Context, message *model.WeComMessage) error {
	return r.db.WithContext(ctx).Create(message).Error
}

func (r *WeComMessageRepository) GetByMerchant(ctx context.Context, page, pageSize int) ([]*model.WeComMessage, int64, error) {
	// 分页夹取 + Count 错误透传（理由同 WeComCustomerRepository.GetByMerchant）。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var messages []*model.WeComMessage
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.WeComMessage{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&messages).Error

	return messages, total, err
}

// WeComTagRepository 企业微信标签仓库
type WeComTagRepository struct {
	db *gorm.DB
}

// NewWeComTagRepository 创建企业微信标签仓库实例
func NewWeComTagRepository(db *gorm.DB) *WeComTagRepository {
	return &WeComTagRepository{
		db: db,
	}
}

// NewWeComTagRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7：service/wecom.go 装配收口——句柄解析收回仓储层。
func NewWeComTagRepositoryDefault() *WeComTagRepository {
	return NewWeComTagRepository(GetDB())
}

// Create 创建标签
func (r *WeComTagRepository) Create(ctx context.Context, tag *model.WeComTag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

func (r *WeComTagRepository) GetByMerchant(ctx context.Context) ([]*model.WeComTag, error) {
	var tags []*model.WeComTag
	err := r.db.WithContext(ctx).Find(&tags).Error
	return tags, err
}

// FindByRiskLevels 按风险等级列表筛选账号（私域独立部署：无 merchant_id）
func (r *WeComAccountRepository) FindByRiskLevels(ctx context.Context, riskLevels []string) ([]model.WeComAccount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if len(riskLevels) == 0 {
		return nil, nil
	}
	var accounts []model.WeComAccount
	err := r.db.WithContext(ctx).Where("risk_level IN ?", riskLevels).Find(&accounts).Error
	return accounts, err
}

// FindHealthyAccounts 查询非排除登录状态且风险等级在指定范围内的账号
// excludeLoginStates: 需要排除的登录状态（如 banned/offline）
func (r *WeComAccountRepository) FindHealthyAccounts(ctx context.Context, riskLevels []string, excludeLoginStates []string) ([]model.WeComAccount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if len(riskLevels) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.WeComAccount{}).Where("risk_level IN ?", riskLevels)
	if len(excludeLoginStates) > 0 {
		q = q.Where("login_state NOT IN ?", excludeLoginStates)
	}
	var accounts []model.WeComAccount
	err := q.Find(&accounts).Error
	return accounts, err
}

// FindActiveAccounts 查找 status=1 且 login_state != banned 的账号（降级选号用）。
//
// 场景：SelectHealthyAccount 找不到健康账号时，降级返回任何已启用、未封禁的账号，
// 避免整体失败。返回的结果按 id ASC 排序，保证选号稳定。
func (r *WeComAccountRepository) FindActiveAccounts(ctx context.Context) ([]model.WeComAccount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var accounts []model.WeComAccount
	err := r.db.WithContext(ctx).Model(&model.WeComAccount{}).
		Where("status = ? AND login_state != ?", 1, "banned").
		Order("id ASC").
		Find(&accounts).Error
	return accounts, err
}

// WeComAccountHealthRepository 企业微信账号健康度仓库
type WeComAccountHealthRepository struct {
	db *gorm.DB
}

// NewWeComAccountHealthRepository 创建企业微信账号健康度仓库（绑定全局 DB）
func NewWeComAccountHealthRepository() *WeComAccountHealthRepository {
	return &WeComAccountHealthRepository{
		db: _db.GetDB(),
	}
}

// NewWeComAccountHealthRepositoryWithDB 创建指定数据库连接的健康度仓库
func NewWeComAccountHealthRepositoryWithDB(db *gorm.DB) *WeComAccountHealthRepository {
	return &WeComAccountHealthRepository{db: db}
}

// SetDB 注入 db（用于测试）
func (r *WeComAccountHealthRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建健康度记录
func (r *WeComAccountHealthRepository) Create(ctx context.Context, rec *model.WeComAccountHealth) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("health repo is nil")
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// GetLatestByAccountID 获取账号最新健康度记录
func (r *WeComAccountHealthRepository) GetLatestByAccountID(ctx context.Context, accountID uint) (*model.WeComAccountHealth, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var rec model.WeComAccountHealth
	err := r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("reported_at DESC, id DESC").
		First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListByAccountIDPaged 分页列出账号健康度历史（按 reported_at DESC）
func (r *WeComAccountHealthRepository) ListByAccountIDPaged(ctx context.Context, accountID uint, page, pageSize int) ([]model.WeComAccountHealth, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.WeComAccountHealth{}).Where("account_id = ?", accountID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.WeComAccountHealth
	err := q.Order("reported_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

// UpdateFields 按 ID 更新指定字段（map 形式，支持 gorm.Expr）
// 服务于 WeComAccountHealthService.IncrementSentCount / syncAccountState 等场景
func (r *WeComAccountRepository) UpdateFields(ctx context.Context, id uint, fields map[string]any) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("wecom account repo is nil")
	}
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.WeComAccount{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// UpdateAllFields 批量更新所有账号的指定字段（无 WHERE 条件）
// 服务于 WeComAccountHealthService.ResetDailyQuota（每日凌晨重置所有账号配额）
// 返回受影响行数
func (r *WeComAccountRepository) UpdateAllFields(ctx context.Context, fields map[string]any) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("wecom account repo is nil")
	}
	if len(fields) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.WeComAccount{}).
		Where("1 = 1").
		Updates(fields)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// ListAllOrderByIDDesc 列出所有账号（按 ID DESC 排序）
// 服务于 WeComAccountHealthService.GetHealthSummary / WeComIntegrationService.ListAccountsWithHealth
func (r *WeComAccountRepository) ListAllOrderByIDDesc(ctx context.Context) ([]model.WeComAccount, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var accounts []model.WeComAccount
	err := r.db.WithContext(ctx).Order("id DESC").Find(&accounts).Error
	return accounts, err
}
