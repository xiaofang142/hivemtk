package service

import (
	"context"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
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
	repo repository.ChannelRepository
}

// NewChannelMasterService 创建渠道主数据服务。
func NewChannelMasterService() ChannelMasterService {
	return &channelMasterService{repo: repository.NewChannelRepository()}
}

// NewChannelMasterServiceWithRepo 用显式仓储构造。
func NewChannelMasterServiceWithRepo(repo repository.ChannelRepository) ChannelMasterService {
	return &channelMasterService{repo: repo}
}

func toChannelOption(ch *model.Channel) dto.ChannelOption {
	return dto.ChannelOption{ID: ch.ID, Name: ch.Name, Code: ch.Code, Type: ch.Type, Enabled: ch.Enabled}
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
	updates := map[string]any{"name": in.Name, "code": in.Code, "type": in.Type, "remark": in.Remark}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}
	return s.repo.Update(ctx, tenantID, id, updates)
}

func (s *channelMasterService) Delete(ctx context.Context, tenantID, id uint) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	return s.repo.Delete(ctx, tenantID, id)
}
