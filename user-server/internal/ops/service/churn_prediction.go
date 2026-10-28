package service

import (
	"context"
	"encoding/json"
	"errors"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/repository"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// ChurnPredictionService 流失预警服务
type ChurnPredictionService struct {
	db             *gorm.DB
	predictionRepo *repository.ChurnPredictionRepository
	warningRepo    *repository.ChurnWarningRepository
	configRepo     *repository.ChurnModelConfigRepository
	statsRepo      *repository.ChurnStatisticsRepository
}

// NewChurnPredictionService 创建流失预警服务实例
func NewChurnPredictionService() *ChurnPredictionService {
	return &ChurnPredictionService{
		db:             _db.GetDB(),
		predictionRepo: repository.NewChurnPredictionRepository(),
		warningRepo:    repository.NewChurnWarningRepository(),
		configRepo:     repository.NewChurnModelConfigRepository(),
		statsRepo:      repository.NewChurnStatisticsRepository(),
	}
}

// NewChurnPredictionServiceWithDB 创建指定数据库连接的流失预警服务实例（用于测试）
func NewChurnPredictionServiceWithDB(db *gorm.DB) *ChurnPredictionService {
	return &ChurnPredictionService{
		db:             db,
		predictionRepo: repository.NewChurnPredictionRepositoryWithDB(db),
		warningRepo:    repository.NewChurnWarningRepositoryWithDB(db),
		configRepo:     repository.NewChurnModelConfigRepositoryWithDB(db),
		statsRepo:      repository.NewChurnStatisticsRepositoryWithDB(db),
	}
}

// GetChurnPrediction 获取用户的流失预测
func (s *ChurnPredictionService) GetChurnPrediction(ctx context.Context, userID string) (*model.ChurnPrediction, error) {
	prediction, err := s.predictionRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.ChurnPrediction{
				UserID:            userID,
				ChurnScore:        0,
				ChurnRisk:         "low",
				RiskFactors:       "[]",
				DaysSinceActive:   0,
				PurchaseFreq:      0,
				AverageOrderValue: 0,
			}, nil
		}
		return nil, err
	}
	return prediction, nil
}

// GetChurnPredictions 获取流失预测列表
func (s *ChurnPredictionService) GetChurnPredictions(ctx context.Context, page, pageSize int) ([]*model.ChurnPrediction, int64, error) {
	return s.predictionRepo.GetAll(ctx, page, pageSize)
}

// GetHighRiskUsers 获取高风险用户列表
func (s *ChurnPredictionService) GetHighRiskUsers(ctx context.Context, limit int) ([]*model.ChurnPrediction, error) {
	return s.predictionRepo.GetHighRiskUsers(ctx, limit)
}

// CalculateChurnPrediction 计算用户流失分数
func (s *ChurnPredictionService) CalculateChurnPrediction(ctx context.Context, userID string, userData map[string]any) error {
	config, err := s.configRepo.GetCurrent(ctx)
	if err != nil || !isValidChurnConfig(config) {
		config = s.getDefaultConfig()
	}

	inactiveScore := s.calculateInactiveScore(userData, config.InactiveThreshold)
	purchaseFreqScore := s.calculatePurchaseFreqScore(userData, config.PurchaseThreshold)
	orderValueScore := s.calculateOrderValueScore(userData)
	engagementScore := s.calculateEngagementScore(userData)

	// 客单价数据不可信（缺金额/多币种）时按 unknown 处理：不补 0 元真值，
	// 也不把缺数据冒充低风险，而是排除该分量、其余权重按比例归一后加权。
	// 泛化 map 入参未带 known 标志时保持旧行为（历史调用方语义不变）。
	aovKnown := true
	if known, ok := userData["average_order_value_known"].(bool); ok {
		aovKnown = known
	}
	inactiveW := config.InactiveDaysWeight
	purchaseFreqW := config.PurchaseFreqWeight
	orderValueW := config.OrderValueWeight
	engagementW := config.EngagementWeight
	if !aovKnown {
		orderValueW = 0
	}
	weightSum := inactiveW + purchaseFreqW + orderValueW + engagementW

	churnScore := (inactiveScore*inactiveW +
		purchaseFreqScore*purchaseFreqW +
		orderValueScore*orderValueW +
		engagementScore*engagementW) / weightSum

	var churnRisk string
	if churnScore >= config.CriticalRiskScore {
		churnRisk = "critical"
	} else if churnScore >= config.HighRiskScore {
		churnRisk = "high"
	} else if churnScore >= 50 {
		churnRisk = "medium"
	} else {
		churnRisk = "low"
	}

	riskFactors := s.identifyRiskFactors(inactiveScore, purchaseFreqScore, orderValueScore, engagementScore)
	if !aovKnown {
		// 金额未知时“订单金额偏低”不是证据，仅是缺数据——不得进入风险因子。
		for i, f := range riskFactors {
			if f == "订单金额偏低" {
				riskFactors = append(riskFactors[:i], riskFactors[i+1:]...)
				break
			}
		}
	}
	riskFactorsJSON, _ := json.Marshal(riskFactors)

	prediction := &model.ChurnPrediction{
		UserID:          userID,
		ChurnScore:      churnScore,
		ChurnRisk:       churnRisk,
		RiskFactors:     string(riskFactorsJSON),
		DaysSinceActive: int(inactiveScore),
		PurchaseFreq:    purchaseFreqScore,
		// 金额未知存 0 表示“无数据”（可信 AOV 恒为正数，不会与 0 元真值混淆）；
		// 已知时仍存该分量得分，与既有行语义一致。
		AverageOrderValue: orderValueScore * boolToFloat(aovKnown),
	}

	if lastActivity, ok := userData["last_activity_at"].(time.Time); ok {
		prediction.LastActivityAt = &lastActivity
	}
	if lastPurchase, ok := userData["last_purchase_at"].(time.Time); ok {
		prediction.LastPurchaseAt = &lastPurchase
	}

	return s.predictionRepo.Upsert(ctx, prediction)
}

func (s *ChurnPredictionService) calculateInactiveScore(userData map[string]any, threshold int) float64 {
	days := 0
	if d, ok := userData["days_since_active"].(int); ok {
		days = d
	}

	if days >= threshold*2 {
		return 100
	}
	if days >= threshold {
		return 50 + float64(days-threshold)*50/float64(threshold)
	}
	return float64(days) * 50 / float64(threshold)
}

func (s *ChurnPredictionService) calculatePurchaseFreqScore(userData map[string]any, threshold int) float64 {
	days := 0
	if d, ok := userData["days_since_purchase"].(int); ok {
		days = d
	}

	if days >= threshold*2 {
		return 100
	}
	if days >= threshold {
		return 50 + float64(days-threshold)*50/float64(threshold)
	}
	return float64(days) * 50 / float64(threshold)
}

func (s *ChurnPredictionService) calculateOrderValueScore(userData map[string]any) float64 {
	aov := 0.0
	if v, ok := userData["average_order_value"].(float64); ok {
		aov = v
	}

	if aov <= 0 {
		return 100
	}
	if aov < 50 {
		return 80
	}
	if aov < 100 {
		return 50
	}
	if aov < 500 {
		return 30
	}
	return 10
}

func (s *ChurnPredictionService) calculateEngagementScore(userData map[string]any) float64 {
	interactions := 0
	if i, ok := userData["interactions_30d"].(int); ok {
		interactions = i
	}

	if interactions == 0 {
		return 100
	}
	if interactions < 3 {
		return 70
	}
	if interactions < 10 {
		return 40
	}
	return 20
}

func (s *ChurnPredictionService) identifyRiskFactors(inactiveScore, purchaseFreqScore, orderValueScore, engagementScore float64) []string {
	var factors []string

	if inactiveScore >= 70 {
		factors = append(factors, "长期未活跃")
	}
	if purchaseFreqScore >= 70 {
		factors = append(factors, "购买频率下降")
	}
	if orderValueScore >= 70 {
		factors = append(factors, "订单金额偏低")
	}
	if engagementScore >= 70 {
		factors = append(factors, "互动频率降低")
	}

	return factors
}

// CreateChurnWarning 创建流失预警
func (s *ChurnPredictionService) CreateChurnWarning(ctx context.Context, userID string, prediction *model.ChurnPrediction) error {
	var warningLevel, warningType, description, suggestion string

	switch prediction.ChurnRisk {
	case "critical":
		warningLevel = "critical"
		warningType = "churn_critical"
		description = "用户流失风险极高，需要立即关注"
		suggestion = "建议立即联系用户，提供专属优惠或关怀活动"
	case "high":
		warningLevel = "high"
		warningType = "churn_high"
		description = "用户流失风险较高，需要重点关注"
		suggestion = "建议发送个性化营销内容，提高用户活跃度"
	case "medium":
		warningLevel = "medium"
		warningType = "churn_medium"
		description = "用户存在一定的流失风险"
		suggestion = "建议关注用户行为，适时进行用户触达"
	default:
		return nil
	}

	warning := &model.ChurnWarning{
		UserID:       userID,
		WarningLevel: warningLevel,
		WarningType:  warningType,
		Description:  description,
		Suggestion:   suggestion,
		IsHandled:    false,
	}

	existing, _, err := s.warningRepo.GetUnhandled(ctx, 1, 200)
	if err == nil {
		for _, w := range existing {
			if w.UserID == userID && w.WarningType == warningType {
				return nil
			}
		}
	}

	return s.warningRepo.Create(ctx, warning)
}

// GetChurnWarnings 获取流失预警列表
func (s *ChurnPredictionService) GetChurnWarnings(ctx context.Context, page, pageSize int) ([]*model.ChurnWarning, int64, error) {
	return s.warningRepo.GetAll(ctx, page, pageSize)
}

// GetUnhandledWarnings 获取未处理的流失预警
func (s *ChurnPredictionService) GetUnhandledWarnings(ctx context.Context, page, pageSize int) ([]*model.ChurnWarning, int64, error) {
	return s.warningRepo.GetUnhandled(ctx, page, pageSize)
}

// MarkWarningHandled 标记预警为已处理
func (s *ChurnPredictionService) MarkWarningHandled(ctx context.Context, id, handledBy uint, note string) error {
	return s.warningRepo.MarkHandled(ctx, id, handledBy, note)
}

// InterveneWarning 对流失预警进行干预，记录干预类型
func (s *ChurnPredictionService) InterveneWarning(ctx context.Context, id, handledBy uint, interventionType string) error {
	note := "干预类型: " + interventionType
	return s.warningRepo.MarkHandled(ctx, id, handledBy, note)
}

// GetModelConfig 获取模型配置
func (s *ChurnPredictionService) GetModelConfig(ctx context.Context) (*model.ChurnModelConfig, error) {
	config, err := s.configRepo.GetCurrent(ctx)
	if err != nil {
		return s.getDefaultConfig(), nil
	}
	return config, nil
}

// SaveModelConfig 保存模型配置
func (s *ChurnPredictionService) SaveModelConfig(ctx context.Context, config *model.ChurnModelConfig) error {
	return s.configRepo.Upsert(ctx, config)
}

func (s *ChurnPredictionService) getDefaultConfig() *model.ChurnModelConfig {
	return &model.ChurnModelConfig{
		InactiveDaysWeight: 0.3,
		PurchaseFreqWeight: 0.3,
		OrderValueWeight:   0.2,
		EngagementWeight:   0.2,
		InactiveThreshold:  30,
		PurchaseThreshold:  60,
		HighRiskScore:      70,
		CriticalRiskScore:  85,
	}
}

func isValidChurnConfig(c *model.ChurnModelConfig) bool {
	if c == nil {
		return false
	}
	weightSum := c.InactiveDaysWeight + c.PurchaseFreqWeight + c.OrderValueWeight + c.EngagementWeight
	if weightSum <= 0 {
		return false
	}
	if c.InactiveThreshold <= 0 || c.PurchaseThreshold <= 0 {
		return false
	}
	if c.HighRiskScore <= 0 || c.CriticalRiskScore <= 0 {
		return false
	}
	return true
}

// GetChurnStatistics 获取流失统计
func (s *ChurnPredictionService) GetChurnStatistics(ctx context.Context, startDate, endDate string) ([]*model.ChurnStatistics, error) {
	return s.statsRepo.GetAll(ctx, startDate, endDate)
}

// CalculateDailyStatistics 计算每日统计
func (s *ChurnPredictionService) CalculateDailyStatistics(ctx context.Context, date string) error {
	predictions, err := s.predictionRepo.ListAll(ctx, 5000)
	if err != nil {
		return err
	}

	totalUsers := len(predictions)
	churnUsers := 0
	highRiskUsers := 0
	criticalUsers := 0

	for _, p := range predictions {
		if p.ChurnRisk == "high" {
			highRiskUsers++
		} else if p.ChurnRisk == "critical" {
			criticalUsers++
		}
		if p.ChurnScore >= 70 {
			churnUsers++
		}
	}

	churnRate := 0.0
	if totalUsers > 0 {
		churnRate = float64(churnUsers) / float64(totalUsers) * 100
	}

	stats := &model.ChurnStatistics{
		StatDate:      date,
		TotalUsers:    totalUsers,
		ChurnUsers:    churnUsers,
		ChurnRate:     churnRate,
		HighRiskUsers: highRiskUsers,
		CriticalUsers: criticalUsers,
	}

	return s.statsRepo.Create(ctx, stats)
}

// RunChurnCalculation 运行流失计算（定时任务）
func (s *ChurnPredictionService) RunChurnCalculation(ctx context.Context, users []map[string]any) error {
	for _, user := range users {
		userID, ok := user["user_id"].(string)
		if !ok {
			continue
		}

		if err := s.CalculateChurnPrediction(ctx, userID, user); err != nil {
			continue
		}

		prediction, _ := s.predictionRepo.GetByUserID(ctx, userID)
		if prediction != nil {
			if err := s.CreateChurnWarning(ctx, userID, prediction); err != nil {
				logger.Warnf("[Churn] 创建流失预警失败 userID=%s: %v", userID, err)
			}
		}
	}

	if err := s.configRepo.UpdateCalcTime(ctx); err != nil {
		logger.Warnf("[Churn] 更新计算时间失败: %v", err)
	}

	date := timeutil.BusinessToday()
	if err := s.CalculateDailyStatistics(ctx, date); err != nil {
		logger.Warnf("[Churn] 计算每日统计失败 date=%s: %v", date, err)
	}

	return nil
}

// UserBehaviorSnapshot 单个客户从 customer_events 聚合出的真实行为快照
type UserBehaviorSnapshot struct {
	CustomerID        string
	DaysSinceActive   int
	LastActivityAt    time.Time
	Interactions30d   int
	DaysSincePurchase int
	LastPurchaseAt    time.Time
	PurchaseFreq      int
	AverageOrderValue float64
	// AOVKnown 表示客单价是否由可信金额算出。事件缺金额、金额非法或
	// 多币种混加时为 false——调用方必须按 unknown 处理（排除该分量重归一），
	// 不得当作 0 元真值，也不得从购买次数反推金额。
	AOVKnown    bool
	AOVCurrency string
}

// DebugAggregate 暴露聚合结果用于调试/测试断言
func (s *ChurnPredictionService) DebugAggregate(ctx context.Context) ([]UserBehaviorSnapshot, error) {
	return s.aggregateCustomerBehavior(ctx)
}

// GetPredictionByUserID 按用户 ID 获取流失预测（用于调试/验证）
func (s *ChurnPredictionService) GetPredictionByUserID(ctx context.Context, userID string) (*model.ChurnPrediction, error) {
	return s.predictionRepo.GetByUserID(ctx, userID)
}

func (s *ChurnPredictionService) aggregateCustomerBehavior(ctx context.Context) ([]UserBehaviorSnapshot, error) {
	type row struct {
		CustomerID       string
		LastActivityAt   time.Time
		Interactions30d  int64
		LastPurchaseAt   *time.Time
		PurchaseCount90d int64
	}

	var rows []row
	// 别名必须与 GORM 对字段名的 snake_case 映射一致：PurchaseCount90d→
	// purchase_count90d（数字前不加下划线）。旧别名 purchase_count_90d 与
	// interactions_30d 映射失配，频次/互动数在生产聚合里一直静默扫成 0。
	if err := s.db.WithContext(ctx).Table("customer_events").
		Select("customer_id, " +
			"MAX(occurred_at) AS last_activity_at, " +
			"COUNT(*) FILTER (WHERE occurred_at >= NOW() - INTERVAL '30 days') AS interactions30d, " +
			"MAX(occurred_at) FILTER (WHERE event_type = 'purchase') AS last_purchase_at, " +
			"COUNT(*) FILTER (WHERE event_type = 'purchase' " +
			"AND occurred_at >= NOW() - INTERVAL '90 days') AS purchase_count90d").
		Group("customer_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	// 金额必须取自事件 payload 的真实 amount 字段。只拉 90 天窗口内的购买事件，
	// 在 Go 侧解析 EventData JSON：缺金额/非法金额不计入，多币种混加判 unknown，
	// 同一 order_id 的重复事件只记一次业务金额。
	type purchaseRow struct {
		CustomerID string
		EventData  string
	}
	var purchaseRows []purchaseRow
	if err := s.db.WithContext(ctx).Table("customer_events").
		Select("customer_id, event_data").
		Where("event_type = ? AND occurred_at >= NOW() - INTERVAL '90 days'", "purchase").
		Scan(&purchaseRows).Error; err != nil {
		return nil, err
	}

	type aovAcc struct {
		sum        float64
		validCount int
		orders     int
		currency   string
		mixed      bool
		seen       map[string]struct{}
	}
	aovByCustomer := make(map[string]*aovAcc)
	for _, pr := range purchaseRows {
		data := parsePurchaseEventData(pr.EventData)
		acc := aovByCustomer[pr.CustomerID]
		if acc == nil {
			acc = &aovAcc{seen: make(map[string]struct{})}
			aovByCustomer[pr.CustomerID] = acc
		}
		if orderID, ok := data["order_id"].(string); ok && orderID != "" {
			if _, dup := acc.seen[orderID]; dup {
				continue
			}
			acc.seen[orderID] = struct{}{}
		}
		acc.orders++
		amount, ok := purchaseAmount(data)
		if !ok || amount <= 0 || math.IsInf(amount, 0) || math.IsNaN(amount) {
			continue
		}
		currency, _ := data["currency"].(string)
		if acc.validCount == 0 {
			acc.currency = currency
		} else if acc.currency != currency {
			acc.mixed = true
		}
		acc.sum += amount
		acc.validCount++
	}

	now := time.Now()
	snaps := make([]UserBehaviorSnapshot, 0, len(rows))
	for _, r := range rows {
		snap := UserBehaviorSnapshot{
			CustomerID:      r.CustomerID,
			LastActivityAt:  r.LastActivityAt,
			Interactions30d: int(r.Interactions30d),
		}
		if acc := aovByCustomer[r.CustomerID]; acc != nil {
			// 频次与金额同源：按 order_id 去重后的订单数（无 order_id 的事件各记一次）。
			snap.PurchaseFreq = acc.orders
			if acc.validCount > 0 && !acc.mixed {
				snap.AverageOrderValue = acc.sum / float64(acc.validCount)
				snap.AOVKnown = true
				snap.AOVCurrency = acc.currency
			}
		} else {
			// 90 天窗口外/无 payload 行，退回 SQL 计数。
			snap.PurchaseFreq = int(r.PurchaseCount90d)
		}
		if !r.LastActivityAt.IsZero() {
			snap.DaysSinceActive = int(now.Sub(r.LastActivityAt).Hours() / 24)
		}
		if r.LastPurchaseAt != nil && !r.LastPurchaseAt.IsZero() {
			snap.LastPurchaseAt = *r.LastPurchaseAt
			snap.DaysSincePurchase = int(now.Sub(*r.LastPurchaseAt).Hours() / 24)
		} else {
			snap.DaysSincePurchase = 9999
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

// parsePurchaseEventData 解析购买事件 payload，坏 JSON 返回空 map（金额记 unknown，不猜值）。
func parsePurchaseEventData(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

// purchaseAmount 从事件 payload 提取金额；兼容 float64 与 json.Number 之外的字符串数字。
func purchaseAmount(data map[string]any) (float64, bool) {
	switch v := data["amount"].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// RunChurnCalculationForAllCustomers 基于真实 customer_events 数据对全部客户执行流失计算。
// 解决此前 churn_predictions 表永远为空（RunChurnCalculation 无生产调用方）的断头功能问题。
func (s *ChurnPredictionService) RunChurnCalculationForAllCustomers(ctx context.Context) (int, error) {
	snaps, err := s.aggregateCustomerBehavior(ctx)
	if err != nil {
		return 0, err
	}

	users := make([]map[string]any, 0, len(snaps))
	for _, snap := range snaps {
		u := map[string]any{
			"user_id":                      snap.CustomerID,
			"days_since_active":            snap.DaysSinceActive,
			"last_activity_at":             snap.LastActivityAt,
			"interactions_30d":             snap.Interactions30d,
			"days_since_purchase":          snap.DaysSincePurchase,
			"last_purchase_at":             snap.LastPurchaseAt,
			"purchase_freq":                snap.PurchaseFreq,
			"average_order_value":          snap.AverageOrderValue,
			"average_order_value_known":    snap.AOVKnown,
			"average_order_value_currency": snap.AOVCurrency,
		}
		users = append(users, u)
	}

	if err := s.RunChurnCalculation(ctx, users); err != nil {
		return 0, err
	}
	return len(users), nil
}

// GetRiskDistribution 获取风险分布
func (s *ChurnPredictionService) GetRiskDistribution(ctx context.Context) (map[string]int, error) {
	predictions, err := s.predictionRepo.ListAll(ctx, 5000)
	if err != nil {
		return nil, err
	}

	distribution := map[string]int{
		"low":      0,
		"medium":   0,
		"high":     0,
		"critical": 0,
	}

	for _, p := range predictions {
		distribution[p.ChurnRisk]++
	}

	return distribution, nil
}

// GenerateSuggestion 生成挽回建议
func (s *ChurnPredictionService) GenerateSuggestion(prediction *model.ChurnPrediction, userData map[string]any) string {
	var riskFactors []string
	if err := json.Unmarshal([]byte(prediction.RiskFactors), &riskFactors); err != nil {
		logger.Warnf("[Churn] 解析风险因子失败: %v", err)
	}

	suggestions := []string{}

	for _, factor := range riskFactors {
		switch factor {
		case "长期未活跃":
			suggestions = append(suggestions, "发送活跃提醒消息，提供登录奖励")
		case "购买频率下降":
			suggestions = append(suggestions, "推送个性化商品推荐，提供限时优惠")
		case "订单金额偏低":
			suggestions = append(suggestions, "提供满减优惠券，鼓励提高客单价")
		case "互动频率降低":
			suggestions = append(suggestions, "邀请参与互动活动，增加品牌曝光")
		}
	}

	if len(suggestions) == 0 {
		return "用户风险较低，保持正常运营即可"
	}

	return "建议措施：" + joinStrings(suggestions, "；")
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// CalculateConfidence 计算置信度（使用正态分布）
func CalculateConfidence(rate float64, sampleSize int) float64 {
	if sampleSize == 0 {
		return 0
	}

	p := rate / 100
	n := float64(sampleSize)

	se := math.Sqrt(p * (1 - p) / n)

	if se == 0 {
		return 0
	}

	z := (p - 0.5) / se

	confidence := 0.5 * (1 + math.Erf(z/math.Sqrt2))

	return confidence
}
