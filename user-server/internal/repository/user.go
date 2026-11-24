package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	GetByID(ctx context.Context, id string) (*model.User, error)
	// GetByIDs 按主键批量取用户（选中导出直接查指定 ID，不靠「第一页+内存过滤」，
	// 否则第一页之外的选中项会被静默漏掉）。租户谓词由租户 callback 从 ctx 注入。
	GetByIDs(ctx context.Context, ids []string) ([]*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	GetUserList(ctx context.Context, page int, limit int) ([]*model.User, int64, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, user *model.User) error
	UpdatePassword(ctx context.Context, id string, hashedPassword string) error
	UserIsExist(ctx context.Context, accountID string, tgID int64, FirstName string, LastName string, UserName string) (string, bool)
	UsernameExists(ctx context.Context, username string, excludeID string) (bool, error)
	EmailExists(ctx context.Context, email string, excludeID string) (bool, error)
}

type userRepo struct {
	db *gorm.DB
}

func NewUserRepository() UserRepository {
	return &userRepo{db: _db.GetDB()}
}

// NewUserRepositoryWithDB 注入版构造口（与全仓 *WithDB 同一约定），供装配层与用例传入指定句柄。
func NewUserRepositoryWithDB(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	return &user, err
}

// GetByIDs 按主键批量取用户。空 ID 列表回空切片（不查库，避免 `id IN (NULL)`
// 之类的意外全表）；租户隔离由 GORM 租户 callback 从 ctx 读 tenant_id 注入。
func (r *userRepo) GetByIDs(ctx context.Context, ids []string) ([]*model.User, error) {
	if len(ids) == 0 {
		return []*model.User{}, nil
	}
	var users []*model.User
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	return &user, err
}

func (r *userRepo) GetUserList(ctx context.Context, page int, limit int) ([]*model.User, int64, error) {
	// 分页夹取：`limit = 0` ⇒ GORM 的 Limit(0) 等于**不加 limit**（客户档案全量
	// 泄进内存），`page = 0` ⇒ offset 为负 ⇒ PG 报错。Count 与 Find 各起一条
	// query（判例 #133 / #150）。
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	var users []*model.User
	var total int64

	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// id 作为二级排序键：create_time 相同（同秒批量创建）时防止翻页边界重复/漏行。
	err := r.db.WithContext(ctx).Offset((page - 1) * limit).Limit(limit).
		Order("create_time DESC, id ASC").Find(&users).Error
	return users, total, err
}

func (r *userRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.User{}).Error
}

// Update 整行回写客户档案。
//
// 走 db.SaveWholeRow：裸 Save 命中 0 行会回退成 `INSERT ... ON CONFLICT (id)
// DO UPDATE`（gorm@v1.31.2）。两个调用方（SSO 登录回写 LastLogin、客户 360
// 改档案）都是读出再改，若行在窗口期被删，裸 Save 会把它按原 id 插回来 ——
// 一个「已注销客户」带着旧资料复活。
func (r *userRepo) Update(ctx context.Context, user *model.User) error {
	if user == nil || user.ID == "" {
		return errors.New("user: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), user)
}

func (r *userRepo) UpdatePassword(ctx context.Context, id string, hashedPassword string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Update("password", hashedPassword).Error
}

func (r *userRepo) UserIsExist(ctx context.Context, accountID string, tgID int64, FirstName string, LastName string, UserName string) (string, bool) {
	var user model.User
	err := r.db.WithContext(ctx).Where("account_id = ? and tg_id = ? and first_name = ? and last_name = ? and user_name = ?", accountID, tgID, FirstName, LastName, UserName).First(&user).Error
	if err != nil {
		return "", false
	}
	if user.ID != "" {
		return user.ID, true
	}
	return "", false
}

func (r *userRepo) UsernameExists(ctx context.Context, username string, excludeID string) (bool, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", username)

	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}

	err := query.Count(&count).Error
	return count > 0, err
}

func (r *userRepo) EmailExists(ctx context.Context, email string, excludeID string) (bool, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&model.User{}).Where("email = ?", email)

	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}

	err := query.Count(&count).Error
	return count > 0, err
}
