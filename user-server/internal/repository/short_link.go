package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

var errShortLinkUnavailable = errors.New("short link repository not initialized")

type ShortLinkRepository interface {
	Create(ctx context.Context, shortLink *model.ShortLink) error
	Update(ctx context.Context, shortLink *model.ShortLink) error
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.ShortLink, error)
	GetByShortCode(ctx context.Context, shortCode string) (*model.ShortLink, error)
	GetList(ctx context.Context, page, pageSize int, shortCode, originalURL string, status int) ([]*model.ShortLink, int64, error)
	IncreaseClickCount(ctx context.Context, id uint) error
	// TenantIDByShortCode 系统作用域反查短码归属租户；跨租户同码时 ok=false
	TenantIDByShortCode(ctx context.Context, shortCode string) (uint, bool, error)
}

type shortLinkRepository struct {
	db *gorm.DB
}

func NewShortLinkRepository(db *gorm.DB) ShortLinkRepository {
	return &shortLinkRepository{db: db}
}

// NewShortLinkRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
// AV-04c 批次7：service/cards/tiktok_card.go 装配收口——句柄解析收回仓储层。
func NewShortLinkRepositoryDefault() ShortLinkRepository {
	return NewShortLinkRepository(GetDB())
}

func (r *shortLinkRepository) Create(ctx context.Context, shortLink *model.ShortLink) error {
	logger.Infof("创建短链，ShortCode: %s, OriginalURL: %s", shortLink.ShortCode, shortLink.OriginalURL)
	err := r.db.WithContext(ctx).Create(shortLink).Error
	if err != nil {
		logger.Errorf("创建短链失败: %v", err)
		return err
	}
	logger.Infof("创建短链成功，ID: %d", shortLink.ID)
	return nil
}

// Update 更新短链（整行回写）；走 db.SaveWholeRow，裸 Save 命中 0 行会回退成 upsert
// 把句间被删的短链按原 id 复活，而其短码可能已被别的链接占用。
func (r *shortLinkRepository) Update(ctx context.Context, shortLink *model.ShortLink) error {
	if r == nil || r.db == nil {
		return errShortLinkUnavailable
	}
	if shortLink == nil {
		return errors.New("short link: nil 不是更新对象")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), shortLink)
}

// Delete 删除短链。0 行 ⇒ ErrRecordNotFound。
// 本方法只删 short_links 行；访问流水由 service 另调 accessRepo.DeleteByShortLinkID。
func (r *shortLinkRepository) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errShortLinkUnavailable
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Delete(&model.ShortLink{}, id))
}

func (r *shortLinkRepository) GetByID(ctx context.Context, id uint) (*model.ShortLink, error) {
	if r == nil || r.db == nil {
		return nil, errShortLinkUnavailable
	}
	var shortLink model.ShortLink
	err := r.db.WithContext(ctx).First(&shortLink, id).Error
	if err != nil {
		return nil, err
	}
	return &shortLink, nil
}

func (r *shortLinkRepository) GetByShortCode(ctx context.Context, shortCode string) (*model.ShortLink, error) {
	if r == nil || r.db == nil {
		return nil, errShortLinkUnavailable
	}
	var shortLink model.ShortLink
	err := r.db.WithContext(ctx).Where("short_code = ?", shortCode).First(&shortLink).Error
	if err != nil {
		return nil, err
	}
	return &shortLink, nil
}

// TenantIDByShortCode 匿名访问只带短码，租户由短链行自证。
// 短码唯一索引是 (tenant_id, short_code)：同码多租户时拒猜（ok=false）。
func (r *shortLinkRepository) TenantIDByShortCode(ctx context.Context, shortCode string) (uint, bool, error) {
	var ids []uint
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Model(&model.ShortLink{}).
		Distinct("tenant_id").
		Where("short_code = ?", shortCode).
		Limit(2).
		Pluck("tenant_id", &ids).Error; err != nil {
		return 0, false, err
	}
	if len(ids) != 1 || ids[0] == 0 {
		return 0, false, nil
	}
	return ids[0], true, nil
}

func (r *shortLinkRepository) GetList(ctx context.Context, page, pageSize int, shortCode, originalURL string, status int) ([]*model.ShortLink, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errShortLinkUnavailable
	}
	if page < 1 {
		page = 1
	}
	// pageSize = 0 ⇒ GORM 的 Limit(0) 等于不加 limit，列表口必须自行夹取。
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var shortLinks []*model.ShortLink
	var total int64

	// Count 与 Find 各起一条 query：同一条链上先 Count 再 Find 会就地改 Statement。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.ShortLink{})
		if shortCode != "" {
			q = q.Where("short_code LIKE ?", "%"+shortCode+"%")
		}
		if originalURL != "" {
			q = q.Where("original_url LIKE ?", "%"+originalURL+"%")
		}
		if status > 0 {
			q = q.Where("status = ?", status)
		}
		return q
	}

	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := base().Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&shortLinks).Error; err != nil {
		return nil, 0, err
	}

	return shortLinks, total, nil
}

// IncreaseClickCount 点击计数 +1。用 UpdateColumn 刻意不刷新 updated_at：
// 这是匿名跳转路径上最高频的一条写，updated_at 应保持「配置何时被改过」的语义。
func (r *shortLinkRepository) IncreaseClickCount(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errShortLinkUnavailable
	}
	return r.db.WithContext(ctx).Model(&model.ShortLink{}).
		Where("id = ?", id).
		UpdateColumn("click_count", gorm.Expr("click_count + ?", 1)).Error
}
