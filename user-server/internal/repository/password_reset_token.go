package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// PasswordResetTokenRepository 密码重置令牌仓储
//
// 负责 password_reset_tokens 表的 CRUD。提供 NewPasswordResetTokenRepository(db)
// 与 NewPasswordResetTokenRepositoryWithTx(tx) 两种构造，便于在事务内复用。
type PasswordResetTokenRepository struct {
	db *gorm.DB
}

// NewPasswordResetTokenRepository 创建密码重置令牌仓储
func NewPasswordResetTokenRepository(db *gorm.DB) *PasswordResetTokenRepository {
	return &PasswordResetTokenRepository{db: db}
}

// NewPasswordResetTokenRepositoryWithTx 在事务中复用仓储
func NewPasswordResetTokenRepositoryWithTx(tx *gorm.DB) *PasswordResetTokenRepository {
	return &PasswordResetTokenRepository{db: tx}
}

// GetUserByEmail 按 email 查 system_user（用于密码重置前置校验）
// 修复 A3：原实现查 model.User（老表），现改为查 model.SystemUser（统一用户表）
func (r *PasswordResetTokenRepository) GetUserByEmail(ctx context.Context, email string) (*model.SystemUser, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("password reset token repository not initialized")
	}
	var user model.SystemUser
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// CountActiveTokensByUserID 统计指定 user 的有效（未使用且未过期）token 数
func (r *PasswordResetTokenRepository) CountActiveTokensByUserID(ctx context.Context, userID string, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("password reset token repository not initialized")
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.PasswordResetToken{}).
		Where("user_id = ? AND used_at IS NULL AND expires_at > ?", userID, now).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Create 创建密码重置令牌
func (r *PasswordResetTokenRepository) Create(ctx context.Context, token *model.PasswordResetToken) error {
	if r == nil || r.db == nil {
		return errors.New("password reset token repository not initialized")
	}
	return r.db.WithContext(ctx).Create(token).Error
}

// GetByToken 按 token 字段查找（唯一索引）
func (r *PasswordResetTokenRepository) GetByToken(ctx context.Context, tokenStr string) (*model.PasswordResetToken, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("password reset token repository not initialized")
	}
	var token model.PasswordResetToken
	if err := r.db.WithContext(ctx).Where("token = ?", tokenStr).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

// MarkUsed 标记令牌已使用
func (r *PasswordResetTokenRepository) MarkUsed(ctx context.Context, id string, usedAt time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("password reset token repository not initialized")
	}
	return r.db.WithContext(ctx).Model(&model.PasswordResetToken{}).
		Where("id = ?", id).
		Update("used_at", usedAt).Error
}

// CleanupExpiredOlderThan 清理超过 threshold（默认 7 天前）且未使用的过期令牌，返回受影响行数
func (r *PasswordResetTokenRepository) CleanupExpiredOlderThan(ctx context.Context, threshold time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("password reset token repository not initialized")
	}
	res := r.db.WithContext(ctx).
		Where("expires_at < ? AND used_at IS NULL", threshold).
		Delete(&model.PasswordResetToken{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// PasswordResetUserTxHelpers 事务内更新用户密码等辅助方法，
// 避免 UserRepository（全局 db）被硬塞到事务中。
type PasswordResetUserTxHelpers struct {
	tx *gorm.DB
}

// NewPasswordResetUserTxHelpers 创建事务内 user 更新辅助器
func NewPasswordResetUserTxHelpers(tx *gorm.DB) *PasswordResetUserTxHelpers {
	return &PasswordResetUserTxHelpers{tx: tx}
}

// UpdatePasswordInTx 事务内更新 system_user 密码
// 修复 A3：原实现查 model.User（老表），现改为查 model.SystemUser（统一用户表）
func (h *PasswordResetUserTxHelpers) UpdatePasswordInTx(ctx context.Context, userID, hashedPassword string) error {
	if h == nil || h.tx == nil {
		return errors.New("password reset tx helper not initialized")
	}

	var uid uint
	if _, err := fmt.Sscanf(userID, "%d", &uid); err != nil {
		return fmt.Errorf("invalid user_id format: %w", err)
	}
	return h.tx.WithContext(ctx).Model(&model.SystemUser{}).
		Where("id = ?", uid).
		Update("password", hashedPassword).Error
}

// RunPasswordResetTransaction 事务封口：标记 token 已用 + 更新用户密码（五层 L5：事务在仓储层编排）
func RunPasswordResetTransaction(ctx context.Context, db *gorm.DB, tokenID, userID, hashedPassword string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewPasswordResetTokenRepositoryWithTx(tx).MarkUsed(ctx, tokenID, time.Now()); err != nil {
			return err
		}
		return NewPasswordResetUserTxHelpers(tx).UpdatePasswordInTx(ctx, userID, hashedPassword)
	})
}
