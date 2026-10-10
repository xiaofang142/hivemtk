package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"time"

	"gorm.io/gorm"
)

// CustomerIdentityService 客户身份识别服务
type CustomerIdentityService struct {
	repo        repository.CustomerRepository
	customerSvc *CustomerService
}

// NewCustomerIdentityService 创建客户身份识别服务实例
func NewCustomerIdentityService() *CustomerIdentityService {
	custSvc := NewCustomerService()
	return &CustomerIdentityService{
		repo:        repository.NewCustomerRepository(),
		customerSvc: custSvc,
	}
}

// ErrIdentityNotFound 身份标识未找到
var ErrIdentityNotFound = errors.New("未找到有效的身份标识")

// IdentifyOrCreate 识别或创建客户
// 优先级：Phone > Email > WechatOpenID > DouyinOpenID > XiaohongshuID
// 输入会先经过归一化（手机号去 +86/空格/横线，邮箱小写），避免同一客户被不同写法误建多条。
// 如果找到匹配的客户则返回，否则创建新客户
func (s *CustomerIdentityService) IdentifyOrCreate(ctx context.Context, identifiers identity.Identifiers) (*model.Customer, error) {
	identifiers = NormalizeIdentifiers(identifiers)

	if !HasAnyIdentifier(identifiers) {
		return nil, ErrIdentityNotFound
	}

	var customer *model.Customer
	var err error

	if identifiers.Phone != "" {
		customer, err = s.repo.GetByPhone(ctx, identifiers.Phone)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if customer != nil {
			return customer, nil
		}
	}

	if identifiers.Email != "" {
		customer, err = s.repo.GetByEmail(ctx, identifiers.Email)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if customer != nil {
			return customer, nil
		}
	}

	if identifiers.WechatOpenID != "" {
		customer, err = s.repo.GetByWechatOpenID(ctx, identifiers.WechatOpenID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if customer != nil {
			return customer, nil
		}
	}

	if identifiers.DouyinOpenID != "" {
		customer, err = s.repo.GetByDouyinOpenID(ctx, identifiers.DouyinOpenID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if customer != nil {
			return customer, nil
		}
	}

	customer = &model.Customer{
		Phone:         identifiers.Phone,
		Email:         identifiers.Email,
		WechatOpenID:  identifiers.WechatOpenID,
		DouyinOpenID:  identifiers.DouyinOpenID,
		XiaohongshuID: identifiers.XiaohongshuID,
		Tags:          "[]",
		ChurnRisk:     "low",
	}

	if err := s.repo.Create(ctx, customer); err != nil {
		if repository.IsDuplicateKeyErr(err) {
			if existing, ferr := s.findExistingWithRetry(ctx, identifiers); existing != nil {
				return existing, nil
			} else if ferr != nil {
				return nil, ferr
			}
		}
		return nil, err
	}

	return customer, nil
}

// Identify 识别客户（不创建）
// 按优先级返回第一个匹配的客户
func (s *CustomerIdentityService) Identify(ctx context.Context, identifiers identity.Identifiers) (*model.Customer, error) {
	customer, err := s.repo.FindByIdentity(ctx, identifiers.Phone, identifiers.Email, identifiers.WechatOpenID, identifiers.DouyinOpenID, identifiers.XiaohongshuID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}

	if customer == nil {
		return nil, ErrIdentityNotFound
	}

	return customer, nil
}

// LinkIdentity 为客户添加新的身份标识
func (s *CustomerIdentityService) LinkIdentity(ctx context.Context, customerID, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) error {
	phone = NormalizePhone(phone)
	email = NormalizeEmail(email)
	wechatOpenID = NormalizeOpenID(wechatOpenID)
	douyinOpenID = NormalizeOpenID(douyinOpenID)
	xiaohongshuID = NormalizeOpenID(xiaohongshuID)

	customer, err := s.repo.GetByID(ctx, customerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return ErrCustomerNotFound
	}

	if phone != "" {

		existing, err := s.repo.GetByPhone(ctx, phone)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Errorf("[OneID LinkIdentity] 查 phone=%s 失败: %v", phone, err)
			return fmt.Errorf("查 phone 失败: %w", err)
		}
		if existing != nil && existing.ID != customerID {
			return errors.New("该手机号已被其他客户使用")
		}
		customer.Phone = phone
	}

	if email != "" {
		existing, _ := s.repo.GetByEmail(ctx, email)
		if existing != nil && existing.ID != customerID {
			return errors.New("该邮箱已被其他客户使用")
		}
		customer.Email = email
	}

	if wechatOpenID != "" {

		existing, err := s.repo.GetByWechatOpenID(ctx, wechatOpenID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Errorf("[OneID LinkIdentity] 查 wechat=%s 失败: %v", wechatOpenID, err)
			return fmt.Errorf("查 wechat openid 失败: %w", err)
		}
		if existing != nil && existing.ID != customerID {
			return errors.New("该微信 OpenID 已被其他客户使用")
		}
		customer.WechatOpenID = wechatOpenID
	}

	if douyinOpenID != "" {
		existing, err := s.repo.GetByDouyinOpenID(ctx, douyinOpenID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Errorf("[OneID LinkIdentity] 查 douyin=%s 失败: %v", douyinOpenID, err)
			return fmt.Errorf("查 douyin openid 失败: %w", err)
		}
		if existing != nil && existing.ID != customerID {
			return errors.New("该抖音 OpenID 已被其他客户使用")
		}
		customer.DouyinOpenID = douyinOpenID
	}

	if xiaohongshuID != "" {
		existing, _ := s.repo.GetByXiaohongshuID(ctx, xiaohongshuID)
		if existing != nil && existing.ID != customerID {
			return errors.New("该小红书 ID 已被其他客户使用")
		}
		customer.XiaohongshuID = xiaohongshuID
	}

	return s.repo.Update(ctx, customer)
}

// MergeByIdentity 根据身份标识合并客户
// 当发现两个身份标识属于同一客户时，自动合并
func (s *CustomerIdentityService) MergeByIdentity(ctx context.Context, identifiers identity.Identifiers) (*model.Customer, error) {
	identifiers = NormalizeIdentifiers(identifiers)
	if !HasAnyIdentifier(identifiers) {
		return nil, ErrIdentityNotFound
	}

	all, err := s.repo.FindByIdentityAll(ctx, identifiers.Phone, identifiers.Email,
		identifiers.WechatOpenID, identifiers.DouyinOpenID, identifiers.XiaohongshuID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var matchedCustomers []*model.Customer
	for _, c := range all {
		if c == nil || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		matchedCustomers = append(matchedCustomers, c)
	}

	if len(matchedCustomers) == 0 {
		return s.IdentifyOrCreate(ctx, identifiers)
	}

	if len(matchedCustomers) == 1 {
		customer := matchedCustomers[0]
		s.updateCustomerIdentifiers(ctx, customer, identifiers)
		return customer, s.repo.Update(ctx, customer)
	}

	primary := matchedCustomers[0]

	if err := s.repo.WithTransaction(ctx, func(txCtx context.Context) error {
		for i := 1; i < len(matchedCustomers); i++ {
			secondary := matchedCustomers[i]
			if secondary.ID != primary.ID {
				if err := s.customerSvc.MergeCustomers(txCtx, primary.ID, secondary.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("merge customers tx: %w", err)
	}

	updatedPrimary, err := s.repo.GetByID(ctx, primary.ID)

	if err != nil {
		return nil, fmt.Errorf("get primary after merge: %w", err)
	}
	if updatedPrimary == nil {
		return nil, fmt.Errorf("primary customer %s not found after merge", primary.ID)
	}
	s.updateCustomerIdentifiers(ctx, updatedPrimary, identifiers)
	return updatedPrimary, s.repo.Update(ctx, updatedPrimary)
}

func (s *CustomerIdentityService) updateCustomerIdentifiers(ctx context.Context, customer *model.Customer, identifiers identity.Identifiers) {
	if customer.Phone == "" && identifiers.Phone != "" {
		customer.Phone = identifiers.Phone
	}
	if customer.Email == "" && identifiers.Email != "" {
		customer.Email = identifiers.Email
	}
	if customer.WechatOpenID == "" && identifiers.WechatOpenID != "" {
		customer.WechatOpenID = identifiers.WechatOpenID
	}
	if customer.DouyinOpenID == "" && identifiers.DouyinOpenID != "" {
		customer.DouyinOpenID = identifiers.DouyinOpenID
	}
	if customer.XiaohongshuID == "" && identifiers.XiaohongshuID != "" {
		customer.XiaohongshuID = identifiers.XiaohongshuID
	}
}

// GetCustomerByUnifiedID 根据 UnifiedID 获取客户
func (s *CustomerIdentityService) GetCustomerByUnifiedID(ctx context.Context, unifiedID string) (*model.Customer, error) {
	return s.repo.GetByUnifiedID(ctx, unifiedID)
}

// ResolveIdentity 解析身份标识，返回所有关联的客户
func (s *CustomerIdentityService) ResolveIdentity(ctx context.Context, identifiers identity.Identifiers) ([]*model.Customer, error) {
	identifiers = NormalizeIdentifiers(identifiers)
	var customers []*model.Customer
	seenIDs := make(map[string]bool)

	addCustomer := func(customer *model.Customer) {
		if customer != nil && !seenIDs[customer.ID] {
			customers = append(customers, customer)
			seenIDs[customer.ID] = true
		}
	}

	if identifiers.Phone != "" {
		if customer, _ := s.repo.GetByPhone(ctx, identifiers.Phone); customer != nil {
			addCustomer(customer)
		}
	}

	if identifiers.Email != "" {
		if customer, _ := s.repo.GetByEmail(ctx, identifiers.Email); customer != nil {
			addCustomer(customer)
		}
	}

	if identifiers.WechatOpenID != "" {
		if customer, _ := s.repo.GetByWechatOpenID(ctx, identifiers.WechatOpenID); customer != nil {
			addCustomer(customer)
		}
	}

	if identifiers.DouyinOpenID != "" {
		if customer, _ := s.repo.GetByDouyinOpenID(ctx, identifiers.DouyinOpenID); customer != nil {
			addCustomer(customer)
		}
	}

	if identifiers.XiaohongshuID != "" {
		if customer, _ := s.repo.GetByXiaohongshuID(ctx, identifiers.XiaohongshuID); customer != nil {
			addCustomer(customer)
		}
	}

	return customers, nil
}

func (s *CustomerIdentityService) findExistingWithRetry(ctx context.Context, identifiers identity.Identifiers) (*model.Customer, error) {

	maxAttempts := identityMaxAttempts()
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * 10 * time.Millisecond)
		}
		if uid := unifiedIDFromIdentifiers(identifiers); uid != "" {
			if uexisting, uferr := s.repo.GetByUnifiedID(ctx, uid); uferr == nil && uexisting != nil {
				return uexisting, nil
			} else if uferr != nil {
				lastErr = uferr
			}
		}
		existing, ferr := s.repo.FindByIdentity(ctx, identifiers.Phone, identifiers.Email, identifiers.WechatOpenID, identifiers.DouyinOpenID, identifiers.XiaohongshuID)
		if ferr != nil {
			lastErr = ferr
			continue
		}
		if existing != nil {
			return existing, nil
		}
	}
	return nil, lastErr
}

func unifiedIDFromIdentifiers(id identity.Identifiers) string {
	switch {
	case id.Phone != "":
		return identity.UnifiedIDFromPhone(id.Phone)
	case id.Email != "":
		return "email:" + id.Email
	case id.WechatOpenID != "":
		return "wechat:" + id.WechatOpenID
	case id.DouyinOpenID != "":
		return "douyin:" + id.DouyinOpenID
	case id.XiaohongshuID != "":
		return "xiaohongshu:" + id.XiaohongshuID
	default:
		return ""
	}
}

// DefaultIdentityMaxAttempts 是参数中心 sales.identity_max_attempts 的兜底值。
const DefaultIdentityMaxAttempts = 8

// identityMaxAttemptsProvider 由 internal/app 的参数装配层注入。
// 传 nil 表示不注入（装配顺序错时不该把兜底值顶掉）。
var identityMaxAttemptsProvider = func() int { return DefaultIdentityMaxAttempts }

// SetIdentityMaxAttemptsProvider 注入身份合并重试上限读取器，仅装配层调用。
func SetIdentityMaxAttemptsProvider(fn func() int) {
	if fn != nil {
		identityMaxAttemptsProvider = fn
	}
}

// identityMaxAttempts 返回生效中的身份合并重试上限。
// 这是仓储写入失败时的乐观重试次数：调高会放大一次合并请求对 DB 的压力，
// 调低则并发写冲突更容易直接把这次合并判失败。
func identityMaxAttempts() int {
	n := identityMaxAttemptsProvider()
	if n <= 0 {
		return DefaultIdentityMaxAttempts
	}
	return n
}

// ProbeIdentityMaxAttempts 供装配层测试读取生效值，避免为了断言而导出正式读取口。
func ProbeIdentityMaxAttempts() int { return identityMaxAttempts() }
