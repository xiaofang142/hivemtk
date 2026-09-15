// script_ab.go 话术版本管理 + AB 曝光归因（T-6/T-7，MASTER_COMPETITIVE_DECISIONS.md M10）
//
// 业界依据（Langfuse prompt management / Gong 话术归因 / GrowthBook 分桶）：
//   - 版本 = 递增整数，历史不可变（script_versions 快照表）
//   - 发布 = 激活某版本（ScriptLibrary.Version 指针），过期 = status=expired 或 expires_at
//   - AB 分桶 = FNV-1a 确定性哈希（同 script+one_id 恒定同桶，粘性）
//   - 曝光日志 fire-and-forget，绝不阻塞会话主链路
//   - 转化归因窗口可配（默认 48h），同 one_id+conversation_id 回写 converted
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// ScriptABAttributionHours 默认归因窗口（小时）
const ScriptABAttributionHours = 48

// ScriptABConfig 话术级 AB 配置（system_config_kv: script_ab.{scriptID}）
type ScriptABConfig struct {
	Enabled      bool `json:"enabled"`
	SplitA       int  `json:"split_a"`
	AttributionH int  `json:"attribution_h"`
}

// DefaultScriptABConfig 默认配置：启用 50/50，48h 归因
func DefaultScriptABConfig() ScriptABConfig {
	return ScriptABConfig{Enabled: true, SplitA: 50, AttributionH: ScriptABAttributionHours}
}

// ScriptABService 话术版本与 AB 曝光服务
type ScriptABService struct {
	repo     *repository.ScriptLibraryRepository
	kvGetter func(ctx context.Context, key string) (string, bool)
	kvSetter func(ctx context.Context, key, val string) error
	now      func() time.Time
}

// NewScriptABService 创建服务（kv 未注入时 AB 分桶退化为默认 50/50 且配置不持久化）
func NewScriptABService(repo *repository.ScriptLibraryRepository) *ScriptABService {
	return &ScriptABService{repo: repo, now: time.Now}
}

// NewScriptABServiceFromGlobal 全局 DB 装配入口：repo + kv 端口一站式注入。
// 供 router 装配层调用，controller 不得直连 repository（depguard 规则）。
func NewScriptABServiceFromGlobal() *ScriptABService {
	svc := NewScriptABService(repository.NewScriptLibraryRepositoryFromGlobal())
	kvRepo := repository.NewSystemConfigKVRepository()
	svc.SetKVStore(
		func(ctx context.Context, key string) (string, bool) {
			v, err := kvRepo.Get(ctx, key)
			if err != nil || v == "" {
				return "", false
			}
			return v, true
		},
		func(ctx context.Context, key, val string) error {
			_, err := kvRepo.Upsert(ctx, key, val)
			return err
		},
	)
	return svc
}

// SetKVStore 注入 system_config_kv 读写端口（DI 由路由装配完成）
func (s *ScriptABService) SetKVStore(getter func(ctx context.Context, key string) (string, bool), setter func(ctx context.Context, key, val string) error) {
	s.kvGetter = getter
	s.kvSetter = setter
}

// SetClock 注入时钟（测试用）
func (s *ScriptABService) SetClock(now func() time.Time) { s.now = now }

func scriptABKVKey(scriptID uint) string { return fmt.Sprintf("script_ab.%d", scriptID) }

// GetConfig 读取 AB 配置（未配置/解析失败回退默认）
func (s *ScriptABService) GetConfig(ctx context.Context, scriptID uint) ScriptABConfig {
	cfg := DefaultScriptABConfig()
	if s.kvGetter == nil {
		return cfg
	}
	raw, ok := s.kvGetter(ctx, scriptABKVKey(scriptID))
	if !ok || raw == "" {
		return cfg
	}
	var parsed ScriptABConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[ScriptAB] 配置解析失败，回退默认", "script_id", scriptID, "err", err)
		return cfg
	}
	if parsed.SplitA <= 0 || parsed.SplitA >= 100 {
		parsed.SplitA = 50
	}
	if parsed.AttributionH <= 0 {
		parsed.AttributionH = ScriptABAttributionHours
	}
	return parsed
}

// SaveConfig 持久化 AB 配置
func (s *ScriptABService) SaveConfig(ctx context.Context, scriptID uint, cfg ScriptABConfig) error {
	if cfg.SplitA <= 0 || cfg.SplitA >= 100 {
		return fmt.Errorf("split_a 必须在 (0,100) 开区间内")
	}
	if cfg.AttributionH <= 0 {
		cfg.AttributionH = ScriptABAttributionHours
	}
	if s.kvSetter == nil {
		return fmt.Errorf("配置存储未初始化")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.kvSetter(ctx, scriptABKVKey(scriptID), string(raw))
}

// AssignBucket FNV-1a 确定性分桶：hash(scriptID:oneID) % 100 < SplitA → A，否则 B
func (s *ScriptABService) AssignBucket(scriptID uint, oneID string, cfg ScriptABConfig) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s", scriptID, oneID)))
	if int(h.Sum32()%100) < cfg.SplitA {
		return "A"
	}
	return "B"
}

// ListVersions 版本历史
func (s *ScriptABService) ListVersions(ctx context.Context, scriptID uint) ([]model.ScriptVersion, error) {
	return s.repo.ListScriptVersions(ctx, scriptID)
}

// CreateVersion 从话术当前内容创建新版本快照并激活（历史不可变，发布=激活指针）
func (s *ScriptABService) CreateVersion(ctx context.Context, scriptID uint, note string, createdBy uint) (*model.ScriptVersion, error) {
	var sc model.ScriptLibrary
	if err := s.repo.FirstScriptByID(ctx, scriptID, &sc); err != nil {
		return nil, fmt.Errorf("话术不存在: %w", err)
	}
	maxVer, err := s.repo.MaxScriptVersion(ctx, scriptID)
	if err != nil {
		return nil, err
	}
	v := &model.ScriptVersion{
		ScriptID:  scriptID,
		Version:   maxVer + 1,
		Title:     sc.Title,
		Content:   sc.Content,
		Status:    "active",
		Note:      note,
		CreatedBy: createdBy,
	}
	if err := s.repo.CreateScriptVersion(ctx, v); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateScriptActivation(ctx, scriptID, v.Version, "active", nil); err != nil {
		return nil, err
	}
	return v, nil
}

// ActivateVersion 激活历史版本（回滚=激活旧版本，Langfuse label 语义）
func (s *ScriptABService) ActivateVersion(ctx context.Context, scriptID uint, version int) error {
	var v model.ScriptVersion
	if err := s.repo.FirstVersionByID(ctx, scriptID, version, &v); err != nil {
		return fmt.Errorf("版本不存在: %w", err)
	}
	return s.repo.UpdateScriptActivation(ctx, scriptID, version, "active", nil)
}

// ExpireScript 过期下线话术（expires_at=now，后续加载链路跳过）
func (s *ScriptABService) ExpireScript(ctx context.Context, scriptID uint) error {
	now := s.now()
	return s.repo.UpdateScriptActivation(ctx, scriptID, 0, "expired", &now)
}

// RecordExposure 记录曝光（调用方以 goroutine fire-and-forget 调用）
func (s *ScriptABService) RecordExposure(scriptID uint, version int, oneID string, customerID string, conversationID, traceID string) {
	if scriptID == 0 || oneID == "" {
		return
	}
	cfg := s.GetConfig(context.Background(), scriptID)
	e := &model.ScriptExposureLog{
		ScriptID:       scriptID,
		Version:        version,
		Bucket:         s.AssignBucket(scriptID, oneID, cfg),
		CustomerID:     customerID,
		OneID:          oneID,
		ConversationID: conversationID,
		TraceID:        traceID,
		ExposedAt:      s.now(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
	defer cancel()
	if err := s.repo.CreateScriptExposure(ctx, e); err != nil {
		slog.Warn("[ScriptAB] 曝光落库失败（不影响主链路）", "script_id", scriptID, "err", err)
	}
}

func (s *ScriptABService) attributionHoursFor(ctx context.Context, scriptID uint) int {
	if s.kvGetter == nil {
		return ScriptABAttributionHours
	}
	if raw, ok := s.kvGetter(ctx, scriptABKVKey(scriptID)); ok && raw != "" {
		var parsed ScriptABConfig
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed.AttributionH > 0 {
			return parsed.AttributionH
		}
	}
	return ScriptABAttributionHours
}

func (s *ScriptABService) RecordConversion(ctx context.Context, oneID, conversationID, outcome string) error {

	scriptIDs, err := s.repo.ListScriptIDsByExposureAnchor(ctx, oneID, conversationID)
	if err != nil || len(scriptIDs) == 0 {

		since := s.now().Add(-ScriptABAttributionHours * time.Hour)
		n, err := s.repo.MarkScriptExposuresConverted(ctx, oneID, conversationID, outcome, s.now(), since)
		if err != nil {
			return err
		}
		if n > 0 {
			slog.Info("[ScriptAB] 转化归因回写(默认窗)", "one_id", oneID, "conversation_id", conversationID, "rows", n)
		}
		return err
	}
	total := int64(0)
	for _, sid := range scriptIDs {
		hours := s.attributionHoursFor(ctx, sid)
		since := s.now().Add(-time.Duration(hours) * time.Hour)
		n, err := s.repo.MarkScriptExposuresConverted(ctx, oneID, conversationID, outcome, s.now(), since, sid)
		if err != nil {
			slog.Warn("[ScriptAB] 转化归因回写失败", "script_id", sid, "err", err)
			continue
		}
		total += n
	}
	if total > 0 {
		slog.Info("[ScriptAB] 转化归因回写", "one_id", oneID, "conversation_id", conversationID, "rows", total)
	}
	return nil
}

// ABStats 各版本×分桶曝光/转化统计（运营对比"哪版话术转化高"）
func (s *ScriptABService) ABStats(ctx context.Context, scriptID uint) (map[string]any, error) {
	rows, err := s.repo.ScriptExposureStats(ctx, scriptID)
	if err != nil {
		return nil, err
	}
	cfg := s.GetConfig(ctx, scriptID)
	type verAgg struct {
		Version     int     `json:"version"`
		Exposures   int64   `json:"exposures"`
		Conversions int64   `json:"conversions"`
		Rate        float64 `json:"conversion_rate"`
	}
	byVer := map[int]*verAgg{}
	for _, r := range rows {
		a, ok := byVer[r.Version]
		if !ok {
			a = &verAgg{Version: r.Version}
			byVer[r.Version] = a
		}
		a.Exposures += r.Exposures
		a.Conversions += r.Conversions
		if r.Exposures > 0 {
			a.Rate = float64(a.Conversions) / float64(a.Exposures)
		}
	}
	versions := make([]verAgg, 0, len(byVer))
	for _, a := range byVer {
		versions = append(versions, *a)
	}
	return map[string]any{
		"config":          cfg,
		"buckets":         rows,
		"versions":        versions,
		"attribution_hrs": cfg.AttributionH,
	}, nil
}
