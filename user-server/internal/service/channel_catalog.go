package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/dto"
)

// ChannelCatalogService 统一渠道目录：一次返回已启用渠道与其下已启用账号。
// 所有前端渠道/账号选择都应消费本服务的出参，避免各处自行拼装列表。
type ChannelCatalogService interface {
	Catalog(ctx context.Context, tenantID uint) (*dto.ChannelCatalogResponse, error)
	// SetChannelEnabledForTenant 租户级渠道开关：预设渠道写租户 KV，非预设渠道写租户行。
	SetChannelEnabledForTenant(ctx context.Context, tenantID, channelID uint, enabled bool) error
}

type channelCatalogService struct {
	channelSvc  ChannelService
	accountSvc  ChannelAccountService
	masterSvc   ChannelMasterService
}

// NewChannelCatalogService 生产装配。
func NewChannelCatalogService() ChannelCatalogService {
	return &channelCatalogService{
		channelSvc: NewChannelService(),
		accountSvc: NewChannelAccountService(),
		masterSvc:  NewChannelMasterService(),
	}
}

// NewChannelCatalogServiceWithDeps 显式依赖构造（测试/装配点用）。
func NewChannelCatalogServiceWithDeps(channelSvc ChannelService, accountSvc ChannelAccountService, masterSvc ChannelMasterService) ChannelCatalogService {
	return &channelCatalogService{channelSvc: channelSvc, accountSvc: accountSvc, masterSvc: masterSvc}
}

func (s *channelCatalogService) Catalog(ctx context.Context, tenantID uint) (*dto.ChannelCatalogResponse, error) {
	if tenantID == 0 {
		return nil, ErrPresetTenantScope
	}
	if s == nil || s.channelSvc == nil || s.accountSvc == nil {
		return nil, fmt.Errorf("channel catalog service not initialized")
	}
	channels, err := s.channelSvc.EnabledOptions(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.accountSvc.ListOptions(ctx, tenantID, dto.ChannelAccountListQuery{})
	if err != nil {
		return nil, err
	}
	enabledByID := make(map[uint]dto.ChannelOption, len(channels))
	out := &dto.ChannelCatalogResponse{
		Channels: make([]dto.ChannelCatalogItem, 0, len(channels)),
		Accounts: make([]dto.ChannelAccountCatalogItem, 0, len(accounts)),
	}
	for i := range channels {
		ch := channels[i]
		enabledByID[ch.ID] = ch
		label := ch.Name
		if label == "" {
			label = ch.Code
		}
		out.Channels = append(out.Channels, dto.ChannelCatalogItem{
			ID: ch.ID, Code: ch.Code, Name: ch.Name, Label: label, Enabled: ch.Enabled, Type: ch.Type,
		})
	}
	for i := range accounts {
		acc := accounts[i]
		ch, ok := enabledByID[acc.ChannelID]
		if !ok {
			// 所属渠道已关闭或不存在：fail-closed，不暴露该账号。
			continue
		}
		label := acc.Name
		if label == "" {
			label = acc.IdentifyCode
		}
		out.Accounts = append(out.Accounts, dto.ChannelAccountCatalogItem{
			ID: acc.ID, ChannelID: acc.ChannelID, ChannelCode: ch.Code,
			Name: acc.Name, Label: label, IdentifyCode: acc.IdentifyCode, Type: acc.Type,
		})
	}
	return out, nil
}

func (s *channelCatalogService) SetChannelEnabledForTenant(ctx context.Context, tenantID, channelID uint, enabled bool) error {
	if tenantID == 0 || channelID == 0 {
		return ErrPresetTenantScope
	}
	if s == nil || s.channelSvc == nil || s.masterSvc == nil {
		return fmt.Errorf("channel catalog service not initialized")
	}
	ch, err := s.masterSvc.Get(ctx, tenantID, channelID)
	if err != nil {
		return err
	}
	if ch == nil {
		return ErrChannelNotFound
	}
	if isPresetChannelCode(ch.Code) {
		return s.channelSvc.SetPresetEnabledForTenant(ctx, tenantID, ch.Code, enabled)
	}
	return s.masterSvc.Update(ctx, tenantID, channelID, &dto.ChannelSave{
		Name: ch.Name, Code: ch.Code, Type: ch.Type, Enabled: &enabled, Remark: ch.Remark,
	})
}
