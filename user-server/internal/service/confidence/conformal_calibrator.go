package confidence

import (
	"context"
	"math"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

type ConformalCalibrator struct {
	mu             sync.RWMutex
	predictor      *ConformalPredictor
	scores         []float64
	maxRetained    int
	recalibrateSec int
}

// NewConformalCalibrator 构造在线校准器
//
// maxRetained: 滑动窗口大小（默认 1000；超过则滑动淘汰）
// recalibrateSec: 重算分位数间隔（默认 60s；避免每条更新都重排）
func NewConformalCalibrator(maxRetained, recalibrateSec int) *ConformalCalibrator {
	if maxRetained <= 0 {
		maxRetained = 1000
	}
	if recalibrateSec <= 0 {
		recalibrateSec = 60
	}

	cp := NewConformalPredictor(nil, 0.1)
	cc := &ConformalCalibrator{
		predictor:      cp,
		scores:         make([]float64, 0, maxRetained),
		maxRetained:    maxRetained,
		recalibrateSec: recalibrateSec,
	}
	return cc
}

// Predictor 返回当前预测器（用于 SetConformal 注入）
func (c *ConformalCalibrator) Predictor() *ConformalPredictor {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.predictor
}

// AddScore 添加一条非一致性分数（异步线程安全）
//
// 业界依据：业务运行时收集 (predicted_conf, actual_correct) 计算
//
//	s = 1 - predicted_conf（如果 correct=true）或 s = predicted_conf（如果 correct=false）
//	1 - δ 保证下，s > threshold → abstention
func (c *ConformalCalibrator) AddScore(score float64) {
	if c == nil || math.IsNaN(score) || math.IsInf(score, 0) {
		return
	}
	c.mu.Lock()
	c.scores = append(c.scores, score)
	if len(c.scores) > c.maxRetained {

		c.scores = c.scores[len(c.scores)-c.maxRetained:]
	}

	shouldRecalib := len(c.scores)%100 == 0
	c.mu.Unlock()

	if shouldRecalib {
		c.Recalibrate()
	}
}

// Recalibrate 立即基于当前 scores 重算分位数
func (c *ConformalCalibrator) Recalibrate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	scores := make([]float64, len(c.scores))
	copy(scores, c.scores)
	delta := c.predictor.delta
	c.mu.Unlock()

	newCP := NewConformalPredictor(scores, delta)
	c.mu.Lock()
	c.predictor = newCP
	c.mu.Unlock()

	logger.GetLogger().Info().
		Int("sample_size", len(scores)).
		Float64("quantile", newCP.Quantile()).
		Msg("[Conformal] recalibrated")
}

// Threshold 便捷方法：返回当前非一致性阈值
func (c *ConformalCalibrator) Threshold() float64 {
	if c == nil {
		return math.Inf(1)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.predictor.Quantile()
}

// Snapshot 导出当前状态（用于诊断 / 持久化）
type ConformalCalibratorSnapshot struct {
	SampleSize   int     `json:"sample_size"`
	Quantile     float64 `json:"quantile"`
	Delta        float64 `json:"delta"`
	CoverageRate float64 `json:"coverage_rate"`
}

// Snapshot 返回当前快照
func (c *ConformalCalibrator) Snapshot() ConformalCalibratorSnapshot {
	if c == nil {
		return ConformalCalibratorSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConformalCalibratorSnapshot{
		SampleSize:   len(c.scores),
		Quantile:     c.predictor.Quantile(),
		Delta:        c.predictor.delta,
		CoverageRate: c.predictor.CoverageGuarantee(),
	}
}

// Reset 清空校准集（用于新实验开始）
func (c *ConformalCalibrator) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.scores = c.scores[:0]
	c.predictor = NewConformalPredictor(nil, c.predictor.delta)
	c.mu.Unlock()
}

// BackgroundRunner 后台定期重算包装
//
// 用法：go calibrator.BackgroundRunner(ctx)
func (c *ConformalCalibrator) BackgroundRunner(ctx context.Context) {
	if c == nil {
		return
	}

	ticker := time.NewTicker(time.Duration(c.recalibrateSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Recalibrate()
		}
	}
}
