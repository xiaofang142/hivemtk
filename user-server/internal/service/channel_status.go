package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// CH-05 统一状态判定/变更契约（先交，供 CH-07 / LEAD-03 消费）。
//
// 单一判据：渠道与账号必须同时处于启用态，任何一侧关闭/缺失/查询失败都拒绝。
// 全入口 worker 退出矩阵属整项余项（随 07/LEAD-03/09 集成后补），不形成互等。
var (
	// ErrChannelDisabled 渠道禁用（含 edition 过滤视同不存在）。
	ErrChannelDisabled = errors.New("渠道已停用")
	// ErrChannelAccountDisabled 账号停用。
	ErrChannelAccountDisabled = errors.New("渠道账号已停用")
)

// ChannelStatusReader 运行状态的最小查询端口（Repository 层实现）。
type ChannelStatusReader interface {
	Available() bool
	ChannelByID(ctx context.Context, tenantID, id uint) (*model.Channel, error)
	AccountByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error)
}

// channelStatusReader 用现有渠道/账号仓储拼装，不改仓储接口。
type channelStatusReader struct {
	ch  repository.ChannelRepository
	acc repository.ChannelAccountRepository
}

// NewChannelStatusReader 创建状态判定仓储。
func NewChannelStatusReader(ch repository.ChannelRepository, acc repository.ChannelAccountRepository) ChannelStatusReader {
	return &channelStatusReader{ch: ch, acc: acc}
}

func (r *channelStatusReader) Available() bool {
	return r != nil && r.ch != nil && r.ch.Available() && r.acc != nil && r.acc.Available()
}

func (r *channelStatusReader) ChannelByID(ctx context.Context, tenantID, id uint) (*model.Channel, error) {
	return r.ch.GetByID(ctx, tenantID, id)
}

func (r *channelStatusReader) AccountByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error) {
	return r.acc.GetByID(ctx, tenantID, id)
}

// ChannelStatusService 统一状态判定入口。
type ChannelStatusService interface {
	// RequireRunnable 校验渠道+账号同时启用；返回规范账号或明确 typed error。
	RequireRunnable(ctx context.Context, tenantID, channelID, accountID uint) (*ResolvedChannelAccount, error)
	// AccountEnabled 只看账号状态（供只知道账号 ID 的调用方使用）。
	AccountEnabled(ctx context.Context, tenantID, accountID uint) (bool, error)
	// ChannelEnabled 只看渠道状态（按租户+渠道码）。
	ChannelEnabled(ctx context.Context, tenantID uint, code string) (bool, error)
}

// ChannelRunnableGate CH-05/CH-10 运行入口统一闸门。
// ChannelStatusService 自身即实现；dispatch 等入口只依赖这个窄口，
// 不碰 *gorm.DB，也不重复渠道/账号状态判据。
type ChannelRunnableGate interface {
	RequireRunnable(ctx context.Context, tenantID, channelID, accountID uint) (*ResolvedChannelAccount, error)
}

type channelStatusService struct {
	reader ChannelStatusReader
	preset ChannelService
}

// NewChannelStatusService 创建状态服务（全局仓储装配）。
func NewChannelStatusService() ChannelStatusService {
	return &channelStatusService{
		reader: NewChannelStatusReader(repository.NewChannelRepository(), repository.NewChannelAccountRepository()),
		preset: NewChannelService(),
	}
}

// NewChannelStatusServiceWithDeps 用显式依赖构造（测试用）。
func NewChannelStatusServiceWithDeps(reader ChannelStatusReader, preset ChannelService) ChannelStatusService {
	return &channelStatusService{reader: reader, preset: preset}
}

func (s *channelStatusService) RequireRunnable(ctx context.Context, tenantID, channelID, accountID uint) (*ResolvedChannelAccount, error) {
	if tenantID == 0 || channelID == 0 || accountID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if s == nil || s.reader == nil || !s.reader.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	ch, err := s.reader.ChannelByID(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	if ch == nil || !ch.Enabled {
		return nil, ErrChannelDisabled
	}
	// CH-01/CH-05：渠道主表 Enabled 之外，必须再消费租户 KV 启停（两租户互不影响）。
	// 预设码走租户 KV，自定义码走租户行；无 KV/仓储故障一律 fail-closed，
	// 不因查库失败把已关闭渠道重新放行。
	if s.preset != nil {
		tenantEnabled, perr := s.preset.IsEnabledForTenant(ctx, tenantID, ch.Code)
		if perr != nil {
			return nil, perr
		}
		if !tenantEnabled {
			return nil, ErrChannelDisabled
		}
	}
	acc, err := s.reader.AccountByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.Status != 1 || acc.ChannelID != channelID {
		return nil, ErrChannelAccountDisabled
	}
	return &ResolvedChannelAccount{TenantID: tenantID, ChannelID: channelID, AccountID: accountID, ChannelCode: ch.Code, IdentifyCode: acc.IdentifyCode}, nil
}

func (s *channelStatusService) AccountEnabled(ctx context.Context, tenantID, accountID uint) (bool, error) {
	if tenantID == 0 || accountID == 0 || s == nil || s.reader == nil || !s.reader.Available() {
		return false, repository.ErrChannelStoreUnavailable
	}
	acc, err := s.reader.AccountByID(ctx, tenantID, accountID)
	if err != nil {
		return false, err
	}
	return acc != nil && acc.Status == 1, nil
}

func (s *channelStatusService) ChannelEnabled(ctx context.Context, tenantID uint, code string) (bool, error) {
	if tenantID == 0 || code == "" || s == nil || s.preset == nil {
		return false, ErrChannelAccountInvalid
	}
	return s.preset.IsEnabledForTenant(ctx, tenantID, code)
}
