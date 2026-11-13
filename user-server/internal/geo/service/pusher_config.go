package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"

	"gorm.io/datatypes"
)

// PusherConfigInput 平台配置的可编辑字段。
//
// 用指针区分「未传」与「传了零值」（Update 只覆盖传了的字段）。id、tenant_id、
// used_today、last_reset_at 一律不接受客户端输入：前两个是系统主键/归属，
// 后两个是配额回写的运行时状态，允许外部写入等于让人随意清空/放大当日额度。
type PusherConfigInput struct {
	Platform   *string
	ConfigJSON json.RawMessage
	DailyLimit *int
	Active     *bool
}

// PusherConfigService 蜘蛛平台配置的读写编排
type PusherConfigService struct {
	repo repository.GeoPusherConfigRepository
}

func NewPusherConfigService(repo repository.GeoPusherConfigRepository) *PusherConfigService {
	return &PusherConfigService{repo: repo}
}

// List 分页查询；limit 上限 200，避免管理端一次拉全表。
func (s *PusherConfigService) List(ctx context.Context, page, limit int) ([]*model.GeoPusherConfig, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	return s.repo.List(ctx, page, limit)
}

func (s *PusherConfigService) Get(ctx context.Context, id string) (*model.GeoPusherConfig, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *PusherConfigService) Create(ctx context.Context, in PusherConfigInput) (*model.GeoPusherConfig, error) {
	platform, err := normalizePlatform(in.Platform)
	if err != nil {
		return nil, err
	}
	if err := validatePusherConfigJSON(in.ConfigJSON); err != nil {
		return nil, err
	}
	cfg := &model.GeoPusherConfig{
		Platform:   platform,
		ConfigJSON: datatypes.JSON(in.ConfigJSON),
		Active:     true,
	}
	if in.DailyLimit != nil {
		cfg.DailyLimit = *in.DailyLimit
	}
	if in.Active != nil {
		cfg.Active = *in.Active
	}
	if err := s.repo.Create(ctx, cfg); err != nil {
		// 平台唯一索引（idx_geo_pusher_configs_platform）冲突时给可执行的提示：
		// 原样透传会让管理端只看到「操作失败，请稍后重试」，用户不知道是重复配置。
		if isPusherPlatformConflict(err) {
			return nil, fmt.Errorf("平台 %s 已存在配置，请直接编辑现有配置", platform)
		}
		return nil, err
	}
	return cfg, nil
}

// isPusherPlatformConflict 认 SQLSTATE 23505 + 本表平台唯一索引名。
// 本仓连接未开 gorm TranslateError，拿到的是底层 *pgconn.PgError 的错误串。
func isPusherPlatformConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") && strings.Contains(msg, "idx_geo_pusher_configs_platform")
}

func (s *PusherConfigService) Update(ctx context.Context, id string, in PusherConfigInput) (*model.GeoPusherConfig, error) {
	cfg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Platform != nil {
		platform, err := normalizePlatform(in.Platform)
		if err != nil {
			return nil, err
		}
		cfg.Platform = platform
	}
	if in.ConfigJSON != nil {
		if err := validatePusherConfigJSON(in.ConfigJSON); err != nil {
			return nil, err
		}
		cfg.ConfigJSON = datatypes.JSON(in.ConfigJSON)
	}
	if in.DailyLimit != nil {
		cfg.DailyLimit = *in.DailyLimit
	}
	if in.Active != nil {
		cfg.Active = *in.Active
	}
	if err := s.repo.Update(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.UpdatedAt = time.Now()
	return cfg, nil
}

func (s *PusherConfigService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// supportedPushPlatforms 是 loadState（push.go）真正会装配 Pusher 的平台全集。
// 必须在配置入口就拒绝未知平台：装配期 switch 对未知平台静默跳过，
// 调用方拿到 code=0 却永远不会被推送（与 ValidatePlatforms 的口径一致）。
var supportedPushPlatforms = map[string]bool{
	"baidu":    true,
	"google":   true,
	"indexnow": true,
}

func normalizePlatform(platform *string) (string, error) {
	if platform == nil || strings.TrimSpace(*platform) == "" {
		return "", errors.New("platform 必填")
	}
	p := strings.TrimSpace(*platform)
	if !supportedPushPlatforms[p] {
		return "", fmt.Errorf("不支持的平台: %s（可选 baidu / google / indexnow）", p)
	}
	return p, nil
}

// validatePusherConfigJSON 必须是 JSON 对象（各平台按 key 取值，数组/标量会让装配静默失效）
func validatePusherConfigJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("config_json 必填")
	}
	if !json.Valid(raw) {
		return errors.New("config_json 不是合法 JSON")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return errors.New("配置 JSON（config_json）必须是 JSON 对象")
	}
	return nil
}
