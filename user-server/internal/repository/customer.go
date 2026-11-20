package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	_utils "hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type txContextKey struct{}

func dbFromCtx(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx
	}
	return _db.GetDB().WithContext(ctx)
}

// CustomerRepository defines the interface for customer data access
type CustomerRepository interface {
	Create(ctx context.Context, customer *model.Customer) error
	GetByID(ctx context.Context, id string) (*model.Customer, error)
	GetByUnifiedID(ctx context.Context, unifiedID string) (*model.Customer, error)
	GetByPhone(ctx context.Context, phone string) (*model.Customer, error)
	GetByEmail(ctx context.Context, email string) (*model.Customer, error)
	GetByWechatOpenID(ctx context.Context, openID string) (*model.Customer, error)
	GetByDouyinOpenID(ctx context.Context, openID string) (*model.Customer, error)
	Update(ctx context.Context, customer *model.Customer) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, page, limit int, keyword string) ([]*model.Customer, int64, error)
	// ListKeyset 使用 keyset（cursor）分页读取客户列表。
	// cursor 为空表示首页；返回的 nextCursor 为空表示已到末页。
	ListKeyset(ctx context.Context, cursor string, limit int, keyword string) ([]*model.Customer, int64, string, error)
	FindByIdentity(ctx context.Context, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) (*model.Customer, error)
	FindByIdentityAll(ctx context.Context, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) ([]*model.Customer, error)
	CountNotEmpty(ctx context.Context, fieldName string) (int64, error)
	CountMultiIdentity(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []string) (map[string]*model.Customer, error)
	GetByXiaohongshuID(ctx context.Context, xhsID string) (*model.Customer, error)
	SearchByFilter(ctx context.Context, filter CustomerSearchFilter) (items []*model.Customer, total int64, err error)
	ReassignSessionOneID(ctx context.Context, oldOneID, newOneID string) error

	ReassignOneID(ctx context.Context, table, oldOneID, newOneID string) error

	ReassignDNCOneID(ctx context.Context, oldOneID, newOneID string) error

	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	LockMergeCustomers(ctx context.Context, primaryID, secondaryID string) (*model.Customer, *model.Customer, error)
	LinkConsentIdentities(ctx context.Context, leftOneID, rightOneID string) error
}

// CustomerSearchFilter 客户搜索过滤条件（CustomerRepository.SearchByFilter 入参）
//
// 字段语义对应 customer.segment 工具的 args：
//   - Tag: tags::jsonb @> '["tag"]' 匹配（PostgreSQL JSON 数组包含）
//   - RFMMin/RFMMax: rfm_score 范围（含两端）
//   - ChurnRisk: churn_risk 等值匹配（low/medium/high）
//   - CreatedAfter/CreatedBefore: created_at 范围（RFC3339 字符串）
//   - Page/PageSize: 分页参数（1-based，PageSize 上限 100）
type CustomerSearchFilter struct {
	Tag           string
	RFMMin        int
	RFMMax        int
	HasRFMMin     bool
	HasRFMMax     bool
	ChurnRisk     string
	CreatedAfter  string
	CreatedBefore string
	Page          int
	PageSize      int
}

type customerRepository struct{}

// NewCustomerRepository creates a new CustomerRepository instance
func NewCustomerRepository() CustomerRepository {
	return &customerRepository{}
}

// LockMergeCustomers 在合并事务内按稳定顺序锁定客户，避免并发合并使用陈旧身份。
func (r *customerRepository) LockMergeCustomers(ctx context.Context, primaryID, secondaryID string) (*model.Customer, *model.Customer, error) {
	tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
	if !ok || tx == nil {
		return nil, nil, errors.New("客户合并必须在事务内执行")
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, nil, errors.New("客户合并缺少租户上下文")
	}
	var customers []*model.Customer
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id IN ?", tenantID, []string{primaryID, secondaryID}).
		Order("id ASC").Find(&customers).Error; err != nil {
		return nil, nil, err
	}
	var primary, secondary *model.Customer
	for _, customer := range customers {
		if customer.ID == primaryID {
			primary = customer
		}
		if customer.ID == secondaryID {
			secondary = customer
		}
	}
	return primary, secondary, nil
}

// LinkConsentIdentities 与客户合并共用事务，只追加身份关联，原事实保持不变。
func (r *customerRepository) LinkConsentIdentities(ctx context.Context, leftOneID, rightOneID string) error {
	tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
	if !ok || tx == nil {
		return errors.New("同意身份关联必须在客户合并事务内执行")
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return errors.New("同意身份关联缺少租户上下文")
	}
	if leftOneID == "" || rightOneID == "" || leftOneID == rightOneID {
		return nil
	}
	if leftOneID > rightOneID {
		leftOneID, rightOneID = rightOneID, leftOneID
	}
	link := model.ContactConsentIdentityLink{TenantID: tenantID, LeftOneID: leftOneID, RightOneID: rightOneID}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error
}

func (r *customerRepository) Create(ctx context.Context, customer *model.Customer) error {
	return dbFromCtx(ctx).Create(customer).Error
}

func (r *customerRepository) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) GetByUnifiedID(ctx context.Context, unifiedID string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "unified_id = ?", unifiedID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) GetByPhone(ctx context.Context, phone string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "phone = ?", phone).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) GetByEmail(ctx context.Context, email string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "email = ?", email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) GetByWechatOpenID(ctx context.Context, openID string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "wechat_open_id = ?", openID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) GetByDouyinOpenID(ctx context.Context, openID string) (*model.Customer, error) {
	var customer model.Customer
	if err := dbFromCtx(ctx).First(&customer, "douyin_open_id = ?", openID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

// Update 整行回写客户主表（调用方先 Get 再改字段）。
//
// 走 saveWholeRow：客户已被合并删除、或 id 属于别的租户时，裸 Save 不是静默成功，
// 而是回退成 INSERT 凭空插一条新行；这里翻成 ErrRecordNotFound 由上层映射 404。
func (r *customerRepository) Update(ctx context.Context, customer *model.Customer) error {
	return saveWholeRow(dbFromCtx(ctx), customer)
}

// Delete 软删除客户；命中 0 行即客户不存在（已删/别租户）。
func (r *customerRepository) Delete(ctx context.Context, id string) error {
	res := dbFromCtx(ctx).Delete(&model.Customer{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *customerRepository) List(ctx context.Context, page, limit int, keyword string) ([]*model.Customer, int64, error) {
	var customers []*model.Customer
	var total int64

	// page/limit 来自查询参数：page=0 会拼出负 OFFSET 让 PG 直接报错、
	// limit=0 会发成 LIMIT 0 让列表恒空，这里统一夹取。
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}

	offset := (page - 1) * limit
	q := dbFromCtx(ctx).Model(&model.Customer{})

	kw := strings.TrimSpace(keyword)
	if kw != "" {
		like := "%" + kw + "%"
		q = q.Where("phone LIKE ? OR email LIKE ? OR unified_id LIKE ?", like, like, like)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&customers).Error; err != nil {
		return nil, 0, err
	}

	return customers, total, nil
}

// ListKeyset 使用 keyset（cursor）分页读取客户列表。
//
// 排序统一为 created_at DESC, id DESC（复合游标）。customer.id 为 uuid 字符串，
// 因此游标使用 utils.EncodeStringIDCursor 编码 (created_at, id)，行值比较走
// (created_at, id) < (?, ?)。多取 1 条用于判断是否还有下一页。
func (r *customerRepository) ListKeyset(ctx context.Context, cursor string, limit int, keyword string) ([]*model.Customer, int64, string, error) {
	var customers []*model.Customer
	var total int64

	if limit <= 0 {
		limit = 20
	}

	q := dbFromCtx(ctx).Model(&model.Customer{})

	kw := strings.TrimSpace(keyword)
	if kw != "" {
		like := "%" + kw + "%"
		q = q.Where("phone LIKE ? OR email LIKE ? OR unified_id LIKE ?", like, like, like)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, "", err
	}

	if cursor != "" {
		ts, id, ok := _utils.DecodeStringIDCursor(cursor)
		if !ok {
			return nil, 0, "", fmt.Errorf("invalid cursor: %s", cursor)
		}
		q = q.Where("(created_at, id) < (?, ?)", ts, id)
	}

	if err := q.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&customers).Error; err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(customers) > limit {
		last := customers[limit-1]
		customers = customers[:limit]
		nextCursor = _utils.EncodeStringIDCursor(last.CreatedAt, last.ID)
	}

	return customers, total, nextCursor, nil
}

func (r *customerRepository) FindByIdentity(ctx context.Context, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) (*model.Customer, error) {
	var customer model.Customer
	query := dbFromCtx(ctx)

	conditions := ""
	args := []any{}

	if phone != "" {
		conditions += "phone = ? OR "
		args = append(args, phone)
	}
	if email != "" {
		conditions += "email = ? OR "
		args = append(args, email)
	}
	if wechatOpenID != "" {
		conditions += "wechat_open_id = ? OR "
		args = append(args, wechatOpenID)
	}
	if douyinOpenID != "" {
		conditions += "douyin_open_id = ? OR "
		args = append(args, douyinOpenID)
	}
	if xiaohongshuID != "" {
		conditions += "xiaohongshu_id = ? OR "
		args = append(args, xiaohongshuID)
	}

	if conditions == "" {
		return nil, nil
	}

	conditions = conditions[:len(conditions)-4]

	if err := query.Where(conditions, args...).First(&customer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &customer, nil
}

func (r *customerRepository) FindByIdentityAll(ctx context.Context, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) ([]*model.Customer, error) {
	query := dbFromCtx(ctx)

	conditions := ""
	args := []any{}

	if phone != "" {
		conditions += "phone = ? OR "
		args = append(args, phone)
	}
	if email != "" {
		conditions += "email = ? OR "
		args = append(args, email)
	}
	if wechatOpenID != "" {
		conditions += "wechat_open_id = ? OR "
		args = append(args, wechatOpenID)
	}
	if douyinOpenID != "" {
		conditions += "douyin_open_id = ? OR "
		args = append(args, douyinOpenID)
	}
	if xiaohongshuID != "" {
		conditions += "xiaohongshu_id = ? OR "
		args = append(args, xiaohongshuID)
	}

	if conditions == "" {
		return nil, nil
	}
	conditions = conditions[:len(conditions)-4]

	var customers []*model.Customer
	if err := query.Where(conditions, args...).Find(&customers).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return customers, nil
}

func (r *customerRepository) CountNotEmpty(ctx context.Context, fieldName string) (int64, error) {
	if fieldName == "" {
		return 0, nil
	}
	var n int64
	allowed := map[string]bool{
		"phone":          true,
		"email":          true,
		"wechat_open_id": true,
		"douyin_open_id": true,
		"xiaohongshu_id": true,
	}
	if !allowed[fieldName] {
		return 0, nil
	}
	stmt := fieldName + " <> '' AND " + fieldName + " IS NOT NULL"
	if err := dbFromCtx(ctx).Model(&model.Customer{}).Where(stmt).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *customerRepository) CountMultiIdentity(ctx context.Context) (int64, error) {
	expr := "(CASE WHEN phone IS NOT NULL AND phone <> '' THEN 1 ELSE 0 END) + " +
		"(CASE WHEN email IS NOT NULL AND email <> '' THEN 1 ELSE 0 END) + " +
		"(CASE WHEN wechat_open_id IS NOT NULL AND wechat_open_id <> '' THEN 1 ELSE 0 END) + " +
		"(CASE WHEN douyin_open_id IS NOT NULL AND douyin_open_id <> '' THEN 1 ELSE 0 END) + " +
		"(CASE WHEN xiaohongshu_id IS NOT NULL AND xiaohongshu_id <> '' THEN 1 ELSE 0 END)"
	var n int64
	if err := dbFromCtx(ctx).Model(&model.Customer{}).
		Where(expr + " >= 2").
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *customerRepository) ReassignSessionOneID(ctx context.Context, oldOneID, newOneID string) error {
	if oldOneID == "" || newOneID == "" || oldOneID == newOneID {
		return nil
	}
	return dbFromCtx(ctx).
		Table("customer_sessions").
		Where("one_id = ?", oldOneID).
		Update("one_id", newOneID).Error
}

func (r *customerRepository) ReassignOneID(ctx context.Context, table, oldOneID, newOneID string) error {
	if table == "" || oldOneID == "" || newOneID == "" || oldOneID == newOneID {
		return nil
	}
	return dbFromCtx(ctx).
		Table(table).
		Where("one_id = ?", oldOneID).
		Update("one_id", newOneID).Error
}

func (r *customerRepository) ReassignDNCOneID(ctx context.Context, oldOneID, newOneID string) error {
	if oldOneID == "" || newOneID == "" || oldOneID == newOneID {
		return nil
	}
	db := dbFromCtx(ctx)

	dupSubQuery := db.
		Table("customer_do_not_contact").
		Where("one_id = ?", oldOneID).
		Select("channel")
	if err := db.
		Table("customer_do_not_contact").
		Where("one_id = ? AND channel IN (?)", newOneID, dupSubQuery).
		Delete(nil).Error; err != nil {
		return fmt.Errorf("delete duplicate DNC rows: %w", err)
	}

	return db.
		Table("customer_do_not_contact").
		Where("one_id = ?", oldOneID).
		Update("one_id", newOneID).Error
}

func (r *customerRepository) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return _db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txContextKey{}, tx)
		return fn(txCtx)
	})
}

func (r *customerRepository) ListByIDs(ctx context.Context, ids []string) (map[string]*model.Customer, error) {
	result := make(map[string]*model.Customer, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var customers []*model.Customer
	if err := dbFromCtx(ctx).Where("id IN ?", unique).Find(&customers).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	for _, c := range customers {
		result[c.ID] = c
	}
	return result, nil
}

func (r *customerRepository) GetByXiaohongshuID(ctx context.Context, xhsID string) (*model.Customer, error) {
	if xhsID == "" {
		return nil, nil
	}
	var customer model.Customer
	if err := dbFromCtx(ctx).Where("xiaohongshu_id = ?", xhsID).First(&customer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) SearchByFilter(ctx context.Context, filter CustomerSearchFilter) ([]*model.Customer, int64, error) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	q := dbFromCtx(ctx).Model(&model.Customer{})

	if filter.Tag != "" {
		q = q.Where("tags::jsonb @> ?", fmt.Sprintf(`["%s"]`, escapeJSONString(filter.Tag)))
	}
	if filter.HasRFMMin {
		q = q.Where("rfm_score >= ?", filter.RFMMin)
	}
	if filter.HasRFMMax {
		q = q.Where("rfm_score <= ?", filter.RFMMax)
	}
	if filter.ChurnRisk != "" {
		q = q.Where("churn_risk = ?", filter.ChurnRisk)
	}
	if filter.CreatedAfter != "" {
		q = q.Where("created_at >= ?", filter.CreatedAfter)
	}
	if filter.CreatedBefore != "" {
		q = q.Where("created_at <= ?", filter.CreatedBefore)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var customers []*model.Customer
	if err := q.Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&customers).Error; err != nil {
		return nil, 0, err
	}
	return customers, total, nil
}

func escapeJSONString(s string) string {
	out := make([]byte, 0, len(s)+2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
