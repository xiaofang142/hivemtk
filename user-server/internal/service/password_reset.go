package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/bcrypt"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	passwordResetTokenExpiry    = 24 * time.Hour
	passwordResetTokenMaxActive = 3
)

type PasswordResetService struct {
	emailService *EmailService
	db           *gorm.DB
	tokenRepo    *repository.PasswordResetTokenRepository
}

func NewPasswordResetService(db *gorm.DB) *PasswordResetService {
	return &PasswordResetService{
		emailService: NewEmailService(db),
		db:           db,
		tokenRepo:    repository.NewPasswordResetTokenRepository(db),
	}
}

type RequestPasswordResetRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func (s *PasswordResetService) RequestPasswordReset(ctx context.Context, req *RequestPasswordResetRequest) error {
	if s.tokenRepo == nil {
		return errors.New("password reset service not initialized")
	}
	user, err := s.tokenRepo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		logger.Ctx(ctx).Warn().Str("email", req.Email).Msg("password reset requested for non-existent email")
		return nil
	}

	userIDStr := strconv.FormatUint(uint64(user.ID), 10)

	activeCount, err := s.tokenRepo.CountActiveTokensByUserID(ctx, userIDStr, time.Now())
	if err != nil {
		return fmt.Errorf("failed to count active reset tokens: %w", err)
	}
	if activeCount >= passwordResetTokenMaxActive {
		return errors.New("too many active reset requests, please try again later")
	}
	resetToken := &model.PasswordResetToken{
		UserID:    userIDStr,
		ExpiresAt: time.Now().Add(passwordResetTokenExpiry),
	}
	if err := s.tokenRepo.Create(ctx, resetToken); err != nil {
		return fmt.Errorf("failed to create reset token: %w", err)
	}
	logger.Ctx(ctx).Info().
		Str("user_id", userIDStr).
		Str("email", req.Email).
		Str("token_id", resetToken.ID).
		Msg("password reset token created")

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", getFrontendBaseURL(), resetToken.Token)
	subject := "重置您的 HiveMTK 密码"
	body := fmt.Sprintf("您请求了密码重置。请点击以下链接完成重置：\n%s\n\n链接将在 24 小时后失效。", resetURL)

	if s.emailService != nil {
		if _, err := s.emailService.Send(ctx, 0, req.Email, subject, body, nil); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("email", req.Email).Str("reset_url", resetURL).Msg("password reset 邮件发送失败，但 token 已生成")
		} else {
			logger.Ctx(ctx).Info().Str("email", req.Email).Msg("password reset 邮件发送成功")
		}
	}
	return nil
}

func (s *PasswordResetService) ValidateResetToken(ctx context.Context, tokenStr string) (*model.PasswordResetToken, error) {
	if s.tokenRepo == nil {
		return nil, errors.New("password reset service not initialized")
	}
	token, err := s.tokenRepo.GetByToken(ctx, tokenStr)
	if err != nil {
		return nil, errors.New("invalid or expired token")
	}
	if !PasswordResetTokenIsValid(token) {
		return nil, errors.New("invalid or expired token")
	}
	return token, nil
}

func (s *PasswordResetService) ResetPassword(ctx context.Context, req *ResetPasswordRequest) error {
	token, err := s.ValidateResetToken(ctx, req.Token)
	if err != nil {
		return err
	}
	// 初始超管（id=1）密码受系统级保护，不允许通过邮件重置流程修改
	if token.UserID == strconv.FormatUint(uint64(initialAdminID), 10) {
		return ErrInitialAdminProtected
	}
	policySvc := NewPasswordPolicyService()
	uid, _ := strconv.ParseUint(token.UserID, 10, 64)
	if err := policySvc.ValidatePassword(ctx, req.NewPassword, uint(uid)); err != nil {
		return err
	}
	hashedPassword, err := bcrypt.HashPassword(req.NewPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		tokenRepo := repository.NewPasswordResetTokenRepositoryWithTx(tx)
		if err := tokenRepo.MarkUsed(ctx, token.ID, now); err != nil {
			return err
		}
		userHelper := repository.NewPasswordResetUserTxHelpers(tx)
		if err := userHelper.UpdatePasswordInTx(ctx, token.UserID, hashedPassword); err != nil {
			return err
		}
		logger.Ctx(ctx).Info().
			Str("user_id", token.UserID).
			Str("token_id", token.ID).
			Msg("password reset successfully")
		return nil
	})
}

func (s *PasswordResetService) CleanupExpiredTokens(ctx context.Context) error {
	if s.tokenRepo == nil {
		return errors.New("password reset service not initialized")
	}
	count, err := s.tokenRepo.CleanupExpiredOlderThan(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return err
	}
	logger.Ctx(ctx).Info().Int64("count", count).Msg("cleaned up expired password reset tokens")
	return nil
}

func getFrontendBaseURL() string {
	if url := os.Getenv("FRONTEND_URL"); url != "" {
		return url
	}
	return "http://localhost:3000"
}

// PasswordResetTokenIsValid 令牌是否有效（未过期且未使用）
// 领域判断自 (*model.PasswordResetToken).IsValid 迁入，model 层仅保留数据结构。
func PasswordResetTokenIsValid(t *model.PasswordResetToken) bool {
	return t != nil && time.Now().Before(t.ExpiresAt) && t.UsedAt == nil
}
