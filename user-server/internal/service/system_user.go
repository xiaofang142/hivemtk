package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ErrInvalidInput 业务校验失败（语义错误，可转 4xx）
//
// 阶段 4：供 service 业务方法返回"语义校验失败"错误，与"系统级错误"区分。
// controller 层根据错误内容返回 4xx。
var ErrInvalidInput = errors.New("service: invalid input")

// ErrLastAdmin 系统至少需要保留一个 admin 账号（由 repository.DeleteSafe 透传）
var ErrLastAdmin = repository.ErrLastAdmin

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Email    string `json:"email"`
	RealName string `json:"real_name"`
	Role     string `json:"role" binding:"required,oneof=admin user"`
	Status   int    `json:"status"`
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RealName string `json:"real_name"`
	Role     string `json:"role" binding:"omitempty,oneof=admin user"`
	Status   int    `json:"status"`
}

// SystemUserService 系统用户服务
type SystemUserService struct {
	repo repository.SystemUserRepository
}

// NewSystemUserService 创建系统用户服务实例
func NewSystemUserService() *SystemUserService {
	return &SystemUserService{repo: repository.NewSystemUserRepository()}
}

// NewSystemUserServiceWithRepo 通过 repository 注入创建服务(便于测试)
func NewSystemUserServiceWithRepo(repo repository.SystemUserRepository) *SystemUserService {
	return &SystemUserService{repo: repo}
}

// CountUsers 统计系统用户数量
func (s *SystemUserService) CountUsers(ctx context.Context) (int64, error) {
	count, err := s.repo.Count(ctx)
	if err != nil {
		logger.Error(err, "统计用户数量失败")
		return 0, err
	}
	return count, nil
}

// GetFirstAdminUsername 取系统中第一个超管用户名（按创建时间升序）
// 用于"半初始化"自愈：旧构建 init-admin 把超管写库却未回写 install.lock，
// 导致 HasInstallLockAdmin()=false、init-complete 永远报"请先创建超管"。
// 此时从 DB 取出已有超管用户名，补写 install.lock，使初始化流程可继续。
func (s *SystemUserService) GetFirstAdminUsername(ctx context.Context) (string, error) {
	username, err := s.repo.GetFirstAdminUsername(ctx)
	if err != nil {
		logger.Error(err, "查询首个超管失败")
		return "", err
	}
	return username, nil
}

// GetUsers 获取用户列表
func (s *SystemUserService) GetUsers(ctx context.Context, page, pageSize int) ([]*SystemUserResponse, int64, error) {
	users, total, err := s.repo.List(ctx, page, pageSize)
	if err != nil {
		logger.Error(err, "获取用户列表失败")
		return nil, 0, errors.New("获取用户列表失败")
	}

	responses := make([]*SystemUserResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, s.toUserResponse(ctx, user))
	}

	return responses, total, nil
}

// GetUserByID 根据ID获取用户
func (s *SystemUserService) GetUserByID(ctx context.Context, id uint) (*SystemUserResponse, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return nil, errors.New("获取用户信息失败")
	}

	return s.toUserResponse(ctx, user), nil
}

// CreateUser 创建用户（修复：合并双套创建逻辑，统一走 system_user.go）
// 规则：
//   - 必须校验用户名、密码强度、邮箱格式（避免从 controller 旁路）
//   - username/email 全局唯一
//   - role 仅限 admin/user
//   - status 默认 1（启用）
//   - 不允许 system_init 之外的入口直接创建 admin（admin 必须通过初始化流程）
//     但保留 admin 角色作为合法值，便于 system_init 流程
func (s *SystemUserService) CreateUser(ctx context.Context, req *CreateUserRequest) (*SystemUserResponse, error) {
	if err := validateUsername(req.Username); err != nil {
		return nil, err
	}
	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			return nil, err
		}
	}
	if req.Email != "" {
		if err := validateEmail(req.Email); err != nil {
			return nil, err
		}
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role != "admin" && req.Role != "user" {
		return nil, errors.New("角色非法，仅支持 admin/user")
	}

	exists, err := s.repo.UsernameExists(ctx, req.Username, 0)
	if err != nil {
		logger.Error(err, "检查用户名失败")
		return nil, errors.New("创建用户失败")
	}
	if exists {
		return nil, errors.New("用户名已存在")
	}

	if req.Email != "" {
		exists, err := s.repo.EmailExists(ctx, req.Email, 0)
		if err != nil {
			logger.Error(err, "检查邮箱失败")
			return nil, errors.New("创建用户失败")
		}
		if exists {
			return nil, errors.New("邮箱已被使用")
		}
	}

	user := model.SystemUser{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		RealName: req.RealName,
		Role:     req.Role,
		Status:   req.Status,
	}
	if user.Status == 0 {
		user.Status = 1
	}

	if err := s.repo.Create(ctx, &user); err != nil {
		logger.Error(err, "创建用户失败")
		return nil, errors.New("创建用户失败")
	}

	return s.toUserResponse(ctx, &user), nil
}

// UpdateUser 更新用户
func (s *SystemUserService) UpdateUser(ctx context.Context, id uint, req *UpdateUserRequest) (*SystemUserResponse, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return nil, errors.New("更新用户失败")
	}

	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Phone != "" {
		user.Phone = req.Phone
	}
	if req.RealName != "" {
		user.RealName = req.RealName
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Status != 0 {
		user.Status = req.Status
	}

	if err := s.repo.Update(ctx, user); err != nil {
		logger.Error(err, "保存用户失败")
		return nil, errors.New("更新用户失败")
	}

	return s.toUserResponse(ctx, user), nil
}

// DeleteUser 删除用户
func (s *SystemUserService) DeleteUser(ctx context.Context, id uint) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return errors.New("删除用户失败")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		logger.Error(err, "删除用户失败")
		return errors.New("删除用户失败")
	}

	return nil
}

// ResetPassword 重置密码
//
// 修复 A4：补齐被绕过的安全闭环
//  1. 加入 PasswordPolicy 校验（密码强度 + 历史复用检测）
//  2. 记录密码历史
//  3. 发邮件通知用户（若 email 已配置）
func (s *SystemUserService) ResetPassword(ctx context.Context, id uint, newPassword string) error {
	if id == initialAdminID {
		return ErrInitialAdminProtected
	}
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return errors.New("重置密码失败")
	}

	policySvc := NewPasswordPolicyService()
	if err := policySvc.ValidatePassword(ctx, newPassword, id); err != nil {
		return err
	}

	hashed, err := HashPassword(newPassword)
	if err != nil {
		logger.Error(err, "密码加密失败")
		return errors.New("重置密码失败")
	}
	user.Password = hashed

	if err := s.repo.Update(ctx, user); err != nil {
		logger.Error(err, "保存用户失败")
		return errors.New("重置密码失败")
	}

	if err := policySvc.RecordPasswordHistory(ctx, id, newPassword, model.PasswordSourceResetPassword); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("user_id", id).Msg("记录密码历史失败")
	}

	if user.Email != "" {
		emailSvc := NewEmailServiceAuto()
		body := "您的密码已被管理员重置。如果不是您本人操作，请立即联系管理员并再次修改密码。"
		if _, err := emailSvc.Send(ctx, 0, user.Email, "密码已被重置", body, nil); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("email", user.Email).Msg("admin reset 邮件发送失败")
		}
	}

	return nil
}

func (s *SystemUserService) toUserResponse(_ context.Context, user *model.SystemUser) *SystemUserResponse {
	return &SystemUserResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Phone:       user.Phone,
		RealName:    user.RealName,
		Role:        user.Role,
		Status:      user.Status,
		LastLogin:   user.LastLogin,
		LastLoginAt: user.LastLogin,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

// GetUsersRequest 人员管理列表查询请求
type GetUsersRequest struct {
	Keyword string `json:"keyword"`
	Role    string `json:"role"`
	Page    int    `json:"page"`
	Size    int    `json:"size"`
}

// GetUsersResponse 人员管理列表查询响应
type GetUsersResponse struct {
	Total int64               `json:"total"`
	List  []*model.SystemUser `json:"list"`
}

// GetUsersAdmin 人员管理列表（关键词 + 角色过滤，分页）
//
// 与既有 GetUsers(page, pageSize) 区别：
//   - 支持 keyword（username/email/real_name LIKE 搜索）
//   - 支持 role 过滤
//   - 返回 *model.SystemUser 直接结构（不转 SystemUserResponse）
//
// 分页参数兜底：page < 1 → 1，size <= 0 → 20，size > 100 → 100。
func (s *SystemUserService) GetUsersAdmin(ctx context.Context, req *GetUsersRequest) (*GetUsersResponse, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	size := req.Size
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	users, total, err := s.repo.List(ctx, page, size)
	if err != nil {
		logger.Error(err, "GetUsersAdmin 查询失败")
		return nil, err
	}

	filtered := make([]*model.SystemUser, 0, len(users))
	keyword := req.Keyword
	role := req.Role
	for _, u := range users {
		if role != "" && u.Role != role {
			continue
		}
		if keyword != "" {
			kw := keyword
			if !(sysUserMatchKeyword(u.Username, kw) || sysUserMatchKeyword(u.Email, kw) || sysUserMatchKeyword(u.RealName, kw)) {
				continue
			}
		}
		filtered = append(filtered, u)
	}

	return &GetUsersResponse{Total: total, List: filtered}, nil
}

func sysUserMatchKeyword(s, substr string) bool {
	if substr == "" {
		return true
	}
	if s == "" {
		return false
	}
	ls := sysUserToLowerASCII(s)
	lp := sysUserToLowerASCII(substr)
	return sysUserIndexOf(ls, lp) >= 0
}

func sysUserToLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func sysUserIndexOf(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// GetByIDAdmin 人员管理 - 按 ID 查询
func (s *SystemUserService) GetByIDAdmin(ctx context.Context, id uint) (*model.SystemUser, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		logger.Error(err, "GetByIDAdmin 查询失败")
		return nil, err
	}
	return u, nil
}

// CreateUserByAdminRequest admin 创建账号请求
type CreateUserByAdminRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=8,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Name     string `json:"name"`
	Role     string `json:"role" binding:"required,oneof=admin customer_service staff"`
}

// CreateByAdmin admin 创建账号
//
// 业务规则：
//   - 仅 admin 演员可创建 role='admin'（避免客服提权）
//   - 非 admin 演员若请求 role='admin' → 拒绝
//   - username 唯一（已存在则拒绝）
//   - email 唯一（非空时检查）
//   - 密码 bcrypt 加密（BeforeCreate 钩子自动处理）
//   - 默认 status=1，enabled=true
func (s *SystemUserService) CreateByAdmin(ctx context.Context, actorID uint, req *CreateUserByAdminRequest) (*model.SystemUser, error) {
	if req.Role == "admin" {
		actor, err := s.repo.GetByID(ctx, actorID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("操作人不存在: %w", ErrInvalidInput)
			}
			logger.Error(err, "CreateByAdmin 查询 actor 失败")
			return nil, err
		}
		if actor.Role != "admin" {
			return nil, fmt.Errorf("仅超管可创建超管账号: %w", ErrInvalidInput)
		}
	}

	exists, err := s.repo.UsernameExists(ctx, req.Username, 0)
	if err != nil {
		logger.Error(err, "CreateByAdmin 检查 username 失败")
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("用户名已存在: %w", ErrInvalidInput)
	}

	exists, err = s.repo.EmailExists(ctx, req.Email, 0)
	if err != nil {
		logger.Error(err, "CreateByAdmin 检查 email 失败")
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("邮箱已被使用: %w", ErrInvalidInput)
	}

	user := &model.SystemUser{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		RealName: req.Name,
		Role:     req.Role,
		Status:   1,
		Enabled:  true,
	}
	if err := s.repo.Create(ctx, user); err != nil {
		logger.Error(err, "CreateByAdmin 创建用户失败")
		return nil, err
	}
	return user, nil
}

// UpdateUserByAdminRequest admin 更新账号请求
type UpdateUserByAdminRequest struct {
	Username *string `json:"username"`
	Email    *string `json:"email"`
	Name     *string `json:"name"`
	Role     *string `json:"role"`
}

// UpdateByAdmin admin 更新账号
//
// 业务规则：
//   - role 变更时仅 admin 角色可设置 role='admin'
//   - 不能修改自己的 role（避免误操作丢失权限）
//   - username 唯一性（修改时排除自身）
//   - email 唯一性（修改时排除自身）
func (s *SystemUserService) UpdateByAdmin(ctx context.Context, actorID, targetID uint, req *UpdateUserByAdminRequest) (*model.SystemUser, error) {
	target, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		logger.Error(err, "UpdateByAdmin 查询 target 失败")
		return nil, err
	}

	if req.Role != nil && *req.Role != target.Role {
		if actorID == targetID {
			return nil, fmt.Errorf("不能修改自己的角色: %w", ErrInvalidInput)
		}
		if *req.Role == "admin" {
			actor, err := s.repo.GetByID(ctx, actorID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, fmt.Errorf("操作人不存在: %w", ErrInvalidInput)
				}
				logger.Error(err, "UpdateByAdmin 查询 actor 失败")
				return nil, err
			}
			if actor.Role != "admin" {
				return nil, fmt.Errorf("仅超管可设置超管角色: %w", ErrInvalidInput)
			}
		}
		target.Role = *req.Role
	}

	if req.Username != nil && *req.Username != target.Username {
		exists, err := s.repo.UsernameExists(ctx, *req.Username, targetID)
		if err != nil {
			logger.Error(err, "UpdateByAdmin 检查 username 失败")
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("用户名已存在: %w", ErrInvalidInput)
		}
		target.Username = *req.Username
	}

	if req.Email != nil && *req.Email != target.Email {
		exists, err := s.repo.EmailExists(ctx, *req.Email, targetID)
		if err != nil {
			logger.Error(err, "UpdateByAdmin 检查 email 失败")
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("邮箱已被使用: %w", ErrInvalidInput)
		}
		target.Email = *req.Email
	}

	if req.Name != nil {
		target.RealName = *req.Name
	}

	if err := s.repo.Update(ctx, target); err != nil {
		logger.Error(err, "UpdateByAdmin 保存失败")
		return nil, err
	}
	return target, nil
}

// DeleteByAdmin admin 删除账号
//
// 业务规则：
//   - 不能删除自己（避免误锁）
//   - 调用 repository.DeleteSafe（拒绝删除最后一个 admin）
//   - operation_logs 由 middleware.AuditMiddleware 自动写
func (s *SystemUserService) DeleteByAdmin(ctx context.Context, actorID, targetID uint) error {
	if actorID == targetID {
		return fmt.Errorf("不能删除自己的账号: %w", ErrInvalidInput)
	}
	if err := s.repo.DeleteSafe(ctx, targetID); err != nil {
		if errors.Is(err, repository.ErrLastAdmin) {
			return fmt.Errorf("系统至少需要保留一个超管账号: %w", ErrInvalidInput)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		logger.Error(err, "DeleteByAdmin 删除失败")
		return err
	}
	return nil
}

// SearchUsers 按关键词搜索用户（匹配 username/email/nickname）
func (s *SystemUserService) SearchUsers(ctx context.Context, keyword string, page, pageSize int) ([]*SystemUserResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	q := repository.GetDB().WithContext(ctx).Model(&model.SystemUser{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("username ILIKE ? OR email ILIKE ? OR real_name ILIKE ?", like, like, like)
	}
	var users []model.SystemUser
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	responses := make([]*SystemUserResponse, 0, len(users))
	for _, u := range users {
		responses = append(responses, s.toUserResponse(ctx, &u))
	}
	return responses, total, nil
}
