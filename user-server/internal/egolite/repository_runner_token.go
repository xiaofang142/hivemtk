package egolite

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	hrepo "hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// runner 令牌仓储（egolite_runner_tokens）。
//
// 与 KV 单值 token 的分工（两者并存，不是替换）：
//   - 本仓储：正常路径。每用户可签多条，撤销/轮换是行状态，库内只有 sha256；
//   - KV（ego_token.go）：遗留/兜底路径。老部署没有令牌行时仍按 KV 双候选校验，
//     保证升级不打断已在跑的 runner；一旦某个用户签了新令牌，就走行校验。
//
// 校验顺序：行（摘要命中）→ KV（双候选常量时间比较）→ 拒。两条都是 fail-closed。
type EgoRunnerTokenRepository interface {
	// Issue 签发一条新令牌；返回明文（仅此一次）与行记录（含 hint）。
	Issue(ctx context.Context, userID uint, label string) (string, *model.EgoliteRunnerToken, error)
	// ListByUser 该用户全部令牌（含已撤销，前端按 revoked_at 置灰）
	ListByUser(ctx context.Context, userID uint) ([]*model.EgoliteRunnerToken, error)
	// FindActiveByPlain 明文 → 行（内部做 sha256；无命中返回 ErrExecutionNotFound 同族）
	FindActiveByPlain(ctx context.Context, plain string) (*model.EgoliteRunnerToken, error)
	// Revoke 撤销（归属校验；不存在/非本人 → ErrExecutionNotFound）
	Revoke(ctx context.Context, id, userID uint) error
	// Touch 记录最近使用（弱写，失败不影响主流程）
	Touch(ctx context.Context, id uint) error
}

type egoRunnerTokenRepo struct{ db *gorm.DB }

func NewEgoRunnerTokenRepositoryWithDB(db *gorm.DB) EgoRunnerTokenRepository {
	return &egoRunnerTokenRepo{db: db}
}

// hashEgoToken 令牌摘要（sha256 十六进制；token 本身是高熵随机串，无需加盐慢哈希）
func hashEgoToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// egoTokenHint 展示前缀：保留 "eg_<uid>_" 与人眼可辨的 4 位，其余以 … 收尾。
func egoTokenHint(plain string) string {
	if len(plain) <= 12 {
		return plain
	}
	return plain[:12] + "…"
}

func (r *egoRunnerTokenRepo) Issue(ctx context.Context, userID uint, label string) (string, *model.EgoliteRunnerToken, error) {
	if userID == 0 {
		return "", nil, fmt.Errorf("userID 必填")
	}
	plain, err := GenerateEgoToken(userID)
	if err != nil {
		return "", nil, err
	}
	rec := &model.EgoliteRunnerToken{
		UserID:    userID,
		TokenHash: hashEgoToken(plain),
		TokenHint: egoTokenHint(plain),
		Label:     strings.TrimSpace(label),
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return "", nil, err
	}
	return plain, rec, nil
}

func (r *egoRunnerTokenRepo) ListByUser(ctx context.Context, userID uint) ([]*model.EgoliteRunnerToken, error) {
	var list []*model.EgoliteRunnerToken
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Limit(50).Find(&list).Error
	return list, err
}

func (r *egoRunnerTokenRepo) FindActiveByPlain(ctx context.Context, plain string) (*model.EgoliteRunnerToken, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil, ErrExecutionNotFound
	}
	var rec model.EgoliteRunnerToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", hashEgoToken(plain)).
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	// 摘要查表已足够，但仍做一次常量时间比较（纵深：防止将来把查询改成前缀匹配）
	if subtle.ConstantTimeCompare([]byte(rec.TokenHash), []byte(hashEgoToken(plain))) != 1 {
		return nil, ErrExecutionNotFound
	}
	return &rec, nil
}

func (r *egoRunnerTokenRepo) Revoke(ctx context.Context, id, userID uint) error {
	res := r.db.WithContext(ctx).Model(&model.EgoliteRunnerToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

func (r *egoRunnerTokenRepo) Touch(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.EgoliteRunnerToken{}).
		Where("id = ?", id).Update("last_seen_at", time.Now()).Error
}

// egoTokenAuthenticator runner 通道的统一鉴权入口（WS 握手与资产拉取共用一处）。
//
// 三条路径按序短路：令牌行（多用户正常路径）→ KV 双候选（老部署兼容）→ 拒。
// 单点实现的意义：pull 与 ws 一旦各写一份，迟早出现"能拉清单但连不上 ws"。
type egoTokenAuthenticator struct {
	tokenRepo EgoRunnerTokenRepository
	kvRepo    hrepo.PlatformConfigKVRepository
}

// NewEgoTokenAuthenticator 构造（tokenRepo 可为 nil——此时退化为纯 KV 兼容模式）
func NewEgoTokenAuthenticator(tokenRepo EgoRunnerTokenRepository, kvRepo hrepo.PlatformConfigKVRepository) *egoTokenAuthenticator {
	return &egoTokenAuthenticator{tokenRepo: tokenRepo, kvRepo: kvRepo}
}

// Authenticate 返回归属 userID 与是否通过；同时刷新 last_seen（弱写）。
func (a *egoTokenAuthenticator) Authenticate(ctx context.Context, plain string) (uint, bool) {
	if a == nil {
		return 0, false
	}
	if a.tokenRepo != nil {
		if rec, err := a.tokenRepo.FindActiveByPlain(ctx, plain); err == nil && rec != nil {
			_ = a.tokenRepo.Touch(ctx, rec.ID)
			return rec.UserID, true
		}
	}
	if a.kvRepo != nil {
		return ValidateEgoToken(ctx, a.kvRepo, plain)
	}
	return 0, false
}
