package service

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/config"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/fingerprint"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/system/install"

	"gorm.io/gorm"
)

// 租户/开户服务（specs/001-saas-multi-tenant T029/T033）
//
// 本期口径（与 contracts/api.md、data-model.md 对齐）：
//   - 开户仅在 TENANT_MODE=on 开放；off 模式恒 4104（离线实例不产生租户行）
//   - 用户名唯一性是「租户内」，邮箱唯一性是「跨租户全局」——两者校验口径不可混用
//   - 短码 code 全局唯一，供回调 URL 与展示使用

// 开户参数与响应
type RegisterTenantRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`

	// ---- 开户登记（平台监管责任）----
	// 需求原文：租户开户必须收集「电话，邮箱，公司名称，负责人等」。
	// CompanyName 缺省回落到 Name（两者语义几乎重合），其余三项必填。
	CompanyName string `json:"company_name"`
	LegalPerson string `json:"legal_person"`
	Phone       string `json:"phone"`
	LicenseNo   string `json:"license_no"`
	Address     string `json:"address"`
	// RegulatoryAck 用户已勾选监管告知
	RegulatoryAck bool `json:"regulatory_ack"`
	// AgreementVersion 用户同意的协议版本（由注册页从 GET /api/tenant/regulatory-notice 取）
	AgreementVersion string `json:"agreement_version"`
}

type RegisterTenantResponse struct {
	TenantID   uint   `json:"tenant_id"`
	TenantCode string `json:"tenant_code"`
	Token      string `json:"token"`
}

type TenantInfo struct {
	TenantID   uint   `json:"tenant_id"`
	TenantCode string `json:"tenant_code"`
	Name       string `json:"name"`
	Status     int16  `json:"status"`
}

// TenantFilter / TenantWithActivity 控制器侧别名：controller 层禁依赖 repository
// （depguard），运营清单的过滤结构与行视图经 service 包透出。
type (
	TenantFilter       = repository.TenantFilter
	TenantWithActivity = repository.TenantWithActivity
)

// TenantService 租户服务
type TenantService struct {
	tenantRepo repository.TenantRepository
	userRepo   repository.SystemUserRepository
	jwtUtils   *utils.JWTUtils
}

// NewTenantService 创建租户服务实例
func NewTenantService() *TenantService {
	return &TenantService{
		tenantRepo: repository.NewTenantRepository(),
		userRepo:   repository.NewSystemUserRepository(),
		jwtUtils:   utils.NewJWTUtils(utils.DefaultJWTConfig),
	}
}

// RegisterTenant 自助开户：租户 + 管理员原子建成，并直接签发登录令牌
//
// 事务边界在 Repository.CreateWithAdmin（两个 INSERT 同事务）；
// 本层负责全部业务判断：开放模式、参数、重复校验、短码生成、令牌签发。
//
// clientKey/userAgent：003 T010 静默合规采集（ClientIP + UA → tenants 三列）。
func (s *TenantService) RegisterTenant(ctx context.Context, req RegisterTenantRequest, clientKey, userAgent string) (*RegisterTenantResponse, error) {
	if !tenant.IsMultiTenant() {
		return nil, bizerr.New(bizerr.CodeTenantRegisterClosed, "当前为离线部署模式，注册未开放")
	}
	if err := validateRegisterReq(req); err != nil {
		return nil, err
	}
	if !registerLimiter.Allow(clientKey) {
		return nil, bizerr.New(bizerr.CodeTenantRegisterRateLimited, "注册请求过于频繁，请稍后再试")
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	username := strings.TrimSpace(req.Username)
	name := strings.TrimSpace(req.Name)

	// 开户登记必填项（监管责任）。校验放在这里而不是 controller：
	// 登录注册、自助开户、离线初始化三条入口都走这个函数，
	// 放 controller 就会有人新增一条入口时忘了校验。
	if err := validateRegulatoryFields(req); err != nil {
		return nil, err
	}
	companyName := strings.TrimSpace(req.CompanyName)
	if companyName == "" {
		companyName = name
	}
	agreementVersion := strings.TrimSpace(req.AgreementVersion)
	if agreementVersion == "" {
		agreementVersion = serviceCurrentAgreementVersion
	}

	// 邮箱全局唯一（跨租户亦不可并存）——登录通道依赖此语义。
	// 003 T012：邮箱降选填，空邮箱跳过查重（部分唯一索引同样排除空值）。
	if email != "" {
		exists, err := s.userRepo.EmailExistsGlobal(ctx, email)
		if err != nil {
			return nil, bizerr.Wrap(bizerr.CodeInternal, "校验邮箱失败", err)
		}
		if exists {
			return nil, bizerr.New(bizerr.CodeTenantEmailTaken, "该邮箱已被注册")
		}
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		t := &model.Tenant{
			Code:   generateTenantCode(),
			Name:   name,
			Status: model.TenantStatusActive,
			Plan:   "free",
			// 开户登记
			CompanyName:      companyName,
			LegalPerson:      strings.TrimSpace(req.LegalPerson),
			Phone:            strings.TrimSpace(req.Phone),
			Email:            email,
			LicenseNo:        strings.TrimSpace(req.LicenseNo),
			Address:          strings.TrimSpace(req.Address),
			RegMode:          model.TenantRegModeSaaS,
			AgreementVersion: agreementVersion,
			RegulatoryAck:    true,
			PlatformSync:     model.TenantSyncPending,
		}
		// 003 T010 静默合规采集：UA 只存 sha256 hex，指纹由 UA+IP 派生
		t.InitIP = strings.TrimSpace(clientKey)
		t.InitUA = fingerprint.HashHex(userAgent)
		t.InitDeviceFingerprint = fingerprint.FromClient(userAgent, clientKey)
		// 每次尝试重建：admin 在 Create 时被 BeforeCreate 钩子原地哈希，
		// 复用同一实例会导致第二次插入存进 bcrypt(bcrypt(明文))。
		// 密码按明文入参，加密职责归模型钩子（与 AuthService.Register 同一口径）。
		admin := &model.SystemUser{
			Username:  username,
			Email:     email,
			Password:  req.Password,
			Role:      model.SystemUserRoleAdmin,
			Status:    1,
			Enabled:   true,
			DataScope: model.DefaultDataScopeForRole(model.SystemUserRoleAdmin),
			RealName:  name,
		}
		// 用户名租户内唯一：新租户 ID 尚未生成，此刻查重无意义，
		// 该语义由 (tenant_id, username) 复合唯一索引在库层兜底（T008 原位重建）。
		if err := s.tenantRepo.CreateWithAdmin(ctx, t, admin); err != nil {
			if isUniqueViolation(err) {
				lastErr = err // 短码撞库：换码重试
				continue
			}
			logger.Error(err, "开户事务失败")
			return nil, bizerr.Wrap(bizerr.CodeInternal, "开户失败，请稍后重试", err)
		}
		token, err := s.jwtUtils.GenerateTokenForTenant(admin.ID, admin.Username, admin.Role,
			admin.DataScope, admin.DepartmentID, admin.TeamID, t.ID)
		if err != nil {
			logger.Error(err, "开户成功但令牌签发失败")
			return nil, bizerr.Wrap(bizerr.CodeInternal, "开户成功，令牌签发失败，请重新登录", err)
		}
		// 上报平台登记：失败不回滚（见 SubmitRegistrationToPlatform 的注释）
		if err := s.SubmitRegistrationToPlatform(ctx, t, admin); err != nil {
			logger.Warnf("[tenant] 开户登记上报失败（已回滚为 pending/failed）: %v", err)
		}

		// 003 T013 开户赠送：失败仅 Warn 不阻断注册（赠送可后补，开户不可逆）
		// AV-04c 批次6：句柄解析下沉仓储层（NewStarterKitRepositoryDefault 在仓储包内
		// 取全局句柄，与 auth.go 开户赠送同款先例）——连接、解析时刻与原
		// `NewStarterKitRepository(s.tenantRepo.GetDB())` 完全一致。
		provisionTenantStarterKit(ctx, t.ID, t.Name, repository.NewStarterKitRepositoryDefault())
		ProvisionTenantModules(ctx, t.ID)

		return &RegisterTenantResponse{
			TenantID:   t.ID,
			TenantCode: t.Code,
			Token:      token,
		}, nil
	}
	return nil, bizerr.Wrap(bizerr.CodeInternal, "生成租户短码失败，请重试", lastErr)
}

// GetTenantInfo 取当前登录租户信息（租户取自 ctx，非请求参数）
func (s *TenantService) GetTenantInfo(ctx context.Context) (*TenantInfo, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		id = tenant.DefaultTenantID
	}
	t, err := s.tenantRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, bizerr.New(bizerr.CodeTenantNotFound, "租户不存在")
		}
		return nil, bizerr.Wrap(bizerr.CodeInternal, "获取租户信息失败", err)
	}
	return &TenantInfo{
		TenantID:   t.ID,
		TenantCode: t.Code,
		Name:       t.Name,
		Status:     t.Status,
	}, nil
}

// ResolvePublicTenantID 公开入口按短码解析租户（站点配置/自助注册的归属）
//
// 公开入口没有登录态，租户只能由请求显式携带的短码确定。查询挂 SystemScope
// （租户由命中的行自身携带）；停用租户按不存在对待，避免给公开页泄露账号状态。
func (s *TenantService) ResolvePublicTenantID(ctx context.Context, code string) (uint, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, bizerr.New(bizerr.CodeTenantParamInvalid, "缺少租户短码")
	}
	t, err := s.tenantRepo.GetByCode(tenant.WithSystemScope(ctx), code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, bizerr.New(bizerr.CodeTenantNotFound, "租户不存在或代码错误")
		}
		return 0, bizerr.Wrap(bizerr.CodeInternal, "解析租户失败", err)
	}
	if !model.TenantIsActive(t) {
		return 0, bizerr.New(bizerr.CodeTenantNotFound, "租户不存在或代码错误")
	}
	return t.ID, nil
}

// ListTenants 平台运营清单（分页 + 状态/关键词过滤）
func (s *TenantService) ListTenants(ctx context.Context, f repository.TenantFilter) ([]repository.TenantWithActivity, int64, error) {
	rows, total, err := s.tenantRepo.List(ctx, f)
	if err != nil {
		return nil, 0, bizerr.Wrap(bizerr.CodeInternal, "获取租户列表失败", err)
	}
	return rows, total, nil
}

// UpdateTenantStatus 租户停用/启用：停用后登录受限、后台任务跳过，数据保留
func (s *TenantService) UpdateTenantStatus(ctx context.Context, id uint, status int16) error {
	if !model.IsValidTenantStatus(status) {
		return bizerr.New(bizerr.CodeTenantParamInvalid, "非法的租户状态取值")
	}
	if _, err := s.tenantRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return bizerr.New(bizerr.CodeTenantNotFound, "租户不存在")
		}
		return bizerr.Wrap(bizerr.CodeInternal, "获取租户失败", err)
	}
	// 首租户 = 平台运营方租户（tenants 表 ID 最小，见 middleware.PlatformOperatorMiddleware）。
	// 停用它 ⇒ 平台运营方自己被锁在门外（登录受限 ⇒ 读不到自己的租户 ⇒
	// 恢复不了），且没有任何人能再停用/启用它 —— 死锁。
	// 所以在**状态变更**这一层就挡住，而不是等登录失败后再救。
	if status != model.TenantStatusActive {
		if first, err := s.tenantRepo.GetFirstTenant(ctx); err == nil && first != nil && first.ID == id {
			return bizerr.New(bizerr.CodeTenantParamInvalid,
				"首个租户是平台运营方租户，不允许停用（如需停用请先新建运营租户并移交）")
		}
	}
	if err := s.tenantRepo.UpdateStatus(ctx, id, status); err != nil {
		return bizerr.Wrap(bizerr.CodeInternal, "更新租户状态失败", err)
	}
	return nil
}

// ── 参数校验与短码生成 ──────────────────────────────────────────────────

var usernamePattern = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-"

func validateRegisterReq(req RegisterTenantRequest) error {
	name := strings.TrimSpace(req.Name)
	username := strings.TrimSpace(req.Username)
	email := strings.ToLower(strings.TrimSpace(req.Email))

	if l := len([]rune(name)); l < 2 || l > 100 {
		return bizerr.New(bizerr.CodeTenantParamInvalid, "企业/团队名称长度需为 2-100 个字符")
	}
	if l := len([]rune(username)); l < 3 || l > 32 {
		return bizerr.New(bizerr.CodeTenantParamInvalid, "用户名长度需为 3-32 个字符")
	}
	for _, c := range username {
		if !strings.ContainsRune(usernamePattern, c) {
			return bizerr.New(bizerr.CodeTenantParamInvalid, "用户名只能包含字母、数字、下划线、点与短横线")
		}
	}
	// 003 T012：邮箱降选填（登录仍有 username 通道；系统邮箱唯一索引为部分
	// 索引，已排除空值，见 tenant_global_email_unique_migration）。
	if email != "" {
		if !strings.Contains(email, "@") || len(email) > 100 {
			return bizerr.New(bizerr.CodeTenantParamInvalid, "邮箱格式不正确")
		}
	}
	if l := len(req.Password); l < 8 || l > 64 {
		return bizerr.New(bizerr.CodeTenantParamInvalid, "密码长度需为 8-64 个字符")
	}
	return nil
}

const tenantCodeAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// generateTenantCode 生成租户短码：t + 6 位低歧义字符（避开易混的 l/o/0/1）
//
// 契约示例形如 "t42xk"（含租户 ID）；此处取不透明随机码而非 ID 前缀，
// 原因是短码在插入前生成即可，避免「先占位再回填」的第二次写入。
// 唯一性由唯一索引 + isUniqueViolation 重试保证。
func generateTenantCode() string {
	b := make([]byte, 6)
	if _, err := cryptorand.Read(b); err != nil {
		// 熵源不可用属极端环境故障：退化为时间派生码，唯一性仍由索引与重试兜底
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i % 8 * 8))
		}
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = tenantCodeAlphabet[int(v)%len(tenantCodeAlphabet)]
	}
	return "t" + string(out)
}

// isUniqueViolation 判定唯一约束冲突（PG 23505）：短码撞库时换码重试
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "Duplicate entry")
}

// ── 注册限流（4105）────────────────────────────────────────────────────
//
// 同源 IP/指纹维度令牌桶：窗口内允许 5 次开户申请，10 分钟内回补。
// 进程内实现（开户是低频写操作，无需跨实例共享配额）。

const (
	registerBurst    = 5
	registerRefillIn = 10 * time.Minute
)

type registerBucket struct {
	tokens float64
	last   time.Time
}

var registerLimiter = &registerBucketStore{buckets: map[string]*registerBucket{}}

type registerBucketStore struct {
	mu      sync.Mutex
	buckets map[string]*registerBucket
}

// Allow 取一个令牌；桶不存在则按满桶初始化
func (s *registerBucketStore) Allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b, ok := s.buckets[key]
	if !ok {
		s.buckets[key] = &registerBucket{tokens: registerBurst - 1, last: now}
		return true
	}
	refill := float64(now.Sub(b.last)) / float64(registerRefillIn)
	// 不用内置 min：包内测试辅助函数 func min(int, int) int 在 test build 下会遮蔽它
	capacity := float64(registerBurst)
	b.tokens += refill
	if b.tokens > capacity {
		b.tokens = capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ── 开户登记校验与上报 ─────────────────────────────────────────────────

// serviceCurrentAgreementVersion 当前监管告知版本。
//
// 与 platform-server 的 CurrentAgreementVersion 保持同值：两端同源维护，
// 离线端用它兜底，改文本时两边一起升版本号 —— 版本号是"用户当时同意的是
// 哪一版"的唯一凭据，只改文本不升版本等于让留痕失去意义。
const serviceCurrentAgreementVersion = "2026-10-17.v1"

// validateRegulatoryFields 校验开户登记的监管必需项
//
// 003 T012 收敛后的判据：公司名称、负责人、电话、**地址** 四项缺一不予开户；
// 邮箱/执照号选填（地址顶替邮箱的必填位——电话失联时它是唯一剩下的定位线索；
// 邮箱此前是必填但与登录邮箱语义纠缠，降选填后由地址补上合规缺口）。
// 少任何一项，平台端持有的就是一条**联系不到人**的经营主体登记 ——
// 在需要配合监管问询时，这样的记录没有价值。
func validateRegulatoryFields(req RegisterTenantRequest) error {
	var missing []string
	if strings.TrimSpace(req.CompanyName) == "" && strings.TrimSpace(req.Name) == "" {
		missing = append(missing, "公司名称")
	}
	if strings.TrimSpace(req.LegalPerson) == "" {
		missing = append(missing, "负责人姓名")
	}
	if strings.TrimSpace(req.Phone) == "" {
		missing = append(missing, "联系电话")
	}
	if strings.TrimSpace(req.Address) == "" {
		missing = append(missing, "联系地址")
	}
	if len(missing) > 0 {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete,
			"开户信息不完整，缺少："+strings.Join(missing, "、"))
	}
	if !tenantPhonePattern.MatchString(strings.TrimSpace(req.Phone)) {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete, "联系电话格式不正确")
	}
	if !req.RegulatoryAck {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete,
			"请先阅读并确认平台监管责任告知")
	}
	return nil
}

// tenantPhonePattern 宽松电话校验：只挡明显不可能的输入。
//
// 不做号码归属地判断：那是运营商的事，平台不该替用户判定，
// 而过严的正则会把境外号码、虚拟号、分机号一并拒掉。
var tenantPhonePattern = regexp.MustCompile(`^[0-9+\-() ]{6,32}$`)

// SubmitRegistrationToPlatform 把开户登记上报平台。
//
// 语义（需求原文）：「user-web 商户注册初始化完成开户打通和 user-platform
// 完成登记」+「注意离线独立部署情况」。所以：
//   - SaaS（PLATFORM_ENABLED=true）→ 上报；失败只标记 pending/failed，**不回滚注册**
//   - 离线部署 → 跳过（PlatformSync=skipped），本地已留痕，一个出站请求都不发
//
// 不回滚的理由：用户已经完成注册并拿到令牌，此时因"平台暂时连不上"把租户删掉，
// 代价（数据、体验、信任）远大于收益（平台的登记晚一点）。上报是可重试的，
// 开户不是。
func (s *TenantService) SubmitRegistrationToPlatform(ctx context.Context, t *model.Tenant, admin *model.SystemUser) error {
	if t == nil {
		return bizerr.New(bizerr.CodeInternal, "租户为空")
	}
	if !config.PlatformEnabled() {
		// 离线部署：不发任何出站请求，只把状态标成 skipped
		return s.tenantRepo.UpdateRegistration(ctx, t.ID, map[string]any{
			"platform_sync":     model.TenantSyncSkipped,
			"platform_sync_err": "",
		})
	}

	// 两个标识是**不同**的东西，曾经被填成同一个值：
	//   merchant_key  —— 本部署在平台上的商业身份（模块授权/订单/LLM 钱包都挂在它上面）
	//   install_id    —— 本部署的安装标识（install.lock 里那份，心跳上报用的也是它）
	//
	// 混填的后果不是"多写了一个字段"，而是平台端**无法把开户登记与心跳记录关联起来**：
	// tenant_registrations.install_id 存的是商户密钥，而 platform_installs 用的是
	// 真正的 install_id，两张表之间没有可连接的键 —— 于是"这个部署是谁、它在线吗、
	// 它登记了吗"这三个问题在平台上答不出来。
	payload := platform.TenantRegistrationPayload{
		MerchantKey:      platform.GetMerchantKey(),
		InstallID:        currentInstallID(),
		TenantCode:       t.Code,
		CompanyName:      t.CompanyName,
		LegalPerson:      t.LegalPerson,
		Phone:            t.Phone,
		Email:            t.Email,
		LicenseNo:        t.LicenseNo,
		Address:          t.Address,
		RegMode:          t.RegMode,
		AgreementVersion: t.AgreementVersion,
		RegulatoryAck:    t.RegulatoryAck,
		// 003 T010 静默合规采集随登记上报（平台端 tenant_registrations 同步落列）
		InitIP:                t.InitIP,
		InitUA:                t.InitUA,
		InitDeviceFingerprint: t.InitDeviceFingerprint,
	}
	if _, err := platform.NewModuleClient().RegisterTenant(ctx, payload); err != nil {
		// 失败不阻断注册：只标记 + 记日志，运营可在平台端手工补录
		_ = s.tenantRepo.UpdateRegistration(ctx, t.ID, map[string]any{
			"platform_sync":     model.TenantSyncFailed,
			"platform_sync_err": err.Error(),
		})
		logger.Warnf("[tenant] 开户登记上报平台失败（不影响注册本身）tenant=%s: %v", t.Code, err)
		return nil
	}
	return s.tenantRepo.UpdateRegistration(ctx, t.ID, map[string]any{
		"platform_sync":     model.TenantSyncSynced,
		"platform_sync_err": "",
	})
}

// RetryRegistrationSync 补同步（管理端「立即重试」按钮）
func (s *TenantService) RetryRegistrationSync(ctx context.Context, tenantID uint) error {
	t, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return bizerr.New(bizerr.CodeTenantNotFound, "租户不存在")
		}
		return err
	}
	// 管理员身份不是上报的必需输入（登记字段都在租户行上），
	// 取不到只记日志不阻断 —— 少传一个用不上的参数不该让补同步失败。
	var admin model.SystemUser
	return s.SubmitRegistrationToPlatform(ctx, t, &admin)
}

// RegistrationInfoView 登记状态视图
type RegistrationInfoView struct {
	TenantCode       string `json:"tenant_code"`
	CompanyName      string `json:"company_name"`
	LegalPerson      string `json:"legal_person"`
	Phone            string `json:"phone"`
	Email            string `json:"email"`
	LicenseNo        string `json:"license_no"`
	Address          string `json:"address"`
	RegMode          string `json:"reg_mode"`
	PlatformSync     string `json:"platform_sync"`
	PlatformSyncErr  string `json:"platform_sync_error"`
	AgreementVersion string `json:"agreement_version"`
	RegulatoryAck    bool   `json:"regulatory_ack"`
}

// RegistrationInfo 当前租户的登记状态
func (s *TenantService) RegistrationInfo(ctx context.Context) (*RegistrationInfoView, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		id = tenant.DefaultTenantID
	}
	t, err := s.tenantRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, bizerr.New(bizerr.CodeTenantNotFound, "租户不存在")
		}
		return nil, bizerr.Wrap(bizerr.CodeInternal, "获取租户登记信息失败", err)
	}
	return &RegistrationInfoView{
		TenantCode: t.Code, CompanyName: t.CompanyName, LegalPerson: t.LegalPerson,
		Phone: t.Phone, Email: t.Email, LicenseNo: t.LicenseNo, Address: t.Address,
		RegMode: t.RegMode, PlatformSync: t.PlatformSync,
		PlatformSyncErr:  t.PlatformSyncErr,
		AgreementVersion: t.AgreementVersion, RegulatoryAck: t.RegulatoryAck,
	}, nil
}

// currentInstallID 读本部署的安装标识（install.lock）。
//
// 读不到时返回空串而不是回落 merchant_key：把两个不同语义的标识混成一个，
// 会让平台端的关联查询"看起来有值、实际对不上"——那比空值更难排查。
// 空值至少让平台端能明确知道"这个部署没提供安装标识"。
func currentInstallID() string {
	lr, err := install.Load()
	if err != nil || lr == nil {
		logger.Warnf("[tenant] 读取 install.lock 失败，开户登记将不带 install_id: %v", err)
		return ""
	}
	return strings.TrimSpace(lr.InstallID)
}
