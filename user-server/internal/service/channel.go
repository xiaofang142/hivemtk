package service

import (
	"context"
	"errors"
	"strconv"
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

// ErrPresetChannelNotFound 预设渠道码不在全局模板中（fail-closed）。
var ErrPresetChannelNotFound = errors.New("预设渠道不存在")

// ErrPresetTenantScope 租户未绑定（fail-closed，绝不回落默认租户）。
var ErrPresetTenantScope = errors.New("预设渠道租户上下文缺失")

// ChannelPresetEnabledDefault 租户未写过 KV 时，预设渠道默认启用。
const ChannelPresetEnabledDefault = "1"

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
	// SetEnabled 开启/关闭渠道（租户内渠道行）。
	SetEnabled(ctx context.Context, tenantID, id uint, enabled bool) error
	// EnsureTenantPresets 幂等初始化租户对全局预设的启用配置（空库/升级/新租户），返回新建行数。
	EnsureTenantPresets(ctx context.Context, tenantID uint) (int, error)
	// IsEnabledForTenant 按租户+渠道码判断启用（模板码走租户 KV，自定义码走租户行）。
	IsEnabledForTenant(ctx context.Context, tenantID uint, code string) (bool, error)
	// RequireEnabledForTenant 同 IsEnabledForTenant，但关闭时返回 ErrChannelCodeDisabled。
	RequireEnabledForTenant(ctx context.Context, tenantID uint, code string) error
	// SetPresetEnabledForTenant 设置租户对预设码的启停（写租户 KV，两租户互不影响）。
	SetPresetEnabledForTenant(ctx context.Context, tenantID uint, code string, enabled bool) error
	// EnabledOptions 需支持租户维度：见下方实现按 tenantID 过滤。
}

type channelService struct {
	repo       repository.ChannelRepository
	presetRepo repository.ChannelPresetStateRepository
}

// NewChannelService 创建预设渠道服务。
func NewChannelService() ChannelService {
	return &channelService{
		repo:       repository.NewChannelRepository(),
		presetRepo: repository.NewChannelPresetStateRepository(),
	}
}

// NewChannelServiceWithRepo 用显式仓储构造（装配点/测试用）。
func NewChannelServiceWithRepo(repo repository.ChannelRepository) ChannelService {
	return &channelService{repo: repo, presetRepo: repository.NewChannelPresetStateRepository()}
}

// NewChannelServiceWithDeps 注入渠道仓储 + 预设状态仓储（测试用）。
func NewChannelServiceWithDeps(repo repository.ChannelRepository, preset repository.ChannelPresetStateRepository) ChannelService {
	return &channelService{repo: repo, presetRepo: preset}
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
		// 全局模板只建一行（channels.code 全局唯一）；租户启用状态另走 KV，
		// 不再给每个租户各插一条同 code（那会因唯一索引失败或只让首个租户拿到预设）。
		if err := s.repo.Create(ctx, &model.Channel{
			TenantID: repository.ChannelGlobalTenantID, Name: p.Name,
			Code: code, Type: typ, Enabled: true,
		}); err != nil {
			return created, err
		}
		created++
	}
	// 顺手幂等初始化该租户的启用配置（新租户/升级路径）。
	if _, err := s.EnsureTenantPresets(ctx, tenantID); err != nil {
		return created, err
	}
	return created, nil
}

// EnsurePresetsOnDemand 是生产装配的「首次访问即自愈」入口：空库启动、老库升级
// 或新租户第一次打开渠道下拉时，经 controller/channel.Options 的 EnabledOptions
// 间接触发，幂等补齐全局模板与该租户启用配置。显式装配点（main.go）不在本批边界内，
// 故以按需幂等初始化接通，不依赖人工执行 SQL 或常量兜底。
func (s *channelService) ensureOnDemand(ctx context.Context, tenantID uint) error {
	if tenantID == 0 || s == nil || s.repo == nil || !s.repo.Available() {
		return nil // 由各方法自己的 fail-closed 判据处理
	}
	if _, err := s.EnsurePresets(ctx, tenantID); err != nil {
		return err
	}
	return nil
}

// EnsureTenantPresets 幂等写租户启用配置（system_config_kv 主键 tenant_id+key）。
// 只写"未配置"的键，不覆盖管理员改过的关闭状态，实现空库/升级/新租户同一入口。
func (s *channelService) EnsureTenantPresets(ctx context.Context, tenantID uint) (int, error) {
	if tenantID == 0 {
		return 0, ErrPresetTenantScope
	}
	if s == nil || s.presetRepo == nil || !s.presetRepo.Available() {
		return 0, repository.ErrChannelStoreUnavailable
	}
	created := 0
	for _, p := range PresetChannelCodes {
		code := strings.ToLower(strings.TrimSpace(p.Code))
		if code == "" || (edition.IsDomesticChannel(code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels)) {
			continue
		}
		val, err := s.presetRepo.Get(ctx, tenantID, code)
		if err != nil {
			return created, err
		}
		if val != "" {
			continue // 已配置（含显式关闭）不覆盖
		}
		if err := s.presetRepo.Set(ctx, tenantID, code, ChannelPresetEnabledDefault); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// tenantPresetEnabled 读租户的预设启用配置：未配置默认启用；显式 0 关闭；
// 非 0/1 脏数据 fail-closed 视为关闭。仓储错误原样上抛。
func (s *channelService) tenantPresetEnabled(ctx context.Context, tenantID uint, code string) (bool, error) {
	if s.presetRepo == nil || !s.presetRepo.Available() {
		return false, repository.ErrChannelStoreUnavailable
	}
	val, err := s.presetRepo.Get(ctx, tenantID, code)
	if err != nil {
		return false, err
	}
	if val == "" {
		return true, nil
	}
	return val == ChannelPresetEnabledDefault, nil
}

func isPresetChannelCode(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, p := range PresetChannelCodes {
		if strings.EqualFold(strings.TrimSpace(p.Code), code) {
			return true
		}
	}
	return false
}

func (s *channelService) IsEnabledForTenant(ctx context.Context, tenantID uint, code string) (bool, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if tenantID == 0 || code == "" {
		return false, ErrPresetTenantScope
	}
	if s == nil || s.repo == nil || !s.repo.Available() {
		return false, repository.ErrChannelStoreUnavailable
	}
	if err := s.ensureOnDemand(ctx, tenantID); err != nil {
		return false, err
	}
	if edition.IsDomesticChannel(code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return false, nil
	}
	// 全局模板行必须存在且启用，否则即使 KV 配了也不能用（fail-closed）。
	tpl, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return false, err
	}
	if tpl == nil || !tpl.Enabled {
		return false, nil
	}
	if isPresetChannelCode(code) {
		return s.tenantPresetEnabled(ctx, tenantID, code)
	}
	// 非预设（自定义）渠道：以租户自己的行状态为准。
	ch, err := s.repo.GetByTenantAndCode(ctx, tenantID, code)
	if err != nil {
		return false, err
	}
	return ch != nil && ch.Enabled, nil
}

func (s *channelService) RequireEnabledForTenant(ctx context.Context, tenantID uint, code string) error {
	ok, err := s.IsEnabledForTenant(ctx, tenantID, code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChannelCodeDisabled
	}
	return nil
}

func (s *channelService) SetPresetEnabledForTenant(ctx context.Context, tenantID uint, code string, enabled bool) error {
	code = strings.ToLower(strings.TrimSpace(code))
	if tenantID == 0 || code == "" || !isPresetChannelCode(code) {
		return ErrPresetChannelNotFound
	}
	if s == nil || s.presetRepo == nil || !s.presetRepo.Available() {
		return repository.ErrChannelStoreUnavailable
	}
	value := "0"
	if enabled {
		value = ChannelPresetEnabledDefault
	}
	return s.presetRepo.Set(ctx, tenantID, code, value)
}

// ParseEnabledValue 把 KV 字符串解析为布尔（仅 0/1 合法；空值默认启用）。
// 独立纯函数便于反例测试，脏数据一律 fail-closed（返回 error）。
func ParseEnabledValue(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		return true, nil
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, strconv.ErrSyntax
	}
}

func (s *channelService) EnabledOptions(ctx context.Context, tenantID uint) ([]dto.ChannelOption, error) {
	if tenantID == 0 {
		return nil, ErrPresetTenantScope
	}
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	if err := s.ensureOnDemand(ctx, tenantID); err != nil {
		return nil, err
	}
	out := make([]dto.ChannelOption, 0, len(PresetChannelCodes))
	// 全局预设模板：逐码读权威行，按该租户 KV 过滤（两租户同预设启停互不影响）。
	for _, p := range PresetChannelCodes {
		code := strings.ToLower(strings.TrimSpace(p.Code))
		if code == "" || (edition.IsDomesticChannel(code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels)) {
			continue
		}
		tpl, err := s.repo.GetByCode(ctx, code)
		if err != nil {
			return nil, err
		}
		if tpl == nil || !tpl.Enabled {
			continue
		}
		ok, err := s.tenantPresetEnabled(ctx, tenantID, code)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, toChannelOption(tpl))
		}
	}
	// 租户内自定义渠道（非预设码）：按租户列表 + 行 Enabled，不对全局码去重。
	rows, err := s.repo.List(ctx, repository.ChannelListOptions{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !isPresetChannelCode(rows[i].Code) && rows[i].Enabled &&
			!(edition.IsDomesticChannel(rows[i].Code) && !edition.AllowsContext(ctx, edition.FeatureDomesticChannels)) {
			out = append(out, toChannelOption(&rows[i]))
		}
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
