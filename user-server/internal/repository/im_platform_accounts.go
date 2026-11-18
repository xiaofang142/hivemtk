package repository

import (
	"context"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// im_platform_accounts.go 025-im-platforms 新平台账号仓储。
//
// 每个平台一个类型（不做泛型单仓）：表名与字段各不相同，泛型化只省几行样板，
// 却会让 GetByID 的返回类型变成 any，service 侧每处都要断言。

// WeComCSAccountRepository 企业微信客服账号。
type WeComCSAccountRepository struct{ db *gorm.DB }

func NewWeComCSAccountRepository() *WeComCSAccountRepository {
	return &WeComCSAccountRepository{db: _db.GetDB()}
}

func (r *WeComCSAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *WeComCSAccountRepository) Create(ctx context.Context, acc *model.WeComCSAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *WeComCSAccountRepository) GetByID(ctx context.Context, id uint) (*model.WeComCSAccount, error) {
	var acc model.WeComCSAccount
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *WeComCSAccountRepository) ListActive(ctx context.Context) ([]*model.WeComCSAccount, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var accs []*model.WeComCSAccount
	err := r.db.WithContext(ctx).Where("status = ?", 1).Order("id ASC").Find(&accs).Error
	return accs, err
}

// UpdateSyncCursor 只写游标列，避免整行回写覆盖并发修改的密钥。
func (r *WeComCSAccountRepository) UpdateSyncCursor(ctx context.Context, id uint, cursor string) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(&model.WeComCSAccount{}).Where("id = ?", id).
		Updates(map[string]any{"sync_cursor": cursor, "cursor_updated_at": gorm.Expr("now()")}).Error
}

func (r *WeComCSAccountRepository) Update(ctx context.Context, acc *model.WeComCSAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), acc)
}

func (r *WeComCSAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.WeComCSAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// OneBotAccountRepository OneBot v11 协议端账号。
type OneBotAccountRepository struct{ db *gorm.DB }

func NewOneBotAccountRepository() *OneBotAccountRepository {
	return &OneBotAccountRepository{db: _db.GetDB()}
}

func (r *OneBotAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *OneBotAccountRepository) Create(ctx context.Context, acc *model.OneBotAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *OneBotAccountRepository) GetByID(ctx context.Context, id uint) (*model.OneBotAccount, error) {
	var acc model.OneBotAccount
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *OneBotAccountRepository) GetAll(ctx context.Context) ([]*model.OneBotAccount, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var accs []*model.OneBotAccount
	err := r.db.WithContext(ctx).Order("id DESC").Find(&accs).Error
	return accs, err
}

func (r *OneBotAccountRepository) Update(ctx context.Context, acc *model.OneBotAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), acc)
}

func (r *OneBotAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.OneBotAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SlackAccountRepository Slack app 账号。
type SlackAccountRepository struct{ db *gorm.DB }

func NewSlackAccountRepository() *SlackAccountRepository {
	return &SlackAccountRepository{db: _db.GetDB()}
}

func (r *SlackAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *SlackAccountRepository) Create(ctx context.Context, acc *model.SlackAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *SlackAccountRepository) GetByID(ctx context.Context, id uint) (*model.SlackAccount, error) {
	var acc model.SlackAccount
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *SlackAccountRepository) Update(ctx context.Context, acc *model.SlackAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), acc)
}

func (r *SlackAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.SlackAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// WeCombBotAccountRepository 企微智能机器人账号。
type WeCombBotAccountRepository struct{ db *gorm.DB }

func NewWeCombBotAccountRepository() *WeCombBotAccountRepository {
	return &WeCombBotAccountRepository{db: _db.GetDB()}
}

func (r *WeCombBotAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *WeCombBotAccountRepository) Create(ctx context.Context, acc *model.WeCombBotAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *WeCombBotAccountRepository) GetByID(ctx context.Context, id uint) (*model.WeCombBotAccount, error) {
	var acc model.WeCombBotAccount
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *WeCombBotAccountRepository) Update(ctx context.Context, acc *model.WeCombBotAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), acc)
}

func (r *WeCombBotAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.WeCombBotAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CustomAccountRepository 通用自定义渠道账号。
type CustomAccountRepository struct{ db *gorm.DB }

func NewCustomAccountRepository() *CustomAccountRepository {
	return &CustomAccountRepository{db: _db.GetDB()}
}

func (r *CustomAccountRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *CustomAccountRepository) Create(ctx context.Context, acc *model.CustomAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *CustomAccountRepository) GetByID(ctx context.Context, id uint) (*model.CustomAccount, error) {
	var acc model.CustomAccount
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := r.db.WithContext(ctx).First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *CustomAccountRepository) Update(ctx context.Context, acc *model.CustomAccount) error {
	return saveWholeRow(r.db.WithContext(ctx), acc)
}

func (r *CustomAccountRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.CustomAccount{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
