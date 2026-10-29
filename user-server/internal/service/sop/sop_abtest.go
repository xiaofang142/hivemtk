package sop

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// SOPABTestVariant A/B 测试 variant 定义
// 已迁移至 dto 包，此处保留类型别名以维持向后兼容
type SOPABTestVariant = dto.SOPABTestVariant

// SOPABTestConfig A/B 测试配置
// 已迁移至 dto 包
type SOPABTestConfig = dto.SOPABTestConfig

// ValidateSOPABTestConfig 校验 A/B 测试配置
// 规则：
//   - enabled=false 时直接通过
//   - variants 至少 2 个
//   - 每个 variant 必须有 name 和 weight>0
//   - 所有 variant 权重之和必须为 100
//   - variant name 不能重复
func ValidateSOPABTestConfig(c SOPABTestConfig) error {
	if !c.Enabled {
		return nil
	}
	if len(c.Variants) < 2 {
		return fmt.Errorf("A/B 测试至少需要 2 个 variant，当前 %d 个", len(c.Variants))
	}
	names := map[string]bool{}
	totalWeight := 0
	for _, v := range c.Variants {
		if v.Name == "" {
			return fmt.Errorf("variant 名称不能为空")
		}
		if names[v.Name] {
			return fmt.Errorf("variant 名称重复：%s", v.Name)
		}
		names[v.Name] = true
		if v.Weight <= 0 {
			return fmt.Errorf("variant [%s] 权重必须 > 0", v.Name)
		}
		totalWeight += v.Weight
	}
	if totalWeight != 100 {
		return fmt.Errorf("variant 权重之和必须为 100，当前 %d", totalWeight)
	}
	return nil
}

// SelectSOPABTestVariant 根据分流键选择 variant
// 算法：FNV-1a 哈希 + 取模，确保同一 customer_id 始终命中同一 variant
// 分流键为空时使用 "customer_id" 作为默认
// 未启用 A/B 测试或配置非法时返回空 variant（表示使用主图）
func SelectSOPABTestVariant(c SOPABTestConfig, customerID string) SOPABTestVariant {
	if !c.Enabled || len(c.Variants) == 0 {
		return SOPABTestVariant{}
	}

	salt := c.Salt
	if salt == "" {
		salt = "customer_id"
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(salt + ":" + customerID))
	hashVal := h.Sum64()

	variants := make([]SOPABTestVariant, len(c.Variants))
	copy(variants, c.Variants)
	sort.Slice(variants, func(i, j int) bool {
		return variants[i].Name < variants[j].Name
	})

	totalWeight := 0
	for _, v := range variants {
		totalWeight += v.Weight
	}
	if totalWeight <= 0 {
		return SOPABTestVariant{}
	}

	target := int(hashVal % uint64(totalWeight))
	cumulative := 0
	for _, v := range variants {
		cumulative += v.Weight
		if target < cumulative {
			return v
		}
	}
	return variants[len(variants)-1]
}

// ParseSOPABTestConfig 从 model.SOPAgent.ABTestConfig (JSONMap) 解析配置
// 配置为空或解析失败时返回 disabled 配置
func ParseSOPABTestConfig(raw model.JSONMap) SOPABTestConfig {
	cfg := SOPABTestConfig{Enabled: false}
	if raw == nil {
		return cfg
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// SOPABTestVariantStats A/B 测试 variant 统计
type SOPABTestVariantStats struct {
	Variant        string  `json:"variant"`
	ExecutionCount int64   `json:"execution_count"`
	SuccessCount   int64   `json:"success_count"`
	FailedCount    int64   `json:"failed_count"`
	RunningCount   int64   `json:"running_count"`
	SuccessRate    float64 `json:"success_rate"`
}

// GetABTestStats 查询指定 SOP 的 A/B 测试 variant 统计
// 未启用 A/B 测试时返回空切片
func (s *SOPService) GetABTestStats(ctx context.Context, sopID uint) ([]SOPABTestVariantStats, error) {
	agent, err := s.Get(ctx, sopID)
	if err != nil {
		return nil, err
	}

	cfg := ParseSOPABTestConfig(agent.ABTestConfig)
	if !cfg.Enabled {
		return []SOPABTestVariantStats{}, nil
	}

	stats := make([]SOPABTestVariantStats, 0, len(cfg.Variants))
	for _, v := range cfg.Variants {
		var execCount, successCount, failedCount, runningCount int64

		execCount, err := s.execRepo.CountBySOPIDAndVariant(ctx, sopID, v.Name)
		if err != nil {
			return nil, fmt.Errorf("查询 variant [%s] 执行数失败：%w", v.Name, err)
		}
		successCount, err = s.execRepo.CountBySOPIDAndVariantAndStatus(ctx, sopID, v.Name, SOPStatusSuccess)
		if err != nil {
			return nil, fmt.Errorf("查询 variant [%s] 成功数失败：%w", v.Name, err)
		}
		failedCount, err = s.execRepo.CountBySOPIDAndVariantAndStatus(ctx, sopID, v.Name, SOPStatusFailed)
		if err != nil {
			return nil, fmt.Errorf("查询 variant [%s] 失败数失败：%w", v.Name, err)
		}
		runningCount, err = s.execRepo.CountBySOPIDAndVariantAndStatus(ctx, sopID, v.Name, SOPStatusRunning)
		if err != nil {
			return nil, fmt.Errorf("查询 variant [%s] 运行中数失败：%w", v.Name, err)
		}

		successRate := 0.0
		if execCount > 0 {
			successRate = float64(successCount) / float64(execCount) * 100
		}

		stats = append(stats, SOPABTestVariantStats{
			Variant:        v.Name,
			ExecutionCount: execCount,
			SuccessCount:   successCount,
			FailedCount:    failedCount,
			RunningCount:   runningCount,
			SuccessRate:    successRate,
		})
	}

	return stats, nil
}

// UpdateABTestConfig 更新 SOP 的 A/B 测试配置
func (s *SOPService) UpdateABTestConfig(ctx context.Context, sopID uint, cfg SOPABTestConfig) (*model.SOPAgent, error) {
	if err := ValidateSOPABTestConfig(cfg); err != nil {
		return nil, fmt.Errorf("A/B 测试配置非法：%w", err)
	}

	agent, err := s.Get(ctx, sopID)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("A/B 测试配置序列化失败：%w", err)
	}
	abMap := model.JSONMap{}
	if err := json.Unmarshal(data, &abMap); err != nil {
		return nil, fmt.Errorf("A/B 测试配置转换失败：%w", err)
	}

	agent.ABTestConfig = abMap
	agent.Version++

	if err := s.agentRepo.Save(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func (s *SOPService) resolveABTestVariant(ctx context.Context, agent *model.SOPAgent, customerID string) (variantName string, sopGraphID uint, err error) {
	cfg := ParseSOPABTestConfig(agent.ABTestConfig)
	if !cfg.Enabled {
		return "", 0, nil
	}
	if err := ValidateSOPABTestConfig(cfg); err != nil {
		return "", 0, err
	}
	variant := SelectSOPABTestVariant(cfg, customerID)
	return variant.Name, variant.SOPGraphID, nil
}

func (s *SOPService) loadSOPGraph(ctx context.Context, agent *model.SOPAgent, graphID uint) (SOPGraph, error) {
	if graphID == 0 {
		var graph SOPGraph
		if err := json.Unmarshal(mustJSON(agent.SOPGraph), &graph); err != nil {
			return SOPGraph{}, ErrSOPInvalidGraph
		}
		return graph, nil
	}

	target, err := s.agentRepo.GetByID(ctx, graphID)
	if err != nil {
		return SOPGraph{}, fmt.Errorf("variant SOP 图加载失败（sop_id=%d）：%w", graphID, err)
	}
	var graph SOPGraph
	if err := json.Unmarshal(mustJSON(target.SOPGraph), &graph); err != nil {
		return SOPGraph{}, ErrSOPInvalidGraph
	}
	return graph, nil
}

var _ = gorm.Expr
