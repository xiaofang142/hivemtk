// feature_flag.go FeatureFlag 服务（K2 五层 L3）
//
// 评估/分桶依据 Unleash stickiness + GrowthBook rollout：
//   - FNV-1a(key:contextID) % 100 < RolloutPercentage → 放量内（同上下文恒定同结果）
//   - kill switch：Enabled=false 一票关闭（优先级最高）
//   - 评估写 eval log（impression data），失败不影响评估结果
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"runtime/debug"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// FeatureFlagStaleDays stale 判定阈值（GrowthBook 默认两周）
const FeatureFlagStaleDays = 14

// FlagEvaluateResult 评估结果
type FlagEvaluateResult struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Value   any    `json:"value,omitempty"`
	Reason  string `json:"reason"`
}

// FeatureFlagService 功能开关服务
type FeatureFlagService struct {
	repo repository.FeatureFlagRepository
	now  func() time.Time
}

// NewFeatureFlagService 构造
func NewFeatureFlagService(repo repository.FeatureFlagRepository) *FeatureFlagService {
	return &FeatureFlagService{repo: repo, now: time.Now}
}

// NewFeatureFlagServiceFromGlobal 便捷构造
func NewFeatureFlagServiceFromGlobal() *FeatureFlagService {
	return NewFeatureFlagService(repository.NewFeatureFlagRepositoryFromGlobal())
}

// CreateReq 创建请求
type FlagCreateRequest struct {
	Key               string `json:"key" binding:"required"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Enabled           bool   `json:"enabled"`
	RolloutPercentage *int   `json:"rollout_percentage"`
	Payload           string `json:"payload"`
	Tags              string `json:"tags"`
}

// UpdateReq 更新请求
type FlagUpdateRequest struct {
	Name              *string `json:"name"`
	Description       *string `json:"description"`
	RolloutPercentage *int    `json:"rollout_percentage"`
	Payload           *string `json:"payload"`
	Tags              *string `json:"tags"`
}

func normalizeRollout(p int) (int, error) {
	if p < 0 || p > 100 {
		return 0, fmt.Errorf("rollout_percentage 必须在 0-100")
	}
	return p, nil
}

// Create 创建 Flag
func (s *FeatureFlagService) Create(ctx context.Context, req *FlagCreateRequest, actorID uint) (*model.FeatureFlag, error) {
	if req.Key == "" {
		return nil, errors.New("key 不能为空")
	}
	if _, err := s.repo.GetByKey(ctx, req.Key); err == nil {
		return nil, fmt.Errorf("flag key 已存在: %s", req.Key)
	}
	rollout := 100
	if req.RolloutPercentage != nil {
		var err error
		if rollout, err = normalizeRollout(*req.RolloutPercentage); err != nil {
			return nil, err
		}
	}
	f := &model.FeatureFlag{
		Key:               req.Key,
		Name:              req.Name,
		Description:       req.Description,
		Enabled:           req.Enabled,
		RolloutPercentage: rollout,
		Payload:           req.Payload,
		Tags:              req.Tags,
		CreatedBy:         actorID,
	}
	if err := s.repo.Create(ctx, f); err != nil {
		return nil, err
	}
	s.auditAsync(ctx, f, "create", actorID, "创建 flag")
	return f, nil
}

// Update 更新 Flag（部分更新，nil 字段不覆盖）
func (s *FeatureFlagService) Update(ctx context.Context, id uint, req *FlagUpdateRequest, actorID uint) (*model.FeatureFlag, error) {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		f.Name = *req.Name
	}
	if req.Description != nil {
		f.Description = *req.Description
	}
	if req.Payload != nil {
		f.Payload = *req.Payload
	}
	if req.Tags != nil {
		f.Tags = *req.Tags
	}
	if req.RolloutPercentage != nil {
		if f.RolloutPercentage, err = normalizeRollout(*req.RolloutPercentage); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Update(ctx, f); err != nil {
		return nil, err
	}
	s.auditAsync(ctx, f, "update", actorID, "更新 flag")
	return f, nil
}

// Delete 删除 Flag
func (s *FeatureFlagService) Delete(ctx context.Context, id uint, actorID uint) error {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAsync(ctx, f, "delete", actorID, "删除 flag")
	return nil
}

// SetEnabled 启用/禁用（kill switch）
func (s *FeatureFlagService) SetEnabled(ctx context.Context, id uint, enabled bool, actorID uint) (*model.FeatureFlag, error) {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	f.Enabled = enabled
	if err := s.repo.Update(ctx, f); err != nil {
		return nil, err
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	s.auditAsync(ctx, f, action, actorID, "")
	return f, nil
}

// SetRollout 设置灰度百分比
func (s *FeatureFlagService) SetRollout(ctx context.Context, id uint, percentage int, actorID uint) (*model.FeatureFlag, error) {
	p, err := normalizeRollout(percentage)
	if err != nil {
		return nil, err
	}
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	f.RolloutPercentage = p
	if err := s.repo.Update(ctx, f); err != nil {
		return nil, err
	}
	s.auditAsync(ctx, f, "rollout", actorID, fmt.Sprintf("rollout=%d%%", p))
	return f, nil
}

// List 分页
func (s *FeatureFlagService) List(ctx context.Context, page, pageSize int) ([]model.FeatureFlag, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

// GetByID 按 ID 查（:id 同时兼容数字 ID 与 flag key）
func (s *FeatureFlagService) GetByIDOrKey(ctx context.Context, idOrKey string) (*model.FeatureFlag, error) {
	var id uint
	if _, err := fmt.Sscanf(idOrKey, "%d", &id); err == nil && id > 0 {
		return s.repo.GetByID(ctx, id)
	}
	return s.repo.GetByKey(ctx, idOrKey)
}

// Evaluate 单 Flag 评估
func (s *FeatureFlagService) Evaluate(ctx context.Context, key string, attributes map[string]any) *FlagEvaluateResult {
	res := &FlagEvaluateResult{Key: key, Enabled: false, Reason: "not_found"}
	f, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		s.logEvalAsync(ctx, key, attributes, res)
		return res
	}
	contextID := contextIDFromAttributes(attributes)
	switch {
	case !f.Enabled:
		res.Reason = "disabled"
	case f.RolloutPercentage >= 100:
		res.Enabled = true
		res.Reason = "rollout"
	case f.RolloutPercentage <= 0:
		res.Reason = "rollout_excluded"
	default:
		if flagBucketHash(key, contextID)%100 < uint32(f.RolloutPercentage) {
			res.Enabled = true
			res.Reason = "rollout"
		} else {
			res.Reason = "rollout_excluded"
		}
	}
	if res.Enabled && f.Payload != "" {
		res.Value = jsonRawValue(f.Payload)
	}
	_ = s.repo.TouchEvaluated(ctx, f.ID)
	s.logEvalAsync(ctx, key, attributes, res)
	return res
}

// EvaluateBatch 批量评估
func (s *FeatureFlagService) EvaluateBatch(ctx context.Context, keys []string, attributes map[string]any) []FlagEvaluateResult {
	out := make([]FlagEvaluateResult, 0, len(keys))
	for _, k := range keys {
		out = append(out, *s.Evaluate(ctx, k, attributes))
	}
	return out
}

// ListStale stale 检测：两周未更新且单边灰度
func (s *FeatureFlagService) ListStale(ctx context.Context) ([]model.FeatureFlag, error) {
	return s.repo.ListStale(ctx, s.now().Add(-FeatureFlagStaleDays*24*time.Hour))
}

// ListAudit 审计日志
func (s *FeatureFlagService) ListAudit(ctx context.Context, flagID uint) ([]model.FeatureFlagAuditLog, error) {
	return s.repo.ListAudit(ctx, flagID, 100)
}

// ListEvalLogs 评估日志
func (s *FeatureFlagService) ListEvalLogs(ctx context.Context, flagKey string) ([]model.FeatureFlagEvalLog, error) {
	return s.repo.ListEvalLogs(ctx, flagKey, 100)
}

func (s *FeatureFlagService) auditAsync(ctx context.Context, f *model.FeatureFlag, action string, actorID uint, detail string) {
	a := &model.FeatureFlagAuditLog{
		FlagID:  f.ID,
		FlagKey: f.Key,
		Action:  action,
		ActorID: actorID,
		Detail:  detail,
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		if err := s.repo.CreateAudit(detached, a); err != nil {
			slog.Warn("[FeatureFlag] 审计写入失败", "key", f.Key, "err", err)
		}
	}()
}

func (s *FeatureFlagService) logEvalAsync(ctx context.Context, key string, attributes map[string]any, res *FlagEvaluateResult) {
	e := &model.FeatureFlagEvalLog{
		FlagKey:   key,
		ContextID: contextIDFromAttributes(attributes),
		Enabled:   res.Enabled,
		Reason:    res.Reason,
	}
	if res.Value != nil {
		e.Value = fmt.Sprintf("%v", res.Value)
	}
	// 请求 ctx 随响应结束而取消，但租户归属必须带进协程：裸 Background 在
	// on 模式下会被隔离 callback fail-closed，曝光日志整批静默丢失；
	// off 模式显式绑定默认租户（语义与历史一致）。
	detached := tenant.BackgroundWithTenant(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		if err := s.repo.CreateEvalLog(detached, e); err != nil {
			slog.Warn("[FeatureFlag] 评估日志写入失败", "key", key, "err", err)
		}
	}()
}

func contextIDFromAttributes(attributes map[string]any) string {
	for _, k := range []string{"user_id", "userId", "one_id", "oneId"} {
		if v, ok := attributes[k]; ok {
			if s := fmt.Sprintf("%v", v); s != "" {
				return s
			}
		}
	}
	return "anonymous"
}

func flagBucketHash(key, contextID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key + ":" + contextID))
	return h.Sum32()
}

func jsonRawValue(payload string) any {
	var parsed any
	if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
		return parsed
	}
	return payload
}
