package egolite

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 通用执行能力（会话 + 轮次）存储层。
//
// 与既有 egolite 存储纪律一致：物理新表、列形状独立、append-only 轮次账本；
// session 只是多轮目标视图，终态事实源仍在 egolite_executions。

// ErrSessionNotFound 会话不存在或不属于该用户（越权与不存在同响应）
var ErrSessionNotFound = errors.New("浏览器会话不存在")

// EgoSessionRepository egolite_sessions 读写
type EgoSessionRepository interface {
	Create(ctx context.Context, s *model.EgoliteSession) error
	GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteSession, error)
	GetBySpaceName(ctx context.Context, userID uint, spaceName string) (*model.EgoliteSession, error)
	// GetByExecutionID 轮次终帧回填用：从 execution 行反查会话（一 execution 只属一会话）
	GetByExecutionID(ctx context.Context, userID, executionID uint) (*model.EgoliteSession, error)
	Update(ctx context.Context, id uint, sets map[string]any) error
	// TransitionState 单语句乐观迁移（与执行域同纪律：禁读-判-写）
	TransitionState(ctx context.Context, id uint, from []string, to string, sets map[string]any) error
	ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteSession, int64, error)
	// ListNonTerminal 看门狗/断连收口扫描（状态集合由调用方给）
	ListNonTerminal(ctx context.Context) ([]*model.EgoliteSession, error)
}

type egoSessionRepo struct{ db *gorm.DB }

// NewEgoSessionRepositoryWithDB 显式注入
func NewEgoSessionRepositoryWithDB(db *gorm.DB) EgoSessionRepository {
	return &egoSessionRepo{db: db}
}

func (r *egoSessionRepo) Create(ctx context.Context, s *model.EgoliteSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *egoSessionRepo) GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteSession, error) {
	var s model.EgoliteSession
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *egoSessionRepo) GetBySpaceName(ctx context.Context, userID uint, spaceName string) (*model.EgoliteSession, error) {
	var s model.EgoliteSession
	err := r.db.WithContext(ctx).Where("user_id = ? AND space_name = ?", userID, spaceName).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *egoSessionRepo) GetByExecutionID(ctx context.Context, userID, executionID uint) (*model.EgoliteSession, error) {
	var s model.EgoliteSession
	err := r.db.WithContext(ctx).Where("user_id = ? AND execution_id = ?", userID, executionID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *egoSessionRepo) Update(ctx context.Context, id uint, sets map[string]any) error {
	sets["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.EgoliteSession{}).Where("id = ?", id).Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *egoSessionRepo) TransitionState(ctx context.Context, id uint, from []string, to string, sets map[string]any) error {
	updates := map[string]any{"state": to, "updated_at": time.Now()}
	for k, v := range sets {
		updates[k] = v
	}
	res := r.db.WithContext(ctx).Model(&model.EgoliteSession{}).
		Where("id = ? AND state IN ?", id, from).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *egoSessionRepo) ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteSession, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.EgoliteSession{}).
		Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.EgoliteSession
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *egoSessionRepo) ListNonTerminal(ctx context.Context) ([]*model.EgoliteSession, error) {
	var list []*model.EgoliteSession
	err := r.db.WithContext(ctx).
		Where("state NOT IN ?", []string{"finished", "failed", "stopped", "timeout"}).
		Order("id ASC").Limit(500).Find(&list).Error
	return list, err
}

// EgoRoundRepository egolite_rounds 读写（append-only：只有 Create/UpdateResult/List）
type EgoRoundRepository interface {
	// CreateOrIgnore 重复终帧/重放幂等：唯一键 (tenant, session, round) ON CONFLICT DO NOTHING
	CreateOrIgnore(ctx context.Context, r *model.EgoliteRound) (bool, error)
	UpdateResult(ctx context.Context, sessionID uint, roundNo int, sets map[string]any) error
	GetByRound(ctx context.Context, sessionID uint, roundNo int) (*model.EgoliteRound, error)
	ListBySession(ctx context.Context, sessionID uint, limit int) ([]*model.EgoliteRound, error)
}

type egoRoundRepo struct{ db *gorm.DB }

// NewEgoRoundRepositoryWithDB 显式注入
func NewEgoRoundRepositoryWithDB(db *gorm.DB) EgoRoundRepository {
	return &egoRoundRepo{db: db}
}

func (r *egoRoundRepo) CreateOrIgnore(ctx context.Context, rec *model.EgoliteRound) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "session_id"}, {Name: "round_no"}},
			DoNothing: true,
		}).Create(rec)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *egoRoundRepo) UpdateResult(ctx context.Context, sessionID uint, roundNo int, sets map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.EgoliteRound{}).
		Where("session_id = ? AND round_no = ?", sessionID, roundNo).Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *egoRoundRepo) GetByRound(ctx context.Context, sessionID uint, roundNo int) (*model.EgoliteRound, error) {
	var rec model.EgoliteRound
	err := r.db.WithContext(ctx).Where("session_id = ? AND round_no = ?", sessionID, roundNo).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *egoRoundRepo) ListBySession(ctx context.Context, sessionID uint, limit int) ([]*model.EgoliteRound, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var list []*model.EgoliteRound
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).
		Order("round_no DESC").Limit(limit).Find(&list).Error
	// 返回按轮次升序（时间线阅读顺序），SQL 用倒序 LIMIT 再翻转
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, err
}
