// wechat_account_repo.go 微信公众号账号与消息仓储（五层 L5）
package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// WechatAccountRepository 公众号账号 CRUD 与消息落库收口
type WechatAccountRepository struct {
	db *gorm.DB
}

// NewWechatAccountRepository 构造
func NewWechatAccountRepository(db *gorm.DB) *WechatAccountRepository {
	return &WechatAccountRepository{db: db}
}

// List 列出全部公众号账号（id 升序）
func (r *WechatAccountRepository) List(ctx context.Context) ([]model.WechatAccount, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var accounts []model.WechatAccount
	err := r.db.WithContext(ctx).Order("id ASC").Find(&accounts).Error
	return accounts, err
}

// GetByID 按 PK 取账号
func (r *WechatAccountRepository) GetByID(ctx context.Context, id uint) (*model.WechatAccount, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var acc model.WechatAccount
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetFirstActive 取第一个 active 且凭证完整的账号（智能选渠道兜底）
func (r *WechatAccountRepository) GetFirstActive(ctx context.Context) (*model.WechatAccount, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var acc model.WechatAccount
	err := r.db.WithContext(ctx).
		Where("status = ?", "active").
		Where("app_id <> ? AND app_secret <> ?", "", "").
		Order("id ASC").
		First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// Create 创建账号
func (r *WechatAccountRepository) Create(ctx context.Context, acc *model.WechatAccount) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(acc).Error
}

// UpdateCredentials 更新账号凭证字段
func (r *WechatAccountRepository) UpdateCredentials(ctx context.Context, acc *model.WechatAccount) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(acc).Updates(map[string]any{
		"app_id":           acc.AppID,
		"app_secret":       acc.AppSecret,
		"original_id":      acc.OriginalID,
		"token":            acc.Token,
		"encoding_aes_key": acc.EncodingAESKey,
		"agent_id":         acc.AgentID,
		"status":           acc.Status,
	}).Error
}

// Delete 删除账号
func (r *WechatAccountRepository) Delete(ctx context.Context, id uint) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Delete(&model.WechatAccount{}, id).Error
}

// CreateMessage 落库公众号消息（收/发共用）
func (r *WechatAccountRepository) CreateMessage(ctx context.Context, msg *model.WechatMessage) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(msg).Error
}
