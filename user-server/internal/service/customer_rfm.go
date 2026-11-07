package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// CustomerRFMService 客户 RFM 服务
//
// H3（技术债清理）：原 RFMCalculatorService（rfm_calculator.go）已删除，
// 本服务为全系统唯一 RFM 口径入口：打分（RFMConfig/Rule）、分层查询、
// 统计分布、规则 CRUD 均收敛于此。
type CustomerRFMService struct {
	rfmRepo      repository.CustomerRFMRepository
	customerRepo repository.CustomerRepository
	orderRepo    repository.OrderRepository
	recoveryRepo repository.RecoveryQueueRepository
	rfmRuleRepo  *repository.RFMRuleRepository
	nowFunc      func() time.Time
}

// NewCustomerRFMService 创建 RFM 服务
func NewCustomerRFMService() *CustomerRFMService {
	return &CustomerRFMService{
		rfmRepo:      repository.NewCustomerRFMRepository(),
		customerRepo: repository.NewCustomerRepository(),
		orderRepo:    repository.NewOrderRepository(),
		recoveryRepo: repository.NewRecoveryQueueRepository(),
		rfmRuleRepo:  repository.NewRFMRuleRepository(),
		nowFunc:      time.Now,
	}
}

// NewCustomerRFMServiceWithRepos 测试用
func NewCustomerRFMServiceWithRepos(
	rfm repository.CustomerRFMRepository,
	cust repository.CustomerRepository,
	ord repository.OrderRepository,
	rec repository.RecoveryQueueRepository,
) *CustomerRFMService {
	return &CustomerRFMService{
		rfmRepo:      rfm,
		customerRepo: cust,
		orderRepo:    ord,
		recoveryRepo: rec,
		nowFunc:      time.Now,
	}
}

// RFMConfig RFM 评分配置
//
// P-3（RFM 双体系统一）：原 rfm_calculator.go 的可配 rule 表（rfm_rules）能力
// 并入本 config——Rule 非 nil 时按规则显式阈值打分，覆盖默认 Buckets，
// 全系统统一走 CustomerRFMService 单一口径。
type RFMConfig struct {
	RecencyBuckets        []int
	FrequencyBuckets      []int
	MonetaryBuckets       []int64
	ChurnRecencyThreshold int
	AutoEnqueueRecovery   bool

	// Rule 可配规则（来源 rfm_rules 表）；非 nil 时优先于 *Buckets 打分
	Rule *model.RFMRule
}

// RFMConfigFromRule 将 rfm_rules 表的可配规则转为统一口径 RFMConfig（P-3）。
// rule 为 nil 时返回默认分位配置。
func RFMConfigFromRule(rule *model.RFMRule) RFMConfig {
	cfg := DefaultRFMConfig()
	if rule == nil {
		return cfg
	}
	cfg.Rule = rule
	return cfg
}

func rfmScoreRecencyCfg(days int, cfg RFMConfig) int {
	if rule := cfg.Rule; rule != nil {
		switch {
		case days <= rule.RDays1:
			return 5
		case days <= rule.RDays2:
			return 4
		case days <= rule.RDays3:
			return 3
		case days <= rule.RDays4:
			return 2
		default:
			return 1
		}
	}
	return rfmScoreRecency(days, cfg.RecencyBuckets)
}

func rfmScoreFrequencyCfg(freq int, cfg RFMConfig) int {
	if rule := cfg.Rule; rule != nil {
		switch {
		case freq >= rule.FCount5:
			return 5
		case freq >= rule.FCount4:
			return 4
		case freq >= rule.FCount3:
			return 3
		case freq >= rule.FCount2:
			return 2
		default:
			return 1
		}
	}
	return rfmScoreFrequency(freq, cfg.FrequencyBuckets)
}

func rfmScoreMonetaryCfg(amount int64, cfg RFMConfig) int {
	if rule := cfg.Rule; rule != nil {
		switch {
		case amount >= rule.MAmount5:
			return 5
		case amount >= rule.MAmount4:
			return 4
		case amount >= rule.MAmount3:
			return 3
		case amount >= rule.MAmount2:
			return 2
		case amount >= rule.MAmount1:
			return 1
		default:
			return 1
		}
	}
	return rfmScoreMonetary(amount, cfg.MonetaryBuckets)
}

// DefaultRFMConfig 默认 RFM 配置
func DefaultRFMConfig() RFMConfig {
	return RFMConfig{
		RecencyBuckets:        []int{7, 30, 90, 180},
		FrequencyBuckets:      []int{10, 5, 3, 1},
		MonetaryBuckets:       []int64{500000, 100000, 30000, 5000},
		ChurnRecencyThreshold: 180,
		AutoEnqueueRecovery:   true,
	}
}

// ComputeForCustomer 计算单个客户的 RFM
func (s *CustomerRFMService) ComputeForCustomer(ctx context.Context, customerID string, cfg RFMConfig) (*model.CustomerRFM, error) {
	if customerID == "" {
		return nil, fmt.Errorf("%w: customer_id 不能为空", utils.ErrInvalidInput)
	}
	cust, err := s.customerRepo.GetByID(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if cust == nil {
		return nil, errors.New("客户不存在")
	}

	r, f, m, lastActive, err := s.computeRawMetricsForCustomer(ctx, cust)
	if err != nil {
		return nil, err
	}

	rs := rfmScoreRecencyCfg(r, cfg)
	fs := rfmScoreFrequencyCfg(f, cfg)
	ms := rfmScoreMonetaryCfg(m, cfg)

	composite := int(float64(rs)*30 + float64(fs)*30 + float64(ms)*40)
	if composite < 0 {
		composite = 0
	}
	if composite > 100 {
		composite = 100
	}

	segment := determineSegment(rs, fs, ms, r, cfg.ChurnRecencyThreshold)
	churnRisk, churnScore := calcChurnRisk(r, f, m, cfg)

	avgOrder := int64(0)
	if f > 0 {
		avgOrder = m / int64(f)
	}

	rfm := &model.CustomerRFM{
		CustomerID:     customerID,
		UnifiedID:      cust.UnifiedID,
		RecencyDays:    r,
		Frequency:      f,
		MonetaryTotal:  m,
		AvgOrderValue:  avgOrder,
		RScore:         rs,
		FScore:         fs,
		MScore:         ms,
		CompositeScore: composite,
		Segment:        segment,
		ChurnRiskLevel: churnRisk,
		ChurnScore:     churnScore,
		LastActiveAt:   lastActive,
		ComputedAt:     s.nowFunc(),
	}
	if err := s.rfmRepo.Upsert(ctx, rfm); err != nil {
		return nil, err
	}

	if cfg.AutoEnqueueRecovery && segment == model.RFMSegmentChurn {
		s.enqueueRecovery(ctx, rfm)
	}
	return rfm, nil
}

func (s *CustomerRFMService) ComputeAll(ctx context.Context, limit int) (int, error) {
	const pageSize = 500
	cfg := DefaultRFMConfig()
	success := 0
	for page := 1; ; page++ {
		customers, total, err := s.customerRepo.List(ctx, page, pageSize, "")
		if err != nil {
			return success, err
		}
		if len(customers) == 0 {
			break
		}
		for _, c := range customers {
			if limit > 0 && success >= limit {
				return success, nil
			}
			if _, err := s.computeForCustomerLoaded(ctx, c, cfg); err != nil {
				return success, err
			}
			success++
		}
		if int64(page*pageSize) >= total {
			break
		}
	}
	return success, nil
}

func (s *CustomerRFMService) computeForCustomerLoaded(ctx context.Context, cust *model.Customer, cfg RFMConfig) (*model.CustomerRFM, error) {
	if cust == nil {
		return nil, errors.New("客户不能为空")
	}
	if cust.ID == "" {
		return nil, errors.New("客户 ID 不能为空")
	}

	r, f, m, lastActive, err := s.computeRawMetricsForCustomer(ctx, cust)
	if err != nil {
		return nil, err
	}

	rs := rfmScoreRecencyCfg(r, cfg)
	fs := rfmScoreFrequencyCfg(f, cfg)
	ms := rfmScoreMonetaryCfg(m, cfg)

	composite := int(float64(rs)*30 + float64(fs)*30 + float64(ms)*40)
	if composite < 0 {
		composite = 0
	}
	if composite > 100 {
		composite = 100
	}

	segment := determineSegment(rs, fs, ms, r, cfg.ChurnRecencyThreshold)
	churnRisk, churnScore := calcChurnRisk(r, f, m, cfg)

	avgOrder := int64(0)
	if f > 0 {
		avgOrder = m / int64(f)
	}

	rfm := &model.CustomerRFM{
		CustomerID:     cust.ID,
		UnifiedID:      cust.UnifiedID,
		RecencyDays:    r,
		Frequency:      f,
		MonetaryTotal:  m,
		AvgOrderValue:  avgOrder,
		RScore:         rs,
		FScore:         fs,
		MScore:         ms,
		CompositeScore: composite,
		Segment:        segment,
		ChurnRiskLevel: churnRisk,
		ChurnScore:     churnScore,
		LastActiveAt:   lastActive,
		ComputedAt:     s.nowFunc(),
	}
	if err := s.rfmRepo.Upsert(ctx, rfm); err != nil {
		return nil, err
	}

	if cfg.AutoEnqueueRecovery && segment == model.RFMSegmentChurn {
		s.enqueueRecovery(ctx, rfm)
	}

	if orch := GetGlobalOrchestrator(); orch != nil {
		orch.OnRFMComputed(ctx, cust.ID, string(segment))
	}
	return rfm, nil
}

// GetByCustomerID 查询客户 RFM
func (s *CustomerRFMService) GetByCustomerID(ctx context.Context, customerID string) (*model.CustomerRFM, error) {
	return s.rfmRepo.GetByCustomerID(ctx, customerID)
}

// ListBySegment 分层分页查询
func (s *CustomerRFMService) ListBySegment(ctx context.Context, segment string, page, pageSize int) ([]*model.CustomerRFM, int64, error) {
	return s.rfmRepo.ListBySegment(ctx, segment, page, pageSize)
}

// Distribution 分层统计
func (s *CustomerRFMService) Distribution(ctx context.Context) (map[string]int64, error) {
	return s.rfmRepo.CountBySegment(ctx)
}

func (s *CustomerRFMService) computeRawMetricsForCustomer(ctx context.Context, cust *model.Customer) (recencyDays, frequency int, monetary int64, lastActive *time.Time, err error) {
	if cust == nil || (cust.ID == "" && cust.Phone == "") {
		recencyDays = 9999
		return
	}

	accountIDs := make([]string, 0, 2)
	if cust.ID != "" {
		accountIDs = append(accountIDs, cust.ID)
	}
	if cust.Phone != "" {
		accountIDs = append(accountIDs, cust.Phone)
	}
	orders, oerr := s.orderRepo.ListByAccountIDs(ctx, accountIDs)
	if oerr != nil {
		err = oerr
		return
	}
	if cust.TelegramChatID > 0 {
		tgOrders, terr := s.orderRepo.GetByTgID(ctx, cust.TelegramChatID)
		if terr != nil {
			err = terr
			return
		}
		seen := make(map[string]bool, len(orders))
		for _, o := range orders {
			seen[o.ID] = true
		}
		for _, o := range tgOrders {
			if !seen[o.ID] {
				orders = append(orders, o)
				seen[o.ID] = true
			}
		}
	}

	frequency = len(orders)
	for _, o := range orders {
		if o.Price == "" {
			continue
		}
		price, perr := strconv.ParseInt(o.Price, 10, 64)
		if perr != nil {
			continue
		}
		monetary += price
		if o.CreateTime > 0 {
			t := time.Unix(o.CreateTime, 0)
			if lastActive == nil || t.After(*lastActive) {
				lastActive = &t
			}
		}
	}
	if lastActive != nil {
		recencyDays = int(time.Since(*lastActive).Hours() / 24)
		if recencyDays < 0 {
			recencyDays = 0
		}
	}
	return
}

func rfmScoreRecency(days int, buckets []int) int {
	if len(buckets) == 0 {
		return 1
	}
	score := 1
	for _, b := range buckets {
		if days <= b {
			score++
		}
	}
	if score > 5 {
		score = 5
	}
	return score
}

func rfmScoreFrequency(freq int, buckets []int) int {
	if len(buckets) == 0 {
		return 1
	}
	score := 1
	for _, b := range buckets {
		if freq >= b {
			score++
		}
	}
	if score > 5 {
		score = 5
	}
	return score
}

func rfmScoreMonetary(m int64, buckets []int64) int {
	if len(buckets) == 0 {
		return 1
	}
	score := 1
	for _, b := range buckets {
		if m >= b {
			score++
		}
	}
	if score > 5 {
		score = 5
	}
	return score
}

func determineSegment(r, f, m, recencyDays, churnThreshold int) string {
	if recencyDays >= churnThreshold || (r <= 1 && f <= 1) {
		return model.RFMSegmentChurn
	}
	if r == 5 && f >= 4 {
		return model.RFMSegmentChampion
	}
	if r >= 4 && f >= 3 {
		return model.RFMSegmentLoyal
	}
	if r == 2 || (f >= 2 && m >= 3) {
		return model.RFMSegmentAtRisk
	}
	return model.RFMSegmentPotential
}

func calcChurnRisk(recencyDays, freq int, monetary int64, cfg RFMConfig) (string, int) {
	score := 0
	if recencyDays >= cfg.ChurnRecencyThreshold {
		score += 70
	} else if recencyDays >= cfg.ChurnRecencyThreshold/2 {
		score += 40
	} else if recencyDays >= 30 {
		score += 15
	}
	if freq >= 5 {
		score -= 25
	} else if freq >= 2 {
		score -= 10
	}
	if monetary >= 100000 {
		score -= 15
	} else if monetary >= 10000 {
		score -= 5
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	switch {
	case score >= 70:
		return "high", score
	case score >= 40:
		return "medium", score
	default:
		return "low", score
	}
}

func (s *CustomerRFMService) enqueueRecovery(ctx context.Context, rfm *model.CustomerRFM) {
	if s.recoveryRepo == nil || rfm == nil {
		return
	}
	existing, err := s.recoveryRepo.GetActiveByCustomerID(ctx, rfm.CustomerID)
	if err != nil {
		logger.Warnf("[rfm] enqueueRecovery: GetActiveByCustomerID failed (customer=%s): %v", rfm.CustomerID, err)
		return
	}
	if existing != nil {
		return
	}
	priority := 5
	if rfm.ChurnScore >= 80 {
		priority = 1
	} else if rfm.ChurnScore >= 60 {
		priority = 2
	}
	item := &model.RecoveryQueue{
		CustomerID: rfm.CustomerID,
		UnifiedID:  rfm.UnifiedID,
		Reason:     "churn",
		Strategy:   "sms_coupon",
		Priority:   priority,
		Stage:      model.RecoveryStageQueued,
	}
	if err := s.recoveryRepo.Create(ctx, item); err != nil {

		logger.Warnf("[rfm] enqueueRecovery: create failed (customer=%s): %v", rfm.CustomerID, err)
	}
}

func (s *CustomerRFMService) ruleRepo() *repository.RFMRuleRepository {
	if s.rfmRuleRepo == nil {
		s.rfmRuleRepo = repository.NewRFMRuleRepository()
	}
	return s.rfmRuleRepo
}

// GetRFMRule 获取当前激活的 RFM 规则
func (s *CustomerRFMService) GetRFMRule(ctx context.Context) (*model.RFMRule, error) {
	return s.ruleRepo().GetActiveRule(ctx)
}

// ListRFMRules 列出所有 RFM 规则（分页，用于分群管理页）
func (s *CustomerRFMService) ListRFMRules(ctx context.Context, page, pageSize int) ([]*model.RFMRule, int64, error) {
	all, err := s.ruleRepo().GetAll(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := int64(len(all))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []*model.RFMRule{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

// SaveRFMRuleRequest 保存 RFM 规则请求
// 金额字段单位：分（前端展示时 / 100 转元）
//
// Name 不再 required（D-40）：历史规则（如 id=2，2026-08-31 建）name 是空串，
// 前端编辑弹窗按 GET 回填后原样提交，required 会把"改一个阈值"整单打回 400，
// 逼运营先编一个名字。名字只是备注，不参与打分。
type SaveRFMRuleRequest struct {
	Name     string `json:"name"`
	RDays1   int    `json:"r_days_1"`
	RDays2   int    `json:"r_days_2"`
	RDays3   int    `json:"r_days_3"`
	RDays4   int    `json:"r_days_4"`
	RDays5   int    `json:"r_days_5"`
	FCount1  int    `json:"f_count_1"`
	FCount2  int    `json:"f_count_2"`
	FCount3  int    `json:"f_count_3"`
	FCount4  int    `json:"f_count_4"`
	FCount5  int    `json:"f_count_5"`
	MAmount1 int64  `json:"m_amount_1"`
	MAmount2 int64  `json:"m_amount_2"`
	MAmount3 int64  `json:"m_amount_3"`
	MAmount4 int64  `json:"m_amount_4"`
	MAmount5 int64  `json:"m_amount_5"`
	// IsActive 用 *bool 表达 PATCH 语义：nil = 未传 = 保留原值。
	// 该接口当前无前端调用方，防御空 body/漏字段把激活状态意外翻转。
	IsActive *bool `json:"is_active"`
}

// SaveRFMRule 保存 RFM 规则
func (s *CustomerRFMService) SaveRFMRule(ctx context.Context, req *SaveRFMRuleRequest) (*model.RFMRule, error) {
	rule := &model.RFMRule{
		Name:     req.Name,
		RDays1:   req.RDays1,
		RDays2:   req.RDays2,
		RDays3:   req.RDays3,
		RDays4:   req.RDays4,
		RDays5:   req.RDays5,
		FCount1:  req.FCount1,
		FCount2:  req.FCount2,
		FCount3:  req.FCount3,
		FCount4:  req.FCount4,
		FCount5:  req.FCount5,
		MAmount1: req.MAmount1,
		MAmount2: req.MAmount2,
		MAmount3: req.MAmount3,
		MAmount4: req.MAmount4,
		MAmount5: req.MAmount5,
	}
	// 创建口径：未显式传 is_active 时默认启用
	if req.IsActive != nil {
		rule.IsActive = *req.IsActive
	} else {
		rule.IsActive = true
	}

	if rule.RDays1 <= 0 {
		rule.RDays1 = 7
	}

	err := s.ruleRepo().Create(ctx, rule)
	if err != nil {
		return nil, err
	}

	return rule, nil
}

// UpdateRFMRule 更新 RFM 规则
func (s *CustomerRFMService) UpdateRFMRule(ctx context.Context, id uint, req *SaveRFMRuleRequest) (*model.RFMRule, error) {
	rule, err := s.ruleRepo().GetByID(ctx, id)
	if err != nil {
		// D-40：保留仓储的 not-found 语义（gorm.ErrRecordNotFound），
		// controller 据 errhttp.IsNotFoundError 映射 404；原先包成
		// errors.New("规则不存在") 后只能落到 400，客户端分不清"没这条"和"参数错"。
		return nil, err
	}

	rule.Name = req.Name
	rule.RDays1 = req.RDays1
	rule.RDays2 = req.RDays2
	rule.RDays3 = req.RDays3
	rule.RDays4 = req.RDays4
	rule.RDays5 = req.RDays5
	rule.FCount1 = req.FCount1
	rule.FCount2 = req.FCount2
	rule.FCount3 = req.FCount3
	rule.FCount4 = req.FCount4
	rule.FCount5 = req.FCount5
	rule.MAmount1 = req.MAmount1
	rule.MAmount2 = req.MAmount2
	rule.MAmount3 = req.MAmount3
	rule.MAmount4 = req.MAmount4
	rule.MAmount5 = req.MAmount5
	// PATCH 语义：未传 is_active 保留原值，防止漏字段把规则意外停用
	if req.IsActive != nil {
		rule.IsActive = *req.IsActive
	}

	if err := s.ruleRepo().Update(ctx, rule); err != nil {
		return nil, err
	}

	return rule, nil
}

// DeleteRFMRule 删除 RFM 规则
func (s *CustomerRFMService) DeleteRFMRule(ctx context.Context, id uint) error {
	return s.ruleRepo().Delete(ctx, id)
}
