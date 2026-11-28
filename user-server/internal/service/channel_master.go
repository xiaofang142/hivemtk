package service

import (
	"context"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/repository"
)

// ChannelMasterService 渠道主数据 CRUD（T012 Controller 的依赖）。
//
// 刻意不与 US4 的 channel_service.go（预设渠道表驱动启停，T055）同名：
// 本服务只负责渠道主数据的增删改查与下拉。
type ChannelMasterService interface {
	List(ctx context.Context, tenantID uint, q dto.ChannelListQuery) ([]dto.ChannelOption, error)
	Get(ctx context.Context, tenantID, id uint) (*dto.ChannelOption, error)
	Create(ctx context.Context, tenantID uint, in *dto.ChannelSave) (*dto.ChannelOption, error)
	Update(ctx context.Context, tenantID, id uint, in *dto.ChannelSave) error
	Delete(ctx context.Context, tenantID, id uint) error
}

type channelMasterService struct {
	repo             repository.ChannelRepository
	identifyCodeRepo repository.ChannelIdentifyCodeRepository
	accountRepo      repository.ChannelAccountRepository
	presetRepo       repository.ChannelPresetStateRepository
}

// NewChannelMasterService 创建渠道主数据服务。
func NewChannelMasterService() ChannelMasterService {
	return &channelMasterService{
		repo:             repository.NewChannelRepository(),
		identifyCodeRepo: repository.NewChannelIdentifyCodeRepository(),
		accountRepo:      repository.NewChannelAccountRepository(),
		presetRepo:       repository.NewChannelPresetStateRepository(),
	}
}

// NewChannelMasterServiceWithRepo 用显式渠道仓储构造（兼容既有调用，其余依赖内部装配）。
func NewChannelMasterServiceWithRepo(repo repository.ChannelRepository) ChannelMasterService {
	return &channelMasterService{
		repo:             repo,
		identifyCodeRepo: repository.NewChannelIdentifyCodeRepository(),
		accountRepo:      repository.NewChannelAccountRepository(),
		presetRepo:       repository.NewChannelPresetStateRepository(),
	}
}

// NewChannelMasterServiceWithDeps 注入全部仓储依赖（测试用）。
func NewChannelMasterServiceWithDeps(
	repo repository.ChannelRepository,
	identifyCodeRepo repository.ChannelIdentifyCodeRepository,
	accountRepo repository.ChannelAccountRepository,
	presetRepos ...repository.ChannelPresetStateRepository,
) ChannelMasterService {
	presetRepo := repository.NewChannelPresetStateRepository()
	if len(presetRepos) > 0 && presetRepos[0] != nil {
		presetRepo = presetRepos[0]
	}
	return &channelMasterService{repo: repo, identifyCodeRepo: identifyCodeRepo, accountRepo: accountRepo, presetRepo: presetRepo}
}

func toChannelOption(ch *model.Channel) dto.ChannelOption {
	return dto.ChannelOption{ID: ch.ID, Name: ch.Name, Code: ch.Code, Type: ch.Type, Enabled: ch.Enabled, Remark: ch.Remark}
}

// toTenantOption 将全局模板状态与当前租户开关合成管理面状态；自定义渠道仍只读租户行。
func (s *channelMasterService) toTenantOption(ctx context.Context, tenantID uint, ch *model.Channel) (dto.ChannelOption, error) {
	opt := toChannelOption(ch)
	if ch.TenantID != repository.ChannelGlobalTenantID {
		return opt, nil
	}
	if s.presetRepo == nil || !s.presetRepo.Available() {
		return dto.ChannelOption{}, repository.ErrChannelStoreUnavailable
	}
	value, err := s.presetRepo.Get(ctx, tenantID, ch.Code)
	if err != nil {
		return dto.ChannelOption{}, err
	}
	tenantEnabled, parseErr := ParseEnabledValue(value)
	if parseErr != nil {
		tenantEnabled = false // 脏值按预设契约 fail-closed，不影响其他渠道管理。
	}
	opt.Enabled = ch.Enabled && tenantEnabled
	return opt, nil
}

func (s *channelMasterService) List(ctx context.Context, tenantID uint, q dto.ChannelListQuery) ([]dto.ChannelOption, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	// 全局预设和当前租户自定义行共同构成管理列表；版本、租户状态与分页在合成后过滤。
	rows, err := s.repo.List(ctx, repository.ChannelListOptions{
		TenantID:      tenantID,
		Keyword:       q.Keyword,
		IncludeGlobal: true,
	})
	if err != nil {
		return nil, err
	}
	filtered := make([]dto.ChannelOption, 0, len(rows))
	for i := range rows {
		if edition.IsDomesticChannel(rows[i].Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
			continue
		}
		opt, err := s.toTenantOption(ctx, tenantID, &rows[i])
		if err != nil {
			return nil, err
		}
		if q.Enabled != nil && opt.Enabled != *q.Enabled {
			continue
		}
		filtered = append(filtered, opt)
	}
	start := q.Offset
	if start < 0 {
		start = 0
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := len(filtered)
	if q.Limit > 0 && start+q.Limit < end {
		end = start + q.Limit
	}
	out := make([]dto.ChannelOption, end-start)
	copy(out, filtered[start:end])
	return out, nil
}

func (s *channelMasterService) Get(ctx context.Context, tenantID, id uint) (*dto.ChannelOption, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	ch, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		// 管理面详情与租户开关还需读取全局预设；自定义行仍严格限定当前租户。
		ch, err = s.repo.GetByID(ctx, repository.ChannelGlobalTenantID, id)
		if err != nil {
			return nil, err
		}
	}
	if ch == nil {
		return nil, ErrChannelNotFound
	}
	opt, err := s.toTenantOption(ctx, tenantID, ch)
	if err != nil {
		return nil, err
	}
	return &opt, nil
}

func (s *channelMasterService) Create(ctx context.Context, tenantID uint, in *dto.ChannelSave) (*dto.ChannelOption, error) {
	if in == nil || tenantID == 0 || in.Name == "" || in.Code == "" {
		return nil, ErrChannelAccountInvalid
	}
	// 版本类型控制：开源版下禁止创建国内渠道（fail-closed）。
	if edition.IsDomesticChannel(in.Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return nil, ErrChannelCodeDisabled
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	typ := in.Type
	if typ == "" {
		typ = "dm"
	}
	ch := &model.Channel{TenantID: tenantID, Name: in.Name, Code: in.Code, Type: typ, Enabled: enabled, Remark: in.Remark}
	if err := s.repo.Create(ctx, ch); err != nil {
		return nil, err
	}
	opt := toChannelOption(ch)
	return &opt, nil
}

func (s *channelMasterService) Update(ctx context.Context, tenantID, id uint, in *dto.ChannelSave) error {
	if in == nil || tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	ch, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if ch == nil {
		return ErrChannelNotFound
	}
	// 版本类型控制：开源版下禁止把渠道改为国内渠道或操作国内渠道。
	if edition.IsDomesticChannel(in.Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return ErrChannelCodeDisabled
	}
	if edition.IsDomesticChannel(ch.Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return ErrChannelCodeDisabled
	}
	updates := map[string]any{"name": in.Name, "code": in.Code, "remark": in.Remark}
	if in.Type != "" {
		updates["type"] = in.Type
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}
	return s.repo.Update(ctx, tenantID, id, updates)
}

func (s *channelMasterService) Delete(ctx context.Context, tenantID, id uint) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	// 先删渠道，再级联清掉该渠道的识别码与账号：识别码表是 BridgeIngressGuard
	// 的权威候选源，漏删会让已删渠道的旧码继续通过鉴权（fail-open）。
	if err := s.repo.Delete(ctx, tenantID, id); err != nil {
		return err
	}
	if err := s.identifyCodeRepo.DeleteByChannel(ctx, tenantID, id); err != nil {
		return err
	}
	return s.accountRepo.DeleteByChannel(ctx, tenantID, id)
}
