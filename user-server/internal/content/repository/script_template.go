package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/content/model"
	mainmodel "hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// ErrScriptTemplateMissing / ErrScriptCategoryMissing 文案含「不存在」：
// 出口层按 errhttp 的口径交 404，而不是把隔离闸或 SQL 细节原样回显。
var (
	ErrScriptTemplateMissing = errors.New("话术模板不存在")
	ErrScriptCategoryMissing = errors.New("话术分类不存在")
)

// ScriptTemplateRepository 话术模板仓库
type ScriptTemplateRepository struct {
	db *gorm.DB
}

// NewScriptTemplateRepository 由装配点注入句柄：仓储不自己摸全局 db.GetDB()。
func NewScriptTemplateRepository(db *gorm.DB) *ScriptTemplateRepository {
	return &ScriptTemplateRepository{db: db}
}

func (r *ScriptTemplateRepository) Create(ctx context.Context, template *model.ScriptTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

// GetByID err 非空时返回 nil：改前回的是零值结构体指针，
// 调用方漏判 err 就拿到一个「ID=0、标题为空」的假模板。
func (r *ScriptTemplateRepository) GetByID(ctx context.Context, id uint) (*model.ScriptTemplate, error) {
	var template model.ScriptTemplate
	if err := r.db.WithContext(ctx).First(&template, id).Error; err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *ScriptTemplateRepository) GetAll(ctx context.Context, category string, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	var templates []*model.ScriptTemplate
	var total int64
	page, pageSize = normalizeScriptPaging(page, pageSize)

	cond := func() *gorm.DB {
		query := r.db.WithContext(ctx).Model(&model.ScriptTemplate{})
		if category != "" {
			query = query.Where("category = ?", category)
		}
		return query
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("usage_count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// ListAll 整批读取，供同步链路使用。
//
// 同步不能借分页口读全量：分页口有 pageSize 上限夹紧（借 page_size=1<<20 绕过它，
// 夹紧生效后只会同步前 20 条，比改前的「LIMIT 1048576」更错）。
func (r *ScriptTemplateRepository) ListAll(ctx context.Context) ([]*model.ScriptTemplate, error) {
	var templates []*model.ScriptTemplate
	if err := r.db.WithContext(ctx).Order("id").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

// Update 只覆写本行可编辑列；归属列（tenant_id、created_by）、主键、创建时间，
// 以及库侧自增的 usage_count 一起排除。
//
// 不用 Save：Save 走 INSERT … ON CONFLICT (id) DO UPDATE，主键命中不到行时不是
// 「更新 0 行」而是按结构体快照把整行重新插入——读改写之间模板被删掉，一次「更新」
// 就是复活已删的行；行属别租户时同一条 upsert 会把归属列并进本租户。
//
// ⚠️ 隔离闸拦不住这条：guardOnConflictTenantAssignments 只拒「DO UPDATE 把 tenant_id
// 改成别的值」，而 Save 生成的正是 tenant_id = excluded.tenant_id，在放行名单里
// （#18 变异 M2 验证：on 模式出口层锁全绿）。归属只能靠「不用 Save」+ 读前置。
func (r *ScriptTemplateRepository) Update(ctx context.Context, template *model.ScriptTemplate) error {
	res := r.db.WithContext(ctx).Model(template).
		Select("*").Omit("id", "created_at", "tenant_id", "created_by", "usage_count").Updates(template)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScriptTemplateMissing
	}
	return nil
}

func (r *ScriptTemplateRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ScriptTemplate{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScriptTemplateMissing
	}
	return nil
}

// IncrementUsage 库内自增，不做读改写：并发计次只有 usage_count = usage_count + 1
// 这一种写法不丢更新。命中 0 行如实交出缺失错误。
func (r *ScriptTemplateRepository) IncrementUsage(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Model(&model.ScriptTemplate{}).
		Where("id = ?", id).
		Update("usage_count", gorm.Expr("usage_count + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScriptTemplateMissing
	}
	return nil
}

func (r *ScriptTemplateRepository) GetPublicTemplates(ctx context.Context, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	var templates []*model.ScriptTemplate
	var total int64
	page, pageSize = normalizeScriptPaging(page, pageSize)

	cond := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.ScriptTemplate{}).Where("is_public = ?", true)
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("usage_count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

func (r *ScriptTemplateRepository) SearchTemplates(ctx context.Context, keyword string, page, pageSize int) ([]*model.ScriptTemplate, int64, error) {
	var templates []*model.ScriptTemplate
	var total int64
	page, pageSize = normalizeScriptPaging(page, pageSize)

	cond := func() *gorm.DB {
		// 通配符当字面量：改前把外部串直接拼进 %…%，搜 % 等于全库扫，
		// 搜 _ 也命中不该命中的行。
		like := "%" + escapeScriptLike(keyword) + "%"
		return r.db.WithContext(ctx).Model(&model.ScriptTemplate{}).
			Where("title LIKE ? OR content LIKE ?", like, like)
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("usage_count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// ScriptCategoryRepository 话术分类仓库
type ScriptCategoryRepository struct {
	db *gorm.DB
}

// NewScriptCategoryRepository 由装配点注入句柄
func NewScriptCategoryRepository(db *gorm.DB) *ScriptCategoryRepository {
	return &ScriptCategoryRepository{db: db}
}

func (r *ScriptCategoryRepository) Create(ctx context.Context, category *model.ScriptCategory) error {
	return r.db.WithContext(ctx).Create(category).Error
}

func (r *ScriptCategoryRepository) GetAll(ctx context.Context) ([]*model.ScriptCategory, error) {
	var categories []*model.ScriptCategory
	if err := r.db.WithContext(ctx).Order("sort_order, id").Find(&categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

// Update 同话术模板腿：不用 Save，命中 0 行交出缺失哨兵，归属列不进 SET。
func (r *ScriptCategoryRepository) Update(ctx context.Context, category *model.ScriptCategory) error {
	res := r.db.WithContext(ctx).Model(category).
		Select("*").Omit("id", "created_at", "tenant_id").Updates(category)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScriptCategoryMissing
	}
	return nil
}

func (r *ScriptCategoryRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ScriptCategory{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScriptCategoryMissing
	}
	return nil
}

// normalizeScriptPaging 夹紧分页参数。夹紧的实际危害在 pageSize：pageSize<=0 会被
// GORM 原样发成 LIMIT 0（响应恒空，运维以为「话术被清空了」），过界则一次把整表
// 拖进一条响应。page<=0 在 GORM 里被吞成「无偏移」而不是报错，不夹紧也照样要修，
// 因为「page=0 回第 1 页」和「page=1 回第 1 页」该有同一个明确口径。
func normalizeScriptPaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// escapeScriptLike 转义 LIKE 模式串里的通配符与转义符本身。
// PG 与 MySQL 的 LIKE 默认转义符都是反斜杠，无需显式 ESCAPE 子句。
func escapeScriptLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ErrScriptLibraryMissing 读前置与写口之间那一行没了（含「不存在」⇒ 出口 404）。
var ErrScriptLibraryMissing = errors.New("运行时话术不存在")

// UpsertLibrary 按 (category, title) 幂等写运行时话术库。
// 只更新正文/场景/标签三列；usage_count 等运行时统计与归属列不动。
func (r *ScriptTemplateRepository) UpsertLibrary(ctx context.Context, lib *mainmodel.ScriptLibrary) (bool, error) {
	var existing mainmodel.ScriptLibrary
	err := r.db.WithContext(ctx).Where("category = ? AND title = ?", lib.Category, lib.Title).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := r.db.WithContext(ctx).Create(lib).Error; err != nil {
			return false, err
		}
		return true, nil
	case err != nil:
		return false, err
	default:
		res := r.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"content":  lib.Content,
			"scenario": lib.Scenario,
			"tags":     lib.Tags,
		})
		if res.Error != nil {
			return false, res.Error
		}
		if res.RowsAffected == 0 {
			return false, ErrScriptLibraryMissing
		}
		return false, nil
	}
}
