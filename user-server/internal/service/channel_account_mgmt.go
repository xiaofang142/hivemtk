package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/repository"
)

// ChannelAccountManageService 管理面账号服务（CH-02/03）。
//
// 与 ChannelAccountService 的业务下拉分工：下拉默认仅启用、字段窄；
// 管理面必须支持启用/停用/全部过滤、准确分页 total、备注完整回读，
// 且详情加载成功前禁止提交，避免把未加载字段清空。
type ChannelAccountManageService interface {
	// ListManaged 管理列表：Status=nil 表示全部，否则按 0/1 过滤；total 与过滤口径一致。
	ListManaged(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountDetail, int64, error)
	// GetDetail 完整字段回填（含 remark/自他名称/类型），未找到 fail-closed。
	GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error)
	// UpdateManaged 详情完整加载后才允许提交：以当前库行作为基线合并入参，
	// 未提供的字段保留原值；明确清空用指针/独立标识（见 ChannelAccountManagedSave）。
	UpdateManaged(ctx context.Context, tenantID, id uint, in *ChannelAccountManagedSave) error
	// SetStatus 启用/停用（停用账号可在「停用/全部」过滤找回再启用）。
	SetStatus(ctx context.Context, tenantID, id uint, status int) error
}

// ChannelAccountManagedSave 管理面编辑入参：指针 nil 表示本次未改（保留库内原值），
// 非 nil 表示明确写入（含空串=明确清空）。这样「未提交」与「明确清空」可区分。
type ChannelAccountManagedSave struct {
	ChannelID        *uint
	Name             *string
	Remark           *string
	SelfIdentifyName *string
	Type             *string
}

type channelAccountManageService struct {
	repo repository.ChannelAccountRepository
	mgmt repository.ChannelAccountManageRepository
}

// NewChannelAccountManageService 创建管理面账号服务（全局 DB 装配）。
func NewChannelAccountManageService() ChannelAccountManageService {
	return &channelAccountManageService{
		repo: repository.NewChannelAccountRepository(),
		mgmt: repository.NewChannelAccountManageRepository(),
	}
}

// NewChannelAccountManageServiceWithRepos 用显式仓储构造（测试用）。
func NewChannelAccountManageServiceWithRepos(repo repository.ChannelAccountRepository, mgmt repository.ChannelAccountManageRepository) ChannelAccountManageService {
	return &channelAccountManageService{repo: repo, mgmt: mgmt}
}

// ListManaged 管理列表：默认「全部」，不套用业务下拉的只启用过滤。
func (s *channelAccountManageService) ListManaged(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountDetail, int64, error) {
	if tenantID == 0 {
		return nil, 0, ErrChannelAccountInvalid
	}
	if s == nil || s.mgmt == nil || !s.mgmt.Available() {
		return nil, 0, repository.ErrChannelStoreUnavailable
	}
	opts := repository.ChannelAccountListOptions{
		TenantID: tenantID, ChannelID: q.ChannelID, Status: q.Status,
		Keyword: q.Keyword, IDs: q.IDs, Limit: q.Limit, Offset: q.Offset,
	}
	rows, err := s.mgmt.ListManaged(ctx, opts)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.mgmt.CountManaged(ctx, opts)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.ChannelAccountDetail, 0, len(rows))
	for i := range rows {
		out = append(out, toAccountDetail(&rows[i]))
	}
	return out, total, nil
}

func (s *channelAccountManageService) GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelAccountNotFound
	}
	detail := toAccountDetail(acc)
	return &detail, nil
}

// UpdateManaged 以库内行作为基线：指针字段 nil 表示「本次不改」，非 nil 表示
// 明确写入（含空串=明确清空）；名称/渠道/类型有值才覆盖，避免详情失败后清空。
func (s *channelAccountManageService) UpdateManaged(ctx context.Context, tenantID, id uint, in *ChannelAccountManagedSave) error {
	if tenantID == 0 || id == 0 || in == nil {
		return ErrChannelAccountInvalid
	}
	if s == nil || s.repo == nil || !s.repo.Available() {
		return repository.ErrChannelStoreUnavailable
	}
	existing, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrChannelAccountNotFound
	}
	updates := map[string]any{}
	if in.ChannelID != nil {
		updates["channel_id"] = *in.ChannelID
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return ErrChannelAccountInvalid
		}
		updates["name"] = name
	}
	if in.Remark != nil {
		updates["remark"] = *in.Remark
	}
	if in.SelfIdentifyName != nil {
		updates["self_identify_name"] = *in.SelfIdentifyName
	}
	if in.Type != nil {
		typ := strings.ToLower(strings.TrimSpace(*in.Type))
		if typ != "" && !allowedChannelAccountTypes[typ] {
			return ErrChannelAccountInvalid
		}
		updates["type"] = typ
	}
	if len(updates) == 0 {
		return nil // 详情失败/未提交：不写任何字段
	}
	return s.repo.Update(ctx, tenantID, id, updates)
}

// SetStatus 仅改 status，不触碰备注/名称/自他名称/类型。
func (s *channelAccountManageService) SetStatus(ctx context.Context, tenantID, id uint, status int) error {
	if tenantID == 0 || id == 0 || (status != 0 && status != 1) {
		return ErrChannelAccountInvalid
	}
	if s == nil || s.repo == nil || !s.repo.Available() {
		return repository.ErrChannelStoreUnavailable
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if acc == nil {
		return ErrChannelAccountNotFound
	}
	return s.repo.Update(ctx, tenantID, id, map[string]any{"status": status})
}
