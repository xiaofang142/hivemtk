package repository

import (
	"context"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

// UnifiedMessageRepository 统一收件箱的读口（写口只有 Create，来自 webhook 入站）。
type UnifiedMessageRepository interface {
	Create(ctx context.Context, msg *model.UnifiedMessage) error
	GetByID(ctx context.Context, id uint) (*model.UnifiedMessage, error)
	GetByMerchant(ctx context.Context, platform model.Platform, page, pageSize int) ([]*model.UnifiedMessage, int64, error)
}

type unifiedMessageRepo struct {
	db *gorm.DB
}

// NewUnifiedMessageRepository 创建统一消息仓库
func NewUnifiedMessageRepository() UnifiedMessageRepository {
	return &unifiedMessageRepo{db: _db.GetDB()}
}

// NewUnifiedMessageRepositoryWithDB 创建指定数据库连接的 UnifiedMessageRepository（测试用）
func NewUnifiedMessageRepositoryWithDB(db *gorm.DB) UnifiedMessageRepository {
	return &unifiedMessageRepo{db: db}
}

func (r *unifiedMessageRepo) Create(ctx context.Context, msg *model.UnifiedMessage) error {
	return r.db.WithContext(ctx).Create(msg).Error
}

func (r *unifiedMessageRepo) GetByID(ctx context.Context, id uint) (*model.UnifiedMessage, error) {
	var msg model.UnifiedMessage
	err := r.db.WithContext(ctx).First(&msg, id).Error
	return &msg, err
}

func (r *unifiedMessageRepo) GetByMerchant(ctx context.Context, platform model.Platform, page, pageSize int) ([]*model.UnifiedMessage, int64, error) {
	// 分页夹取：controller 侧用了 pagination.Parse，但 service/repo 不设防 ——
	// `pageSize = 0` ⇒ GORM 的 Limit(0) 等于**不加 limit**（收件箱只增不减），
	// `page = 0` ⇒ offset 为负 ⇒ PG 报 "OFFSET must not be negative"。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	// Count 与 Find 必须各起一条 query（见 base()），不能在同一条链上先 Count 再 Find。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.UnifiedMessage{})
		if platform != "" {
			q = q.Where("platform = ?", platform)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var messages []*model.UnifiedMessage
	err := base().Offset((page - 1) * pageSize).Limit(pageSize).
		Order("created_at DESC").Find(&messages).Error
	return messages, total, err
}

// PlatformAccountRepository 平台账号仓库接口
type PlatformAccountRepository interface {
	Create(ctx context.Context, account *model.PlatformAccount) error
	GetByID(ctx context.Context, id uint) (*model.PlatformAccount, error)
	GetAll(ctx context.Context) ([]*model.PlatformAccount, error)
	GetByPlatform(ctx context.Context, platform model.Platform) ([]*model.PlatformAccount, error)
	Update(ctx context.Context, account *model.PlatformAccount) error
	Delete(ctx context.Context, id uint) error
	UpdateStatus(ctx context.Context, id uint, status int) error
	UpdateLastSync(ctx context.Context, id uint) error
}

type platformAccountRepo struct {
	db *gorm.DB
}

// NewPlatformAccountRepository 创建平台账号仓库
func NewPlatformAccountRepository() PlatformAccountRepository {
	return &platformAccountRepo{db: _db.GetDB()}
}

func (r *platformAccountRepo) Create(ctx context.Context, account *model.PlatformAccount) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *platformAccountRepo) GetByID(ctx context.Context, id uint) (*model.PlatformAccount, error) {
	var account model.PlatformAccount
	err := r.db.WithContext(ctx).First(&account, id).Error
	return &account, err
}

func (r *platformAccountRepo) GetAll(ctx context.Context) ([]*model.PlatformAccount, error) {
	var accounts []*model.PlatformAccount
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&accounts).Error
	return accounts, err
}

func (r *platformAccountRepo) GetByPlatform(ctx context.Context, platform model.Platform) ([]*model.PlatformAccount, error) {
	var accounts []*model.PlatformAccount
	err := r.db.WithContext(ctx).Where("platform = ?", platform).Find(&accounts).Error
	return accounts, err
}

func (r *platformAccountRepo) Update(ctx context.Context, account *model.PlatformAccount) error {
	return r.db.WithContext(ctx).Save(account).Error
}

func (r *platformAccountRepo) Delete(ctx context.Context, id uint) error {
	// D-35：必须校验 RowsAffected——删除 0 行说明账号不存在，
	// 返回 gorm.ErrRecordNotFound 让 controller 的 HandleDBError 映射 404，
	// 避免「删除成功」假成功误导前端。
	res := r.db.WithContext(ctx).Delete(&model.PlatformAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *platformAccountRepo) UpdateStatus(ctx context.Context, id uint, status int) error {
	return r.db.WithContext(ctx).Model(&model.PlatformAccount{}).Where("id = ?", id).Update("status", status).Error
}

func (r *platformAccountRepo) UpdateLastSync(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.PlatformAccount{}).Where("id = ?", id).Update("last_sync_at", now).Error
}
