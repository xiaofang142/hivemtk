package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
	_pagination "hivemtk-user/internal/pkg/pagination"

	"gorm.io/gorm"
)

type SecurityAuditRepository struct {
	db *gorm.DB
}

// NewSecurityAuditRepository 创建安全审计仓储
func NewSecurityAuditRepository() *SecurityAuditRepository {
	return &SecurityAuditRepository{}
}

// SetDB 注入 db
func (r *SecurityAuditRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// GetDB 获取 db（内部 / service 层 withDB 使用）
func (r *SecurityAuditRepository) GetDB(ctx context.Context) *gorm.DB {
	return r.db
}

// Create 写入一条审计记录
func (r *SecurityAuditRepository) Create(ctx context.Context, a *model.SecurityAudit) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// List 分页查询审计记录
func (r *SecurityAuditRepository) List(ctx context.Context, page, pageSize int) ([]model.SecurityAudit, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.SecurityAudit{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.SecurityAudit
	offset := (page - 1) * pageSize
	if err := r.db.WithContext(ctx).Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListKeyset 使用 keyset（cursor）分页查询审计记录。
//
// security_audits 为 uint 自增 ID + created_at，游标复用 pagination.EncodeCursor
// （格式 <unix_nano>:<id>）。排序统一为 created_at DESC, id DESC，行值比较走
// (created_at, id) < (?, ?)。多取 1 条用于判断是否还有下一页。
func (r *SecurityAuditRepository) ListKeyset(ctx context.Context, cursor string, pageSize int) ([]model.SecurityAudit, int64, string, error) {
	if r.db == nil {
		return nil, 0, "", errors.New("security audit repository not initialized")
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	var total int64
	if err := r.db.WithContext(ctx).Model(&model.SecurityAudit{}).Count(&total).Error; err != nil {
		return nil, 0, "", err
	}

	q := r.db.WithContext(ctx).Model(&model.SecurityAudit{})
	if cursor != "" {
		ts, id, ok := _pagination.DecodeCursor(_pagination.Cursor(cursor))
		if !ok {
			return nil, 0, "", fmt.Errorf("invalid cursor: %s", cursor)
		}
		q = q.Where("(created_at, id) < (?, ?)", ts, id)
	}

	var list []model.SecurityAudit
	if err := q.Order("created_at DESC, id DESC").Limit(pageSize + 1).Find(&list).Error; err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(list) > pageSize {
		last := list[pageSize-1]
		list = list[:pageSize]
		nextCursor = string(_pagination.EncodeCursor(last.CreatedAt, uint64(last.ID)))
	}

	return list, total, nextCursor, nil
}

// GetByID 获取审计详情（含 items）
func (r *SecurityAuditRepository) GetByID(ctx context.Context, id uint) (*model.SecurityAudit, error) {
	var a model.SecurityAudit
	if err := r.db.WithContext(ctx).Preload("Items").First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// PingDB 执行 SELECT 1 探活
func (r *SecurityAuditRepository) PingDB(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("security audit repository not initialized")
	}
	return r.db.WithContext(ctx).Exec("SELECT 1").Error
}

// CountSystemUserByRole 按角色统计 SystemUser 数
func (r *SecurityAuditRepository) CountSystemUserByRole(ctx context.Context, role string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("security audit repository not initialized")
	}
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&model.SystemUser{}).
		Where("role = ?", role).Count(&cnt).Error; err != nil {
		return 0, err
	}
	return cnt, nil
}

// FirstSystemUserByRole 取首个指定角色的 SystemUser（默认密码校验）
func (r *SecurityAuditRepository) FirstSystemUserByRole(ctx context.Context, role string) (*model.SystemUser, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("security audit repository not initialized")
	}
	var u model.SystemUser
	if err := r.db.WithContext(ctx).Where("role = ?", role).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// CountEnabledLLMProviders 统计已启用 LLM 提供商数
func (r *SecurityAuditRepository) CountEnabledLLMProviders(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("security audit repository not initialized")
	}
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&model.LLMProvider{}).Where("enabled = ?", true).Count(&cnt).Error; err != nil {
		return 0, err
	}
	return cnt, nil
}

// ListDomainPoolDomains 列出所有自有域名（仅 domain 字段）
func (r *SecurityAuditRepository) ListDomainPoolDomains(ctx context.Context) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("security audit repository not initialized")
	}
	var rows []model.DomainPool
	if err := r.db.WithContext(ctx).Model(&model.DomainPool{}).
		Select("domain").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Domain)
	}
	return out, nil
}

// ListShortLinkOriginalURLs 列出全部短链原始 URL（用于外链审计）
func (r *SecurityAuditRepository) ListShortLinkOriginalURLs(ctx context.Context) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("security audit repository not initialized")
	}
	var rows []model.ShortLink
	if err := r.db.WithContext(ctx).Select("original_url").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.OriginalURL)
	}
	return out, nil
}

// ListLiveCodeOutboundURLs 列出全部活码的入口/落地 URL
func (r *SecurityAuditRepository) ListLiveCodeOutboundURLs(ctx context.Context) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("security audit repository not initialized")
	}
	var rows []model.LiveCode
	if err := r.db.WithContext(ctx).Select("entry_url, landing_url").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows)*2)
	for _, r := range rows {
		if r.EntryURL != "" {
			out = append(out, r.EntryURL)
		}
		if r.LandingURL != "" {
			out = append(out, r.LandingURL)
		}
	}
	return out, nil
}
