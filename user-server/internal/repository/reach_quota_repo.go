// reach_quota_repo.go 触达硬配额预占仓储（五层 L5）。
//
// 原子性设计：预占在**单个 PG 事务**里完成「锁 counter 行 → 查已用 → 判上限 →
// used+1 → 落预占行」。外部调用（渠道发送）绝不发生在事务内——锁只在判额瞬间持有。
// Redis 只承担 QPS 层，硬额度真源在这里；Redis 不可用不会退化成无限放行。
package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"

	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 配额仓储错误（对外契约）
var (
	// ErrQuotaExceeded 上限已满（本次动作应拒发或持久延后）
	ErrQuotaExceeded = errors.New("reach quota: limit exceeded for scope")
	// ErrQuotaIntentConflict 同一意图幂等键、不同载荷：业务冲突
	ErrQuotaIntentConflict = errors.New("reach quota: intent id reused with different payload")
	// ErrQuotaTokenMismatch 回写令牌不匹配（旧持有者/串账）
	ErrQuotaTokenMismatch = errors.New("reach quota: token mismatch")
	// ErrQuotaInvalidState 当前状态不允许该迁移（如 committed 不可 release）
	ErrQuotaInvalidState = errors.New("reach quota: state transition not allowed")
)

// ReachQuotaRepository 配额预占数据访问
type ReachQuotaRepository struct {
	db *gorm.DB
}

// NewReachQuotaRepository 构造
func NewReachQuotaRepository(db *gorm.DB) *ReachQuotaRepository {
	return &ReachQuotaRepository{db: db}
}

// ReserveRes 预占结果
type ReserveRes struct {
	Token  string
	Reused bool // 同意图同载荷重入：复用既有预占
}

// Reserve 单事务原子预占：intent 幂等 → 锁 counter → 判上限 → used+1 → 落预占行。
// limit<=0 视为该 scope 不设限（直接给 reserved 行，计数照记）。
func (r *ReachQuotaRepository) Reserve(ctx context.Context, scope, day, intentID, payloadHash string, limit int) (ReserveRes, error) {
	if r == nil || r.db == nil {
		return ReserveRes{}, errors.New("reach quota repository not initialized")
	}
	var res ReserveRes
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 意图幂等：同键重入按载荷哈希分流
		var existing model.ReachQuotaReservation
		err := tx.Where("intent_id = ?", intentID).First(&existing).Error
		switch {
		case err == nil:
			if existing.PayloadHash != payloadHash {
				return ErrQuotaIntentConflict
			}
			switch existing.State {
			case model.QuotaStateReserved, model.QuotaStateCommitted, model.QuotaStateUnknown:
				res = ReserveRes{Token: existing.Token, Reused: true}
				return nil
			case model.QuotaStateReleased:
				// 失败重试：复用同一行，回 reserved 重新占额
			default:
				return fmt.Errorf("reach quota: 未知预占状态 %q", existing.State)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 新意图：走正常预占
		default:
			return err
		}

		// 2. 锁 counter 行（不存在则插入空行再锁）——单 scope 单行锁，
		//    多 scope 由调用方按 key 排序后逐个 Reserve，锁顺序确定不成死锁。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ReachQuotaCounter{
			Scope: scope, Day: day,
		}).Error; err != nil {
			return err
		}
		var counter model.ReachQuotaCounter
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope = ? AND day = ?", scope, day).First(&counter).Error; err != nil {
			return err
		}

		// 3. 判上限
		if limit > 0 && counter.Used >= limit && existing.ID == 0 {
			return ErrQuotaExceeded
		}
		// released 复用重占也受上限约束（used 已在 release 时返还，这里是重新计数）
		if limit > 0 && counter.Used >= limit && existing.State == model.QuotaStateReleased {
			return ErrQuotaExceeded
		}

		// 4. used+1 + 落预占行（同一事务）
		if err := tx.Model(&model.ReachQuotaCounter{}).Where("id = ?", counter.ID).
			Update("used", gorm.Expr("used + 1")).Error; err != nil {
			return err
		}
		token := newQuotaToken()
		if existing.ID != 0 {
			// released 复用：原地复活
			if err := tx.Model(&model.ReachQuotaReservation{}).Where("id = ?", existing.ID).
				Updates(map[string]any{"state": model.QuotaStateReserved, "token": token}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(&model.ReachQuotaReservation{
				IntentID: intentID, PayloadHash: payloadHash,
				Scope: scope, Day: day,
				State: model.QuotaStateReserved, Token: token,
			}).Error; err != nil {
				return err
			}
		}
		res = ReserveRes{Token: token}
		return nil
	})
	return res, err
}

// CommitIntent 回执成功：reserved/unknown → committed（幂等，重复 commit 不重复计数）。
func (r *ReachQuotaRepository) CommitIntent(ctx context.Context, intentID, token string) error {
	return r.transition(ctx, intentID, token, model.QuotaStateCommitted)
}

// MarkUnknownIntent 外部结果未知：reserved → unknown（额度保留待核对）。
func (r *ReachQuotaRepository) MarkUnknownIntent(ctx context.Context, intentID, token string) error {
	return r.transition(ctx, intentID, token, model.QuotaStateUnknown)
}

// ReleaseIntent 确定未发送才可释放：reserved → released 并返还额度。
// committed（已发生的动作不得错误返额）与 unknown（结果未知不释放）拒绝。
func (r *ReachQuotaRepository) ReleaseIntent(ctx context.Context, intentID, token string) error {
	if r == nil || r.db == nil {
		return errors.New("reach quota repository not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec model.ReachQuotaReservation
		if err := tx.Where("intent_id = ?", intentID).First(&rec).Error; err != nil {
			return err
		}
		if rec.Token != token {
			return ErrQuotaTokenMismatch
		}
		if rec.State == model.QuotaStateCommitted {
			return ErrQuotaInvalidState // 取消已提交动作不得错误返额
		}
		if rec.State == model.QuotaStateUnknown {
			return ErrQuotaInvalidState // unknown 不释放：可能已发生的动作不能当未发生
		}
		if rec.State == model.QuotaStateReleased {
			return nil // 幂等
		}
		// reserved → released：返还额度（同事务）
		if err := tx.Model(&model.ReachQuotaCounter{}).
			Where("scope = ? AND day = ?", rec.Scope, rec.Day).
			Update("used", gorm.Expr("GREATEST(used - 1, 0)")).Error; err != nil {
			return err
		}
		return tx.Model(&model.ReachQuotaReservation{}).Where("id = ?", rec.ID).
			Update("state", model.QuotaStateReleased).Error
	})
}

// transition CAS 状态迁移（commit / unknown 共用）
func (r *ReachQuotaRepository) transition(ctx context.Context, intentID, token, target string) error {
	if r == nil || r.db == nil {
		return errors.New("reach quota repository not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec model.ReachQuotaReservation
		if err := tx.Where("intent_id = ?", intentID).First(&rec).Error; err != nil {
			return err
		}
		if rec.Token != token {
			return ErrQuotaTokenMismatch
		}
		switch rec.State {
		case target:
			return nil // 幂等：重复回执不重复计数
		case model.QuotaStateReserved, model.QuotaStateUnknown:
			return tx.Model(&model.ReachQuotaReservation{}).Where("id = ?", rec.ID).
				Update("state", target).Error
		default:
			return ErrQuotaInvalidState
		}
	})
}

// GetReservation 读预占（观测/测试）
func (r *ReachQuotaRepository) GetReservation(ctx context.Context, intentID string) (*model.ReachQuotaReservation, error) {
	if r.db == nil {
		return nil, nil
	}
	var rec model.ReachQuotaReservation
	err := r.db.WithContext(ctx).Where("intent_id = ?", intentID).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetCounterUsed 读 scope 当日已用（观测/测试）
func (r *ReachQuotaRepository) GetCounterUsed(ctx context.Context, scope, day string) (int, error) {
	if r.db == nil {
		return 0, nil
	}
	var counter model.ReachQuotaCounter
	err := r.db.WithContext(ctx).Where("scope = ? AND day = ?", scope, day).First(&counter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return counter.Used, nil
}

// newQuotaToken 预占令牌（回写栅栏）
func newQuotaToken() string {
	return uuid.NewString()
}

// ListByIntentPrefix 按意图前缀取全部 scope 预占行（多 scope 提交/释放用）
func (r *ReachQuotaRepository) ListByIntentPrefix(ctx context.Context, prefix string) ([]struct {
	IntentID string
	Token    string
}, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.ReachQuotaReservation
	if err := r.db.WithContext(ctx).Where("intent_id LIKE ?", prefix+"%").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]struct {
		IntentID string
		Token    string
	}, 0, len(rows))
	for _, row := range rows {
		out = append(out, struct {
			IntentID string
			Token    string
		}{IntentID: row.IntentID, Token: row.Token})
	}
	return out, nil
}
