package repository

import (
	"context"
	"errors"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// CRM-21：跨账号多实体检索。所有实体都通过同一份 CustomerScopeFilter 收敛到
// 可见客户集合，再按实体类型回查，避免"客户查得到、关联实体越过账号边界"。

type CRMCrossSearchItem struct {
	EntityType      string `json:"entity_type"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Phone           string `json:"phone,omitempty"`
	Email           string `json:"email,omitempty"`
	Industry        string `json:"industry,omitempty"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	CustomerID      string `json:"customer_id,omitempty"`
	SourceRef       string `json:"source_ref,omitempty"`
}

type CRMIndustryAccountCount struct {
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	Count           int64  `json:"count"`
}

// CRMCrossSearchFilter 直接复用 CustomerScopeFilter 的账号/行业/负责人语义。
type CRMCrossSearchFilter struct {
	EntityType string
	CustomerScopeFilter
}

type CRMCrossSearchRepository interface {
	ListScoped(ctx context.Context, f CRMCrossSearchFilter) ([]CRMCrossSearchItem, int64, error)
	CountIndustryAccount(ctx context.Context, f CRMCrossSearchFilter) ([]CRMIndustryAccountCount, error)
}

type crmCrossSearchRepository struct{}

func NewCRMCrossSearchRepository() CRMCrossSearchRepository { return &crmCrossSearchRepository{} }

func (r *crmCrossSearchRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

// crossSearchEntityTable 返回实体表与其客户关联方式。未知实体类型一律返回错误，
// 不静默退化成"客户全量"，避免把不支持的类型假报为已覆盖。
func crossSearchEntityTable(entityType string) (table string, via string, err error) {
	switch entityType {
	case "customer":
		return "customers", "self", nil
	case "contact":
		return "contacts", "customer", nil
	case "supplier":
		return "suppliers", "customer", nil
	case "service_provider":
		return "service_providers", "customer", nil
	case "company":
		return "companies", "relation", nil
	default:
		return "", "", errors.New("不支持的跨账号检索实体类型: " + entityType)
	}
}

// scopedCustomerIDs 用同一份权限过滤出可见客户主键，作为关联实体的边界。
// 关键词只作用于实体自身，因此从客户集合里剔除 Keyword，避免"客户名命中 + 实体名命中"
// 的双重条件把本应可见的实体错误排除。
func (r *crmCrossSearchRepository) scopedCustomerIDs(ctx context.Context, f CRMCrossSearchFilter) *gorm.DB {
	base := f.CustomerScopeFilter
	base.Page, base.PageSize, base.Keyword = 0, 0, ""
	q := r.db(ctx).Model(&model.Customer{}).Select("customers.id").Where("customers.tenant_id = ?", tenant.WriteTenantID(ctx))
	return applyCustomerScope(q, base)
}

func normalizeCrossPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	return page, pageSize
}

// ListScoped 按实体类型返回受权分页结果，total 为当前类型全量计数（非当前页行数）。
func (r *crmCrossSearchRepository) ListScoped(ctx context.Context, f CRMCrossSearchFilter) ([]CRMCrossSearchItem, int64, error) {
	table, via, err := crossSearchEntityTable(f.EntityType)
	if err != nil {
		return nil, 0, err
	}
	tid := tenant.WriteTenantID(ctx)

	// 客户自身：沿用 ListScoped 的同款过滤（含 name/phone/email 关键词与合并排除）。
	if via == "self" {
		q := r.db(ctx).Model(&model.Customer{}).Where("customers.tenant_id = ?", tid)
		q = applyCustomerScope(q, f.CustomerScopeFilter)
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		page, pageSize := normalizeCrossPage(f.Page, f.PageSize)
		var rows []*model.Customer
		if err := q.Select("customers.id AS id, customers.name AS name, customers.phone AS phone, customers.email AS email, " +
			"customers.industry AS industry, customers.source_account_id AS source_account_id, " +
			"COALESCE(customers.owner_user_id,0) AS owner_user_id").
			Order("customers.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		out := make([]CRMCrossSearchItem, 0, len(rows))
		for _, c := range rows {
			item := CRMCrossSearchItem{
				EntityType: "customer", ID: c.ID, Name: c.Name, Phone: c.Phone, Email: c.Email,
				Industry: c.Industry, SourceAccountID: c.SourceAccountID,
			}
			if c.OwnerUserID != nil {
				item.OwnerUserID = strconv.FormatUint(uint64(*c.OwnerUserID), 10)
			}
			out = append(out, item)
		}
		return out, total, nil
	}

	// 关联实体：先用可见客户集合收敛，再对实体自身名称做关键词过滤。
	q := r.db(ctx).Table(table).Where(table+".tenant_id = ?", tid)
	switch via {
	case "customer":
		q = q.Where(table+".customer_id IN (?)", r.scopedCustomerIDs(ctx, f))
	case "relation":
		q = q.Where(table+".id IN (?)", r.db(ctx).Table("customer_company_relations").
			Select("company_id").Where("tenant_id = ?", tid).
			Where("customer_id IN (?)", r.scopedCustomerIDs(ctx, f)))
	}
	if f.Keyword != "" {
		q = q.Where(table+".name ILIKE ?", "%"+f.Keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizeCrossPage(f.Page, f.PageSize)
	var raw []struct {
		ID         string `gorm:"column:id"`
		Name       string `gorm:"column:name"`
		CustomerID string `gorm:"column:customer_id"`
		SourceRef  string `gorm:"column:source_ref"`
		Industry   string `gorm:"column:industry"`
	}
	if err := q.Select(table + ".id AS id, " + table + ".name AS name, " + table + ".customer_id AS customer_id, " +
		table + ".source_ref AS source_ref, " +
		"(SELECT industry FROM customers WHERE customers.id = " + table + ".customer_id) AS industry").
		Order(table + ".id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&raw).Error; err != nil {
		return nil, 0, err
	}
	out := make([]CRMCrossSearchItem, 0, len(raw))
	for _, row := range raw {
		item := CRMCrossSearchItem{
			EntityType: f.EntityType, ID: row.ID, Name: row.Name,
			CustomerID: row.CustomerID, SourceRef: row.SourceRef, Industry: row.Industry,
		}
		// 关联实体没有独立 account_id 列时，来源账号由关联客户回查（不伪造）。
		if item.CustomerID != "" {
			var acc string
			_ = r.db(ctx).Model(&model.Customer{}).Select("source_account_id").
				Where("tenant_id = ? AND id = ?", tid, item.CustomerID).Scan(&acc).Error
			item.SourceAccountID = acc
		}
		out = append(out, item)
	}
	return out, total, nil
}

// CountIndustryAccount 从全部可见记录聚合行业×账号（不是当前页）。
// 关联实体统一通过 scopedCustomerIDs 收敛可见客户，杜绝 join 上的裸列名歧义，
// 并保证与 ListScoped 的计数口径完全一致。
func (r *crmCrossSearchRepository) CountIndustryAccount(ctx context.Context, f CRMCrossSearchFilter) ([]CRMIndustryAccountCount, error) {
	table, via, err := crossSearchEntityTable(f.EntityType)
	if err != nil {
		return nil, err
	}
	tid := tenant.WriteTenantID(ctx)

	var q *gorm.DB
	switch via {
	case "self":
		q = r.db(ctx).Table("customers c").Where("c.tenant_id = ?", tid)
		q = applyCustomerScope(q, f.CustomerScopeFilter)
	case "customer":
		q = r.db(ctx).Table(table+" e").
			Joins("JOIN customers c ON c.id = e.customer_id AND c.tenant_id = ?", tid).
			Where("e.tenant_id = ?", tid).
			Where("c.id IN (?)", r.scopedCustomerIDs(ctx, f))
		if f.Keyword != "" {
			q = q.Where("e.name ILIKE ?", "%"+f.Keyword+"%")
		}
	case "relation":
		q = r.db(ctx).Table("companies e").
			Joins("JOIN customer_company_relations rel ON rel.company_id = e.id AND rel.tenant_id = ?", tid).
			Joins("JOIN customers c ON c.id = rel.customer_id AND c.tenant_id = ?", tid).
			Where("e.tenant_id = ?", tid).
			Where("c.id IN (?)", r.scopedCustomerIDs(ctx, f))
		if f.Keyword != "" {
			q = q.Where("e.name ILIKE ?", "%"+f.Keyword+"%")
		}
	}
	return scanIndustryAccountCounts(q)
}

func scanIndustryAccountCounts(q *gorm.DB) ([]CRMIndustryAccountCount, error) {
	var out []CRMIndustryAccountCount
	err := q.Select("c.industry AS industry, c.source_account_id AS source_account_id, COUNT(*) AS count").
		Group("c.industry, c.source_account_id").Scan(&out).Error
	return out, err
}
