package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TelegramGroupGateRepository TG 群组管控网关仓库
type TelegramGroupGateRepository struct {
	db *gorm.DB
}

func NewTelegramGroupGateRepository() *TelegramGroupGateRepository {
	return &TelegramGroupGateRepository{db: _db.GetDB()}
}

func NewTelegramGroupGateRepositoryWithDB(db *gorm.DB) *TelegramGroupGateRepository {
	return &TelegramGroupGateRepository{db: db}
}

func (r *TelegramGroupGateRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *TelegramGroupGateRepository) Create(ctx context.Context, gate *model.TelegramGroupGate) error {
	return r.db.WithContext(ctx).Create(gate).Error
}

func (r *TelegramGroupGateRepository) Update(ctx context.Context, gate *model.TelegramGroupGate) error {
	return r.db.WithContext(ctx).Save(gate).Error
}

func (r *TelegramGroupGateRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.TelegramGroupGate{}, id).Error
}

func (r *TelegramGroupGateRepository) GetByID(ctx context.Context, id uint) (*model.TelegramGroupGate, error) {
	var gate model.TelegramGroupGate
	if err := r.db.WithContext(ctx).First(&gate, id).Error; err != nil {
		return nil, err
	}
	return &gate, nil
}

func (r *TelegramGroupGateRepository) GetByChatID(ctx context.Context, accountID uint, chatID string) (*model.TelegramGroupGate, error) {
	var gate model.TelegramGroupGate
	if err := r.db.WithContext(ctx).Where("account_id = ? AND chat_id = ?", accountID, chatID).First(&gate).Error; err != nil {
		return nil, err
	}
	return &gate, nil
}

func (r *TelegramGroupGateRepository) List(ctx context.Context, accountID uint) ([]*model.TelegramGroupGate, error) {
	var gates []*model.TelegramGroupGate
	q := r.db.WithContext(ctx)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.Order("id DESC").Find(&gates).Error; err != nil {
		return nil, err
	}
	return gates, nil
}

func (r *TelegramGroupGateRepository) ListEnabled(ctx context.Context) ([]*model.TelegramGroupGate, error) {
	var gates []*model.TelegramGroupGate
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&gates).Error; err != nil {
		return nil, err
	}
	return gates, nil
}

// TelegramGroupMemberRepository TG 群成员验证台账仓库
type TelegramGroupMemberRepository struct {
	db *gorm.DB
}

func NewTelegramGroupMemberRepository() *TelegramGroupMemberRepository {
	return &TelegramGroupMemberRepository{db: _db.GetDB()}
}

func NewTelegramGroupMemberRepositoryWithDB(db *gorm.DB) *TelegramGroupMemberRepository {
	return &TelegramGroupMemberRepository{db: db}
}

func (r *TelegramGroupMemberRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Upsert 按 (account_id, chat_id, user_id) 幂等写入成员记录
func (r *TelegramGroupMemberRepository) Upsert(ctx context.Context, m *model.TelegramGroupMember) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "chat_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "full_name", "join_status", "join_mode", "verify_token", "expires_at", "updated_at"}),
	}).Create(m).Error
}

func (r *TelegramGroupMemberRepository) Update(ctx context.Context, m *model.TelegramGroupMember) error {
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *TelegramGroupMemberRepository) Get(ctx context.Context, accountID uint, chatID, userID string) (*model.TelegramGroupMember, error) {
	var m model.TelegramGroupMember
	if err := r.db.WithContext(ctx).Where("account_id = ? AND chat_id = ? AND user_id = ?", accountID, chatID, userID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetByToken 按 /start <token> 查找待验证成员
func (r *TelegramGroupMemberRepository) GetByToken(ctx context.Context, token string) (*model.TelegramGroupMember, error) {
	var m model.TelegramGroupMember
	if err := r.db.WithContext(ctx).Where("verify_token = ? AND authorized = ?", token, false).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListExpired 找出超时未验证的成员（用于 TTL 清扫）
func (r *TelegramGroupMemberRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]*model.TelegramGroupMember, error) {
	var members []*model.TelegramGroupMember
	q := r.db.WithContext(ctx).Where("authorized = ? AND join_status IN ? AND expires_at IS NOT NULL AND expires_at < ?",
		false, []string{model.TGMemberPending, model.TGMemberRestricted}, now)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListStalledRestricted 找出"禁言中、未验证且 expires_at 早于 before"的成员（入群补偿循环用）
func (r *TelegramGroupMemberRepository) ListStalledRestricted(ctx context.Context, before time.Time, limit int) ([]*model.TelegramGroupMember, error) {
	var members []*model.TelegramGroupMember
	q := r.db.WithContext(ctx).Where("join_status = ? AND authorized = ? AND expires_at IS NOT NULL AND expires_at < ?",
		model.TGMemberRestricted, false, before)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListByChat 群成员台账列表（管理端）
func (r *TelegramGroupMemberRepository) ListByChat(ctx context.Context, accountID uint, chatID string, status string, limit, offset int) ([]*model.TelegramGroupMember, int64, error) {
	var members []*model.TelegramGroupMember
	var total int64
	q := r.db.WithContext(ctx).Model(&model.TelegramGroupMember{}).Where("account_id = ? AND chat_id = ?", accountID, chatID)
	if status != "" {
		q = q.Where("join_status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&members).Error; err != nil {
		return nil, 0, err
	}
	return members, total, nil
}

// GetMemberByID 按主键读取成员台账（管理端人工放行兜底用）
func (r *TelegramGroupMemberRepository) GetMemberByID(ctx context.Context, memberID uint) (*model.TelegramGroupMember, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var member model.TelegramGroupMember
	if err := r.db.WithContext(ctx).First(&member, memberID).Error; err != nil {
		return nil, err
	}
	return &member, nil
}
