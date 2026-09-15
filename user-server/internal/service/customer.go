package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// Operator 表示触发操作的人（来自鉴权上下文），用于审计日志追踪。
type Operator struct {
	UserID   uint
	Username string
}

type operatorCtxKey struct{}

// WithOperator 将操作人注入 context（供 service 层写审计日志）。
func WithOperator(ctx context.Context, op Operator) context.Context {
	return context.WithValue(ctx, operatorCtxKey{}, op)
}

// OperatorFromContext 从 context 提取操作人；缺失时返回 system 默认，确保审计不中断主流程。
func OperatorFromContext(ctx context.Context) Operator {
	if ctx != nil {
		if op, ok := ctx.Value(operatorCtxKey{}).(Operator); ok {
			return op
		}
	}
	return Operator{UserID: 0, Username: "system"}
}

// CustomerService 客户服务
type CustomerService struct {
	repo      repository.CustomerRepository
	auditRepo repository.OperationLogRepository
}

// NewCustomerService 创建客户服务实例
func NewCustomerService() *CustomerService {
	return &CustomerService{
		repo:      repository.NewCustomerRepository(),
		auditRepo: repository.NewOperationLogRepository(),
	}
}

// CustomerDTO 客户数据传输对象
type CustomerDTO struct {
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	WechatOpenID  string `json:"wechat_open_id"`
	DouyinOpenID  string `json:"douyin_open_id"`
	XiaohongshuID string `json:"xiaohongshu_id"`
}

// CustomerProfile 客户 360 视图
type CustomerProfile struct {
	Customer     *model.Customer        `json:"customer"`
	RecentEvents []*model.CustomerEvent `json:"recent_events"`
	Tags         []string               `json:"tags"`
}

// ErrCustomerNotFound 客户未找到
var ErrCustomerNotFound = errors.New("客户不存在")

// ErrInvalidDTO DTO 无效
var ErrInvalidDTO = errors.New("无效的客戶 DTO")

// 分页常量
const (
	DefaultLimit = 50

	MaxLimit    = 100
	DefaultPage = 1
)

// CreateOrUpdate 创建或更新客户
//
// 修复：空 body（全部标识符为空）直接拒绝，避免脏数据入库；
// 至少需要提供一个有效身份标识（phone/email/wechat_open_id/douyin_open_id/xiaohongshu_id）。
func (s *CustomerService) CreateOrUpdate(ctx context.Context, dto *CustomerDTO) (*model.Customer, error) {
	if dto == nil {
		return nil, ErrInvalidDTO
	}

	dto.Phone = strings.TrimSpace(dto.Phone)
	dto.Email = strings.TrimSpace(dto.Email)
	dto.WechatOpenID = strings.TrimSpace(dto.WechatOpenID)
	dto.DouyinOpenID = strings.TrimSpace(dto.DouyinOpenID)
	dto.XiaohongshuID = strings.TrimSpace(dto.XiaohongshuID)

	if dto.Phone == "" && dto.Email == "" && dto.WechatOpenID == "" && dto.DouyinOpenID == "" && dto.XiaohongshuID == "" {
		return nil, errors.New("至少需要提供一个身份标识（手机/邮箱/微信/抖音/小红书 ID）")
	}

	if dto.Phone != "" && !looksLikePhone(dto.Phone) {
		return nil, errors.New("手机号格式不合法")
	}
	if dto.Email != "" && !looksLikeEmail(dto.Email) {
		return nil, errors.New("邮箱格式不合法")
	}

	existing, err := s.repo.FindByIdentity(ctx, dto.Phone, dto.Email, dto.WechatOpenID, dto.DouyinOpenID, dto.XiaohongshuID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if existing != nil {
		if dto.Phone != "" {
			existing.Phone = dto.Phone
		}
		if dto.Email != "" {
			existing.Email = dto.Email
		}
		if dto.WechatOpenID != "" {
			existing.WechatOpenID = dto.WechatOpenID
		}
		if dto.DouyinOpenID != "" {
			existing.DouyinOpenID = dto.DouyinOpenID
		}
		if dto.XiaohongshuID != "" {
			existing.XiaohongshuID = dto.XiaohongshuID
		}

		if err := s.repo.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	customer := &model.Customer{
		Phone:         dto.Phone,
		Email:         dto.Email,
		WechatOpenID:  dto.WechatOpenID,
		DouyinOpenID:  dto.DouyinOpenID,
		XiaohongshuID: dto.XiaohongshuID,
		Tags:          "[]",
		ChurnRisk:     "low",
	}

	if err := s.repo.Create(ctx, customer); err != nil {

		if repository.IsDuplicateKeyErr(err) {
			existing2, getErr := s.repo.FindByIdentity(ctx, dto.Phone, dto.Email, dto.WechatOpenID, dto.DouyinOpenID, dto.XiaohongshuID)
			if getErr == nil && existing2 != nil {
				return existing2, nil
			}
		}
		return nil, err
	}

	return customer, nil
}

// GetCustomerProfile 获取客户 360 视图
func (s *CustomerService) GetCustomerProfile(ctx context.Context, customerID string) (*CustomerProfile, error) {
	customer, err := s.repo.GetByID(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, ErrCustomerNotFound
	}

	eventRepo := repository.NewCustomerEventRepository()
	events, err := eventRepo.GetByCustomerID(ctx, customerID, 50)
	if err != nil {
		events = []*model.CustomerEvent{}
	}

	tags := GetCustomerTags(customer)

	return &CustomerProfile{
		Customer:     customer,
		RecentEvents: events,
		Tags:         tags,
	}, nil
}

// List 获取客户列表（带分页）
func (s *CustomerService) List(ctx context.Context, page, limit int) ([]*model.Customer, int64, error) {
	if page <= 0 {
		page = DefaultPage
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return s.repo.List(ctx, page, limit, "")
}

// ListKeyset 获取客户列表（keyset 分页）。
// cursor 为空表示首页；返回 nextCursor 为空表示已到末页。
func (s *CustomerService) ListKeyset(ctx context.Context, cursor string, limit int) ([]*model.Customer, int64, string, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return s.repo.ListKeyset(ctx, cursor, limit, "")
}

// AddTags 给客户添加标签
func (s *CustomerService) AddTags(ctx context.Context, customerID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	customer, err := s.repo.GetByID(ctx, customerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return ErrCustomerNotFound
	}

	existingTags := GetCustomerTags(customer)

	tagSet := make(map[string]bool)
	for _, tag := range existingTags {
		tagSet[tag] = true
	}
	for _, tag := range tags {
		if tag != "" {
			tagSet[tag] = true
		}
	}

	newTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		newTags = append(newTags, tag)
	}

	if err := SetCustomerTags(customer, newTags); err != nil {
		return err
	}

	return s.repo.Update(ctx, customer)
}

// RemoveTags 从客户移除标签
func (s *CustomerService) RemoveTags(ctx context.Context, customerID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	customer, err := s.repo.GetByID(ctx, customerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return ErrCustomerNotFound
	}

	existingTags := GetCustomerTags(customer)

	removeSet := make(map[string]bool)
	for _, tag := range tags {
		if tag != "" {
			removeSet[tag] = true
		}
	}

	newTags := make([]string, 0)
	for _, tag := range existingTags {
		if !removeSet[tag] {
			newTags = append(newTags, tag)
		}
	}

	if err := SetCustomerTags(customer, newTags); err != nil {
		return err
	}

	return s.repo.Update(ctx, customer)
}

func (s *CustomerService) MergeCustomers(ctx context.Context, primaryID, secondaryID string) error {
	if primaryID == secondaryID {
		return errors.New("不能合并同一个客户")
	}

	primary, err := s.repo.GetByID(ctx, primaryID)
	if err != nil {
		return err
	}
	if primary == nil {
		return ErrCustomerNotFound
	}

	secondary, err := s.repo.GetByID(ctx, secondaryID)
	if err != nil {
		return err
	}
	if secondary == nil {
		return errors.New("次要客户不存在")
	}

	if secondary.Phone != "" && primary.Phone == "" {
		primary.Phone = secondary.Phone

		primary.PhoneHash = PhoneHash(primary.Phone)
	}
	if secondary.Email != "" && primary.Email == "" {
		primary.Email = secondary.Email
	}
	if secondary.WechatOpenID != "" && primary.WechatOpenID == "" {
		primary.WechatOpenID = secondary.WechatOpenID
	}
	if secondary.DouyinOpenID != "" && primary.DouyinOpenID == "" {
		primary.DouyinOpenID = secondary.DouyinOpenID
	}
	if secondary.XiaohongshuID != "" && primary.XiaohongshuID == "" {
		primary.XiaohongshuID = secondary.XiaohongshuID
	}

	primaryTags := GetCustomerTags(primary)
	secondaryTags := GetCustomerTags(secondary)
	tagSet := make(map[string]bool)
	for _, tag := range primaryTags {
		tagSet[tag] = true
	}
	for _, tag := range secondaryTags {
		tagSet[tag] = true
	}
	mergedTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		mergedTags = append(mergedTags, tag)
	}
	if err := SetCustomerTags(primary, mergedTags); err != nil {
		return err
	}

	err = s.repo.WithTransaction(ctx, func(txCtx context.Context) error {

		if err := s.repo.Update(txCtx, primary); err != nil {
			return fmt.Errorf("更新主客户失败: %w", err)
		}

		if secondary.UnifiedID != "" && secondary.UnifiedID != primary.UnifiedID {

			if err := s.repo.ReassignSessionOneID(txCtx, secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移会话失败: %w", err)
			}

			if err := s.repo.ReassignDNCOneID(txCtx, secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移 DNC 失败: %w", err)
			}

			if err := s.repo.ReassignOneID(txCtx, "customer_channels", secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移客户渠道失败: %w", err)
			}

			if err := s.repo.ReassignOneID(txCtx, "csat_surveys", secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移 CSAT 失败: %w", err)
			}

			if err := s.repo.ReassignOneID(txCtx, "clues", secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移线索失败: %w", err)
			}

			if err := s.repo.ReassignOneID(txCtx, "script_exposure_logs", secondary.UnifiedID, primary.UnifiedID); err != nil {
				return fmt.Errorf("迁移话术曝光日志失败: %w", err)
			}
		}

		if err := s.migrateCustomerEvents(txCtx, secondaryID, primaryID); err != nil {
			return fmt.Errorf("迁移事件失败: %w", err)
		}

		if err := s.repo.Delete(txCtx, secondaryID); err != nil {
			return fmt.Errorf("删除次要客户失败: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	s.writeMergeAuditLog(ctx, primary, secondary)
	return nil
}

func (s *CustomerService) writeMergeAuditLog(ctx context.Context, primary, secondary *model.Customer) {
	op := OperatorFromContext(ctx)
	detail, _ := json.Marshal(map[string]any{
		"primary_id":            primary.ID,
		"secondary_id":          secondary.ID,
		"secondary_unified_id":  secondary.UnifiedID,
		"secondary_phone":       secondary.Phone,
		"secondary_email":       secondary.Email,
		"secondary_wechat":      secondary.WechatOpenID,
		"secondary_douyin":      secondary.DouyinOpenID,
		"secondary_xiaohongshu": secondary.XiaohongshuID,
	})
	log := &model.OperationLog{
		UserID:     op.UserID,
		Username:   op.Username,
		Action:     "merge",
		Module:     "customer",
		Resource:   "customer",
		ResourceID: primary.ID,
		Detail:     string(detail),
	}
	auditRepo := s.auditRepo
	if auditRepo == nil {
		auditRepo = repository.NewOperationLogRepository()
	}
	if err := auditRepo.Create(ctx, log); err != nil {
		fmt.Printf("[audit][WARN] 合并审计写入失败: primary=%s secondary=%s op=%s err=%v\n",
			primary.ID, secondary.ID, op.Username, err)
	}
}

func (s *CustomerService) migrateCustomerEvents(ctx context.Context, secondaryID, primaryID string) error {
	eventRepo := repository.NewCustomerEventRepository()
	if _, err := eventRepo.ReassignCustomerID(ctx, secondaryID, primaryID); err != nil {
		return fmt.Errorf("迁移事件失败: %w", err)
	}
	return nil
}

// GetCustomerByIdentity 根据身份标识获取客户
func (s *CustomerService) GetCustomerByIdentity(ctx context.Context, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID string) (*model.Customer, error) {
	return s.repo.FindByIdentity(ctx, phone, email, wechatOpenID, douyinOpenID, xiaohongshuID)
}

// SerializeTags 序列化标签数组为 JSON 字符串
func SerializeTags(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetCustomerTags 获取客户标签数组
func GetCustomerTags(c *model.Customer) []string {
	return model.GetCustomerTags(c)
}

// SetCustomerTags 设置客户标签数组
func SetCustomerTags(c *model.Customer, tags []string) error {
	return model.SetCustomerTags(c, tags)
}

// GenerateCustomerUnifiedID 生成客户统一 ID（按优先级 phone>email>wechat>douyin>xiaohongshu）
func GenerateCustomerUnifiedID(c *model.Customer) string {
	return model.GenerateCustomerUnifiedID(c)
}

var (
	customerPhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)
	customerEmailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
)

func looksLikePhone(s string) bool {
	return customerPhonePattern.MatchString(s)
}

func looksLikeEmail(s string) bool {
	return customerEmailPattern.MatchString(s)
}
