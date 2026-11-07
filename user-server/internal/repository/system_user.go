package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

// ErrLastAdmin 系统至少需要保留一个 admin 账号
//
// 业务语义：DeleteSafe 在检测到要删除的是 admin 且当前 admin 总数 == 1 时，
// 返回该错误供上层转换为 4xx 响应。
var ErrLastAdmin = errors.New("system: cannot delete the last admin")

// SystemUserRepository 系统用户仓储接口
//
// 提供对 model.SystemUser 的完整 CRUD 与查询。
type SystemUserRepository interface {
	Create(ctx context.Context, u *model.SystemUser) error
	Update(ctx context.Context, u *model.SystemUser) error
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.SystemUser, error)
	GetByUsername(ctx context.Context, username string) (*model.SystemUser, error)
	GetByEmail(ctx context.Context, email string) (*model.SystemUser, error)
	List(ctx context.Context, page, pageSize int) ([]*model.SystemUser, int64, error)
	SearchUsers(ctx context.Context, keyword string, offset, limit int) ([]*model.SystemUser, int64, error)
	Count(ctx context.Context) (int64, error)
	UsernameExists(ctx context.Context, username string, excludeID uint) (bool, error)
	EmailExists(ctx context.Context, email string, excludeID uint) (bool, error)
	GetFirstAdminUsername(ctx context.Context) (string, error)
	GetUpdatedAt(ctx context.Context, userID uint) (*time.Time, error)

	ListByRole(ctx context.Context, role string, page, size int) ([]*model.SystemUser, int64, error)
	CountByRole(ctx context.Context, role string) (int64, error)
	CountByRoles(ctx context.Context, roles []string) (map[string]int64, error)
	CountAdmins(ctx context.Context) (int64, error)
	DeleteSafe(ctx context.Context, id uint) error
	SetEnabled(ctx context.Context, id uint, enabled bool) error
	CountEnabledAdmins(ctx context.Context) (int64, error)
	UpdatePassword(ctx context.Context, id uint, hashedPassword string) error

	// 租户身份解析：登录时租户未知，四组方法均显式 SystemScope，结果自带 tenant_id
	ResolveByEmailGlobal(ctx context.Context, email string) (*model.SystemUser, error)
	FindByUsernameAnyTenant(ctx context.Context, username string, limit int) ([]*model.SystemUser, error)
	// FindByUsernameInTenant 按租户内用户名解析登录身份（tenant_code 定向登录通道）
	FindByUsernameInTenant(ctx context.Context, tenantID uint, username string) (*model.SystemUser, error)
	EmailExistsGlobal(ctx context.Context, email string) (bool, error)
	// GetEmailByID 取用户通知邮箱（失败通知收件人解析，AV-04c 批次3），见实现处契约。
	GetEmailByID(ctx context.Context, userID uint) (string, error)
}

type systemUserRepo struct {
	db *gorm.DB
}

// NewSystemUserRepository 构造
func NewSystemUserRepository() SystemUserRepository {
	return &systemUserRepo{db: db.GetDB()}
}

func (r *systemUserRepo) Create(ctx context.Context, u *model.SystemUser) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// Update 整行回写一个系统用户。
//
// 走 db.SaveWholeRow：裸 Save 命中 0 行会回退成 INSERT，把已被删除的账号
// （连同口令串与角色）按原主键复活——一个已撤销的登录身份复活且无任何报错。
func (r *systemUserRepo) Update(ctx context.Context, u *model.SystemUser) error {
	if r == nil || r.db == nil {
		return errors.New("system user repository not initialized")
	}
	if u == nil {
		return errors.New("system user: nil 不是更新对象")
	}
	return db.SaveWholeRow(r.db.WithContext(ctx), u)
}

// Delete 删除一个系统用户。0 行 ⇒ ErrRecordNotFound：
// 「删一条已被别人删掉的用户」不该回成功，否则管理端提示在说谎。
func (r *systemUserRepo) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("system user repository not initialized")
	}
	return db.EnsureAffected(r.db.WithContext(ctx).Delete(&model.SystemUser{}, id))
}

func (r *systemUserRepo) GetByID(ctx context.Context, id uint) (*model.SystemUser, error) {
	var u model.SystemUser
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *systemUserRepo) GetByUsername(ctx context.Context, username string) (*model.SystemUser, error) {
	var u model.SystemUser
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *systemUserRepo) GetByEmail(ctx context.Context, email string) (*model.SystemUser, error) {
	if email == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var u model.SystemUser
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *systemUserRepo) List(ctx context.Context, page, pageSize int) ([]*model.SystemUser, int64, error) {
	var list []*model.SystemUser
	var total int64
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	// 上限 200：列表页直连 HTTP，`pageSize=1000000` 会一次把整张 users 表读进内存。
	if pageSize > 200 {
		pageSize = 200
	}
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := r.db.WithContext(ctx).Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *systemUserRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// SearchUsers 按关键词搜索用户（username/email/real_name 不区分大小写），offset/limit 由调用方分页
func (r *systemUserRepo) SearchUsers(ctx context.Context, keyword string, offset, limit int) ([]*model.SystemUser, int64, error) {
	var users []*model.SystemUser
	var total int64
	// 分页参数由调用方算好传进来（offset/limit），这里只做兜底：
	// `limit = 0` ⇒ GORM 的 Limit(0) 等于**不加 limit**（整表）；
	// `offset < 0` ⇒ PG 报 "OFFSET must not be negative"。
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	// Count 与 Find 必须各起一条 query（见 base()），不能在同一条链上先 Count 再 Find。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.SystemUser{})
		if keyword != "" {
			like := "%" + keyword + "%"
			q = q.Where("username ILIKE ? OR email ILIKE ? OR real_name ILIKE ?", like, like, like)
		}
		return q
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := base().Order("id DESC").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (r *systemUserRepo) UsernameExists(ctx context.Context, username string, excludeID uint) (bool, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.SystemUser{}).Where("username = ?", username)
	if excludeID > 0 {
		q = q.Where("id != ?", excludeID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *systemUserRepo) EmailExists(ctx context.Context, email string, excludeID uint) (bool, error) {
	if email == "" {
		return false, nil
	}
	var count int64
	q := r.db.WithContext(ctx).Model(&model.SystemUser{}).Where("email = ?", email)
	if excludeID > 0 {
		q = q.Where("id != ?", excludeID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetFirstAdminUsername 取系统中第一个超管用户名（按创建时间升序）
//
// 仅服务于 install 状态探针（平台级安装态判断，不属于任何租户）：
// TENANT_MODE=on 时无租户 ctx 的查询会被隔离 callback fail-closed，
// 故与 ResolveByEmailGlobal/FindByUsernameAnyTenant 同口径显式挂 SystemScope。
func (r *systemUserRepo) GetFirstAdminUsername(ctx context.Context) (string, error) {
	var u model.SystemUser
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).Where("role = ?", "admin").Order("id ASC").First(&u).Error; err != nil {
		return "", err
	}
	return u.Username, nil
}

// GetEmailByID 取用户通知邮箱（AV-04c 批次3 自 browser_automation/service/feedback.go
// notifyFailure 下沉——原实现 service 直持 pkgdb.GetDB() 发 Raw SQL）。
//
// 契约（与下沉前直查逐项一致）：
//   - 谓词 id = ? AND email <> ”：空串视同未配置邮箱，返回 ("", nil)
//     （原 len(emails)==0 分支同形）；多行在 id 主键约束下不可能出现，取首行同原 Scan。
//   - 作用域：system_users 是租户表，原 Raw 绕过隔离 callback 做**跨租户全局解析**
//     （browser_tasks.user_id 的归属用户不一定在发起 ctx 的租户里）。显式挂
//     SystemScope 平台级豁免——与 GetFirstAdminUsername/ResolveByEmailGlobal 同口径：
//     on 模式下隔离 callback 对 system scope 不注入条件，全局解析语义保持。
//   - 形态：GORM API（Model+Where）而非保留 Raw——system_users 无软删除列，
//     生成的 SQL 与原 Raw 逐词等价，且过 router 的 S-18 租户字面量盲区扫描
//     （该扫描只对 repository 层生效，下沉后适用）。
//   - 错误原样上抛；调用方（notifyFailure）静默跳过通知，与改前一致。
func (r *systemUserRepo) GetEmailByID(ctx context.Context, userID uint) (string, error) {
	if r == nil || r.db == nil {
		return "", errors.New("system user repository not initialized")
	}
	var emails []string
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Model(&model.SystemUser{}).
		Select("email").
		Where("id = ? AND email <> ''", userID).
		Scan(&emails).Error; err != nil {
		return "", err
	}
	if len(emails) == 0 {
		return "", nil
	}
	return emails[0], nil
}

func (r *systemUserRepo) GetUpdatedAt(ctx context.Context, userID uint) (*time.Time, error) {
	var u model.SystemUser
	if err := r.db.WithContext(ctx).Select("updated_at").First(&u, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	t := u.UpdatedAt
	return &t, nil
}

func (r *systemUserRepo) ListByRole(ctx context.Context, role string, page, size int) ([]*model.SystemUser, int64, error) {
	var list []*model.SystemUser
	var total int64
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	// Count 与 Find 必须各起一条 query（见 base()），不能在同一条链上先 Count 再 Find。
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.SystemUser{}).Where("role = ?", role)
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := base().Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *systemUserRepo) CountByRole(ctx context.Context, role string) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).Where("role = ?", role).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountByRoles 批量统计多个角色的成员数（一次 GROUP BY 替代 N 次 CountByRole，消除角色列表 N+1）。
func (r *systemUserRepo) CountByRoles(ctx context.Context, roles []string) (map[string]int64, error) {
	out := make(map[string]int64, len(roles))
	if len(roles) == 0 {
		return out, nil
	}
	type row struct {
		Role  string
		Count int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Select("role, COUNT(*) as count").
		Where("role IN ?", roles).
		Group("role").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Role] = r.Count
	}
	return out, nil
}

func (r *systemUserRepo) CountAdmins(ctx context.Context) (int64, error) {
	return r.CountByRole(ctx, "admin")
}

func (r *systemUserRepo) DeleteSafe(ctx context.Context, id uint) error {
	var target model.SystemUser
	if err := r.db.WithContext(ctx).First(&target, id).Error; err != nil {
		return fmt.Errorf("query target user: %w", err)
	}
	if target.Role == "admin" {
		count, err := r.CountAdmins(ctx)
		if err != nil {
			return fmt.Errorf("count admins: %w", err)
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	if err := r.db.WithContext(ctx).Delete(&model.SystemUser{}, id).Error; err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (r *systemUserRepo) SetEnabled(ctx context.Context, id uint, enabled bool) error {
	res := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Where("id = ?", id).
		Update("enabled", enabled)
	// 0 行 ⇒ ErrRecordNotFound（本方法既有对外契约），判据统一走 db.EnsureAffected。
	if err := db.EnsureAffected(res); err != nil {
		return fmt.Errorf("update enabled: %w", err)
	}
	return nil
}

func (r *systemUserRepo) CountEnabledAdmins(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Where("role = ? AND enabled = ?", model.SystemUserRoleAdmin, true).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count enabled admins: %w", err)
	}
	return n, nil
}

func (r *systemUserRepo) UpdatePassword(ctx context.Context, id uint, hashedPassword string) error {
	res := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Where("id = ?", id).
		Update("password", hashedPassword)
	// 0 行 ⇒ ErrRecordNotFound：改口令命中 0 行却回成功，等于告诉用户
	// "新口令已生效"而旧口令仍然能登录。
	if err := db.EnsureAffected(res); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// ── 租户身份解析（登录通道）────────────────
//
// 登录发生在「尚未解析出租户」的时刻，因此这四组方法一律显式挂 SystemScope
// 绕过隔离注入：租户由查询结果自身携带，而不是由 ctx 假定。

// ResolveByEmailGlobal 按邮箱全局解析登录身份（邮箱跨租户全局唯一，contracts/api.md）
func (r *systemUserRepo) ResolveByEmailGlobal(ctx context.Context, email string) (*model.SystemUser, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var u model.SystemUser
	// 与 global_uniq_system_users_email 索引同归一化口径（lower/btrim）
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SystemUser{}).
		Where("lower(btrim(email)) = ?", strings.ToLower(email)).
		First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByUsernameAnyTenant 按用户名取跨租户候选（上限 limit）：
// 命中 0=不存在，1=唯一可登录，>1=跨租户歧义（应提示改用邮箱，业务码 4108）
func (r *systemUserRepo) FindByUsernameAnyTenant(ctx context.Context, username string, limit int) ([]*model.SystemUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 2 // 只需区分 0 / 1 / >1
	}
	var out []*model.SystemUser
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SystemUser{}).
		Where("username = ?", username).
		Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// FindByUsernameInTenant 按租户内用户名解析登录身份（tenant_code 定向登录通道）
//
// 用户名唯一性语义是租户内，故 tenant_id + username 即可唯一定位；
// 与同组方法一致显式挂 SystemScope：登录时租户上下文尚未建立，由查询条件显式限定。
// 未命中返回 gorm.ErrRecordNotFound（调用方映射为防枚举文案）。
func (r *systemUserRepo) FindByUsernameInTenant(ctx context.Context, tenantID uint, username string) (*model.SystemUser, error) {
	username = strings.TrimSpace(username)
	if username == "" || tenantID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var u model.SystemUser
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SystemUser{}).
		Where("tenant_id = ? AND username = ?", tenantID, username).
		First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// EmailExistsGlobal 邮箱全局查重（开户 4103 校验；邮箱唯一性语义是跨租户全局）
func (r *systemUserRepo) EmailExistsGlobal(ctx context.Context, email string) (bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return false, nil
	}
	var count int64
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SystemUser{}).
		Where("lower(btrim(email)) = ?", strings.ToLower(email)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

var _ SystemUserRepository = (*systemUserRepo)(nil)
