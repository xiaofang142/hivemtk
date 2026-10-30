package service

import (
	"context"
	"encoding/json"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/utils/logger"
	"math"
)

// ABExperimentService A/B 测试服务
type ABExperimentService struct {
	experimentRepo *repository.ABExperimentRepository
	variantRepo    *repository.ABVariantRepository
	conversionRepo *repository.ABConversionEventRepository
	resultRepo     *repository.ABExperimentResultRepository
}

// NewABExperimentService 创建 A/B 测试服务实例
func NewABExperimentService() *ABExperimentService {
	return &ABExperimentService{
		experimentRepo: repository.NewABExperimentRepository(),
		variantRepo:    repository.NewABVariantRepository(),
		conversionRepo: repository.NewABConversionEventRepository(),
		resultRepo:     repository.NewABExperimentResultRepository(),
	}
}

// CreateExperimentRequest 创建实验请求
type CreateExperimentRequest struct {
	Name         string          `json:"name" binding:"required"`
	Description  string          `json:"description"`
	SourceType   string          `json:"source_type" binding:"required"`
	SourceID     string          `json:"source_id" binding:"required"`
	TrafficSplit int             `json:"traffic_split"`
	StartDate    string          `json:"start_date"`
	EndDate      string          `json:"end_date"`
	Variants     []VariantConfig `json:"variants"`
	// CreatedBy 由控制器从 JWT 上下文注入，不信任请求体（避免伪造创建人）
	CreatedBy uint `json:"-"`
}

// VariantConfig 变体配置
type VariantConfig struct {
	Name      string         `json:"name" binding:"required"`
	IsControl bool           `json:"is_control"`
	Weight    int            `json:"weight"`
	Config    map[string]any `json:"config"`
}

// CreateExperiment 创建实验
func (s *ABExperimentService) CreateExperiment(ctx context.Context, req *CreateExperimentRequest) (*model.ABExperiment, error) {
	experiment := &model.ABExperiment{
		Name:         req.Name,
		Description:  req.Description,
		SourceType:   req.SourceType,
		SourceID:     req.SourceID,
		TrafficSplit: req.TrafficSplit,
		Status:       "draft",
		CreatedBy:    req.CreatedBy,
	}

	if err := s.experimentRepo.Create(ctx, experiment); err != nil {
		return nil, err
	}

	for i, v := range req.Variants {
		configJSON, _ := json.Marshal(v.Config)
		variant := &model.ABVariant{
			ExperimentID: experiment.ID,
			Name:         v.Name,
			IsControl:    v.IsControl,
			Weight:       v.Weight,
			Config:       string(configJSON),
		}
		if i == 0 {
			variant.IsControl = true
		}
		if err := s.variantRepo.Create(ctx, variant); err != nil {
			return nil, err
		}
	}

	return experiment, nil
}

// GetExperiment 获取实验详情
func (s *ABExperimentService) GetExperiment(ctx context.Context, id uint) (*model.ABExperiment, error) {
	return s.experimentRepo.GetByID(ctx, id)
}

// GetExperimentBySourceID 根据 sourceID 获取最新实验（不限状态）
func (s *ABExperimentService) GetExperimentBySourceID(ctx context.Context, sourceID string) (*model.ABExperiment, error) {
	return s.experimentRepo.GetLatestBySourceID(ctx, sourceID)
}

// GetExperimentList 获取实验列表
func (s *ABExperimentService) GetExperimentList(ctx context.Context, page, pageSize int) ([]*model.ABExperiment, int64, error) {
	return s.experimentRepo.GetAll(ctx, page, pageSize)
}

// UpdateExperiment 更新实验
func (s *ABExperimentService) UpdateExperiment(ctx context.Context, id uint, req *CreateExperimentRequest) (*model.ABExperiment, error) {
	experiment, err := s.experimentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	experiment.Name = req.Name
	experiment.Description = req.Description
	experiment.SourceType = req.SourceType
	experiment.SourceID = req.SourceID
	experiment.TrafficSplit = req.TrafficSplit

	if err := s.experimentRepo.Update(ctx, experiment); err != nil {
		return nil, err
	}

	if err := s.variantRepo.DeleteByExperiment(ctx, id); err != nil {
		return nil, err
	}
	for i, v := range req.Variants {
		configJSON, _ := json.Marshal(v.Config)
		variant := &model.ABVariant{
			ExperimentID: id,
			Name:         v.Name,
			IsControl:    v.IsControl,
			Weight:       v.Weight,
			Config:       string(configJSON),
		}
		if i == 0 {
			variant.IsControl = true
		}
		if err := s.variantRepo.Create(ctx, variant); err != nil {
			return nil, err
		}
	}

	return experiment, nil
}

// DeleteExperiment 删除实验
func (s *ABExperimentService) DeleteExperiment(ctx context.Context, id uint) error {
	if err := s.variantRepo.DeleteByExperiment(ctx, id); err != nil {
		return err
	}
	return s.experimentRepo.Delete(ctx, id)
}

// StartExperiment 启动实验
func (s *ABExperimentService) StartExperiment(ctx context.Context, id uint) error {
	return s.experimentRepo.UpdateStatus(ctx, id, "running")
}

// PauseExperiment 暂停实验
func (s *ABExperimentService) PauseExperiment(ctx context.Context, id uint) error {
	return s.experimentRepo.UpdateStatus(ctx, id, "paused")
}

// StopExperiment 停止实验
func (s *ABExperimentService) StopExperiment(ctx context.Context, id uint) error {
	if err := s.experimentRepo.UpdateStatus(ctx, id, "completed"); err != nil {
		return err
	}
	return s.CalculateResults(ctx, id)
}

// CalculateResults 计算实验结果
func (s *ABExperimentService) CalculateResults(ctx context.Context, experimentID uint) error {
	variants, err := s.variantRepo.GetByExperiment(ctx, experimentID)
	if err != nil {
		return err
	}

	for _, v := range variants {
		trafficCount := v.TrafficCount
		conversionCount := v.ConversionCount

		conversionRate := 0.0
		if trafficCount > 0 {
			conversionRate = float64(conversionCount) / float64(trafficCount) * 100
		}

		result := &model.ABExperimentResult{
			ExperimentID:    experimentID,
			VariantID:       v.ID,
			VariantName:     v.Name,
			IsControl:       v.IsControl,
			TrafficCount:    trafficCount,
			ConversionCount: conversionCount,
			ConversionRate:  conversionRate,
		}

		if err := s.resultRepo.Upsert(ctx, result); err != nil {
			return err
		}
	}

	return s.calculateWinner(ctx, experimentID)
}

func (s *ABExperimentService) calculateWinner(ctx context.Context, experimentID uint) error {
	results, err := s.resultRepo.GetByExperiment(ctx, experimentID)
	if err != nil {
		return err
	}

	if len(results) < 2 {
		return nil
	}

	var winner *model.ABExperimentResult
	maxRate := 0.0

	for _, r := range results {
		if r.ConversionRate > maxRate {
			maxRate = r.ConversionRate
			winner = r
		}
		r.ConfidenceLevel = s.calculateConfidence(r)
		if err := s.resultRepo.Upsert(ctx, r); err != nil {
			logger.Warnf("[ABExperiment] 结果统计落库失败 variantID=%d: %v", r.VariantID, err)
		}
	}

	if winner != nil {
		return s.resultRepo.UpdateWinner(ctx, experimentID, winner.VariantID)
	}

	return nil
}

func (s *ABExperimentService) calculateConfidence(result *model.ABExperimentResult) float64 {
	if result.TrafficCount == 0 {
		return 0
	}

	p := result.ConversionRate / 100
	n := float64(result.TrafficCount)

	se := math.Sqrt(p * (1 - p) / n)

	if se == 0 {
		return 0
	}

	z := (p - 0.5) / se

	confidence := 0.5 * (1 + math.Erf(z/math.Sqrt2))

	return confidence
}

// GetExperimentResults 获取实验结果
func (s *ABExperimentService) GetExperimentResults(ctx context.Context, experimentID uint) ([]*model.ABExperimentResult, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	return s.resultRepo.GetByExperiment(ctx, experimentID)
}

// GetConversionEvents 获取转化事件列表
func (s *ABExperimentService) GetConversionEvents(ctx context.Context, experimentID uint, page, pageSize int) ([]*model.ABConversionEvent, int64, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, 0, err
	}
	return s.conversionRepo.GetByExperiment(ctx, experimentID, page, pageSize)
}

func (s *ABExperimentService) variantCountsFromRepo(ctx context.Context, experimentID uint) ([]VariantCounts, error) {
	variants, err := s.variantRepo.GetByExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	out := make([]VariantCounts, 0, len(variants))
	for _, v := range variants {
		out = append(out, VariantCounts{
			VariantID:       v.ID,
			VariantName:     v.Name,
			IsControl:       v.IsControl,
			TrafficCount:    v.TrafficCount,
			ConversionCount: v.ConversionCount,
		})
	}
	return out, nil
}

// ensureExperimentExists 校验实验存在。
// 统计类接口（stats/diagnostics/cuped/sequential/bayesian）只读分桶与事件表，
// 实验不存在时这些表查不出任何行，会静默返回"空统计"而不是 404，
// 前端会把"实验已被删除"误显示成"样本量为 0"。故统一前置一次存在性校验。
func (s *ABExperimentService) ensureExperimentExists(ctx context.Context, experimentID uint) error {
	_, err := s.experimentRepo.GetByID(ctx, experimentID)
	return err
}

// GetAdvancedStats 频率派 z 检验结果（method=frequentist）
func (s *ABExperimentService) GetAdvancedStats(ctx context.Context, experimentID uint) (map[string]any, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	counts, err := s.variantCountsFromRepo(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"method":   "frequentist",
		"variants": FrequentistStats(counts),
	}, nil
}

// GetBayesianTest 贝叶斯胜率
func (s *ABExperimentService) GetBayesianTest(ctx context.Context, experimentID uint) (map[string]any, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	counts, err := s.variantCountsFromRepo(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"method":   "bayesian",
		"variants": BayesianTest(counts, 20000, nil),
	}, nil
}

// GetSequentialTest SPRT 序贯检验
func (s *ABExperimentService) GetSequentialTest(ctx context.Context, experimentID uint, alpha float64) (map[string]any, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	counts, err := s.variantCountsFromRepo(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	v := SequentialTest(counts, alpha)
	return map[string]any{"method": "sequential", "result": v}, nil
}

// GetDiagnostics SRM + 最小样本 + 多曝光诊断
func (s *ABExperimentService) GetDiagnostics(ctx context.Context, experimentID uint) (map[string]any, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	counts, err := s.variantCountsFromRepo(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	multi := s.countMultiExposeUsers(ctx, experimentID)
	d := Diagnostics(counts, multi)
	return map[string]any{"diagnostics": d}, nil
}

func (s *ABExperimentService) countMultiExposeUsers(ctx context.Context, experimentID uint) int {
	events, err := s.conversionRepo.ListAllByExperiment(ctx, experimentID, 5000)
	if err != nil {
		return 0
	}
	userVariants := map[string]map[uint]bool{}
	for _, e := range events {
		if e.UserID == "" {
			continue
		}
		if userVariants[e.UserID] == nil {
			userVariants[e.UserID] = map[uint]bool{}
		}
		userVariants[e.UserID][e.VariantID] = true
	}
	multi := 0
	for _, vs := range userVariants {
		if len(vs) > 1 {
			multi++
		}
	}
	return multi
}

// GetCUPED CUPED 方差缩减（协变量=实验前事件计数；无数据退化）
func (s *ABExperimentService) GetCUPED(ctx context.Context, experimentID uint) (map[string]any, error) {
	if err := s.ensureExperimentExists(ctx, experimentID); err != nil {
		return nil, err
	}
	counts, err := s.variantCountsFromRepo(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	users, err := s.cupedUserMetrics(ctx, experimentID)
	if err != nil {
		users = nil
	}
	res := CUPED(counts, users)
	return map[string]any{"cuped": res}, nil
}

func (s *ABExperimentService) cupedUserMetrics(ctx context.Context, experimentID uint) ([]cupedUserMetric, error) {
	exp, err := s.experimentRepo.GetByID(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	events, err := s.conversionRepo.ListAllByExperiment(ctx, experimentID, 5000)
	if err != nil {
		return nil, err
	}
	type agg struct {
		post int
		pre  int
	}
	byUser := map[string]*agg{}
	for _, e := range events {
		if e.UserID == "" {
			continue
		}
		a, ok := byUser[e.UserID]
		if !ok {
			a = &agg{}
			byUser[e.UserID] = a
		}
		if exp.StartDate != nil && e.CreatedAt.Before(*exp.StartDate) {
			a.pre++
		} else {
			a.post++
		}
	}
	out := make([]cupedUserMetric, 0, len(byUser))
	for _, a := range byUser {
		out = append(out, cupedUserMetric{y: float64(a.post), x: float64(a.pre)})
	}
	return out, nil
}

// GetResultsWithReach 结果 + 触达维度聚合（reach 回流：按事件类型分桶）
func (s *ABExperimentService) GetResultsWithReach(ctx context.Context, experimentID uint) (map[string]any, error) {
	results, err := s.resultRepo.GetByExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	events, err := s.conversionRepo.ListAllByExperiment(ctx, experimentID, 5000)
	if err != nil {
		return nil, err
	}
	byEventType := map[string]int64{}
	byVariantEvent := map[string]int64{}
	for _, e := range events {
		byEventType[e.EventType]++
		key := e.EventType
		byVariantEvent[key]++
	}
	return map[string]any{
		"results":         results,
		"event_breakdown": byEventType,
		"variant_event":   byVariantEvent,
	}, nil
}
