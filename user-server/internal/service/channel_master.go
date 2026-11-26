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
}

// NewChannelMasterService 创建渠道主数据服务。
func NewChannelMasterService() ChannelMasterService {
	return &channelMasterService{
		repo:             repository.NewChannelRepository(),
		identifyCodeRepo: repository.NewChannelIdentifyCodeRepository(),
		accountRepo:      repository.NewChannelAccountRepository(),
	}
}

// NewChannelMasterServiceWithRepo 用显式渠道仓储构造（兼容既有调用，其余依赖内部装配）。
func NewChannelMasterServiceWithRepo(repo repository.ChannelRepository) ChannelMasterService {
	return &channelMasterService{
		repo:             repo,
		identifyCodeRepo: repository.NewChannelIdentifyCodeRepository(),
		accountRepo:      repository.NewChannelAccountRepository(),
	}
}

// NewChannelMasterServiceWithDeps 注入全部仓储依赖（测试用）。
func NewChannelMasterServiceWithDeps(
	repo repository.ChannelRepository,
	identifyCodeRepo repository.ChannelIdentifyCodeRepository,
	accountRepo repository.ChannelAccountRepository,
) ChannelMasterService {
	return &channelMasterService{repo: repo, identifyCodeRepo: identifyCodeRepo, accountRepo: accountRepo}
}

func toChannelOption(ch *model.Channel) dto.ChannelOption {
	return dto.ChannelOption{ID: ch.ID, Name: ch.Name, Code: ch.Code, Type: ch.Type, Enabled: ch.Enabled, Remark: ch.Remark}
}

func (s *channelMasterService) List(ctx context.Context, tenantID uint, q dto.ChannelListQuery) ([]dto.ChannelOption, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	rows, err := s.repo.List(ctx, repository.ChannelListOptions{
		TenantID: tenantID,
		Keyword:  q.Keyword,
		Enabled:  q.Enabled,
		Limit:    q.Limit,
		Offset:   q.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelOption, 0, len(rows))
	for i := range rows {
		if edition.IsDomesticChannel(rows[i].Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
			continue
		}
		out = append(out, toChannelOption(&rows[i]))
	}
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
		return nil, ErrChannelNotFound
	}
	opt := toChannelOption(ch)
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
