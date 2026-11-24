package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/repository"
)

// PresetChannelCodes 预设渠道码（数据表驱动的「一次配置全系统生效」基线）。
//
// 新增预设渠道只需在数据表里插入一行（channels.code 全局唯一），全系统读同一张表；
// 这里仅是启动期幂等播种的清单，不是运行期判据——运行期一律以 channels 表为准。
var PresetChannelCodes = []struct {
	Code     string
	Name     string
	Type     string
	Domestic bool
}{
	{"douyin", "抖音", "dm", true},
	{"xiaohongshu", "小红书", "dm", true},
	{"kuaishou", "快手", "dm", true},
	{"xianyu", "闲鱼", "dm", true},
	{"tiktok", "TikTok", "dm", false},
	{"telegram", "Telegram", "group", false},
	{"whatsapp", "WhatsApp", "dm", false},
	{"feishu", "飞书", "group", true},
	{"wecom", "企业微信", "group", true},
	{"qq", "QQ", "dm", true},
}

// ErrChannelCodeDisabled 渠道码存在但被关闭（使用方据此 fail-closed 拒绝）。
var ErrChannelCodeDisabled = errors.New("渠道已关闭")

// ChannelService 预设渠道表驱动服务（FR-022/023）。
//
// 与 ChannelMasterService 的分工：后者负责渠道主数据 CRUD；本服务负责「预设目录」
// 语义——启动期幂等播种、按 code 判启用、开关键、启用下拉。二者共用 ChannelRepository，
// 不重复建模、不重复建表。
type ChannelService interface {
	// EnsurePresets 幂等播种预设渠道（已存在的 code 跳过，不覆盖手工改名/停用）。
	EnsurePresets(ctx context.Context, tenantID uint) (int, error)
	// EnabledOptions 仅返回启用中的渠道（预设目录的唯一下拉源）。
	EnabledOptions(ctx context.Context, tenantID uint) ([]dto.ChannelOption, error)
	// IsEnabled 按渠道码判断是否启用；渠道不存在一律视为未启用（fail-closed）。
	IsEnabled(ctx context.Context, code string) (bool, error)
	// RequireEnabled 与 IsEnabled 等价，但关闭时返回 ErrChannelCodeDisabled。
	RequireEnabled(ctx context.Context, code string) error
	// SetEnabled 开启/关闭渠道。
	SetEnabled(ctx context.Context, tenantID, id uint, enabled bool) error
}

type channelService struct {
	repo repository.ChannelRepository
}

// NewChannelService 创建预设渠道服务。
func NewChannelService() ChannelService {
	return &channelService{repo: repository.NewChannelRepository()}
}

// NewChannelServiceWithRepo 用显式仓储构造（装配点/测试用）。
func NewChannelServiceWithRepo(repo repository.ChannelRepository) ChannelService {
	return &channelService{repo: repo}
}

func (s *channelService) EnsurePresets(ctx context.Context, tenantID uint) (int, error) {
	if s == nil || s.repo == nil || !s.repo.Available() || tenantID == 0 {
		return 0, repository.ErrChannelStoreUnavailable
	}
	created := 0
	for _, p := range PresetChannelCodes {
		code := strings.ToLower(strings.TrimSpace(p.Code))
		if code == "" {
			continue
		}
		// 开源版过滤国内渠道：不播种、不暴露（减少传播风险）。
		if edition.IsDomesticChannel(code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
			continue
		}
		existing, err := s.repo.GetByCode(ctx, code)
		if err != nil {
			return created, err
		}
		if existing != nil {
			continue
		}
		typ := p.Type
		if typ == "" {
			typ = "dm"
		}
		if err := s.repo.Create(ctx, &model.Channel{
			TenantID: tenantID, Name: p.Name, Code: code, Type: typ, Enabled: true,
		}); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func (s *channelService) EnabledOptions(ctx context.Context, tenantID uint) ([]dto.ChannelOption, error) {
	if s == nil || s.repo == nil || !s.repo.Available() || tenantID == 0 {
		return nil, repository.ErrChannelStoreUnavailable
	}
	on := true
	rows, err := s.repo.List(ctx, repository.ChannelListOptions{TenantID: tenantID, Enabled: &on})
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

func (s *channelService) IsEnabled(ctx context.Context, code string) (bool, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return false, repository.ErrChannelStoreUnavailable
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return false, nil
	}
	// 开源版：国内渠道一律视为未启用（fail-closed）。
	if edition.IsDomesticChannel(code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return false, nil
	}
	ch, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return false, err
	}
	return ch != nil && ch.Enabled, nil
}

func (s *channelService) RequireEnabled(ctx context.Context, code string) error {
	ok, err := s.IsEnabled(ctx, code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChannelCodeDisabled
	}
	return nil
}

func (s *channelService) SetEnabled(ctx context.Context, tenantID, id uint, enabled bool) error {
	if s == nil || s.repo == nil || !s.repo.Available() || tenantID == 0 || id == 0 {
		return repository.ErrChannelStoreUnavailable
	}
	// 版本类型控制：开源版下不允许启用/操作国内渠道（fail-closed）。
	if ch, err := s.repo.GetByID(ctx, tenantID, id); err == nil && ch != nil {
		if edition.IsDomesticChannel(ch.Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
			return ErrChannelCodeDisabled
		}
	}
	return s.repo.Update(ctx, tenantID, id, map[string]any{"enabled": enabled})
}
