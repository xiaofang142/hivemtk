package service

import (
	"encoding/json"
	"errors"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/utils/logger"
	"math"
	"sync"
)

// ABExperimentService A/B 测试服务
type ABExperimentService struct {
	experimentRepo *repository.ABExperimentRepository
	variantRepo    *repository.ABVariantRepository
	conversionRepo *repository.ABConversionEventRepository
	resultRepo     *repository.ABExperimentResultRepository
	variantCache   map[uint]*model.ABVariant
	cacheMutex     sync.RWMutex
}

// NewABExperimentService 创建 A/B 测试服务实例
func NewABExperimentService() *ABExperimentService {
	return &ABExperimentService{
		experimentRepo: repository.NewABExperimentRepository(),
		variantRepo:    repository.NewABVariantRepository(),
		conversionRepo: repository.NewABConversionEventRepository(),
		resultRepo:     repository.NewABExperimentResultRepository(),
		variantCache:   make(map[uint]*model.ABVariant),
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
}

// VariantConfig 变体配置
type VariantConfig struct {
	Name      string         `json:"name" binding:"required"`
	IsControl bool           `json:"is_control"`
	Weight    int            `json:"weight"`
	Config    map[string]any `json:"config"`
}

// CreateExperiment 创建实验
func (s *ABExperimentService) CreateExperiment(req *CreateExperimentRequest) (*model.ABExperiment, error) {
	experiment := &model.ABExperiment{
		Name:         req.Name,
		Description:  req.Description,
		SourceType:   req.SourceType,
		SourceID:     req.SourceID,
		TrafficSplit: req.TrafficSplit,
		Status:       "draft",
	}

	if err := s.experimentRepo.Create(experiment); err != nil {
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
		if err := s.variantRepo.Create(variant); err != nil {
			return nil, err
		}
	}

	return experiment, nil
}

// GetExperiment 获取实验详情
func (s *ABExperimentService) GetExperiment(id uint) (*model.ABExperiment, error) {
	return s.experimentRepo.GetByID(id)
}

// GetExperimentBySourceID 根据 sourceID 获取最新实验（不限状态）
func (s *ABExperimentService) GetExperimentBySourceID(sourceID string) (*model.ABExperiment, error) {
	return s.experimentRepo.GetLatestBySourceID(sourceID)
}

// GetExperimentList 获取实验列表
func (s *ABExperimentService) GetExperimentList(page, pageSize int) ([]*model.ABExperiment, int64, error) {
	return s.experimentRepo.GetAll(page, pageSize)
}

// UpdateExperiment 更新实验
func (s *ABExperimentService) UpdateExperiment(id uint, req *CreateExperimentRequest) (*model.ABExperiment, error) {
	experiment, err := s.experimentRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	experiment.Name = req.Name
	experiment.Description = req.Description
	experiment.SourceType = req.SourceType
	experiment.SourceID = req.SourceID
	experiment.TrafficSplit = req.TrafficSplit

	if err := s.experimentRepo.Update(experiment); err != nil {
		return nil, err
	}

	if err := s.variantRepo.DeleteByExperiment(id); err != nil {
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
		if err := s.variantRepo.Create(variant); err != nil {
			return nil, err
		}
	}

	return experiment, nil
}

// DeleteExperiment 删除实验
func (s *ABExperimentService) DeleteExperiment(id uint) error {
	if err := s.variantRepo.DeleteByExperiment(id); err != nil {
		return err
	}
	return s.experimentRepo.Delete(id)
}

// StartExperiment 启动实验
func (s *ABExperimentService) StartExperiment(id uint) error {
	return s.experimentRepo.UpdateStatus(id, "running")
}

// PauseExperiment 暂停实验
func (s *ABExperimentService) PauseExperiment(id uint) error {
	return s.experimentRepo.UpdateStatus(id, "paused")
}

// StopExperiment 停止实验
func (s *ABExperimentService) StopExperiment(id uint) error {
	if err := s.experimentRepo.UpdateStatus(id, "completed"); err != nil {
		return err
	}
	return s.CalculateResults(id)
}

// GetVariant 获取变体（用于流量分配）
func (s *ABExperimentService) GetVariant(sourceID string) (*model.ABVariant, error) {
	variant, err := s.getVariantFromDB(sourceID)
	if err != nil {

		s.cacheMutex.RLock()
		cached, ok := s.variantCache[s.hashSourceID(sourceID)]
		s.cacheMutex.RUnlock()
		if ok {
			return cached, nil
		}
		return nil, err
	}
	return variant, nil
}

func (s *ABExperimentService) hashSourceID(sourceID string) uint {
	hash := uint(0)
	for i := 0; i < len(sourceID); i++ {
		hash = hash*31 + uint(sourceID[i])
	}
	return hash % 1000000
}

func (s *ABExperimentService) getVariantFromDB(sourceID string) (*model.ABVariant, error) {
	experiment, err := s.experimentRepo.GetRunningBySourceID(sourceID)
	if err != nil {
		return nil, errors.New("no running experiment for this source")
	}

	variants, err := s.variantRepo.GetByExperiment(experiment.ID)
	if err != nil || len(variants) == 0 {
		return nil, errors.New("no variants found for experiment")
	}

	totalWeight := 0
	for _, v := range variants {
		if v.Weight <= 0 {
			totalWeight++
		} else {
			totalWeight += v.Weight
		}
	}

	if totalWeight <= 0 {
		return variants[0], nil
	}

	seed := s.hashSourceID(sourceID)
	pick := int(seed) % totalWeight

	cumulative := 0
	for _, v := range variants {
		w := v.Weight
		if w <= 0 {
			w = 1
		}
		cumulative += w
		if pick < cumulative {
			if err := s.variantRepo.IncrementTraffic(v.ID); err != nil {
				logger.Warnf("[ABExperiment] 流量计数失败 variantID=%d: %v", v.ID, err)
			}

			s.cacheMutex.Lock()
			s.variantCache[s.hashSourceID(sourceID)+uint(experiment.ID)*1000000] = v
			s.cacheMutex.Unlock()

			return v, nil
		}
	}

	return variants[0], nil
}

// RecordConversion 记录转化事件
// eventValue 单位：分（int64），如购买金额 99.99 元应传 9999
func (s *ABExperimentService) RecordConversion(experimentID, variantID uint, eventName, eventType string, eventValue int64, userID string, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	event := &model.ABConversionEvent{
		ExperimentID: experimentID,
		EventName:    eventName,
		EventType:    eventType,
		EventValue:   eventValue,
		UserID:       userID,
		VariantID:    variantID,
		Metadata:     string(metadataJSON),
	}
	if err := s.conversionRepo.Create(event); err != nil {
		return err
	}

	return s.variantRepo.IncrementConversion(variantID)
}

// CalculateResults 计算实验结果
func (s *ABExperimentService) CalculateResults(experimentID uint) error {
	variants, err := s.variantRepo.GetByExperiment(experimentID)
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

		if err := s.resultRepo.Upsert(result); err != nil {
			return err
		}
	}

	return s.calculateWinner(experimentID)
}

func (s *ABExperimentService) calculateWinner(experimentID uint) error {
	results, err := s.resultRepo.GetByExperiment(experimentID)
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
		if err := s.resultRepo.Upsert(r); err != nil {
			logger.Warnf("[ABExperiment] 结果统计落库失败 variantID=%d: %v", r.VariantID, err)
		}
	}

	if winner != nil {
		return s.resultRepo.UpdateWinner(experimentID, winner.VariantID)
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
func (s *ABExperimentService) GetExperimentResults(experimentID uint) ([]*model.ABExperimentResult, error) {
	return s.resultRepo.GetByExperiment(experimentID)
}

// GetConversionEvents 获取转化事件列表
func (s *ABExperimentService) GetConversionEvents(experimentID uint, page, pageSize int) ([]*model.ABConversionEvent, int64, error) {
	return s.conversionRepo.GetByExperiment(experimentID, page, pageSize)
}

func (s *ABExperimentService) variantCountsFromRepo(experimentID uint) ([]VariantCounts, error) {
	variants, err := s.variantRepo.GetByExperiment(experimentID)
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

// GetAdvancedStats 频率派 z 检验结果（method=frequentist）
func (s *ABExperimentService) GetAdvancedStats(experimentID uint) (map[string]any, error) {
	counts, err := s.variantCountsFromRepo(experimentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"method":   "frequentist",
		"variants": FrequentistStats(counts),
	}, nil
}

// GetBayesianTest 贝叶斯胜率
func (s *ABExperimentService) GetBayesianTest(experimentID uint) (map[string]any, error) {
	counts, err := s.variantCountsFromRepo(experimentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"method":   "bayesian",
		"variants": BayesianTest(counts, 20000, nil),
	}, nil
}

// GetSequentialTest SPRT 序贯检验
func (s *ABExperimentService) GetSequentialTest(experimentID uint, alpha float64) (map[string]any, error) {
	counts, err := s.variantCountsFromRepo(experimentID)
	if err != nil {
		return nil, err
	}
	v := SequentialTest(counts, alpha)
	return map[string]any{"method": "sequential", "result": v}, nil
}

// GetDiagnostics SRM + 最小样本 + 多曝光诊断
func (s *ABExperimentService) GetDiagnostics(experimentID uint) (map[string]any, error) {
	counts, err := s.variantCountsFromRepo(experimentID)
	if err != nil {
		return nil, err
	}
	multi := s.countMultiExposeUsers(experimentID)
	d := Diagnostics(counts, multi)
	return map[string]any{"diagnostics": d}, nil
}

func (s *ABExperimentService) countMultiExposeUsers(experimentID uint) int {
	events, _, err := s.conversionRepo.GetByExperiment(experimentID, 1, 5000)
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
func (s *ABExperimentService) GetCUPED(experimentID uint) (map[string]any, error) {
	counts, err := s.variantCountsFromRepo(experimentID)
	if err != nil {
		return nil, err
	}
	users, err := s.cupedUserMetrics(experimentID)
	if err != nil {
		users = nil
	}
	res := CUPED(counts, users)
	return map[string]any{"cuped": res}, nil
}

func (s *ABExperimentService) cupedUserMetrics(experimentID uint) ([]cupedUserMetric, error) {
	exp, err := s.experimentRepo.GetByID(experimentID)
	if err != nil {
		return nil, err
	}
	events, _, err := s.conversionRepo.GetByExperiment(experimentID, 1, 5000)
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
func (s *ABExperimentService) GetResultsWithReach(experimentID uint) (map[string]any, error) {
	results, err := s.resultRepo.GetByExperiment(experimentID)
	if err != nil {
		return nil, err
	}
	events, _, err := s.conversionRepo.GetByExperiment(experimentID, 1, 5000)
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
