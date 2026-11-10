package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

// TenantFilter 租户运营清单过滤条件（contracts/api.md GET /api/manage/tenants）
type TenantFilter struct {
	Page     int
	PageSize int
	// Status 为 nil 表示不过滤状态
	Status  *int16
	Keyword string
}

// TenantActivity 租户活跃度概览（成员数与最近活跃时间）
type TenantActivity struct {
	MemberCount  int64      `json:"member_count"`
	LastActiveAt *time.Time `json:"last_active_at"`
}

// TenantWithActivity 租户行 + 活跃度概览（运营清单展示用）
type TenantWithActivity struct {
	*model.Tenant
	Activity TenantActivity `json:"activity"`
}

// TenantRepository 租户仓储接口
//
// tenants 表在 tenancyidx 登记表中标记为全局表（豁免隔离），
// 因此租户本体的 CRUD 不需要租户上下文；跨租户聚合（成员数/最近活跃）
// 走 tenant.WithSystemScope 显式绕过隔离注入，属平台运营语义。
type TenantRepository interface {
	Create(ctx context.Context, t *model.Tenant) error
	GetByID(ctx context.Context, id uint) (*model.Tenant, error)
	GetByCode(ctx context.Context, code string) (*model.Tenant, error)
	// GetFirstTenant 取本实例最早建立的租户（ID 最小）＝平台运营方租户。
	// tenants 是全局表，不受隔离 callback 约束。
	GetFirstTenant(ctx context.Context) (*model.Tenant, error)
	UpdateStatus(ctx context.Context, id uint, status int16) error
	UpdateRegistration(ctx context.Context, id uint, fields map[string]any) error
	// List 运营清单：分页 + 状态/关键词过滤，每行附活跃度概览
	List(ctx context.Context, f TenantFilter) ([]TenantWithActivity, int64, error)
	// ListForRegistrationRetry 取开户登记待补偿重报的租户行
	// （platform_sync ∈ {pending, failed}），供登记补偿扫描 runner 逐行重报。
	ListForRegistrationRetry(ctx context.Context, limit int) ([]*model.Tenant, error)

	// CreateWithAdmin 事务内建租户与首位管理员：全成或全回滚
	//
	// 仅承担原子写入编排（两个 INSERT 同事务），不做业务判断：
	// 重复校验、租户短码生成、状态默认值均由 Service 决定。
	// admin.TenantID 由调用方显式给出（开户事务内新租户 ID 尚不可知，
	// 故不走 callback 上下文填充路径，见 fillCreateForTenant 的显式值放行）。
	CreateWithAdmin(ctx context.Context, t *model.Tenant, admin *model.SystemUser) error
	// GetDB 供起步赠送等系统域直写场景取句柄（service 不得全局摸 db.GetDB，
	// 经仓储转发是唯一合规通道）。
	GetDB() *gorm.DB
}

// ErrTenantStoreUnavailable 仓储句柄缺失（装配错，不是「没数据」）。
var ErrTenantStoreUnavailable = errors.New("tenant repository not initialized")

type tenantRepo struct {
	db *gorm.DB
}

// GetDB 返回底层连接（service 取句柄经仓储，不走全局单例）。
func (r *tenantRepo) GetDB() *gorm.DB { return r.db }

// NewTenantRepository 构造
func NewTenantRepository() TenantRepository {
	return &tenantRepo{db: db.GetDB()}
}

func (r *tenantRepo) Create(ctx context.Context, t *model.Tenant) error {
	if r == nil || r.db == nil {
		return ErrTenantStoreUnavailable
	}
	if t == nil {
		return errors.New("tenant: nil 不是写入对象")
	}
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *tenantRepo) GetByID(ctx context.Context, id uint) (*model.Tenant, error) {
	var t model.Tenant
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *tenantRepo) GetByCode(ctx context.Context, code string) (*model.Tenant, error) {
	var t model.Tenant
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// GetFirstTenant 取本实例最早建立的租户（ID 最小）。
//
// 用途：判定「平台运营方」——SaaS 模式下首个租户就是平台自己开的那个，
// 它的管理员是唯一有权看全部租户、停用租户、做实例级操作的角色。
//
// 判据用 ID 最小而不是 code 前缀：租户只增不删（停用是软删，不改 ID），
// ID 最小恒定不变；code 前缀是生成策略的副产品，改了生成规则就会漂移。
//
// tenants 是全局表（无 tenant_id 列），不受隔离 callback 约束；
// 刻意不挂 SystemScope——查的是全局表第一行，ctx 带不带租户绑定不影响结果。
func (r *tenantRepo) GetFirstTenant(ctx context.Context) (*model.Tenant, error) {
	var t model.Tenant
	if err := r.db.WithContext(ctx).Order("id ASC").First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateStatus 改租户状态（启停）。
//
// 0 行 ⇒ ErrRecordNotFound：「停用一个已被删掉的租户」回成功，运营会以为它已停，
// 而租户的业务请求照旧通过（tenants 表里没有那一行，停用判定落不到任何东西上）。
func (r *tenantRepo) UpdateStatus(ctx context.Context, id uint, status int16) error {
	if r == nil || r.db == nil {
		return ErrTenantStoreUnavailable
	}
	return db.EnsureAffected(r.db.WithContext(ctx).Model(&model.Tenant{}).Where("id = ?", id).
		Update("status", status))
}

// UpdateRegistration 局部更新开户登记字段
//
// 0 行 ⇒ ErrRecordNotFound：开户登记是平台监管必需项，
// 写了「已登记」而库里一行没变，比直接报错更难查。
func (r *tenantRepo) UpdateRegistration(ctx context.Context, id uint, fields map[string]any) error {
	if r == nil || r.db == nil {
		return ErrTenantStoreUnavailable
	}
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now()
	return db.EnsureAffected(r.db.WithContext(ctx).Model(&model.Tenant{}).
		Where("id = ?", id).Updates(fields))
}

// ListForRegistrationRetry 取开户登记待补偿的租户行（platform_sync ∈ {pending, failed}）。
//
// 补偿扫描（tenant_reg_compensation_runner）的取数口。三条口径：
//   - pending 与 failed 都是"平台端可能没收到"的态；skipped（离线部署）与
//     synced（已确认）永不入列 —— 扫描器在 off 模式根本不启动，这里再挡一道
//     skipped 纯属防御，真正不能漏的是 synced 不被重报（避免无意义出域）。
//   - 不按 status=active 过滤：登记是监管留痕，停用租户的"它曾经开户"记录
//     同样要补到平台（与手动重试按钮口径一致，见 RetryRegistrationSync——
//     它也不看租户状态）。
//   - 按 id ASC + limit 圈批：行数异常多时单轮有界，余下行下一轮接续，
//     避免一轮扫描拉出无界长事务。
//
// tenants 是全局表（豁免隔离，见接口注释），不需要租户上下文。
func (r *tenantRepo) ListForRegistrationRetry(ctx context.Context, limit int) ([]*model.Tenant, error) {
	if r == nil || r.db == nil {
		return nil, ErrTenantStoreUnavailable
	}
	if limit < 1 {
		limit = 1
	}
	var rows []*model.Tenant
	err := r.db.WithContext(ctx).Model(&model.Tenant{}).
		Where("platform_sync IN ?", []string{model.TenantSyncPending, model.TenantSyncFailed}).
		Order("id ASC").Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *tenantRepo) CreateWithAdmin(ctx context.Context, t *model.Tenant, admin *model.SystemUser) error {
	if r == nil || r.db == nil {
		return ErrTenantStoreUnavailable
	}
	if t == nil || admin == nil {
		return errors.New("tenant: 开户需要租户行与管理员行两者")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		admin.TenantID = t.ID
		return tx.Create(admin).Error
	})
}

func (r *tenantRepo) List(ctx context.Context, f TenantFilter) ([]TenantWithActivity, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 20
	}

	// Count 与 Find 必须各起一条 query（见 base()），不能在同一条链上先 Count 再 Find
	// （Count 会把 Selects 换成 count(*)，就地改 Statement）。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.Tenant{})
		if f.Status != nil {
			q = q.Where("status = ?", *f.Status)
		}
		if kw := strings.TrimSpace(f.Keyword); kw != "" {
			// 模糊匹配企业名或短码（转义 LIKE 通配符，避免 % 注入放大匹配面）
			like := "%" + escapeLike(kw) + "%"
			q = q.Where("name ILIKE ? ESCAPE '\\' OR code ILIKE ? ESCAPE '\\'", like, like)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []*model.Tenant
	if err := base().Order("id DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, total, nil
	}

	acts, err := r.activities(ctx, rows)
	if err != nil {
		return nil, 0, err
	}

	out := make([]TenantWithActivity, 0, len(rows))
	for _, row := range rows {
		out = append(out, TenantWithActivity{Tenant: row, Activity: acts[row.ID]})
	}
	return out, total, nil
}

// activities 批量取租户活跃度：一次分组聚合，避免逐租户 N+1
//
// 读的是其他租户的成员行，属平台运营视角——显式挂 SystemScope 绕过隔离注入。
func (r *tenantRepo) activities(ctx context.Context, rows []*model.Tenant) (map[uint]TenantActivity, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	out := make(map[uint]TenantActivity, len(rows))
	if len(ids) == 0 {
		return out, nil
	}

	type agg struct {
		TenantID     uint
		MemberCount  int64
		LastActiveAt *time.Time
	}
	var stats []agg
	sysCtx := tenant.WithSystemScope(ctx)
	if err := r.db.WithContext(sysCtx).Model(&model.SystemUser{}).
		Select("tenant_id, COUNT(*) AS member_count, MAX(last_login) AS last_active_at").
		Where("tenant_id IN ?", ids).
		Group("tenant_id").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	for _, s := range stats {
		out[s.TenantID] = TenantActivity{MemberCount: s.MemberCount, LastActiveAt: s.LastActiveAt}
	}
	return out, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
