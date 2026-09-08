package repository

import (
	"context"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// FeishuAccountRepository 飞书账号仓库
type FeishuAccountRepository struct {
	db *gorm.DB
}

// NewFeishuAccountRepository 创建飞书账号仓库
func NewFeishuAccountRepository() *FeishuAccountRepository {
	return &FeishuAccountRepository{db: _db.GetDB()}
}

// SetDB 注入 db（用于测试）
func (r *FeishuAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建飞书账号
func (r *FeishuAccountRepository) Create(ctx context.Context, acc *model.FeishuAccount) error {
	return r.db.Create(acc).Error
}

// GetByID 根据 ID 获取
func (r *FeishuAccountRepository) GetByID(ctx context.Context, id uint) (*model.FeishuAccount, error) {
	var acc model.FeishuAccount
	if err := r.db.First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetByAppID 根据 AppID 获取
func (r *FeishuAccountRepository) GetByAppID(ctx context.Context, appID string) (*model.FeishuAccount, error) {
	var acc model.FeishuAccount
	if err := r.db.Where("app_id = ?", appID).First(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetEnabled 获取所有启用的账号
func (r *FeishuAccountRepository) GetEnabled(ctx context.Context) ([]*model.FeishuAccount, error) {
	var accs []*model.FeishuAccount
	if err := r.db.Where("webhook_enabled = ? AND status = ?", true, 1).Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

// GetAll 获取所有飞书账号
func (r *FeishuAccountRepository) GetAll(ctx context.Context) ([]*model.FeishuAccount, error) {
	var accs []*model.FeishuAccount
	if err := r.db.Order("id DESC").Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

// Update 更新飞书账号
func (r *FeishuAccountRepository) Update(ctx context.Context, acc *model.FeishuAccount) error {
	return r.db.Save(acc).Error
}

// Delete 删除飞书账号
func (r *FeishuAccountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.FeishuAccount{}, id).Error
}

// UpdateToken 更新访问令牌
func (r *FeishuAccountRepository) UpdateToken(ctx context.Context, id uint, token string, expires *int64) error {
	updates := map[string]any{
		"access_token": token,
	}
	if expires != nil {
		updates["token_expires"] = expires
	}
	return r.db.Model(&model.FeishuAccount{}).Where("id = ?", id).Updates(updates).Error
}

// FeishuCustomerRepository 飞书客户仓库
type FeishuCustomerRepository struct {
	db *gorm.DB
}

func NewFeishuCustomerRepository() *FeishuCustomerRepository {
	return &FeishuCustomerRepository{db: _db.GetDB()}
}

func (r *FeishuCustomerRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *FeishuCustomerRepository) Create(ctx context.Context, c *model.FeishuCustomer) error {
	return r.db.Create(c).Error
}

func (r *FeishuCustomerRepository) GetByOpenID(ctx context.Context, accountID uint, openID string) (*model.FeishuCustomer, error) {
	var c model.FeishuCustomer
	if err := r.db.Where("account_id = ? AND open_id = ?", accountID, openID).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *FeishuCustomerRepository) GetByUnionID(ctx context.Context, unionID string) (*model.FeishuCustomer, error) {
	var c model.FeishuCustomer
	if err := r.db.Where("union_id = ?", unionID).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *FeishuCustomerRepository) Update(ctx context.Context, c *model.FeishuCustomer) error {
	return r.db.Save(c).Error
}

// FeishuMessageRepository 飞书消息仓库
type FeishuMessageRepository struct {
	db *gorm.DB
}

func NewFeishuMessageRepository() *FeishuMessageRepository {
	return &FeishuMessageRepository{db: _db.GetDB()}
}

func (r *FeishuMessageRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *FeishuMessageRepository) Create(ctx context.Context, m *model.FeishuMessage) error {
	return r.db.Create(m).Error
}

func (r *FeishuMessageRepository) GetByMsgID(ctx context.Context, msgID string) (*model.FeishuMessage, error) {
	var m model.FeishuMessage
	if err := r.db.Where("msg_id = ?", msgID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// TelegramAccountRepository Telegram 账号仓库
type TelegramAccountRepository struct {
	db *gorm.DB
}

func NewTelegramAccountRepository() *TelegramAccountRepository {
	return &TelegramAccountRepository{db: _db.GetDB()}
}

func (r *TelegramAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *TelegramAccountRepository) Create(ctx context.Context, acc *model.TelegramAccount) error {
	return r.db.Create(acc).Error
}

func (r *TelegramAccountRepository) GetByID(ctx context.Context, id uint) (*model.TelegramAccount, error) {
	var acc model.TelegramAccount
	if err := r.db.First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *TelegramAccountRepository) GetEnabled(ctx context.Context) ([]*model.TelegramAccount, error) {
	var accs []*model.TelegramAccount
	if err := r.db.Where("webhook_enabled = ? AND status = ?", true, 1).Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

func (r *TelegramAccountRepository) GetAll(ctx context.Context) ([]*model.TelegramAccount, error) {
	var accs []*model.TelegramAccount
	if err := r.db.Order("id DESC").Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

func (r *TelegramAccountRepository) Update(ctx context.Context, acc *model.TelegramAccount) error {
	return r.db.Save(acc).Error
}

func (r *TelegramAccountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.TelegramAccount{}, id).Error
}

// WhatsAppCloudAccountRepository WhatsApp Cloud API 账号仓库
type WhatsAppCloudAccountRepository struct {
	db *gorm.DB
}

func NewWhatsAppCloudAccountRepository() *WhatsAppCloudAccountRepository {
	return &WhatsAppCloudAccountRepository{db: _db.GetDB()}
}

func (r *WhatsAppCloudAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *WhatsAppCloudAccountRepository) Create(ctx context.Context, acc *model.WhatsAppCloudAccount) error {
	return r.db.Create(acc).Error
}

func (r *WhatsAppCloudAccountRepository) GetByID(ctx context.Context, id uint) (*model.WhatsAppCloudAccount, error) {
	var acc model.WhatsAppCloudAccount
	if err := r.db.First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *WhatsAppCloudAccountRepository) GetByPhoneNumberID(ctx context.Context, phoneID string) (*model.WhatsAppCloudAccount, error) {
	var acc model.WhatsAppCloudAccount
	if err := r.db.Where("phone_number_id = ?", phoneID).First(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *WhatsAppCloudAccountRepository) GetEnabled(ctx context.Context) ([]*model.WhatsAppCloudAccount, error) {
	var accs []*model.WhatsAppCloudAccount
	if err := r.db.Where("webhook_enabled = ? AND status = ?", true, 1).Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

func (r *WhatsAppCloudAccountRepository) GetAll(ctx context.Context) ([]*model.WhatsAppCloudAccount, error) {
	var accs []*model.WhatsAppCloudAccount
	if err := r.db.Order("id DESC").Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

func (r *WhatsAppCloudAccountRepository) Update(ctx context.Context, acc *model.WhatsAppCloudAccount) error {
	return r.db.Save(acc).Error
}

func (r *WhatsAppCloudAccountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.WhatsAppCloudAccount{}, id).Error
}

// GetFirstEnabled 取第一个启用的飞书账号（webhook_enabled=true 且 status=1）
// 找不到返回 gorm.ErrRecordNotFound
func (r *FeishuAccountRepository) GetFirstEnabled(ctx context.Context) (*model.FeishuAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.FeishuAccount
	err := r.db.WithContext(ctx).
		Where("webhook_enabled = ? AND status = ?", true, 1).
		Order("id ASC").
		First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirst 取第一条飞书账号（兜底，不区分启用状态）
func (r *FeishuAccountRepository) GetFirst(ctx context.Context) (*model.FeishuAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.FeishuAccount
	err := r.db.WithContext(ctx).Order("id ASC").First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirstEnabled 取第一个启用的 Telegram 账号（status=1）
func (r *TelegramAccountRepository) GetFirstEnabled(ctx context.Context) (*model.TelegramAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.TelegramAccount
	err := r.db.WithContext(ctx).
		Where("status = ?", 1).
		Order("id ASC").
		First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirst 取第一条 Telegram 账号（兜底，不区分启用状态）
func (r *TelegramAccountRepository) GetFirst(ctx context.Context) (*model.TelegramAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.TelegramAccount
	err := r.db.WithContext(ctx).Order("id ASC").First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirstEnabled 取第一个启用的 WhatsApp Cloud 账号（status=1）
func (r *WhatsAppCloudAccountRepository) GetFirstEnabled(ctx context.Context) (*model.WhatsAppCloudAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.WhatsAppCloudAccount
	err := r.db.WithContext(ctx).
		Where("status = ?", 1).
		Order("id ASC").
		First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirst 取第一条 WhatsApp Cloud 账号（兜底，不区分启用状态）
func (r *WhatsAppCloudAccountRepository) GetFirst(ctx context.Context) (*model.WhatsAppCloudAccount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var acc model.WhatsAppCloudAccount
	err := r.db.WithContext(ctx).Order("id ASC").First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// ---------------------------------------------------------------------------
// QQAccountRepository QQ 机器人账号仓库
// ---------------------------------------------------------------------------

// QQAccountRepository QQ 账号仓库
type QQAccountRepository struct {
	db *gorm.DB
}

// NewQQAccountRepository 创建 QQ 账号仓库
func NewQQAccountRepository() *QQAccountRepository {
	return &QQAccountRepository{db: _db.GetDB()}
}

// SetDB 注入 db（用于测试）
func (r *QQAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建 QQ 账号
func (r *QQAccountRepository) Create(ctx context.Context, acc *model.QQAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

// GetByID 按 ID 查询
func (r *QQAccountRepository) GetByID(ctx context.Context, id uint) (*model.QQAccount, error) {
	var acc model.QQAccount
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetAll 全量列表（按 ID 倒序）
func (r *QQAccountRepository) GetAll(ctx context.Context) ([]*model.QQAccount, error) {
	var accs []*model.QQAccount
	if err := r.db.WithContext(ctx).Order("id DESC").Find(&accs).Error; err != nil {
		return nil, err
	}
	return accs, nil
}

// Update 更新
func (r *QQAccountRepository) Update(ctx context.Context, acc *model.QQAccount) error {
	return r.db.WithContext(ctx).Save(acc).Error
}

// Delete 删除
func (r *QQAccountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.QQAccount{}, id).Error
}
