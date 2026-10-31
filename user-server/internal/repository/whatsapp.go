package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsappRepository interface {
	CreateAccount(ctx context.Context, acc *model.WhatsappAccount) error
	UpdateAccount(ctx context.Context, acc *model.WhatsappAccount) error
	DeleteAccount(ctx context.Context, id uuid.UUID) error
	ListAccounts(ctx context.Context) ([]*model.WhatsappAccount, error)
	GetAccount(ctx context.Context, id uuid.UUID) (*model.WhatsappAccount, error)

	UpsertSession(ctx context.Context, sess *model.WhatsappSession) error
	GetSession(ctx context.Context, accountID uuid.UUID) (*model.WhatsappSession, error)

	CreateDraft(ctx context.Context, d *model.WhatsappDraft) error
	UpdateDraft(ctx context.Context, d *model.WhatsappDraft) error
	DeleteDraft(ctx context.Context, id uuid.UUID) error
	ListDrafts(ctx context.Context, page, pageSize int) ([]*model.WhatsappDraft, int64, error)
	GetDraft(ctx context.Context, id uuid.UUID) (*model.WhatsappDraft, error)

	CreateJob(ctx context.Context, j *model.WhatsappJob) error
	UpdateJob(ctx context.Context, j *model.WhatsappJob) error
	DeleteJob(ctx context.Context, id uuid.UUID) error
	ListJobs(ctx context.Context, page, pageSize int) ([]*model.WhatsappJob, int64, error)
	GetJob(ctx context.Context, id uuid.UUID) (*model.WhatsappJob, error)

	CreateJobDetail(ctx context.Context, d *model.WhatsappJobDetail) error
	UpdateJobDetail(ctx context.Context, d *model.WhatsappJobDetail) error
}

type whatsappRepo struct {
	db *gorm.DB
}

func NewWhatsappRepository() WhatsappRepository {
	return &whatsappRepo{db: _db.GetDB()}
}

func (r *whatsappRepo) CreateAccount(ctx context.Context, acc *model.WhatsappAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *whatsappRepo) UpdateAccount(ctx context.Context, acc *model.WhatsappAccount) error {
	return r.db.WithContext(ctx).Model(&model.WhatsappAccount{}).Where("id = ?", acc.ID).Updates(acc).Error
}

func (r *whatsappRepo) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	// 0 行必须报 not found：controller 据此回 404。修前直接返回 Delete 的 nil 错误，
	// 「删一个不存在的账号」也回 200「删除成功」（A2 同族，现场实测）。
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WhatsappAccount{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *whatsappRepo) ListAccounts(ctx context.Context) ([]*model.WhatsappAccount, error) {
	var list []*model.WhatsappAccount
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&list).Error
	return list, err
}

func (r *whatsappRepo) GetAccount(ctx context.Context, id uuid.UUID) (*model.WhatsappAccount, error) {
	var acc model.WhatsappAccount
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&acc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &acc, err
}

// UpsertSession 按 account_id 幂等保存会话凭证。
//
// 已知竞态（记录不修）：read-then-write 窗口里并发 Upsert 同一账号会有一方
// Create 撞 account_id 唯一约束（若建了索引）或双写两行。接线幂等语义时应改
// `ON CONFLICT (account_id) DO UPDATE` 一条语句；当前唯一调用方是登录流程
// （单账号单次会话），窗口期撞上的概率可忽略。
func (r *whatsappRepo) UpsertSession(ctx context.Context, sess *model.WhatsappSession) error {
	var exist model.WhatsappSession
	err := r.db.WithContext(ctx).Where("account_id = ?", sess.AccountID).First(&exist).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(sess).Error
	}
	if err != nil {
		return err
	}
	sess.ID = exist.ID
	return r.db.WithContext(ctx).Model(&model.WhatsappSession{}).Where("id = ?", sess.ID).Updates(sess).Error
}

func (r *whatsappRepo) GetSession(ctx context.Context, accountID uuid.UUID) (*model.WhatsappSession, error) {
	var sess model.WhatsappSession
	err := r.db.WithContext(ctx).Where("account_id = ?", accountID).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &sess, err
}

func (r *whatsappRepo) CreateDraft(ctx context.Context, d *model.WhatsappDraft) error {
	return r.db.WithContext(ctx).Create(d).Error
}
func (r *whatsappRepo) UpdateDraft(ctx context.Context, d *model.WhatsappDraft) error {
	return r.db.WithContext(ctx).Model(&model.WhatsappDraft{}).Where("id = ?", d.ID).Updates(d).Error
}
func (r *whatsappRepo) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WhatsappDraft{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// page/pageSize 由调用方归一化后传入（controller 层缺省 10、封顶 50）。
func (r *whatsappRepo) ListDrafts(ctx context.Context, page, pageSize int) ([]*model.WhatsappDraft, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.WhatsappDraft{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.WhatsappDraft
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}
func (r *whatsappRepo) GetDraft(ctx context.Context, id uuid.UUID) (*model.WhatsappDraft, error) {
	var d model.WhatsappDraft
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &d, err
}

func (r *whatsappRepo) CreateJob(ctx context.Context, j *model.WhatsappJob) error {
	return r.db.WithContext(ctx).Create(j).Error
}
func (r *whatsappRepo) UpdateJob(ctx context.Context, j *model.WhatsappJob) error {
	return r.db.WithContext(ctx).Model(&model.WhatsappJob{}).Where("id = ?", j.ID).Updates(j).Error
}
func (r *whatsappRepo) DeleteJob(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WhatsappJob{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// page/pageSize 由调用方归一化后传入（controller 层缺省 10、封顶 50）。
func (r *whatsappRepo) ListJobs(ctx context.Context, page, pageSize int) ([]*model.WhatsappJob, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.WhatsappJob{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.WhatsappJob
	err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}
func (r *whatsappRepo) GetJob(ctx context.Context, id uuid.UUID) (*model.WhatsappJob, error) {
	var j model.WhatsappJob
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&j).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &j, err
}

func (r *whatsappRepo) CreateJobDetail(ctx context.Context, d *model.WhatsappJobDetail) error {
	return r.db.WithContext(ctx).Create(d).Error
}
func (r *whatsappRepo) UpdateJobDetail(ctx context.Context, d *model.WhatsappJobDetail) error {
	return r.db.WithContext(ctx).Model(&model.WhatsappJobDetail{}).Where("id = ?", d.ID).Updates(d).Error
}
