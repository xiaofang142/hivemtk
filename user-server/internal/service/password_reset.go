package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/bcrypt"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	passwordResetTokenExpiry    = 24 * time.Hour
	passwordResetTokenMaxActive = 3
)

// ErrInvalidResetToken 重置令牌无效/已过期/已使用的统一哨兵错误。
// 控制器据此区分"令牌爆破失败"（计入防爆破计数）与"新密码不合策略"（不计），
// 保持对外的模糊提示不变（不泄露令牌是否存在）。
var ErrInvalidResetToken = errors.New("invalid or expired token")

type PasswordResetService struct {
	emailService *EmailService
	tokenRepo    *repository.PasswordResetTokenRepository
}

func NewPasswordResetService(tokenRepo *repository.PasswordResetTokenRepository) *PasswordResetService {
	return &PasswordResetService{
		emailService: NewEmailService(),
		tokenRepo:    tokenRepo,
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
	ctx, user, err := s.resolveResetUser(ctx, req.Email)
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

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", getFrontendBaseURL(ctx), resetToken.RawToken)
	subject := "重置您的 HiveMTK 密码"
	body := fmt.Sprintf("您请求了密码重置。请点击以下链接完成重置：\n%s\n\n链接将在 24 小时后失效。", resetURL)

	if s.emailService != nil {
		if _, err := s.emailService.Send(ctx, 0, req.Email, subject, body, nil); err != nil {
			// 只记录明文前缀（前 8 位），完整 reset URL 带 token 明文不能落日志
			logger.Ctx(ctx).Warn().Err(err).Str("email", req.Email).
				Str("token_prefix", resetToken.RawToken[:min(8, len(resetToken.RawToken))]).
				Msg("password reset 邮件发送失败，但 token 已生成")
		} else {
			logger.Ctx(ctx).Info().Str("email", req.Email).Msg("password reset 邮件发送成功")
		}
	}
	return nil
}

// resolveResetUser 解析找回口令请求的用户与租户。
//
// 已绑定租户的调用方（后台/测试）按本租户查；匿名入口没有租户线索，
// on 模式先经系统作用域按全局唯一邮箱定位，再 OnlyTenant 钉死归属。
// 定位不到与"邮箱不存在"同一返回：对外仍是模糊提示，不泄露账号是否存在。
func (s *PasswordResetService) resolveResetUser(ctx context.Context, email string) (context.Context, *model.SystemUser, error) {
	if _, bound := tenant.FromContext(ctx); bound {
		user, err := s.tokenRepo.GetUserByEmail(ctx, email)
		return ctx, user, err
	}
	if !tenant.IsMultiTenant() {
		ctx = tenant.ContextWithDefault(ctx)
		user, err := s.tokenRepo.GetUserByEmail(ctx, email)
		return ctx, user, err
	}
	user, err := s.tokenRepo.GetUserByEmailAnyTenant(ctx, email)
	if err != nil {
		return ctx, nil, err
	}
	return tenant.OnlyTenant(ctx, user.TenantID), user, nil
}

func (s *PasswordResetService) ValidateResetToken(ctx context.Context, tokenStr string) (*model.PasswordResetToken, error) {
	if s.tokenRepo == nil {
		return nil, errors.New("password reset service not initialized")
	}
	// 库里那列存的是明文的 SHA-256 ⇒ 查之前先哈希。
	// 反过来说：拿得到库（备份、只读副本、SQL 日志）的人也重放不了链接。
	hashed := model.HashPasswordResetToken(tokenStr)
	var token *model.PasswordResetToken
	var err error
	if _, bound := tenant.FromContext(ctx); bound || !tenant.IsMultiTenant() {
		token, err = s.tokenRepo.GetByToken(ctx, hashed)
	} else {
		// 匿名重置入口：令牌行自身携带 tenant_id，按系统作用域定位后由调用方绑定
		token, err = s.tokenRepo.GetByTokenAnyTenant(ctx, hashed)
	}
	if err != nil {
		return nil, ErrInvalidResetToken
	}
	if !PasswordResetTokenIsValid(token) {
		return nil, ErrInvalidResetToken
	}
	return token, nil
}

func (s *PasswordResetService) ResetPassword(ctx context.Context, req *ResetPasswordRequest) error {
	token, err := s.ValidateResetToken(ctx, req.Token)
	if err != nil {
		return err
	}
	// 后续所有读写（口令策略、令牌标记、system_users 更新、会话吊销）都必须
	// 落在令牌所属租户：匿名请求 ctx 本来无绑定，on 模式下否则 fail-closed。
	ctx = tenant.OnlyTenant(ctx, token.TenantID)
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
	if s.tokenRepo == nil {
		return errors.New("password reset service not initialized")
	}
	if err := s.tokenRepo.RunResetTransaction(ctx, token.ID, token.UserID, hashedPassword); err != nil {
		// 事务里任何一步命中 0 行（令牌已被并发用掉、用户行已不在）都会回滚。
		// 令牌那条尤其要翻译清楚：重放同一枚链接应当得到"令牌无效或已使用"，
		// 而不是把 GORM 的 "record not found" 原样吐给前端。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidResetToken
		}
		return err
	}
	// 邮件重置成功＝账号可能已泄露，旧会话一律吊销（含攻击者手上那把令牌）
	utils.RevokeUserTokens(ctx, uint(uid))

	return nil
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

// getFrontendBaseURL 解析前端站点基址（SaaS 配置面改造 B19，2026-11-17 翻转）：
// 参数中心 urls.frontend_url（按重置用户所属租户） > env FRONTEND_URL（部署级兜底）
// > 代码兜底。参数中心排前是"多租户合理"：某租户在参数中心配了自己的前端站点后，
// 不应被部署 env 的全局单值静默顶掉；种子默认值已改为 ""（未设语义），env 旧行为保留。
//
// ctx 必须带重置用户所属租户（RequestPasswordReset 经 resolveResetUser 绑定）：
// SaaS 模式按租户读覆盖值；无绑定（off 模式或平台级调用）时读平台默认值。
func getFrontendBaseURL(ctx context.Context) string {
	// 读取点形状：GetString( 与 group/key 字面量同一行，满足读取点硬闸。
	if url := GlobalConfigParam().GetString(ctx, "urls", "frontend_url", ""); url != "" {
		return url
	}
	if url := strings.TrimSpace(os.Getenv("FRONTEND_URL")); url != "" {
		return url
	}
	return "http://localhost:3000"
}

// PasswordResetTokenIsValid 令牌是否有效（未过期且未使用）
// 领域判断自 (*model.PasswordResetToken).IsValid 迁入，model 层仅保留数据结构。
func PasswordResetTokenIsValid(t *model.PasswordResetToken) bool {
	return t != nil && time.Now().Before(t.ExpiresAt) && t.UsedAt == nil
}
